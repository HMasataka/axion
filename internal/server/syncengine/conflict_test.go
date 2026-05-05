package syncengine

import (
	"context"
	"testing"

	"github.com/HMasataka/axion/internal/proto"
	"github.com/HMasataka/axion/internal/server/store"
)

func TestConflictFilename_Format(t *testing.T) {
	// Given
	rel := "foo.txt"
	clientID := "abcd1234efgh"
	ts := int64(1234567890)

	// When
	got := conflictFilename(rel, clientID, ts)

	// Then
	want := "foo.txt.conflict-abcd1234-1234567890"
	if got != want {
		t.Errorf("conflictFilename: want %q, got %q", want, got)
	}
}

func TestConflictFilename_ShortClientID(t *testing.T) {
	// Given: client_id が 8 文字未満
	rel := "bar.md"
	clientID := "short"
	ts := int64(999)

	// When
	got := conflictFilename(rel, clientID, ts)

	// Then: client_id をそのまま使う
	want := "bar.md.conflict-short-999"
	if got != want {
		t.Errorf("conflictFilename short: want %q, got %q", want, got)
	}
}

func TestDispatchConflict_SendsRenameAndFetch(t *testing.T) {
	// Given
	s := openTestDB(t)
	pair := insertClientPair(t, s, "bidirectional", true)
	sender := &fakeSender{}
	eng := New(s, sender)
	ctx := context.Background()

	sha := "winner-sha"
	winner := store.FileState{
		PairID:  pair.ID,
		Side:    "a",
		RelPath: "docs/file.txt",
		SHA256:  &sha,
		Op:      "write",
	}

	// When
	err := eng.dispatchConflict(ctx, pair, "a", winner, "b", "client-b", "loser-sha")
	if err != nil {
		t.Fatalf("dispatchConflict: %v", err)
	}

	// Then: 3 件送信 (loser: rename, loser: fetch, winner: fetch)
	msgs := sender.messages()
	if len(msgs) != 3 {
		t.Fatalf("expected 3 sent messages, got %d", len(msgs))
	}

	// msgs[0]: loser への rename
	var renameCmd proto.FileSyncCommand
	if err := proto.UnmarshalPayload(msgs[0].Env.Payload, &renameCmd); err != nil {
		t.Fatalf("unmarshal rename cmd: %v", err)
	}
	if msgs[0].ClientID != "client-b" {
		t.Errorf("msgs[0].ClientID: want client-b, got %s", msgs[0].ClientID)
	}
	if renameCmd.Op != "rename" {
		t.Errorf("msgs[0].Op: want rename, got %s", renameCmd.Op)
	}
	if renameCmd.RelPath != "docs/file.txt" {
		t.Errorf("msgs[0].RelPath: want docs/file.txt, got %s", renameCmd.RelPath)
	}
	if renameCmd.NewRelPath == "" {
		t.Error("msgs[0].NewRelPath: want non-empty")
	}

	// msgs[1]: loser への fetch (勝者コンテンツを取得)
	var loserFetch proto.FileSyncCommand
	if err := proto.UnmarshalPayload(msgs[1].Env.Payload, &loserFetch); err != nil {
		t.Fatalf("unmarshal loser fetch cmd: %v", err)
	}
	if msgs[1].ClientID != "client-b" {
		t.Errorf("msgs[1].ClientID: want client-b, got %s", msgs[1].ClientID)
	}
	if loserFetch.Op != "fetch" {
		t.Errorf("msgs[1].Op: want fetch, got %s", loserFetch.Op)
	}
	if loserFetch.SHA256 != "winner-sha" {
		t.Errorf("msgs[1].SHA256: want winner-sha, got %s", loserFetch.SHA256)
	}

	// msgs[2]: winner への fetch (先行 dispatch による上書き補正)
	var winnerFetch proto.FileSyncCommand
	if err := proto.UnmarshalPayload(msgs[2].Env.Payload, &winnerFetch); err != nil {
		t.Fatalf("unmarshal winner fetch cmd: %v", err)
	}
	if winnerFetch.Op != "fetch" {
		t.Errorf("msgs[2].Op: want fetch, got %s", winnerFetch.Op)
	}
	if winnerFetch.SHA256 != "winner-sha" {
		t.Errorf("msgs[2].SHA256: want winner-sha, got %s", winnerFetch.SHA256)
	}
}

func TestDispatchConflict_RecordsAuditLog(t *testing.T) {
	// Given
	s := openTestDB(t)
	pair := insertClientPair(t, s, "bidirectional", true)
	sender := &fakeSender{}
	eng := New(s, sender)
	ctx := context.Background()

	sha := "winner-sha"
	winner := store.FileState{
		PairID:  pair.ID,
		Side:    "a",
		RelPath: "docs/file.txt",
		SHA256:  &sha,
		Op:      "write",
	}

	// When
	if err := eng.dispatchConflict(ctx, pair, "a", winner, "b", "client-b", "loser-sha"); err != nil {
		t.Fatalf("dispatchConflict: %v", err)
	}

	// Then: audit_log に "conflict_renamed" 1 件
	logs, err := s.ListRecentAuditLog(ctx, 10)
	if err != nil {
		t.Fatalf("ListRecentAuditLog: %v", err)
	}
	var found int
	for _, l := range logs {
		if l.Kind == "conflict_renamed" {
			found++
		}
	}
	if found != 1 {
		t.Errorf("expected 1 conflict_renamed audit entry, got %d", found)
	}
}
