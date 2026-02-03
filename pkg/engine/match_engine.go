package engine

import (
	"fmt"
	"sync/atomic"
	"time"

	"match-engine/pkg/orderbook"
	"match-engine/pkg/types"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// MatchEngine 撮合引擎
type MatchEngine struct {
	symbol     string
	orderBook  *orderbook.OrderBook
	sequenceID int64
	tradeID    int64
	eventChan  chan types.Event
	isPaused   int32 // 原子操作
	stopOrders *StopOrderManager
}

// NewMatchEngine 创建撮合引擎
func NewMatchEngine(symbol string, eventChan chan types.Event) *MatchEngine {
	engine := &MatchEngine{
		symbol:     symbol,
		orderBook:  orderbook.NewOrderBook(symbol),
		sequenceID: 0,
		tradeID:    0,
		eventChan:  eventChan,
	}
	engine.stopOrders = NewStopOrderManager(engine)
	return engine
}

// Symbol 返回交易对
func (e *MatchEngine) Symbol() string {
	return e.symbol
}

// OrderBook 返回订单簿
func (e *MatchEngine) OrderBook() *orderbook.OrderBook {
	return e.orderBook
}

// IsPaused 检查是否暂停
func (e *MatchEngine) IsPaused() bool {
	return atomic.LoadInt32(&e.isPaused) == 1
}

// Pause 暂停撮合
func (e *MatchEngine) Pause() {
	atomic.StoreInt32(&e.isPaused, 1)
}

// Resume 恢复撮合
func (e *MatchEngine) Resume() {
	atomic.StoreInt32(&e.isPaused, 0)
}

// nextSequenceID 生成下一个序列号
func (e *MatchEngine) nextSequenceID() int64 {
	return atomic.AddInt64(&e.sequenceID, 1)
}

// nextTradeID 生成下一个成交ID
func (e *MatchEngine) nextTradeID() string {
	id := atomic.AddInt64(&e.tradeID, 1)
	return fmt.Sprintf("%s-%d", e.symbol, id)
}

// ProcessOrder 处理订单
func (e *MatchEngine) ProcessOrder(order *types.Order) *types.MatchResult {
	if e.IsPaused() {
		result := types.NewMatchResult(order)
		result.Rejected = true
		result.RejectReason = "engine is paused"
		order.Status = types.OrderStatusRejected
		return result
	}

	order.SequenceID = e.nextSequenceID()

	// 处理止损/止盈单
	if order.Type == types.OrderTypeStopLoss || order.Type == types.OrderTypeTakeProfit {
		return e.processStopOrder(order)
	}

	// 处理不同订单类型
	switch order.Type {
	case types.OrderTypeMarket:
		return e.processMarketOrder(order)
	case types.OrderTypeLimit:
		return e.processLimitOrder(order)
	case types.OrderTypeIceberg:
		return e.processIcebergOrder(order)
	default:
		result := types.NewMatchResult(order)
		result.Rejected = true
		result.RejectReason = "unsupported order type"
		order.Status = types.OrderStatusRejected
		return result
	}
}

// processMarketOrder 处理市价单
func (e *MatchEngine) processMarketOrder(order *types.Order) *types.MatchResult {
	result := types.NewMatchResult(order)

	// 获取对手盘
	oppositeTree := e.orderBook.GetOppositeTree(order.Side)

	remainingQty := order.Quantity

	// 持续匹配直到数量用完或对手盘为空
	for remainingQty.IsPositive() && oppositeTree.Len() > 0 {
		bestLevel := oppositeTree.Best()
		if bestLevel == nil {
			break
		}

		// 匹配该价格档位的订单
		for remainingQty.IsPositive() && !bestLevel.IsEmpty() {
			makerOrder := bestLevel.Front()
			trade, filled := e.matchOrders(order, makerOrder, &remainingQty)
			if trade != nil {
				result.AddTrade(trade, makerOrder)
			}
			if filled {
				e.orderBook.RemoveOrder(makerOrder.OrderID)
			}
		}

		// 如果档位为空，移除
		if bestLevel.IsEmpty() {
			oppositeTree.Remove(bestLevel.Price)
		}
	}

	// 更新Taker订单状态
	order.FilledQty = order.Quantity.Sub(remainingQty)
	if order.FilledQty.IsZero() {
		order.Status = types.OrderStatusNew
	} else if remainingQty.IsZero() {
		order.Status = types.OrderStatusFilled
	} else {
		order.Status = types.OrderStatusPartiallyFilled
	}

	// 市价单不挂单，剩余部分取消
	if remainingQty.IsPositive() {
		order.Status = types.OrderStatusCanceled
	}

	// 检查并触发止损单
	if result.HasTrades() {
		lastTrade := result.Trades[len(result.Trades)-1]
		e.orderBook.SetLastPrice(lastTrade.Price)
		e.stopOrders.CheckTriggers(lastTrade.Price)
	}

	return result
}

// processLimitOrder 处理限价单
func (e *MatchEngine) processLimitOrder(order *types.Order) *types.MatchResult {
	result := types.NewMatchResult(order)

	// 检查Post-Only
	if order.TimeInForce == types.TimeInForcePostOnly {
		if e.wouldMatch(order) {
			result.Rejected = true
			result.RejectReason = "post-only order would match immediately"
			order.Status = types.OrderStatusRejected
			return result
		}
	}

	// 获取对手盘
	oppositeTree := e.orderBook.GetOppositeTree(order.Side)

	remainingQty := order.Quantity

	// 持续匹配
	for remainingQty.IsPositive() && oppositeTree.Len() > 0 {
		bestLevel := oppositeTree.Best()
		if bestLevel == nil {
			break
		}

		// 检查价格是否可以匹配
		if !e.canMatch(order.Side, order.Price, bestLevel.Price) {
			break
		}

		// 匹配该价格档位的订单
		for remainingQty.IsPositive() && !bestLevel.IsEmpty() {
			makerOrder := bestLevel.Front()
			trade, filled := e.matchOrders(order, makerOrder, &remainingQty)
			if trade != nil {
				result.AddTrade(trade, makerOrder)
			}
			if filled {
				e.orderBook.RemoveOrder(makerOrder.OrderID)
			}
		}

		// 如果档位为空，移除
		if bestLevel.IsEmpty() {
			oppositeTree.Remove(bestLevel.Price)
		}
	}

	// 更新Taker订单状态
	order.FilledQty = order.Quantity.Sub(remainingQty)
	order.UpdateTime = time.Now().UnixNano()

	// 根据TimeInForce处理剩余数量
	switch order.TimeInForce {
	case types.TimeInForceFOK:
		if remainingQty.IsPositive() {
			// FOK: 未能全部成交，取消
			result.Rejected = true
			result.RejectReason = "FOK order cannot be fully filled"
			order.Status = types.OrderStatusCanceled
			result.Trades = nil // 清空成交
			return result
		}
		order.Status = types.OrderStatusFilled

	case types.TimeInForceIOC:
		if remainingQty.IsPositive() {
			// IOC: 剩余部分取消
			order.Status = types.OrderStatusCanceled
			if order.FilledQty.IsPositive() {
				order.Status = types.OrderStatusPartiallyFilled
			}
		} else {
			order.Status = types.OrderStatusFilled
		}

	default: // GTC
		if remainingQty.IsZero() {
			order.Status = types.OrderStatusFilled
		} else if order.FilledQty.IsPositive() {
			order.Status = types.OrderStatusPartiallyFilled
			// 将剩余订单加入订单簿
			result.RemainingOrder = order.Clone()
			result.RemainingOrder.Quantity = remainingQty
			result.RemainingOrder.FilledQty = decimal.Zero
			e.orderBook.AddOrder(result.RemainingOrder)
		} else {
			order.Status = types.OrderStatusNew
			// 未成交，加入订单簿
			e.orderBook.AddOrder(order)
		}
	}

	// 检查并触发止损单
	if result.HasTrades() {
		lastTrade := result.Trades[len(result.Trades)-1]
		e.orderBook.SetLastPrice(lastTrade.Price)
		e.stopOrders.CheckTriggers(lastTrade.Price)
	}

	return result
}

// processIcebergOrder 处理冰山单
func (e *MatchEngine) processIcebergOrder(order *types.Order) *types.MatchResult {
	// 冰山单：只显示部分数量
	visibleQty := order.VisibleQty
	if visibleQty.IsZero() {
		visibleQty = order.Quantity.Div(decimal.NewFromInt(10)) // 默认显示10%
	}

	// 创建可见订单
	visibleOrder := order.Clone()
	visibleOrder.OrderID = order.OrderID + "-visible"
	if visibleQty.GreaterThan(order.RemainingQty()) {
		visibleQty = order.RemainingQty()
	}
	visibleOrder.Quantity = visibleQty
	visibleOrder.Type = types.OrderTypeLimit

	result := e.processLimitOrder(visibleOrder)

	// 如果可见部分全部成交，且还有剩余数量，创建新的可见订单
	// 这里简化处理，实际实现需要更复杂的逻辑
	order.FilledQty = order.FilledQty.Add(visibleOrder.FilledQty)
	order.Status = visibleOrder.Status

	result.TakerOrder = order
	return result
}

// processStopOrder 处理止损/止盈单
func (e *MatchEngine) processStopOrder(order *types.Order) *types.MatchResult {
	result := types.NewMatchResult(order)

	// 添加到止损单管理器
	e.stopOrders.AddOrder(order)
	order.Status = types.OrderStatusNew

	return result
}

// matchOrders 匹配两个订单
func (e *MatchEngine) matchOrders(taker, maker *types.Order, remainingQty *decimal.Decimal) (*types.Trade, bool) {
	// 计算成交数量
	makerRemaining := maker.RemainingQty()
	matchQty := *remainingQty
	if matchQty.GreaterThan(makerRemaining) {
		matchQty = makerRemaining
	}

	if matchQty.IsZero() {
		return nil, false
	}

	// 成交价格以Maker价格为准
	tradePrice := maker.Price

	// 创建成交记录
	trade := &types.Trade{
		TradeID:      e.nextTradeID(),
		Symbol:       e.symbol,
		MakerOrderID: maker.OrderID,
		TakerOrderID: taker.OrderID,
		MakerUserID:  maker.UserID,
		TakerUserID:  taker.UserID,
		Price:        tradePrice,
		Quantity:     matchQty,
		MakerSide:    maker.Side,
		TakerSide:    taker.Side,
		TradeTime:    time.Now().UnixNano(),
		SequenceID:   e.nextSequenceID(),
	}

	// 更新数量
	*remainingQty = remainingQty.Sub(matchQty)
	maker.UpdateTime = time.Now().UnixNano()

	// 更新Maker状态和订单簿
	makerFilled := false
	newFilledQty := maker.FilledQty.Add(matchQty)
	if newFilledQty.GreaterThanOrEqual(maker.Quantity) {
		maker.FilledQty = newFilledQty
		maker.Status = types.OrderStatusFilled
		makerFilled = true
	} else {
		maker.FilledQty = newFilledQty
		maker.Status = types.OrderStatusPartiallyFilled
		// 更新订单簿中档位的数量（不更新订单的FilledQty，因为已经更新过了）
		tree := e.orderBook.GetOppositeTree(taker.Side)
		level := tree.Get(maker.Price)
		if level != nil {
			level.UpdateVolume(matchQty.Neg())
		}
	}

	// 发送成交事件
	if e.eventChan != nil {
		e.eventChan <- &types.TradeEvent{
			BaseEvent: types.BaseEvent{
				Type:       types.EventTypeTrade,
				Symbol:     e.symbol,
				Timestamp:  trade.TradeTime,
				SequenceID: trade.SequenceID,
			},
			Trade: trade,
		}
	}

	return trade, makerFilled
}

// canMatch 检查是否可以匹配
func (e *MatchEngine) canMatch(side types.OrderSide, orderPrice, levelPrice decimal.Decimal) bool {
	if side == types.OrderSideBuy {
		// 买单：订单价格 >= 卖盘价格
		return orderPrice.GreaterThanOrEqual(levelPrice)
	}
	// 卖单：订单价格 <= 买盘价格
	return orderPrice.LessThanOrEqual(levelPrice)
}

// wouldMatch 检查订单是否会立即匹配（用于Post-Only检查）
func (e *MatchEngine) wouldMatch(order *types.Order) bool {
	oppositeTree := e.orderBook.GetOppositeTree(order.Side)
	if oppositeTree.Len() == 0 {
		return false
	}
	bestLevel := oppositeTree.Best()
	if bestLevel == nil {
		return false
	}
	return e.canMatch(order.Side, order.Price, bestLevel.Price)
}

// CancelOrder 取消订单
func (e *MatchEngine) CancelOrder(orderID string) (*types.Order, error) {
	order := e.orderBook.RemoveOrder(orderID)
	if order == nil {
		// 检查是否是止损单
		order = e.stopOrders.RemoveOrder(orderID)
		if order == nil {
			return nil, fmt.Errorf("order not found: %s", orderID)
		}
	}

	order.Status = types.OrderStatusCanceled
	order.UpdateTime = time.Now().UnixNano()

	// 发送取消事件
	if e.eventChan != nil {
		e.eventChan <- &types.OrderEvent{
			BaseEvent: types.BaseEvent{
				Type:       types.EventTypeOrderCancel,
				Symbol:     e.symbol,
				Timestamp:  order.UpdateTime,
				SequenceID: e.nextSequenceID(),
			},
			Order: order,
		}
	}

	return order, nil
}

// CancelAllOrders 取消用户所有订单
func (e *MatchEngine) CancelAllOrders(userID string) []*types.Order {
	canceledOrders := make([]*types.Order, 0)

	// 获取用户所有订单
	orders := e.orderBook.GetUserOrders(userID)
	for _, order := range orders {
		canceled, err := e.CancelOrder(order.OrderID)
		if err == nil {
			canceledOrders = append(canceledOrders, canceled)
		}
	}

	// 取消止损单
	stopOrders := e.stopOrders.GetUserOrders(userID)
	for _, order := range stopOrders {
		canceled := e.stopOrders.RemoveOrder(order.OrderID)
		if canceled != nil {
			canceled.Status = types.OrderStatusCanceled
			canceledOrders = append(canceledOrders, canceled)
		}
	}

	return canceledOrders
}

// GetOrder 获取订单
func (e *MatchEngine) GetOrder(orderID string) *types.Order {
	order := e.orderBook.GetOrder(orderID)
	if order != nil {
		return order
	}
	return e.stopOrders.GetOrder(orderID)
}

// GetUserOrders 获取用户订单
func (e *MatchEngine) GetUserOrders(userID string) []*types.Order {
	orders := e.orderBook.GetUserOrders(userID)
	stopOrders := e.stopOrders.GetUserOrders(userID)
	return append(orders, stopOrders...)
}

// Snapshot 生成订单簿快照
func (e *MatchEngine) Snapshot(depth int) *types.OrderBookSnapshot {
	return e.orderBook.Snapshot(depth)
}

// StopOrderManager 止损单管理器
type StopOrderManager struct {
	engine     *MatchEngine
	buyStops   map[string]*types.Order            // 止损买单：当价格上涨时触发
	sellStops  map[string]*types.Order            // 止损卖单：当价格下跌时触发
	orderIndex map[string]*types.Order            // 订单索引
	userOrders map[string]map[string]*types.Order // 用户订单索引
}

// NewStopOrderManager 创建止损单管理器
func NewStopOrderManager(engine *MatchEngine) *StopOrderManager {
	return &StopOrderManager{
		engine:     engine,
		buyStops:   make(map[string]*types.Order),
		sellStops:  make(map[string]*types.Order),
		orderIndex: make(map[string]*types.Order),
		userOrders: make(map[string]map[string]*types.Order),
	}
}

// AddOrder 添加止损单
func (m *StopOrderManager) AddOrder(order *types.Order) {
	m.orderIndex[order.OrderID] = order

	if m.userOrders[order.UserID] == nil {
		m.userOrders[order.UserID] = make(map[string]*types.Order)
	}
	m.userOrders[order.UserID][order.OrderID] = order

	// 根据类型和方向确定触发条件
	if order.Type == types.OrderTypeStopLoss {
		if order.Side == types.OrderSideBuy {
			m.buyStops[order.OrderID] = order
		} else {
			m.sellStops[order.OrderID] = order
		}
	} else if order.Type == types.OrderTypeTakeProfit {
		if order.Side == types.OrderSideSell {
			m.buyStops[order.OrderID] = order // 价格上涨时触发卖出
		} else {
			m.sellStops[order.OrderID] = order // 价格下跌时触发买入
		}
	}
}

// RemoveOrder 移除止损单
func (m *StopOrderManager) RemoveOrder(orderID string) *types.Order {
	order, ok := m.orderIndex[orderID]
	if !ok {
		return nil
	}

	delete(m.orderIndex, orderID)
	delete(m.buyStops, orderID)
	delete(m.sellStops, orderID)

	if userOrders, ok := m.userOrders[order.UserID]; ok {
		delete(userOrders, orderID)
	}

	return order
}

// GetOrder 获取订单
func (m *StopOrderManager) GetOrder(orderID string) *types.Order {
	return m.orderIndex[orderID]
}

// GetUserOrders 获取用户订单
func (m *StopOrderManager) GetUserOrders(userID string) []*types.Order {
	orders := make([]*types.Order, 0)
	if userOrders, ok := m.userOrders[userID]; ok {
		for _, order := range userOrders {
			orders = append(orders, order)
		}
	}
	return orders
}

// CheckTriggers 检查是否需要触发止损单
func (m *StopOrderManager) CheckTriggers(currentPrice decimal.Decimal) {
	// 检查买入止损（价格上涨触发）
	for _, order := range m.buyStops {
		if currentPrice.GreaterThanOrEqual(order.StopPrice) {
			m.triggerOrder(order)
		}
	}

	// 检查卖出止损（价格下跌触发）
	for _, order := range m.sellStops {
		if currentPrice.LessThanOrEqual(order.StopPrice) {
			m.triggerOrder(order)
		}
	}
}

// triggerOrder 触发止损单
func (m *StopOrderManager) triggerOrder(order *types.Order) {
	m.RemoveOrder(order.OrderID)

	// 创建新的市价单或限价单
	newOrder := &types.Order{
		OrderID:     uuid.New().String(),
		UserID:      order.UserID,
		Symbol:      order.Symbol,
		Side:        order.Side,
		Type:        types.OrderTypeMarket, // 触发后转为市价单
		Price:       order.Price,
		Quantity:    order.Quantity,
		FilledQty:   decimal.Zero,
		Status:      types.OrderStatusNew,
		TimeInForce: types.TimeInForceIOC,
		CreateTime:  time.Now().UnixNano(),
		UpdateTime:  time.Now().UnixNano(),
	}

	// 如果有指定价格，转为限价单
	if order.Price.IsPositive() {
		newOrder.Type = types.OrderTypeLimit
		newOrder.TimeInForce = types.TimeInForceGTC
	}

	// 处理订单
	m.engine.ProcessOrder(newOrder)
}
