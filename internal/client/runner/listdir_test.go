package runner

import (
	"context"
	"os"
	"testing"

	"github.com/HMasataka/axion/internal/clientfs"
	"github.com/HMasataka/axion/internal/proto"
)

func TestHandleListDir_Success(t *testing.T) {
	// Given: jail root にファイルが存在する Runner
	dir := t.TempDir()
	jail, err := clientfs.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(dir + "/hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	r, _ := makeRunner(t, nil)
	r.cfg.Jail = jail

	// When: ReadDir を実行する
	ctx := context.Background()
	resp := r.HandleListDir(ctx, proto.ListDirRequest{RelPath: "."})

	// Then: エラーなし、エントリに hello.txt が含まれる
	if resp.Error != "" {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(resp.Entries))
	}
	if resp.Entries[0].Name != "hello.txt" {
		t.Errorf("expected name=hello.txt, got %s", resp.Entries[0].Name)
	}
	if resp.Entries[0].IsDir {
		t.Error("expected IsDir=false")
	}
}

func TestHandleListDir_PathEscape_ReturnsError(t *testing.T) {
	// Given: Runner
	r, _ := makeRunner(t, nil)

	// When: jail root の外を指す relPath を渡す
	ctx := context.Background()
	resp := r.HandleListDir(ctx, proto.ListDirRequest{RelPath: "../etc"})

	// Then: Error が "path escape:" で始まる
	if resp.Error == "" {
		t.Fatal("expected error, got empty")
	}
	if len(resp.Error) < len("path escape:") || resp.Error[:len("path escape:")] != "path escape:" {
		t.Errorf("expected error to start with 'path escape:', got: %s", resp.Error)
	}
}

func TestHandleListDir_Sorted(t *testing.T) {
	// Given: jail root に複数ファイルがアルファベット逆順で作られている
	dir := t.TempDir()
	jail, err := clientfs.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"z.txt", "a.txt", "m.txt"} {
		f, err := os.Create(dir + "/" + name)
		if err != nil {
			t.Fatal(err)
		}
		f.Close()
	}

	r, _ := makeRunner(t, nil)
	r.cfg.Jail = jail

	// When: ReadDir を実行する
	ctx := context.Background()
	resp := r.HandleListDir(ctx, proto.ListDirRequest{RelPath: "."})

	// Then: エントリがアルファベット順に並んでいる
	if resp.Error != "" {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
	if len(resp.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(resp.Entries))
	}
	expected := []string{"a.txt", "m.txt", "z.txt"}
	for i, e := range resp.Entries {
		if e.Name != expected[i] {
			t.Errorf("entries[%d]: expected %s, got %s", i, expected[i], e.Name)
		}
	}
}

func TestHandleListDir_NestedSubdir(t *testing.T) {
	// Given: jail root に subdir/file.txt が存在する
	dir := t.TempDir()
	jail, err := clientfs.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir+"/subdir", 0755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(dir + "/subdir/file.txt")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	r, _ := makeRunner(t, nil)
	r.cfg.Jail = jail

	// When: subdir を ReadDir する
	ctx := context.Background()
	resp := r.HandleListDir(ctx, proto.ListDirRequest{RelPath: "subdir"})

	// Then: file.txt が1件返る
	if resp.Error != "" {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(resp.Entries))
	}
	if resp.Entries[0].Name != "file.txt" {
		t.Errorf("expected file.txt, got %s", resp.Entries[0].Name)
	}
}
