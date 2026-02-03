package orderbook

import (
	"container/list"
	"sync"
	"time"

	"match-engine/pkg/types"

	"github.com/shopspring/decimal"
)

// PriceLevel 价格档位
type PriceLevel struct {
	Price    decimal.Decimal
	Orders   *list.List               // 订单队列 (FIFO)
	OrderMap map[string]*list.Element // 订单ID -> 队列元素
	Volume   decimal.Decimal          // 总数量
}

// NewPriceLevel 创建价格档位
func NewPriceLevel(price decimal.Decimal) *PriceLevel {
	return &PriceLevel{
		Price:    price,
		Orders:   list.New(),
		OrderMap: make(map[string]*list.Element),
		Volume:   decimal.Zero,
	}
}

// AddOrder 添加订单到价格档位
func (p *PriceLevel) AddOrder(order *types.Order) {
	elem := p.Orders.PushBack(order)
	p.OrderMap[order.OrderID] = elem
	p.Volume = p.Volume.Add(order.RemainingQty())
}

// RemoveOrder 从价格档位移除订单
func (p *PriceLevel) RemoveOrder(orderID string) *types.Order {
	elem, ok := p.OrderMap[orderID]
	if !ok {
		return nil
	}
	order := p.Orders.Remove(elem).(*types.Order)
	delete(p.OrderMap, orderID)
	p.Volume = p.Volume.Sub(order.RemainingQty())
	return order
}

// Front 获取队首订单
func (p *PriceLevel) Front() *types.Order {
	if p.Orders.Len() == 0 {
		return nil
	}
	return p.Orders.Front().Value.(*types.Order)
}

// UpdateVolume 更新总数量
func (p *PriceLevel) UpdateVolume(delta decimal.Decimal) {
	p.Volume = p.Volume.Add(delta)
}

// IsEmpty 检查是否为空
func (p *PriceLevel) IsEmpty() bool {
	return p.Orders.Len() == 0
}

// Len 返回订单数量
func (p *PriceLevel) Len() int {
	return p.Orders.Len()
}

// OrderBook 订单簿
type OrderBook struct {
	symbol      string
	bids        *PriceLevelTree                    // 买盘 (价格从高到低)
	asks        *PriceLevelTree                    // 卖盘 (价格从低到高)
	orderIndex  map[string]*types.Order            // 订单ID -> 订单
	userOrders  map[string]map[string]*types.Order // 用户ID -> 订单ID -> 订单
	lastPrice   decimal.Decimal
	sequenceID  int64
	mu          sync.RWMutex
	deltaBuffer []*types.OrderBookDelta // 增量更新缓冲
}

// NewOrderBook 创建订单簿
func NewOrderBook(symbol string) *OrderBook {
	return &OrderBook{
		symbol:      symbol,
		bids:        NewPriceLevelTree(true),  // 买盘：价格降序
		asks:        NewPriceLevelTree(false), // 卖盘：价格升序
		orderIndex:  make(map[string]*types.Order),
		userOrders:  make(map[string]map[string]*types.Order),
		lastPrice:   decimal.Zero,
		deltaBuffer: make([]*types.OrderBookDelta, 0, 100),
	}
}

// Symbol 返回交易对
func (ob *OrderBook) Symbol() string {
	return ob.symbol
}

// AddOrder 添加订单到订单簿
func (ob *OrderBook) AddOrder(order *types.Order) {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	ob.addOrderInternal(order)
}

func (ob *OrderBook) addOrderInternal(order *types.Order) {
	tree := ob.getTree(order.Side)
	level := tree.GetOrCreate(order.Price)

	oldVolume := level.Volume
	level.AddOrder(order)

	// 索引订单
	ob.orderIndex[order.OrderID] = order

	// 用户订单索引
	if ob.userOrders[order.UserID] == nil {
		ob.userOrders[order.UserID] = make(map[string]*types.Order)
	}
	ob.userOrders[order.UserID][order.OrderID] = order

	// 记录增量更新
	action := types.DeltaActionAdd
	if !oldVolume.IsZero() {
		action = types.DeltaActionUpdate
	}
	ob.addDelta(order.Side, order.Price, level.Volume, action)
}

// RemoveOrder 从订单簿移除订单
func (ob *OrderBook) RemoveOrder(orderID string) *types.Order {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	return ob.removeOrderInternal(orderID)
}

func (ob *OrderBook) removeOrderInternal(orderID string) *types.Order {
	order, ok := ob.orderIndex[orderID]
	if !ok {
		return nil
	}

	tree := ob.getTree(order.Side)
	level := tree.Get(order.Price)
	if level == nil {
		return nil
	}

	removedOrder := level.RemoveOrder(orderID)
	if removedOrder == nil {
		return nil
	}

	// 移除索引
	delete(ob.orderIndex, orderID)
	if userOrders, ok := ob.userOrders[order.UserID]; ok {
		delete(userOrders, orderID)
		if len(userOrders) == 0 {
			delete(ob.userOrders, order.UserID)
		}
	}

	// 记录增量更新
	if level.IsEmpty() {
		tree.Remove(order.Price)
		ob.addDelta(order.Side, order.Price, decimal.Zero, types.DeltaActionDelete)
	} else {
		ob.addDelta(order.Side, order.Price, level.Volume, types.DeltaActionUpdate)
	}

	return removedOrder
}

