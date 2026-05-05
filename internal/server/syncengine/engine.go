package syncengine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/HMasataka/axion/internal/proto"
	"github.com/HMasataka/axion/internal/server/store"
)

// Sender は Hub への依存を抽象化する interface（依存逆転）。
type Sender interface {
	Send(ctx context.Context, clientID string, env proto.Envelope) error
}

// Engine は同期コアロジック。Hub と Store の間に立つ。
type Engine struct {
	store  store.Store
	sender Sender
}

// New は Engine を生成する。
func New(s store.Store, sender Sender) *Engine {
	return &Engine{store: s, sender: sender}
}

// HandleFileChanged は Hub から受信した FileChangedEvent を処理する。
func (e *Engine) HandleFileChanged(ctx context.Context, srcClientID string, ev proto.FileChangedEvent) error {
	pair, err := e.store.GetPair(ctx, ev.PairID)
	if errors.Is(err, sql.ErrNoRows) {
		slog.WarnContext(ctx, "file changed for unknown pair", "pair_id", ev.PairID)
		return nil
	}
	if err != nil {
		return fmt.Errorf("get pair %s: %w", ev.PairID, err)
	}
	if pair == nil {
		slog.WarnContext(ctx, "file changed for unknown pair", "pair_id", ev.PairID)
		return nil
	}

	if err := e.upsertFileState(ctx, ev); err != nil {
		return err
	}

	if !pair.Enabled {
		return nil
	}

	switch pair.Direction {
	case "bidirectional":
		slog.WarnContext(ctx, "bidirectional sync not yet implemented in v0.2.3", "pair_id", ev.PairID)
		return nil
	case "a_to_b", "b_to_a":
		// mirror
	default:
		return fmt.Errorf("unknown direction: %s", pair.Direction)
	}

	targets := resolveMirrorTargets(*pair, ev.Side)
	for _, t := range targets {
		if err := e.dispatchSync(ctx, srcClientID, *pair, t, ev); err != nil {
			slog.ErrorContext(ctx, "dispatch sync", "target_client", t.ClientID, "error", err)
			e.recordSyncRun(ctx, *pair, srcClientID, t.ClientID, ev.RelPath, ev.SHA256, "failed", err.Error())
			continue
		}
	}

	return nil
}

// HandleFileSyncAck は完了通知を sync_runs に反映する（v0.2.3 では noop）。
func (e *Engine) HandleFileSyncAck(_ context.Context, _ string, _ proto.FileSyncAck) error {
	return nil
}

func (e *Engine) upsertFileState(ctx context.Context, ev proto.FileChangedEvent) error {
	now := time.Now().UnixNano()
	fs := store.FileState{
		PairID:        ev.PairID,
		Side:          ev.Side,
		RelPath:       ev.RelPath,
		Op:            ev.Op,
		IsDir:         ev.IsDir,
		ModTime:       &ev.ModTime,
		ServerModTime: now,
	}
	if ev.SHA256 != "" {
		fs.SHA256 = &ev.SHA256
	}
	if ev.Size > 0 {
		fs.Size = &ev.Size
	}
	if err := e.store.UpsertFileState(ctx, fs); err != nil {
		return fmt.Errorf("upsert file_state: %w", err)
	}

	return nil
}

func (e *Engine) dispatchSync(ctx context.Context, srcClientID string, pair store.SyncPair, t Target, ev proto.FileChangedEvent) error {
	op := "fetch"
	if ev.Op == "delete" {
		op = "delete"
	}

	cmd := proto.FileSyncCommand{
		PairID:             pair.ID,
		Side:               t.Side,
		RelPath:            ev.RelPath,
		SHA256:             ev.SHA256,
		Op:                 op,
		OriginatorClientID: srcClientID,
	}

	payload, err := proto.MarshalPayload(cmd)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	env := proto.Envelope{
		Type:    proto.TypeFileSyncCommand,
		Payload: payload,
	}

	if err := e.sender.Send(ctx, t.ClientID, env); err != nil {
		return err
	}

	e.recordSyncRun(ctx, pair, srcClientID, t.ClientID, ev.RelPath, ev.SHA256, "ok", "")
	return nil
}

func (e *Engine) recordSyncRun(ctx context.Context, pair store.SyncPair, src, dst, rel, sha, status, errMsg string) {
	now := time.Now()
	run := store.SyncRun{
		PairID:      pair.ID,
		SrcClientID: src,
		DstClientID: dst,
		RelPath:     rel,
		Status:      status,
		StartedAt:   now,
		FinishedAt:  now,
	}
	if sha != "" {
		run.SHA256 = &sha
	}
	if errMsg != "" {
		run.Error = &errMsg
	}

	if _, err := e.store.InsertSyncRun(ctx, run); err != nil {
		slog.ErrorContext(ctx, "insert sync_run", "error", err)
	}
}
