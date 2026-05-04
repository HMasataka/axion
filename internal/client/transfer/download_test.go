package transfer_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/HMasataka/axion/internal/client/transfer"
)

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// TestDownload_NewFile: GET 200 + body → Jail.Stat(rel) で size 一致、内容一致
func TestDownload_NewFile(t *testing.T) {
	content := []byte("download me")
	sha := sha256Hex(content)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write(content) //nolint:errcheck
	}))
	defer srv.Close()

	cli, jail, dir := newTestClient(t, srv)

	if err := cli.Download(context.Background(), "output.bin", sha); err != nil {
		t.Fatalf("Download: %v", err)
	}

	info, err := jail.Stat("output.bin")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Size() != int64(len(content)) {
		t.Errorf("expected size %d, got %d", len(content), info.Size())
	}

	got, err := os.ReadFile(filepath.Join(dir, "output.bin"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("content mismatch: got %q, want %q", got, content)
	}

	// partial は残らない
	if _, err := jail.Stat("output.bin.partial"); err == nil {
		t.Error("partial file should not exist after successful download")
	}
}

// TestDownload_SHAMismatch: GET body と sha 不一致 → ErrSHAMismatch、partial 残置
func TestDownload_SHAMismatch(t *testing.T) {
	content := []byte("wrong content")
	wrongSHA := sha256Hex([]byte("different content"))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(content) //nolint:errcheck
	}))
	defer srv.Close()

	cli, jail, _ := newTestClient(t, srv)

	err := cli.Download(context.Background(), "output.bin", wrongSHA)
	if !errors.Is(err, transfer.ErrSHAMismatch) {
		t.Fatalf("expected ErrSHAMismatch, got %v", err)
	}

	// partial が残っていること
	if _, err := jail.Stat("output.bin.partial"); err != nil {
		t.Errorf("partial file should remain after sha mismatch: %v", err)
	}

	// 最終ファイルは存在しない
	if _, err := jail.Stat("output.bin"); err == nil {
		t.Error("output file should not exist after sha mismatch")
	}
}

// TestDownload_NotFound: GET 404 → error
func TestDownload_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	cli, _, _ := newTestClient(t, srv)

	err := cli.Download(context.Background(), "output.bin", "nonexistentsha")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// TestDownload_RootEscapeRejected: rel に "../etc/passwd" → error
func TestDownload_RootEscapeRejected(t *testing.T) {
	content := []byte("evil")
	sha := sha256Hex(content)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(content) //nolint:errcheck
	}))
	defer srv.Close()

	cli, _, _ := newTestClient(t, srv)

	err := cli.Download(context.Background(), "../etc/passwd", sha)
	if err == nil {
		t.Fatal("expected error for path escape, got nil")
	}
}
