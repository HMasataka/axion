package syncengine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/HMasataka/axion/internal/proto"
	"github.com/HMasataka/axion/internal/server/store"
)

func insertClient(t *testing.T, s store.Store, id string) {
	t.Helper()
	now := time.Now()
	c := store.Client{
		ID: id, DisplayName: id, Hostname: "h", RootPath: "/",
		Version: "1", ProtoVersion: "1", Status: "online",
		LastSeen: now, CreatedAt: now, UpdatedAt: now, Etag: 1,
	}
	if err := s.UpsertClient(context.Background(), c); err != nil {
		t.Fatalf("UpsertClient %s: %v", id, err)
	}
}

func insertPair(t *testing.T, s store.Store, id, clientA, pathA, clientB, pathB, direction string, enabled bool) store.SyncPair {
	t.Helper()
	now := time.Now()
	p := store.SyncPair{
		ID:        id,
		Name:      id,
		ClientAID: clientA,
		PathA:     pathA,
		ClientBID: clientB,
		PathB:     pathB,
		Direction: direction,
		Enabled:   enabled,
		CreatedAt: now,
		UpdatedAt: now,
		Etag:      1,
	}
	if err := s.UpsertPair(context.Background(), p); err != nil {
		t.Fatalf("UpsertPair %s: %v", id, err)
	}
	return p
}

func decodeSubscribePair(t *testing.T, msg sentMsg) proto.SubscribePair {
	t.Helper()
	var sp proto.SubscribePair
	if err := proto.UnmarshalPayload(msg.Env.Payload, &sp); err != nil {
		t.Fatalf("UnmarshalPayload: %v", err)
	}
	return sp
}

func TestPublishSubscriptions_SendsAllEnabledPairs(t *testing.T) {
	// Given: 2 enabled + 1 disabled ペアが存在する client-a
	s := openTestDB(t)
	insertClient(t, s, "client-a")
	insertClient(t, s, "client-b")
	insertClient(t, s, "client-c")
	insertPair(t, s, "pair-1", "client-a", "/a1", "client-b", "/b1", "a_to_b", true)
	insertPair(t, s, "pair-2", "client-a", "/a2", "client-c", "/c2", "a_to_b", true)
	insertPair(t, s, "pair-3", "client-a", "/a3", "client-b", "/b3", "a_to_b", false)

	sender := &fakeSender{}
	eng := New(s, sender)

	// When
	if err := eng.PublishSubscriptions(context.Background(), "client-a"); err != nil {
		t.Fatalf("PublishSubscriptions: %v", err)
	}

	// Then: enabled の 2 件だけ送信
	msgs := sender.messages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 sent messages, got %d", len(msgs))
	}
	for _, m := range msgs {
		if m.ClientID != "client-a" {
			t.Errorf("expected client-a, got %s", m.ClientID)
		}
		if m.Env.Type != proto.TypeSubscribePair {
			t.Errorf("expected TypeSubscribePair, got %s", m.Env.Type)
		}
	}
}

func TestPublishSubscriptions_SkipsClientNotInPair(t *testing.T) {
	// Given: client-z は A でも B でもないペアが存在する
	s := openTestDB(t)
	insertClient(t, s, "client-a")
	insertClient(t, s, "client-b")
	insertClient(t, s, "client-z")
	insertPair(t, s, "pair-1", "client-a", "/a", "client-b", "/b", "a_to_b", true)

	sender := &fakeSender{}
	eng := New(s, sender)

	// When: ListPairsForClient は client-z に対してマッチしないので 0 件
	if err := eng.PublishSubscriptions(context.Background(), "client-z"); err != nil {
		t.Fatalf("PublishSubscriptions: %v", err)
	}

	// Then: 送信 0 件
	if len(sender.messages()) != 0 {
		t.Errorf("expected 0 sent messages, got %d", len(sender.messages()))
	}
}

func TestPublishSubscriptions_SendsCorrectSide(t *testing.T) {
	// Given: ペアに client-a (side=a) と client-b (side=b)
	s := openTestDB(t)
	insertClient(t, s, "client-a")
	insertClient(t, s, "client-b")
	insertPair(t, s, "pair-1", "client-a", "/path/a", "client-b", "/path/b", "a_to_b", true)

	sender := &fakeSender{}
	eng := New(s, sender)
	ctx := context.Background()

	// When: client-a で PublishSubscriptions
	if err := eng.PublishSubscriptions(ctx, "client-a"); err != nil {
		t.Fatalf("PublishSubscriptions client-a: %v", err)
	}
	msgsA := sender.messages()
	if len(msgsA) != 1 {
		t.Fatalf("expected 1 msg for client-a, got %d", len(msgsA))
	}
	spA := decodeSubscribePair(t, msgsA[0])

	// Then: side="a", RootSubpath="/path/a"
	if spA.Side != "a" {
		t.Errorf("Side: want a, got %s", spA.Side)
	}
	if spA.RootSubpath != "/path/a" {
		t.Errorf("RootSubpath: want /path/a, got %s", spA.RootSubpath)
	}

	// When: client-b で PublishSubscriptions
	senderB := &fakeSender{}
	eng2 := New(s, senderB)
	if err := eng2.PublishSubscriptions(ctx, "client-b"); err != nil {
		t.Fatalf("PublishSubscriptions client-b: %v", err)
	}
	msgsB := senderB.messages()
	if len(msgsB) != 1 {
		t.Fatalf("expected 1 msg for client-b, got %d", len(msgsB))
	}
	spB := decodeSubscribePair(t, msgsB[0])

	// Then: side="b", RootSubpath="/path/b"
	if spB.Side != "b" {
		t.Errorf("Side: want b, got %s", spB.Side)
	}
	if spB.RootSubpath != "/path/b" {
		t.Errorf("RootSubpath: want /path/b, got %s", spB.RootSubpath)
	}
}

