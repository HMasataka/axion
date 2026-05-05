package hub

import (
	"context"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/HMasataka/axion/internal/proto"
)

var (
	pingInterval = 10 * time.Second
	pongTimeout  = 30 * time.Second
)

const maxMessageBytes = 64 * 1024 // 64KB

// Conn は1クライアントの接続。
type Conn struct {
	clientID        string
	ws              *websocket.Conn
	hub             *Hub
	writeMu         sync.Mutex
	closed          chan struct{}
	closeOnce       sync.Once
	writeEnvelopeFn func(ctx context.Context, env proto.Envelope) error // テスト差し替え用; nil なら ws を使う
}

// NewConn は WebSocket 接続から Conn を作る。
func NewConn(clientID string, ws *websocket.Conn, h *Hub) *Conn {
	ws.SetReadLimit(maxMessageBytes)
	return &Conn{
		clientID: clientID,
		ws:       ws,
		hub:      h,
		closed:   make(chan struct{}),
	}
}

// Run は読み取りループ + ping ループを開始する。
// ctx canceled / ping error / read error / Hub.Close() で終了し、
// Hub から自動的に Unregister + OnDisconnect 発火。
func (c *Conn) Run(ctx context.Context) {
	defer c.hub.wg.Done()
	go c.pingLoop(ctx)
	c.readLoop(ctx)
	c.Close()
	c.hub.Unregister(c)
	c.hub.onDisc(ctx, c.clientID)
}

func (c *Conn) readLoop(ctx context.Context) {
	for {
		_, payload, err := c.ws.Read(ctx)
		if err != nil {
			return
		}

		env, err := proto.Unmarshal(payload)
		if err != nil {
			return
		}

		if env.Type == proto.TypePong {
			continue
		}

		if env.CorrelationID != "" && c.hub.pending.Resolve(env.CorrelationID, env) {
			continue
		}

		if err := c.hub.handler.HandleEnvelope(ctx, c.clientID, env); err != nil {
			return
		}
	}
}

func (c *Conn) pingLoop(ctx context.Context) {
	t := time.NewTicker(pingInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.closed:
			return
		case <-t.C:
			pingCtx, cancel := context.WithTimeout(ctx, pongTimeout)
			err := c.ws.Ping(pingCtx)
			cancel()
			if err != nil {
				c.Close()
				return
			}
		}
	}
}

// WriteEnvelope は Envelope を JSON で送信する。
func (c *Conn) WriteEnvelope(ctx context.Context, env proto.Envelope) error {
	if c.writeEnvelopeFn != nil {
		return c.writeEnvelopeFn(ctx, env)
	}
	payload, err := proto.Marshal(env)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.ws.Write(ctx, websocket.MessageText, payload)
}

// Close は接続を閉じる（idempotent）。
func (c *Conn) Close() {
	c.closeOnce.Do(func() {
		close(c.closed)
		if c.ws != nil {
			c.ws.CloseNow()
		}
	})
}
