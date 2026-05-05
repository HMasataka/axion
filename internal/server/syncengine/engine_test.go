package syncengine

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/HMasataka/axion/internal/proto"
	"github.com/HMasataka/axion/internal/server/store"
)

type fakeSender struct {
	mu   sync.Mutex
	sent []sentMsg
	err  error
}

type sentMsg struct {
	ClientID string
	Env      proto.Envelope
}

func (f *fakeSender) Send(_ context.Context, clientID string, env proto.Envelope) error {
	if f.err != nil {
		return f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sentMsg{ClientID: clientID, Env: env})
	return nil
}

func (f *fakeSender) messages() []sentMsg {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]sentMsg, len(f.sent))
	copy(out, f.sent)
	return out
}

func openTestDB(t *testing.T) *store.SQLite {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func insertClientPair(t *testing.T, s store.Store, direction string, enabled bool) store.SyncPair {
	t.Helper()
	ctx := context.Background()
	now := time.Now()

	for _, id := range []string{"client-a", "client-b"} {
		c := store.Client{
			ID: id, DisplayName: id, Hostname: "h", RootPath: "/",
			Version: "1", ProtoVersion: "1", Status: "online",
			LastSeen: now, CreatedAt: now, UpdatedAt: now, Etag: 1,
		}
		if err := s.UpsertClient(ctx, c); err != nil {
			t.Fatalf("UpsertClient %s: %v", id, err)
		}
	}

	p := store.SyncPair{
		ID:        "pair-1",
		Name:      "test pair",
		ClientAID: "client-a",
		PathA:     "/path/a",
		ClientBID: "client-b",
		PathB:     "/path/b",
		Direction: direction,
		Enabled:   enabled,
		CreatedAt: now,
		UpdatedAt: now,
		Etag:      1,
	}
	if err := s.UpsertPair(ctx, p); err != nil {
		t.Fatalf("UpsertPair: %v", err)
	}

	return p
}

func makeEvent(side, op string) proto.FileChangedEvent {
	return proto.FileChangedEvent{
		PairID:  "pair-1",
		Side:    side,
		RelPath: "docs/file.txt",
		SHA256:  "abc123",
		Size:    512,
		ModTime: time.Now().UnixNano(),
		Op:      op,
	}
}

func TestHandleFileChanged_MirrorAToB(t *testing.T) {
	// Given: a_to_b pair, ev.Side="a"
	s := openTestDB(t)
	insertClientPair(t, s, "a_to_b", true)
	sender := &fakeSender{}
	eng := New(s, sender)
	ctx := context.Background()
	ev := makeEvent("a", "write")

	// When
	if err := eng.HandleFileChanged(ctx, "client-a", ev); err != nil {
		t.Fatalf("HandleFileChanged: %v", err)
	}

	// Then: client-b に FileSyncCommand が1件
	msgs := sender.messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 sent message, got %d", len(msgs))
	}
	if msgs[0].ClientID != "client-b" {
		t.Errorf("expected client-b, got %s", msgs[0].ClientID)
	}
	if msgs[0].Env.Type != proto.TypeFileSyncCommand {
		t.Errorf("expected TypeFileSyncCommand, got %s", msgs[0].Env.Type)
	}

	var cmd proto.FileSyncCommand
	if err := proto.UnmarshalPayload(msgs[0].Env.Payload, &cmd); err != nil {
		t.Fatalf("UnmarshalPayload: %v", err)
	}
	if cmd.Side != "b" {
		t.Errorf("cmd.Side: want b, got %s", cmd.Side)
	}
	if cmd.Op != "fetch" {
		t.Errorf("cmd.Op: want fetch, got %s", cmd.Op)
	}

	// Then: sync_runs に "ok" 1件
	runs, err := s.ListRecentSyncRuns(ctx, "pair-1", 10)
	if err != nil {
		t.Fatalf("ListRecentSyncRuns: %v", err)
	}
	if len(runs) != 1 || runs[0].Status != "ok" {
		t.Errorf("expected 1 ok run, got %+v", runs)
	}

	// Then: file_state に A 側エントリ
	fs, err := s.GetFileState(ctx, "pair-1", "a", "docs/file.txt")
	if err != nil {
		t.Fatalf("GetFileState: %v", err)
	}
	if fs == nil {
		t.Fatal("expected file_state entry, got nil")
	}
}

