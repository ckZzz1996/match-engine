package engine

import (
	"sort"
	"sync"
	"time"

	"match-engine/pkg/orderbook"
	"match-engine/pkg/types"

	"github.com/shopspring/decimal"
)

// AuctionPhase 集合竞价阶段
type AuctionPhase int8

const (
	AuctionPhaseNone  AuctionPhase = 0 // 非集合竞价
	AuctionPhaseCall  AuctionPhase = 1 // 集合竞价接单阶段
	AuctionPhaseMatch AuctionPhase = 2 // 集合竞价撮合阶段
	AuctionPhaseEnd   AuctionPhase = 3 // 集合竞价结束
)

// AuctionEngine 集合竞价引擎
type AuctionEngine struct {
	symbol     string
	phase      AuctionPhase
	buyOrders  []*types.Order // 买单队列
	sellOrders []*types.Order // 卖单队列
	eventChan  chan types.Event
	mu         sync.RWMutex
	sequenceID int64
	tradeID    int64
}

// NewAuctionEngine 创建集合竞价引擎
func NewAuctionEngine(symbol string, eventChan chan types.Event) *AuctionEngine {
	return &AuctionEngine{
		symbol:     symbol,
		phase:      AuctionPhaseNone,
		buyOrders:  make([]*types.Order, 0),
		sellOrders: make([]*types.Order, 0),
		eventChan:  eventChan,
	}
}

// Symbol 返回交易对
func (e *AuctionEngine) Symbol() string {
	return e.symbol
}

// Phase 返回当前阶段
func (e *AuctionEngine) Phase() AuctionPhase {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.phase
}

// StartAuction 开始集合竞价
func (e *AuctionEngine) StartAuction() {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.phase = AuctionPhaseCall
	e.buyOrders = e.buyOrders[:0]
	e.sellOrders = e.sellOrders[:0]

	// 发送事件
	if e.eventChan != nil {
		e.eventChan <- &types.AuctionEvent{
			BaseEvent: types.BaseEvent{
				Type:       types.EventTypeAuctionStart,
				Symbol:     e.symbol,
				Timestamp:  time.Now().UnixNano(),
				SequenceID: e.nextSequenceID(),
			},
			Phase:     "CALL",
			AuctionID: e.symbol + "-" + time.Now().Format("20060102150405"),
		}
	}
}

// AddOrder 在集合竞价阶段接收订单
func (e *AuctionEngine) AddOrder(order *types.Order) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.phase != AuctionPhaseCall {
		return orderbook.ErrAuctionNotInCallPhase
	}

	// 只接受限价单
	if order.Type != types.OrderTypeLimit {
		return orderbook.ErrOnlyLimitOrderInAuction
	}

	order.SequenceID = e.nextSequenceID()
	order.Status = types.OrderStatusNew

	if order.Side == types.OrderSideBuy {
		e.buyOrders = append(e.buyOrders, order)
	} else {
		e.sellOrders = append(e.sellOrders, order)
	}

	return nil
}

// CancelOrder 在集合竞价阶段取消订单
func (e *AuctionEngine) CancelOrder(orderID string) (*types.Order, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.phase != AuctionPhaseCall {
		return nil, orderbook.ErrAuctionNotInCallPhase
	}

	// 在买单中查找
	for i, order := range e.buyOrders {
		if order.OrderID == orderID {
			e.buyOrders = append(e.buyOrders[:i], e.buyOrders[i+1:]...)
			order.Status = types.OrderStatusCanceled
			return order, nil
		}
	}

	// 在卖单中查找
	for i, order := range e.sellOrders {
		if order.OrderID == orderID {
			e.sellOrders = append(e.sellOrders[:i], e.sellOrders[i+1:]...)
			order.Status = types.OrderStatusCanceled
			return order, nil
		}
	}

	return nil, orderbook.ErrOrderNotFound
}

// EndAuction 结束集合竞价并执行撮合
func (e *AuctionEngine) EndAuction() (*AuctionResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.phase != AuctionPhaseCall {
		return nil, orderbook.ErrAuctionNotInCallPhase
	}

	e.phase = AuctionPhaseMatch

	// 计算开盘价并撮合
	result := e.match()

	e.phase = AuctionPhaseEnd

	// 发送事件
	if e.eventChan != nil {
		e.eventChan <- &types.AuctionEvent{
			BaseEvent: types.BaseEvent{
				Type:       types.EventTypeAuctionEnd,
				Symbol:     e.symbol,
				Timestamp:  time.Now().UnixNano(),
				SequenceID: e.nextSequenceID(),
			},
			Phase:     "END",
			AuctionID: e.symbol + "-" + time.Now().Format("20060102150405"),
		}
	}

	return result, nil
}

