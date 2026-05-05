package syncengine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/HMasataka/axion/internal/proto"
	"github.com/HMasataka/axion/internal/server/store"
)

// conflictFilename は <name>.conflict-<short>-<unix_ns> 形式の名前を生成する。
// e.g. "foo.txt" → "foo.txt.conflict-abcd1234-1714999999000000000"
// shortClientID は client_id の先頭 8 文字。
func conflictFilename(rel, clientID string, ts int64) string {
	short := clientID
	if len(short) > 8 {
		short = short[:8]
	}
	return rel + ".conflict-" + short + "-" + fmt.Sprint(ts)
}

// dispatchConflict は負け側クライアントに rename コマンドを送り、勝ち側コンテンツを両側に行き渡らせる。
//  1. FileSyncCommand{Op:"rename"} を負け側に送信
//  2. FileSyncCommand{Op:"fetch"} を負け側に送信 (勝者ファイルを取得)
//  3. FileSyncCommand{Op:"fetch"} を勝者側に送信 (先行 dispatch で上書きされた場合の補正)
//  4. audit_log: kind="conflict_renamed"
func (e *Engine) dispatchConflict(
	ctx context.Context,
	pair store.SyncPair,
	winnerSide string,
	winner store.FileState,
	loserSide, loserClient, loserSHA string,
) error {
	loserName := winner.RelPath
	now := time.Now().UnixNano()
	newName := conflictFilename(loserName, loserClient, now)

	renameCmd := proto.FileSyncCommand{
		PairID:             pair.ID,
		Side:               loserSide,
		RelPath:            loserName,
		NewRelPath:         newName,
		Op:                 "rename",
		OriginatorClientID: loserClient,
	}
	payload, err := proto.MarshalPayload(renameCmd)
	if err != nil {
		return fmt.Errorf("marshal rename: %w", err)
	}
	if err := e.sender.Send(ctx, loserClient, proto.Envelope{Type: proto.TypeFileSyncCommand, Payload: payload}); err != nil {
		return fmt.Errorf("send rename: %w", err)
	}

	winnerSHA := ""
	if winner.SHA256 != nil {
		winnerSHA = *winner.SHA256
	}

	// 負け側に勝者ファイルを fetch させる。
	loserFetch := proto.FileSyncCommand{
		PairID:             pair.ID,
		Side:               loserSide,
		RelPath:            loserName,
		SHA256:             winnerSHA,
		Op:                 "fetch",
		OriginatorClientID: loserClient,
	}
	payload, err = proto.MarshalPayload(loserFetch)
	if err != nil {
		return fmt.Errorf("marshal loser fetch: %w", err)
	}
	if err := e.sender.Send(ctx, loserClient, proto.Envelope{Type: proto.TypeFileSyncCommand, Payload: payload}); err != nil {
		return fmt.Errorf("send loser fetch: %w", err)
	}

	// 勝者側が先行 dispatch で負け側ファイルをダウンロード済みの可能性があるため、
	// 勝者側にも自分のファイルを fetch させて一貫性を確保する。
	winnerClient := pair.ClientAID
	if winnerSide == "b" {
		winnerClient = pair.ClientBID
	}
	winnerFetch := proto.FileSyncCommand{
		PairID:             pair.ID,
		Side:               winnerSide,
		RelPath:            loserName,
		SHA256:             winnerSHA,
		Op:                 "fetch",
		OriginatorClientID: winnerClient,
	}
	payload, err = proto.MarshalPayload(winnerFetch)
	if err != nil {
		return fmt.Errorf("marshal winner fetch: %w", err)
	}
	if err := e.sender.Send(ctx, winnerClient, proto.Envelope{Type: proto.TypeFileSyncCommand, Payload: payload}); err != nil {
		return fmt.Errorf("send winner fetch: %w", err)
	}

	e.auditConflictRenamed(ctx, pair.ID, loserName, newName)
	return nil
}

func (e *Engine) auditConflictDetected(ctx context.Context, pairID, relPath string) {
	e.audit(ctx, "conflict_detected", nil, &pairID, &relPath, "")
}

func (e *Engine) auditConflictRenamed(ctx context.Context, pairID, relPath, newName string) {
	d, _ := json.Marshal(map[string]string{"new_name": newName})
	ds := string(d)
	e.audit(ctx, "conflict_renamed", nil, &pairID, &relPath, ds)
}

func (e *Engine) auditDeleteVsEdit(ctx context.Context, pairID, relPath, srcClient string) {
	d, _ := json.Marshal(map[string]string{"deleted_by": srcClient})
	ds := string(d)
	e.audit(ctx, "delete_vs_edit", nil, &pairID, &relPath, ds)
}

func (e *Engine) audit(ctx context.Context, kind string, clientID, pairID, relPath *string, detail string) {
	entry := store.AuditEntry{
		TS:       time.Now(),
		Kind:     kind,
		ClientID: clientID,
		PairID:   pairID,
		RelPath:  relPath,
	}
	if detail != "" {
		entry.Detail = &detail
	}
	if err := e.store.AppendAuditLog(ctx, entry); err != nil {
		slog.WarnContext(ctx, "audit log failed", "kind", kind, "error", err)
	}
}
