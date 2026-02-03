package config

import (
	"encoding/json"
	"os"
	"time"
)

// Config 系统配置
type Config struct {
	Server   ServerConfig   `json:"server"`
	Engine   EngineConfig   `json:"engine"`
	NATS     NATSConfig     `json:"nats"`
	Metrics  MetricsConfig  `json:"metrics"`
	Audit    AuditConfig    `json:"audit"`
	Snapshot SnapshotConfig `json:"snapshot"`
	Logging  LoggingConfig  `json:"logging"`
	Symbols  []string       `json:"symbols"`
}

// ServerConfig 服务器配置
type ServerConfig struct {
	GRPCAddr      string `json:"grpc_addr"`
	HTTPAddr      string `json:"http_addr"`
	WebSocketAddr string `json:"websocket_addr"`
}

// EngineConfig 引擎配置
type EngineConfig struct {
	EventBufferSize   int           `json:"event_buffer_size"`
	CommandBufferSize int           `json:"command_buffer_size"`
	WorkerCount       int           `json:"worker_count"`
	SnapshotInterval  time.Duration `json:"snapshot_interval"`
}

// NATSConfig NATS配置
type NATSConfig struct {
	URL           string `json:"url"`
	SubjectPrefix string `json:"subject_prefix"`
	Enabled       bool   `json:"enabled"`
}

// MetricsConfig 监控配置
type MetricsConfig struct {
	Enabled   bool   `json:"enabled"`
	Addr      string `json:"addr"`
	Namespace string `json:"namespace"`
}

// AuditConfig 审计配置
type AuditConfig struct {
	Enabled       bool          `json:"enabled"`
	FilePath      string        `json:"file_path"`
	BufferSize    int           `json:"buffer_size"`
	FlushSize     int           `json:"flush_size"`
	FlushInterval time.Duration `json:"flush_interval"`
}

// SnapshotConfig 快照配置
type SnapshotConfig struct {
	Enabled     bool          `json:"enabled"`
	SnapshotDir string        `json:"snapshot_dir"`
	Interval    time.Duration `json:"interval"`
	Depth       int           `json:"depth"`
}

// LoggingConfig 日志配置
type LoggingConfig struct {
	Level  string `json:"level"`
	Format string `json:"format"`
	Output string `json:"output"`
}

// DefaultConfig 默认配置
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			GRPCAddr:      ":50051",
			HTTPAddr:      ":8080",
			WebSocketAddr: ":8081",
		},
		Engine: EngineConfig{
			EventBufferSize:   100000,
			CommandBufferSize: 100000,
			WorkerCount:       1,
			SnapshotInterval:  time.Second,
		},
		NATS: NATSConfig{
			URL:           "nats://localhost:4222",
			SubjectPrefix: "matchengine",
			Enabled:       false,
		},
		Metrics: MetricsConfig{
			Enabled:   true,
			Addr:      ":9090",
			Namespace: "matchengine",
		},
		Audit: AuditConfig{
			Enabled:       true,
			FilePath:      "audit.log",
			BufferSize:    10000,
			FlushSize:     100,
			FlushInterval: time.Second,
		},
		Snapshot: SnapshotConfig{
			Enabled:     true,
			SnapshotDir: "./snapshots",
			Interval:    time.Second,
			Depth:       100,
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "json",
			Output: "stdout",
		},
		Symbols: []string{
			"BTC-USDT",
			"ETH-USDT",
		},
	}
}

// Load 从文件加载配置
func Load(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return nil, err
	}

	config := DefaultConfig()
	if err := json.Unmarshal(data, config); err != nil {
		return nil, err
	}

	return config, nil
}

// Save 保存配置到文件
func (c *Config) Save(filename string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filename, data, 0644)
}

// Validate 验证配置
func (c *Config) Validate() error {
	// TODO: 添加配置验证逻辑
	return nil
}
