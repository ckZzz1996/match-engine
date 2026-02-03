package types

import (
	"sync/atomic"
	"time"

	"github.com/shopspring/decimal"
)

// OrderSide 订单方向
type OrderSide int8

const (
	OrderSideBuy  OrderSide = 1
	OrderSideSell OrderSide = 2
)

func (s OrderSide) String() string {
	switch s {
	case OrderSideBuy:
		return "BUY"
	case OrderSideSell:
		return "SELL"
	default:
		return "UNKNOWN"
	}
}

// OrderType 订单类型
type OrderType int8

const (
	OrderTypeLimit      OrderType = 1 // 限价单
	OrderTypeMarket     OrderType = 2 // 市价单
	OrderTypeStopLoss   OrderType = 3 // 止损单
	OrderTypeTakeProfit OrderType = 4 // 止盈单
	OrderTypeIceberg    OrderType = 5 // 冰山单
)

func (t OrderType) String() string {
	switch t {
	case OrderTypeLimit:
		return "LIMIT"
	case OrderTypeMarket:
		return "MARKET"
	case OrderTypeStopLoss:
		return "STOP_LOSS"
	case OrderTypeTakeProfit:
		return "TAKE_PROFIT"
	case OrderTypeIceberg:
		return "ICEBERG"
	default:
		return "UNKNOWN"
	}
}

// TimeInForce 订单有效期类型
type TimeInForce int8

const (
	// TimeInForceGTC 撤销前有效 (Good Till Cancel)
	// 订单会一直有效直到被手动取消，未成交部分会挂在订单簿中等待成交
	TimeInForceGTC TimeInForce = 1

	// TimeInForceIOC 立即成交或取消 (Immediate or Cancel)
	// 订单必须立即成交，能成交多少就成交多少，未成交部分会被自动取消
	TimeInForceIOC TimeInForce = 2

	// TimeInForceFOK 全部成交或取消 (Fill or Kill)
	// 订单必须全部成交，如果无法全部成交则整个订单会被取消，不允许部分成交
	TimeInForceFOK TimeInForce = 3

	// TimeInForcePostOnly 只挂单 (Post Only / Maker Only)
	// 订单只允许作为 Maker 挂单，如果会立即与现有订单成交则会被拒绝
	// 适用于做市商，确保只赚取 Maker 手续费（通常更低）
	TimeInForcePostOnly TimeInForce = 4
)

func (t TimeInForce) String() string {
	switch t {
	case TimeInForceGTC:
		return "GTC"
	case TimeInForceIOC:
		return "IOC"
	case TimeInForceFOK:
		return "FOK"
	case TimeInForcePostOnly:
		return "POST_ONLY"
	default:
		return "UNKNOWN"
	}
}

// OrderStatus 订单状态
type OrderStatus int8

const (
	OrderStatusNew             OrderStatus = 1 // 新建
	OrderStatusPartiallyFilled OrderStatus = 2 // 部分成交
	OrderStatusFilled          OrderStatus = 3 // 完全成交
	OrderStatusCanceled        OrderStatus = 4 // 已取消
	OrderStatusRejected        OrderStatus = 5 // 已拒绝
	OrderStatusExpired         OrderStatus = 6 // 已过期
)

func (s OrderStatus) String() string {
	switch s {
	case OrderStatusNew:
		return "NEW"
	case OrderStatusPartiallyFilled:
		return "PARTIALLY_FILLED"
	case OrderStatusFilled:
		return "FILLED"
	case OrderStatusCanceled:
		return "CANCELED"
	case OrderStatusRejected:
		return "REJECTED"
	case OrderStatusExpired:
		return "EXPIRED"
	default:
		return "UNKNOWN"
	}
}

// Order 订单结构
type Order struct {
	OrderID     string          `json:"order_id"`      // 订单ID
	UserID      string          `json:"user_id"`       // 用户ID
	Symbol      string          `json:"symbol"`        // 交易对
	Side        OrderSide       `json:"side"`          // 买卖方向
	Type        OrderType       `json:"type"`          // 订单类型
	Price       decimal.Decimal `json:"price"`         // 价格
	Quantity    decimal.Decimal `json:"quantity"`      // 原始数量
	FilledQty   decimal.Decimal `json:"filled_qty"`    // 已成交数量
	Status      OrderStatus     `json:"status"`        // 订单状态
	TimeInForce TimeInForce     `json:"time_in_force"` // 有效期类型
	StopPrice   decimal.Decimal `json:"stop_price"`    // 止损/止盈触发价
	VisibleQty  decimal.Decimal `json:"visible_qty"`   // 冰山单可见数量
	ReduceOnly  bool            `json:"reduce_only"`   // 仅减仓
	CreateTime  int64           `json:"create_time"`   // 创建时间(纳秒)
	UpdateTime  int64           `json:"update_time"`   // 更新时间(纳秒)
	SequenceID  int64           `json:"sequence_id"`   // 序列号（用于排序）
}

// NewOrder 创建新订单
func NewOrder(orderID, userID, symbol string, side OrderSide, orderType OrderType,
	price, quantity decimal.Decimal, tif TimeInForce) *Order {
	now := time.Now().UnixNano()
	return &Order{
		OrderID:     orderID,
		UserID:      userID,
		Symbol:      symbol,
		Side:        side,
		Type:        orderType,
		Price:       price,
		Quantity:    quantity,
		FilledQty:   decimal.Zero,
		Status:      OrderStatusNew,
		TimeInForce: tif,
		CreateTime:  now,
		UpdateTime:  now,
	}
}

// RemainingQty 返回剩余未成交数量
func (o *Order) RemainingQty() decimal.Decimal {
	return o.Quantity.Sub(o.FilledQty)
}

// IsFilled 检查订单是否完全成交
func (o *Order) IsFilled() bool {
	return o.FilledQty.GreaterThanOrEqual(o.Quantity)
}

// Clone 深拷贝订单
func (o *Order) Clone() *Order {
	return &Order{
		OrderID:     o.OrderID,
		UserID:      o.UserID,
		Symbol:      o.Symbol,
		Side:        o.Side,
		Type:        o.Type,
		Price:       o.Price,
		Quantity:    o.Quantity,
		FilledQty:   o.FilledQty,
		Status:      o.Status,
		TimeInForce: o.TimeInForce,
		StopPrice:   o.StopPrice,
		VisibleQty:  o.VisibleQty,
		ReduceOnly:  o.ReduceOnly,
		CreateTime:  o.CreateTime,
		UpdateTime:  o.UpdateTime,
		SequenceID:  o.SequenceID,
	}
}

// OrderIDGenerator 订单ID生成器
type OrderIDGenerator struct {
	counter uint64
	prefix  string
}

// NewOrderIDGenerator 创建订单ID生成器
func NewOrderIDGenerator(prefix string) *OrderIDGenerator {
	return &OrderIDGenerator{
		counter: 0,
		prefix:  prefix,
	}
}

// NextID 生成下一个订单ID
func (g *OrderIDGenerator) NextID() string {
	id := atomic.AddUint64(&g.counter, 1)
	return g.prefix + string(rune(id))
}
