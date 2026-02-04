package engine

import (
	"fmt"
	"time"

	"github.com/shopspring/decimal"
	"match-engine/pkg/funding"
	"match-engine/pkg/liquidation"
	"match-engine/pkg/position"
	"match-engine/pkg/types"
)

// ContractMatchEngine 合约撮合引擎
type ContractMatchEngine struct {
	*MatchEngine                       // 继承现货撮合引擎
	positionMgr    *position.Manager   // 仓位管理器
	liquidationEng *liquidation.Engine // 强平引擎
	fundingSvc     *funding.Service    // 资金费率服务
	contract       *types.Contract     // 合约配置
}

// NewContractMatchEngine 创建合约撮合引擎
func NewContractMatchEngine(
	symbol string,
	eventChan chan types.Event,
	positionMgr *position.Manager,
	liquidationEng *liquidation.Engine,
	fundingSvc *funding.Service,
	contract *types.Contract,
) *ContractMatchEngine {
	return &ContractMatchEngine{
		MatchEngine:    NewMatchEngine(symbol, eventChan),
		positionMgr:    positionMgr,
		liquidationEng: liquidationEng,
		fundingSvc:     fundingSvc,
		contract:       contract,
	}
}

// ProcessContractOrder 处理合约订单
func (e *ContractMatchEngine) ProcessContractOrder(order *types.ContractOrder) *ContractMatchResult {
	if e.IsPaused() {
		result := NewContractMatchResult(order)
		result.Rejected = true
		result.RejectReason = "engine is paused"
		order.Status = types.OrderStatusRejected
		return result
	}

	order.SequenceID = e.nextSequenceID()

	// 1. 验证订单
	if err := e.validateContractOrder(order); err != nil {
		result := NewContractMatchResult(order)
		result.Rejected = true
		result.RejectReason = err.Error()
		order.Status = types.OrderStatusRejected
		return result
	}

	// 2. 检查保证金 (简化实现)
	if !order.ReduceOnly {
		if err := e.checkMargin(order); err != nil {
			result := NewContractMatchResult(order)
			result.Rejected = true
			result.RejectReason = err.Error()
			order.Status = types.OrderStatusRejected
			return result
		}
	}

	// 3. 处理减仓单
	if order.ReduceOnly {
		return e.processReduceOnlyOrder(order)
	}

	// 4. 处理平仓单
	if order.ClosePosition {
		return e.processClosePositionOrder(order)
	}

	// 5. 普通合约订单撮合
	return e.processContractOrder(order)
}

// validateContractOrder 验证合约订单
func (e *ContractMatchEngine) validateContractOrder(order *types.ContractOrder) error {
	// 检查杠杆
	if order.Leverage <= 0 || order.Leverage > e.contract.MaxLeverage {
		return fmt.Errorf("invalid leverage: %d, max: %d", order.Leverage, e.contract.MaxLeverage)
	}

	// 检查数量
	if order.Quantity.LessThan(e.contract.LotSize) {
		return fmt.Errorf("quantity below minimum: %s", e.contract.LotSize.String())
	}

	if order.Quantity.GreaterThan(e.contract.MaxOrderQty) {
		return fmt.Errorf("quantity exceeds maximum: %s", e.contract.MaxOrderQty.String())
	}

	// 检查价格精度
	if order.Type == types.OrderTypeLimit {
		if order.Price.LessThanOrEqual(decimal.Zero) {
			return fmt.Errorf("invalid price")
		}
	}

	return nil
}

// checkMargin 检查保证金
func (e *ContractMatchEngine) checkMargin(order *types.ContractOrder) error {
	// 计算所需保证金
	// 保证金 = 数量 * 价格 / 杠杆
	var price decimal.Decimal
	if order.Type == types.OrderTypeMarket {
		// 使用标记价格或最优价格
		markPrice, ok := e.liquidationEng.GetMarkPrice(e.symbol)
		if ok {
			price = markPrice
		} else {
			return fmt.Errorf("mark price not available")
		}
	} else {
		price = order.Price
	}

	requiredMargin := order.Quantity.Mul(price).Div(decimal.NewFromInt(int64(order.Leverage)))

	// TODO: 检查用户可用保证金
	// 这里需要与账户系统集成
	_ = requiredMargin

	return nil
}

