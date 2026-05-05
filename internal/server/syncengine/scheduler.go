package syncengine

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"golang.org/x/sync/semaphore"
)

const (
	defaultGlobalLimit    = 64
	defaultPerClientLimit = 4
)

// JobKey は重複ジョブを判定するためのキー。
type JobKey struct {
	ClientID string
	PairID   string
	RelPath  string
}

// Scheduler は同期ジョブの実行制限を管理する。
type Scheduler struct {
	globalLimit    int64
	perClientLimit int64

	global  *semaphore.Weighted
	mu      sync.Mutex
	perCli  map[string]*semaphore.Weighted
	pending map[JobKey]struct{}
}

var (
	ErrDuplicateJob = errors.New("scheduler: duplicate job")
	ErrCanceled     = errors.New("scheduler: canceled")
)

// NewScheduler はデフォルト制限値で Scheduler を生成する。
func NewScheduler() *Scheduler {
	return NewSchedulerWithLimits(defaultGlobalLimit, defaultPerClientLimit)
}

// NewSchedulerWithLimits は指定した制限値で Scheduler を生成する。
func NewSchedulerWithLimits(globalLimit, perClientLimit int64) *Scheduler {
	return &Scheduler{
		globalLimit:    globalLimit,
		perClientLimit: perClientLimit,
		global:         semaphore.NewWeighted(globalLimit),
		perCli:         make(map[string]*semaphore.Weighted),
		pending:        make(map[JobKey]struct{}),
	}
}

// Acquire は global と該当 client の semaphore を順に取得する。
// 同じ JobKey が既に pending にある場合は ErrDuplicateJob を返す（dedup）。
// ctx canceled で ErrCanceled。
// 成功時は release 関数を返す。release は idempotent。
func (s *Scheduler) Acquire(ctx context.Context, key JobKey) (func(), error) {
	s.mu.Lock()
	if _, exists := s.pending[key]; exists {
		s.mu.Unlock()
		return nil, ErrDuplicateJob
	}
	s.pending[key] = struct{}{}
	cliSem := s.perCli[key.ClientID]
	if cliSem == nil {
		cliSem = semaphore.NewWeighted(s.perClientLimit)
		s.perCli[key.ClientID] = cliSem
	}
	s.mu.Unlock()

	if err := s.global.Acquire(ctx, 1); err != nil {
		s.removePending(key)
		return nil, fmt.Errorf("%w: %v", ErrCanceled, err)
	}

	if err := cliSem.Acquire(ctx, 1); err != nil {
		s.global.Release(1)
		s.removePending(key)
		return nil, fmt.Errorf("%w: %v", ErrCanceled, err)
	}

	var once sync.Once
	release := func() {
		once.Do(func() {
			cliSem.Release(1)
			s.global.Release(1)
			s.removePending(key)
		})
	}
	return release, nil
}

func (s *Scheduler) removePending(key JobKey) {
	s.mu.Lock()
	delete(s.pending, key)
	s.mu.Unlock()
}
