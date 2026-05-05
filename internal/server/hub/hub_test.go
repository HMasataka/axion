package hub

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/HMasataka/axion/internal/proto"
)

// fakeConn は Hub 単体テスト用の書き込み不要な Conn スタブ。
// ws フィールドは nil のため WriteEnvelope は呼ばない前提で使う。
func newFakeConn(clientID string, h *Hub) *Conn {
	return &Conn{
		clientID: clientID,
		ws:       nil,
		hub:      h,
		closed:   make(chan struct{}),
	}
}

func noopHandler() Handler {
	return HandlerFunc(func(_ context.Context, _ string, _ proto.Envelope) error {
		return nil
	})
}

func noopOnDisc() OnDisconnect {
	return func(_ context.Context, _ string) {}
}

func TestHub_RegisterAndOnline(t *testing.T) {
	// Given
	h := New(noopHandler(), nil, noopOnDisc())
	c := newFakeConn("client-1", h)

	// When
	h.Register(c)

	// Then
	if !h.IsOnline("client-1") {
		t.Fatal("expected client-1 to be online")
	}
	clients := h.OnlineClients()
	if len(clients) != 1 || clients[0] != "client-1" {
		t.Fatalf("unexpected OnlineClients: %v", clients)
	}
}

func TestHub_UnregisterRemovesClient(t *testing.T) {
	// Given
	h := New(noopHandler(), nil, noopOnDisc())
	c := newFakeConn("client-1", h)
	h.Register(c)

	// When
	h.Unregister("client-1")

	// Then
	if h.IsOnline("client-1") {
		t.Fatal("expected client-1 to be offline after unregister")
	}
	if len(h.OnlineClients()) != 0 {
		t.Fatalf("expected empty OnlineClients, got: %v", h.OnlineClients())
	}
}

func TestHub_RegisterDuplicateReplaces(t *testing.T) {
	// Given
	h := New(noopHandler(), nil, noopOnDisc())
	old := newFakeConn("client-1", h)
	h.Register(old)

	// 旧接続の closed チャネルが閉じられることを検証するため別 Conn を用意
	// old.Close() が呼ばれると old.closed が close される
	newC := newFakeConn("client-1", h)

	// When: 同 clientID で再 Register
	h.Register(newC)

	// Then: 古い接続が閉じられている
	select {
	case <-old.closed:
		// ok
	case <-time.After(100 * time.Millisecond):
		t.Fatal("old connection was not closed after duplicate Register")
	}

	// 新しい接続がオンライン
	if !h.IsOnline("client-1") {
		t.Fatal("expected client-1 to remain online with new conn")
	}
}

func TestHub_Send_Offline(t *testing.T) {
	// Given
	h := New(noopHandler(), nil, noopOnDisc())

	// When: 未登録 clientID への送信
	err := h.Send(context.Background(), "ghost", proto.Envelope{Type: proto.TypePing})

	// Then
	if !errors.Is(err, ErrClientOffline) {
		t.Fatalf("expected ErrClientOffline, got: %v", err)
	}
}

func TestHub_SendAndWait_Timeout(t *testing.T) {
	// Given
	h := New(noopHandler(), nil, noopOnDisc())

	// 送信をキャプチャする writeOnly Conn を conns に直接挿入
	captured := make(chan proto.Envelope, 1)
	h.mu.Lock()
	h.conns["client-1"] = &Conn{
		clientID: "client-1",
		ws:       nil,
		hub:      h,
		closed:   make(chan struct{}),
		writeEnvelopeFn: func(_ context.Context, env proto.Envelope) error {
			captured <- env
			return nil
		},
	}
	h.mu.Unlock()

	env := proto.Envelope{Type: proto.TypePing, CorrelationID: "corr-timeout"}

	// When: 応答なしで短 timeout
	_, err := h.SendAndWait(context.Background(), "client-1", env, 50*time.Millisecond)

	// Then
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("expected ErrTimeout, got: %v", err)
	}
}

func TestHub_SendAndWait_Success(t *testing.T) {
	// Given
	h := New(noopHandler(), nil, noopOnDisc())
	corrID := "corr-success"

	h.mu.Lock()
	h.conns["client-1"] = &Conn{
		clientID: "client-1",
		ws:       nil,
		hub:      h,
		closed:   make(chan struct{}),
		writeEnvelopeFn: func(_ context.Context, env proto.Envelope) error {
			// 送信後、非同期で Resolve する
			go func() {
				time.Sleep(10 * time.Millisecond)
				resp := proto.Envelope{Type: proto.TypePong, CorrelationID: corrID}
				h.pending.Resolve(corrID, resp)
			}()
			return nil
		},
	}
	h.mu.Unlock()

	env := proto.Envelope{Type: proto.TypePing, CorrelationID: corrID}

	// When
	resp, err := h.SendAndWait(context.Background(), "client-1", env, 200*time.Millisecond)

	// Then
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.CorrelationID != corrID {
		t.Errorf("correlation_id mismatch: want %q, got %q", corrID, resp.CorrelationID)
	}
}
