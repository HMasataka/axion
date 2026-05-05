package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/HMasataka/axion/internal/client/conn"
	"github.com/HMasataka/axion/internal/client/id"
	"github.com/HMasataka/axion/internal/client/runner"
	"github.com/HMasataka/axion/internal/client/transfer"
	"github.com/HMasataka/axion/internal/clientfs"
	"github.com/HMasataka/axion/internal/proto"
)

const ClientVersion = "0.2.3"

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

	transferClient, err := transfer.New(transfer.Config{
		ServerURL: cfg.ServerURL,
		PSK:       psk,
		ClientID:  clientID,
		Jail:      jail,
	})
	if err != nil {
		return fmt.Errorf("app: create transfer client: %w", err)
	}

	c := conn.New(conn.Config{
		ServerURL: cfg.ServerURL,
		PSK:       psk,
		ClientID:  clientID,
		Hostname:  hostname,
		RootPath:  jail.Root(),
		Version:   ClientVersion,
	})

	// connSender は conn.Conn を runner.Sender として橋渡しする。
	sender := &connSender{c: c}

	var r *runner.Runner

	return c.Run(ctx,
		func(settings map[string]string) {
			ignoreList := parseIgnoreList(settings["ignore_list"])

			var newErr error
			r, newErr = runner.New(runner.Config{
				Jail:       jail,
				Transfer:   transferClient,
				Sender:     sender,
				IgnoreList: ignoreList,
			})
			if newErr != nil {
				slog.ErrorContext(ctx, "create runner", "error", newErr)
				return
			}
			if err := r.Start(ctx); err != nil {
				slog.ErrorContext(ctx, "start runner", "error", err)
			}

			attrs := make([]any, 0, len(settings)*2+2)
			for k, v := range settings {
				attrs = append(attrs, k, v)
			}
			slog.InfoContext(ctx, "registered", attrs...)
		},
		func(env proto.Envelope) error {
			if r == nil {
				slog.WarnContext(ctx, "message received before runner ready", "type", env.Type)
				return nil
			}
			return r.HandleEnvelope(ctx, env)
		},
	)
}

// connSender は conn.Conn を runner.Sender に適合させる。
type connSender struct {
	c *conn.Conn
}

func (s *connSender) Send(ctx context.Context, env proto.Envelope) error {
	return s.c.Send(ctx, env)
}

// parseIgnoreList は settings の ignore_list 値（JSON 配列文字列）を []string に変換する。
// 値が空または不正な JSON の場合は空スライスを返す。
func parseIgnoreList(raw string) []string {
	if raw == "" {
		return nil
	}
	var list []string
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		slog.Warn("ignore_list parse failed", "raw", raw, "error", err)
		return nil
	}
	return list
}
