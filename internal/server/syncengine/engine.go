package syncengine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
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
	store    store.Store
	sender   Sender
	pairMu   sync.Map // pair_id → *sync.Mutex (per-pair serialization of HandleFileChanged)
}

// New は Engine を生成する。
func New(s store.Store, sender Sender) *Engine {
	return &Engine{store: s, sender: sender}
}

func (e *Engine) pairLock(pairID string) func() {
	v, _ := e.pairMu.LoadOrStore(pairID, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// HandleFileChanged は Hub から受信した FileChangedEvent を処理する。
// 同一 pair の並列処理は per-pair mutex で serialize する。
func (e *Engine) HandleFileChanged(ctx context.Context, srcClientID string, ev proto.FileChangedEvent) error {
	unlock := e.pairLock(ev.PairID)
	defer unlock()

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

	fs, err := e.upsertFileState(ctx, ev)
	if err != nil {
		return err
	}

	if !pair.Enabled {
		return nil
	}

	switch pair.Direction {
	case "bidirectional":
		return e.handleBidirectional(ctx, srcClientID, *pair, ev, fs)
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

func (e *Engine) upsertFileState(ctx context.Context, ev proto.FileChangedEvent) (store.FileState, error) {
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
		return store.FileState{}, fmt.Errorf("upsert file_state: %w", err)
	}

	return fs, nil
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

func (e *Engine) handleBidirectional(
	ctx context.Context,
	srcClientID string,
	pair store.SyncPair,
	ev proto.FileChangedEvent,
	src store.FileState,
) error {
	otherSideName := otherSide(ev.Side)
	otherState, err := e.store.GetFileState(ctx, pair.ID, otherSideName, ev.RelPath)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("get other side state: %w", err)
	}

	otherClient := pair.ClientAID
	otherPath := pair.PathA
	if otherSideName == "b" {
		otherClient = pair.ClientBID
		otherPath = pair.PathB
	}

	srcView := toView(src)
	var otherView *fileStateView
	if otherState != nil {
		v := toView(*otherState)
		otherView = &v
	}

	dec := decideLWW(ev.Side, srcView, otherView, defaultConflictWindowNs)

	switch dec.Action {
	case LWWActionNone:
		return nil
	case LWWActionDispatch:
		return e.dispatchSync(ctx, srcClientID, pair, Target{
			ClientID: otherClient, Side: otherSideName, Path: otherPath,
		}, ev)
	case LWWActionEditWins:
		return e.handleEditWins(ctx, srcClientID, pair, ev, src, otherState, otherClient, otherSideName, otherPath)
	case LWWActionConflict:
		e.auditConflictDetected(ctx, pair.ID, ev.RelPath)
		if src.ServerModTime >= otherState.ServerModTime {
			loserSHA := ""
			if otherState.SHA256 != nil {
				loserSHA = *otherState.SHA256
			}
			return e.dispatchConflict(ctx, pair, ev.Side, src, otherSideName, otherClient, loserSHA)
		}
		loserSHA := ev.SHA256
		return e.dispatchConflict(ctx, pair, otherSideName, *otherState, ev.Side, srcClientID, loserSHA)
	}
	return nil
}

func (e *Engine) handleEditWins(
	ctx context.Context,
	srcClientID string,
	pair store.SyncPair,
	ev proto.FileChangedEvent,
	src store.FileState,
	otherState *store.FileState,
	otherClient, otherSideName, otherPath string,
) error {
	if ev.Op == "write" {
		// ev が write、otherState が delete → ev 側が勝ち、相手に dispatch
		return e.dispatchSync(ctx, srcClientID, pair, Target{
			ClientID: otherClient, Side: otherSideName, Path: otherPath,
		}, ev)
	}

	// ev が delete、otherState が write → otherState が勝ち
	// src 側に otherState を fetch で配布
	e.auditDeleteVsEdit(ctx, pair.ID, ev.RelPath, srcClientID)
	if otherState == nil || otherState.SHA256 == nil {
		return nil
	}
	return e.dispatchFetch(ctx, otherClient, pair, Target{
		ClientID: srcClientID, Side: ev.Side, Path: pathFor(pair, ev.Side),
	}, ev.RelPath, *otherState.SHA256)
}

func (e *Engine) dispatchFetch(
	ctx context.Context,
	srcClientID string,
	pair store.SyncPair,
	target Target,
	relPath, sha string,
) error {
	cmd := proto.FileSyncCommand{
		PairID:             pair.ID,
		Side:               target.Side,
		RelPath:            relPath,
		SHA256:             sha,
		Op:                 "fetch",
		OriginatorClientID: srcClientID,
	}
	payload, err := proto.MarshalPayload(cmd)
	if err != nil {
		return fmt.Errorf("marshal fetch payload: %w", err)
	}
	return e.sender.Send(ctx, target.ClientID, proto.Envelope{Type: proto.TypeFileSyncCommand, Payload: payload})
}

func pathFor(pair store.SyncPair, side string) string {
	if side == "a" {
		return pair.PathA
	}
	return pair.PathB
}

func toView(fs store.FileState) fileStateView {
	return fileStateView{
		Op:            fs.Op,
		SHA256:        fs.SHA256,
		ServerModTime: fs.ServerModTime,
	}
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
