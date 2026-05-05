package syncengine

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/HMasataka/axion/internal/proto"
	"github.com/HMasataka/axion/internal/server/store"
)

type fakeRequester struct {
	mu       sync.Mutex
	response proto.ListFilesResponse
	err      error
}

func (f *fakeRequester) SendAndWait(_ context.Context, _ string, _ proto.Envelope, _ time.Duration) (proto.Envelope, error) {
	if f.err != nil {
		return proto.Envelope{}, f.err
	}
	payload, err := proto.MarshalPayload(f.response)
	if err != nil {
		return proto.Envelope{}, err
	}
	return proto.Envelope{
		Type:    proto.TypeListFilesResponse,
		Payload: payload,
	}, nil
}

func setupDiffTest(t *testing.T) (*store.SQLite, *Engine, *fakeSender) {
	t.Helper()
	s := openTestDB(t)
	insertClientPair(t, s, "bidirectional", true)
	sender := &fakeSender{}
	eng := New(s, sender)
	return s, eng, sender
}

func makeFileSnapshot(relPath, sha string, size int64) proto.FileSnapshot {
	return proto.FileSnapshot{
		RelPath: relPath,
		SHA256:  sha,
		Size:    size,
		ModTime: time.Now().UnixNano(),
	}
}

func TestRequestSnapshotAndDiff_NewFiles(t *testing.T) {
	// Given: クライアントに 5 ファイル、DB 0 件
	s, eng, sender := setupDiffTest(t)
	ctx := context.Background()

	snapshots := make([]proto.FileSnapshot, 5)
	for i := range snapshots {
		snapshots[i] = makeFileSnapshot(
			"file"+string(rune('0'+i))+".txt",
			"sha"+string(rune('0'+i)),
			int64(i+1)*100,
		)
	}

	requester := &fakeRequester{
		response: proto.ListFilesResponse{Entries: snapshots},
	}

	// When
	err := eng.RequestSnapshotAndDiff(ctx, requester, "client-a", "pair-1", "a")

	// Then: エラーなし
	if err != nil {
		t.Fatalf("RequestSnapshotAndDiff: %v", err)
	}

	// Then: 5 件 dispatch (FileSyncCommand が送信される)
	msgs := sender.messages()
	if len(msgs) != 5 {
		t.Errorf("expected 5 dispatched messages, got %d", len(msgs))
	}

	// Then: file_state に 5 件登録される
	states, err := s.ListFileStates(ctx, "pair-1", "a")
	if err != nil {
		t.Fatalf("ListFileStates: %v", err)
	}
	if len(states) != 5 {
		t.Errorf("expected 5 file states, got %d", len(states))
	}
}

func TestRequestSnapshotAndDiff_DeletedFiles(t *testing.T) {
	// Given: DB に 5 件、クライアントに 0 件
	s, eng, sender := setupDiffTest(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		sha := "sha" + string(rune('0'+i))
		size := int64((i + 1) * 100)
		if err := s.UpsertFileState(ctx, store.FileState{
			PairID:  "pair-1",
			Side:    "a",
			RelPath: "file" + string(rune('0'+i)) + ".txt",
			SHA256:  &sha,
			Size:    &size,
			Op:      "write",
		}); err != nil {
			t.Fatalf("UpsertFileState: %v", err)
		}
	}

	requester := &fakeRequester{
		response: proto.ListFilesResponse{Entries: []proto.FileSnapshot{}},
	}

	// When
	err := eng.RequestSnapshotAndDiff(ctx, requester, "client-a", "pair-1", "a")

	// Then: エラーなし
	if err != nil {
		t.Fatalf("RequestSnapshotAndDiff: %v", err)
	}

	// Then: 5 件 delete dispatch
	msgs := sender.messages()
	if len(msgs) != 5 {
		t.Errorf("expected 5 delete dispatched messages, got %d", len(msgs))
	}
	for _, m := range msgs {
		var cmd proto.FileSyncCommand
		if err := proto.UnmarshalPayload(m.Env.Payload, &cmd); err != nil {
			t.Fatalf("UnmarshalPayload: %v", err)
		}
		if cmd.Op != "delete" {
			t.Errorf("expected delete op, got %s", cmd.Op)
		}
	}
}

func TestRequestSnapshotAndDiff_NoChange(t *testing.T) {
	// Given: DB とクライアントが同じ SHA → dispatch なし
	s, eng, sender := setupDiffTest(t)
	ctx := context.Background()

	sha := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	size := int64(100)
	if err := s.UpsertFileState(ctx, store.FileState{
		PairID:  "pair-1",
		Side:    "a",
		RelPath: "file.txt",
		SHA256:  &sha,
		Size:    &size,
		Op:      "write",
	}); err != nil {
		t.Fatalf("UpsertFileState: %v", err)
	}

	requester := &fakeRequester{
		response: proto.ListFilesResponse{Entries: []proto.FileSnapshot{
			{RelPath: "file.txt", SHA256: sha, Size: size, ModTime: time.Now().UnixNano()},
		}},
	}

	// When
	err := eng.RequestSnapshotAndDiff(ctx, requester, "client-a", "pair-1", "a")

	// Then: dispatch なし
	if err != nil {
		t.Fatalf("RequestSnapshotAndDiff: %v", err)
	}
	if len(sender.messages()) != 0 {
		t.Errorf("expected 0 dispatched messages, got %d", len(sender.messages()))
	}
}

func TestRequestSnapshotAndDiff_PartialChange(t *testing.T) {
	// Given: 3 ファイル中 1 件だけ SHA が異なる
	s, eng, sender := setupDiffTest(t)
	ctx := context.Background()

	shas := []string{
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
	}
	size := int64(100)

	for i, sha := range shas {
		shaCopy := sha
		if err := s.UpsertFileState(ctx, store.FileState{
			PairID:  "pair-1",
			Side:    "a",
			RelPath: "file" + string(rune('0'+i)) + ".txt",
			SHA256:  &shaCopy,
			Size:    &size,
			Op:      "write",
		}); err != nil {
			t.Fatalf("UpsertFileState: %v", err)
		}
	}

	// file1.txt だけ SHA を変える
	changedSHA := "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	requester := &fakeRequester{
		response: proto.ListFilesResponse{Entries: []proto.FileSnapshot{
			{RelPath: "file0.txt", SHA256: shas[0], Size: size, ModTime: time.Now().UnixNano()},
			{RelPath: "file1.txt", SHA256: changedSHA, Size: size, ModTime: time.Now().UnixNano()},
			{RelPath: "file2.txt", SHA256: shas[2], Size: size, ModTime: time.Now().UnixNano()},
		}},
	}

	// When
	err := eng.RequestSnapshotAndDiff(ctx, requester, "client-a", "pair-1", "a")

	// Then: 1 件だけ dispatch
	if err != nil {
		t.Fatalf("RequestSnapshotAndDiff: %v", err)
	}
	msgs := sender.messages()
	if len(msgs) != 1 {
		t.Errorf("expected 1 dispatched message, got %d", len(msgs))
	}
}
