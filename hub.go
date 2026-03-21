package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 64 * 1024 // 64 KB
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(*http.Request) bool { return true },
}

// ── Client ────────────────────────────────────────────────────────────────────

// Client represents one live WebSocket connection.
// role is one of "player", "gm", "operator", "overlay".
// playerID is only set for role == "player".
type Client struct {
	hub      *Hub
	conn     *websocket.Conn
	send     chan []byte // outbound message queue
	role     string
	playerID string
}

// writePump drains the send queue onto the WebSocket, coalescing pending frames
// and sending periodic pings to keep the connection alive.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(msg)
			// Coalesce any messages that arrived while writing.
			for n := len(c.send); n > 0; n-- {
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

// readPump reads inbound messages and forwards them to the game dispatcher.
func (c *Client) readPump(game *GameState) {
	defer func() {
		game.onDisconnect(c.hub, c)
		c.hub.unregister(c)
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("ws read error: %v", err)
			}
			break
		}
		game.dispatch(c.hub, c, raw)
	}
}

// ── Hub ───────────────────────────────────────────────────────────────────────

// Hub maintains the set of all live WebSocket clients.
type Hub struct {
	mu      sync.RWMutex
	clients map[*Client]bool
}

func newHub() *Hub {
	return &Hub{clients: make(map[*Client]bool)}
}

func (h *Hub) register(c *Client) {
	h.mu.Lock()
	h.clients[c] = true
	h.mu.Unlock()
}

func (h *Hub) unregister(c *Client) {
	h.mu.Lock()
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		close(c.send)
	}
	h.mu.Unlock()
}

// deliver JSON-encodes v and enqueues it for the given client.
// If the client's send buffer is full the message is silently dropped —
// the client will receive a consistent snapshot on reconnect.
func (h *Hub) deliver(c *Client, v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	select {
	case c.send <- data:
	default:
	}
}

// broadcastRole delivers v to all clients with the given role.
func (h *Hub) broadcastRole(role string, v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		if c.role == role {
			select {
			case c.send <- data:
			default:
			}
		}
	}
}

// broadcastAll delivers v to every connected client.
func (h *Hub) broadcastAll(v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		select {
		case c.send <- data:
		default:
		}
	}
}

// findPlayer returns the Client whose playerID matches, or nil.
func (h *Hub) findPlayer(playerID string) *Client {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		if c.role == "player" && c.playerID == playerID {
			return c
		}
	}
	return nil
}

// forEachRole calls fn for every client with the given role (under read lock).
func (h *Hub) forEachRole(role string, fn func(*Client)) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		if c.role == role {
			fn(c)
		}
	}
}

// ── Upgrade ───────────────────────────────────────────────────────────────────

// serveWS upgrades an HTTP request to a WebSocket connection and starts its
// read/write goroutines.
func serveWS(hub *Hub, game *GameState, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade: %v", err)
		return
	}
	c := &Client{
		hub:  hub,
		conn: conn,
		send: make(chan []byte, 256),
	}
	hub.register(c)
	go c.writePump()
	go c.readPump(game)
}
