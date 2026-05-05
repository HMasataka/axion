package syncengine

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/HMasataka/axion/internal/proto"
	"github.com/HMasataka/axion/internal/server/store"
)

// PublishSubscriptions は指定クライアントが参加している enabled なペアを取得し、
// 各ペアごとに SubscribePair メッセージを当該クライアントへ送信する。
// クライアント接続時に呼ぶ。
func (e *Engine) PublishSubscriptions(ctx context.Context, clientID string) error {
	pairs, err := e.store.ListPairsForClient(ctx, clientID)
	if err != nil {
		return fmt.Errorf("list pairs for client %s: %w", clientID, err)
	}

	for _, p := range pairs {
		if !p.Enabled {
			continue
		}
		side, path := sideAndPathFor(p, clientID)
		if side == "" {
			continue
		}
		if err := e.sendSubscribe(ctx, clientID, p, side, path); err != nil {
			slog.WarnContext(ctx, "send subscribe failed", "client_id", clientID, "pair_id", p.ID, "error", err)
		}
	}

	return nil
}

// PublishPairUpdate はペアの作成/更新/有効化時に該当する両クライアント (A, B) へ
// SubscribePair を送り直す。両クライアントがオンラインでなくても警告ログのみ
// (オフライン側は次回接続時 PublishSubscriptions で受け取る)。
func (e *Engine) PublishPairUpdate(ctx context.Context, pairID string) error {
	pair, err := e.store.GetPair(ctx, pairID)
	if err != nil {
		return fmt.Errorf("get pair %s: %w", pairID, err)
	}
	if pair == nil {
		return nil
	}
	if !pair.Enabled {
		return e.PublishPairUnsubscribe(ctx, pairID)
	}

	for _, clientID := range []string{pair.ClientAID, pair.ClientBID} {
		side, path := sideAndPathFor(*pair, clientID)
		if err := e.sendSubscribe(ctx, clientID, *pair, side, path); err != nil {
			slog.WarnContext(ctx, "send subscribe to client failed", "client_id", clientID, "pair_id", pairID, "error", err)
		}
	}

	return nil
}

// PublishPairUnsubscribe はペア削除/無効化時に当該両クライアントへ unsubscribe を通知する。
// v0.2.3 では SubscribePair の Direction を空にして「無効化」を示す簡易プロトコル。
// (純粋な unsubscribe メッセージは v0.2.4 以降で追加)
func (e *Engine) PublishPairUnsubscribe(ctx context.Context, pairID string) error {
	pair, err := e.store.GetPair(ctx, pairID)
	if err != nil {
		return fmt.Errorf("get pair %s: %w", pairID, err)
	}
	if pair == nil {
		return nil
	}

	for _, clientID := range []string{pair.ClientAID, pair.ClientBID} {
		side, _ := sideAndPathFor(*pair, clientID)
		msg := proto.SubscribePair{
			PairID:      pair.ID,
			Side:        side,
			RootSubpath: "",
			Direction:   "", // empty = unsubscribe
		}
		payload, err := proto.MarshalPayload(msg)
		if err != nil {
			continue
		}
		if err := e.sender.Send(ctx, clientID, proto.Envelope{
			Type:    proto.TypeSubscribePair,
			Payload: payload,
		}); err != nil {
			slog.WarnContext(ctx, "send unsubscribe failed", "client_id", clientID, "pair_id", pairID, "error", err)
		}
	}

	return nil
}

func sideAndPathFor(p store.SyncPair, clientID string) (string, string) {
	switch clientID {
	case p.ClientAID:
		return "a", p.PathA
	case p.ClientBID:
		return "b", p.PathB
	default:
		return "", ""
	}
}

func (e *Engine) sendSubscribe(ctx context.Context, clientID string, p store.SyncPair, side, path string) error {
	msg := proto.SubscribePair{
		PairID:      p.ID,
		Side:        side,
		RootSubpath: path,
		Direction:   p.Direction,
	}
	payload, err := proto.MarshalPayload(msg)
	if err != nil {
		return err
	}
	return e.sender.Send(ctx, clientID, proto.Envelope{
		Type:    proto.TypeSubscribePair,
		Payload: payload,
	})
}
