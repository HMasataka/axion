package syncengine

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestScheduler_AcquireRelease_Basic(t *testing.T) {
	// Given: デフォルト設定の Scheduler
	s := NewScheduler()
	key := JobKey{ClientID: "c1", PairID: "p1", RelPath: "foo.txt"}

	// When: 1件取得して解放する
	release, err := s.Acquire(context.Background(), key)

	// Then: エラーなく取得でき、解放も正常終了する
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	release()
}

func TestScheduler_GlobalLimit_64(t *testing.T) {
	// Given: global limit=64 の Scheduler
	s := NewSchedulerWithLimits(64, 64)
	releases := make([]func(), 64)

	for i := range 64 {
		key := JobKey{ClientID: "c1", PairID: "p1", RelPath: string(rune('a' + i%26)) + string(rune('0'+i/26))}
		release, err := s.Acquire(context.Background(), key)
		if err != nil {
			t.Fatalf("slot %d: unexpected error: %v", i, err)
		}
		releases[i] = release
	}

	// When: 65件目を context timeout 付きで取得しようとする
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	key65 := JobKey{ClientID: "c2", PairID: "p2", RelPath: "extra.txt"}
	_, err := s.Acquire(ctx, key65)

	// Then: ErrCanceled が返る（global がブロックされる）
	if !errors.Is(err, ErrCanceled) {
		t.Fatalf("expected ErrCanceled, got %v", err)
	}

	for _, r := range releases {
		r()
	}
}

func TestScheduler_PerClientLimit_4(t *testing.T) {
	// Given: per-client limit=2 の Scheduler（小さい値でテスト）
	s := NewSchedulerWithLimits(64, 2)
	clientA := "clientA"

	releaseA1, err := s.Acquire(context.Background(), JobKey{ClientID: clientA, PairID: "p1", RelPath: "a.txt"})
	if err != nil {
		t.Fatalf("slot 1: %v", err)
	}
	releaseA2, err := s.Acquire(context.Background(), JobKey{ClientID: clientA, PairID: "p1", RelPath: "b.txt"})
	if err != nil {
		t.Fatalf("slot 2: %v", err)
	}

	// When: 同 clientA で 3件目（上限超え）を取得しようとする
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err = s.Acquire(ctx, JobKey{ClientID: clientA, PairID: "p1", RelPath: "c.txt"})

	// Then: ErrCanceled が返る（per-client がブロックされる）
	if !errors.Is(err, ErrCanceled) {
		t.Fatalf("expected ErrCanceled for clientA 3rd slot, got %v", err)
	}

	// When: 別 client で取得する
	releaseB, err := s.Acquire(context.Background(), JobKey{ClientID: "clientB", PairID: "p1", RelPath: "a.txt"})

	// Then: 別 client は正常に取得できる
	if err != nil {
		t.Fatalf("clientB should not be blocked: %v", err)
	}

	releaseA1()
	releaseA2()
	releaseB()
}

func TestScheduler_Dedup_RejectsSameKey(t *testing.T) {
	// Given: Scheduler と 1件取得済みの JobKey
	s := NewScheduler()
	key := JobKey{ClientID: "c1", PairID: "p1", RelPath: "dup.txt"}

	release, err := s.Acquire(context.Background(), key)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	// When: 同じ JobKey で再度取得しようとする
	_, err = s.Acquire(context.Background(), key)

	// Then: ErrDuplicateJob が返る
	if !errors.Is(err, ErrDuplicateJob) {
		t.Fatalf("expected ErrDuplicateJob, got %v", err)
	}

	// When: release 後に同じキーで再取得する
	release()
	release2, err := s.Acquire(context.Background(), key)

	// Then: 正常に取得できる
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	release2()
}

func TestScheduler_ContextCancel_Releases(t *testing.T) {
	// Given: per-client limit=1 の Scheduler で 1件取得済み
	s := NewSchedulerWithLimits(64, 1)
	key1 := JobKey{ClientID: "c1", PairID: "p1", RelPath: "hold.txt"}

	hold, err := s.Acquire(context.Background(), key1)
	if err != nil {
		t.Fatalf("hold acquire: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	key2 := JobKey{ClientID: "c1", PairID: "p1", RelPath: "wait.txt"}

	var wg sync.WaitGroup
	wg.Add(1)
	var acquireErr error
	go func() {
		defer wg.Done()
		_, acquireErr = s.Acquire(ctx, key2)
	}()

	// When: context をキャンセルする
	cancel()
	wg.Wait()

	// Then: ErrCanceled が返り、内部状態がクリーン（key2 が pending から除去）
	if !errors.Is(acquireErr, ErrCanceled) {
		t.Fatalf("expected ErrCanceled, got %v", acquireErr)
	}

	hold()

	// key2 が pending から除去されていることを確認（再取得できる）
	release, err := s.Acquire(context.Background(), key2)
	if err != nil {
		t.Fatalf("key2 should be acquirable after cancel: %v", err)
	}
	release()
}

func TestScheduler_ReleaseIsIdempotent(t *testing.T) {
	// Given: 1件取得済みの Scheduler
	s := NewScheduler()
	key := JobKey{ClientID: "c1", PairID: "p1", RelPath: "idem.txt"}

	release, err := s.Acquire(context.Background(), key)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}

	// When: release を 2 回呼ぶ
	release()
	release()

	// Then: パニックせず、次の取得も正常に動作する
	release2, err := s.Acquire(context.Background(), key)
	if err != nil {
		t.Fatalf("acquire after double release: %v", err)
	}
	release2()
}
