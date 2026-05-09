package ws

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/jiangminghong/texas-holdem-mp/server/internal/auth"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10 // 54s
	maxMessageSize = 4096
	sendBuffer     = 64
)

// Conn wraps a single client websocket. Read loop dispatches client messages
// to the Hub; write loop drains a buffered send channel onto the socket and
// also drives ping/pong.
type Conn struct {
	ws     *websocket.Conn
	send   chan []byte
	closed chan struct{}
	once   sync.Once

	UserID string // populated after Join
	RoomID string // populated after Join

	// Authenticated holds the verified session claims when the upgrade was
	// performed via HTTPHandler with WithAuth. When non-nil, the hub will
	// trust UserID/Nickname/Avatar from the claims rather than the join
	// payload supplied by the client.
	Authenticated *auth.Claims
}

func NewConn(ws *websocket.Conn) *Conn {
	return &Conn{
		ws:     ws,
		send:   make(chan []byte, sendBuffer),
		closed: make(chan struct{}),
	}
}

// SendMessage marshals and queues a message. Drops message if the send
// buffer is full (slow client) and closes the conn.
func (c *Conn) SendMessage(msg ServerMessage) {
	b, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[ws] marshal err: %v", err)
		return
	}
	select {
	case c.send <- b:
	default:
		log.Printf("[ws] send buffer full for user=%s, closing", c.UserID)
		c.Close()
	}
}

func (c *Conn) SendError(code, message string) {
	c.SendMessage(ServerMessage{
		Type: SMsgError,
		Data: ErrorPayload{Code: code, Message: message},
	})
}

// Close shuts down the connection. Idempotent.
func (c *Conn) Close() {
	c.once.Do(func() {
		close(c.closed)
		_ = c.ws.Close()
	})
}

// IsClosed reports whether Close has been called.
func (c *Conn) IsClosed() bool {
	select {
	case <-c.closed:
		return true
	default:
		return false
	}
}

// ReadLoop blocks reading messages from the socket and invokes handler.
// Returns when the connection is closed (cleanly or with an error).
func (c *Conn) ReadLoop(handler func(c *Conn, msg ClientMessage)) {
	defer c.Close()
	c.ws.SetReadLimit(maxMessageSize)
	_ = c.ws.SetReadDeadline(time.Now().Add(pongWait))
	c.ws.SetPongHandler(func(string) error {
		_ = c.ws.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
	for {
		_, data, err := c.ws.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("[ws] read err for user=%s: %v", c.UserID, err)
			}
			return
		}
		var msg ClientMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			c.SendError("bad-message", "invalid JSON: "+err.Error())
			continue
		}
		handler(c, msg)
	}
}

// WriteLoop drains the send channel and emits pings. Returns when closed.
func (c *Conn) WriteLoop() {
	pinger := time.NewTicker(pingPeriod)
	defer func() {
		pinger.Stop()
		c.Close()
	}()
	for {
		select {
		case <-c.closed:
			return
		case data, ok := <-c.send:
			if !ok {
				return
			}
			_ = c.ws.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.ws.WriteMessage(websocket.TextMessage, data); err != nil {
				log.Printf("[ws] write err for user=%s: %v", c.UserID, err)
				return
			}
		case <-pinger.C:
			_ = c.ws.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.ws.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
