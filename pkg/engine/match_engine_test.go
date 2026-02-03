package engine

import (
	"testing"
	"time"

	"match-engine/pkg/types"

	"github.com/shopspring/decimal"
)

func TestMatchEngine_LimitOrder(t *testing.T) {
	eventChan := make(chan types.Event, 1000)
	engine := NewMatchEngine("BTC-USDT", eventChan)

	// 创建卖单
	sellOrder := &types.Order{
		OrderID:     "sell-1",
		UserID:      "user-1",
		Symbol:      "BTC-USDT",
		Side:        types.OrderSideSell,
		Type:        types.OrderTypeLimit,
		Price:       decimal.NewFromInt(50000),
		Quantity:    decimal.NewFromInt(1),
		FilledQty:   decimal.Zero,
		Status:      types.OrderStatusNew,
		TimeInForce: types.TimeInForceGTC,
		CreateTime:  time.Now().UnixNano(),
	}

	result := engine.ProcessOrder(sellOrder)
	if result.Rejected {
		t.Errorf("sell order should not be rejected: %s", result.RejectReason)
	}
	if len(result.Trades) != 0 {
		t.Error("no trades should happen")
	}

	// 创建买单 - 价格高于卖单
	buyOrder := &types.Order{
		OrderID:     "buy-1",
		UserID:      "user-2",
		Symbol:      "BTC-USDT",
		Side:        types.OrderSideBuy,
		Type:        types.OrderTypeLimit,
		Price:       decimal.NewFromInt(50000),
		Quantity:    decimal.NewFromFloat(0.5),
		FilledQty:   decimal.Zero,
		Status:      types.OrderStatusNew,
		TimeInForce: types.TimeInForceGTC,
		CreateTime:  time.Now().UnixNano(),
	}

	result = engine.ProcessOrder(buyOrder)
	if result.Rejected {
		t.Errorf("buy order should not be rejected: %s", result.RejectReason)
	}
	if len(result.Trades) != 1 {
		t.Errorf("expected 1 trade, got %d", len(result.Trades))
	}
	if !result.Trades[0].Quantity.Equal(decimal.NewFromFloat(0.5)) {
		t.Errorf("expected trade quantity 0.5, got %s", result.Trades[0].Quantity)
	}
	if !result.Trades[0].Price.Equal(decimal.NewFromInt(50000)) {
		t.Errorf("expected trade price 50000, got %s", result.Trades[0].Price)
	}
}

func TestMatchEngine_MarketOrder(t *testing.T) {
	eventChan := make(chan types.Event, 1000)
	engine := NewMatchEngine("BTC-USDT", eventChan)

	// 先添加多个卖单
	for i := 1; i <= 5; i++ {
		sellOrder := &types.Order{
			OrderID:     "sell-" + string(rune('0'+i)),
			UserID:      "user-1",
			Symbol:      "BTC-USDT",
			Side:        types.OrderSideSell,
			Type:        types.OrderTypeLimit,
			Price:       decimal.NewFromInt(int64(50000 + i*100)),
			Quantity:    decimal.NewFromInt(1),
			FilledQty:   decimal.Zero,
			Status:      types.OrderStatusNew,
			TimeInForce: types.TimeInForceGTC,
			CreateTime:  time.Now().UnixNano(),
		}
		engine.ProcessOrder(sellOrder)
	}

	// 市价买入3个
	marketOrder := &types.Order{
		OrderID:     "market-1",
		UserID:      "user-2",
		Symbol:      "BTC-USDT",
		Side:        types.OrderSideBuy,
		Type:        types.OrderTypeMarket,
		Price:       decimal.Zero,
		Quantity:    decimal.NewFromInt(3),
		FilledQty:   decimal.Zero,
		Status:      types.OrderStatusNew,
		TimeInForce: types.TimeInForceGTC,
		CreateTime:  time.Now().UnixNano(),
	}

	result := engine.ProcessOrder(marketOrder)
	if result.Rejected {
		t.Errorf("market order should not be rejected: %s", result.RejectReason)
	}
	if len(result.Trades) != 3 {
		t.Errorf("expected 3 trades, got %d", len(result.Trades))
	}

	// 验证成交价格是否按最优价格排序
	expectedPrices := []int64{50100, 50200, 50300}
	for i, trade := range result.Trades {
		if !trade.Price.Equal(decimal.NewFromInt(expectedPrices[i])) {
			t.Errorf("trade %d: expected price %d, got %s", i, expectedPrices[i], trade.Price)
		}
	}
}