// processReduceOnlyOrder 处理减仓单
func (e *ContractMatchEngine) processReduceOnlyOrder(order *types.ContractOrder) *ContractMatchResult {
	result := NewContractMatchResult(order)

	// 获取当前仓位
	positionSide := e.getOppositePositionSide(order)
	pos, ok := e.positionMgr.GetPosition(order.UserID, fmt.Sprintf("%s_%s", e.symbol, positionSide.String()))

	if !ok || pos.Quantity.IsZero() {
		result.Rejected = true
		result.RejectReason = "no position to reduce"
		order.Status = types.OrderStatusRejected
		return result
	}

	// 限制减仓数量不超过持仓
	if order.Quantity.GreaterThan(pos.Quantity) {
		order.Quantity = pos.Quantity
	}

	// 撮合
	matchResult := e.MatchEngine.ProcessOrder(order.Order)
	result.MatchResult = matchResult

	// 处理成交后的仓位更新
	if matchResult.TotalFilledQty().IsPositive() {
		e.updatePositionAfterTrade(order, matchResult, true)
	}

	return result
}

// processClosePositionOrder 处理平仓单
func (e *ContractMatchEngine) processClosePositionOrder(order *types.ContractOrder) *ContractMatchResult {
	result := NewContractMatchResult(order)

	// 获取当前仓位
	positionSide := e.getOppositePositionSide(order)
	pos, ok := e.positionMgr.GetPosition(order.UserID, fmt.Sprintf("%s_%s", e.symbol, positionSide.String()))

	if !ok || pos.Quantity.IsZero() {
		result.Rejected = true
		result.RejectReason = "no position to close"
		order.Status = types.OrderStatusRejected
		return result
	}

	// 设置数量为全部持仓
	order.Quantity = pos.Quantity

	// 撮合
	matchResult := e.MatchEngine.ProcessOrder(order.Order)
	result.MatchResult = matchResult

	// 处理成交后的仓位更新
	if matchResult.TotalFilledQty().IsPositive() {
		e.updatePositionAfterTrade(order, matchResult, true)
	}

	return result
}

// processContractOrder 处理普通合约订单
func (e *ContractMatchEngine) processContractOrder(order *types.ContractOrder) *ContractMatchResult {
	result := NewContractMatchResult(order)

	// 撮合
	matchResult := e.MatchEngine.ProcessOrder(order.Order)
	result.MatchResult = matchResult

	// 处理成交后的仓位更新
	if matchResult.TotalFilledQty().IsPositive() {
		e.updatePositionAfterTrade(order, matchResult, false)
	}

	return result
}

// updatePositionAfterTrade 成交后更新仓位
func (e *ContractMatchEngine) updatePositionAfterTrade(order *types.ContractOrder, matchResult *types.MatchResult, isReduce bool) {
	filledQty := matchResult.TotalFilledQty()
	if filledQty.IsZero() {
		return
	}

	// 计算成交均价
	avgPrice := decimal.Zero
	if len(matchResult.Trades) > 0 {
		totalValue := decimal.Zero
		totalQty := decimal.Zero
		for _, trade := range matchResult.Trades {
			totalValue = totalValue.Add(trade.Price.Mul(trade.Quantity))
			totalQty = totalQty.Add(trade.Quantity)
		}
		if !totalQty.IsZero() {
			avgPrice = totalValue.Div(totalQty)
		}
	}

	if isReduce {
		// 减仓
		positionSide := e.getOppositePositionSide(order)
		pos, pnl, err := e.positionMgr.ReducePosition(
			order.UserID, e.symbol, positionSide, avgPrice, filledQty,
		)
		if err == nil {
			e.emitPositionEvent(pos, pnl)
		}
	} else {
		// 开仓/加仓
		positionSide := e.getPositionSide(order)
		pos, err := e.positionMgr.UpdatePosition(
			order.UserID, e.symbol, positionSide,
			avgPrice, filledQty,
			order.MarginType, order.Leverage,
		)
		if err == nil {
			e.emitPositionEvent(pos, decimal.Zero)
		}
	}
}

// getPositionSide 根据订单获取仓位方向
func (e *ContractMatchEngine) getPositionSide(order *types.ContractOrder) types.PositionSide {
	if order.PositionSide != types.PositionSideBoth {
		return order.PositionSide
	}

	// 单向持仓模式
	if order.Side == types.OrderSideBuy {
		return types.PositionSideLong
	}
	return types.PositionSideShort
}

// getOppositePositionSide 获取相反的仓位方向(用于平仓)
func (e *ContractMatchEngine) getOppositePositionSide(order *types.ContractOrder) types.PositionSide {
	if order.PositionSide != types.PositionSideBoth {
		return order.PositionSide
	}

	// 单向持仓模式: 买入平空, 卖出平多
	if order.Side == types.OrderSideBuy {
		return types.PositionSideShort
	}
	return types.PositionSideLong
}

