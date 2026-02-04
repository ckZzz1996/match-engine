package types

import (
	"time"

	"github.com/shopspring/decimal"
)

// ContractType 合约类型
type ContractType int8

const (
	ContractTypePerpetual ContractType = 1 // 永续合约
	ContractTypeFutures   ContractType = 2 // 交割合约
)

func (t ContractType) String() string {
	switch t {
	case ContractTypePerpetual:
		return "PERPETUAL"
	case ContractTypeFutures:
		return "FUTURES"
	default:
		return "UNKNOWN"
	}
}

// MarginType 保证金类型
type MarginType int8

const (
	MarginTypeCross    MarginType = 1 // 全仓
	MarginTypeIsolated MarginType = 2 // 逐仓
)

func (t MarginType) String() string {
	switch t {
	case MarginTypeCross:
		return "CROSS"
	case MarginTypeIsolated:
		return "ISOLATED"
	default:
		return "UNKNOWN"
	}
}

// PositionSide 持仓方向
type PositionSide int8

const (
	PositionSideLong  PositionSide = 1 // 多头
	PositionSideShort PositionSide = 2 // 空头
	PositionSideBoth  PositionSide = 3 // 双向持仓模式
)

func (s PositionSide) String() string {
	switch s {
	case PositionSideLong:
		return "LONG"
	case PositionSideShort:
		return "SHORT"
	case PositionSideBoth:
		return "BOTH"
	default:
		return "UNKNOWN"
	}
}

// Contract 合约信息
type Contract struct {
	Symbol          string          `json:"symbol"`           // 合约符号 (BTC-USDT-PERPETUAL)
	BaseAsset       string          `json:"base_asset"`       // 基础资产 (BTC)
	QuoteAsset      string          `json:"quote_asset"`      // 报价资产 (USDT)
	ContractType    ContractType    `json:"contract_type"`    // 合约类型
	ContractSize    decimal.Decimal `json:"contract_size"`    // 合约面值
	TickSize        decimal.Decimal `json:"tick_size"`        // 最小价格变动
	LotSize         decimal.Decimal `json:"lot_size"`         // 最小数量变动
	MaxLeverage     int             `json:"max_leverage"`     // 最大杠杆
	MakerFeeRate    decimal.Decimal `json:"maker_fee_rate"`   // Maker 费率
	TakerFeeRate    decimal.Decimal `json:"taker_fee_rate"`   // Taker 费率
	MaintenanceRate decimal.Decimal `json:"maintenance_rate"` // 维持保证金率
	InitialRate     decimal.Decimal `json:"initial_rate"`     // 初始保证金率
	MaxOrderQty     decimal.Decimal `json:"max_order_qty"`    // 单笔最大下单量
	MaxPositionQty  decimal.Decimal `json:"max_position_qty"` // 最大持仓量
	FundingInterval int64           `json:"funding_interval"` // 资金费率结算间隔(秒)
	DeliveryTime    int64           `json:"delivery_time"`    // 交割时间(交割合约)
	Status          int8            `json:"status"`           // 状态 1:交易中 2:暂停 3:已交割
}

// Position 仓位
type Position struct {
	PositionID       string          `json:"position_id"`       // 仓位ID
	UserID           string          `json:"user_id"`           // 用户ID
	Symbol           string          `json:"symbol"`            // 合约符号
	Side             PositionSide    `json:"side"`              // 持仓方向
	MarginType       MarginType      `json:"margin_type"`       // 保证金类型
	Leverage         int             `json:"leverage"`          // 杠杆倍数
	EntryPrice       decimal.Decimal `json:"entry_price"`       // 开仓均价
	MarkPrice        decimal.Decimal `json:"mark_price"`        // 标记价格
	LiquidationPrice decimal.Decimal `json:"liquidation_price"` // 强平价格
	Quantity         decimal.Decimal `json:"quantity"`          // 持仓数量
	Margin           decimal.Decimal `json:"margin"`            // 保证金
	UnrealizedPnL    decimal.Decimal `json:"unrealized_pnl"`    // 未实现盈亏
	RealizedPnL      decimal.Decimal `json:"realized_pnl"`      // 已实现盈亏
	CreateTime       int64           `json:"create_time"`       // 创建时间
	UpdateTime       int64           `json:"update_time"`       // 更新时间
}

// NewPosition 创建新仓位
func NewPosition(positionID, userID, symbol string, side PositionSide, marginType MarginType, leverage int) *Position {
	now := time.Now().UnixNano()
	return &Position{
		PositionID:    positionID,
		UserID:        userID,
		Symbol:        symbol,
		Side:          side,
		MarginType:    marginType,
		Leverage:      leverage,
		EntryPrice:    decimal.Zero,
		MarkPrice:     decimal.Zero,
		Quantity:      decimal.Zero,
		Margin:        decimal.Zero,
		UnrealizedPnL: decimal.Zero,
		RealizedPnL:   decimal.Zero,
		CreateTime:    now,
		UpdateTime:    now,
	}
}

