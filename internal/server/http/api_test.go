package httpsrv_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/HMasataka/axion/internal/proto"
)

// connectFakeClient はテスト用 WS クライアントを接続し RegisterRequest を送り登録する。
// 返された *websocket.Conn でサーバ側への応答を制御できる。
func connectFakeClient(t *testing.T, srv *httptest.Server, clientID string) *websocket.Conn {
	t.Helper()
	ws, _ := dialWS(t, srv, testPSK)
	if ws == nil {
		t.Fatal("WebSocket dial failed")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	sendRegisterRequest(t, ctx, ws, proto.RegisterRequest{
		ClientID:     clientID,
		Hostname:     "test-host",
		RootPath:     "/data",
		Version:      "0.1.0",
		ProtoVersion: proto.ProtoVersion,
	})

	// RegisterResponse を受信する
	var env proto.Envelope
	if err := wsjson.Read(ctx, ws, &env); err != nil {
		t.Fatalf("read RegisterResponse: %v", err)
	}
	if env.Type != proto.TypeRegisterResponse {
		t.Fatalf("expected RegisterResponse, got %s", env.Type)
	}
	return ws
}

// respondWithListDir は WS 接続で受信した ListDirRequest に resp を返す。
func respondWithListDir(ctx context.Context, ws *websocket.Conn, resp proto.ListDirResponse) {
	var env proto.Envelope
	if err := wsjson.Read(ctx, ws, &env); err != nil {
		return
	}
	if env.Type != proto.TypeListDirRequest {
		return
	}
	payload, _ := proto.MarshalPayload(resp)
	respEnv := proto.Envelope{
		Type:          proto.TypeListDirResponse,
		CorrelationID: env.CorrelationID,
		Payload:       payload,
	}
	_ = wsjson.Write(ctx, ws, respEnv)
}

func doListDirHTTP(t *testing.T, srv *httptest.Server, clientID, path string) *http.Response {
	t.Helper()
	url := srv.URL + "/v1/clients/" + clientID + "/listdir"
	if path != "" {
		url += "?path=" + path
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+testPSK)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// TestListDir_OnlineClient_Returns200 はオンラインクライアントから ListDirResponse を受け取り 200 を返す。
func TestListDir_OnlineClient_Returns200(t *testing.T) {
	// Given: WS クライアントが接続・登録済みのサーバー
	s := openTestStore(t)
	h := newTestHub()
	srv := startTestServer(t, s, h)

	clientID := "client-listdir-ok"
	ws := connectFakeClient(t, srv, clientID)

	fakeResp := proto.ListDirResponse{
		Entries: []proto.DirEntry{
			{Name: "file.txt", IsDir: false, Size: 100},
		},
	}

	// 別 goroutine でクライアント側の応答をシミュレート
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	go respondWithListDir(ctx, ws, fakeResp)

	// 少し待って Hub に登録されるのを待つ
	time.Sleep(50 * time.Millisecond)

	// When: GET /v1/clients/{id}/listdir
	resp := doListDirHTTP(t, srv, clientID, ".")

	// Then: 200 + JSON body
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body proto.ListDirResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(body.Entries))
	}
	if body.Entries[0].Name != "file.txt" {
		t.Errorf("expected name=file.txt, got %s", body.Entries[0].Name)
	}
}

// TestListDir_OfflineClient_Returns503 はオフラインクライアントに対して 503 を返す。
func TestListDir_OfflineClient_Returns503(t *testing.T) {
	// Given: 空の Hub (クライアント未接続)
	s := openTestStore(t)
	h := newTestHub()
	srv := startTestServer(t, s, h)

	// When: オフラインのクライアント ID を指定
	resp := doListDirHTTP(t, srv, "offline-client", ".")

	// Then: 503
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", resp.StatusCode)
	}
}

// TestListDir_Timeout_Returns504 はクライアントが返答しないシナリオで 504 を返す。
func TestListDir_Timeout_Returns504(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timeout test in short mode")
	}

	// Given: WS クライアントが接続済みだが応答しない
	s := openTestStore(t)
	h := newTestHub()
	srv := startTestServer(t, s, h)

	clientID := "client-no-respond"
	connectFakeClient(t, srv, clientID)
	// 応答 goroutine を起動しない

	time.Sleep(50 * time.Millisecond)

	// When: GET /v1/clients/{id}/listdir (5s タイムアウト待ち)
	resp := doListDirHTTP(t, srv, clientID, ".")

	// Then: 504
	if resp.StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("expected 504, got %d", resp.StatusCode)
	}
}

// TestListDir_PathEscape_Returns400_AndAudits はパスエスケープ時に 400 + audit_log を返す。
func TestListDir_PathEscape_Returns400_AndAudits(t *testing.T) {
	// Given: クライアントが path escape エラーを返す
	s := openTestStore(t)
	h := newTestHub()
	srv := startTestServer(t, s, h)

	clientID := "client-escape"
	ws := connectFakeClient(t, srv, clientID)

	escapeResp := proto.ListDirResponse{Error: "path escape: ../../etc"}

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	go respondWithListDir(ctx, ws, escapeResp)

	time.Sleep(50 * time.Millisecond)

	// When: path=../../etc で GET
	resp := doListDirHTTP(t, srv, clientID, "../../etc")

	// Then: 400
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}

	// audit_log に path_escape_blocked がある
	time.Sleep(50 * time.Millisecond)
	if !hasAuditKind(t, s, "path_escape_blocked") {
		t.Error("expected audit_log entry with kind=path_escape_blocked")
	}
}
