package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/HMasataka/axion/internal/client/conn"
	"github.com/HMasataka/axion/internal/client/id"
	"github.com/HMasataka/axion/internal/clientfs"
	"github.com/HMasataka/axion/internal/proto"
)

const ClientVersion = "0.2.1"

type Config struct {
	ServerURL string
	Root      string
	IDFile    string
	PSKFile   string
}

func Run(ctx context.Context, cfg Config) error {
	if cfg.Root == "" {
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("app: getwd: %w", err)
		}
		cfg.Root = wd
	}

	pskData, err := os.ReadFile(cfg.PSKFile) //nolint:forbidigo // PSK file is outside jail
	if err != nil {
		return fmt.Errorf("app: read psk: %w", err)
	}
	psk := strings.TrimSpace(string(pskData))

	jail, err := clientfs.New(cfg.Root)
	if err != nil {
		return fmt.Errorf("app: create jail: %w", err)
	}

	clientID, err := id.LoadOrCreate(cfg.IDFile)
	if err != nil {
		return fmt.Errorf("app: load client id: %w", err)
	}

	hostname, err := os.Hostname() //nolint:forbidigo // hostname lookup is outside jail
	if err != nil {
		return fmt.Errorf("app: hostname: %w", err)
	}

	c := conn.New(conn.Config{
		ServerURL: cfg.ServerURL,
		PSK:       psk,
		ClientID:  clientID,
		Hostname:  hostname,
		RootPath:  jail.Root(),
		Version:   ClientVersion,
	})

	return c.Run(ctx, onConnected(ctx), onMessage(ctx))
}

func onConnected(ctx context.Context) func(settings map[string]string) {
	return func(settings map[string]string) {
		attrs := make([]any, 0, len(settings)*2+2)
		for k, v := range settings {
			attrs = append(attrs, k, v)
		}
		slog.InfoContext(ctx, "registered", attrs...)
	}
}

func onMessage(ctx context.Context) func(env proto.Envelope) error {
	return func(env proto.Envelope) error {
		slog.WarnContext(ctx, "unhandled message", "type", env.Type)
		return nil
	}
}
