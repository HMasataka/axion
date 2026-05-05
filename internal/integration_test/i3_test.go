//go:build integration

package integration_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	clientapp "github.com/HMasataka/axion/internal/client/app"
	serverapp "github.com/HMasataka/axion/internal/server/app"
	"github.com/HMasataka/axion/internal/server/store"
	"github.com/stretchr/testify/require"
)

func TestI3_BidirectionalConflict_RenamesLoser(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tmp := t.TempDir()
	pskFile := filepath.Join(tmp, "psk")
	must(t, os.WriteFile(pskFile, []byte("test-psk-i3"), 0o600)) //nolint:forbidigo

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

	const i3Bind = "127.0.0.1:18901"

	go func() {
		if err := serverapp.Run(ctx, serverapp.Config{
			Bind:          i3Bind,
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

	var hook hookResult
	select {
	case hook = <-hookCh:
	case <-time.After(5 * time.Second):
		t.Fatal("server OnReady not called in time")
	}

	waitI3ServerReady(t, i3Bind)

	idA := filepath.Join(tmp, "id-a")
	idB := filepath.Join(tmp, "id-b")

	go func() {
		_ = clientapp.Run(ctx, clientapp.Config{
			ServerURL: "ws://" + i3Bind,
			Root:      rootA,
			IDFile:    idA,
			PSKFile:   pskFile,
		})
	}()

	go func() {
		_ = clientapp.Run(ctx, clientapp.Config{
			ServerURL: "ws://" + i3Bind,
			Root:      rootB,
			IDFile:    idB,
			PSKFile:   pskFile,
		})
	}()

	clients := waitClientsOnlineAt(t, hook.srvStore, ctx, 2)

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

	pair := store.SyncPair{
		ID:        "p1",
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
	must(t, hook.engine.PublishPairUpdate(ctx, "p1"))

	time.Sleep(1 * time.Second)

	// 同時書き込みで conflict を発生させる
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		must(t, os.WriteFile(filepath.Join(rootA, "foo.txt"), []byte("from-A"), 0o644)) //nolint:forbidigo
	}()
	go func() {
		defer wg.Done()
		must(t, os.WriteFile(filepath.Join(rootB, "foo.txt"), []byte("from-B"), 0o644)) //nolint:forbidigo
	}()
	wg.Wait()

	// LWW + rename + fetch + propagate の往復を待つ (最大 10 秒ポーリング)
	var aData, bData []byte
	convergeDeadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(convergeDeadline) {
		ad, errA := os.ReadFile(filepath.Join(rootA, "foo.txt")) //nolint:forbidigo
		bd, errB := os.ReadFile(filepath.Join(rootB, "foo.txt")) //nolint:forbidigo
		if errA == nil && errB == nil && string(ad) == string(bd) {
			aData = ad
			bData = bd
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	// 検証 1: 両側の foo.txt が一致 (勝者の内容が両側に伝播している)
	require.NotNil(t, aData, "rootA/foo.txt should be readable")
	require.NotNil(t, bData, "rootB/foo.txt should be readable")
	require.Equal(t, string(aData), string(bData), "winner should be propagated to both sides")

	// 検証 2: 片方に conflict ファイルが存在する
	confA, _ := filepath.Glob(filepath.Join(rootA, "foo.txt.conflict-*"))
	confB, _ := filepath.Glob(filepath.Join(rootB, "foo.txt.conflict-*"))
	totalConflicts := len(confA) + len(confB)
	require.GreaterOrEqual(t, totalConflicts, 1, "expected at least 1 conflict file across A and B")

	// 検証 3: audit_log に conflict_detected と conflict_renamed が各 1 件以上
	audit, err := hook.srvStore.ListRecentAuditLog(ctx, 100)
	require.NoError(t, err)
	var detected, renamed int
	for _, a := range audit {
		if a.Kind == "conflict_detected" {
			detected++
		}
		if a.Kind == "conflict_renamed" {
			renamed++
		}
	}
	require.GreaterOrEqual(t, detected, 1, "expected at least 1 conflict_detected audit entry")
	require.GreaterOrEqual(t, renamed, 1, "expected at least 1 conflict_renamed audit entry")
}

func waitI3ServerReady(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + addr + "/v1/ws") //nolint:forbidigo,noctx
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("server did not become ready in time")
}

func waitClientsOnlineAt(t *testing.T, srvStore store.Store, ctx context.Context, count int) []store.Client {
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
