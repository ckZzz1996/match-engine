package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics 监控指标
type Metrics struct {
	// 订单相关指标
	OrdersReceived  *prometheus.CounterVec
	OrdersProcessed *prometheus.CounterVec
	OrdersCanceled  *prometheus.CounterVec
	OrdersRejected  *prometheus.CounterVec

	// 成交相关指标
	TradesExecuted *prometheus.CounterVec
	TradeVolume    *prometheus.CounterVec

	// 延迟指标
	MatchLatency *prometheus.HistogramVec
	OrderLatency *prometheus.HistogramVec

	// 订单簿指标
	OrderBookDepth  *prometheus.GaugeVec
	OrderBookSpread *prometheus.GaugeVec
	PendingOrders   *prometheus.GaugeVec

	// 队列指标
	EventQueueLen   prometheus.Gauge
	CommandQueueLen prometheus.Gauge

	// 系统指标
	ActiveSymbols prometheus.Gauge
	Uptime        prometheus.Gauge

	startTime time.Time
}

// NewMetrics 创建监控指标
func NewMetrics(namespace string) *Metrics {
	m := &Metrics{
		startTime: time.Now(),
	}

	// 订单计数器
	m.OrdersReceived = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "orders_received_total",
			Help:      "Total number of orders received",
		},
		[]string{"symbol", "side", "type"},
	)

	m.OrdersProcessed = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "orders_processed_total",
			Help:      "Total number of orders processed",
		},
		[]string{"symbol", "side", "status"},
	)

	m.OrdersCanceled = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "orders_canceled_total",
			Help:      "Total number of orders canceled",
		},
		[]string{"symbol"},
	)

	m.OrdersRejected = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "orders_rejected_total",
			Help:      "Total number of orders rejected",
		},
		[]string{"symbol", "reason"},
	)

	// 成交计数器
	m.TradesExecuted = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "trades_executed_total",
			Help:      "Total number of trades executed",
		},
		[]string{"symbol"},
	)

	m.TradeVolume = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "trade_volume_total",
			Help:      "Total trade volume",
		},
		[]string{"symbol"},
	)

	// 延迟直方图
	m.MatchLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "match_latency_microseconds",
			Help:      "Order matching latency in microseconds",
			Buckets:   []float64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000},
		},
		[]string{"symbol"},
	)

	m.OrderLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "order_processing_latency_microseconds",
			Help:      "Order processing latency in microseconds",
			Buckets:   []float64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000},
		},
		[]string{"symbol", "type"},
	)

	// 订单簿指标
	m.OrderBookDepth = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "orderbook_depth",
			Help:      "Number of price levels in the order book",
		},
		[]string{"symbol", "side"},
	)

	m.OrderBookSpread = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "orderbook_spread",
			Help:      "Bid-ask spread",
		},
		[]string{"symbol"},
	)

	m.PendingOrders = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "pending_orders",
			Help:      "Number of pending orders",
		},
		[]string{"symbol"},
	)

	// 队列指标
	m.EventQueueLen = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "event_queue_length",
			Help:      "Length of the event queue",
		},
	)

	m.CommandQueueLen = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "command_queue_length",
			Help:      "Length of the command queue",
		},
	)

	// 系统指标
	m.ActiveSymbols = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "active_symbols",
			Help:      "Number of active trading symbols",
		},
	)

	m.Uptime = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "uptime_seconds",
			Help:      "System uptime in seconds",
		},
	)

	// 启动uptime更新
	go m.updateUptime()

	return m
}

// updateUptime 更新运行时间
func (m *Metrics) updateUptime() {
	ticker := time.NewTicker(time.Second)
	for range ticker.C {
		m.Uptime.Set(time.Since(m.startTime).Seconds())
	}
}

// RecordOrderReceived 记录收到订单
func (m *Metrics) RecordOrderReceived(symbol, side, orderType string) {
	m.OrdersReceived.WithLabelValues(symbol, side, orderType).Inc()
}