// emitPositionEvent 发送仓位变更事件
func (e *ContractMatchEngine) emitPositionEvent(pos *types.Position, realizedPnL decimal.Decimal) {
	event := &types.PositionEvent{
		BaseEvent: types.BaseEvent{
			Type:      types.EventTypePosition,
			Symbol:    e.symbol,
			Timestamp: time.Now().UnixNano(),
		},
		Position:    pos,
		RealizedPnL: realizedPnL,
	}

	select {
	case e.eventChan <- event:
	default:
	}
}

// ProcessLiquidationOrder 处理强平订单
func (e *ContractMatchEngine) ProcessLiquidationOrder(order *types.LiquidationOrder) *LiquidationResult {
	result := &LiquidationResult{
		LiquidationOrder: order,
		Success:          false,
	}

	// 创建市价单进行平仓
	closeOrder := &types.Order{
		OrderID:     order.LiquidationID,
		UserID:      order.UserID,
		Symbol:      order.Symbol,
		Side:        order.Side,
		Type:        types.OrderTypeMarket,
		Quantity:    order.Quantity,
		TimeInForce: types.TimeInForceIOC,
		CreateTime:  time.Now().UnixNano(),
	}

	// 撮合
	matchResult := e.MatchEngine.ProcessOrder(closeOrder)

	filledQty := matchResult.TotalFilledQty()
	if filledQty.IsPositive() {
		result.Success = true
		result.FilledQty = filledQty
		result.AvgPrice = e.calculateAvgPrice(matchResult)
		result.Trades = matchResult.Trades

		// 更新仓位
		var positionSide types.PositionSide
		if order.Side == types.OrderSideSell {
			positionSide = types.PositionSideLong
		} else {
			positionSide = types.PositionSideShort
		}

		_, pnl, _ := e.positionMgr.ReducePosition(
			order.UserID, e.symbol, positionSide, result.AvgPrice, result.FilledQty,
		)
		result.RealizedPnL = pnl

		// 通知强平引擎处理结果
		e.liquidationEng.ProcessLiquidationResult(order, result.FilledQty, result.AvgPrice, true)
	} else {
		// 强平失败，触发 ADL
		e.liquidationEng.ProcessLiquidationResult(order, decimal.Zero, decimal.Zero, false)
	}

	return result
}

// calculateAvgPrice 计算成交均价
func (e *ContractMatchEngine) calculateAvgPrice(result *types.MatchResult) decimal.Decimal {
	if len(result.Trades) == 0 {
		return decimal.Zero
	}

	totalValue := decimal.Zero
	totalQty := decimal.Zero
	for _, trade := range result.Trades {
		totalValue = totalValue.Add(trade.Price.Mul(trade.Quantity))
		totalQty = totalQty.Add(trade.Quantity)
	}

	if totalQty.IsZero() {
		return decimal.Zero
	}
	return totalValue.Div(totalQty)
}

// UpdateMarkPrice 更新标记价格
func (e *ContractMatchEngine) UpdateMarkPrice(markPrice decimal.Decimal) {
	e.liquidationEng.UpdateMarkPrice(e.symbol, markPrice)

	// 同时更新资金费率服务
	indexPrice := markPrice // 简化: 使用标记价作为指数价
	e.fundingSvc.UpdatePrices(e.symbol, markPrice, indexPrice)

	// 检查是否触发止损/止盈
	e.stopOrders.CheckTriggers(markPrice)
}

// ContractMatchResult 合约撮合结果
type ContractMatchResult struct {
	Order        *types.ContractOrder
	MatchResult  *types.MatchResult
	Rejected     bool
	RejectReason string
}

// NewContractMatchResult 创建合约撮合结果
func NewContractMatchResult(order *types.ContractOrder) *ContractMatchResult {
	return &ContractMatchResult{
		Order: order,
	}
}

// PositionUpdate 仓位更新事件
type PositionUpdate struct {
	Position    *types.Position
	RealizedPnL decimal.Decimal
}

// LiquidationResult 强平结果
type LiquidationResult struct {
	LiquidationOrder *types.LiquidationOrder
	Success          bool
	FilledQty        decimal.Decimal
	AvgPrice         decimal.Decimal
	RealizedPnL      decimal.Decimal
	Trades           []*types.Trade
}
