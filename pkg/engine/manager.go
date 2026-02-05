package engine

import (
	"sync"
	"time"

	"match-engine/pkg/funding"
	"match-engine/pkg/liquidation"
	"match-engine/pkg/position"
	"match-engine/pkg/types"

	"github.com/shopspring/decimal"
)

// EngineManager 撮合引擎管理器（支持多交易对）
type EngineManager struct {
	engines         map[string]*MatchEngine
	contractEngines map[string]*ContractMatchEngine // 合约撮合引擎
	auctions        map[string]*AuctionEngine
	eventChan       chan types.Event
	commandChan     chan types.Command
	mu              sync.RWMutex
	stopChan        chan struct{}
	workerCount     int

	// 合约相关服务
	positionMgr    *position.Manager
	liquidationEng *liquidation.Engine
	fundingSvc     *funding.Service
}

// EngineConfig 引擎配置
type EngineConfig struct {
	EventBufferSize   int
	CommandBufferSize int
	WorkerCount       int
	SnapshotInterval  time.Duration
}

// DefaultEngineConfig 默认配置
func DefaultEngineConfig() *EngineConfig {
	return &EngineConfig{
		EventBufferSize:   100000,
		CommandBufferSize: 100000,
		WorkerCount:       1,
		SnapshotInterval:  time.Second,
	}
}

// NewEngineManager 创建引擎管理器
func NewEngineManager(config *EngineConfig) *EngineManager {
	if config == nil {
		config = DefaultEngineConfig()
	}

	return &EngineManager{
		engines:         make(map[string]*MatchEngine),
		contractEngines: make(map[string]*ContractMatchEngine),
		auctions:        make(map[string]*AuctionEngine),
		eventChan:       make(chan types.Event, config.EventBufferSize),
		commandChan:     make(chan types.Command, config.CommandBufferSize),
		stopChan:        make(chan struct{}),
		workerCount:     config.WorkerCount,
	}
}

// SetContractServices 设置合约相关服务
func (m *EngineManager) SetContractServices(
	positionMgr *position.Manager,
	liquidationEng *liquidation.Engine,
	fundingSvc *funding.Service,
) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.positionMgr = positionMgr
	m.liquidationEng = liquidationEng
	m.fundingSvc = fundingSvc
}

// AddSymbol 添加交易对
func (m *EngineManager) AddSymbol(symbol string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.engines[symbol]; !ok {
		m.engines[symbol] = NewMatchEngine(symbol, m.eventChan)
		m.auctions[symbol] = NewAuctionEngine(symbol, m.eventChan)
	}
}

// AddContractSymbol 添加合约交易对
func (m *EngineManager) AddContractSymbol(contract *types.Contract) {
	m.mu.Lock()
	defer m.mu.Unlock()

	symbol := contract.Symbol
	if _, ok := m.contractEngines[symbol]; !ok {
		// 注册合约配置
		if m.positionMgr != nil {
			m.positionMgr.RegisterContract(contract)
		}

		// 创建合约撮合引擎
		m.contractEngines[symbol] = NewContractMatchEngine(
			symbol,
			m.eventChan,
			m.positionMgr,
			m.liquidationEng,
			m.fundingSvc,
			contract,
		)

		// 同时创建现货引擎用于底层撮合
		m.engines[symbol] = NewMatchEngine(symbol, m.eventChan)
	}
}

// RemoveContractSymbol 移除合约交易对
func (m *EngineManager) RemoveContractSymbol(symbol string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.contractEngines, symbol)
	delete(m.engines, symbol)
}

// RemoveSymbol 移除交易对
func (m *EngineManager) RemoveSymbol(symbol string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.engines, symbol)
	delete(m.auctions, symbol)
}

// GetEngine 获取撮合引擎
func (m *EngineManager) GetEngine(symbol string) *MatchEngine {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.engines[symbol]
}

// GetAuctionEngine 获取集合竞价引擎
func (m *EngineManager) GetAuctionEngine(symbol string) *AuctionEngine {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.auctions[symbol]
}

// GetContractEngine 获取合约撮合引擎
func (m *EngineManager) GetContractEngine(symbol string) *ContractMatchEngine {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.contractEngines[symbol]
}

// ListContractSymbols 列出所有合约交易对
func (m *EngineManager) ListContractSymbols() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	symbols := make([]string, 0, len(m.contractEngines))
	for symbol := range m.contractEngines {
		symbols = append(symbols, symbol)
	}
	return symbols
}

// ListSymbols 列出所有交易对
func (m *EngineManager) ListSymbols() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	symbols := make([]string, 0, len(m.engines))
	for symbol := range m.engines {
		symbols = append(symbols, symbol)
	}
	return symbols
}

// ProcessOrder 处理订单
func (m *EngineManager) ProcessOrder(order *types.Order) *types.MatchResult {
	engine := m.GetEngine(order.Symbol)
	if engine == nil {
		result := types.NewMatchResult(order)
		result.Rejected = true
		result.RejectReason = "symbol not found"
		return result
	}

	return engine.ProcessOrder(order)
}