func TestPublishSubscriptions_NoMatchingPairs(t *testing.T) {
	// Given: ペアが存在しない
	s := openTestDB(t)
	insertClient(t, s, "client-a")

	sender := &fakeSender{}
	eng := New(s, sender)

	// When
	err := eng.PublishSubscriptions(context.Background(), "client-a")

	// Then: エラーなし、送信 0 件
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(sender.messages()) != 0 {
		t.Errorf("expected 0 sent messages, got %d", len(sender.messages()))
	}
}

func TestPublishPairUpdate_SendsToBoth(t *testing.T) {
	// Given: enabled ペア
	s := openTestDB(t)
	insertClient(t, s, "client-a")
	insertClient(t, s, "client-b")
	insertPair(t, s, "pair-1", "client-a", "/a", "client-b", "/b", "a_to_b", true)

	sender := &fakeSender{}
	eng := New(s, sender)

	// When
	if err := eng.PublishPairUpdate(context.Background(), "pair-1"); err != nil {
		t.Fatalf("PublishPairUpdate: %v", err)
	}

	// Then: A と B 両方に送信
	msgs := sender.messages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 sent messages, got %d", len(msgs))
	}
	clientIDs := map[string]bool{}
	for _, m := range msgs {
		clientIDs[m.ClientID] = true
		if m.Env.Type != proto.TypeSubscribePair {
			t.Errorf("expected TypeSubscribePair, got %s", m.Env.Type)
		}
	}
	if !clientIDs["client-a"] {
		t.Error("expected message to client-a")
	}
	if !clientIDs["client-b"] {
		t.Error("expected message to client-b")
	}
}

func TestPublishPairUpdate_DisabledPair_SendsUnsubscribe(t *testing.T) {
	// Given: disabled ペア
	s := openTestDB(t)
	insertClient(t, s, "client-a")
	insertClient(t, s, "client-b")
	insertPair(t, s, "pair-1", "client-a", "/a", "client-b", "/b", "a_to_b", false)

	sender := &fakeSender{}
	eng := New(s, sender)

	// When
	if err := eng.PublishPairUpdate(context.Background(), "pair-1"); err != nil {
		t.Fatalf("PublishPairUpdate: %v", err)
	}

	// Then: 両クライアントに Direction="" 送信
	msgs := sender.messages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 sent messages, got %d", len(msgs))
	}
	for _, m := range msgs {
		sp := decodeSubscribePair(t, m)
		if sp.Direction != "" {
			t.Errorf("expected empty Direction for unsubscribe, got %q", sp.Direction)
		}
	}
}

func TestPublishPairUnsubscribe_SendsToBoth(t *testing.T) {
	// Given: ペアが存在する（削除予定）
	s := openTestDB(t)
	insertClient(t, s, "client-a")
	insertClient(t, s, "client-b")
	insertPair(t, s, "pair-1", "client-a", "/a", "client-b", "/b", "a_to_b", true)

	sender := &fakeSender{}
	eng := New(s, sender)

	// When
	if err := eng.PublishPairUnsubscribe(context.Background(), "pair-1"); err != nil {
		t.Fatalf("PublishPairUnsubscribe: %v", err)
	}

	// Then: 両者に Direction="" 送信
	msgs := sender.messages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 sent messages, got %d", len(msgs))
	}
	clientIDs := map[string]bool{}
	for _, m := range msgs {
		clientIDs[m.ClientID] = true
		sp := decodeSubscribePair(t, m)
		if sp.Direction != "" {
			t.Errorf("expected empty Direction, got %q", sp.Direction)
		}
		if sp.RootSubpath != "" {
			t.Errorf("expected empty RootSubpath, got %q", sp.RootSubpath)
		}
	}
	if !clientIDs["client-a"] {
		t.Error("expected message to client-a")
	}
	if !clientIDs["client-b"] {
		t.Error("expected message to client-b")
	}
}

func TestPublishSubscriptions_SkipsOnSenderError(t *testing.T) {
	// Given: 2 件の enabled ペアがあり、sender が常に error を返す
	s := openTestDB(t)
	insertClient(t, s, "client-a")
	insertClient(t, s, "client-b")
	insertClient(t, s, "client-c")
	insertPair(t, s, "pair-1", "client-a", "/a1", "client-b", "/b1", "a_to_b", true)
	insertPair(t, s, "pair-2", "client-a", "/a2", "client-c", "/c2", "a_to_b", true)

	sender := &fakeSender{err: errors.New("network error")}
	eng := New(s, sender)

	// When
	err := eng.PublishSubscriptions(context.Background(), "client-a")

	// Then: ループ全体は止まらず error なし
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}
