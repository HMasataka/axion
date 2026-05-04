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

	"github.com/HMasataka/axion/internal/client/app"
)

func runClient(args []string) {
	fs := flag.NewFlagSet("client", flag.ExitOnError)

	server := fs.String("server", "ws://127.0.0.1:8765", "axion server URL")
	root := fs.String("root", "", "root directory to sync (default: current working directory)")
	idFile := fs.String("id-file", defaultClientIDFile(), "file storing client UUID")
	pskFile := fs.String("psk-file", defaultClientPSKFile(), "file containing pre-shared key")

	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: axion client [flags]")
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
		ServerURL: *server,
		Root:      *root,
		IDFile:    *idFile,
		PSKFile:   *pskFile,
	}

	if err := app.Run(ctx, cfg); err != nil {
		slog.Error("axion client failed", "error", err)
		os.Exit(1)
	}
}

func defaultClientIDFile() string {
	if h, err := os.UserConfigDir(); err == nil {
		return filepath.Join(h, "axion", "client.id")
	}
	return "./axion.client.id"
}

func defaultClientPSKFile() string {
	if h, err := os.UserConfigDir(); err == nil {
		return filepath.Join(h, "axion", "server.token")
	}
	return "./axion.server.token"
}
