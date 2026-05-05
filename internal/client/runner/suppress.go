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
	// shas は suppress 対象の SHA256 集合。空集合のとき任意 SHA を suppress する。
	shas map[string]struct{}
}

// NewSuppressor は新しい Suppressor を作る（GC は StartGC で別途開始する）。
func NewSuppressor() *Suppressor {
	return &Suppressor{
		m:      make(map[string]suppressEntry),
		clock:  time.Now,
		stopGC: nil,
	}
}

// Add は rel を ttl 期間 suppress 対象に登録する。
// sha256 は空文字でも可（任意 SHA を suppress）。
// 既存エントリがある場合は SHA を追加し、TTL をより長い方に更新する。
func (s *Suppressor) Add(rel, sha256 string, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	newExpiry := s.clock().Add(ttl)
	e, exists := s.m[rel]
	if !exists {
		shas := make(map[string]struct{})
		if sha256 != "" {
			shas[sha256] = struct{}{}
		}
		s.m[rel] = suppressEntry{expiresAt: newExpiry, shas: shas}
		return
	}
	// 既存エントリを更新: TTL を延長し SHA を追加する。
	if newExpiry.After(e.expiresAt) {
		e.expiresAt = newExpiry
	}
	if sha256 != "" {
		e.shas[sha256] = struct{}{}
	} else {
		// 空 SHA は全 SHA を suppress する意味なので集合をクリア。
		e.shas = make(map[string]struct{})
	}
	s.m[rel] = e
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
// 登録された SHA 集合に含まれるか、集合が空（任意 SHA を suppress）なら true。
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
	if len(e.shas) == 0 {
		// 空集合 = 任意 SHA を suppress
		return true
	}
	_, found := e.shas[sha256]
	return found
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
