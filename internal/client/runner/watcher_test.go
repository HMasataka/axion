package runner

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/HMasataka/axion/internal/clientfs"
	"github.com/HMasataka/axion/internal/proto"
)

func newTestJail(t *testing.T) *clientfs.Jail {
	t.Helper()
	root := t.TempDir()
	j, err := clientfs.New(root)
	if err != nil {
		t.Fatalf("clientfs.New: %v", err)
	}
	return j
}

func startWatcherRunner(t *testing.T, cfg WatchConfig) *WatcherRunner {
	t.Helper()
	wr, err := NewWatcherRunner(cfg)
	if err != nil {
		t.Fatalf("NewWatcherRunner: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := wr.Start(ctx); err != nil {
		cancel()
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		wr.Stop() //nolint:errcheck
	})
	return wr
}

func subscribeRootAll(r *Registry, pairID, side string) {
	r.Apply(proto.SubscribePair{
		PairID:      pairID,
		Side:        side,
		RootSubpath: "",
		Direction:   "bidirectional",
	})
}

func TestWatcherRunner_GeneratesEvent(t *testing.T) {
	// Given: subscription 1 件登録
	j := newTestJail(t)
	reg := NewRegistry()
	subscribeRootAll(reg, "pair-1", "a")

	wr := startWatcherRunner(t, WatchConfig{Jail: j, Registry: reg})

	// When: jail root にファイル作成
	time.Sleep(30 * time.Millisecond)
	filePath := filepath.Join(j.Root(), "hello.txt")
	if err := os.WriteFile(filePath, []byte("content"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Then: 600ms 以内に FileChangedEvent 受信、SHA256/Size 設定済み
	select {
	case ev := <-wr.Events():
		if ev.PairID != "pair-1" {
			t.Fatalf("expected pair-1, got %s", ev.PairID)
		}
		if ev.Op != "write" {
			t.Fatalf("expected op=write, got %s", ev.Op)
		}
		if ev.SHA256 == "" {
			t.Fatal("expected SHA256 to be set")
		}
		if ev.Size == 0 {
			t.Fatal("expected Size > 0")
		}
	case <-time.After(600 * time.Millisecond):
		t.Fatal("timeout waiting for FileChangedEvent")
	}
}

func TestWatcherRunner_NoSubscription_Suppressed(t *testing.T) {
	// Given: subscription なし
	j := newTestJail(t)
	reg := NewRegistry()

	wr := startWatcherRunner(t, WatchConfig{Jail: j, Registry: reg})

	// When: ファイル作成
	time.Sleep(30 * time.Millisecond)
	filePath := filepath.Join(j.Root(), "ignored.txt")
	if err := os.WriteFile(filePath, []byte("data"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Then: イベントなし
	select {
	case ev := <-wr.Events():
		t.Fatalf("expected no event, got %+v", ev)
	case <-time.After(600 * time.Millisecond):
	}
}

func TestWatcherRunner_OpRemove(t *testing.T) {
	// Given: ファイル作成済み + subscription 1 件
	j := newTestJail(t)
	filePath := filepath.Join(j.Root(), "remove.txt")
	if err := os.WriteFile(filePath, []byte("bye"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	reg := NewRegistry()
	subscribeRootAll(reg, "pair-1", "a")

	wr := startWatcherRunner(t, WatchConfig{Jail: j, Registry: reg})

	// When: ファイル削除
	time.Sleep(30 * time.Millisecond)
	if err := os.Remove(filePath); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	// Then: op="delete" のイベント受信 (write イベントが先行する場合はスキップ)
	deadline := time.After(600 * time.Millisecond)
	for {
		select {
		case ev := <-wr.Events():
			if ev.Op == "delete" {
				return
			}
		case <-deadline:
			t.Fatal("timeout waiting for delete event")
		}
	}
}

func TestWatcherRunner_MultipleSubscriptions(t *testing.T) {
	// Given: 2 ペアが同じ RootSubpath="" で登録
	j := newTestJail(t)
	reg := NewRegistry()
	subscribeRootAll(reg, "pair-ab", "a")
	subscribeRootAll(reg, "pair-ac", "a")

	wr := startWatcherRunner(t, WatchConfig{Jail: j, Registry: reg})

	// When: 1ファイル作成
	time.Sleep(30 * time.Millisecond)
	filePath := filepath.Join(j.Root(), "shared.txt")
	if err := os.WriteFile(filePath, []byte("shared content"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Then: 2件の FileChangedEvent が届く
	received := make(map[string]bool)
	deadline := time.After(600 * time.Millisecond)
	for len(received) < 2 {
		select {
		case ev := <-wr.Events():
			received[ev.PairID] = true
		case <-deadline:
			keys := sortedKeys(received)
			t.Fatalf("timeout: received events for pairs %v, want pair-ab and pair-ac", keys)
		}
	}
	if !received["pair-ab"] || !received["pair-ac"] {
		t.Fatalf("expected events for pair-ab and pair-ac, got %v", sortedKeys(received))
	}
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
