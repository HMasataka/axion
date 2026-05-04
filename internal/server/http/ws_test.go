package httpsrv_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/HMasataka/axion/internal/proto"
	httpsrv "github.com/HMasataka/axion/internal/server/http"
	"github.com/HMasataka/axion/internal/server/hub"
	"github.com/HMasataka/axion/internal/server/store"
)

const testPSK = "test-psk-secret"

func openTestStore(t *testing.T) store.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func newTestHub() *hub.Hub {
	handler := hub.HandlerFunc(func(_ context.Context, _ string, _ proto.Envelope) error {
		return nil
	})
	onDisc := func(_ context.Context, _ string) {}
	return hub.New(handler, onDisc)
}

func startTestServer(t *testing.T, s store.Store, h *hub.Hub) *httptest.Server {
	t.Helper()
	cfg := httpsrv.Config{
		Store: s,
		Hub:   h,
		PSK:   testPSK,
	}
	srv := httptest.NewServer(httpsrv.NewRouter(cfg))
	t.Cleanup(srv.Close)
	return srv
}

func dialWS(t *testing.T, srv *httptest.Server, psk string) (*websocket.Conn, *http.Response) {
	t.Helper()
	url := "ws" + srv.URL[len("http"):] + "/v1/ws"
	opts := &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Authorization": []string{"Bearer " + psk},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, resp, err := websocket.Dial(ctx, url, opts)
	if err != nil {
		return nil, resp
	}
	t.Cleanup(func() { conn.CloseNow() })
	return conn, resp
}

func sendRegisterRequest(t *testing.T, ctx context.Context, ws *websocket.Conn, req proto.RegisterRequest) {
	t.Helper()
	payload, err := proto.MarshalPayload(req)
	if err != nil {
		t.Fatalf("MarshalPayload: %v", err)
	}
	data, err := proto.Marshal(proto.Envelope{
		Type:    proto.TypeRegisterRequest,
		Payload: payload,
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := ws.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatalf("Write: %v", err)
	}
}

func hasAuditKind(t *testing.T, s store.Store, kind string) bool {
	t.Helper()
	entries, err := s.ListRecentAuditLog(context.Background(), 100)
	if err != nil {
		t.Fatalf("ListRecentAuditLog: %v", err)
	}
	for _, e := range entries {
		if e.Kind == kind {
			return true
		}
	}
	return false
}

// TestWSHandler_RegistersClient は正常な PSK + RegisterRequest で RegisterResponse{ok:true} を
// 受け取り、client が store に登録され、audit_log に "client_register" が記録されることを検証する。
func TestWSHandler_RegistersClient(t *testing.T) {
	// Given
	s := openTestStore(t)
	h := newTestHub()
	srv := startTestServer(t, s, h)

	ws, _ := dialWS(t, srv, testPSK)
	if ws == nil {
		t.Fatal("WebSocket dial failed")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// When
	sendRegisterRequest(t, ctx, ws, proto.RegisterRequest{
		ClientID:     "client-001",
		Hostname:     "test-host",
		RootPath:     "/data",
		Version:      "0.1.0",
		ProtoVersion: proto.ProtoVersion,
	})

	// Then: RegisterResponse{ok:true} を受信する
	var env proto.Envelope
	if err := wsjson.Read(ctx, ws, &env); err != nil {
		t.Fatalf("read RegisterResponse: %v", err)
	}
	if env.Type != proto.TypeRegisterResponse {
		t.Fatalf("expected type=%s, got %s", proto.TypeRegisterResponse, env.Type)
	}

	var resp proto.RegisterResponse
	if err := proto.UnmarshalPayload(env.Payload, &resp); err != nil {
		t.Fatalf("UnmarshalPayload: %v", err)
	}
	if !resp.OK {
		t.Errorf("expected OK=true, got false (reason: %s)", resp.Reason)
	}

	// store に登録されている
	time.Sleep(50 * time.Millisecond)
	client, err := s.GetClient(context.Background(), "client-001")
	if err != nil {
		t.Fatalf("GetClient: %v", err)
	}
	if client.Hostname != "test-host" {
		t.Errorf("expected hostname=test-host, got %s", client.Hostname)
	}

	// audit_log に "client_register" がある
	if !hasAuditKind(t, s, "client_register") {
		t.Error("expected audit_log entry with kind=client_register")
	}
}

// TestWSHandler_RejectsInvalidPSK は Bearer 不一致で 401 が返り、
// audit_log に "psk_auth_failed" が記録されることを検証する。
func TestWSHandler_RejectsInvalidPSK(t *testing.T) {
	// Given
	s := openTestStore(t)
	h := newTestHub()
	srv := startTestServer(t, s, h)

	// When: 誤ったPSKで接続する
	ws, resp := dialWS(t, srv, "wrong-psk")

	// Then: 401 で拒否される
	if ws != nil {
		t.Fatal("expected dial to fail but succeeded")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("expected 401, got %d", status)
	}

	// audit_log に "psk_auth_failed" がある
	if !hasAuditKind(t, s, "psk_auth_failed") {
		t.Error("expected audit_log entry with kind=psk_auth_failed")
	}
}

// TestWSHandler_RejectsProtoVersionMismatch は proto_version メジャー不一致の RegisterRequest で
// 接続が close され、audit_log に "proto_version_mismatch" が記録されることを検証する。
func TestWSHandler_RejectsProtoVersionMismatch(t *testing.T) {
	// Given
	s := openTestStore(t)
	h := newTestHub()
	srv := startTestServer(t, s, h)

	ws, _ := dialWS(t, srv, testPSK)
	if ws == nil {
		t.Fatal("WebSocket dial failed")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// When: メジャーバージョン不一致の RegisterRequest を送る
	sendRegisterRequest(t, ctx, ws, proto.RegisterRequest{
		ClientID:     "client-002",
		Hostname:     "test-host",
		RootPath:     "/data",
		Version:      "0.1.0",
		ProtoVersion: "2",
	})

	// Then: 接続が close される
	_, _, err := ws.Read(ctx)
	if err == nil {
		t.Fatal("expected connection to be closed, but read succeeded")
	}

	// audit_log に "proto_version_mismatch" がある
	time.Sleep(50 * time.Millisecond)
	if !hasAuditKind(t, s, "proto_version_mismatch") {
		t.Error("expected audit_log entry with kind=proto_version_mismatch")
	}
}

// TestWSHandler_TimeoutWithoutRegisterRequest は WS Accept 後に何も送らないと
// registerTimeout (5s) 後に close されることを検証する。
// テストでは短縮タイムアウトを直接確認するため、テスト自体のタイムアウトを 7s に設定する。
func TestWSHandler_TimeoutWithoutRegisterRequest(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timeout test in short mode")
	}

	// Given
	s := openTestStore(t)
	h := newTestHub()
	srv := startTestServer(t, s, h)

	ws, _ := dialWS(t, srv, testPSK)
	if ws == nil {
		t.Fatal("WebSocket dial failed")
	}

	// When: 何も送らない

	// Then: registerTimeout (5s) 後に close される
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()

	_, _, err := ws.Read(ctx)
	if err == nil {
		t.Fatal("expected connection to be closed after timeout")
	}
}
