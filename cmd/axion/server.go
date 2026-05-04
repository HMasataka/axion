package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/HMasataka/axion/internal/server/app"
)

func runServer(args []string) {
	fs := flag.NewFlagSet("server", flag.ExitOnError)

	bind := fs.String("bind", "127.0.0.1:8765", "address to bind (use 0.0.0.0:port to expose externally)")
	dataDir := fs.String("data-dir", defaultDataDir(), "directory for sqlite db and blob storage")
	adminUser := fs.String("admin-user", "", "Basic auth user for Web UI (v0.2.5+)")
	adminPassword := fs.String("admin-password", "", "Basic auth password for Web UI")
	pskFile := fs.String("psk-file", defaultPSKFile(), "file containing pre-shared key for client authentication")

	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: axion server [flags]")
		fmt.Fprintln(fs.Output(), "\nFlags:")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cfg := app.Config{
		Bind:          *bind,
		DataDir:       *dataDir,
		AdminUser:     *adminUser,
		AdminPassword: *adminPassword,
		PSKFile:       *pskFile,
		ShutdownGrace: 30 * time.Second,
	}

	if err := app.Run(ctx, cfg); err != nil {
		slog.Error("axion server failed", "error", err)
		os.Exit(1)
	}
}

func defaultDataDir() string {
	if h, err := os.UserConfigDir(); err == nil {
		return filepath.Join(filepath.Dir(h), "share", "axion")
	}
	return "./axion-data"
}

func defaultPSKFile() string {
	if h, err := os.UserConfigDir(); err == nil {
		return filepath.Join(h, "axion", "server.token")
	}
	return "./axion-server.token"
}
