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
)

const testBind = "127.0.0.1:18900"

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func waitServerReady(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + testBind + "/v1/ws") //nolint:forbidigo,noctx
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("server did not become ready in time")
}

func waitClientsOnline(t *testing.T, srvStore store.Store, ctx context.Context, count int) []store.Client {
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

func TestI1_OneWayMirror_AtoB(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tmp := t.TempDir()
	pskFile := filepath.Join(tmp, "psk")
	must(t, os.WriteFile(pskFile, []byte("test-psk"), 0o600)) //nolint:forbidigo

	serverDataDir := filepath.Join(tmp, "server")
	rootA := filepath.Join(tmp, "rootA")
	rootB := filepath.Join(tmp, "rootB")
	must(t, os.MkdirAll(rootA, 0o755))  //nolint:forbidigo
	must(t, os.MkdirAll(rootB, 0o755))  //nolint:forbidigo

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// server と内部コンポーネントを取得する channel
	type hookResult struct {
		srvStore store.Store
		engine   interface {
			PublishPairUpdate(ctx context.Context, pairID string) error
		}
	}
	hookCh := make(chan hookResult, 1)

	go func() {
		if err := serverapp.Run(ctx, serverapp.Config{
			Bind:          testBind,
			DataDir:       serverDataDir,
			PSKFile:       pskFile,
			ShutdownGrace: 2 * time.Second,
			Hooks: &serverapp.Hooks{
				OnReady: func(h *serverapp.HookContext) {
					hookCh <- hookResult{srvStore: h.Store, engine: h.Engine}
				},
			},
		}); err != nil {
			// ctx キャンセルによる shutdown は正常
		}
	}()

	// OnReady コールバックを受け取る
	var hook hookResult
	select {
	case hook = <-hookCh:
	case <-time.After(5 * time.Second):
		t.Fatal("server OnReady not called in time")
	}

	// サーバが実際に listen し始めるのを待つ
	waitServerReady(t)

	idA := filepath.Join(tmp, "id-a")
	idB := filepath.Join(tmp, "id-b")

	go func() {
		_ = clientapp.Run(ctx, clientapp.Config{
			ServerURL: "ws://" + testBind,
			Root:      rootA,
			IDFile:    idA,
			PSKFile:   pskFile,
		})
	}()

	go func() {
		_ = clientapp.Run(ctx, clientapp.Config{
			ServerURL: "ws://" + testBind,
			Root:      rootB,
			IDFile:    idB,
			PSKFile:   pskFile,
		})
	}()

	// 両クライアントが online になるのを待つ
	clients := waitClientsOnline(t, hook.srvStore, ctx, 2)

	// rootA / rootB で A/B を区別する（macOS では t.TempDir() が symlink を解決する前のパスを返すため EvalSymlinks で正規化する）。
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

	// mirror ペアを A→B で投入
	pair := store.SyncPair{
		ID:        "pair-i1",
		Name:      "test",
		ClientAID: aID,
		PathA:     ".",
		ClientBID: bID,
		PathB:     ".",
		Direction: "a_to_b",
		Enabled:   true,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	must(t, hook.srvStore.UpsertPair(ctx, pair))

	// SubscribePair を両クライアントに配信
	must(t, hook.engine.PublishPairUpdate(ctx, "pair-i1"))

	// 配信反映を待つ
	time.Sleep(1 * time.Second)

	// A の root に foo.txt を書く
	must(t, os.WriteFile(filepath.Join(rootA, "foo.txt"), []byte("hello v023"), 0o644)) //nolint:forbidigo

	// B の root に foo.txt が出現するまで待つ
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(filepath.Join(rootB, "foo.txt")) //nolint:forbidigo
		if err == nil && string(data) == "hello v023" {
			return // 成功
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("file did not propagate to B in 10s")
}