func TestMatchEngine_IOC(t *testing.T) {
	eventChan := make(chan types.Event, 1000)
	engine := NewMatchEngine("BTC-USDT", eventChan)

	// 添加卖单
	sellOrder := &types.Order{
		OrderID:     "sell-1",
		UserID:      "user-1",
		Symbol:      "BTC-USDT",
		Side:        types.OrderSideSell,
		Type:        types.OrderTypeLimit,
		Price:       decimal.NewFromInt(50000),
		Quantity:    decimal.NewFromInt(1),
		FilledQty:   decimal.Zero,
		Status:      types.OrderStatusNew,
		TimeInForce: types.TimeInForceGTC,
		CreateTime:  time.Now().UnixNano(),
	}
	engine.ProcessOrder(sellOrder)

	// IOC买单，数量大于卖单
	iocOrder := &types.Order{
		OrderID:     "ioc-1",
		UserID:      "user-2",
		Symbol:      "BTC-USDT",
		Side:        types.OrderSideBuy,
		Type:        types.OrderTypeLimit,
		Price:       decimal.NewFromInt(50000),
		Quantity:    decimal.NewFromInt(2),
		FilledQty:   decimal.Zero,
		Status:      types.OrderStatusNew,
		TimeInForce: types.TimeInForceIOC,
		CreateTime:  time.Now().UnixNano(),
	}

	result := engine.ProcessOrder(iocOrder)
	if result.Rejected {
		t.Errorf("IOC order should not be rejected: %s", result.RejectReason)
	}
	if len(result.Trades) != 1 {
		t.Errorf("expected 1 trade, got %d", len(result.Trades))
	}
	// IOC订单剩余部分应该被取消
	if result.TakerOrder.Status != types.OrderStatusPartiallyFilled {
		t.Errorf("expected status PARTIALLY_FILLED, got %s", result.TakerOrder.Status)
	}
}

func TestMatchEngine_FOK(t *testing.T) {
	eventChan := make(chan types.Event, 1000)
	engine := NewMatchEngine("BTC-USDT", eventChan)

	// 添加卖单
	sellOrder := &types.Order{
		OrderID:     "sell-1",
		UserID:      "user-1",
		Symbol:      "BTC-USDT",
		Side:        types.OrderSideSell,
		Type:        types.OrderTypeLimit,
		Price:       decimal.NewFromInt(50000),
		Quantity:    decimal.NewFromInt(1),
		FilledQty:   decimal.Zero,
		Status:      types.OrderStatusNew,
		TimeInForce: types.TimeInForceGTC,
		CreateTime:  time.Now().UnixNano(),
	}
	engine.ProcessOrder(sellOrder)

	// FOK买单，数量大于卖单 - 应该被拒绝
	fokOrder := &types.Order{
		OrderID:     "fok-1",
		UserID:      "user-2",
		Symbol:      "BTC-USDT",
		Side:        types.OrderSideBuy,
		Type:        types.OrderTypeLimit,
		Price:       decimal.NewFromInt(50000),
		Quantity:    decimal.NewFromInt(2),
		FilledQty:   decimal.Zero,
		Status:      types.OrderStatusNew,
		TimeInForce: types.TimeInForceFOK,
		CreateTime:  time.Now().UnixNano(),
	}

	result := engine.ProcessOrder(fokOrder)
	if !result.Rejected {
		t.Error("FOK order should be rejected when cannot be fully filled")
	}
	if len(result.Trades) != 0 {
		t.Error("no trades should happen for rejected FOK order")
	}
}

func TestMatchEngine_PostOnly(t *testing.T) {
	eventChan := make(chan types.Event, 1000)
	engine := NewMatchEngine("BTC-USDT", eventChan)

	// 添加卖单
	sellOrder := &types.Order{
		OrderID:     "sell-1",
		UserID:      "user-1",
		Symbol:      "BTC-USDT",
		Side:        types.OrderSideSell,
		Type:        types.OrderTypeLimit,
		Price:       decimal.NewFromInt(50000),
		Quantity:    decimal.NewFromInt(1),
		FilledQty:   decimal.Zero,
		Status:      types.OrderStatusNew,
		TimeInForce: types.TimeInForceGTC,
		CreateTime:  time.Now().UnixNano(),
	}
	engine.ProcessOrder(sellOrder)

	// Post-Only买单，价格能匹配 - 应该被拒绝
	postOnlyOrder := &types.Order{
		OrderID:     "post-only-1",
		UserID:      "user-2",
		Symbol:      "BTC-USDT",
		Side:        types.OrderSideBuy,
		Type:        types.OrderTypeLimit,
		Price:       decimal.NewFromInt(50000),
		Quantity:    decimal.NewFromInt(1),
		FilledQty:   decimal.Zero,
		Status:      types.OrderStatusNew,
		TimeInForce: types.TimeInForcePostOnly,
		CreateTime:  time.Now().UnixNano(),
	}

	result := engine.ProcessOrder(postOnlyOrder)
	if !result.Rejected {
		t.Error("Post-Only order should be rejected when would match immediately")
	}

	// Post-Only买单，价格不能匹配 - 应该挂单
	postOnlyOrder2 := &types.Order{
		OrderID:     "post-only-2",
		UserID:      "user-2",
		Symbol:      "BTC-USDT",
		Side:        types.OrderSideBuy,
		Type:        types.OrderTypeLimit,
		Price:       decimal.NewFromInt(49000),
		Quantity:    decimal.NewFromInt(1),
		FilledQty:   decimal.Zero,
		Status:      types.OrderStatusNew,
		TimeInForce: types.TimeInForcePostOnly,
		CreateTime:  time.Now().UnixNano(),
	}

	result = engine.ProcessOrder(postOnlyOrder2)
	if result.Rejected {
		t.Errorf("Post-Only order should not be rejected: %s", result.RejectReason)
	}
}

