package liquidation

import (
	"context"
	"fmt"
	"sync"
	"time"

	"match-engine/pkg/funding"
	"match-engine/pkg/position"
	"match-engine/pkg/types"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

// Engine 强平引擎
type Engine struct {
	positionMgr *position.Manager
	fundingSvc  *funding.Service
	logger      *zap.Logger

	// 标记价格 map[symbol]markPrice
	markPrices map[string]decimal.Decimal
	mu         sync.RWMutex

	// 强平订单通道
	liquidationChan chan *types.LiquidationOrder

	// ADL 订单通道
	adlChan chan *types.ADLOrder

	// 配置
	checkInterval time.Duration
}

// NewEngine 创建强平引擎
func NewEngine(positionMgr *position.Manager, fundingSvc *funding.Service, logger *zap.Logger) *Engine {
	return &Engine{
		positionMgr:     positionMgr,
		fundingSvc:      fundingSvc,
		logger:          logger,
		markPrices:      make(map[string]decimal.Decimal),
		liquidationChan: make(chan *types.LiquidationOrder, 10000),
		adlChan:         make(chan *types.ADLOrder, 10000),
		checkInterval:   100 * time.Millisecond,
	}
}

// Start 启动强平引擎
func (e *Engine) Start(ctx context.Context) {
	go e.monitorLoop(ctx)
}

// monitorLoop 监控循环
func (e *Engine) monitorLoop(ctx context.Context) {
	ticker := time.NewTicker(e.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.checkAllPositions()
		}
	}
}

// checkAllPositions 检查所有仓位
func (e *Engine) checkAllPositions() {
	e.mu.RLock()
	symbols := make([]string, 0, len(e.markPrices))
	for symbol := range e.markPrices {
		symbols = append(symbols, symbol)
	}
	e.mu.RUnlock()

	for _, symbol := range symbols {
		e.checkSymbolPositions(symbol)
	}
}

// checkSymbolPositions 检查特定合约的所有仓位
func (e *Engine) checkSymbolPositions(symbol string) {
	e.mu.RLock()
	markPrice, ok := e.markPrices[symbol]
	e.mu.RUnlock()

	if !ok {
		return
	}

	// 获取需要强平的仓位
	positionsToLiquidate := e.positionMgr.GetPositionsToLiquidate(symbol, markPrice)

	for _, pos := range positionsToLiquidate {
		e.liquidatePosition(pos, markPrice)
	}
}

// liquidatePosition 强平仓位
func (e *Engine) liquidatePosition(pos *types.Position, markPrice decimal.Decimal) {
	e.logger.Warn("Liquidating position",
		zap.String("user_id", pos.UserID),
		zap.String("symbol", pos.Symbol),
		zap.String("side", pos.Side.String()),
		zap.String("quantity", pos.Quantity.String()),
		zap.String("entry_price", pos.EntryPrice.String()),
		zap.String("liquidation_price", pos.LiquidationPrice.String()),
		zap.String("mark_price", markPrice.String()),
	)

	// 计算亏损金额
	var lossAmount decimal.Decimal
	if pos.Side == types.PositionSideLong {
		lossAmount = pos.EntryPrice.Sub(markPrice).Mul(pos.Quantity)
	} else {
		lossAmount = markPrice.Sub(pos.EntryPrice).Mul(pos.Quantity)
	}

	// 确定平仓方向
	var side types.OrderSide
	if pos.Side == types.PositionSideLong {
		side = types.OrderSideSell // 多头强平 -> 卖出
	} else {
		side = types.OrderSideBuy // 空头强平 -> 买入
	}

	// 创建强平订单
	order := &types.LiquidationOrder{
		LiquidationID: fmt.Sprintf("liq_%s_%d", pos.PositionID, time.Now().UnixNano()),
		UserID:        pos.UserID,
		Symbol:        pos.Symbol,
		PositionID:    pos.PositionID,
		Side:          side,
		Price:         markPrice,
		Quantity:      pos.Quantity,
		LossAmount:    lossAmount,
		Reason:        "margin_insufficient",
		Timestamp:     time.Now().UnixNano(),
	}

	// 发送强平订单
	select {
	case e.liquidationChan <- order:
		e.logger.Info("Liquidation order created",
			zap.String("liquidation_id", order.LiquidationID),
		)
	default:
		e.logger.Error("Liquidation channel full, dropping order")
	}
}