func TestHandleFileChanged_MirrorAToB_IgnoresBSideChanges(t *testing.T) {
	// Given: a_to_b pair, ev.Side="b"
	s := openTestDB(t)
	insertClientPair(t, s, "a_to_b", true)
	sender := &fakeSender{}
	eng := New(s, sender)
	ctx := context.Background()
	ev := makeEvent("b", "write")

	// When
	if err := eng.HandleFileChanged(ctx, "client-b", ev); err != nil {
		t.Fatalf("HandleFileChanged: %v", err)
	}

	// Then: 何も送られない
	if len(sender.messages()) != 0 {
		t.Errorf("expected 0 sent messages, got %d", len(sender.messages()))
	}

	// Then: file_state は更新される
	fs, err := s.GetFileState(ctx, "pair-1", "b", "docs/file.txt")
	if err != nil {
		t.Fatalf("GetFileState: %v", err)
	}
	if fs == nil {
		t.Fatal("expected file_state entry, got nil")
	}
}

func TestHandleFileChanged_MirrorBToA(t *testing.T) {
	// Given: b_to_a pair, ev.Side="b"
	s := openTestDB(t)
	insertClientPair(t, s, "b_to_a", true)
	sender := &fakeSender{}
	eng := New(s, sender)
	ctx := context.Background()
	ev := makeEvent("b", "write")

	// When
	if err := eng.HandleFileChanged(ctx, "client-b", ev); err != nil {
		t.Fatalf("HandleFileChanged: %v", err)
	}

	// Then: client-a に FileSyncCommand が1件
	msgs := sender.messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 sent message, got %d", len(msgs))
	}
	if msgs[0].ClientID != "client-a" {
		t.Errorf("expected client-a, got %s", msgs[0].ClientID)
	}

	var cmd proto.FileSyncCommand
	if err := proto.UnmarshalPayload(msgs[0].Env.Payload, &cmd); err != nil {
		t.Fatalf("UnmarshalPayload: %v", err)
	}
	if cmd.Side != "a" {
		t.Errorf("cmd.Side: want a, got %s", cmd.Side)
	}

	// ev.Side="a" は無視される
	sender2 := &fakeSender{}
	eng2 := New(s, sender2)
	evA := makeEvent("a", "write")
	if err := eng2.HandleFileChanged(ctx, "client-a", evA); err != nil {
		t.Fatalf("HandleFileChanged side=a: %v", err)
	}
	if len(sender2.messages()) != 0 {
		t.Errorf("expected 0 messages for a_side in b_to_a, got %d", len(sender2.messages()))
	}
}

func TestHandleFileChanged_Bidirectional_OtherStateNil_Dispatch(t *testing.T) {
	// Given: bidirectional pair, 相手側に file_state なし
	s := openTestDB(t)
	insertClientPair(t, s, "bidirectional", true)
	sender := &fakeSender{}
	eng := New(s, sender)
	ctx := context.Background()
	ev := makeEvent("a", "write")

	// When
	if err := eng.HandleFileChanged(ctx, "client-a", ev); err != nil {
		t.Fatalf("HandleFileChanged: %v", err)
	}

	// Then: client-b に dispatch される
	msgs := sender.messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 sent message, got %d", len(msgs))
	}
	if msgs[0].ClientID != "client-b" {
		t.Errorf("expected client-b, got %s", msgs[0].ClientID)
	}

	// Then: file_state は更新される
	fs, err := s.GetFileState(ctx, "pair-1", "a", "docs/file.txt")
	if err != nil {
		t.Fatalf("GetFileState: %v", err)
	}
	if fs == nil {
		t.Fatal("expected file_state entry, got nil")
	}
}

func TestHandleFileChanged_DisabledPair_NoDispatch(t *testing.T) {
	// Given: disabled pair
	s := openTestDB(t)
	insertClientPair(t, s, "a_to_b", false)
	sender := &fakeSender{}
	eng := New(s, sender)
	ctx := context.Background()
	ev := makeEvent("a", "write")

	// When
	if err := eng.HandleFileChanged(ctx, "client-a", ev); err != nil {
		t.Fatalf("HandleFileChanged: %v", err)
	}

	// Then: 何も送られない
	if len(sender.messages()) != 0 {
		t.Errorf("expected 0 sent messages, got %d", len(sender.messages()))
	}

	// Then: file_state は更新される
	fs, err := s.GetFileState(ctx, "pair-1", "a", "docs/file.txt")
	if err != nil {
		t.Fatalf("GetFileState: %v", err)
	}
	if fs == nil {
		t.Fatal("expected file_state entry, got nil")
	}
}