func TestMatchEngine_CancelOrder(t *testing.T) {
	eventChan := make(chan types.Event, 1000)
	engine := NewMatchEngine("BTC-USDT", eventChan)

	// 添加订单
	order := &types.Order{
		OrderID:     "order-1",
		UserID:      "user-1",
		Symbol:      "BTC-USDT",
		Side:        types.OrderSideBuy,
		Type:        types.OrderTypeLimit,
		Price:       decimal.NewFromInt(49000),
		Quantity:    decimal.NewFromInt(1),
		FilledQty:   decimal.Zero,
		Status:      types.OrderStatusNew,
		TimeInForce: types.TimeInForceGTC,
		CreateTime:  time.Now().UnixNano(),
	}
	engine.ProcessOrder(order)

	// 取消订单
	canceled, err := engine.CancelOrder("order-1")
	if err != nil {
		t.Errorf("failed to cancel order: %v", err)
	}
	if canceled.Status != types.OrderStatusCanceled {
		t.Errorf("expected status CANCELED, got %s", canceled.Status)
	}

	// 再次取消应该失败
	_, err = engine.CancelOrder("order-1")
	if err == nil {
		t.Error("canceling non-existent order should fail")
	}
}

func TestMatchEngine_PartialFill(t *testing.T) {
	eventChan := make(chan types.Event, 1000)
	engine := NewMatchEngine("BTC-USDT", eventChan)

	// 添加大卖单
	sellOrder := &types.Order{
		OrderID:     "sell-1",
		UserID:      "user-1",
		Symbol:      "BTC-USDT",
		Side:        types.OrderSideSell,
		Type:        types.OrderTypeLimit,
		Price:       decimal.NewFromInt(50000),
		Quantity:    decimal.NewFromInt(10),
		FilledQty:   decimal.Zero,
		Status:      types.OrderStatusNew,
		TimeInForce: types.TimeInForceGTC,
		CreateTime:  time.Now().UnixNano(),
	}
	engine.ProcessOrder(sellOrder)

	// 买入一部分
	buyOrder := &types.Order{
		OrderID:     "buy-1",
		UserID:      "user-2",
		Symbol:      "BTC-USDT",
		Side:        types.OrderSideBuy,
		Type:        types.OrderTypeLimit,
		Price:       decimal.NewFromInt(50000),
		Quantity:    decimal.NewFromInt(3),
		FilledQty:   decimal.Zero,
		Status:      types.OrderStatusNew,
		TimeInForce: types.TimeInForceGTC,
		CreateTime:  time.Now().UnixNano(),
	}
	result := engine.ProcessOrder(buyOrder)

	if len(result.Trades) != 1 {
		t.Errorf("expected 1 trade, got %d", len(result.Trades))
	}

	// 检查卖单剩余数量
	remainingOrder := engine.GetOrder("sell-1")
	if remainingOrder == nil {
		t.Error("sell order should still exist")
	}
	if !remainingOrder.FilledQty.Equal(decimal.NewFromInt(3)) {
		t.Errorf("expected filled qty 3, got %s", remainingOrder.FilledQty)
	}
	if remainingOrder.Status != types.OrderStatusPartiallyFilled {
		t.Errorf("expected status PARTIALLY_FILLED, got %s", remainingOrder.Status)
	}
}

func BenchmarkMatchEngine_ProcessOrder(b *testing.B) {
	eventChan := make(chan types.Event, 100000)
	engine := NewMatchEngine("BTC-USDT", eventChan)

	// 消费事件以避免阻塞
	go func() {
		for range eventChan {
		}
	}()

	// 预先添加一些订单到订单簿
	for i := 0; i < 1000; i++ {
		order := &types.Order{
			OrderID:     "sell-" + string(rune(i)),
			UserID:      "user-1",
			Symbol:      "BTC-USDT",
			Side:        types.OrderSideSell,
			Type:        types.OrderTypeLimit,
			Price:       decimal.NewFromInt(int64(50000 + i)),
			Quantity:    decimal.NewFromInt(1),
			FilledQty:   decimal.Zero,
			Status:      types.OrderStatusNew,
			TimeInForce: types.TimeInForceGTC,
			CreateTime:  time.Now().UnixNano(),
		}
		engine.ProcessOrder(order)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		order := &types.Order{
			OrderID:     "buy-" + string(rune(i)),
			UserID:      "user-2",
			Symbol:      "BTC-USDT",
			Side:        types.OrderSideBuy,
			Type:        types.OrderTypeLimit,
			Price:       decimal.NewFromInt(50500),
			Quantity:    decimal.NewFromFloat(0.1),
			FilledQty:   decimal.Zero,
			Status:      types.OrderStatusNew,
			TimeInForce: types.TimeInForceGTC,
			CreateTime:  time.Now().UnixNano(),
		}
		engine.ProcessOrder(order)
	}
}
