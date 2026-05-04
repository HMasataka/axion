package logctx

import (
	"context"
	"log/slog"
)

// 型衝突を防ぐためにパッケージプライベートな構造体をキーに使う。
type ctxKey struct{}

const (
	KeyClientID = "client_id"
	KeyPairID   = "pair_id"
	KeyRelPath  = "rel_path"
	KeySHA      = "sha"
)

// With は ctx に slog.Attr を追加した新しい context を返す。
// 複数回呼ぶと attrs が累積する。
func With(ctx context.Context, attrs ...slog.Attr) context.Context {
	existing := attrsFrom(ctx)
	merged := make([]slog.Attr, len(existing)+len(attrs))
	copy(merged, existing)
	copy(merged[len(existing):], attrs)
	return context.WithValue(ctx, ctxKey{}, merged)
}

// From は ctx に紐づいた slog.Logger を返す。ctx に値が無ければ slog.Default() を返す。
func From(ctx context.Context) *slog.Logger {
	attrs := attrsFrom(ctx)
	if len(attrs) == 0 {
		return slog.Default()
	}
	args := make([]any, len(attrs))
	for i, a := range attrs {
		args[i] = a
	}
	return slog.Default().With(args...)
}

func attrsFrom(ctx context.Context) []slog.Attr {
	if ctx == nil {
		return nil
	}
	v, _ := ctx.Value(ctxKey{}).([]slog.Attr)
	return v
}

func ClientID(id string) slog.Attr { return slog.String(KeyClientID, id) }
func PairID(id string) slog.Attr   { return slog.String(KeyPairID, id) }
func RelPath(p string) slog.Attr   { return slog.String(KeyRelPath, p) }

// SHA は sha256 hex を受けて先頭 8 文字に切り詰める。
func SHA(sha string) slog.Attr {
	if len(sha) > 8 {
		sha = sha[:8]
	}
	return slog.String(KeySHA, sha)
}
