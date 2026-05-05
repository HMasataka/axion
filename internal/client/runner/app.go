package runner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/HMasataka/axion/internal/client/transfer"
	"github.com/HMasataka/axion/internal/clientfs"
	"github.com/HMasataka/axion/internal/proto"
)

const (
	suppressTTL    = 2 * time.Second
	suppressGCRate = 10 * time.Second
)

// Sender はサーバーへの WS 送信責務。
type Sender interface {
	Send(ctx context.Context, env proto.Envelope) error
}

// TransferClient は upload/download の責務。transfer.Client はこの interface を満たす。
type TransferClient interface {
	Upload(ctx context.Context, rel string) (transfer.UploadResult, error)
	Download(ctx context.Context, rel, sha string) error
}

// Config は Runner の設定。
type Config struct {
	Jail       *clientfs.Jail
	Transfer   TransferClient
	Sender     Sender
	IgnoreList []string
}

// Runner はクライアント側の同期オーケストレータ。
type Runner struct {
	cfg       Config
	reg       *Registry
	supp      *Suppressor
	wr        *WatcherRunner
	startOnce sync.Once
}

// New は新しい Runner を作る（Start を呼ぶまで watcher は起動しない）。
func New(cfg Config) (*Runner, error) {
	if cfg.Jail == nil {
		return nil, errors.New("runner: Jail is required")
	}
	if cfg.Transfer == nil {
		return nil, errors.New("runner: Transfer is required")
	}
	if cfg.Sender == nil {
		return nil, errors.New("runner: Sender is required")
	}
	return &Runner{
		cfg:  cfg,
		reg:  NewRegistry(),
		supp: NewSuppressor(),
	}, nil
}

// Start は watcher を起動し、変換ループを開始する。
func (r *Runner) Start(ctx context.Context) error {
	var startErr error
	r.startOnce.Do(func() {
		wr, err := NewWatcherRunner(WatchConfig{
			Jail:       r.cfg.Jail,
			Registry:   r.reg,
			IgnoreList: r.cfg.IgnoreList,
		})
		if err != nil {
			startErr = err
			return
		}
		r.wr = wr
		if err := r.wr.Start(ctx); err != nil {
			startErr = err
			return
		}
		r.supp.StartGC(suppressGCRate)
		go r.eventLoop(ctx)
		go r.errorPump(ctx)
	})
	return startErr
}

// HandleEnvelope はサーバー受信メッセージを処理する入り口。
func (r *Runner) HandleEnvelope(ctx context.Context, env proto.Envelope) error {
	switch env.Type {
	case proto.TypeSubscribePair:
		var sub proto.SubscribePair
		if err := proto.UnmarshalPayload(env.Payload, &sub); err != nil {
			return err
		}
		r.reg.Apply(sub)
		slog.InfoContext(ctx, "subscribe pair",
			"pair_id", sub.PairID,
			"side", sub.Side,
			"direction", sub.Direction,
		)
		return nil
	case proto.TypeFileSyncCommand:
		var cmd proto.FileSyncCommand
		if err := proto.UnmarshalPayload(env.Payload, &cmd); err != nil {
			return err
		}
		go r.handleSyncCommand(ctx, cmd)
		return nil
	case proto.TypeListDirRequest:
		var req proto.ListDirRequest
		if err := proto.UnmarshalPayload(env.Payload, &req); err != nil {
			return err
		}
		resp := r.HandleListDir(ctx, req)
		respPayload, err := proto.MarshalPayload(resp)
		if err != nil {
			return err
		}
		respEnv := proto.Envelope{
			Type:          proto.TypeListDirResponse,
			CorrelationID: env.CorrelationID,
			Payload:       respPayload,
		}
		return r.cfg.Sender.Send(ctx, respEnv)
	default:
		return nil
	}
}

// Stop は watcher 停止 + suppress GC 停止。
func (r *Runner) Stop() error {
	r.supp.StopGC()
	if r.wr != nil {
		return r.wr.Stop()
	}
	return nil
}

func (r *Runner) eventLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-r.wr.Events():
			if !ok {
				return
			}
			r.handleLocalChange(ctx, ev)
		}
	}
}

func (r *Runner) errorPump(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-r.wr.Errors():
			if !ok {
				return
			}
			slog.ErrorContext(ctx, "watcher error", "error", e)
		}
	}
}

