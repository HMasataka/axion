package conn_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/HMasataka/axion/internal/client/conn"
	"github.com/HMasataka/axion/internal/proto"
)

// makeTestServer は RegisterRequest を受け取り RegisterResponse を返す WS テストサーバーを返す。
// afterN 回接続後に onNthConn が呼ばれる。
func makeTestServer(t *testing.T, settings map[string]string, closeAfterRegister bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer ws.CloseNow()

		ctx := r.Context()

		var env proto.Envelope
		if err := wsjson.Read(ctx, ws, &env); err != nil {
			return
		}

		if env.Type != proto.TypeRegisterRequest {
			ws.Close(websocket.StatusPolicyViolation, "expected register_request")
			return
		}

		payload, _ := json.Marshal(proto.RegisterResponse{
			OK:       true,
			Settings: settings,
		})

		resp := proto.Envelope{
			Type:    proto.TypeRegisterResponse,
			Payload: payload,
		}
		if err := wsjson.Write(ctx, ws, resp); err != nil {
			return
		}

		if closeAfterRegister {
			ws.Close(websocket.StatusNormalClosure, "test close")
			return
		}

		// Hold connection open until context cancels
		<-ctx.Done()
	}))
}

func TestConn_RegistersOnConnect(t *testing.T) {
	settings := map[string]string{"ignore_list": "*.tmp"}
	srv := makeTestServer(t, settings, false)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	cfg := conn.Config{
		ServerURL: wsURL,
		PSK:       "test-psk",
		ClientID:  "client-1",
		Hostname:  "host-1",
		RootPath:  "/tmp",
		Version:   "0.2.1",
	}

	c := conn.New(cfg)

	connectedCh := make(chan map[string]string, 1)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		c.Run(ctx, func(s map[string]string) {
			connectedCh <- s
			cancel()
		}, func(env proto.Envelope) error {
			return nil
		})
	}()

	select {
	case got := <-connectedCh:
		if got["ignore_list"] != settings["ignore_list"] {
			t.Fatalf("settings mismatch: got %v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for onConnected")
	}
}

func TestConn_ReconnectsOnDrop(t *testing.T) {
	settings := map[string]string{}
	var connCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer ws.CloseNow()

		ctx := r.Context()

		var env proto.Envelope
		if err := wsjson.Read(ctx, ws, &env); err != nil {
			return
		}

		payload, _ := json.Marshal(proto.RegisterResponse{
			OK:       true,
			Settings: settings,
		})
		resp := proto.Envelope{
			Type:    proto.TypeRegisterResponse,
			Payload: payload,
		}
		if err := wsjson.Write(ctx, ws, resp); err != nil {
			return
		}

		n := connCount.Add(1)
		// First connection: drop immediately to trigger reconnect
		if n == 1 {
			ws.Close(websocket.StatusNormalClosure, "test drop")
			return
		}
		// Second connection: hold until ctx done
		<-ctx.Done()
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	cfg := conn.Config{
		ServerURL: wsURL,
		PSK:       "test-psk",
		ClientID:  "client-2",
		Hostname:  "host-2",
		RootPath:  "/tmp",
		Version:   "0.2.1",
	}

	c := conn.New(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	connectedCount := make(chan struct{}, 10)

	go func() {
		c.Run(ctx, func(_ map[string]string) {
			connectedCount <- struct{}{}
			if connCount.Load() >= 2 {
				cancel()
			}
		}, func(env proto.Envelope) error {
			return nil
		})
	}()

	// Wait for at least 2 connections (initial + reconnect)
	count := 0
	deadline := time.After(10 * time.Second)
	for count < 2 {
		select {
		case <-connectedCount:
			count++
		case <-deadline:
			t.Fatalf("reconnect did not happen within timeout, got %d connections", count)
		}
	}
}
