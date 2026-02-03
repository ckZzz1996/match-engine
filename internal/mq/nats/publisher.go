package nats

import (
	"encoding/json"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"match-engine/pkg/engine"
	"match-engine/pkg/types"
)

// Publisher NATS消息发布器
type Publisher struct {
	conn    *nats.Conn
	manager *engine.EngineManager
	logger  *zap.Logger
	prefix  string
}

// PublisherConfig 发布器配置
type PublisherConfig struct {
	URL           string
	SubjectPrefix string
}

// DefaultPublisherConfig 默认配置
func DefaultPublisherConfig() *PublisherConfig {
	return &PublisherConfig{
		URL:           nats.DefaultURL,
		SubjectPrefix: "matchengine",
	}
}

// NewPublisher 创建发布器
func NewPublisher(config *PublisherConfig, manager *engine.EngineManager, logger *zap.Logger) (*Publisher, error) {
	if config == nil {
		config = DefaultPublisherConfig()
	}

	conn, err := nats.Connect(config.URL,
		nats.MaxReconnects(-1),
		nats.ReconnectWait(time.Second),
		nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
			if err != nil {
				logger.Warn("nats disconnected", zap.Error(err))
			}
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			logger.Info("nats reconnected", zap.String("url", nc.ConnectedUrl()))
		}),
	)
	if err != nil {
		return nil, err
	}

	return &Publisher{
		conn:    conn,
		manager: manager,
		logger:  logger,
		prefix:  config.SubjectPrefix,
	}, nil
}

// Start 启动发布器
func (p *Publisher) Start() {
	go p.listenEvents()
}

// Stop 停止发布器
func (p *Publisher) Stop() {
	p.conn.Drain()
	p.conn.Close()
}

// listenEvents 监听引擎事件
func (p *Publisher) listenEvents() {
	eventChan := p.manager.EventChannel()
	for event := range eventChan {
		p.publishEvent(event)
	}
}

// publishEvent 发布事件
func (p *Publisher) publishEvent(event types.Event) {
	subject := p.getSubject(event)
	data, err := json.Marshal(event)
	if err != nil {
		p.logger.Error("failed to marshal event", zap.Error(err))
		return
	}

	if err := p.conn.Publish(subject, data); err != nil {
		p.logger.Error("failed to publish event",
			zap.String("subject", subject),
			zap.Error(err))
	}
}

// getSubject 获取NATS主题
func (p *Publisher) getSubject(event types.Event) string {
	return p.prefix + "." + event.GetSymbol() + "." + event.GetType().String()
}

// PublishTrade 发布成交事件
func (p *Publisher) PublishTrade(trade *types.Trade) error {
	subject := p.prefix + "." + trade.Symbol + ".trade"
	data, err := json.Marshal(trade)
	if err != nil {
		return err
	}
	return p.conn.Publish(subject, data)
}

// PublishOrderUpdate 发布订单更新
func (p *Publisher) PublishOrderUpdate(order *types.Order) error {
	subject := p.prefix + "." + order.Symbol + ".order"
	data, err := json.Marshal(order)
	if err != nil {
		return err
	}
	return p.conn.Publish(subject, data)
}

// PublishBookSnapshot 发布订单簿快照
func (p *Publisher) PublishBookSnapshot(snapshot *types.OrderBookSnapshot) error {
	subject := p.prefix + "." + snapshot.Symbol + ".book.snapshot"
	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	return p.conn.Publish(subject, data)
}

// PublishBookDelta 发布订单簿增量
func (p *Publisher) PublishBookDelta(delta *types.OrderBookDelta) error {
	subject := p.prefix + "." + delta.Symbol + ".book.delta"
	data, err := json.Marshal(delta)
	if err != nil {
		return err
	}
	return p.conn.Publish(subject, data)
}

// Subscriber NATS消息订阅器
type Subscriber struct {
	conn   *nats.Conn
	logger *zap.Logger
	prefix string
	subs   []*nats.Subscription
}

// NewSubscriber 创建订阅器
func NewSubscriber(config *PublisherConfig, logger *zap.Logger) (*Subscriber, error) {
	if config == nil {
		config = DefaultPublisherConfig()
	}

	conn, err := nats.Connect(config.URL)
	if err != nil {
		return nil, err
	}

	return &Subscriber{
		conn:   conn,
		logger: logger,
		prefix: config.SubjectPrefix,
		subs:   make([]*nats.Subscription, 0),
	}, nil
}

// SubscribeTrades 订阅成交
func (s *Subscriber) SubscribeTrades(symbol string, handler func(*types.Trade)) error {
	subject := s.prefix + "." + symbol + ".trade"
	sub, err := s.conn.Subscribe(subject, func(msg *nats.Msg) {
		var trade types.Trade
		if err := json.Unmarshal(msg.Data, &trade); err != nil {
			s.logger.Error("failed to unmarshal trade", zap.Error(err))
			return
		}
		handler(&trade)
	})
	if err != nil {
		return err
	}
	s.subs = append(s.subs, sub)
	return nil
}

// SubscribeOrders 订阅订单更新
func (s *Subscriber) SubscribeOrders(symbol string, handler func(*types.Order)) error {
	subject := s.prefix + "." + symbol + ".order"
	sub, err := s.conn.Subscribe(subject, func(msg *nats.Msg) {
		var order types.Order
		if err := json.Unmarshal(msg.Data, &order); err != nil {
			s.logger.Error("failed to unmarshal order", zap.Error(err))
			return
		}
		handler(&order)
	})
	if err != nil {
		return err
	}
	s.subs = append(s.subs, sub)
	return nil
}

// SubscribeBookSnapshots 订阅订单簿快照
func (s *Subscriber) SubscribeBookSnapshots(symbol string, handler func(*types.OrderBookSnapshot)) error {
	subject := s.prefix + "." + symbol + ".book.snapshot"
	sub, err := s.conn.Subscribe(subject, func(msg *nats.Msg) {
		var snapshot types.OrderBookSnapshot
		if err := json.Unmarshal(msg.Data, &snapshot); err != nil {
			s.logger.Error("failed to unmarshal snapshot", zap.Error(err))
			return
		}
		handler(&snapshot)
	})
	if err != nil {
		return err
	}
	s.subs = append(s.subs, sub)
	return nil
}

// Close 关闭订阅器
func (s *Subscriber) Close() {
	for _, sub := range s.subs {
		sub.Unsubscribe()
	}
	s.conn.Close()
}
