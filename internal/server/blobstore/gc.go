package blobstore

import (
	"context"
	"log/slog"
	"time"

	"github.com/HMasataka/axion/internal/server/store"
)

// GC は参照されていない blob を削除する。
// referencedSHAs は現在 file_state で参照されている SHA のセット。
// olderThan より古い blob のみ対象とする（新しい未参照 blob は転送中の可能性があるため保護）。
func (fs *FS) GC(ctx context.Context, referencedSHAs map[string]struct{}, olderThan time.Duration) (int, error) {
	cutoff := time.Now().Add(-olderThan)
	blobs, err := fs.List(ctx)
	if err != nil {
		return 0, err
	}

	deleted := 0
	for _, b := range blobs {
		if ctx.Err() != nil {
			return deleted, ctx.Err()
		}
		if _, referenced := referencedSHAs[b.SHA]; referenced {
			continue
		}
		if b.CreatedAt.After(cutoff) {
			continue
		}
		if err := fs.Delete(ctx, b.SHA); err != nil {
			slog.WarnContext(ctx, "gc delete", "sha", b.SHA, "error", err)
			continue
		}
		deleted++
	}
	return deleted, nil
}

// RunGCLoop は ticker 間隔で GC を実行する goroutine。ctx のキャンセルで終了する。
func RunGCLoop(ctx context.Context, fs *FS, s store.Store, getMaxAge func() time.Duration) {
	t := time.NewTicker(1 * time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			referenced, err := collectReferencedSHAs(ctx, s)
			if err != nil {
				slog.WarnContext(ctx, "gc collect refs", "error", err)
				continue
			}
			n, err := fs.GC(ctx, referenced, getMaxAge())
			if err != nil {
				slog.WarnContext(ctx, "gc", "error", err)
				continue
			}
			slog.InfoContext(ctx, "blob gc complete", "deleted", n)
		}
	}
}

func collectReferencedSHAs(ctx context.Context, s store.Store) (map[string]struct{}, error) {
	pairs, err := s.ListPairs(ctx)
	if err != nil {
		return nil, err
	}
	refs := make(map[string]struct{})
	for _, p := range pairs {
		for _, side := range []string{"a", "b"} {
			states, err := s.ListFileStates(ctx, p.ID, side)
			if err != nil {
				return nil, err
			}
			for _, st := range states {
				if st.SHA256 != nil && *st.SHA256 != "" {
					refs[*st.SHA256] = struct{}{}
				}
			}
		}
	}
	return refs, nil
}