func TestHandleFileChanged_UnknownPair_LogsAndSkips(t *testing.T) {
	// Given: pair_id 未登録
	s := openTestDB(t)
	sender := &fakeSender{}
	eng := New(s, sender)
	ctx := context.Background()

	ev := proto.FileChangedEvent{
		PairID:  "no-such-pair",
		Side:    "a",
		RelPath: "foo.txt",
		Op:      "write",
		ModTime: time.Now().UnixNano(),
	}

	// When / Then: エラーなし、何もしない
	if err := eng.HandleFileChanged(ctx, "client-a", ev); err != nil {
		t.Fatalf("HandleFileChanged: %v", err)
	}
	if len(sender.messages()) != 0 {
		t.Errorf("expected 0 sent messages, got %d", len(sender.messages()))
	}
}

func TestHandleFileChanged_DeleteOp(t *testing.T) {
	// Given: a_to_b pair, ev.Op="delete"
	s := openTestDB(t)
	insertClientPair(t, s, "a_to_b", true)
	sender := &fakeSender{}
	eng := New(s, sender)
	ctx := context.Background()
	ev := makeEvent("a", "delete")
	ev.SHA256 = ""
	ev.Size = 0

	// When
	if err := eng.HandleFileChanged(ctx, "client-a", ev); err != nil {
		t.Fatalf("HandleFileChanged: %v", err)
	}

	// Then: cmd.Op="delete"
	msgs := sender.messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 sent message, got %d", len(msgs))
	}

	var cmd proto.FileSyncCommand
	if err := proto.UnmarshalPayload(msgs[0].Env.Payload, &cmd); err != nil {
		t.Fatalf("UnmarshalPayload: %v", err)
	}
	if cmd.Op != "delete" {
		t.Errorf("cmd.Op: want delete, got %s", cmd.Op)
	}
}

func TestHandleFileChanged_RecordsSyncRun(t *testing.T) {
	// Given: a_to_b pair
	s := openTestDB(t)
	insertClientPair(t, s, "a_to_b", true)
	sender := &fakeSender{}
	eng := New(s, sender)
	ctx := context.Background()
	ev := makeEvent("a", "write")

	// When
	if err := eng.HandleFileChanged(ctx, "client-a", ev); err != nil {
		t.Fatalf("HandleFileChanged: %v", err)
	}

	// Then: sync_runs に1行追加
	runs, err := s.ListRecentSyncRuns(ctx, "pair-1", 10)
	if err != nil {
		t.Fatalf("ListRecentSyncRuns: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 sync run, got %d", len(runs))
	}
	if runs[0].SrcClientID != "client-a" {
		t.Errorf("SrcClientID: want client-a, got %s", runs[0].SrcClientID)
	}
	if runs[0].DstClientID != "client-b" {
		t.Errorf("DstClientID: want client-b, got %s", runs[0].DstClientID)
	}
	if runs[0].Status != "ok" {
		t.Errorf("Status: want ok, got %s", runs[0].Status)
	}
}

func TestHandleFileChanged_SenderFails_RecordsFailedRun(t *testing.T) {
	// Given: a_to_b pair, sender が error を返す
	s := openTestDB(t)
	insertClientPair(t, s, "a_to_b", true)
	sender := &fakeSender{err: errors.New("network error")}
	eng := New(s, sender)
	ctx := context.Background()
	ev := makeEvent("a", "write")

	// When
	if err := eng.HandleFileChanged(ctx, "client-a", ev); err != nil {
		t.Fatalf("HandleFileChanged: %v", err)
	}

	// Then: sync_runs に "failed" で記録
	runs, err := s.ListRecentSyncRuns(ctx, "pair-1", 10)
	if err != nil {
		t.Fatalf("ListRecentSyncRuns: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 sync run, got %d", len(runs))
	}
	if runs[0].Status != "failed" {
		t.Errorf("Status: want failed, got %s", runs[0].Status)
	}
	if runs[0].Error == nil || *runs[0].Error == "" {
		t.Error("expected non-empty error message in sync run")
	}
}

// upsertFileStateForTest は engine の upsertFileState を通してファイル状態を DB に登録するヘルパー。
// bidirectional テストで相手側の状態を事前設定するために使う。
func upsertFileStateForTest(t *testing.T, s store.Store, fs store.FileState) {
	t.Helper()
	if err := s.UpsertFileState(context.Background(), fs); err != nil {
		t.Fatalf("UpsertFileState: %v", err)
	}
}

