package logctx_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/HMasataka/axion/pkg/logctx"
)

func TestWith_AccumulatesAttrs(t *testing.T) {
	// Given: 2 回 With を呼んで attrs を累積させる
	ctx := logctx.With(context.Background(), logctx.ClientID("c1"))
	ctx = logctx.With(ctx, logctx.PairID("p1"))

	// When: From でロガーを取得してメッセージを出力する
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, nil)
	logger := logctx.From(ctx)

	// slog.Default を一時的に差し替えて From の出力を捕捉する
	orig := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(orig)

	logger = logctx.From(ctx)
	logger.Info("test")

	// Then: 両方の attrs が出力に含まれる
	out := buf.String()
	if !strings.Contains(out, "client_id=c1") {
		t.Errorf("expected client_id=c1 in output, got: %s", out)
	}
	if !strings.Contains(out, "pair_id=p1") {
		t.Errorf("expected pair_id=p1 in output, got: %s", out)
	}
}

func TestFrom_NoContextReturnsDefault(t *testing.T) {
	// Given: 値を持たない空の context
	ctx := context.Background()

	// When/Then: panic しない
	logger := logctx.From(ctx)
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}
}

func TestSHA_Truncates(t *testing.T) {
	// Given: 64 文字の sha256 hex
	full := strings.Repeat("a", 64)

	// When: SHA ヘルパーで attr を生成する
	attr := logctx.SHA(full)

	// Then: 値が先頭 8 文字に切り詰められる
	if attr.Value.String() != "aaaaaaaa" {
		t.Errorf("expected 8 chars, got: %s", attr.Value.String())
	}
}

func TestSHA_ShortInput(t *testing.T) {
	// Given: 8 文字未満の入力
	short := "abc"

	// When: SHA ヘルパーで attr を生成する
	attr := logctx.SHA(short)

	// Then: そのまま返す
	if attr.Value.String() != "abc" {
		t.Errorf("expected abc, got: %s", attr.Value.String())
	}
}
