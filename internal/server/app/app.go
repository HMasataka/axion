package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/HMasataka/axion/internal/proto"
	"github.com/HMasataka/axion/internal/server/blobstore"
	"github.com/HMasataka/axion/internal/server/hub"
	httpsrv "github.com/HMasataka/axion/internal/server/http"
	"github.com/HMasataka/axion/internal/server/store"
	"github.com/HMasataka/axion/internal/server/syncengine"
)

const perClientQuotaBytes int64 = 10 * 1024 * 1024 * 1024 // 10GB (spec.md L62)

// HookContext はテスト/埋め込み用に内部コンポーネントを露出する。
type HookContext struct {
	Store  store.Store
	Engine *syncengine.Engine
}

// Hooks はサーバー起動後に内部コンポーネントへのアクセスを提供する。
type Hooks struct {
	OnReady func(*HookContext)
}

// Config はサーバー起動に必要な設定。
type Config struct {
	Bind          string
	DataDir       string
	AdminUser     string
	AdminPassword string
	PSKFile       string
	ShutdownGrace time.Duration
	Hooks         *Hooks
}

func (c *Config) applyDefaults() {
	if c.Bind == "" {
		c.Bind = "127.0.0.1:8765"
	}
	if c.ShutdownGrace == 0 {
		c.ShutdownGrace = 30 * time.Second
	}
}

// Run はサーバーを起動し、ctx のキャンセルで graceful shutdown する。
func Run(ctx context.Context, cfg Config) error {
	cfg.applyDefaults()

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil { //nolint:forbidigo // server bootstrap creates data dir
		return fmt.Errorf("create data dir: %w", err)
	}

	psk, err := readPSK(cfg.PSKFile)
	if err != nil {
		return fmt.Errorf("read psk: %w", err)
	}

	s, err := store.Open(ctx, filepath.Join(cfg.DataDir, "axion.db"))
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer s.Close()

	settings, err := s.LoadAllSettings(ctx)
	if err != nil {
		return fmt.Errorf("load settings: %w", err)
	}
	slog.InfoContext(ctx, "server starting", "bind", cfg.Bind, "settings_count", len(settings))

	maxFileSize, err := parseInt64Setting(settings, "max_file_size_bytes", 1024*1024*1024)
	if err != nil {
		return err
	}

	bs, err := blobstore.New(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("init blobstore: %w", err)
	}

	// hubSender は Hub を engine の Sender として橋渡しする。
	// engine → hubSender → hub の順で依存するが、hub は engine を参照しないため循環はない。
	var hubSender hubSenderProxy
	engine := syncengine.New(s, &hubSender)

	h := hub.New(
		hub.HandlerFunc(func(hctx context.Context, clientID string, env proto.Envelope) error {
			switch env.Type {
			case proto.TypeFileChangedEvent:
				var ev proto.FileChangedEvent
				if err := proto.UnmarshalPayload(env.Payload, &ev); err != nil {
					return fmt.Errorf("unmarshal file_changed_event: %w", err)
				}
				return engine.HandleFileChanged(hctx, clientID, ev)
			case proto.TypeFileSyncAck:
				var ack proto.FileSyncAck
				if err := proto.UnmarshalPayload(env.Payload, &ack); err != nil {
					return fmt.Errorf("unmarshal file_sync_ack: %w", err)
				}
				return engine.HandleFileSyncAck(hctx, clientID, ack)
			default:
				slog.WarnContext(hctx, "unhandled message", "client_id", clientID, "type", env.Type)
				return nil
			}
		}),
		func(hctx context.Context, clientID string) {
			if err := engine.PublishSubscriptions(hctx, clientID); err != nil {
				slog.ErrorContext(hctx, "publish subscriptions", "client_id", clientID, "error", err)
			}
		},
		func(dctx context.Context, clientID string) {
			if err := s.UpdateClientStatus(dctx, clientID, "offline", time.Now()); err != nil {
				slog.ErrorContext(dctx, "update client status", "client_id", clientID, "error", err)
			}
		},
	)
	hubSender.h = h

	router := httpsrv.NewRouter(httpsrv.Config{
		Store:               s,
		Hub:                 h,
		BlobStore:           bs,
		PSK:                 psk,
		AdminUser:           cfg.AdminUser,
		AdminPassword:       cfg.AdminPassword,
		MaxFileSizeBytes:    maxFileSize,
		PerClientQuotaBytes: perClientQuotaBytes,
	})

	srv := &http.Server{
		Addr:              cfg.Bind,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	var onReady func()
	if cfg.Hooks != nil && cfg.Hooks.OnReady != nil {
		hctx := &HookContext{Store: s, Engine: engine}
		onReady = func() { cfg.Hooks.OnReady(hctx) }
	}

	return runServer(ctx, srv, cfg.ShutdownGrace, onReady)
}

// hubSenderProxy は Hub が生成された後に差し込めるように engine の Sender を遅延バインドする。
type hubSenderProxy struct {
	h *hub.Hub
}

func (p *hubSenderProxy) Send(ctx context.Context, clientID string, env proto.Envelope) error {
	return p.h.Send(ctx, clientID, env)
}

func runServer(ctx context.Context, srv *http.Server, grace time.Duration, onReady func()) error {
	serveErr := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
		close(serveErr)
	}()

	// サーバ起動直後に onReady を goroutine で呼ぶ（listen が開始してから）。
	// 実際の listen 開始を検知する低コストな方法として短い sleep を使う代わりに、
	// onReady は呼び出し側がポーリングして待機することを前提とする。
	if onReady != nil {
		go onReady()
	}

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
		slog.Info("shutting down server")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server shutdown: %w", err)
	}

	return <-serveErr
}

// readPSK はファイルから PSK を読み込み、改行を除去して返す。
func readPSK(path string) (string, error) {
	data, err := os.ReadFile(path) //nolint:forbidigo // server bootstrap reads PSK from configured path
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func parseInt64Setting(settings map[string]string, key string, fallback int64) (int64, error) {
	v, ok := settings[key]
	if !ok {
		return fallback, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid setting %s=%q: %w", key, v, err)
	}
	return n, nil
}
