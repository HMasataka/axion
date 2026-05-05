package runner

import (
	"sync"
	"time"
)

// Suppressor は「自分が書いたファイル」由来の watcher 発火を抑止する。
type Suppressor struct {
	mu     sync.Mutex
	m      map[string]suppressEntry
	clock  func() time.Time
	stopGC chan struct{}
}

type suppressEntry struct {
	expiresAt time.Time
	sha256    string
}

// NewSuppressor は新しい Suppressor を作る（GC は StartGC で別途開始する）。
func NewSuppressor() *Suppressor {
	return &Suppressor{
		m:      make(map[string]suppressEntry),
		clock:  time.Now,
		stopGC: nil,
	}
}

// Add は rel を ttl 期間 suppress 対象に登録する。sha256 は空文字でも可。
func (s *Suppressor) Add(rel, sha256 string, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[rel] = suppressEntry{
		expiresAt: s.clock().Add(ttl),
		sha256:    sha256,
	}
}

// Hit は rel が suppress 対象なら true を返す。期限切れは false。
func (s *Suppressor) Hit(rel string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.m[rel]
	if !ok {
		return false
	}
	if s.clock().After(e.expiresAt) {
		delete(s.m, rel)
		return false
	}
	return true
}

// HitWithSHA は rel + sha256 の組み合わせが suppress 対象なら true を返す。
// sha256 が記録と異なる場合は実 watcher 由来として false を返す。
func (s *Suppressor) HitWithSHA(rel, sha256 string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.m[rel]
	if !ok {
		return false
	}
	if s.clock().After(e.expiresAt) {
		delete(s.m, rel)
		return false
	}
	return e.sha256 == "" || e.sha256 == sha256
}

// StartGC は ticker 間隔で期限切れエントリを掃除する goroutine を起動する。
// 既に起動済みの場合は何もしない（idempotent）。
func (s *Suppressor) StartGC(interval time.Duration) {
	s.mu.Lock()
	if s.stopGC != nil {
		s.mu.Unlock()
		return
	}
	stop := make(chan struct{})
	s.stopGC = stop
	s.mu.Unlock()

	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				s.gcOnce()
			}
		}
	}()
}

// StopGC は GC goroutine を停止する（idempotent）。
func (s *Suppressor) StopGC() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopGC != nil {
		close(s.stopGC)
		s.stopGC = nil
	}
}

func (s *Suppressor) gcOnce() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock()
	for k, e := range s.m {
		if now.After(e.expiresAt) {
			delete(s.m, k)
		}
	}
}