func TestHandleFileChanged_Bidirectional_Dispatch(t *testing.T) {
	// Given: bidirectional + otherState=nil → 通常 dispatch
	s := openTestDB(t)
	insertClientPair(t, s, "bidirectional", true)
	sender := &fakeSender{}
	eng := New(s, sender)
	ctx := context.Background()
	ev := makeEvent("a", "write")

	// When
	if err := eng.HandleFileChanged(ctx, "client-a", ev); err != nil {
		t.Fatalf("HandleFileChanged: %v", err)
	}

	// Then: client-b に FileSyncCommand 1件
	msgs := sender.messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 sent message, got %d", len(msgs))
	}
	if msgs[0].ClientID != "client-b" {
		t.Errorf("expected client-b, got %s", msgs[0].ClientID)
	}
	var cmd proto.FileSyncCommand
	if err := proto.UnmarshalPayload(msgs[0].Env.Payload, &cmd); err != nil {
		t.Fatalf("UnmarshalPayload: %v", err)
	}
	if cmd.Op != "fetch" {
		t.Errorf("cmd.Op: want fetch, got %s", cmd.Op)
	}
}

func TestHandleFileChanged_Bidirectional_Conflict(t *testing.T) {
	// Given: bidirectional + sha 不一致 + 同時刻 → rename + fetch の 2 件送信 + audit
	s := openTestDB(t)
	insertClientPair(t, s, "bidirectional", true)
	sender := &fakeSender{}
	eng := New(s, sender)
	ctx := context.Background()

	// 相手側 (b) に事前に write を登録 (同時刻とみなす: 差を 1sec 以内にする)
	now := time.Now().UnixNano()
	bSHA := "b-hash"
	upsertFileStateForTest(t, s, store.FileState{
		PairID:        "pair-1",
		Side:          "b",
		RelPath:       "docs/file.txt",
		SHA256:        &bSHA,
		Op:            "write",
		ServerModTime: now - int64(500*time.Millisecond),
		ModTime:       &now,
	})

	// a 側のイベント: 異なる sha、ほぼ同時刻
	ev := proto.FileChangedEvent{
		PairID:  "pair-1",
		Side:    "a",
		RelPath: "docs/file.txt",
		SHA256:  "a-hash",
		Size:    512,
		ModTime: now,
		Op:      "write",
	}

	// When
	if err := eng.HandleFileChanged(ctx, "client-a", ev); err != nil {
		t.Fatalf("HandleFileChanged: %v", err)
	}

	// Then: rename + loser fetch + winner fetch の 3 件送信
	msgs := sender.messages()
	if len(msgs) != 3 {
		t.Fatalf("expected 3 sent messages (rename+loser fetch+winner fetch), got %d", len(msgs))
	}
	var renameCmd proto.FileSyncCommand
	if err := proto.UnmarshalPayload(msgs[0].Env.Payload, &renameCmd); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if renameCmd.Op != "rename" {
		t.Errorf("msgs[0].Op: want rename, got %s", renameCmd.Op)
	}
	var loserFetch proto.FileSyncCommand
	if err := proto.UnmarshalPayload(msgs[1].Env.Payload, &loserFetch); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if loserFetch.Op != "fetch" {
		t.Errorf("msgs[1].Op: want fetch, got %s", loserFetch.Op)
	}
	var winnerFetch proto.FileSyncCommand
	if err := proto.UnmarshalPayload(msgs[2].Env.Payload, &winnerFetch); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if winnerFetch.Op != "fetch" {
		t.Errorf("msgs[2].Op: want fetch, got %s", winnerFetch.Op)
	}

	// Then: audit_log に conflict_detected + conflict_renamed
	logs, err := s.ListRecentAuditLog(ctx, 10)
	if err != nil {
		t.Fatalf("ListRecentAuditLog: %v", err)
	}
	kinds := map[string]int{}
	for _, l := range logs {
		kinds[l.Kind]++
	}
	if kinds["conflict_detected"] == 0 {
		t.Error("expected conflict_detected audit entry")
	}
	if kinds["conflict_renamed"] == 0 {
		t.Error("expected conflict_renamed audit entry")
	}
}

