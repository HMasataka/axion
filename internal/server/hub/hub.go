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

// OnConnect はクライアントが接続登録したときのコールバック。
type OnConnect func(ctx context.Context, clientID string)

// OnDisconnect はクライアントが offline 遷移したときのコールバック。
type OnDisconnect func(ctx context.Context, clientID string)

// Hub は全 WS 接続を集中管理する。
type Hub struct {
	mu        sync.RWMutex
	conns     map[string]*Conn
	handler   Handler
	onConnect OnConnect
	onDisc    OnDisconnect
	pending   *PendingRegistry
	wg        sync.WaitGroup // Run 中の Conn 数
}

func New(handler Handler, onConnect OnConnect, onDisc OnDisconnect) *Hub {
	return &Hub{
		conns:     make(map[string]*Conn),
		handler:   handler,
		onConnect: onConnect,
		onDisc:    onDisc,
		pending:   NewPendingRegistry(),
	}
}

// Register は新規 WS 接続を登録する。同 ClientID の既存接続があれば閉じて差し替える。
// 登録後に onConnect コールバックを goroutine で呼ぶ（nil の場合はスキップ）。
func (h *Hub) Register(c *Conn) {
	h.mu.Lock()
	if old, ok := h.conns[c.clientID]; ok {
		old.Close()
	}
	h.conns[c.clientID] = c
	onConnect := h.onConnect
	h.wg.Add(1)
	h.mu.Unlock()

	if onConnect != nil {
		go onConnect(context.Background(), c.clientID)
	}
}

// Close は全 Conn を閉じ Run 完了を待つ。http.Server.Shutdown が hijacked WS を待たないため必要。
func (h *Hub) Close(timeout time.Duration) {
	h.mu.Lock()
	conns := make([]*Conn, 0, len(h.conns))
	for _, c := range h.conns {
		conns = append(conns, c)
	}
	h.mu.Unlock()

	for _, c := range conns {
		c.Close()
	}

	done := make(chan struct{})
	go func() {
		h.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
	}
}

// Unregister は c が現在登録されている接続と同一の場合のみ解除する。
// 再接続で新しい Conn が既に登録されている場合は削除しない。
func (h *Hub) Unregister(c *Conn) {
	h.mu.Lock()
	if h.conns[c.clientID] == c {
		delete(h.conns, c.clientID)
	}
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
