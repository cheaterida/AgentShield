package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// WSEvent 是推送给前端的事件载荷。
type WSEvent struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// Hub 管理 WebSocket 连接与广播。
type Hub struct {
	mu      sync.RWMutex
	clients map[*wsClient]struct{}
	log     *slog.Logger
}

type wsClient struct {
	conn *websocket.Conn
	send chan []byte
}

func NewHub(log *slog.Logger) *Hub {
	return &Hub{
		clients: make(map[*wsClient]struct{}),
		log:     log,
	}
}

// ServeHTTP 处理 WebSocket 升级请求。
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.log.Error("ws upgrade", "err", err)
		return
	}
	c := &wsClient{conn: conn, send: make(chan []byte, 64)}
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()

	go c.writePump(h)
	go c.readPump(h)
}

// Broadcast 向所有已连接客户端发送事件。
func (h *Hub) Broadcast(ev WSEvent) {
	data, err := json.Marshal(ev)
	if err != nil {
		h.log.Error("ws marshal", "err", err)
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		select {
		case c.send <- data:
		default:
			// client too slow, drop
		}
	}
}

func (c *wsClient) writePump(h *Hub) {
	defer func() {
		c.conn.Close()
		h.mu.Lock()
		delete(h.clients, c)
		h.mu.Unlock()
	}()
	for msg := range c.send {
		if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			return
		}
	}
}

func (c *wsClient) readPump(h *Hub) {
	defer func() {
		c.conn.Close()
		h.mu.Lock()
		delete(h.clients, c)
		h.mu.Unlock()
	}()
	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
	}
}