// RecordOrderProcessed 记录处理完成的订单
func (m *Metrics) RecordOrderProcessed(symbol, side, status string) {
	m.OrdersProcessed.WithLabelValues(symbol, side, status).Inc()
}

// RecordOrderCanceled 记录取消的订单
func (m *Metrics) RecordOrderCanceled(symbol string) {
	m.OrdersCanceled.WithLabelValues(symbol).Inc()
}

// RecordOrderRejected 记录拒绝的订单
func (m *Metrics) RecordOrderRejected(symbol, reason string) {
	m.OrdersRejected.WithLabelValues(symbol, reason).Inc()
}

// RecordTrade 记录成交
func (m *Metrics) RecordTrade(symbol string, volume float64) {
	m.TradesExecuted.WithLabelValues(symbol).Inc()
	m.TradeVolume.WithLabelValues(symbol).Add(volume)
}

// RecordMatchLatency 记录撮合延迟
func (m *Metrics) RecordMatchLatency(symbol string, latencyMicros float64) {
	m.MatchLatency.WithLabelValues(symbol).Observe(latencyMicros)
}

// RecordOrderLatency 记录订单处理延迟
func (m *Metrics) RecordOrderLatency(symbol, orderType string, latencyMicros float64) {
	m.OrderLatency.WithLabelValues(symbol, orderType).Observe(latencyMicros)
}

// UpdateOrderBookDepth 更新订单簿深度
func (m *Metrics) UpdateOrderBookDepth(symbol string, bidLevels, askLevels int) {
	m.OrderBookDepth.WithLabelValues(symbol, "bid").Set(float64(bidLevels))
	m.OrderBookDepth.WithLabelValues(symbol, "ask").Set(float64(askLevels))
}

// UpdateOrderBookSpread 更新买卖价差
func (m *Metrics) UpdateOrderBookSpread(symbol string, spread float64) {
	m.OrderBookSpread.WithLabelValues(symbol).Set(spread)
}

// UpdatePendingOrders 更新待处理订单数
func (m *Metrics) UpdatePendingOrders(symbol string, count int) {
	m.PendingOrders.WithLabelValues(symbol).Set(float64(count))
}

// UpdateQueueLengths 更新队列长度
func (m *Metrics) UpdateQueueLengths(eventQueueLen, commandQueueLen int) {
	m.EventQueueLen.Set(float64(eventQueueLen))
	m.CommandQueueLen.Set(float64(commandQueueLen))
}

// UpdateActiveSymbols 更新活跃交易对数量
func (m *Metrics) UpdateActiveSymbols(count int) {
	m.ActiveSymbols.Set(float64(count))
}

// Timer 计时器
type Timer struct {
	start   time.Time
	metrics *Metrics
	symbol  string
}

// NewMatchTimer 创建撮合计时器
func (m *Metrics) NewMatchTimer(symbol string) *Timer {
	return &Timer{
		start:   time.Now(),
		metrics: m,
		symbol:  symbol,
	}
}

// Stop 停止计时并记录
func (t *Timer) Stop() {
	elapsed := time.Since(t.start).Microseconds()
	t.metrics.RecordMatchLatency(t.symbol, float64(elapsed))
}

// NewOrderTimer 创建订单计时器
func (m *Metrics) NewOrderTimer(symbol, orderType string) *OrderTimer {
	return &OrderTimer{
		start:     time.Now(),
		metrics:   m,
		symbol:    symbol,
		orderType: orderType,
	}
}

// OrderTimer 订单计时器
type OrderTimer struct {
	start     time.Time
	metrics   *Metrics
	symbol    string
	orderType string
}

// Stop 停止计时并记录
func (t *OrderTimer) Stop() {
	elapsed := time.Since(t.start).Microseconds()
	t.metrics.RecordOrderLatency(t.symbol, t.orderType, float64(elapsed))
}
