package websocket

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"match-engine/pkg/engine"
	"match-engine/pkg/types"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 4096
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // 允许所有来源，生产环境应该限制
	},
}

// Server WebSocket服务器
type Server struct {
	manager    *engine.EngineManager
	clients    map[*Client]bool
	register   chan *Client
	unregister chan *Client
	broadcast  chan []byte
	mu         sync.RWMutex
	logger     *zap.Logger
}

// NewServer 创建WebSocket服务器
func NewServer(manager *engine.EngineManager, logger *zap.Logger) *Server {
	return &Server{
		manager:    manager,
		clients:    make(map[*Client]bool),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan []byte, 256),
		logger:     logger,
	}
}

// Run 运行服务器
func (s *Server) Run() {
	// 启动事件监听
	go s.listenEvents()

	for {
		select {
		case client := <-s.register:
			s.mu.Lock()
			s.clients[client] = true
			s.mu.Unlock()
			s.logger.Info("client connected", zap.String("addr", client.conn.RemoteAddr().String()))

		case client := <-s.unregister:
			if _, ok := s.clients[client]; ok {
				s.mu.Lock()
				delete(s.clients, client)
				s.mu.Unlock()
				close(client.send)
				s.logger.Info("client disconnected", zap.String("addr", client.conn.RemoteAddr().String()))
			}

		case message := <-s.broadcast:
			s.mu.RLock()
			for client := range s.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(s.clients, client)
				}
			}
			s.mu.RUnlock()
		}
	}
}

// listenEvents 监听引擎事件
func (s *Server) listenEvents() {
	eventChan := s.manager.EventChannel()
	for event := range eventChan {
		s.handleEvent(event)
	}
}

// handleEvent 处理事件
func (s *Server) handleEvent(event types.Event) {
	msg := &WSMessage{
		Type:      event.GetType().String(),
		Symbol:    event.GetSymbol(),
		Timestamp: event.GetTimestamp(),
	}

	switch e := event.(type) {
	case *types.TradeEvent:
		msg.Data = e.Trade
	case *types.OrderEvent:
		msg.Data = e.Order
	case *types.BookUpdateEvent:
		msg.Data = e.Deltas
	case *types.BookSnapshotEvent:
		msg.Data = e.Snapshot
	case *types.MarketDataEvent:
		msg.Data = e.Data
	}

	data, err := json.Marshal(msg)
	if err != nil {
		s.logger.Error("failed to marshal event", zap.Error(err))
		return
	}

	s.broadcastToSubscribers(event.GetSymbol(), data)
}

// broadcastToSubscribers 广播给订阅者
func (s *Server) broadcastToSubscribers(symbol string, data []byte) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for client := range s.clients {
		if client.isSubscribed(symbol) {
			select {
			case client.send <- data:
			default:
				// 客户端缓冲区满，跳过
			}
		}
	}
}

// HandleConnection 处理WebSocket连接
func (s *Server) HandleConnection(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error("failed to upgrade connection", zap.Error(err))
		return
	}

	client := &Client{
		server:        s,
		conn:          conn,
		send:          make(chan []byte, 256),
		subscriptions: make(map[string]bool),
	}

	s.register <- client

	go client.writePump()
	go client.readPump()
}

// Client WebSocket客户端
type Client struct {
	server        *Server
	conn          *websocket.Conn
	send          chan []byte
	subscriptions map[string]bool
	mu            sync.RWMutex
}

// isSubscribed 检查是否订阅了某个交易对
func (c *Client) isSubscribed(symbol string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.subscriptions[symbol] || c.subscriptions["*"]
}

// subscribe 订阅交易对
func (c *Client) subscribe(symbol string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.subscriptions[symbol] = true
}

// unsubscribe 取消订阅
func (c *Client) unsubscribe(symbol string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.subscriptions, symbol)
}

// readPump 读取消息
func (c *Client) readPump() {
	defer func() {
		c.server.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.server.logger.Error("websocket error", zap.Error(err))
			}
			break
		}

		c.handleMessage(message)
	}
}

// writePump 写入消息
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// 批量发送队列中的消息
			n := len(c.send)
			for i := 0; i < n; i++ {
				w.Write([]byte{'\n'})
				w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// handleMessage 处理客户端消息
func (c *Client) handleMessage(data []byte) {
	var msg WSRequest
	if err := json.Unmarshal(data, &msg); err != nil {
		c.sendError("invalid message format")
		return
	}

	switch msg.Action {
	case "subscribe":
		c.subscribe(msg.Symbol)
		c.sendResponse(&WSResponse{
			Action:  "subscribed",
			Symbol:  msg.Symbol,
			Success: true,
		})

	case "unsubscribe":
		c.unsubscribe(msg.Symbol)
		c.sendResponse(&WSResponse{
			Action:  "unsubscribed",
			Symbol:  msg.Symbol,
			Success: true,
		})

	case "snapshot":
		// 发送订单簿快照
		snapshot := c.server.manager.Snapshot(msg.Symbol, 20)
		if snapshot != nil {
			data, _ := json.Marshal(&WSMessage{
				Type:      "BOOK_SNAPSHOT",
				Symbol:    msg.Symbol,
				Timestamp: snapshot.Timestamp,
				Data:      snapshot,
			})
			c.send <- data
		}

	case "ping":
		c.sendResponse(&WSResponse{
			Action:  "pong",
			Success: true,
		})

	default:
		c.sendError("unknown action: " + msg.Action)
	}
}

// sendError 发送错误消息
func (c *Client) sendError(message string) {
	c.sendResponse(&WSResponse{
		Action:  "error",
		Success: false,
		Message: message,
	})
}

// sendResponse 发送响应
func (c *Client) sendResponse(resp *WSResponse) {
	data, _ := json.Marshal(resp)
	select {
	case c.send <- data:
	default:
	}
}

// WSMessage WebSocket消息
type WSMessage struct {
	Type      string      `json:"type"`
	Symbol    string      `json:"symbol"`
	Timestamp int64       `json:"timestamp"`
	Data      interface{} `json:"data"`
}

// WSRequest WebSocket请求
type WSRequest struct {
	Action string                 `json:"action"`
	Symbol string                 `json:"symbol"`
	Params map[string]interface{} `json:"params,omitempty"`
}

// WSResponse WebSocket响应
type WSResponse struct {
	Action  string `json:"action"`
	Symbol  string `json:"symbol,omitempty"`
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}
