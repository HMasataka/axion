package watcher

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// --- helpers ---

func newWatcher(t *testing.T, cfg Config) *Watcher {
	t.Helper()
	w, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return w
}

func startWatcher(t *testing.T, w *Watcher) context.CancelFunc {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	if err := w.Start(ctx); err != nil {
		cancel()
		t.Fatalf("Start: %v", err)
	}
	return cancel
}

// --- debounce tests ---

func TestDebounce_200ms(t *testing.T) {
	root := t.TempDir()
	testFile := filepath.Join(root, "a.txt")
	os.WriteFile(testFile, []byte("v1"), 0644)

	w := newWatcher(t, Config{Root: root})
	cancel := startWatcher(t, w)
	defer cancel()
	defer w.Stop()

	// trigger a single write
	time.Sleep(30 * time.Millisecond)
	os.WriteFile(testFile, []byte("v2"), 0644)

	// 150ms 後はまだ通知が来ないはず（debounce 200ms）
	select {
	case ev := <-w.Events():
		t.Fatalf("event arrived too early (150ms check): %+v", ev)
	case <-time.After(150 * time.Millisecond):
	}

	// さらに 100ms 待つと合計 250ms → 通知到達
	select {
	case ev := <-w.Events():
		if ev.Op != OpWrite {
			t.Fatalf("expected OpWrite, got %v", ev.Op)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("event did not arrive within expected window")
	}
}

func TestDebounce_ResetOnRapidEvents(t *testing.T) {
	root := t.TempDir()
	testFile := filepath.Join(root, "rapid.txt")
	os.WriteFile(testFile, []byte("init"), 0644)

	w := newWatcher(t, Config{Root: root})
	cancel := startWatcher(t, w)
	defer cancel()
	defer w.Stop()

	// 50ms 間隔で 5 回書き込み（各書き込みで debounce がリセットされる）
	time.Sleep(30 * time.Millisecond)
	for i := 0; i < 5; i++ {
		os.WriteFile(testFile, []byte(strings.Repeat("x", i+1)), 0644)
		time.Sleep(50 * time.Millisecond)
	}

	// 最後の書き込みから 200ms 後に 1 件だけ届く
	count := 0
	deadline := time.After(600 * time.Millisecond)
drain:
	for {
		select {
		case <-w.Events():
			count++
		case <-deadline:
			break drain
		}
	}

	if count != 1 {
		t.Fatalf("expected 1 event after debounce, got %d", count)
	}
}

// --- ignore list tests ---

func TestIgnoreList_FromConfig(t *testing.T) {
	root := t.TempDir()

	// create ignored paths
	gitDir := filepath.Join(root, ".git")
	os.Mkdir(gitDir, 0755)
	tmpFile := filepath.Join(root, "temp.tmp")
	os.WriteFile(tmpFile, []byte("x"), 0644)

	w := newWatcher(t, Config{
		Root:       root,
		IgnoreList: []string{".git", "*.tmp"},
	})
	cancel := startWatcher(t, w)
	defer cancel()
	defer w.Stop()

	time.Sleep(30 * time.Millisecond)
	os.WriteFile(filepath.Join(gitDir, "COMMIT_EDITMSG"), []byte("msg"), 0644)
	os.WriteFile(tmpFile, []byte("updated"), 0644)

	select {
	case ev := <-w.Events():
		t.Fatalf("expected no events for ignored paths, got %+v", ev)
	case <-time.After(500 * time.Millisecond):
	}
}

// --- SHA256 / event content tests ---

func TestEvent_OpWrite_HasSHA(t *testing.T) {
	root := t.TempDir()
	testFile := filepath.Join(root, "sha.txt")
	os.WriteFile(testFile, []byte("initial"), 0644)

	w := newWatcher(t, Config{Root: root})
	cancel := startWatcher(t, w)
	defer cancel()
	defer w.Stop()

	time.Sleep(30 * time.Millisecond)
	os.WriteFile(testFile, []byte("content for sha"), 0644)

	select {
	case ev := <-w.Events():
		if ev.Op != OpWrite {
			t.Fatalf("expected OpWrite, got %v", ev.Op)
		}
		if ev.SHA256 == "" {
			t.Fatal("SHA256 should not be empty on OpWrite")
		}
		if ev.Size == 0 {
			t.Fatal("Size should not be zero on OpWrite")
		}
		if ev.ModTime.IsZero() {
			t.Fatal("ModTime should not be zero on OpWrite")
		}
	case <-time.After(600 * time.Millisecond):
		t.Fatal("timeout waiting for write event")
	}
}

func TestEvent_OpRemove(t *testing.T) {
	root := t.TempDir()
	testFile := filepath.Join(root, "remove.txt")
	os.WriteFile(testFile, []byte("bye"), 0644)

	w := newWatcher(t, Config{Root: root})
	cancel := startWatcher(t, w)
	defer cancel()
	defer w.Stop()

	time.Sleep(30 * time.Millisecond)
	os.Remove(testFile)

	select {
	case ev := <-w.Events():
		if ev.Op != OpRemove {
			t.Fatalf("expected OpRemove, got %v", ev.Op)
		}
		if ev.RelPath != "remove.txt" {
			t.Fatalf("expected RelPath=remove.txt, got %s", ev.RelPath)
		}
	case <-time.After(600 * time.Millisecond):
		t.Fatal("timeout waiting for remove event")
	}
}

// --- custom Opener test ---

type mockOpener struct {
	inner    Opener
	callCount atomic.Int64
}

func (m *mockOpener) Open(rel string) (io.ReadCloser, error) {
	m.callCount.Add(1)
	return m.inner.Open(rel)
}

func TestOpener_Custom(t *testing.T) {
	root := t.TempDir()
	testFile := filepath.Join(root, "custom.txt")
	os.WriteFile(testFile, []byte("hello opener"), 0644)

	mock := &mockOpener{inner: fsBackedFS{}}
	w := newWatcher(t, Config{
		Root:   root,
		Opener: mock,
	})
	cancel := startWatcher(t, w)
	defer cancel()
	defer w.Stop()

	time.Sleep(30 * time.Millisecond)
	os.WriteFile(testFile, []byte("updated via opener"), 0644)

	select {
	case ev := <-w.Events():
		if ev.Op != OpWrite {
			t.Fatalf("expected OpWrite, got %v", ev.Op)
		}
	case <-time.After(600 * time.Millisecond):
		t.Fatal("timeout waiting for write event")
	}

	if mock.callCount.Load() == 0 {
		t.Fatal("custom Opener.Open was never called")
	}
}
