package audit

import (
	"encoding/json"
	"os"
	"sync"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// AuditLogLevel 审计日志级别
type AuditLogLevel string

const (
	LevelInfo     AuditLogLevel = "INFO"
	LevelWarning  AuditLogLevel = "WARNING"
	LevelCritical AuditLogLevel = "CRITICAL"
)

// AuditEvent 审计事件
type AuditEvent struct {
	Timestamp time.Time              `json:"timestamp"`
	Level     AuditLogLevel          `json:"level"`
	EventType string                 `json:"event_type"`
	UserID    string                 `json:"user_id,omitempty"`
	Symbol    string                 `json:"symbol,omitempty"`
	OrderID   string                 `json:"order_id,omitempty"`
	Action    string                 `json:"action"`
	Details   map[string]interface{} `json:"details,omitempty"`
	IP        string                 `json:"ip,omitempty"`
	RequestID string                 `json:"request_id,omitempty"`
	Success   bool                   `json:"success"`
	ErrorMsg  string                 `json:"error_msg,omitempty"`
}

// AuditLogger 审计日志记录器
type AuditLogger struct {
	logger     *zap.Logger
	file       *os.File
	buffer     chan *AuditEvent
	mu         sync.Mutex
	flushSize  int
	bufferList []*AuditEvent
}

// AuditConfig 审计配置
type AuditConfig struct {
	FilePath      string
	BufferSize    int
	FlushSize     int
	FlushInterval time.Duration
}

// DefaultAuditConfig 默认配置
func DefaultAuditConfig() *AuditConfig {
	return &AuditConfig{
		FilePath:      "audit.log",
		BufferSize:    10000,
		FlushSize:     100,
		FlushInterval: time.Second,
	}
}

// NewAuditLogger 创建审计日志记录器
func NewAuditLogger(config *AuditConfig) (*AuditLogger, error) {
	if config == nil {
		config = DefaultAuditConfig()
	}

	// 打开日志文件
	file, err := os.OpenFile(config.FilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}

	// 创建zap logger
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.TimeKey = "timestamp"
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderConfig),
		zapcore.AddSync(file),
		zap.InfoLevel,
	)

	logger := zap.New(core)

	al := &AuditLogger{
		logger:     logger,
		file:       file,
		buffer:     make(chan *AuditEvent, config.BufferSize),
		flushSize:  config.FlushSize,
		bufferList: make([]*AuditEvent, 0, config.FlushSize),
	}

	// 启动后台写入协程
	go al.backgroundWriter(config.FlushInterval)

	return al, nil
}

// backgroundWriter 后台写入
func (al *AuditLogger) backgroundWriter(flushInterval time.Duration) {
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	for {
		select {
		case event := <-al.buffer:
			al.mu.Lock()
			al.bufferList = append(al.bufferList, event)
			if len(al.bufferList) >= al.flushSize {
				al.flush()
			}
			al.mu.Unlock()

		case <-ticker.C:
			al.mu.Lock()
			if len(al.bufferList) > 0 {
				al.flush()
			}
			al.mu.Unlock()
		}
	}
}

// flush 刷新缓冲区
func (al *AuditLogger) flush() {
	for _, event := range al.bufferList {
		al.writeEvent(event)
	}
	al.bufferList = al.bufferList[:0]
	al.logger.Sync()
}

// writeEvent 写入事件
func (al *AuditLogger) writeEvent(event *AuditEvent) {
	fields := []zap.Field{
		zap.String("level", string(event.Level)),
		zap.String("event_type", event.EventType),
		zap.String("action", event.Action),
		zap.Bool("success", event.Success),
	}

	if event.UserID != "" {
		fields = append(fields, zap.String("user_id", event.UserID))
	}
	if event.Symbol != "" {
		fields = append(fields, zap.String("symbol", event.Symbol))
	}
	if event.OrderID != "" {
		fields = append(fields, zap.String("order_id", event.OrderID))
	}
	if event.IP != "" {
		fields = append(fields, zap.String("ip", event.IP))
	}
	if event.RequestID != "" {
		fields = append(fields, zap.String("request_id", event.RequestID))
	}
	if event.ErrorMsg != "" {
		fields = append(fields, zap.String("error_msg", event.ErrorMsg))
	}
	if event.Details != nil {
		detailsJSON, _ := json.Marshal(event.Details)
		fields = append(fields, zap.String("details", string(detailsJSON)))
	}

	al.logger.Info("audit", fields...)
}

// Log 记录审计日志
func (al *AuditLogger) Log(event *AuditEvent) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	select {
	case al.buffer <- event:
	default:
		// 缓冲区满，直接写入
		al.writeEvent(event)
	}
}

// LogOrderCreate 记录订单创建
func (al *AuditLogger) LogOrderCreate(userID, symbol, orderID string, details map[string]interface{}, success bool, errMsg string) {
	al.Log(&AuditEvent{
		Level:     LevelInfo,
		EventType: "ORDER",
		UserID:    userID,
		Symbol:    symbol,
		OrderID:   orderID,
		Action:    "CREATE",
		Details:   details,
		Success:   success,
		ErrorMsg:  errMsg,
	})
}

// LogOrderCancel 记录订单取消
func (al *AuditLogger) LogOrderCancel(userID, symbol, orderID string, success bool, errMsg string) {
	al.Log(&AuditEvent{
		Level:     LevelInfo,
		EventType: "ORDER",
		UserID:    userID,
		Symbol:    symbol,
		OrderID:   orderID,
		Action:    "CANCEL",
		Success:   success,
		ErrorMsg:  errMsg,
	})
}

// LogTrade 记录成交
func (al *AuditLogger) LogTrade(symbol, tradeID string, details map[string]interface{}) {
	al.Log(&AuditEvent{
		Level:     LevelInfo,
		EventType: "TRADE",
		Symbol:    symbol,
		Action:    "EXECUTED",
		Details:   details,
		Success:   true,
	})
}

// LogAdminAction 记录管理操作
func (al *AuditLogger) LogAdminAction(userID, action, symbol string, details map[string]interface{}, success bool, errMsg string) {
	al.Log(&AuditEvent{
		Level:     LevelCritical,
		EventType: "ADMIN",
		UserID:    userID,
		Symbol:    symbol,
		Action:    action,
		Details:   details,
		Success:   success,
		ErrorMsg:  errMsg,
	})
}

// LogSystemEvent 记录系统事件
func (al *AuditLogger) LogSystemEvent(action string, details map[string]interface{}) {
	al.Log(&AuditEvent{
		Level:     LevelWarning,
		EventType: "SYSTEM",
		Action:    action,
		Details:   details,
		Success:   true,
	})
}

// Close 关闭日志记录器
func (al *AuditLogger) Close() error {
	al.mu.Lock()
	al.flush()
	al.mu.Unlock()

	al.logger.Sync()
	return al.file.Close()
}
