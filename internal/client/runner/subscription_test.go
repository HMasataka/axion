package runner

import (
	"sort"
	"sync"
	"testing"

	"github.com/HMasataka/axion/internal/proto"
)

func applyPair(r *Registry, pairID, side, rootSubpath, direction string) {
	r.Apply(proto.SubscribePair{
		PairID:      pairID,
		Side:        side,
		RootSubpath: rootSubpath,
		Direction:   direction,
	})
}

func TestRegistry_Apply_Adds(t *testing.T) {
	// Given: 空の Registry
	r := NewRegistry()

	// When: subscription を Apply
	applyPair(r, "pair-1", "a", "work/projects", "bidirectional")

	// Then: All() に 1 件含まれる
	all := r.All()
	if len(all) != 1 {
		t.Fatalf("expected 1 subscription, got %d", len(all))
	}
	if all[0].PairID != "pair-1" {
		t.Fatalf("expected pair-1, got %s", all[0].PairID)
	}
}

func TestRegistry_Apply_EmptyDirection_Removes(t *testing.T) {
	// Given: subscription が1件登録済み
	r := NewRegistry()
	applyPair(r, "pair-1", "a", "work/projects", "bidirectional")

	// When: Direction="" で Apply
	applyPair(r, "pair-1", "a", "work/projects", "")

	// Then: 削除されている
	all := r.All()
	if len(all) != 0 {
		t.Fatalf("expected 0 subscriptions after unsubscribe, got %d", len(all))
	}
}

func TestRegistry_Resolve_NoMatch(t *testing.T) {
	// Given: "work/projects" に登録
	r := NewRegistry()
	applyPair(r, "pair-1", "a", "work/projects", "bidirectional")

	// When: 無関係なパスを Resolve
	matches := r.Resolve("other/path/foo.txt")

	// Then: 0件
	if len(matches) != 0 {
		t.Fatalf("expected 0 matches, got %d", len(matches))
	}
}

func TestRegistry_Resolve_RootMatchExact(t *testing.T) {
	// Given: RootSubpath="work"
	r := NewRegistry()
	applyPair(r, "pair-1", "a", "work", "bidirectional")

	// When: relPath="work" (exact match)
	matches := r.Resolve("work")

	// Then: SubRel="."
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if matches[0].SubRel != "." {
		t.Fatalf("expected SubRel='.', got %s", matches[0].SubRel)
	}
}

func TestRegistry_Resolve_NestedMatch(t *testing.T) {
	// Given: RootSubpath="work"
	r := NewRegistry()
	applyPair(r, "pair-1", "a", "work", "bidirectional")

	// When: relPath="work/foo.txt"
	matches := r.Resolve("work/foo.txt")

	// Then: SubRel="foo.txt"
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if matches[0].SubRel != "foo.txt" {
		t.Fatalf("expected SubRel='foo.txt', got %s", matches[0].SubRel)
	}
}

func TestRegistry_Resolve_NoFalseMatchOnPrefix(t *testing.T) {
	// Given: RootSubpath="work"
	r := NewRegistry()
	applyPair(r, "pair-1", "a", "work", "bidirectional")

	// When: relPath="working/foo.txt" (prefix が一致するが境界不一致)
	matches := r.Resolve("working/foo.txt")

	// Then: 0件
	if len(matches) != 0 {
		t.Fatalf("expected 0 matches for prefix-only match, got %d", len(matches))
	}
}

func TestRegistry_Resolve_MultipleMatches(t *testing.T) {
	// Given: 同じ RootSubpath で 2 ペア登録
	r := NewRegistry()
	applyPair(r, "pair-ab", "a", "work/projects", "bidirectional")
	applyPair(r, "pair-ac", "a", "work/projects", "bidirectional")

	// When: 共通パスを Resolve
	matches := r.Resolve("work/projects/main.go")

	// Then: 2件
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}
	ids := make([]string, 0, 2)
	for _, m := range matches {
		ids = append(ids, m.PairID)
		if m.SubRel != "main.go" {
			t.Fatalf("expected SubRel='main.go', got %s", m.SubRel)
		}
	}
	sort.Strings(ids)
	if ids[0] != "pair-ab" || ids[1] != "pair-ac" {
		t.Fatalf("unexpected pair IDs: %v", ids)
	}
}

func TestRegistry_Resolve_RootEmpty(t *testing.T) {
	// Given: RootSubpath="" (全 rel にマッチ)
	r := NewRegistry()
	applyPair(r, "pair-1", "a", "", "bidirectional")

	// When: 任意のパスを Resolve
	matches := r.Resolve("any/deep/path/file.txt")

	// Then: 1件、SubRel は relPath そのまま
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if matches[0].SubRel != "any/deep/path/file.txt" {
		t.Fatalf("expected full relPath as SubRel, got %s", matches[0].SubRel)
	}
}

func TestRegistry_Apply_Concurrent(t *testing.T) {
	// Given: Registry
	r := NewRegistry()
	const n = 100

	// When: 並行 Apply と Resolve
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(2)
		pairID := "pair-concurrent"
		go func() {
			defer wg.Done()
			applyPair(r, pairID, "a", "work", "bidirectional")
		}()
		go func() {
			defer wg.Done()
			r.Resolve("work/foo.txt")
		}()
	}
	wg.Wait()

	// Then: race detector でパニックしない (テスト自体が通ること)
}
