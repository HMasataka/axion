//go:build integration

package integration_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	clientapp "github.com/HMasataka/axion/internal/client/app"
	serverapp "github.com/HMasataka/axion/internal/server/app"
	"github.com/HMasataka/axion/internal/server/store"
	"github.com/stretchr/testify/require"
)

const i5Bind = "127.0.0.1:18904"

// TestI5_PathTraversalBlocked は listdir で path escape を試みると 400 が返り、
// audit_log に "path_escape_blocked" が記録されることを検証する。
func TestI5_PathTraversalBlocked(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tmp := t.TempDir()
	pskFile := filepath.Join(tmp, "psk")
	must(t, os.WriteFile(pskFile, []byte("test-psk-i5"), 0o600)) //nolint:forbidigo

	serverDataDir := filepath.Join(tmp, "server")
	rootA := filepath.Join(tmp, "rootA")
	must(t, os.MkdirAll(rootA, 0o755)) //nolint:forbidigo

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type hookResult struct {
		srvStore store.Store
	}
	hookCh := make(chan hookResult, 1)

	go func() {
		_ = serverapp.Run(ctx, serverapp.Config{
			Bind:          i5Bind,
			DataDir:       serverDataDir,
			PSKFile:       pskFile,
			ShutdownGrace: 2 * time.Second,
			Hooks: &serverapp.Hooks{
				OnReady: func(h *serverapp.HookContext) {
					hookCh <- hookResult{srvStore: h.Store}
				},
			},
		})
	}()

	var hook hookResult
	select {
	case hook = <-hookCh:
	case <-time.After(5 * time.Second):
		t.Fatal("server OnReady not called in time")
	}

	waitI5ServerReady(t)

	idA := filepath.Join(tmp, "id-a")
	go func() {
		_ = clientapp.Run(ctx, clientapp.Config{
			ServerURL: "ws://" + i5Bind,
			Root:      rootA,
			IDFile:    idA,
			PSKFile:   pskFile,
		})
	}()

	// クライアントが online になるまで待つ。
	clients := waitClientsOnlineI5(t, hook.srvStore, ctx, 1)
	clientID := clients[0].ID

	// GET /v1/clients/{id}/listdir?path=../../etc で path escape を試みる。
	url := "http://" + i5Bind + "/v1/clients/" + clientID + "/listdir?path=../../etc"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer test-psk-i5")

	client := &http.Client{Timeout: 10 * time.Second} //nolint:forbidigo
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// 400 Bad Request が返ることを確認。
	require.Equal(t, http.StatusBadRequest, resp.StatusCode,
		"expected 400 for path traversal attempt, got %d", resp.StatusCode)

	// audit_log に path_escape_blocked が記録されるまで最大 3 秒待つ。
	deadline := time.Now().Add(3 * time.Second)
	var found bool
	for time.Now().Before(deadline) {
		audit, err := hook.srvStore.ListRecentAuditLog(ctx, 50)
		require.NoError(t, err)
		for _, a := range audit {
			if a.Kind == "path_escape_blocked" {
				found = true
				break
			}
		}
		if found {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	require.True(t, found, "expected path_escape_blocked audit entry to be recorded")
}

func waitI5ServerReady(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + i5Bind + "/v1/ws") //nolint:forbidigo,noctx
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("server did not become ready in time")
}

func waitClientsOnlineI5(t *testing.T, srvStore store.Store, ctx context.Context, count int) []store.Client {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		cs, err := srvStore.ListClients(ctx)
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		online := make([]store.Client, 0, count)
		for _, c := range cs {
			if c.Status == "online" {
				online = append(online, c)
			}
		}
		if len(online) >= count {
			return online
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("clients not online in time")
	return nil
}
