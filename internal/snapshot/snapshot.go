package snapshot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.uber.org/zap"

	"match-engine/pkg/engine"
	"match-engine/pkg/types"
)

// SnapshotManager 快照管理器
type SnapshotManager struct {
	manager     *engine.EngineManager
	logger      *zap.Logger
	snapshotDir string
	interval    time.Duration
	depth       int
	stopChan    chan struct{}
	mu          sync.RWMutex
	snapshots   map[string]*types.OrderBookSnapshot
}

// SnapshotConfig 快照配置
type SnapshotConfig struct {
	SnapshotDir string
	Interval    time.Duration
	Depth       int
}

// DefaultSnapshotConfig 默认配置
func DefaultSnapshotConfig() *SnapshotConfig {
	return &SnapshotConfig{
		SnapshotDir: "./snapshots",
		Interval:    time.Second,
		Depth:       100,
	}
}

// NewSnapshotManager 创建快照管理器
func NewSnapshotManager(config *SnapshotConfig, manager *engine.EngineManager, logger *zap.Logger) *SnapshotManager {
	if config == nil {
		config = DefaultSnapshotConfig()
	}

	// 创建快照目录
	os.MkdirAll(config.SnapshotDir, 0755)

	return &SnapshotManager{
		manager:     manager,
		logger:      logger,
		snapshotDir: config.SnapshotDir,
		interval:    config.Interval,
		depth:       config.Depth,
		stopChan:    make(chan struct{}),
		snapshots:   make(map[string]*types.OrderBookSnapshot),
	}
}

// Start 启动快照服务
func (sm *SnapshotManager) Start() {
	go sm.snapshotLoop()
}

// Stop 停止快照服务
func (sm *SnapshotManager) Stop() {
	close(sm.stopChan)
}

// snapshotLoop 快照循环
func (sm *SnapshotManager) snapshotLoop() {
	ticker := time.NewTicker(sm.interval)
	defer ticker.Stop()

	for {
		select {
		case <-sm.stopChan:
			return
		case <-ticker.C:
			sm.takeSnapshots()
		}
	}
}

// takeSnapshots 生成所有交易对的快照
func (sm *SnapshotManager) takeSnapshots() {
	symbols := sm.manager.ListSymbols()

	for _, symbol := range symbols {
		snapshot := sm.manager.Snapshot(symbol, sm.depth)
		if snapshot != nil {
			sm.mu.Lock()
			sm.snapshots[symbol] = snapshot
			sm.mu.Unlock()
		}
	}
}

// GetSnapshot 获取快照
func (sm *SnapshotManager) GetSnapshot(symbol string) *types.OrderBookSnapshot {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.snapshots[symbol]
}

// GetAllSnapshots 获取所有快照
func (sm *SnapshotManager) GetAllSnapshots() map[string]*types.OrderBookSnapshot {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	result := make(map[string]*types.OrderBookSnapshot)
	for k, v := range sm.snapshots {
		result[k] = v
	}
	return result
}

// SaveSnapshot 保存快照到文件
func (sm *SnapshotManager) SaveSnapshot(symbol string) error {
	snapshot := sm.GetSnapshot(symbol)
	if snapshot == nil {
		return nil
	}

	filename := filepath.Join(sm.snapshotDir, symbol+"_"+time.Now().Format("20060102_150405")+".json")

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filename, data, 0644)
}

// SaveAllSnapshots 保存所有快照
func (sm *SnapshotManager) SaveAllSnapshots() error {
	snapshots := sm.GetAllSnapshots()

	for symbol := range snapshots {
		if err := sm.SaveSnapshot(symbol); err != nil {
			sm.logger.Error("failed to save snapshot",
				zap.String("symbol", symbol),
				zap.Error(err))
		}
	}
	return nil
}

// LoadSnapshot 从文件加载快照
func (sm *SnapshotManager) LoadSnapshot(filename string) (*types.OrderBookSnapshot, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var snapshot types.OrderBookSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, err
	}

	return &snapshot, nil
}

// FullSnapshot 完整快照（包含所有订单）
type FullSnapshot struct {
	Symbol     string         `json:"symbol"`
	Orders     []*types.Order `json:"orders"`
	LastPrice  string         `json:"last_price"`
	Timestamp  int64          `json:"timestamp"`
	SequenceID int64          `json:"sequence_id"`
}

// TakeFullSnapshot 生成完整快照（用于恢复）
func (sm *SnapshotManager) TakeFullSnapshot(symbol string) *FullSnapshot {
	eng := sm.manager.GetEngine(symbol)
	if eng == nil {
		return nil
	}

	// 这里需要访问订单簿的所有订单
	// 简化实现，只返回订单簿快照
	snapshot := eng.Snapshot(1000)

	return &FullSnapshot{
		Symbol:     symbol,
		Orders:     nil, // TODO: 实现完整订单导出
		LastPrice:  snapshot.LastPrice.String(),
		Timestamp:  snapshot.Timestamp,
		SequenceID: snapshot.SequenceID,
	}
}

// SaveFullSnapshot 保存完整快照
func (sm *SnapshotManager) SaveFullSnapshot(symbol string) error {
	snapshot := sm.TakeFullSnapshot(symbol)
	if snapshot == nil {
		return nil
	}

	filename := filepath.Join(sm.snapshotDir, symbol+"_full_"+time.Now().Format("20060102_150405")+".json")

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filename, data, 0644)
}
