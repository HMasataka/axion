package runner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/HMasataka/axion/internal/client/transfer"
	"github.com/HMasataka/axion/internal/clientfs"
	"github.com/HMasataka/axion/internal/proto"
)

// --- fakes ---

type fakeSender struct {
	mu   sync.Mutex
	sent []proto.Envelope
}

func (f *fakeSender) Send(_ context.Context, env proto.Envelope) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, env)
	return nil
}

func (f *fakeSender) Received() []proto.Envelope {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]proto.Envelope, len(f.sent))
	copy(out, f.sent)
	return out
}

type fakeTransferClient struct {
	uploadErr   error
	downloadErr error
}

func (f *fakeTransferClient) Upload(_ context.Context, _ string) (transfer.UploadResult, error) {
	if f.uploadErr != nil {
		return transfer.UploadResult{}, f.uploadErr
	}
	return transfer.UploadResult{SHA256: "fakeSHA", Size: 0, Skipped: false}, nil
}

func (f *fakeTransferClient) Download(_ context.Context, _, _ string) error {
	return f.downloadErr
}

// makeRunner は t.TempDir() を使った Jail と fake 依存を持つ Runner を返す。
func makeRunner(t *testing.T, transferClient TransferClient) (*Runner, *fakeSender) {
	t.Helper()
	dir := t.TempDir()
	jail, err := clientfs.New(dir)
	if err != nil {
		t.Fatalf("clientfs.New: %v", err)
	}
	sender := &fakeSender{}
	if transferClient == nil {
		transferClient = &fakeTransferClient{}
	}
	r, err := New(Config{
		Jail:     jail,
		Transfer: transferClient,
		Sender:   sender,
	})
	if err != nil {
		t.Fatalf("runner.New: %v", err)
	}
	return r, sender
}

func mustMarshalPayload(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := proto.MarshalPayload(v)
	if err != nil {
		t.Fatalf("MarshalPayload: %v", err)
	}
	return b
}

// --- tests ---

func TestRunner_HandleSubscribePair_Updates(t *testing.T) {
	r, _ := makeRunner(t, nil)
	ctx := context.Background()

	sub := proto.SubscribePair{
		PairID:      "pair-1",
		Side:        "a",
		RootSubpath: "work",
		Direction:   "bidirectional",
	}
	env := proto.Envelope{
		Type:    proto.TypeSubscribePair,
		Payload: mustMarshalPayload(t, sub),
	}

	if err := r.HandleEnvelope(ctx, env); err != nil {
		t.Fatalf("HandleEnvelope: %v", err)
	}

	all := r.reg.All()
	if len(all) != 1 {
		t.Fatalf("expected 1 subscription, got %d", len(all))
	}
	if all[0].PairID != "pair-1" {
		t.Errorf("expected PairID=pair-1, got %s", all[0].PairID)
	}
}

func TestRunner_HandleSubscribePair_EmptyDirection_Removes(t *testing.T) {
	r, _ := makeRunner(t, nil)
	ctx := context.Background()

	// まず登録
	add := proto.Envelope{
		Type: proto.TypeSubscribePair,
		Payload: mustMarshalPayload(t, proto.SubscribePair{
			PairID:    "pair-x",
			Side:      "b",
			Direction: "bidirectional",
		}),
	}
	if err := r.HandleEnvelope(ctx, add); err != nil {
		t.Fatalf("add: %v", err)
	}

	// Direction="" で削除
	remove := proto.Envelope{
		Type: proto.TypeSubscribePair,
		Payload: mustMarshalPayload(t, proto.SubscribePair{
			PairID:    "pair-x",
			Side:      "b",
			Direction: "",
		}),
	}
	if err := r.HandleEnvelope(ctx, remove); err != nil {
		t.Fatalf("remove: %v", err)
	}

	if len(r.reg.All()) != 0 {
		t.Fatalf("expected 0 subscriptions after removal, got %d", len(r.reg.All()))
	}
}

