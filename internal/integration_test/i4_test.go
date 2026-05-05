//go:build integration

package integration_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	serverapp "github.com/HMasataka/axion/internal/server/app"
	"github.com/HMasataka/axion/internal/server/store"
	"github.com/stretchr/testify/require"
)

const i4Bind = "127.0.0.1:18903"

// TestI4_PSKMismatch_RejectsAndAudits は PSK 不一致の WS 接続試行が拒否され、
// audit_log に "psk_auth_failed" が記録されることを検証する。
func TestI4_PSKMismatch_RejectsAndAudits(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tmp := t.TempDir()
	pskFile := filepath.Join(tmp, "psk")
	must(t, os.WriteFile(pskFile, []byte("correct-psk"), 0o600)) //nolint:forbidigo

	serverDataDir := filepath.Join(tmp, "server")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type hookResult struct {
		srvStore store.Store
	}
	hookCh := make(chan hookResult, 1)

	go func() {
		_ = serverapp.Run(ctx, serverapp.Config{
			Bind:          i4Bind,
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

	waitI4ServerReady(t)

	// PSK が "wrong-psk" の WS 接続試行。
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+i4Bind+"/v1/ws", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer wrong-psk")

	client := &http.Client{} //nolint:forbidigo
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// 401 Unauthorized が返ることを確認。
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode, "expected 401 for wrong PSK")

	// audit_log に psk_auth_failed が記録されるまで最大 3 秒待つ。
	deadline := time.Now().Add(3 * time.Second)
	var found bool
	for time.Now().Before(deadline) {
		audit, err := hook.srvStore.ListRecentAuditLog(ctx, 50)
		require.NoError(t, err)
		for _, a := range audit {
			if a.Kind == "psk_auth_failed" {
				found = true
				break
			}
		}
		if found {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	require.True(t, found, "expected psk_auth_failed audit entry to be recorded")
}

func waitI4ServerReady(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + i4Bind + "/v1/ws") //nolint:forbidigo,noctx
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("server did not become ready in time")
}