func TestHandleFileChanged_Bidirectional_DeleteVsEdit_DeleteSideAck(t *testing.T) {
	// Given: 削除側がイベント送信、相手は write → 削除側に fetch + audit "delete_vs_edit"
	s := openTestDB(t)
	insertClientPair(t, s, "bidirectional", true)
	sender := &fakeSender{}
	eng := New(s, sender)
	ctx := context.Background()

	// 相手側 (b) に事前に write を登録
	now := time.Now().UnixNano()
	bSHA := "b-existing-hash"
	upsertFileStateForTest(t, s, store.FileState{
		PairID:        "pair-1",
		Side:          "b",
		RelPath:       "docs/file.txt",
		SHA256:        &bSHA,
		Op:            "write",
		ServerModTime: now - int64(30*time.Second),
		ModTime:       &now,
	})

	// a 側が delete を送信
	ev := proto.FileChangedEvent{
		PairID:  "pair-1",
		Side:    "a",
		RelPath: "docs/file.txt",
		Op:      "delete",
		ModTime: now,
	}

	// When
	if err := eng.HandleFileChanged(ctx, "client-a", ev); err != nil {
		t.Fatalf("HandleFileChanged: %v", err)
	}

	// Then: 削除側 (client-a) に fetch が送られる
	msgs := sender.messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 sent message (fetch to delete side), got %d", len(msgs))
	}
	if msgs[0].ClientID != "client-a" {
		t.Errorf("expected fetch to client-a (delete side), got %s", msgs[0].ClientID)
	}
	var cmd proto.FileSyncCommand
	if err := proto.UnmarshalPayload(msgs[0].Env.Payload, &cmd); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cmd.Op != "fetch" {
		t.Errorf("cmd.Op: want fetch, got %s", cmd.Op)
	}
	if cmd.SHA256 != bSHA {
		t.Errorf("cmd.SHA256: want %s, got %s", bSHA, cmd.SHA256)
	}

	// Then: audit_log に delete_vs_edit
	logs, err := s.ListRecentAuditLog(ctx, 10)
	if err != nil {
		t.Fatalf("ListRecentAuditLog: %v", err)
	}
	var found bool
	for _, l := range logs {
		if l.Kind == "delete_vs_edit" {
			found = true
		}
	}
	if !found {
		t.Error("expected delete_vs_edit audit entry")
	}
}

func TestHandleFileChanged_Bidirectional_DeleteVsEdit_EditSideAck(t *testing.T) {
	// Given: 編集側がイベント送信、相手は delete → 削除側に通常 dispatch (上書き)
	s := openTestDB(t)
	insertClientPair(t, s, "bidirectional", true)
	sender := &fakeSender{}
	eng := New(s, sender)
	ctx := context.Background()

	// 相手側 (b) に事前に delete を登録
	now := time.Now().UnixNano()
	upsertFileStateForTest(t, s, store.FileState{
		PairID:        "pair-1",
		Side:          "b",
		RelPath:       "docs/file.txt",
		SHA256:        nil,
		Op:            "delete",
		ServerModTime: now - int64(30*time.Second),
		ModTime:       &now,
	})

	// a 側が write を送信
	ev := proto.FileChangedEvent{
		PairID:  "pair-1",
		Side:    "a",
		RelPath: "docs/file.txt",
		SHA256:  "new-content-hash",
		Size:    1024,
		Op:      "write",
		ModTime: now,
	}

	// When
	if err := eng.HandleFileChanged(ctx, "client-a", ev); err != nil {
		t.Fatalf("HandleFileChanged: %v", err)
	}

	// Then: 削除側 (client-b) に fetch が dispatch される
	msgs := sender.messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 sent message, got %d", len(msgs))
	}
	if msgs[0].ClientID != "client-b" {
		t.Errorf("expected client-b, got %s", msgs[0].ClientID)
	}
	var cmd proto.FileSyncCommand
	if err := proto.UnmarshalPayload(msgs[0].Env.Payload, &cmd); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cmd.Op != "fetch" {
		t.Errorf("cmd.Op: want fetch, got %s", cmd.Op)
	}
}

func TestHandleFileChanged_Bidirectional_BothDelete_None(t *testing.T) {
	// Given: 両側 delete → 送信なし
	s := openTestDB(t)
	insertClientPair(t, s, "bidirectional", true)
	sender := &fakeSender{}
	eng := New(s, sender)
	ctx := context.Background()

	// 相手側 (b) に事前に delete を登録
	now := time.Now().UnixNano()
	upsertFileStateForTest(t, s, store.FileState{
		PairID:        "pair-1",
		Side:          "b",
		RelPath:       "docs/file.txt",
		SHA256:        nil,
		Op:            "delete",
		ServerModTime: now - int64(2*time.Second),
		ModTime:       &now,
	})

	// a 側も delete
	ev := proto.FileChangedEvent{
		PairID:  "pair-1",
		Side:    "a",
		RelPath: "docs/file.txt",
		Op:      "delete",
		ModTime: now,
	}

	// When
	if err := eng.HandleFileChanged(ctx, "client-a", ev); err != nil {
		t.Fatalf("HandleFileChanged: %v", err)
	}

	// Then: 送信なし
	if len(sender.messages()) != 0 {
		t.Errorf("expected 0 sent messages, got %d", len(sender.messages()))
	}
}
