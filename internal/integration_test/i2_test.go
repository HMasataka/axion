//go:build integration

package integration_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	clientapp "github.com/HMasataka/axion/internal/client/app"
	serverapp "github.com/HMasataka/axion/internal/server/app"
	"github.com/HMasataka/axion/internal/server/store"
)

const i2Bind = "127.0.0.1:18902"

// TestI2_DiffScanCatchesUpAfterReconnect は、クライアント B がオフライン中に A で行われた
// 5 ファイルの変更が、B 再接続後に正しくキャッチアップされることを検証する。
func TestI2_DiffScanCatchesUpAfterReconnect(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tmp := t.TempDir()
	pskFile := filepath.Join(tmp, "psk")
	must(t, os.WriteFile(pskFile, []byte("test-psk-i2"), 0o600)) //nolint:forbidigo

	serverDataDir := filepath.Join(tmp, "server")
	rootA := filepath.Join(tmp, "rootA")
	rootB := filepath.Join(tmp, "rootB")
	must(t, os.MkdirAll(rootA, 0o755)) //nolint:forbidigo
	must(t, os.MkdirAll(rootB, 0o755)) //nolint:forbidigo

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type hookResult struct {
		srvStore store.Store
		engine   interface {
			PublishPairUpdate(ctx context.Context, pairID string) error
		}
	}
	hookCh := make(chan hookResult, 1)

	go func() {
		_ = serverapp.Run(ctx, serverapp.Config{
			Bind:          i2Bind,
			DataDir:       serverDataDir,
			PSKFile:       pskFile,
			ShutdownGrace: 2 * time.Second,
			Hooks: &serverapp.Hooks{
				OnReady: func(h *serverapp.HookContext) {
					hookCh <- hookResult{srvStore: h.Store, engine: h.Engine}
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

	waitI2ServerReady(t)

	idA := filepath.Join(tmp, "id-a")
	idB := filepath.Join(tmp, "id-b")

	// クライアント A は ctx (テスト全体) で動かし続ける。
	go func() {
		_ = clientapp.Run(ctx, clientapp.Config{
			ServerURL: "ws://" + i2Bind,
			Root:      rootA,
			IDFile:    idA,
			PSKFile:   pskFile,
		})
	}()

	// クライアント B は最初だけ起動して後で止める。
	ctxB, cancelB := context.WithCancel(ctx)
	go func() {
		_ = clientapp.Run(ctxB, clientapp.Config{
			ServerURL: "ws://" + i2Bind,
			Root:      rootB,
			IDFile:    idB,
			PSKFile:   pskFile,
		})
	}()

	// 両クライアントが online になるのを待つ。
	clients := waitClientsOnlineI2(t, hook.srvStore, ctx, 2)

	realRootA, err := filepath.EvalSymlinks(rootA)
	must(t, err)
	realRootB, err := filepath.EvalSymlinks(rootB)
	must(t, err)

	var aID, bID string
	for _, c := range clients {
		switch c.RootPath {
		case realRootA:
			aID = c.ID
		case realRootB:
			bID = c.ID
		}
	}
	if aID == "" || bID == "" {
		t.Fatalf("could not identify clients by root path: clients=%v rootA=%s rootB=%s", clients, realRootA, realRootB)
	}

	// bidirectional ペア作成・配信。
	pair := store.SyncPair{
		ID:        "pair-i2",
		Name:      "test",
		ClientAID: aID,
		PathA:     ".",
		ClientBID: bID,
		PathB:     ".",
		Direction: "bidirectional",
		Enabled:   true,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	must(t, hook.srvStore.UpsertPair(ctx, pair))
	must(t, hook.engine.PublishPairUpdate(ctx, "pair-i2"))

	// 初期同期が安定するまで待つ。
	time.Sleep(1 * time.Second)

	// 1 ファイルを A→B 伝播させて動作確認。
	must(t, os.WriteFile(filepath.Join(rootA, "ping.txt"), []byte("ping"), 0o644)) //nolint:forbidigo
	pingDeadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(pingDeadline) {
		if _, err := os.ReadFile(filepath.Join(rootB, "ping.txt")); err == nil { //nolint:forbidigo
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if _, err := os.ReadFile(filepath.Join(rootB, "ping.txt")); err != nil { //nolint:forbidigo
		t.Fatal("initial sync did not work: ping.txt did not appear in B")
	}

	// B を停止する。
	cancelB()
	// B がオフラインになるまで待つ。
	waitClientOfflineI2(t, hook.srvStore, ctx, bID)

	// A 側で 5 ファイル変更。
	for i := range 5 {
		name := fmt.Sprintf("catch%d.txt", i)
		content := fmt.Sprintf("content-%d", i)
		must(t, os.WriteFile(filepath.Join(rootA, name), []byte(content), 0o644)) //nolint:forbidigo
	}

	// B クライアントを再起動（idB ファイルを再利用して同じ ClientID を使う）。
	ctxB2, cancelB2 := context.WithCancel(ctx)
	defer cancelB2()
	go func() {
		_ = clientapp.Run(ctxB2, clientapp.Config{
			ServerURL: "ws://" + i2Bind,
			Root:      rootB,
			IDFile:    idB,
			PSKFile:   pskFile,
		})
	}()

	// 5 秒以内に B 側に 5 ファイル全部出現することを確認。
	catchDeadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(catchDeadline) {
		allPresent := true
		for i := range 5 {
			name := fmt.Sprintf("catch%d.txt", i)
			expected := fmt.Sprintf("content-%d", i)
			data, err := os.ReadFile(filepath.Join(rootB, name)) //nolint:forbidigo
			if err != nil || string(data) != expected {
				allPresent = false
				break
			}
		}
		if allPresent {
			return // 成功
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("B did not catch up with 5 files within 15s after reconnect")
}

func waitI2ServerReady(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + i2Bind + "/v1/ws") //nolint:forbidigo,noctx
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("server did not become ready in time")
}

func waitClientsOnlineI2(t *testing.T, srvStore store.Store, ctx context.Context, count int) []store.Client {
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

func waitClientOfflineI2(t *testing.T, srvStore store.Store, ctx context.Context, clientID string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		c, err := srvStore.GetClient(ctx, clientID)
		if err == nil && c != nil && c.Status == "offline" {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("client B did not go offline in time")
}
