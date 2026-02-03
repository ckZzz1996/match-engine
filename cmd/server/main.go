package main

import (
	"context"
	"flag"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"match-engine/internal/audit"
	"match-engine/internal/config"
	"match-engine/internal/metrics"
	grpcserver "match-engine/internal/server/grpc"
	"match-engine/internal/server/websocket"
	"match-engine/internal/snapshot"
	"match-engine/pkg/engine"
)

var (
	configFile = flag.String("config", "config.json", "配置文件路径")
	version    = "1.0.0"
)

func main() {
	flag.Parse()

	// 加载配置
	cfg, err := config.Load(*configFile)
	if err != nil {
		panic("failed to load config: " + err.Error())
	}

	// 初始化日志
	logger := initLogger(cfg.Logging)
	defer logger.Sync()

	logger.Info("starting match engine",
		zap.String("version", version),
		zap.Any("config", cfg))

	// 初始化引擎管理器
	engineCfg := &engine.EngineConfig{
		EventBufferSize:   cfg.Engine.EventBufferSize,
		CommandBufferSize: cfg.Engine.CommandBufferSize,
		WorkerCount:       cfg.Engine.WorkerCount,
		SnapshotInterval:  cfg.Engine.SnapshotInterval,
	}
	manager := engine.NewEngineManager(engineCfg)

	// 添加交易对
	for _, symbol := range cfg.Symbols {
		manager.AddSymbol(symbol)
		logger.Info("added symbol", zap.String("symbol", symbol))
	}

	// 启动引擎管理器
	manager.Start()

	// 初始化监控指标
	var metricsCollector *metrics.Metrics
	if cfg.Metrics.Enabled {
		metricsCollector = metrics.NewMetrics(cfg.Metrics.Namespace)
		go startMetricsServer(cfg.Metrics.Addr, logger)
		logger.Info("metrics server started", zap.String("addr", cfg.Metrics.Addr))
	}

	// 初始化审计日志
	var auditLogger *audit.AuditLogger
	if cfg.Audit.Enabled {
		auditCfg := &audit.AuditConfig{
			FilePath:      cfg.Audit.FilePath,
			BufferSize:    cfg.Audit.BufferSize,
			FlushSize:     cfg.Audit.FlushSize,
			FlushInterval: cfg.Audit.FlushInterval,
		}
		auditLogger, err = audit.NewAuditLogger(auditCfg)
		if err != nil {
			logger.Fatal("failed to create audit logger", zap.Error(err))
		}
		defer auditLogger.Close()
		logger.Info("audit logging enabled", zap.String("file", cfg.Audit.FilePath))
	}

	// 初始化快照管理器
	var snapshotManager *snapshot.SnapshotManager
	if cfg.Snapshot.Enabled {
		snapshotCfg := &snapshot.SnapshotConfig{
			SnapshotDir: cfg.Snapshot.SnapshotDir,
			Interval:    cfg.Snapshot.Interval,
			Depth:       cfg.Snapshot.Depth,
		}
		snapshotManager = snapshot.NewSnapshotManager(snapshotCfg, manager, logger)
		snapshotManager.Start()
		defer snapshotManager.Stop()
		logger.Info("snapshot manager started", zap.String("dir", cfg.Snapshot.SnapshotDir))
	}

	// 启动gRPC服务器
	grpcServer := grpc.NewServer()
	orderServer := grpcserver.NewOrderServer(manager)
	orderServer.Register(grpcServer)
	marketServer := grpcserver.NewMarketDataServer(manager)
	marketServer.Register(grpcServer)
	adminServer := grpcserver.NewAdminServer(manager)
	adminServer.Register(grpcServer)
	reflection.Register(grpcServer)

	go func() {
		lis, err := net.Listen("tcp", cfg.Server.GRPCAddr)
		if err != nil {
			logger.Fatal("failed to listen", zap.Error(err))
		}
		logger.Info("gRPC server started", zap.String("addr", cfg.Server.GRPCAddr))
		if err := grpcServer.Serve(lis); err != nil {
			logger.Fatal("failed to serve", zap.Error(err))
		}
	}()

	// 启动WebSocket服务器
	wsServer := websocket.NewServer(manager, logger)
	go wsServer.Run()

	httpMux := http.NewServeMux()
	httpMux.HandleFunc("/ws", wsServer.HandleConnection)
	httpMux.HandleFunc("/health", healthHandler)
	httpMux.HandleFunc("/ready", readyHandler)

	httpServer := &http.Server{
		Addr:    cfg.Server.WebSocketAddr,
		Handler: httpMux,
	}

	go func() {
		logger.Info("WebSocket server started", zap.String("addr", cfg.Server.WebSocketAddr))
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("failed to start http server", zap.Error(err))
		}
	}()

	// 启动指标收集
	if metricsCollector != nil {
		go collectMetrics(manager, metricsCollector)
	}

	// 等待退出信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down...")

	// 优雅关闭
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 停止接收新请求
	grpcServer.GracefulStop()
	httpServer.Shutdown(ctx)

	// 停止引擎
	manager.Stop()

	// 保存最终快照
	if snapshotManager != nil {
		snapshotManager.SaveAllSnapshots()
	}

	logger.Info("match engine stopped")
}

