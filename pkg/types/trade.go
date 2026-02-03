package types

import (
	"github.com/shopspring/decimal"
)

// Trade 成交记录
type Trade struct {
	TradeID      string          `json:"trade_id"`       // 成交ID
	Symbol       string          `json:"symbol"`         // 交易对
	MakerOrderID string          `json:"maker_order_id"` // Maker订单ID
	TakerOrderID string          `json:"taker_order_id"` // Taker订单ID
	MakerUserID  string          `json:"maker_user_id"`  // Maker用户ID
	TakerUserID  string          `json:"taker_user_id"`  // Taker用户ID
	Price        decimal.Decimal `json:"price"`          // 成交价格
	Quantity     decimal.Decimal `json:"quantity"`       // 成交数量
	MakerSide    OrderSide       `json:"maker_side"`     // Maker方向
	TakerSide    OrderSide       `json:"taker_side"`     // Taker方向
	TradeTime    int64           `json:"trade_time"`     // 成交时间(纳秒)
	SequenceID   int64           `json:"sequence_id"`    // 序列号
}

// MatchResult 撮合结果
type MatchResult struct {
	TakerOrder     *Order   `json:"taker_order"`     // Taker订单
	Trades         []*Trade `json:"trades"`          // 成交列表
	MakerOrders    []*Order `json:"maker_orders"`    // 被匹配的Maker订单
	RemainingOrder *Order   `json:"remaining_order"` // 剩余未成交订单
	Rejected       bool     `json:"rejected"`        // 是否被拒绝
	RejectReason   string   `json:"reject_reason"`   // 拒绝原因
}

// NewMatchResult 创建撮合结果
func NewMatchResult(takerOrder *Order) *MatchResult {
	return &MatchResult{
		TakerOrder:  takerOrder,
		Trades:      make([]*Trade, 0),
		MakerOrders: make([]*Order, 0),
	}
}

// AddTrade 添加成交记录
func (r *MatchResult) AddTrade(trade *Trade, makerOrder *Order) {
	r.Trades = append(r.Trades, trade)
	r.MakerOrders = append(r.MakerOrders, makerOrder)
}

// TotalFilledQty 返回总成交数量
func (r *MatchResult) TotalFilledQty() decimal.Decimal {
	total := decimal.Zero
	for _, trade := range r.Trades {
		total = total.Add(trade.Quantity)
	}
	return total
}

// HasTrades 检查是否有成交
func (r *MatchResult) HasTrades() bool {
	return len(r.Trades) > 0
}
