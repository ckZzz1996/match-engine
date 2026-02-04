package position

import (
	"fmt"
	"sync"
	"time"

	"match-engine/pkg/types"

	"github.com/shopspring/decimal"
)

// Manager 仓位管理器
type Manager struct {
	// positions: map[userID]map[symbol]*Position
	positions map[string]map[string]*types.Position
	mu        sync.RWMutex

	// 合约配置
	contracts  map[string]*types.Contract
	contractMu sync.RWMutex
}

// NewManager 创建仓位管理器
func NewManager() *Manager {
	return &Manager{
		positions: make(map[string]map[string]*types.Position),
		contracts: make(map[string]*types.Contract),
	}
}

// RegisterContract 注册合约
func (m *Manager) RegisterContract(contract *types.Contract) {
	m.contractMu.Lock()
	defer m.contractMu.Unlock()
	m.contracts[contract.Symbol] = contract
}

// GetContract 获取合约配置
func (m *Manager) GetContract(symbol string) (*types.Contract, bool) {
	m.contractMu.RLock()
	defer m.contractMu.RUnlock()
	contract, ok := m.contracts[symbol]
	return contract, ok
}

// GetPosition 获取用户仓位
func (m *Manager) GetPosition(userID, symbol string) (*types.Position, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	userPositions, ok := m.positions[userID]
	if !ok {
		return nil, false
	}
	pos, ok := userPositions[symbol]
	return pos, ok
}

// GetOrCreatePosition 获取或创建仓位
func (m *Manager) GetOrCreatePosition(userID, symbol string, side types.PositionSide, marginType types.MarginType, leverage int) *types.Position {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.positions[userID]; !ok {
		m.positions[userID] = make(map[string]*types.Position)
	}

	posKey := fmt.Sprintf("%s_%s", symbol, side.String())
	if pos, ok := m.positions[userID][posKey]; ok {
		return pos
	}

	positionID := fmt.Sprintf("pos_%s_%s_%d", userID, symbol, time.Now().UnixNano())
	pos := types.NewPosition(positionID, userID, symbol, side, marginType, leverage)
	m.positions[userID][posKey] = pos
	return pos
}

// UpdatePosition 更新仓位(开仓/加仓)
func (m *Manager) UpdatePosition(userID, symbol string, side types.PositionSide,
	price, quantity decimal.Decimal, marginType types.MarginType, leverage int) (*types.Position, error) {

	pos := m.GetOrCreatePosition(userID, symbol, side, marginType, leverage)

	m.mu.Lock()
	defer m.mu.Unlock()

	// 计算新的开仓均价
	if pos.Quantity.IsZero() {
		pos.EntryPrice = price
	} else {
		// 加权平均价 = (原持仓 * 原均价 + 新增 * 新价格) / (原持仓 + 新增)
		totalValue := pos.Quantity.Mul(pos.EntryPrice).Add(quantity.Mul(price))
		totalQty := pos.Quantity.Add(quantity)
		pos.EntryPrice = totalValue.Div(totalQty)
	}

	pos.Quantity = pos.Quantity.Add(quantity)
	pos.Leverage = leverage
	pos.MarginType = marginType
	pos.UpdateTime = time.Now().UnixNano()

	// 计算保证金
	pos.Margin = m.CalculateMargin(pos)
	// 计算强平价格
	pos.LiquidationPrice = m.CalculateLiquidationPrice(pos)

	return pos, nil
}

// ReducePosition 减仓
func (m *Manager) ReducePosition(userID, symbol string, side types.PositionSide,
	price, quantity decimal.Decimal) (*types.Position, decimal.Decimal, error) {

	m.mu.Lock()
	defer m.mu.Unlock()

	posKey := fmt.Sprintf("%s_%s", symbol, side.String())
	userPositions, ok := m.positions[userID]
	if !ok {
		return nil, decimal.Zero, fmt.Errorf("no position found for user %s", userID)
	}

	pos, ok := userPositions[posKey]
	if !ok || pos.Quantity.IsZero() {
		return nil, decimal.Zero, fmt.Errorf("no position found for %s %s", symbol, side.String())
	}

	if quantity.GreaterThan(pos.Quantity) {
		return nil, decimal.Zero, fmt.Errorf("reduce quantity exceeds position quantity")
	}

	// 计算已实现盈亏
	var realizedPnL decimal.Decimal
	if side == types.PositionSideLong {
		// 多头平仓: (平仓价 - 开仓价) * 数量
		realizedPnL = price.Sub(pos.EntryPrice).Mul(quantity)
	} else {
		// 空头平仓: (开仓价 - 平仓价) * 数量
		realizedPnL = pos.EntryPrice.Sub(price).Mul(quantity)
	}

	pos.Quantity = pos.Quantity.Sub(quantity)
	pos.RealizedPnL = pos.RealizedPnL.Add(realizedPnL)
	pos.UpdateTime = time.Now().UnixNano()

	// 重新计算保证金和强平价
	if !pos.Quantity.IsZero() {
		pos.Margin = m.CalculateMargin(pos)
		pos.LiquidationPrice = m.CalculateLiquidationPrice(pos)
	} else {
		pos.Margin = decimal.Zero
		pos.LiquidationPrice = decimal.Zero
		pos.EntryPrice = decimal.Zero
	}

	return pos, realizedPnL, nil
}

