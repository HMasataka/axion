package runner

import (
	"path/filepath"
	"strings"
	"sync"

	"github.com/HMasataka/axion/internal/proto"
)

// Subscription は1ペアのクライアント側登録情報。
type Subscription struct {
	PairID      string
	Side        string // "a" | "b"
	RootSubpath string // クライアント root からの相対 (例: "work/projects")
	Direction   string
}

// ResolvedSubscription は Resolve の返り値。
type ResolvedSubscription struct {
	PairID string
	Side   string
	SubRel string // RootSubpath からの相対
}

// Registry は受信した SubscribePair を保持し、watcher イベントから (pair_id, side) を解決する。
type Registry struct {
	mu sync.RWMutex
	m  map[string]Subscription // pair_id -> Subscription
}

// NewRegistry は空の Registry を返す。
func NewRegistry() *Registry {
	return &Registry{m: make(map[string]Subscription)}
}

// Apply は SubscribePair メッセージを反映する。Direction="" は unsubscribe シグナル。
func (r *Registry) Apply(sub proto.SubscribePair) {
	if sub.Direction == "" {
		r.Remove(sub.PairID)
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.m[sub.PairID] = Subscription{
		PairID:      sub.PairID,
		Side:        sub.Side,
		RootSubpath: sub.RootSubpath,
		Direction:   sub.Direction,
	}
}

// Remove は pair_id の subscription を削除する。
func (r *Registry) Remove(pairID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.m, pairID)
}

// Resolve は relPath (jail root からの相対) を見て、該当する全 (pair_id, side, sub_rel) を返す。
func (r *Registry) Resolve(relPath string) []ResolvedSubscription {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cleaned := filepath.ToSlash(filepath.Clean(relPath))
	var out []ResolvedSubscription
	for _, s := range r.m {
		root := filepath.ToSlash(filepath.Clean(s.RootSubpath))
		if root == "." || root == "" {
			out = append(out, ResolvedSubscription{PairID: s.PairID, Side: s.Side, SubRel: cleaned})
			continue
		}
		if cleaned == root {
			out = append(out, ResolvedSubscription{PairID: s.PairID, Side: s.Side, SubRel: "."})
			continue
		}
		if strings.HasPrefix(cleaned, root+"/") {
			subRel := strings.TrimPrefix(cleaned, root+"/")
			out = append(out, ResolvedSubscription{PairID: s.PairID, Side: s.Side, SubRel: subRel})
		}
	}
	return out
}

// All は現在登録中の subscription 全件を返す（テスト・debug 用）。
func (r *Registry) All() []Subscription {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Subscription, 0, len(r.m))
	for _, s := range r.m {
		result = append(result, s)
	}
	return result
}
