package types

import "github.com/shopspring/decimal"

// EventType 事件类型
type EventType int8

const (
	EventTypeOrderNew     EventType = 1  // 新订单
	EventTypeOrderCancel  EventType = 2  // 取消订单
	EventTypeOrderUpdate  EventType = 3  // 订单更新
	EventTypeOrderFilled  EventType = 4  // 订单成交
	EventTypeTrade        EventType = 5  // 成交事件
	EventTypeBookUpdate   EventType = 6  // 订单簿更新
	EventTypeBookSnapshot EventType = 7  // 订单簿快照
	EventTypeMarketData   EventType = 8  // 行情数据
	EventTypeSystemStatus EventType = 9  // 系统状态
	EventTypeAuctionStart EventType = 10 // 集合竞价开始
	EventTypeAuctionEnd   EventType = 11 // 集合竞价结束

	// 合约相关事件
	EventTypePosition    EventType = 20 // 仓位变更
	EventTypeLiquidation EventType = 21 // 强平事件
	EventTypeFunding     EventType = 22 // 资金费率结算
	EventTypeADL         EventType = 23 // 自动减仓
	EventTypeMarkPrice   EventType = 24 // 标记价格更新
)

func (e EventType) String() string {
	switch e {
	case EventTypeOrderNew:
		return "ORDER_NEW"
	case EventTypeOrderCancel:
		return "ORDER_CANCEL"
	case EventTypeOrderUpdate:
		return "ORDER_UPDATE"
	case EventTypeOrderFilled:
		return "ORDER_FILLED"
	case EventTypeTrade:
		return "TRADE"
	case EventTypeBookUpdate:
		return "BOOK_UPDATE"
	case EventTypeBookSnapshot:
		return "BOOK_SNAPSHOT"
	case EventTypeMarketData:
		return "MARKET_DATA"
	case EventTypeSystemStatus:
		return "SYSTEM_STATUS"
	case EventTypeAuctionStart:
		return "AUCTION_START"
	case EventTypeAuctionEnd:
		return "AUCTION_END"
	case EventTypePosition:
		return "POSITION"
	case EventTypeLiquidation:
		return "LIQUIDATION"
	case EventTypeFunding:
		return "FUNDING"
	case EventTypeADL:
		return "ADL"
	case EventTypeMarkPrice:
		return "MARK_PRICE"
	default:
		return "UNKNOWN"
	}
}

// Event 事件接口
type Event interface {
	GetType() EventType
	GetSymbol() string
	GetTimestamp() int64
	GetSequenceID() int64
}

// BaseEvent 基础事件
type BaseEvent struct {
	Type       EventType `json:"type"`
	Symbol     string    `json:"symbol"`
	Timestamp  int64     `json:"timestamp"`
	SequenceID int64     `json:"sequence_id"`
}

func (e *BaseEvent) GetType() EventType   { return e.Type }
func (e *BaseEvent) GetSymbol() string    { return e.Symbol }
func (e *BaseEvent) GetTimestamp() int64  { return e.Timestamp }
func (e *BaseEvent) GetSequenceID() int64 { return e.SequenceID }

// OrderEvent 订单事件
type OrderEvent struct {
	BaseEvent
	Order  *Order `json:"order"`
	Reason string `json:"reason,omitempty"`
}

// TradeEvent 成交事件
type TradeEvent struct {
	BaseEvent
	Trade *Trade `json:"trade"`
}

// BookUpdateEvent 订单簿更新事件
type BookUpdateEvent struct {
	BaseEvent
	Deltas []*OrderBookDelta `json:"deltas"`
}

// BookSnapshotEvent 订单簿快照事件
type BookSnapshotEvent struct {
	BaseEvent
	Snapshot *OrderBookSnapshot `json:"snapshot"`
}

// MarketDataEvent 行情事件
type MarketDataEvent struct {
	BaseEvent
	Data *MarketData `json:"data"`
}

// SystemStatusEvent 系统状态事件
type SystemStatusEvent struct {
	BaseEvent
	Status  string `json:"status"`
	Message string `json:"message"`
}

// AuctionEvent 集合竞价事件
type AuctionEvent struct {
	BaseEvent
	Phase     string `json:"phase"` // CALL, MATCH, END
	AuctionID string `json:"auction_id"`
}

// PositionEvent 仓位变更事件
type PositionEvent struct {
	BaseEvent
	Position    *Position       `json:"position"`
	RealizedPnL decimal.Decimal `json:"realized_pnl"`
}

// LiquidationEvent 强平事件
type LiquidationEvent struct {
	BaseEvent
	UserID     string          `json:"user_id"`
	PositionID string          `json:"position_id"`
	Side       PositionSide    `json:"side"`
	Quantity   decimal.Decimal `json:"quantity"`
	Price      decimal.Decimal `json:"price"`
	LossAmount decimal.Decimal `json:"loss_amount"`
}

// FundingEvent 资金费率结算事件
type FundingEvent struct {
	BaseEvent
	FundingRate decimal.Decimal `json:"funding_rate"`
	MarkPrice   decimal.Decimal `json:"mark_price"`
	IndexPrice  decimal.Decimal `json:"index_price"`
}

// MarkPriceEvent 标记价格更新事件
type MarkPriceEvent struct {
	BaseEvent
	MarkPrice  decimal.Decimal `json:"mark_price"`
	IndexPrice decimal.Decimal `json:"index_price"`
	LastPrice  decimal.Decimal `json:"last_price"`
}

// Command 命令类型
type CommandType int8

const (
	CommandTypeNewOrder     CommandType = 1 // 新订单
	CommandTypeCancelOrder  CommandType = 2 // 取消订单
	CommandTypeModifyOrder  CommandType = 3 // 修改订单
	CommandTypeStartAuction CommandType = 4 // 开始集合竞价
	CommandTypeEndAuction   CommandType = 5 // 结束集合竞价
	CommandTypePause        CommandType = 6 // 暂停撮合
	CommandTypeResume       CommandType = 7 // 恢复撮合
)

// Command 命令接口
type Command interface {
	GetType() CommandType
	GetSymbol() string
}

// NewOrderCommand 新订单命令
type NewOrderCommand struct {
	Type  CommandType
	Order *Order
}

func (c *NewOrderCommand) GetType() CommandType { return c.Type }
func (c *NewOrderCommand) GetSymbol() string    { return c.Order.Symbol }

// CancelOrderCommand 取消订单命令
type CancelOrderCommand struct {
	Type    CommandType
	Symbol  string
	OrderID string
	UserID  string
}

func (c *CancelOrderCommand) GetType() CommandType { return c.Type }
func (c *CancelOrderCommand) GetSymbol() string    { return c.Symbol }