// initLogger 初始化日志
func initLogger(cfg config.LoggingConfig) *zap.Logger {
	var level zapcore.Level
	switch cfg.Level {
	case "debug":
		level = zap.DebugLevel
	case "info":
		level = zap.InfoLevel
	case "warn":
		level = zap.WarnLevel
	case "error":
		level = zap.ErrorLevel
	default:
		level = zap.InfoLevel
	}

	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.TimeKey = "timestamp"
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	var encoder zapcore.Encoder
	if cfg.Format == "console" {
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	} else {
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	}

	var output zapcore.WriteSyncer
	if cfg.Output == "stdout" {
		output = zapcore.AddSync(os.Stdout)
	} else {
		file, err := os.OpenFile(cfg.Output, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			panic("failed to open log file: " + err.Error())
		}
		output = zapcore.AddSync(file)
	}

	core := zapcore.NewCore(encoder, output, level)
	return zap.New(core, zap.AddCaller())
}

// startMetricsServer 启动监控服务器
func startMetricsServer(addr string, logger *zap.Logger) {
	http.Handle("/metrics", promhttp.Handler())
	if err := http.ListenAndServe(addr, nil); err != nil {
		logger.Error("metrics server error", zap.Error(err))
	}
}

// healthHandler 健康检查
func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// readyHandler 就绪检查
func readyHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Ready"))
}

// collectMetrics 收集指标
func collectMetrics(manager *engine.EngineManager, m *metrics.Metrics) {
	ticker := time.NewTicker(time.Second)
	for range ticker.C {
		stats := manager.Stats()

		if symbolCount, ok := stats["symbol_count"].(int); ok {
			m.UpdateActiveSymbols(symbolCount)
		}
		if eventQueueLen, ok := stats["event_queue_len"].(int); ok {
			if commandQueueLen, ok := stats["command_queue_len"].(int); ok {
				m.UpdateQueueLengths(eventQueueLen, commandQueueLen)
			}
		}

		// 收集每个交易对的指标
		if symbolStats, ok := stats["symbols"].(map[string]interface{}); ok {
			for symbol, stat := range symbolStats {
				if s, ok := stat.(map[string]interface{}); ok {
					bidLevels := 0
					askLevels := 0
					totalOrders := 0

					if v, ok := s["bid_levels"].(int); ok {
						bidLevels = v
					}
					if v, ok := s["ask_levels"].(int); ok {
						askLevels = v
					}
					if v, ok := s["total_orders"].(int); ok {
						totalOrders = v
					}

					m.UpdateOrderBookDepth(symbol, bidLevels, askLevels)
					m.UpdatePendingOrders(symbol, totalOrders)
				}
			}
		}
	}
}
