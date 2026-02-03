package types

import (
	"github.com/shopspring/decimal"
)

// PriceLevel 价格档位
type PriceLevel struct {
	Price    decimal.Decimal `json:"price"`    // 价格
	Quantity decimal.Decimal `json:"quantity"` // 总数量
	Count    int             `json:"count"`    // 订单数量
}

// OrderBookSnapshot 订单簿快照
type OrderBookSnapshot struct {
	Symbol     string          `json:"symbol"`      // 交易对
	Bids       []*PriceLevel   `json:"bids"`        // 买盘
	Asks       []*PriceLevel   `json:"asks"`        // 卖盘
	LastPrice  decimal.Decimal `json:"last_price"`  // 最新价
	BestBid    decimal.Decimal `json:"best_bid"`    // 最优买价
	BestAsk    decimal.Decimal `json:"best_ask"`    // 最优卖价
	Timestamp  int64           `json:"timestamp"`   // 时间戳
	SequenceID int64           `json:"sequence_id"` // 序列号
}

// OrderBookDelta 订单簿增量更新
type OrderBookDelta struct {
	Symbol    string          `json:"symbol"`    // 交易对
	Side      OrderSide       `json:"side"`      // 方向
	Price     decimal.Decimal `json:"price"`     // 价格
	Quantity  decimal.Decimal `json:"quantity"`  // 数量(0表示删除)
	Action    DeltaAction     `json:"action"`    // 动作
	Timestamp int64           `json:"timestamp"` // 时间戳
}

// DeltaAction 增量动作类型
type DeltaAction int8

const (
	DeltaActionAdd    DeltaAction = 1 // 新增
	DeltaActionUpdate DeltaAction = 2 // 更新
	DeltaActionDelete DeltaAction = 3 // 删除
)

// MarketData 行情数据
type MarketData struct {
	Symbol      string          `json:"symbol"`       // 交易对
	LastPrice   decimal.Decimal `json:"last_price"`   // 最新价
	BestBid     decimal.Decimal `json:"best_bid"`     // 最优买价
	BestAsk     decimal.Decimal `json:"best_ask"`     // 最优卖价
	BidQty      decimal.Decimal `json:"bid_qty"`      // 买一数量
	AskQty      decimal.Decimal `json:"ask_qty"`      // 卖一数量
	Volume24h   decimal.Decimal `json:"volume_24h"`   // 24小时成交量
	High24h     decimal.Decimal `json:"high_24h"`     // 24小时最高价
	Low24h      decimal.Decimal `json:"low_24h"`      // 24小时最低价
	OpenPrice   decimal.Decimal `json:"open_price"`   // 开盘价
	PriceChange decimal.Decimal `json:"price_change"` // 价格变化
	Timestamp   int64           `json:"timestamp"`    // 时间戳
}

// KLine K线数据
type KLine struct {
	Symbol    string          `json:"symbol"`     // 交易对
	Interval  string          `json:"interval"`   // 间隔 (1m, 5m, 15m, 1h, 4h, 1d)
	OpenTime  int64           `json:"open_time"`  // 开盘时间
	CloseTime int64           `json:"close_time"` // 收盘时间
	Open      decimal.Decimal `json:"open"`       // 开盘价
	High      decimal.Decimal `json:"high"`       // 最高价
	Low       decimal.Decimal `json:"low"`        // 最低价
	Close     decimal.Decimal `json:"close"`      // 收盘价
	Volume    decimal.Decimal `json:"volume"`     // 成交量
	Trades    int64           `json:"trades"`     // 成交笔数
}