// GetOrder 获取订单
func (ob *OrderBook) GetOrder(orderID string) *types.Order {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	return ob.orderIndex[orderID]
}

// GetUserOrders 获取用户所有订单
func (ob *OrderBook) GetUserOrders(userID string) []*types.Order {
	ob.mu.RLock()
	defer ob.mu.RUnlock()

	orders := make([]*types.Order, 0)
	if userOrders, ok := ob.userOrders[userID]; ok {
		for _, order := range userOrders {
			orders = append(orders, order.Clone())
		}
	}
	return orders
}

// BestBid 获取最优买价
func (ob *OrderBook) BestBid() *PriceLevel {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	return ob.bids.Best()
}

// BestAsk 获取最优卖价
func (ob *OrderBook) BestAsk() *PriceLevel {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	return ob.asks.Best()
}

// BestBidPrice 获取最优买价价格
func (ob *OrderBook) BestBidPrice() decimal.Decimal {
	level := ob.BestBid()
	if level == nil {
		return decimal.Zero
	}
	return level.Price
}

// BestAskPrice 获取最优卖价价格
func (ob *OrderBook) BestAskPrice() decimal.Decimal {
	level := ob.BestAsk()
	if level == nil {
		return decimal.Zero
	}
	return level.Price
}

// UpdateOrderVolume 更新订单数量（部分成交后）
func (ob *OrderBook) UpdateOrderVolume(orderID string, filledQty decimal.Decimal) {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	order, ok := ob.orderIndex[orderID]
	if !ok {
		return
	}

	order.FilledQty = order.FilledQty.Add(filledQty)
	order.UpdateTime = time.Now().UnixNano()

	if order.IsFilled() {
		order.Status = types.OrderStatusFilled
		ob.removeOrderInternal(orderID)
	} else {
		order.Status = types.OrderStatusPartiallyFilled
		tree := ob.getTree(order.Side)
		level := tree.Get(order.Price)
		if level != nil {
			level.UpdateVolume(filledQty.Neg())
			ob.addDelta(order.Side, order.Price, level.Volume, types.DeltaActionUpdate)
		}
	}
}

// SetLastPrice 设置最新价
func (ob *OrderBook) SetLastPrice(price decimal.Decimal) {
	ob.mu.Lock()
	defer ob.mu.Unlock()
	ob.lastPrice = price
}

// GetLastPrice 获取最新价
func (ob *OrderBook) GetLastPrice() decimal.Decimal {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	return ob.lastPrice
}

// Snapshot 生成订单簿快照
func (ob *OrderBook) Snapshot(depth int) *types.OrderBookSnapshot {
	ob.mu.RLock()
	defer ob.mu.RUnlock()

	ob.sequenceID++

	snapshot := &types.OrderBookSnapshot{
		Symbol:     ob.symbol,
		Bids:       ob.bids.TopLevels(depth),
		Asks:       ob.asks.TopLevels(depth),
		LastPrice:  ob.lastPrice,
		Timestamp:  time.Now().UnixNano(),
		SequenceID: ob.sequenceID,
	}

	if len(snapshot.Bids) > 0 {
		snapshot.BestBid = snapshot.Bids[0].Price
	}
	if len(snapshot.Asks) > 0 {
		snapshot.BestAsk = snapshot.Asks[0].Price
	}

	return snapshot
}

// FlushDeltas 刷新并返回增量更新
func (ob *OrderBook) FlushDeltas() []*types.OrderBookDelta {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	deltas := ob.deltaBuffer
	ob.deltaBuffer = make([]*types.OrderBookDelta, 0, 100)
	return deltas
}

func (ob *OrderBook) addDelta(side types.OrderSide, price, quantity decimal.Decimal, action types.DeltaAction) {
	delta := &types.OrderBookDelta{
		Symbol:    ob.symbol,
		Side:      side,
		Price:     price,
		Quantity:  quantity,
		Action:    action,
		Timestamp: time.Now().UnixNano(),
	}
	ob.deltaBuffer = append(ob.deltaBuffer, delta)
}

func (ob *OrderBook) getTree(side types.OrderSide) *PriceLevelTree {
	if side == types.OrderSideBuy {
		return ob.bids
	}
	return ob.asks
}

// GetOppositeTree 获取对手盘
func (ob *OrderBook) GetOppositeTree(side types.OrderSide) *PriceLevelTree {
	if side == types.OrderSideBuy {
		return ob.asks
	}
	return ob.bids
}

// Stats 返回订单簿统计信息
func (ob *OrderBook) Stats() map[string]interface{} {
	ob.mu.RLock()
	defer ob.mu.RUnlock()

	return map[string]interface{}{
		"symbol":       ob.symbol,
		"bid_levels":   ob.bids.Len(),
		"ask_levels":   ob.asks.Len(),
		"total_orders": len(ob.orderIndex),
		"total_users":  len(ob.userOrders),
		"last_price":   ob.lastPrice.String(),
	}
}
