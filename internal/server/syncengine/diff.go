package syncengine

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/HMasataka/axion/internal/proto"
	"github.com/HMasataka/axion/internal/server/store"
	"github.com/google/uuid"
)

// SyncRequester は SendAndWait のための interface (hub への抽象化)。
type SyncRequester interface {
	SendAndWait(ctx context.Context, clientID string, env proto.Envelope, timeout time.Duration) (proto.Envelope, error)
}

// RequestSnapshotAndDiff は指定 client + pair の現状スナップショットを要求し、
// file_state と diff して必要な FileSyncCommand を発行する。
// 接続時/ペア有効化時に呼ぶ。
func (e *Engine) RequestSnapshotAndDiff(ctx context.Context, requester SyncRequester, clientID, pairID, side string) error {
	pair, err := e.store.GetPair(ctx, pairID)
	if err != nil {
		return err
	}
	if pair == nil {
		return nil
	}

	req := proto.ListFilesRequest{PairID: pairID, Side: side}
	payload, err := proto.MarshalPayload(req)
	if err != nil {
		return fmt.Errorf("marshal list files request: %w", err)
	}
	env := proto.Envelope{
		Type:          proto.TypeListFilesRequest,
		CorrelationID: uuid.NewString(),
		Payload:       payload,
	}

	respEnv, err := requester.SendAndWait(ctx, clientID, env, 30*time.Second)
	if err != nil {
		return fmt.Errorf("send list files: %w", err)
	}

	var resp proto.ListFilesResponse
	if err := proto.UnmarshalPayload(respEnv.Payload, &resp); err != nil {
		return err
	}

	return e.applyDiff(ctx, clientID, pairID, side, resp.Entries)
}

func (e *Engine) applyDiff(ctx context.Context, clientID, pairID, side string, snapshots []proto.FileSnapshot) error {
	dbStates, err := e.store.ListFileStates(ctx, pairID, side)
	if err != nil {
		return err
	}

	dbByPath := make(map[string]store.FileState, len(dbStates))
	for _, s := range dbStates {
		dbByPath[s.RelPath] = s
	}

	clientByPath := make(map[string]proto.FileSnapshot, len(snapshots))
	for _, snap := range snapshots {
		clientByPath[snap.RelPath] = snap
	}

	for _, snap := range snapshots {
		if snap.IsDir {
			continue
		}
		dbState, hasDB := dbByPath[snap.RelPath]
		needUpdate := !hasDB
		if hasDB && dbState.SHA256 != nil && *dbState.SHA256 != snap.SHA256 {
			needUpdate = true
		}
		if !needUpdate {
			continue
		}

		ev := proto.FileChangedEvent{
			PairID:  pairID,
			Side:    side,
			RelPath: snap.RelPath,
			SHA256:  snap.SHA256,
			Size:    snap.Size,
			ModTime: snap.ModTime,
			Op:      "write",
			IsDir:   false,
		}
		if err := e.HandleFileChanged(ctx, clientID, ev); err != nil {
			slog.WarnContext(ctx, "diff dispatch", "rel", snap.RelPath, "error", err)
		}
	}

	for _, dbState := range dbStates {
		if dbState.IsDir {
			continue
		}
		if _, ok := clientByPath[dbState.RelPath]; ok {
			continue
		}
		if dbState.Op == "delete" {
			continue
		}
		ev := proto.FileChangedEvent{
			PairID:  pairID,
			Side:    side,
			RelPath: dbState.RelPath,
			Op:      "delete",
		}
		if err := e.HandleFileChanged(ctx, clientID, ev); err != nil {
			slog.WarnContext(ctx, "diff delete dispatch", "rel", dbState.RelPath, "error", err)
		}
	}

	return nil
}
