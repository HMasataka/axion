package runner

import (
	"sync"
	"testing"
	"time"
)

func fixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

func TestSuppressor_AddAndHit(t *testing.T) {
	s := NewSuppressor()
	now := time.Now()
	s.clock = fixedClock(now)

	s.Add("foo/bar.txt", "", time.Second)

	if !s.Hit("foo/bar.txt") {
		t.Fatal("expected Hit to return true after Add")
	}
}

func TestSuppressor_TTLExpires(t *testing.T) {
	s := NewSuppressor()
	base := time.Now()
	s.clock = fixedClock(base)

	s.Add("foo/bar.txt", "", 10*time.Millisecond)

	// 時計を TTL 後に進める
	s.clock = fixedClock(base.Add(20 * time.Millisecond))

	if s.Hit("foo/bar.txt") {
		t.Fatal("expected Hit to return false after TTL expired")
	}
}

func TestSuppressor_HitWithSHA_Match(t *testing.T) {
	s := NewSuppressor()
	now := time.Now()
	s.clock = fixedClock(now)

	s.Add("a.txt", "abc123", time.Second)

	if !s.HitWithSHA("a.txt", "abc123") {
		t.Fatal("expected HitWithSHA to return true when SHA matches")
	}
}

func TestSuppressor_HitWithSHA_Mismatch(t *testing.T) {
	s := NewSuppressor()
	now := time.Now()
	s.clock = fixedClock(now)

	s.Add("a.txt", "abc123", time.Second)

	if s.HitWithSHA("a.txt", "different") {
		t.Fatal("expected HitWithSHA to return false when SHA mismatches")
	}
}

func TestSuppressor_HitWithSHA_EmptyStoredSHA(t *testing.T) {
	s := NewSuppressor()
	now := time.Now()
	s.clock = fixedClock(now)

	// 空 SHA で登録した場合は任意 SHA でヒット
	s.Add("a.txt", "", time.Second)

	if !s.HitWithSHA("a.txt", "anything") {
		t.Fatal("expected HitWithSHA to return true when stored SHA is empty")
	}
}

func TestSuppressor_GCRemovesExpired(t *testing.T) {
	s := NewSuppressor()
	base := time.Now()
	s.clock = fixedClock(base)

	s.Add("gc.txt", "", 5*time.Millisecond)

	// 時計を期限後に進めて GC 実行
	s.clock = fixedClock(base.Add(10 * time.Millisecond))
	s.gcOnce()

	// 内部 map から消えているか確認（Hit を経由せず直接確認）
	s.mu.Lock()
	_, exists := s.m["gc.txt"]
	s.mu.Unlock()

	if exists {
		t.Fatal("expected GC to remove expired entry")
	}
}

func TestSuppressor_ConcurrentAddHit(t *testing.T) {
	s := NewSuppressor()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(n int) {
			defer wg.Done()
			s.Add("concurrent.txt", "", 100*time.Millisecond)
		}(i)
		go func(n int) {
			defer wg.Done()
			s.Hit("concurrent.txt")
		}(i)
	}
	wg.Wait()
}

func TestSuppressor_StopGC_Idempotent(t *testing.T) {
	s := NewSuppressor()
	s.StartGC(100 * time.Millisecond)

	// 2 回 StopGC を呼んでもパニックしない
	s.StopGC()
	s.StopGC()
}
