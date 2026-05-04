package transfer_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HMasataka/axion/internal/client/transfer"
	"github.com/HMasataka/axion/internal/clientfs"
)

func newTestClient(t *testing.T, srv *httptest.Server) (*transfer.Client, *clientfs.Jail, string) {
	t.Helper()
	dir := t.TempDir()
	jail, err := clientfs.New(dir)
	if err != nil {
		t.Fatalf("clientfs.New: %v", err)
	}
	cli, err := transfer.New(transfer.Config{
		ServerURL: srv.URL,
		PSK:       "test-psk",
		ClientID:  "test-client-id",
		Jail:      jail,
		HTTP:      srv.Client(),
	})
	if err != nil {
		t.Fatalf("transfer.New: %v", err)
	}
	return cli, jail, dir
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

// TestUpload_PutsNewBlob: HEAD 404 → PUT 201 → Skipped=false
func TestUpload_PutsNewBlob(t *testing.T) {
	putCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodHead:
			w.WriteHeader(http.StatusNotFound)
		case http.MethodPut:
			putCalled = true
			io.Copy(io.Discard, r.Body) //nolint:errcheck
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer srv.Close()

	cli, _, dir := newTestClient(t, srv)
	writeFile(t, dir, "hello.txt", "hello world")

	result, err := cli.Upload(context.Background(), "hello.txt")
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if result.Skipped {
		t.Error("expected Skipped=false")
	}
	if result.SHA256 == "" {
		t.Error("expected non-empty SHA256")
	}
	if result.Size != int64(len("hello world")) {
		t.Errorf("expected size %d, got %d", len("hello world"), result.Size)
	}
	if !putCalled {
		t.Error("expected PUT to be called")
	}
}

// TestUpload_SkipsExistingBlob: HEAD 200 → PUT は呼ばれない → Skipped=true
func TestUpload_SkipsExistingBlob(t *testing.T) {
	putCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodHead:
			w.WriteHeader(http.StatusOK)
		case http.MethodPut:
			putCalled = true
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer srv.Close()

	cli, _, dir := newTestClient(t, srv)
	writeFile(t, dir, "existing.txt", "already on server")

	result, err := cli.Upload(context.Background(), "existing.txt")
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if !result.Skipped {
		t.Error("expected Skipped=true")
	}
	if putCalled {
		t.Error("expected PUT not to be called")
	}
}

// TestUpload_LargeFile: 1MB ファイルを正常アップロード
func TestUpload_LargeFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodHead:
			w.WriteHeader(http.StatusNotFound)
		case http.MethodPut:
			io.Copy(io.Discard, r.Body) //nolint:errcheck
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer srv.Close()

	cli, _, dir := newTestClient(t, srv)
	content := strings.Repeat("x", 1024*1024)
	writeFile(t, dir, "large.bin", content)

	result, err := cli.Upload(context.Background(), "large.bin")
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if result.Size != int64(len(content)) {
		t.Errorf("expected size %d, got %d", len(content), result.Size)
	}
	if result.Skipped {
		t.Error("expected Skipped=false")
	}
}

// TestUpload_ServerErrorOnPUT: HEAD 404, PUT 500 → error
func TestUpload_ServerErrorOnPUT(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodHead:
			w.WriteHeader(http.StatusNotFound)
		case http.MethodPut:
			io.Copy(io.Discard, r.Body) //nolint:errcheck
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer srv.Close()

	cli, _, dir := newTestClient(t, srv)
	writeFile(t, dir, "fail.txt", "content")

	_, err := cli.Upload(context.Background(), "fail.txt")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// TestUpload_RootEscapeRejected: rel に "../etc/passwd" → Jail から ErrPathEscape
func TestUpload_RootEscapeRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cli, _, _ := newTestClient(t, srv)

	_, err := cli.Upload(context.Background(), "../etc/passwd")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