// ClosePosition 平仓
func (m *Manager) ClosePosition(userID, symbol string, side types.PositionSide, price decimal.Decimal) (*types.Position, decimal.Decimal, error) {
	pos, ok := m.GetPosition(userID, fmt.Sprintf("%s_%s", symbol, side.String()))
	if !ok || pos.Quantity.IsZero() {
		return nil, decimal.Zero, fmt.Errorf("no position to close")
	}
	return m.ReducePosition(userID, symbol, side, price, pos.Quantity)
}

// CalculateMargin 计算保证金
func (m *Manager) CalculateMargin(pos *types.Position) decimal.Decimal {
	if pos.Quantity.IsZero() {
		return decimal.Zero
	}
	// 保证金 = 持仓价值 / 杠杆
	positionValue := pos.Quantity.Mul(pos.EntryPrice)
	return positionValue.Div(decimal.NewFromInt(int64(pos.Leverage)))
}

// CalculateLiquidationPrice 计算强平价格
func (m *Manager) CalculateLiquidationPrice(pos *types.Position) decimal.Decimal {
	if pos.Quantity.IsZero() {
		return decimal.Zero
	}

	contract, ok := m.GetContract(pos.Symbol)
	if !ok {
		return decimal.Zero
	}

	// 维持保证金率
	mmr := contract.MaintenanceRate
	leverage := decimal.NewFromInt(int64(pos.Leverage))

	if pos.Side == types.PositionSideLong {
		// 多头强平价 = 开仓价 * (1 - 1/杠杆 + 维持保证金率)
		factor := decimal.NewFromInt(1).Sub(decimal.NewFromInt(1).Div(leverage)).Add(mmr)
		return pos.EntryPrice.Mul(factor)
	} else {
		// 空头强平价 = 开仓价 * (1 + 1/杠杆 - 维持保证金率)
		factor := decimal.NewFromInt(1).Add(decimal.NewFromInt(1).Div(leverage)).Sub(mmr)
		return pos.EntryPrice.Mul(factor)
	}
}

// CalculateUnrealizedPnL 计算未实现盈亏
func (m *Manager) CalculateUnrealizedPnL(pos *types.Position, markPrice decimal.Decimal) decimal.Decimal {
	if pos.Quantity.IsZero() {
		return decimal.Zero
	}

	if pos.Side == types.PositionSideLong {
		// 多头: (标记价 - 开仓价) * 数量
		return markPrice.Sub(pos.EntryPrice).Mul(pos.Quantity)
	} else {
		// 空头: (开仓价 - 标记价) * 数量
		return pos.EntryPrice.Sub(markPrice).Mul(pos.Quantity)
	}
}

// UpdateMarkPrice 更新标记价格并计算未实现盈亏
func (m *Manager) UpdateMarkPrice(symbol string, markPrice decimal.Decimal) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, userPositions := range m.positions {
		for posKey, pos := range userPositions {
			if pos.Symbol == symbol && !pos.Quantity.IsZero() {
				pos.MarkPrice = markPrice
				pos.UnrealizedPnL = m.CalculateUnrealizedPnL(pos, markPrice)
				userPositions[posKey] = pos
			}
		}
	}
}

// GetUserPositions 获取用户所有仓位
func (m *Manager) GetUserPositions(userID string) []*types.Position {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*types.Position
	if userPositions, ok := m.positions[userID]; ok {
		for _, pos := range userPositions {
			if !pos.Quantity.IsZero() {
				result = append(result, pos.Clone())
			}
		}
	}
	return result
}

// CheckLiquidation 检查是否需要强平
func (m *Manager) CheckLiquidation(userID, symbol string, side types.PositionSide, markPrice decimal.Decimal) bool {
	pos, ok := m.GetPosition(userID, fmt.Sprintf("%s_%s", symbol, side.String()))
	if !ok || pos.Quantity.IsZero() {
		return false
	}

	if side == types.PositionSideLong {
		// 多头: 标记价 <= 强平价
		return markPrice.LessThanOrEqual(pos.LiquidationPrice)
	} else {
		// 空头: 标记价 >= 强平价
		return markPrice.GreaterThanOrEqual(pos.LiquidationPrice)
	}
}

// GetPositionsToLiquidate 获取需要强平的仓位
func (m *Manager) GetPositionsToLiquidate(symbol string, markPrice decimal.Decimal) []*types.Position {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*types.Position
	for _, userPositions := range m.positions {
		for _, pos := range userPositions {
			if pos.Symbol != symbol || pos.Quantity.IsZero() {
				continue
			}

			needLiquidation := false
			if pos.Side == types.PositionSideLong {
				needLiquidation = markPrice.LessThanOrEqual(pos.LiquidationPrice)
			} else {
				needLiquidation = markPrice.GreaterThanOrEqual(pos.LiquidationPrice)
			}

			if needLiquidation {
				result = append(result, pos.Clone())
			}
		}
	}
	return result
}