// AuctionResult 集合竞价结果
type AuctionResult struct {
	OpenPrice       decimal.Decimal `json:"open_price"`        // 开盘价
	MaxVolume       decimal.Decimal `json:"max_volume"`        // 最大成交量
	Trades          []*types.Trade  `json:"trades"`            // 成交记录
	UnmatchedBuys   []*types.Order  `json:"unmatched_buys"`    // 未成交买单
	UnmatchedSells  []*types.Order  `json:"unmatched_sells"`   // 未成交卖单
	TotalBuyVolume  decimal.Decimal `json:"total_buy_volume"`  // 买单总量
	TotalSellVolume decimal.Decimal `json:"total_sell_volume"` // 卖单总量
}

// match 执行集合竞价撮合
func (e *AuctionEngine) match() *AuctionResult {
	result := &AuctionResult{
		Trades:         make([]*types.Trade, 0),
		UnmatchedBuys:  make([]*types.Order, 0),
		UnmatchedSells: make([]*types.Order, 0),
	}

	// 没有订单
	if len(e.buyOrders) == 0 || len(e.sellOrders) == 0 {
		result.UnmatchedBuys = e.buyOrders
		result.UnmatchedSells = e.sellOrders
		return result
	}

	// 排序：买单价格降序，卖单价格升序
	sort.Slice(e.buyOrders, func(i, j int) bool {
		cmp := e.buyOrders[i].Price.Cmp(e.buyOrders[j].Price)
		if cmp != 0 {
			return cmp > 0 // 价格降序
		}
		return e.buyOrders[i].SequenceID < e.buyOrders[j].SequenceID // 时间优先
	})

	sort.Slice(e.sellOrders, func(i, j int) bool {
		cmp := e.sellOrders[i].Price.Cmp(e.sellOrders[j].Price)
		if cmp != 0 {
			return cmp < 0 // 价格升序
		}
		return e.sellOrders[i].SequenceID < e.sellOrders[j].SequenceID // 时间优先
	})

	// 计算每个价格点的可成交量，找出最大成交量对应的价格
	openPrice := e.calculateOpenPrice()
	if openPrice.IsZero() {
		result.UnmatchedBuys = e.buyOrders
		result.UnmatchedSells = e.sellOrders
		return result
	}

	result.OpenPrice = openPrice

	// 筛选可成交的订单
	matchableBuys := make([]*types.Order, 0)
	matchableSells := make([]*types.Order, 0)

	for _, order := range e.buyOrders {
		if order.Price.GreaterThanOrEqual(openPrice) {
			matchableBuys = append(matchableBuys, order)
			result.TotalBuyVolume = result.TotalBuyVolume.Add(order.RemainingQty())
		} else {
			result.UnmatchedBuys = append(result.UnmatchedBuys, order)
		}
	}

	for _, order := range e.sellOrders {
		if order.Price.LessThanOrEqual(openPrice) {
			matchableSells = append(matchableSells, order)
			result.TotalSellVolume = result.TotalSellVolume.Add(order.RemainingQty())
		} else {
			result.UnmatchedSells = append(result.UnmatchedSells, order)
		}
	}

	// 执行撮合
	buyIdx := 0
	sellIdx := 0

	for buyIdx < len(matchableBuys) && sellIdx < len(matchableSells) {
		buyOrder := matchableBuys[buyIdx]
		sellOrder := matchableSells[sellIdx]

		// 计算成交数量
		matchQty := decimal.Min(buyOrder.RemainingQty(), sellOrder.RemainingQty())

		// 创建成交记录
		trade := &types.Trade{
			TradeID:      e.nextTradeIDStr(),
			Symbol:       e.symbol,
			MakerOrderID: sellOrder.OrderID,
			TakerOrderID: buyOrder.OrderID,
			MakerUserID:  sellOrder.UserID,
			TakerUserID:  buyOrder.UserID,
			Price:        openPrice,
			Quantity:     matchQty,
			MakerSide:    types.OrderSideSell,
			TakerSide:    types.OrderSideBuy,
			TradeTime:    time.Now().UnixNano(),
			SequenceID:   e.nextSequenceID(),
		}
		result.Trades = append(result.Trades, trade)
		result.MaxVolume = result.MaxVolume.Add(matchQty)

		// 更新订单
		buyOrder.FilledQty = buyOrder.FilledQty.Add(matchQty)
		sellOrder.FilledQty = sellOrder.FilledQty.Add(matchQty)

		if buyOrder.IsFilled() {
			buyOrder.Status = types.OrderStatusFilled
			buyIdx++
		} else {
			buyOrder.Status = types.OrderStatusPartiallyFilled
		}

		if sellOrder.IsFilled() {
			sellOrder.Status = types.OrderStatusFilled
			sellIdx++
		} else {
			sellOrder.Status = types.OrderStatusPartiallyFilled
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
	}

	// 处理未完全成交的订单
	for i := buyIdx; i < len(matchableBuys); i++ {
		if !matchableBuys[i].IsFilled() {
			result.UnmatchedBuys = append(result.UnmatchedBuys, matchableBuys[i])
		}
	}
	for i := sellIdx; i < len(matchableSells); i++ {
		if !matchableSells[i].IsFilled() {
			result.UnmatchedSells = append(result.UnmatchedSells, matchableSells[i])
		}
	}

	return result
}

// calculateOpenPrice 计算开盘价（最大成交量原则）
func (e *AuctionEngine) calculateOpenPrice() decimal.Decimal {
	if len(e.buyOrders) == 0 || len(e.sellOrders) == 0 {
		return decimal.Zero
	}

	// 收集所有可能的价格点
	priceSet := make(map[string]decimal.Decimal)
	for _, order := range e.buyOrders {
		priceSet[order.Price.String()] = order.Price
	}
	for _, order := range e.sellOrders {
		priceSet[order.Price.String()] = order.Price
	}

	prices := make([]decimal.Decimal, 0, len(priceSet))
	for _, price := range priceSet {
		prices = append(prices, price)
	}
	sort.Slice(prices, func(i, j int) bool {
		return prices[i].LessThan(prices[j])
	})

	// 对每个价格点计算可成交量
	var bestPrice decimal.Decimal
	var maxVolume decimal.Decimal

	for _, price := range prices {
		// 计算该价格下的买卖总量
		var buyVolume, sellVolume decimal.Decimal
		for _, order := range e.buyOrders {
			if order.Price.GreaterThanOrEqual(price) {
				buyVolume = buyVolume.Add(order.RemainingQty())
			}
		}
		for _, order := range e.sellOrders {
			if order.Price.LessThanOrEqual(price) {
				sellVolume = sellVolume.Add(order.RemainingQty())
			}
		}

		// 可成交量 = min(买量, 卖量)
		matchVolume := decimal.Min(buyVolume, sellVolume)
		if matchVolume.GreaterThan(maxVolume) {
			maxVolume = matchVolume
			bestPrice = price
		}
	}

	return bestPrice
}

// GetIndicativePrice 获取指示性开盘价（集合竞价期间的预估价）
func (e *AuctionEngine) GetIndicativePrice() (decimal.Decimal, decimal.Decimal) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	price := e.calculateOpenPrice()
	volume := decimal.Zero

	if !price.IsZero() {
		// 计算该价格下的可成交量
		var buyVolume, sellVolume decimal.Decimal
		for _, order := range e.buyOrders {
			if order.Price.GreaterThanOrEqual(price) {
				buyVolume = buyVolume.Add(order.RemainingQty())
			}
		}
		for _, order := range e.sellOrders {
			if order.Price.LessThanOrEqual(price) {
				sellVolume = sellVolume.Add(order.RemainingQty())
			}
		}
		volume = decimal.Min(buyVolume, sellVolume)
	}

	return price, volume
}

// Reset 重置集合竞价引擎
func (e *AuctionEngine) Reset() {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.phase = AuctionPhaseNone
	e.buyOrders = e.buyOrders[:0]
	e.sellOrders = e.sellOrders[:0]
}

func (e *AuctionEngine) nextSequenceID() int64 {
	e.sequenceID++
	return e.sequenceID
}

func (e *AuctionEngine) nextTradeIDStr() string {
	e.tradeID++
	return e.symbol + "-A-" + time.Now().Format("20060102") + "-" + string(rune(e.tradeID))
}

// OrderCount 返回订单数量
func (e *AuctionEngine) OrderCount() (int, int) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.buyOrders), len(e.sellOrders)
}
