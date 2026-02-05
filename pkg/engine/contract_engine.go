package engine

import (
	"fmt"
	"sync"
	"time"

	"match-engine/pkg/funding"
	"match-engine/pkg/liquidation"
	"match-engine/pkg/position"
	"match-engine/pkg/types"

	"github.com/shopspring/decimal"
)

// ContractMatchEngine 合约撮合引擎
type ContractMatchEngine struct {
	*MatchEngine                       // 继承现货撮合引擎
	positionMgr    *position.Manager   // 仓位管理器
	liquidationEng *liquidation.Engine // 强平引擎
	fundingSvc     *funding.Service    // 资金费率服务
	contract       *types.Contract     // 合约配置
	tpslOrders     *TPSLOrderManager   // 止盈止损订单管理器
}

// TPSLOrderManager 止盈止损订单管理器
type TPSLOrderManager struct {
	engine       *ContractMatchEngine
	orders       map[string]*types.ContractOrder // orderID -> order
	userOrders   map[string][]string             // userID -> []orderID
	positionTPSL map[string]*PositionTPSL        // positionKey -> TPSL
	mu           sync.RWMutex
}

// PositionTPSL 仓位的止盈止损设置
type PositionTPSL struct {
	PositionKey     string // userID_symbol_side
	TakeProfitPrice decimal.Decimal
	StopLossPrice   decimal.Decimal
	TPOrderID       string
	SLOrderID       string
}

