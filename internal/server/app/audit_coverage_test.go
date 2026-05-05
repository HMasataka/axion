//go:build !integration

package app_test

// TestAuditCoverage_AllKindsImplemented は spec で定義されたすべての audit kind が
// コードベースで参照されていることを確認する。
//
// 実装箇所の対応:
//
//	"client_register"        → internal/server/http/ws.go: appendClientRegisterDetail
//	"psk_auth_failed"        → internal/server/http/middleware.go: AuthBearer, AuthBearerWithClientID
//	"proto_version_mismatch" → internal/server/http/middleware.go: ProtoVersionHeader
//	                         → internal/server/http/ws.go: ServeHTTP (WS 登録時)
//	"pair_create"            → internal/server/web/pairs.go: auditPairAction
//	"pair_update"            → internal/server/web/pairs.go: auditPairAction
//	"pair_delete"            → internal/server/web/pairs.go: auditPairAction
//	"conflict_detected"      → internal/server/syncengine/conflict.go: handleConflict
//	"conflict_renamed"       → internal/server/syncengine/conflict.go: handleConflict
//	"delete_vs_edit"         → internal/server/syncengine/conflict.go: handleConflict
//	"disk_full"              → internal/server/http/blobs.go: auditDiskFull
//	"quota_exceeded"         → internal/server/http/blobs.go: auditQuotaExceeded
//	"path_escape_blocked"    → internal/server/http/api.go: handleClientError
//
// 既知のギャップ (v0.3 で対応予定):
//
//	"unsupported_file_skipped" → クライアント側で skip するが、サーバー側 audit_log には未記録
//	"case_insensitive_fs"      → クライアント側で warn ログのみ

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuditCoverage_AllKindsImplemented(t *testing.T) {
	t.Parallel()

	expected := []string{
		"client_register",
		"psk_auth_failed",
		"proto_version_mismatch",
		"pair_create", "pair_update", "pair_delete",
		"conflict_detected", "conflict_renamed",
		"delete_vs_edit",
		"disk_full",
		"quota_exceeded",
		"path_escape_blocked",
	}

	// internal/server 以下の非テスト Go ソースを結合して検索する。
	// このテストファイル自身 (audit_coverage_test.go) はテストファイルなので除外される。
	source := readAllSourceFiles(t, filepath.Join("..", "..", "..") /* repo root */ + "/internal/server")

	for _, kind := range expected {
		if !strings.Contains(source, `"`+kind+`"`) {
			t.Errorf("audit kind %q not found in internal/server source", kind)
		}
	}
}

// readAllSourceFiles は root 以下の非テスト .go ファイルを結合した文字列を返す。
func readAllSourceFiles(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path) //nolint:forbidigo
		if err != nil {
			t.Logf("skip %s: %v", path, err)
			return nil
		}
		b.Write(data)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return b.String()
}
