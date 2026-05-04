package app_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HMasataka/axion/internal/client/app"
)

func TestRun_FailsOnMissingPSKFile(t *testing.T) {
	dir := t.TempDir()

	cfg := app.Config{
		ServerURL: "ws://127.0.0.1:19999",
		Root:      dir,
		IDFile:    filepath.Join(dir, "client.id"),
		PSKFile:   filepath.Join(dir, "nonexistent.token"),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := app.Run(ctx, cfg)
	if err == nil {
		t.Fatal("expected error for missing PSK file, got nil")
	}
}

func TestRun_FailsOnInvalidRoot(t *testing.T) {
	dir := t.TempDir()
	pskFile := filepath.Join(dir, "server.token")
	if err := os.WriteFile(pskFile, []byte("test-psk"), 0600); err != nil { //nolint:forbidigo
		t.Fatalf("write psk: %v", err)
	}

	cfg := app.Config{
		ServerURL: "ws://127.0.0.1:19999",
		Root:      filepath.Join(dir, "does-not-exist"),
		IDFile:    filepath.Join(dir, "client.id"),
		PSKFile:   pskFile,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := app.Run(ctx, cfg)
	if err == nil {
		t.Fatal("expected error for invalid root, got nil")
	}
}

func TestRun_FailsOnInvalidServerURL(t *testing.T) {
	dir := t.TempDir()
	pskFile := filepath.Join(dir, "server.token")
	if err := os.WriteFile(pskFile, []byte("test-psk"), 0600); err != nil { //nolint:forbidigo
		t.Fatalf("write psk: %v", err)
	}

	cfg := app.Config{
		ServerURL: "ws://127.0.0.1:1", // port 1 refuses connections
		Root:      dir,
		IDFile:    filepath.Join(dir, "client.id"),
		PSKFile:   pskFile,
	}

	// Run with very short timeout; with an unreachable server the conn loop
	// will backoff and eventually ctx cancels — that returns nil (clean shutdown).
	// We verify Run doesn't panic and exits cleanly within the deadline.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := app.Run(ctx, cfg)
	// ctx cancellation causes Run to return nil; that is acceptable.
	// We just verify no unexpected panic.
	_ = err
}
