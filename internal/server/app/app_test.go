package app

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRun_StartsAndShutsDown(t *testing.T) {
	t.Parallel()

	// Given: tempdir with PSK file and data dir
	dir := t.TempDir()
	pskPath := filepath.Join(dir, "server.token")
	if err := os.WriteFile(pskPath, []byte("test-psk-12345\n"), 0o600); err != nil {
		t.Fatalf("write psk: %v", err)
	}

	cfg := Config{
		Bind:          "127.0.0.1:0",
		DataDir:       filepath.Join(dir, "data"),
		PSKFile:       pskPath,
		ShutdownGrace: 5 * time.Second,
	}

	// When: Run is started and context is cancelled
	ctx, cancel := context.WithCancel(context.Background())

	// Bind to a dynamic port is not directly observable; use a fixed test port
	cfg.Bind = "127.0.0.1:18766"

	runDone := make(chan error, 1)
	go func() {
		runDone <- Run(ctx, cfg)
	}()

	// Wait for server to become ready
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://127.0.0.1:18766/v1/ws") //nolint:forbidigo,noctx
		if err == nil {
			resp.Body.Close()
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	cancel()

	// Then: Run returns within 5 seconds
	select {
	case err := <-runDone:
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not shut down within 5 seconds")
	}
}

func TestRun_FailsOnMissingPSKFile(t *testing.T) {
	t.Parallel()

	// Given: PSK file does not exist
	dir := t.TempDir()
	cfg := Config{
		Bind:          "127.0.0.1:18767",
		DataDir:       filepath.Join(dir, "data"),
		PSKFile:       filepath.Join(dir, "nonexistent.token"),
		ShutdownGrace: 5 * time.Second,
	}

	// When: Run is called
	err := Run(context.Background(), cfg)

	// Then: error is returned
	if err == nil {
		t.Fatal("expected error for missing PSK file, got nil")
	}
}

func TestRun_FailsOnInvalidBind(t *testing.T) {
	t.Parallel()

	// Given: PSK file exists but Bind address is invalid
	dir := t.TempDir()
	pskPath := filepath.Join(dir, "server.token")
	if err := os.WriteFile(pskPath, []byte("test-psk"), 0o600); err != nil {
		t.Fatalf("write psk: %v", err)
	}

	cfg := Config{
		Bind:          "invalid",
		DataDir:       filepath.Join(dir, "data"),
		PSKFile:       pskPath,
		ShutdownGrace: 5 * time.Second,
	}

	// When: Run is called
	err := Run(context.Background(), cfg)

	// Then: error is returned
	if err == nil {
		t.Fatal("expected error for invalid bind address, got nil")
	}
}
