package hub

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/HMasataka/axion/internal/proto"
)

// Handler はクライアントから受信したメッセージを処理する関数。
type Handler interface {
	HandleEnvelope(ctx context.Context, clientID string, env proto.Envelope) error
}

// HandlerFunc は Handler の関数型実装。
type HandlerFunc func(ctx context.Context, clientID string, env proto.Envelope) error

func (f HandlerFunc) HandleEnvelope(ctx context.Context, clientID string, env proto.Envelope) error {
	return f(ctx, clientID, env)
}

// OnDisconnect はクライアントが offline 遷移したときのコールバック。
type OnDisconnect func(ctx context.Context, clientID string)

// Hub は全 WS 接続を集中管理する。
type Hub struct {
	mu      sync.RWMutex
	conns   map[string]*Conn
	handler Handler
	onDisc  OnDisconnect
	pending *PendingRegistry
}

func New(handler Handler, onDisc OnDisconnect) *Hub {
	return &Hub{
		conns:   make(map[string]*Conn),
		handler: handler,
		onDisc:  onDisc,
		pending: NewPendingRegistry(),
	}
}

// Register は新規 WS 接続を登録する。同 ClientID の既存接続があれば閉じて差し替える。
func (h *Hub) Register(c *Conn) {
	h.mu.Lock()
	if old, ok := h.conns[c.clientID]; ok {
		old.Close()
	}
	h.conns[c.clientID] = c
	h.mu.Unlock()
}

// Unregister は接続を解除する。
func (h *Hub) Unregister(clientID string) {
	h.mu.Lock()
	delete(h.conns, clientID)
	h.mu.Unlock()
}

// Send は指定クライアントへ Envelope を送信する。接続が無ければ ErrClientOffline。
func (h *Hub) Send(ctx context.Context, clientID string, env proto.Envelope) error {
	h.mu.RLock()
	c, ok := h.conns[clientID]
	h.mu.RUnlock()
	if !ok {
		return ErrClientOffline
	}
	return c.WriteEnvelope(ctx, env)
}

// SendAndWait は Envelope を送信し、同じ correlation_id の応答を待つ。
// timeout 超過で ErrTimeout。
func (h *Hub) SendAndWait(ctx context.Context, clientID string, env proto.Envelope, timeout time.Duration) (proto.Envelope, error) {
	ch := h.pending.Register(env.CorrelationID)
	defer h.pending.Cancel(env.CorrelationID)

	if err := h.Send(ctx, clientID, env); err != nil {
		return proto.Envelope{}, err
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case resp := <-ch:
		return resp, nil
	case <-timer.C:
		return proto.Envelope{}, ErrTimeout
	case <-ctx.Done():
		return proto.Envelope{}, ctx.Err()
	}
}

// OnlineClients は現在接続中の clientID リストを返す。
func (h *Hub) OnlineClients() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	ids := make([]string, 0, len(h.conns))
	for id := range h.conns {
		ids = append(ids, id)
	}
	return ids
}

// IsOnline は指定 clientID が接続中か返す。
func (h *Hub) IsOnline(clientID string) bool {
	h.mu.RLock()
	_, ok := h.conns[clientID]
	h.mu.RUnlock()
	return ok
}

var (
	ErrClientOffline = errors.New("hub: client offline")
	ErrTimeout       = errors.New("hub: response timeout")
)
