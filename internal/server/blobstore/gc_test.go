package blobstore_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HMasataka/axion/internal/server/blobstore"
)

// backdateBlobFile は blob ファイルの mtime を過去に設定することで
// GC の olderThan 判定をシミュレートする。
func backdateBlobFile(t *testing.T, root, sha string, age time.Duration) {
	t.Helper()
	p := filepath.Join(root, "blobs", sha[0:2], sha)
	past := time.Now().Add(-age)
	if err := os.Chtimes(p, past, past); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
}

func TestGC_RemovesUnreferencedOldBlob(t *testing.T) {
	// Given: 1 blob、参照されていない、作成 8 日前
	root := t.TempDir()
	fs, err := blobstore.New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	data := []byte("unreferenced old blob")
	sha := sha256Hex(data)
	putBlob(t, fs, data)
	backdateBlobFile(t, root, sha, 8*24*time.Hour)

	// When
	deleted, err := fs.GC(ctx, map[string]struct{}{}, 7*24*time.Hour)

	// Then
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 deleted, got %d", deleted)
	}
	ok, _ := fs.Has(ctx, sha)
	if ok {
		t.Error("blob should have been deleted")
	}
}

func TestGC_KeepsReferencedBlob(t *testing.T) {
	// Given: 参照されている blob（古くても）
	root := t.TempDir()
	fs, err := blobstore.New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	data := []byte("referenced blob")
	sha := sha256Hex(data)
	putBlob(t, fs, data)
	backdateBlobFile(t, root, sha, 30*24*time.Hour)

	refs := map[string]struct{}{sha: {}}

	// When
	deleted, err := fs.GC(ctx, refs, 7*24*time.Hour)

	// Then
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if deleted != 0 {
		t.Errorf("expected 0 deleted, got %d", deleted)
	}
	ok, _ := fs.Has(ctx, sha)
	if !ok {
		t.Error("referenced blob should not be deleted")
	}
}

func TestGC_KeepsRecentUnreferencedBlob(t *testing.T) {
	// Given: 未参照だが新しい blob
	root := t.TempDir()
	fs, err := blobstore.New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	data := []byte("recent unreferenced blob")
	putBlob(t, fs, data)
	// mtime を変更しない（新しいまま）

	// When
	deleted, err := fs.GC(ctx, map[string]struct{}{}, 7*24*time.Hour)

	// Then
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if deleted != 0 {
		t.Errorf("expected 0 deleted (too recent), got %d", deleted)
	}
}

func TestGC_MultipleBlobs(t *testing.T) {
	// Given: 5 blob、3 参照あり + 2 つが古い未参照
	root := t.TempDir()
	fs, err := blobstore.New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()

	type blobCase struct {
		data       []byte
		referenced bool
		old        bool
	}
	cases := []blobCase{
		{data: []byte("blob-ref-1"), referenced: true, old: true},
		{data: []byte("blob-ref-2"), referenced: true, old: true},
		{data: []byte("blob-ref-3"), referenced: true, old: false},
		{data: []byte("blob-unref-old-1"), referenced: false, old: true},
		{data: []byte("blob-unref-old-2"), referenced: false, old: true},
	}

	refs := map[string]struct{}{}
	for _, c := range cases {
		sha := sha256Hex(c.data)
		putBlob(t, fs, c.data)
		if c.old {
			backdateBlobFile(t, root, sha, 10*24*time.Hour)
		}
		if c.referenced {
			refs[sha] = struct{}{}
		}
	}

	// When
	deleted, err := fs.GC(ctx, refs, 7*24*time.Hour)

	// Then: 2 件削除
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if deleted != 2 {
		t.Errorf("expected 2 deleted, got %d", deleted)
	}

	// 参照されている 3 件が残っている
	for _, c := range cases {
		sha := sha256Hex(c.data)
		ok, _ := fs.Has(ctx, sha)
		if c.referenced && !ok {
			t.Errorf("referenced blob %s should exist", sha[:8])
		}
		if !c.referenced && ok {
			t.Errorf("unreferenced old blob %s should be deleted", sha[:8])
		}
	}
}
