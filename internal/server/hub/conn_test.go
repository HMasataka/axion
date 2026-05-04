package hub

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/HMasataka/axion/internal/proto"
)

// wsTestServer は httptest.Server + websocket.Accept でサーバ側を提供する。
// onAccept は Accept 後にサーバ側 *websocket.Conn を受け取り任意処理を行う。
func wsTestServer(t *testing.T, onAccept func(serverWS *websocket.Conn)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		onAccept(ws)
	}))
}

// dialClient はテスト用クライアント WS 接続を返す。
func dialClient(t *testing.T, srv *httptest.Server) *websocket.Conn {
	t.Helper()
	u := "ws" + strings.TrimPrefix(srv.URL, "http")
	ws, _, err := websocket.Dial(context.Background(), u, &websocket.DialOptions{})
	if err != nil {
		t.Fatalf("websocket.Dial: %v", err)
	}
	return ws
}

func TestConn_ReceivedMessageRoutesToHandler(t *testing.T) {
	// Given: サーバ側で Hub + Conn を起動し、クライアントからメッセージを送る
	handled := make(chan proto.Envelope, 1)
	handler := HandlerFunc(func(_ context.Context, _ string, env proto.Envelope) error {
		handled <- env
		return nil
	})
	discCalled := make(chan struct{}, 1)
	onDisc := OnDisconnect(func(_ context.Context, _ string) {
		discCalled <- struct{}{}
	})
	h := New(handler, onDisc)

	srv := wsTestServer(t, func(serverWS *websocket.Conn) {
		c := NewConn("client-1", serverWS, h)
		h.Register(c)
		go c.Run(context.Background())
	})
	defer srv.Close()

	clientWS := dialClient(t, srv)
	defer clientWS.Close(websocket.StatusNormalClosure, "")

	// When: クライアントからメッセージ送信
	env := proto.Envelope{Type: proto.TypePing}
	payload, err := proto.Marshal(env)
	if err != nil {
		t.Fatalf("proto.Marshal: %v", err)
	}
	if err := clientWS.Write(context.Background(), websocket.MessageText, payload); err != nil {
		t.Fatalf("clientWS.Write: %v", err)
	}

	// Then: Handler が呼ばれる
	select {
	case got := <-handled:
		if got.Type != proto.TypePing {
			t.Errorf("handler got type %q, want %q", got.Type, proto.TypePing)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("handler was not called within timeout")
	}
}

func TestConn_PongResponseExtendsConnection(t *testing.T) {
	// Given: 短い pingInterval で Conn を起動する。クライアント側も read ループを走らせ、
	// ping に対して pong を自動応答させる。
	origInterval := pingInterval
	origTimeout := pongTimeout
	pingInterval = 50 * time.Millisecond
	pongTimeout = 200 * time.Millisecond
	defer func() {
		pingInterval = origInterval
		pongTimeout = origTimeout
	}()

	discCalled := make(chan struct{}, 1)
	onDisc := OnDisconnect(func(_ context.Context, _ string) {
		select {
		case discCalled <- struct{}{}:
		default:
		}
	})
	h := New(noopHandler(), onDisc)

	srv := wsTestServer(t, func(serverWS *websocket.Conn) {
		c := NewConn("client-1", serverWS, h)
		h.Register(c)
		go c.Run(context.Background())
	})
	defer srv.Close()

	clientCtx, clientCancel := context.WithCancel(context.Background())
	defer clientCancel()

	clientWS := dialClient(t, srv)
	defer clientWS.Close(websocket.StatusNormalClosure, "")

	// クライアント側も read ループを走らせて ping に対する pong 自動応答を有効にする
	go func() {
		for {
			_, _, err := clientWS.Read(clientCtx)
			if err != nil {
				return
			}
		}
	}()

	// When: 300ms 待機（複数の ping/pong サイクルをカバー）
	time.Sleep(300 * time.Millisecond)

	// Then: OnDisconnect が呼ばれていない（pong が正常に返り ping timeout しなかった）
	select {
	case <-discCalled:
		t.Fatal("connection was disconnected unexpectedly")
	default:
		// ok: 接続維持
	}
}

func TestConn_PongTimeout_Disconnects(t *testing.T) {
	// Given: 極端に短い pingInterval/pongTimeout で Conn を起動する。
	// クライアントが TCP 接続を強制切断すると ws.Ping がエラーを返し、
	// Conn.Close → Unregister → OnDisconnect が発火する。
	origInterval := pingInterval
	origTimeout := pongTimeout
	pingInterval = 30 * time.Millisecond
	pongTimeout = 50 * time.Millisecond
	defer func() {
		pingInterval = origInterval
		pongTimeout = origTimeout
	}()

	discCalled := make(chan struct{}, 1)
	onDisc := OnDisconnect(func(_ context.Context, _ string) {
		select {
		case discCalled <- struct{}{}:
		default:
		}
	})
	h := New(noopHandler(), onDisc)

	srv := wsTestServer(t, func(serverWS *websocket.Conn) {
		c := NewConn("client-1", serverWS, h)
		h.Register(c)
		go c.Run(context.Background())
	})
	defer srv.Close()

	clientWS := dialClient(t, srv)

	// When: クライアントが TCP を強制切断（CloseNow）する。
	// サーバ側の ws.Ping が接続消滅でエラーを返し pingLoop が Conn.Close を呼ぶ。
	time.Sleep(20 * time.Millisecond)
	clientWS.CloseNow()

	// Then: OnDisconnect が呼ばれる
	select {
	case <-discCalled:
		// ok: ping loop が切断を検知して OnDisconnect を発火した
	case <-time.After(500 * time.Millisecond):
		t.Fatal("OnDisconnect was not called after client disconnect")
	}
}

func TestConn_RegisterReplacesPrevious(t *testing.T) {
	// Given: 同 clientID で2回 Register する
	h := New(noopHandler(), noopOnDisc())

	var (
		connMu   sync.Mutex
		connIdx  int
		oldClosed chan struct{}
	)

	srv := wsTestServer(t, func(serverWS *websocket.Conn) {
		connMu.Lock()
		idx := connIdx
		connIdx++
		connMu.Unlock()

		c := NewConn("client-1", serverWS, h)
		if idx == 0 {
			connMu.Lock()
			oldClosed = c.closed
			connMu.Unlock()
		}
		h.Register(c)
		go c.Run(context.Background())
	})
	defer srv.Close()

	// 1回目の接続
	_ = dialClient(t, srv)
	time.Sleep(50 * time.Millisecond)

	// When: 2回目の接続（同 clientID）で古い接続が差し替えられる
	client2 := dialClient(t, srv)
	defer client2.Close(websocket.StatusNormalClosure, "")

	// Then: 旧接続の closed チャネルが閉じられる
	connMu.Lock()
	ch := oldClosed
	connMu.Unlock()

	select {
	case <-ch:
		// ok
	case <-time.After(500 * time.Millisecond):
		t.Fatal("old connection was not closed after duplicate Register")
	}

	if !h.IsOnline("client-1") {
		t.Fatal("expected client-1 to remain online with new conn")
	}
}