// NewTPSLOrderManager 创建止盈止损管理器
func NewTPSLOrderManager(engine *ContractMatchEngine) *TPSLOrderManager {
	return &TPSLOrderManager{
		engine:       engine,
		orders:       make(map[string]*types.ContractOrder),
		userOrders:   make(map[string][]string),
		positionTPSL: make(map[string]*PositionTPSL),
	}
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
	engine := &ContractMatchEngine{
		MatchEngine:    NewMatchEngine(symbol, eventChan),
		positionMgr:    positionMgr,
		liquidationEng: liquidationEng,
		fundingSvc:     fundingSvc,
		contract:       contract,
	}
	engine.tpslOrders = NewTPSLOrderManager(engine)
	return engine
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

	// 检查是否触发止损/止盈订单
	e.stopOrders.CheckTriggers(markPrice)

	// 检查仓位止盈止损
	e.CheckAndTriggerTPSL(markPrice)
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

// ProcessStopContractOrder 处理止损/止盈订单
func (e *ContractMatchEngine) ProcessStopContractOrder(order *types.ContractOrder) *ContractMatchResult {
	result := NewContractMatchResult(order)

	// 验证止损价格
	if order.StopPrice.IsZero() {
		result.Rejected = true
		result.RejectReason = "stop price is required"
		return result
	}

	// 添加到止损单管理器
	e.stopOrders.AddOrder(order.Order)

	order.Status = types.OrderStatusNew
	result.MatchResult = types.NewMatchResult(order.Order)

	return result
}

// GetContractConfig 获取合约配置
func (e *ContractMatchEngine) GetContractConfig() *types.Contract {
	return e.contract
}

// CalculateROE 计算收益率
func (e *ContractMatchEngine) CalculateROE(pos *types.Position) decimal.Decimal {
	if pos.Margin.IsZero() {
		return decimal.Zero
	}
	// ROE = 未实现盈亏 / 保证金 * 100%
	return pos.UnrealizedPnL.Div(pos.Margin).Mul(decimal.NewFromInt(100))
}

// CalculatePnL 计算指定价格的盈亏
func (e *ContractMatchEngine) CalculatePnL(pos *types.Position, price decimal.Decimal) decimal.Decimal {
	if pos.Quantity.IsZero() {
		return decimal.Zero
	}

	if pos.Side == types.PositionSideLong {
		return price.Sub(pos.EntryPrice).Mul(pos.Quantity)
	}
	return pos.EntryPrice.Sub(price).Mul(pos.Quantity)
}

// GetMarkPrice 获取标记价格
func (e *ContractMatchEngine) GetMarkPrice() (decimal.Decimal, bool) {
	return e.liquidationEng.GetMarkPrice(e.symbol)
}

// GetIndexPrice 获取指数价格
func (e *ContractMatchEngine) GetIndexPrice() (decimal.Decimal, bool) {
	rate, ok := e.fundingSvc.GetFundingRate(e.symbol)
	if !ok {
		return decimal.Zero, false
	}
	return rate.IndexPrice, true
}

// GetFundingRate 获取当前资金费率
func (e *ContractMatchEngine) GetFundingRate() (*types.FundingRate, bool) {
	return e.fundingSvc.GetFundingRate(e.symbol)
}

// CalculateRequiredMargin 计算开仓所需保证金
func (e *ContractMatchEngine) CalculateRequiredMargin(quantity, price decimal.Decimal, leverage int) decimal.Decimal {
	// 保证金 = 数量 * 价格 * 合约面值 / 杠杆
	positionValue := quantity.Mul(price).Mul(e.contract.ContractSize)
	return positionValue.Div(decimal.NewFromInt(int64(leverage)))
}

// CalculateFee 计算手续费
func (e *ContractMatchEngine) CalculateFee(quantity, price decimal.Decimal, isMaker bool) decimal.Decimal {
	value := quantity.Mul(price).Mul(e.contract.ContractSize)
	if isMaker {
		return value.Mul(e.contract.MakerFeeRate)
	}
	return value.Mul(e.contract.TakerFeeRate)
}

// GetPositionValue 获取仓位价值
func (e *ContractMatchEngine) GetPositionValue(pos *types.Position) decimal.Decimal {
	return pos.Quantity.Mul(pos.MarkPrice).Mul(e.contract.ContractSize)
}

// ValidatePositionSize 验证仓位大小是否超限
func (e *ContractMatchEngine) ValidatePositionSize(userID string, side types.PositionSide, addQty decimal.Decimal) error {
	posKey := fmt.Sprintf("%s_%s", e.symbol, side.String())
	pos, ok := e.positionMgr.GetPosition(userID, posKey)

	totalQty := addQty
	if ok {
		totalQty = totalQty.Add(pos.Quantity)
	}

	if totalQty.GreaterThan(e.contract.MaxPositionQty) {
		return fmt.Errorf("position size exceeds maximum: %s", e.contract.MaxPositionQty.String())
	}

	return nil
}

// GetOpenOrders 获取用户的挂单
func (e *ContractMatchEngine) GetOpenOrders(userID string) []*types.Order {
	return e.GetUserOrders(userID)
}

// CancelAllOrders 取消用户所有订单
func (e *ContractMatchEngine) CancelAllOrders(userID string) []*types.Order {
	orders := e.GetUserOrders(userID)
	canceledOrders := make([]*types.Order, 0, len(orders))

	for _, order := range orders {
		canceled, err := e.CancelOrder(order.OrderID)
		if err == nil && canceled != nil {
			canceledOrders = append(canceledOrders, canceled)
		}
	}

	return canceledOrders
}

// GetOrderBookDepth 获取合约订单簿深度
func (e *ContractMatchEngine) GetOrderBookDepth(depth int) *types.OrderBookSnapshot {
	return e.Snapshot(depth)
}

// SetPositionTPSL 设置仓位止盈止损
func (e *ContractMatchEngine) SetPositionTPSL(userID, symbol string, side types.PositionSide, tpPrice, slPrice decimal.Decimal) *PositionTPSL {
	return e.tpslOrders.SetPositionTPSL(userID, symbol, side, tpPrice, slPrice)
}

// GetPositionTPSL 获取仓位止盈止损设置
func (e *ContractMatchEngine) GetPositionTPSL(userID, symbol string, side types.PositionSide) *PositionTPSL {
	return e.tpslOrders.GetPositionTPSL(userID, symbol, side)
}

// CheckAndTriggerTPSL 检查并触发止盈止损
func (e *ContractMatchEngine) CheckAndTriggerTPSL(markPrice decimal.Decimal) {
	triggeredOrders := e.tpslOrders.CheckTPSLTriggers(markPrice)
	for _, order := range triggeredOrders {
		e.ProcessContractOrder(order)
	}
}

// Stats 返回合约引擎统计信息
func (e *ContractMatchEngine) Stats() map[string]interface{} {
	stats := make(map[string]interface{})
	stats["symbol"] = e.symbol
	stats["contract_type"] = e.contract.ContractType.String()
	stats["max_leverage"] = e.contract.MaxLeverage

	// 标记价格
	if markPrice, ok := e.liquidationEng.GetMarkPrice(e.symbol); ok {
		stats["mark_price"] = markPrice.String()
	}

	// 资金费率
	if rate, ok := e.fundingSvc.GetFundingRate(e.symbol); ok {
		stats["funding_rate"] = rate.FundingRate.String()
		stats["next_funding_time"] = rate.NextFundingTime
	}

	// 订单簿统计
	bookStats := e.OrderBook().Stats()
	stats["orderbook"] = bookStats

	return stats
}

// =============== TPSLOrderManager 方法 ===============

// AddTPSLOrder 添加止盈止损订单
func (m *TPSLOrderManager) AddTPSLOrder(order *types.ContractOrder) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.orders[order.OrderID] = order
	m.userOrders[order.UserID] = append(m.userOrders[order.UserID], order.OrderID)
}

// RemoveOrder 移除订单
func (m *TPSLOrderManager) RemoveOrder(orderID string) *types.ContractOrder {
	m.mu.Lock()
	defer m.mu.Unlock()

	order, ok := m.orders[orderID]
	if !ok {
		return nil
	}

	delete(m.orders, orderID)

	// 从用户订单列表中移除
	if userOrders, ok := m.userOrders[order.UserID]; ok {
		for i, id := range userOrders {
			if id == orderID {
				m.userOrders[order.UserID] = append(userOrders[:i], userOrders[i+1:]...)
				break
			}
		}
	}

	return order
}