func TestRunner_HandleFileSyncCommand_Fetch(t *testing.T) {
	r, sender := makeRunner(t, nil)
	ctx := context.Background()

	// subscription を先に登録
	r.reg.Apply(proto.SubscribePair{
		PairID:      "pair-1",
		Side:        "a",
		RootSubpath: "",
		Direction:   "bidirectional",
	})

	cmd := proto.FileSyncCommand{
		PairID:  "pair-1",
		Side:    "a",
		RelPath: "hello.txt",
		SHA256:  "abc",
		Op:      "fetch",
	}
	env := proto.Envelope{
		Type:    proto.TypeFileSyncCommand,
		Payload: mustMarshalPayload(t, cmd),
	}

	if err := r.HandleEnvelope(ctx, env); err != nil {
		t.Fatalf("HandleEnvelope: %v", err)
	}

	// handleSyncCommand は goroutine で動くため少し待つ
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if len(sender.Received()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	received := sender.Received()
	if len(received) != 1 {
		t.Fatalf("expected 1 ack, got %d", len(received))
	}
	if received[0].Type != proto.TypeFileSyncAck {
		t.Errorf("expected TypeFileSyncAck, got %s", received[0].Type)
	}
	var ack proto.FileSyncAck
	if err := proto.UnmarshalPayload(received[0].Payload, &ack); err != nil {
		t.Fatalf("unmarshal ack: %v", err)
	}
	if ack.Status != "ok" {
		t.Errorf("expected status=ok, got %s (error=%s)", ack.Status, ack.Error)
	}
}

func TestRunner_HandleFileSyncCommand_Delete(t *testing.T) {
	r, sender := makeRunner(t, nil)
	ctx := context.Background()

	r.reg.Apply(proto.SubscribePair{
		PairID:    "pair-2",
		Side:      "a",
		Direction: "bidirectional",
	})

	// 存在しないファイルの delete は ErrNotExist として ok を返す
	cmd := proto.FileSyncCommand{
		PairID:  "pair-2",
		Side:    "a",
		RelPath: "nonexistent.txt",
		Op:      "delete",
	}
	env := proto.Envelope{
		Type:    proto.TypeFileSyncCommand,
		Payload: mustMarshalPayload(t, cmd),
	}

	if err := r.HandleEnvelope(ctx, env); err != nil {
		t.Fatalf("HandleEnvelope: %v", err)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if len(sender.Received()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	received := sender.Received()
	if len(received) != 1 {
		t.Fatalf("expected 1 ack, got %d", len(received))
	}
	var ack proto.FileSyncAck
	if err := proto.UnmarshalPayload(received[0].Payload, &ack); err != nil {
		t.Fatalf("unmarshal ack: %v", err)
	}
	if ack.Status != "ok" {
		t.Errorf("expected status=ok for ErrNotExist, got %s (error=%s)", ack.Status, ack.Error)
	}
}

func TestRunner_HandleFileSyncCommand_Delete_ExistingFile(t *testing.T) {
	dir := t.TempDir()
	jail, err := clientfs.New(dir)
	if err != nil {
		t.Fatal(err)
	}

	// ファイルを実際に作成
	f, err := jail.Create("todelete.txt")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	sender := &fakeSender{}
	r, err := New(Config{
		Jail:     jail,
		Transfer: &fakeTransferClient{},
		Sender:   sender,
	})
	if err != nil {
		t.Fatal(err)
	}

	r.reg.Apply(proto.SubscribePair{
		PairID:    "pair-del",
		Side:      "a",
		Direction: "bidirectional",
	})

	ctx := context.Background()
	cmd := proto.FileSyncCommand{
		PairID:  "pair-del",
		Side:    "a",
		RelPath: "todelete.txt",
		Op:      "delete",
	}
	env := proto.Envelope{
		Type:    proto.TypeFileSyncCommand,
		Payload: mustMarshalPayload(t, cmd),
	}

	if err := r.HandleEnvelope(ctx, env); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if len(sender.Received()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	received := sender.Received()
	if len(received) != 1 {
		t.Fatalf("expected 1 ack, got %d", len(received))
	}
	var ack proto.FileSyncAck
	if err := proto.UnmarshalPayload(received[0].Payload, &ack); err != nil {
		t.Fatal(err)
	}
	if ack.Status != "ok" {
		t.Errorf("expected status=ok, got %s (error=%s)", ack.Status, ack.Error)
	}

	// ファイルが削除されているか確認
	if _, err := os.Stat(dir + "/todelete.txt"); !os.IsNotExist(err) {
		t.Error("expected file to be deleted")
	}
}

func TestRunner_HandleFileSyncCommand_UnknownOp_AcksFailed(t *testing.T) {
	r, sender := makeRunner(t, nil)
	ctx := context.Background()

	r.reg.Apply(proto.SubscribePair{
		PairID:    "pair-3",
		Side:      "a",
		Direction: "bidirectional",
	})

	cmd := proto.FileSyncCommand{
		PairID:  "pair-3",
		Side:    "a",
		RelPath: "file.txt",
		Op:      "unknown_op",
	}
	env := proto.Envelope{
		Type:    proto.TypeFileSyncCommand,
		Payload: mustMarshalPayload(t, cmd),
	}

	if err := r.HandleEnvelope(ctx, env); err != nil {
		t.Fatalf("HandleEnvelope: %v", err)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if len(sender.Received()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	received := sender.Received()
	if len(received) != 1 {
		t.Fatalf("expected 1 ack, got %d", len(received))
	}
	var ack proto.FileSyncAck
	if err := proto.UnmarshalPayload(received[0].Payload, &ack); err != nil {
		t.Fatalf("unmarshal ack: %v", err)
	}
	if ack.Status != "failed" {
		t.Errorf("expected status=failed, got %s", ack.Status)
	}
}

func TestRunner_HandleFileSyncCommand_Fetch_DownloadError(t *testing.T) {
	downloadErr := errors.New("network error")
	r, sender := makeRunner(t, &fakeTransferClient{downloadErr: downloadErr})
	ctx := context.Background()

	r.reg.Apply(proto.SubscribePair{
		PairID:    "pair-err",
		Side:      "a",
		Direction: "bidirectional",
	})

	cmd := proto.FileSyncCommand{
		PairID:  "pair-err",
		Side:    "a",
		RelPath: "file.txt",
		SHA256:  "sha",
		Op:      "fetch",
	}
	env := proto.Envelope{
		Type:    proto.TypeFileSyncCommand,
		Payload: mustMarshalPayload(t, cmd),
	}

	if err := r.HandleEnvelope(ctx, env); err != nil {
		t.Fatalf("HandleEnvelope: %v", err)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if len(sender.Received()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	received := sender.Received()
	if len(received) != 1 {
		t.Fatalf("expected 1 ack, got %d", len(received))
	}
	var ack proto.FileSyncAck
	if err := proto.UnmarshalPayload(received[0].Payload, &ack); err != nil {
		t.Fatal(err)
	}
	if ack.Status != "failed" {
		t.Errorf("expected status=failed, got %s", ack.Status)
	}
}

func TestRunner_New_MissingJail(t *testing.T) {
	_, err := New(Config{
		Transfer: &fakeTransferClient{},
		Sender:   &fakeSender{},
	})
	if err == nil {
		t.Fatal("expected error when Jail is nil")
	}
}

func TestRunner_HandleFileSyncCommand_Rename(t *testing.T) {
	// Given: jail にファイルが存在する runner と subscription
	dir := t.TempDir()
	jail, err := clientfs.New(dir)
	if err != nil {
		t.Fatal(err)
	}

	f, err := jail.Create("original.txt")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	sender := &fakeSender{}
	r, err := New(Config{
		Jail:     jail,
		Transfer: &fakeTransferClient{},
		Sender:   sender,
	})
	if err != nil {
		t.Fatal(err)
	}

	r.reg.Apply(proto.SubscribePair{
		PairID:    "pair-rename",
		Side:      "a",
		Direction: "bidirectional",
	})

	// When: rename コマンドを受信
	ctx := context.Background()
	cmd := proto.FileSyncCommand{
		PairID:     "pair-rename",
		Side:       "a",
		RelPath:    "original.txt",
		NewRelPath: "renamed.txt",
		Op:         "rename",
	}
	env := proto.Envelope{
		Type:    proto.TypeFileSyncCommand,
		Payload: mustMarshalPayload(t, cmd),
	}

	if err := r.HandleEnvelope(ctx, env); err != nil {
		t.Fatal(err)
	}

	// handleSyncCommand は goroutine で動くため少し待つ
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if len(sender.Received()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Then: ack が ok で返り、ファイルが rename されている
	received := sender.Received()
	if len(received) != 1 {
		t.Fatalf("expected 1 ack, got %d", len(received))
	}
	var ack proto.FileSyncAck
	if err := proto.UnmarshalPayload(received[0].Payload, &ack); err != nil {
		t.Fatal(err)
	}
	if ack.Status != "ok" {
		t.Errorf("expected status=ok, got %s (error=%s)", ack.Status, ack.Error)
	}

	if _, err := os.Stat(dir + "/original.txt"); !os.IsNotExist(err) {
		t.Error("expected original.txt to be gone after rename")
	}
	if _, err := os.Stat(dir + "/renamed.txt"); err != nil {
		t.Errorf("expected renamed.txt to exist: %v", err)
	}
}

func TestRunner_Rename_SuppressesWatcher(t *testing.T) {
	// Given: jail にファイルが存在する runner と subscription
	dir := t.TempDir()
	jail, err := clientfs.New(dir)
	if err != nil {
		t.Fatal(err)
	}

	f, err := jail.Create("src.txt")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	sender := &fakeSender{}
	r, err := New(Config{
		Jail:     jail,
		Transfer: &fakeTransferClient{},
		Sender:   sender,
	})
	if err != nil {
		t.Fatal(err)
	}

	r.reg.Apply(proto.SubscribePair{
		PairID:    "pair-suppress",
		Side:      "a",
		Direction: "bidirectional",
	})

	// When: rename コマンドを送り、suppress に両パスが登録されていることを確認
	ctx := context.Background()
	cmd := proto.FileSyncCommand{
		PairID:     "pair-suppress",
		Side:       "a",
		RelPath:    "src.txt",
		NewRelPath: "dst.txt",
		Op:         "rename",
	}
	env := proto.Envelope{
		Type:    proto.TypeFileSyncCommand,
		Payload: mustMarshalPayload(t, cmd),
	}

	if err := r.HandleEnvelope(ctx, env); err != nil {
		t.Fatal(err)
	}

	// handleSyncCommand は goroutine で動くため少し待つ
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if len(sender.Received()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Then: 両パスが suppress 登録されている (rename イベントを抑制できる)
	if !r.supp.Hit("src.txt") {
		t.Error("expected src.txt to be suppressed after rename")
	}
	if !r.supp.Hit("dst.txt") {
		t.Error("expected dst.txt to be suppressed after rename")
	}
}