// IsEmpty 检查仓位是否为空
func (p *Position) IsEmpty() bool {
	return p.Quantity.IsZero()
}

// Clone 深拷贝仓位
func (p *Position) Clone() *Position {
	return &Position{
		PositionID:       p.PositionID,
		UserID:           p.UserID,
		Symbol:           p.Symbol,
		Side:             p.Side,
		MarginType:       p.MarginType,
		Leverage:         p.Leverage,
		EntryPrice:       p.EntryPrice,
		MarkPrice:        p.MarkPrice,
		LiquidationPrice: p.LiquidationPrice,
		Quantity:         p.Quantity,
		Margin:           p.Margin,
		UnrealizedPnL:    p.UnrealizedPnL,
		RealizedPnL:      p.RealizedPnL,
		CreateTime:       p.CreateTime,
		UpdateTime:       p.UpdateTime,
	}
}

// ContractOrder 合约订单(扩展现货订单)
type ContractOrder struct {
	*Order                     // 继承现货订单
	PositionSide  PositionSide `json:"position_side"` // 持仓方向
	Leverage      int          `json:"leverage"` // 杠杆倍数
	MarginType    MarginType   `json:"margin_type"` // 保证金类型
	ReduceOnly    bool         `json:"reduce_only"` // 只减仓
	ClosePosition bool         `json:"close_position"` // 是否平仓
}

// FundingRate 资金费率
type FundingRate struct {
	Symbol          string          `json:"symbol"`            // 合约符号
	FundingRate     decimal.Decimal `json:"funding_rate"`      // 资金费率
	FundingTime     int64           `json:"funding_time"`      // 结算时间
	MarkPrice       decimal.Decimal `json:"mark_price"`        // 标记价格
	IndexPrice      decimal.Decimal `json:"index_price"`       // 指数价格
	NextFundingTime int64           `json:"next_funding_time"` // 下次结算时间
}

// FundingPayment 资金费用记录
type FundingPayment struct {
	PaymentID   string          `json:"payment_id"`   // 记录ID
	UserID      string          `json:"user_id"`      // 用户ID
	Symbol      string          `json:"symbol"`       // 合约符号
	PositionID  string          `json:"position_id"`  // 仓位ID
	FundingRate decimal.Decimal `json:"funding_rate"` // 资金费率
	Amount      decimal.Decimal `json:"amount"`       // 费用金额(正数收入,负数支出)
	PositionQty decimal.Decimal `json:"position_qty"` // 结算时仓位数量
	Timestamp   int64           `json:"timestamp"`    // 结算时间
}

// LiquidationOrder 强平订单
type LiquidationOrder struct {
	LiquidationID string          `json:"liquidation_id"` // 强平ID
	UserID        string          `json:"user_id"`        // 用户ID
	Symbol        string          `json:"symbol"`         // 合约符号
	PositionID    string          `json:"position_id"`    // 仓位ID
	Side          OrderSide       `json:"side"`           // 订单方向
	Price         decimal.Decimal `json:"price"`          // 强平价格
	Quantity      decimal.Decimal `json:"quantity"`       // 强平数量
	LossAmount    decimal.Decimal `json:"loss_amount"`    // 亏损金额
	Reason        string          `json:"reason"`         // 强平原因
	Timestamp     int64           `json:"timestamp"`      // 强平时间
}

// ADLOrder ADL(自动减仓)订单
type ADLOrder struct {
	ADLID          string          `json:"adl_id"`          // ADL ID
	Symbol         string          `json:"symbol"`          // 合约符号
	LiquidatorID   string          `json:"liquidator_id"`   // 被强平用户
	CounterpartyID string          `json:"counterparty_id"` // 对手方用户
	Side           OrderSide       `json:"side"`            // 方向
	Price          decimal.Decimal `json:"price"`           // 成交价格
	Quantity       decimal.Decimal `json:"quantity"`        // 数量
	Timestamp      int64           `json:"timestamp"`       // 时间
}

// InsuranceFund 保险基金
type InsuranceFund struct {
	Symbol    string          `json:"symbol"`    // 合约符号
	Balance   decimal.Decimal `json:"balance"`   // 余额
	Timestamp int64           `json:"timestamp"` // 更新时间
}

// MarkPriceInfo 标记价格信息
type MarkPriceInfo struct {
	Symbol     string          `json:"symbol"`      // 合约符号
	MarkPrice  decimal.Decimal `json:"mark_price"`  // 标记价格
	IndexPrice decimal.Decimal `json:"index_price"` // 指数价格
	LastPrice  decimal.Decimal `json:"last_price"`  // 最新成交价
	Timestamp  int64           `json:"timestamp"`   // 时间戳
}