// ProcessLiquidationResult 处理强平结果
func (e *Engine) ProcessLiquidationResult(order *types.LiquidationOrder, filledQty, avgPrice decimal.Decimal, success bool) {
	if !success {
		// 强平失败，需要触发 ADL
		e.triggerADL(order)
		return
	}

	// 强平成功，计算剩余保证金
	var remainingMargin decimal.Decimal
	if order.Side == types.OrderSideSell {
		// 多头平仓
		remainingMargin = avgPrice.Sub(order.Price).Mul(filledQty)
	} else {
		// 空头平仓
		remainingMargin = order.Price.Sub(avgPrice).Mul(filledQty)
	}

	// 如果有剩余，加入保险基金
	if remainingMargin.IsPositive() {
		e.fundingSvc.AddInsuranceFund(order.Symbol, remainingMargin)
		e.logger.Info("Added to insurance fund",
			zap.String("symbol", order.Symbol),
			zap.String("amount", remainingMargin.String()),
		)
	}
}

// triggerADL 触发自动减仓
func (e *Engine) triggerADL(order *types.LiquidationOrder) {
	e.logger.Warn("Triggering ADL",
		zap.String("symbol", order.Symbol),
		zap.String("user_id", order.UserID),
		zap.String("quantity", order.Quantity.String()),
	)

	// ADL 逻辑: 按照盈利排名选择对手方
	// 这里简化实现，实际需要根据盈利排名选择
	adl := &types.ADLOrder{
		ADLID:        fmt.Sprintf("adl_%d", time.Now().UnixNano()),
		Symbol:       order.Symbol,
		LiquidatorID: order.UserID,
		Side:         order.Side,
		Price:        order.Price,
		Quantity:     order.Quantity,
		Timestamp:    time.Now().UnixNano(),
	}

	select {
	case e.adlChan <- adl:
		e.logger.Info("ADL order created", zap.String("adl_id", adl.ADLID))
	default:
		e.logger.Error("ADL channel full")
	}
}

// UpdateMarkPrice 更新标记价格
func (e *Engine) UpdateMarkPrice(symbol string, markPrice decimal.Decimal) {
	e.mu.Lock()
	e.markPrices[symbol] = markPrice
	e.mu.Unlock()

	// 同步更新仓位管理器中的标记价格
	e.positionMgr.UpdateMarkPrice(symbol, markPrice)
}

// GetMarkPrice 获取标记价格
func (e *Engine) GetMarkPrice(symbol string) (decimal.Decimal, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	price, ok := e.markPrices[symbol]
	return price, ok
}

// LiquidationChannel 返回强平订单通道
func (e *Engine) LiquidationChannel() <-chan *types.LiquidationOrder {
	return e.liquidationChan
}

// ADLChannel 返回 ADL 订单通道
func (e *Engine) ADLChannel() <-chan *types.ADLOrder {
	return e.adlChan
}

// CalculateMaintenanceMargin 计算维持保证金
func (e *Engine) CalculateMaintenanceMargin(pos *types.Position) decimal.Decimal {
	contract, ok := e.positionMgr.GetContract(pos.Symbol)
	if !ok {
		return decimal.Zero
	}

	// 维持保证金 = 仓位价值 * 维持保证金率
	positionValue := pos.Quantity.Mul(pos.MarkPrice)
	return positionValue.Mul(contract.MaintenanceRate)
}

// CalculateMarginRatio 计算保证金率
func (e *Engine) CalculateMarginRatio(pos *types.Position) decimal.Decimal {
	if pos.Quantity.IsZero() {
		return decimal.Zero
	}

	// 保证金率 = (保证金 + 未实现盈亏) / 仓位价值
	positionValue := pos.Quantity.Mul(pos.MarkPrice)
	if positionValue.IsZero() {
		return decimal.Zero
	}

	effectiveMargin := pos.Margin.Add(pos.UnrealizedPnL)
	return effectiveMargin.Div(positionValue)
}