// ProcessContractOrder 处理合约订单
func (m *EngineManager) ProcessContractOrder(order *types.ContractOrder) *ContractMatchResult {
	engine := m.GetContractEngine(order.Symbol)
	if engine == nil {
		result := NewContractMatchResult(order)
		result.Rejected = true
		result.RejectReason = "contract symbol not found"
		return result
	}

	return engine.ProcessContractOrder(order)
}

// ProcessLiquidationOrder 处理强平订单
func (m *EngineManager) ProcessLiquidationOrder(order *types.LiquidationOrder) *LiquidationResult {
	engine := m.GetContractEngine(order.Symbol)
	if engine == nil {
		return &LiquidationResult{
			LiquidationOrder: order,
			Success:          false,
		}
	}

	return engine.ProcessLiquidationOrder(order)
}

// UpdateMarkPrice 更新合约标记价格
func (m *EngineManager) UpdateMarkPrice(symbol string, markPrice decimal.Decimal) {
	engine := m.GetContractEngine(symbol)
	if engine != nil {
		engine.UpdateMarkPrice(markPrice)
	}
}

// CancelOrder 取消订单
func (m *EngineManager) CancelOrder(symbol, orderID string) (*types.Order, error) {
	engine := m.GetEngine(symbol)
	if engine == nil {
		return nil, ErrSymbolNotFound
	}

	return engine.CancelOrder(orderID)
}

// GetOrder 获取订单
func (m *EngineManager) GetOrder(symbol, orderID string) *types.Order {
	engine := m.GetEngine(symbol)
	if engine == nil {
		return nil
	}

	return engine.GetOrder(orderID)
}

// GetUserOrders 获取用户订单
func (m *EngineManager) GetUserOrders(symbol, userID string) []*types.Order {
	engine := m.GetEngine(symbol)
	if engine == nil {
		return nil
	}

	return engine.GetUserOrders(userID)
}

// Snapshot 获取订单簿快照
func (m *EngineManager) Snapshot(symbol string, depth int) *types.OrderBookSnapshot {
	engine := m.GetEngine(symbol)
	if engine == nil {
		return nil
	}

	return engine.Snapshot(depth)
}

// EventChannel 返回事件通道
func (m *EngineManager) EventChannel() <-chan types.Event {
	return m.eventChan
}

// SubmitCommand 提交命令
func (m *EngineManager) SubmitCommand(cmd types.Command) {
	m.commandChan <- cmd
}

// Start 启动引擎管理器
func (m *EngineManager) Start() {
	go m.commandWorker()
}

// Stop 停止引擎管理器
func (m *EngineManager) Stop() {
	close(m.stopChan)
}

// commandWorker 命令处理工作协程
func (m *EngineManager) commandWorker() {
	for {
		select {
		case <-m.stopChan:
			return
		case cmd := <-m.commandChan:
			m.processCommand(cmd)
		}
	}
}

// processCommand 处理命令
func (m *EngineManager) processCommand(cmd types.Command) {
	switch c := cmd.(type) {
	case *types.NewOrderCommand:
		m.ProcessOrder(c.Order)
	case *types.CancelOrderCommand:
		m.CancelOrder(c.Symbol, c.OrderID)
	}
}

// Stats 返回统计信息
func (m *EngineManager) Stats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := make(map[string]interface{})
	stats["symbol_count"] = len(m.engines)
	stats["event_queue_len"] = len(m.eventChan)
	stats["command_queue_len"] = len(m.commandChan)

	symbolStats := make(map[string]interface{})
	for symbol, engine := range m.engines {
		symbolStats[symbol] = engine.OrderBook().Stats()
	}
	stats["symbols"] = symbolStats

	return stats
}

// PauseSymbol 暂停交易对
func (m *EngineManager) PauseSymbol(symbol string) bool {
	engine := m.GetEngine(symbol)
	if engine == nil {
		return false
	}
	engine.Pause()
	return true
}

// ResumeSymbol 恢复交易对
func (m *EngineManager) ResumeSymbol(symbol string) bool {
	engine := m.GetEngine(symbol)
	if engine == nil {
		return false
	}
	engine.Resume()
	return true
}

// StartAuction 开始集合竞价
func (m *EngineManager) StartAuction(symbol string) bool {
	auction := m.GetAuctionEngine(symbol)
	if auction == nil {
		return false
	}
	auction.StartAuction()
	return true
}

// EndAuction 结束集合竞价
func (m *EngineManager) EndAuction(symbol string) (*AuctionResult, error) {
	auction := m.GetAuctionEngine(symbol)
	if auction == nil {
		return nil, ErrSymbolNotFound
	}

	result, err := auction.EndAuction()
	if err != nil {
		return nil, err
	}

	// 将未成交订单转入连续竞价
	engine := m.GetEngine(symbol)
	if engine != nil {
		for _, order := range result.UnmatchedBuys {
			engine.ProcessOrder(order)
		}
		for _, order := range result.UnmatchedSells {
			engine.ProcessOrder(order)
		}

		// 设置开盘价
		if !result.OpenPrice.IsZero() {
			engine.OrderBook().SetLastPrice(result.OpenPrice)
		}
	}

	return result, nil
}

// Error definitions
var (
	ErrSymbolNotFound = newEngineError("symbol not found")
)

type EngineError struct {
	message string
}

func newEngineError(message string) *EngineError {
	return &EngineError{message: message}
}

func (e *EngineError) Error() string {
	return e.message
}
