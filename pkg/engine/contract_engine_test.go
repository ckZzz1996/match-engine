package engine

import (
	"testing"
	"time"

	"match-engine/pkg/funding"
	"match-engine/pkg/liquidation"
	"match-engine/pkg/position"
	"match-engine/pkg/types"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

func newTestContractEngine(symbol string) *ContractMatchEngine {
	eventChan := make(chan types.Event, 1000)
	positionMgr := position.NewManager()
	logger, _ := zap.NewDevelopment()
	fundingSvc := funding.NewService(positionMgr, logger)
	liquidationEng := liquidation.NewEngine(positionMgr, fundingSvc, logger)

	contract := &types.Contract{
		Symbol:          symbol,
		BaseAsset:       "BTC",
		QuoteAsset:      "USDT",
		ContractType:    types.ContractTypePerpetual,
		ContractSize:    decimal.NewFromInt(1),
		TickSize:        decimal.NewFromFloat(0.1),
		LotSize:         decimal.NewFromFloat(0.001),
		MaxLeverage:     125,
		MakerFeeRate:    decimal.NewFromFloat(0.0002),
		TakerFeeRate:    decimal.NewFromFloat(0.0004),
		MaintenanceRate: decimal.NewFromFloat(0.004),
		InitialRate:     decimal.NewFromFloat(0.01),
		MaxOrderQty:     decimal.NewFromInt(1000),
		MaxPositionQty:  decimal.NewFromInt(10000),
		FundingInterval: 28800,
	}

	positionMgr.RegisterContract(contract)

	return NewContractMatchEngine(symbol, eventChan, positionMgr, liquidationEng, fundingSvc, contract)
}

func TestContractMatchEngine_OpenPosition(t *testing.T) {
	engine := newTestContractEngine("BTC-USDT-PERP")

	// 先添加一些卖单到订单簿
	sellOrder := &types.Order{
		OrderID:     "sell1",
		UserID:      "maker1",
		Symbol:      "BTC-USDT-PERP",
		Side:        types.OrderSideSell,
		Type:        types.OrderTypeLimit,
		Price:       decimal.NewFromFloat(50000),
		Quantity:    decimal.NewFromFloat(1),
		TimeInForce: types.TimeInForceGTC,
		CreateTime:  time.Now().UnixNano(),
	}
	engine.MatchEngine.ProcessOrder(sellOrder)

	// 创建开多仓订单
	order := &types.ContractOrder{
		Order: &types.Order{
			OrderID:     "order1",
			UserID:      "user1",
			Symbol:      "BTC-USDT-PERP",
			Side:        types.OrderSideBuy,
			Type:        types.OrderTypeLimit,
			Price:       decimal.NewFromFloat(50000),
			Quantity:    decimal.NewFromFloat(0.5),
			TimeInForce: types.TimeInForceGTC,
			CreateTime:  time.Now().UnixNano(),
		},
		PositionSide: types.PositionSideLong,
		Leverage:     10,
		MarginType:   types.MarginTypeCross,
	}

	// 更新标记价格
	engine.UpdateMarkPrice(decimal.NewFromFloat(50000))

	result := engine.ProcessContractOrder(order)

	if result.Rejected {
		t.Errorf("Order should not be rejected: %s", result.RejectReason)
	}

	if result.MatchResult == nil {
		t.Error("MatchResult should not be nil")
	}

	if result.MatchResult.TotalFilledQty().IsZero() {
		t.Error("Order should have been filled")
	}

	// 检查仓位
	pos, ok := engine.positionMgr.GetPosition("user1", "BTC-USDT-PERP_LONG")
	if !ok {
		t.Error("Position should exist")
	}

	if pos.Quantity.String() != "0.5" {
		t.Errorf("Position quantity should be 0.5, got %s", pos.Quantity.String())
	}

	if pos.Side != types.PositionSideLong {
		t.Errorf("Position side should be LONG, got %s", pos.Side.String())
	}
}

func TestContractMatchEngine_ClosePosition(t *testing.T) {
	engine := newTestContractEngine("BTC-USDT-PERP")

	// 更新标记价格
	engine.UpdateMarkPrice(decimal.NewFromFloat(50000))

	// 先创建一个仓位
	engine.positionMgr.UpdatePosition(
		"user1", "BTC-USDT-PERP", types.PositionSideLong,
		decimal.NewFromFloat(50000), decimal.NewFromFloat(1),
		types.MarginTypeCross, 10,
	)

	// 添加买单到订单簿（作为平仓的对手盘）
	buyOrder := &types.Order{
		OrderID:     "buy1",
		UserID:      "maker1",
		Symbol:      "BTC-USDT-PERP",
		Side:        types.OrderSideBuy,
		Type:        types.OrderTypeLimit,
		Price:       decimal.NewFromFloat(51000),
		Quantity:    decimal.NewFromFloat(1),
		TimeInForce: types.TimeInForceGTC,
		CreateTime:  time.Now().UnixNano(),
	}
	engine.MatchEngine.ProcessOrder(buyOrder)

	// 创建平仓订单
	closeOrder := &types.ContractOrder{
		Order: &types.Order{
			OrderID:     "close1",
			UserID:      "user1",
			Symbol:      "BTC-USDT-PERP",
			Side:        types.OrderSideSell,
			Type:        types.OrderTypeMarket,
			Quantity:    decimal.NewFromFloat(1),
			TimeInForce: types.TimeInForceIOC,
			CreateTime:  time.Now().UnixNano(),
		},
		PositionSide:  types.PositionSideLong,
		Leverage:      10,
		MarginType:    types.MarginTypeCross,
		ReduceOnly:    true,
		ClosePosition: true,
	}

	result := engine.ProcessContractOrder(closeOrder)

	if result.Rejected {
		t.Errorf("Close order should not be rejected: %s", result.RejectReason)
	}

	// 检查仓位应该已清空
	pos, _ := engine.positionMgr.GetPosition("user1", "BTC-USDT-PERP_LONG")
	if pos != nil && !pos.Quantity.IsZero() {
		t.Errorf("Position should be closed, got quantity: %s", pos.Quantity.String())
	}
}

func TestContractMatchEngine_Leverage(t *testing.T) {
	engine := newTestContractEngine("BTC-USDT-PERP")

	// 测试无效杠杆
	order := &types.ContractOrder{
		Order: &types.Order{
			OrderID:     "order1",
			UserID:      "user1",
			Symbol:      "BTC-USDT-PERP",
			Side:        types.OrderSideBuy,
			Type:        types.OrderTypeLimit,
			Price:       decimal.NewFromFloat(50000),
			Quantity:    decimal.NewFromFloat(0.1),
			TimeInForce: types.TimeInForceGTC,
			CreateTime:  time.Now().UnixNano(),
		},
		PositionSide: types.PositionSideLong,
		Leverage:     200, // 超过最大杠杆
		MarginType:   types.MarginTypeCross,
	}

	result := engine.ProcessContractOrder(order)

	if !result.Rejected {
		t.Error("Order with invalid leverage should be rejected")
	}
}

func TestContractMatchEngine_ReduceOnly(t *testing.T) {
	engine := newTestContractEngine("BTC-USDT-PERP")

	// 更新标记价格
	engine.UpdateMarkPrice(decimal.NewFromFloat(50000))

	// 没有仓位时发送减仓单应该被拒绝
	reduceOrder := &types.ContractOrder{
		Order: &types.Order{
			OrderID:     "reduce1",
			UserID:      "user1",
			Symbol:      "BTC-USDT-PERP",
			Side:        types.OrderSideSell,
			Type:        types.OrderTypeMarket,
			Quantity:    decimal.NewFromFloat(0.5),
			TimeInForce: types.TimeInForceIOC,
			CreateTime:  time.Now().UnixNano(),
		},
		PositionSide: types.PositionSideLong,
		Leverage:     10,
		MarginType:   types.MarginTypeCross,
		ReduceOnly:   true,
	}

	result := engine.ProcessContractOrder(reduceOrder)

	if !result.Rejected {
		t.Error("Reduce only order without position should be rejected")
	}

	if result.RejectReason != "no position to reduce" {
		t.Errorf("Unexpected reject reason: %s", result.RejectReason)
	}
}

func TestContractMatchEngine_TPSL(t *testing.T) {
	engine := newTestContractEngine("BTC-USDT-PERP")

	// 更新标记价格
	engine.UpdateMarkPrice(decimal.NewFromFloat(50000))

	// 创建一个多头仓位
	engine.positionMgr.UpdatePosition(
		"user1", "BTC-USDT-PERP", types.PositionSideLong,
		decimal.NewFromFloat(50000), decimal.NewFromFloat(1),
		types.MarginTypeCross, 10,
	)

	// 设置止盈止损
	tpPrice := decimal.NewFromFloat(55000)
	slPrice := decimal.NewFromFloat(48000)

	tpsl := engine.SetPositionTPSL("user1", "BTC-USDT-PERP", types.PositionSideLong, tpPrice, slPrice)

	if tpsl == nil {
		t.Error("TPSL should not be nil")
	}

	if !tpsl.TakeProfitPrice.Equal(tpPrice) {
		t.Errorf("Take profit price should be %s, got %s", tpPrice.String(), tpsl.TakeProfitPrice.String())
	}

	if !tpsl.StopLossPrice.Equal(slPrice) {
		t.Errorf("Stop loss price should be %s, got %s", slPrice.String(), tpsl.StopLossPrice.String())
	}

	// 获取止盈止损
	retrievedTPSL := engine.GetPositionTPSL("user1", "BTC-USDT-PERP", types.PositionSideLong)

	if retrievedTPSL == nil {
		t.Error("Retrieved TPSL should not be nil")
	}

	if !retrievedTPSL.TakeProfitPrice.Equal(tpPrice) {
		t.Errorf("Retrieved take profit price should be %s, got %s", tpPrice.String(), retrievedTPSL.TakeProfitPrice.String())
	}
}

func TestContractMatchEngine_Stats(t *testing.T) {
	engine := newTestContractEngine("BTC-USDT-PERP")

	// 更新标记价格
	engine.UpdateMarkPrice(decimal.NewFromFloat(50000))

	stats := engine.Stats()

	if stats["symbol"] != "BTC-USDT-PERP" {
		t.Errorf("Symbol should be BTC-USDT-PERP, got %s", stats["symbol"])
	}

	if stats["contract_type"] != "PERPETUAL" {
		t.Errorf("Contract type should be PERPETUAL, got %s", stats["contract_type"])
	}

	if stats["max_leverage"] != 125 {
		t.Errorf("Max leverage should be 125, got %d", stats["max_leverage"])
	}

	if stats["mark_price"] != "50000" {
		t.Errorf("Mark price should be 50000, got %s", stats["mark_price"])
	}
}

func TestContractMatchEngine_CalculateFee(t *testing.T) {
	engine := newTestContractEngine("BTC-USDT-PERP")

	quantity := decimal.NewFromFloat(1)
	price := decimal.NewFromFloat(50000)

	// Maker fee
	makerFee := engine.CalculateFee(quantity, price, true)
	expectedMakerFee := decimal.NewFromFloat(10) // 50000 * 1 * 0.0002

	if !makerFee.Equal(expectedMakerFee) {
		t.Errorf("Maker fee should be %s, got %s", expectedMakerFee.String(), makerFee.String())
	}

	// Taker fee
	takerFee := engine.CalculateFee(quantity, price, false)
	expectedTakerFee := decimal.NewFromFloat(20) // 50000 * 1 * 0.0004

	if !takerFee.Equal(expectedTakerFee) {
		t.Errorf("Taker fee should be %s, got %s", expectedTakerFee.String(), takerFee.String())
	}
}

func TestContractMatchEngine_CalculateRequiredMargin(t *testing.T) {
	engine := newTestContractEngine("BTC-USDT-PERP")

	quantity := decimal.NewFromFloat(1)
	price := decimal.NewFromFloat(50000)
	leverage := 10

	margin := engine.CalculateRequiredMargin(quantity, price, leverage)
	expectedMargin := decimal.NewFromFloat(5000) // 50000 * 1 / 10

	if !margin.Equal(expectedMargin) {
		t.Errorf("Required margin should be %s, got %s", expectedMargin.String(), margin.String())
	}
}