// SetPositionTPSL 设置仓位止盈止损
func (m *TPSLOrderManager) SetPositionTPSL(userID, symbol string, side types.PositionSide, tpPrice, slPrice decimal.Decimal) *PositionTPSL {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := fmt.Sprintf("%s_%s_%s", userID, symbol, side.String())

	tpsl := &PositionTPSL{
		PositionKey:     key,
		TakeProfitPrice: tpPrice,
		StopLossPrice:   slPrice,
	}

	m.positionTPSL[key] = tpsl
	return tpsl
}

// GetPositionTPSL 获取仓位止盈止损设置
func (m *TPSLOrderManager) GetPositionTPSL(userID, symbol string, side types.PositionSide) *PositionTPSL {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := fmt.Sprintf("%s_%s_%s", userID, symbol, side.String())
	return m.positionTPSL[key]
}

// CheckTPSLTriggers 检查止盈止损触发
func (m *TPSLOrderManager) CheckTPSLTriggers(markPrice decimal.Decimal) []*types.ContractOrder {
	m.mu.Lock()
	defer m.mu.Unlock()

	var triggeredOrders []*types.ContractOrder

	for key, tpsl := range m.positionTPSL {
		// 解析 key: userID_symbol_side
		var triggered bool
		var triggerType string

		// 获取仓位信息
		pos, ok := m.engine.positionMgr.GetPosition(extractUserID(key), extractPositionKey(key))
		if !ok || pos.Quantity.IsZero() {
			// 仓位已清空，删除止盈止损
			delete(m.positionTPSL, key)
			continue
		}

		if pos.Side == types.PositionSideLong {
			// 多头: 止盈 - 价格 >= 止盈价, 止损 - 价格 <= 止损价
			if !tpsl.TakeProfitPrice.IsZero() && markPrice.GreaterThanOrEqual(tpsl.TakeProfitPrice) {
				triggered = true
				triggerType = "take_profit"
			} else if !tpsl.StopLossPrice.IsZero() && markPrice.LessThanOrEqual(tpsl.StopLossPrice) {
				triggered = true
				triggerType = "stop_loss"
			}
		} else {
			// 空头: 止盈 - 价格 <= 止盈价, 止损 - 价格 >= 止损价
			if !tpsl.TakeProfitPrice.IsZero() && markPrice.LessThanOrEqual(tpsl.TakeProfitPrice) {
				triggered = true
				triggerType = "take_profit"
			} else if !tpsl.StopLossPrice.IsZero() && markPrice.GreaterThanOrEqual(tpsl.StopLossPrice) {
				triggered = true
				triggerType = "stop_loss"
			}
		}

		if triggered {
			// 创建平仓订单
			closeOrder := &types.ContractOrder{
				Order: &types.Order{
					OrderID:     fmt.Sprintf("tpsl_%d", time.Now().UnixNano()),
					UserID:      pos.UserID,
					Symbol:      pos.Symbol,
					Side:        m.getCloseSide(pos.Side),
					Type:        types.OrderTypeMarket,
					Quantity:    pos.Quantity,
					TimeInForce: types.TimeInForceIOC,
					CreateTime:  time.Now().UnixNano(),
				},
				PositionSide:  pos.Side,
				Leverage:      pos.Leverage,
				MarginType:    pos.MarginType,
				ReduceOnly:    true,
				ClosePosition: true,
			}

			triggeredOrders = append(triggeredOrders, closeOrder)

			// 删除已触发的止盈止损
			delete(m.positionTPSL, key)

			// 可以在这里记录触发原因
			_ = triggerType
		}
	}

	return triggeredOrders
}

// getCloseSide 获取平仓方向
func (m *TPSLOrderManager) getCloseSide(positionSide types.PositionSide) types.OrderSide {
	if positionSide == types.PositionSideLong {
		return types.OrderSideSell
	}
	return types.OrderSideBuy
}

// extractUserID 从 key 中提取 userID
func extractUserID(key string) string {
	// key format: userID_symbol_side
	for i := 0; i < len(key); i++ {
		if key[i] == '_' {
			return key[:i]
		}
	}
	return key
}

// extractPositionKey 从 key 中提取 position key (symbol_side)
func extractPositionKey(key string) string {
	// key format: userID_symbol_side
	firstUnderscore := -1
	for i := 0; i < len(key); i++ {
		if key[i] == '_' {
			if firstUnderscore == -1 {
				firstUnderscore = i
			} else {
				return key[firstUnderscore+1:]
			}
		}
	}
	return ""
}

// ClearUserTPSL 清除用户所有止盈止损
func (m *TPSLOrderManager) ClearUserTPSL(userID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for key := range m.positionTPSL {
		if extractUserID(key) == userID {
			delete(m.positionTPSL, key)
		}
	}
}