func (r *Runner) handleLocalChange(ctx context.Context, ev proto.FileChangedEvent) {
	switch ev.Op {
	case "write":
		if r.supp.HitWithSHA(ev.RelPath, ev.SHA256) {
			shaPrefix := ev.SHA256
			if len(shaPrefix) > 8 {
				shaPrefix = shaPrefix[:8]
			}
			slog.DebugContext(ctx, "suppressed local change", "rel", ev.RelPath, "sha", shaPrefix)
			return
		}
	case "delete":
		if r.supp.Hit(ev.RelPath) {
			return
		}
	}

	if ev.Op == "write" {
		sub, ok := r.lookupSubscription(ev.PairID)
		if !ok {
			slog.WarnContext(ctx, "no subscription for pair", "pair_id", ev.PairID)
			return
		}
		jailRel := filepath.Join(sub.RootSubpath, ev.RelPath)
		if _, err := r.cfg.Transfer.Upload(ctx, jailRel); err != nil {
			slog.ErrorContext(ctx, "upload failed", "rel", jailRel, "error", err)
			return
		}
	}

	payload, err := proto.MarshalPayload(ev)
	if err != nil {
		slog.ErrorContext(ctx, "marshal file changed event", "error", err)
		return
	}
	env := proto.Envelope{Type: proto.TypeFileChangedEvent, Payload: payload}
	if err := r.cfg.Sender.Send(ctx, env); err != nil {
		slog.ErrorContext(ctx, "send file changed", "error", err)
	}
}

func (r *Runner) lookupSubscription(pairID string) (Subscription, bool) {
	for _, s := range r.reg.All() {
		if s.PairID == pairID {
			return s, true
		}
	}
	return Subscription{}, false
}

func (r *Runner) handleSyncCommand(ctx context.Context, cmd proto.FileSyncCommand) {
	sub, ok := r.lookupSubscription(cmd.PairID)
	if !ok {
		r.sendAck(ctx, cmd, "failed", "no subscription")
		return
	}
	jailRel := filepath.Join(sub.RootSubpath, cmd.RelPath)

	switch cmd.Op {
	case "fetch":
		r.supp.Add(jailRel, cmd.SHA256, suppressTTL)
		if err := r.cfg.Transfer.Download(ctx, jailRel, cmd.SHA256); err != nil {
			r.sendAck(ctx, cmd, "failed", err.Error())
			return
		}
		r.sendAck(ctx, cmd, "ok", "")
	case "delete":
		r.supp.Add(jailRel, "", suppressTTL)
		if err := r.cfg.Jail.Remove(jailRel); err != nil && !errors.Is(err, os.ErrNotExist) {
			r.sendAck(ctx, cmd, "failed", err.Error())
			return
		}
		r.sendAck(ctx, cmd, "ok", "")
	case "rename":
		newJailRel := filepath.Join(sub.RootSubpath, cmd.NewRelPath)
		r.supp.Add(jailRel, "", suppressTTL)
		r.supp.Add(newJailRel, "", suppressTTL)
		if err := r.cfg.Jail.Rename(jailRel, newJailRel); err != nil {
			r.sendAck(ctx, cmd, "failed", err.Error())
			return
		}
		r.sendAck(ctx, cmd, "ok", "")
	default:
		r.sendAck(ctx, cmd, "failed", fmt.Sprintf("unknown op: %s", cmd.Op))
	}
}

func (r *Runner) sendAck(ctx context.Context, cmd proto.FileSyncCommand, status, errMsg string) {
	ack := proto.FileSyncAck{
		PairID:  cmd.PairID,
		Side:    cmd.Side,
		RelPath: cmd.RelPath,
		SHA256:  cmd.SHA256,
		Status:  status,
		Error:   errMsg,
	}
	payload, err := proto.MarshalPayload(ack)
	if err != nil {
		slog.ErrorContext(ctx, "marshal ack", "error", err)
		return
	}
	env := proto.Envelope{Type: proto.TypeFileSyncAck, Payload: payload}
	if err := r.cfg.Sender.Send(ctx, env); err != nil {
		slog.ErrorContext(ctx, "send ack failed", "error", err)
	}
}
