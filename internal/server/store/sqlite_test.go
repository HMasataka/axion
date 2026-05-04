package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func openTestDB(t *testing.T) *SQLite {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestOpen_AppliesMigrations(t *testing.T) {
	// Given: 新規 DB
	dbPath := filepath.Join(t.TempDir(), "test.db")

	// When: Open
	s, err := Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	// Then: schema_version に version 1 と 2 が存在する
	rows, err := s.db.QueryContext(context.Background(), `SELECT version FROM schema_version ORDER BY version`)
	if err != nil {
		t.Fatalf("query schema_version: %v", err)
	}
	defer rows.Close()

	var versions []int
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("scan: %v", err)
		}
		versions = append(versions, v)
	}

	if len(versions) != 2 || versions[0] != 1 || versions[1] != 2 {
		t.Errorf("expected versions [1 2], got %v", versions)
	}
}

func TestOpen_SkipsAppliedMigrations(t *testing.T) {
	// Given: 既に Open 済みの DB
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s1, err := Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	s1.Close()

	// When: 同じ DB を再度 Open
	s2, err := Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer s2.Close()

	// Then: schema_version のレコード数が変わらない (重複挿入なし)
	var count int
	if err := s2.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM schema_version`).Scan(&count); err != nil {
		t.Fatalf("count schema_version: %v", err)
	}

	if count != 2 {
		t.Errorf("expected 2 schema_version rows, got %d", count)
	}
}

func TestUpsertClient_NewAndUpdate(t *testing.T) {
	// Given: 空の DB
	s := openTestDB(t)
	ctx := context.Background()
	now := time.Now()

	c := Client{
		ID:           "c-1",
		DisplayName:  "Alice",
		Hostname:     "host-a",
		RootPath:     "/data",
		Version:      "1.0.0",
		ProtoVersion: "1",
		Status:       "online",
		LastSeen:     now,
		CreatedAt:    now,
		UpdatedAt:    now,
		Etag:         1,
	}

	// When: 新規挿入
	if err := s.UpsertClient(ctx, c); err != nil {
		t.Fatalf("UpsertClient new: %v", err)
	}

	got, err := s.GetClient(ctx, "c-1")
	if err != nil {
		t.Fatalf("GetClient: %v", err)
	}

	if got.DisplayName != "Alice" {
		t.Errorf("DisplayName: want Alice, got %s", got.DisplayName)
	}

	// When: 同一 ID で status 更新 (ハートビート相当)
	c.Status = "offline"
	c.LastSeen = now.Add(time.Second)
	if err := s.UpsertClient(ctx, c); err != nil {
		t.Fatalf("UpsertClient update: %v", err)
	}

	// Then: status が更新される
	got, err = s.GetClient(ctx, "c-1")
	if err != nil {
		t.Fatalf("GetClient after update: %v", err)
	}

	if got.Status != "offline" {
		t.Errorf("Status: want offline, got %s", got.Status)
	}
}

func TestUpdateClientDisplayName_OptimisticLock(t *testing.T) {
	// Given: 登録済みクライアント (etag=1)
	s := openTestDB(t)
	ctx := context.Background()
	now := time.Now()

	c := Client{
		ID: "c-lock", DisplayName: "Bob", Hostname: "h", RootPath: "/",
		Version: "1", ProtoVersion: "1", Status: "online",
		LastSeen: now, CreatedAt: now, UpdatedAt: now, Etag: 1,
	}
	if err := s.UpsertClient(ctx, c); err != nil {
		t.Fatalf("UpsertClient: %v", err)
	}

	// When: 正しい etag で更新
	newEtag, err := s.UpdateClientDisplayName(ctx, "c-lock", "Bobby", 1)
	if err != nil {
		t.Fatalf("UpdateClientDisplayName: %v", err)
	}

	// Then: etag が +1
	if newEtag != 2 {
		t.Errorf("expected newEtag=2, got %d", newEtag)
	}

	// When: 古い etag で更新を試みる
	_, err = s.UpdateClientDisplayName(ctx, "c-lock", "Robert", 1)

	// Then: ErrEtagMismatch が返る
	if !errors.Is(err, ErrEtagMismatch) {
		t.Errorf("expected ErrEtagMismatch, got %v", err)
	}
}

func TestUpsertPair_RoundTrip(t *testing.T) {
	// Given: 2クライアント登録済み
	s := openTestDB(t)
	ctx := context.Background()
	now := time.Now()

	for _, id := range []string{"ca", "cb"} {
		c := Client{
			ID: id, DisplayName: id, Hostname: "h", RootPath: "/",
			Version: "1", ProtoVersion: "1", Status: "online",
			LastSeen: now, CreatedAt: now, UpdatedAt: now, Etag: 1,
		}
		if err := s.UpsertClient(ctx, c); err != nil {
			t.Fatalf("UpsertClient %s: %v", id, err)
		}
	}

	p := SyncPair{
		ID: "pair-1", Name: "my pair", ClientAID: "ca", PathA: "/a",
		ClientBID: "cb", PathB: "/b", Direction: "bidirectional",
		Enabled: true, CreatedAt: now, UpdatedAt: now, Etag: 1,
	}

	// When: ペア挿入
	if err := s.UpsertPair(ctx, p); err != nil {
		t.Fatalf("UpsertPair: %v", err)
	}

	// Then: GetPair でラウンドトリップ
	got, err := s.GetPair(ctx, "pair-1")
	if err != nil {
		t.Fatalf("GetPair: %v", err)
	}

	if got.Name != "my pair" || !got.Enabled || got.Direction != "bidirectional" {
		t.Errorf("pair mismatch: %+v", got)
	}

	// Then: ListPairsForClient でも見える
	pairs, err := s.ListPairsForClient(ctx, "ca")
	if err != nil {
		t.Fatalf("ListPairsForClient: %v", err)
	}

	if len(pairs) != 1 || pairs[0].ID != "pair-1" {
		t.Errorf("ListPairsForClient: expected 1 pair, got %d", len(pairs))
	}
}

func TestUpsertFileState_MonotonicGuard(t *testing.T) {
	// Given: ペアと初期 file_state 登録
	s := openTestDB(t)
	ctx := context.Background()
	now := time.Now()

	for _, id := range []string{"ca", "cb"} {
		c := Client{
			ID: id, DisplayName: id, Hostname: "h", RootPath: "/",
			Version: "1", ProtoVersion: "1", Status: "online",
			LastSeen: now, CreatedAt: now, UpdatedAt: now, Etag: 1,
		}
		if err := s.UpsertClient(ctx, c); err != nil {
			t.Fatalf("UpsertClient: %v", err)
		}
	}

	pair := SyncPair{
		ID: "p1", Name: "p", ClientAID: "ca", PathA: "/a",
		ClientBID: "cb", PathB: "/b", Direction: "bidirectional",
		Enabled: true, CreatedAt: now, UpdatedAt: now, Etag: 1,
	}
	if err := s.UpsertPair(ctx, pair); err != nil {
		t.Fatalf("UpsertPair: %v", err)
	}

	sha := "abc123"
	size := int64(100)
	mt := now.UnixNano()

	newerSMT := now.Add(time.Hour).UnixNano()
	fs := FileState{
		PairID: "p1", Side: "a", RelPath: "foo.txt",
		SHA256: &sha, Size: &size, ModTime: &mt,
		ServerModTime: newerSMT, Op: "write",
	}

	// When: 新しい server_mod_time で挿入
	if err := s.UpsertFileState(ctx, fs); err != nil {
		t.Fatalf("UpsertFileState: %v", err)
	}

	// When: 古い server_mod_time で upsert を試みる
	olderSMT := now.UnixNano()
	oldSHA := "old000"
	fsOld := FileState{
		PairID: "p1", Side: "a", RelPath: "foo.txt",
		SHA256: &oldSHA, Size: &size, ModTime: &mt,
		ServerModTime: olderSMT, Op: "write",
	}
	if err := s.UpsertFileState(ctx, fsOld); err != nil {
		t.Fatalf("UpsertFileState old: %v", err)
	}

	// Then: sha256 は新しいままで変化しない
	got, err := s.GetFileState(ctx, "p1", "a", "foo.txt")
	if err != nil {
		t.Fatalf("GetFileState: %v", err)
	}

	if got.SHA256 == nil || *got.SHA256 != "abc123" {
		t.Errorf("expected sha256=abc123, got %v", got.SHA256)
	}
}

func TestInsertSyncRun_AndList(t *testing.T) {
	// Given: ペア登録済み
	s := openTestDB(t)
	ctx := context.Background()
	now := time.Now()

	for _, id := range []string{"ca", "cb"} {
		c := Client{
			ID: id, DisplayName: id, Hostname: "h", RootPath: "/",
			Version: "1", ProtoVersion: "1", Status: "online",
			LastSeen: now, CreatedAt: now, UpdatedAt: now, Etag: 1,
		}
		if err := s.UpsertClient(ctx, c); err != nil {
			t.Fatalf("UpsertClient: %v", err)
		}
	}

	pair := SyncPair{
		ID: "p1", Name: "p", ClientAID: "ca", PathA: "/a",
		ClientBID: "cb", PathB: "/b", Direction: "bidirectional",
		Enabled: true, CreatedAt: now, UpdatedAt: now, Etag: 1,
	}
	if err := s.UpsertPair(ctx, pair); err != nil {
		t.Fatalf("UpsertPair: %v", err)
	}

	run := SyncRun{
		PairID:      "p1",
		SrcClientID: "ca",
		DstClientID: "cb",
		RelPath:     "file.txt",
		Bytes:       1024,
		Status:      "ok",
		StartedAt:   now,
		FinishedAt:  now.Add(time.Second),
	}

	// When: SyncRun 挿入
	id, err := s.InsertSyncRun(ctx, run)
	if err != nil {
		t.Fatalf("InsertSyncRun: %v", err)
	}

	// Then: id が 1 以上
	if id < 1 {
		t.Errorf("expected id >= 1, got %d", id)
	}

	// Then: ListRecentSyncRuns で取得できる
	runs, err := s.ListRecentSyncRuns(ctx, "p1", 10)
	if err != nil {
		t.Fatalf("ListRecentSyncRuns: %v", err)
	}

	if len(runs) != 1 || runs[0].Status != "ok" || runs[0].Bytes != 1024 {
		t.Errorf("run mismatch: %+v", runs)
	}
}

func TestAppendAuditLog_AndList(t *testing.T) {
	// Given: 空の DB
	s := openTestDB(t)
	ctx := context.Background()
	now := time.Now()

	clientID := "c1"
	e := AuditEntry{
		TS:       now,
		Kind:     "client_register",
		ClientID: &clientID,
	}

	// When: AuditEntry を追加
	if err := s.AppendAuditLog(ctx, e); err != nil {
		t.Fatalf("AppendAuditLog: %v", err)
	}

	// Then: ListRecentAuditLog で取得できる
	entries, err := s.ListRecentAuditLog(ctx, 10)
	if err != nil {
		t.Fatalf("ListRecentAuditLog: %v", err)
	}

	if len(entries) != 1 || entries[0].Kind != "client_register" {
		t.Errorf("audit entry mismatch: %+v", entries)
	}

	if entries[0].ClientID == nil || *entries[0].ClientID != "c1" {
		t.Errorf("audit ClientID mismatch: %v", entries[0].ClientID)
	}
}

func TestLoadAllSettings_HasDefaults(t *testing.T) {
	// Given: 新規 DB (migration で初期設定が挿入される)
	s := openTestDB(t)
	ctx := context.Background()

	// When: LoadAllSettings
	settings, err := s.LoadAllSettings(ctx)
	if err != nil {
		t.Fatalf("LoadAllSettings: %v", err)
	}

	// Then: 3つのデフォルト設定が存在する
	for _, key := range []string{"ignore_list", "blob_gc_age_seconds", "max_file_size_bytes"} {
		if _, ok := settings[key]; !ok {
			t.Errorf("missing default setting: %s", key)
		}
	}
}

func TestSetSetting_GetSetting_RoundTrip(t *testing.T) {
	// Given: 新規 DB
	s := openTestDB(t)
	ctx := context.Background()

	// When: 新しい設定を保存
	if err := s.SetSetting(ctx, "my_key", "my_value"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	// Then: GetSetting で取得できる
	got, err := s.GetSetting(ctx, "my_key")
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}

	if got != "my_value" {
		t.Errorf("expected my_value, got %s", got)
	}

	// When: 既存キーを上書き
	if err := s.SetSetting(ctx, "my_key", "updated"); err != nil {
		t.Fatalf("SetSetting update: %v", err)
	}

	// Then: 更新後の値が返る
	got, err = s.GetSetting(ctx, "my_key")
	if err != nil {
		t.Fatalf("GetSetting after update: %v", err)
	}

	if got != "updated" {
		t.Errorf("expected updated, got %s", got)
	}
}

func TestMonotonicGuard_AbortsOnClockRegression(t *testing.T) {
	// Given: DB に未来の server_mod_time を持つ file_state を仕込む
	dbPath := filepath.Join(t.TempDir(), "test.db")

	s, err := Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}

	ctx := context.Background()
	now := time.Now()

	for _, id := range []string{"ca", "cb"} {
		c := Client{
			ID: id, DisplayName: id, Hostname: "h", RootPath: "/",
			Version: "1", ProtoVersion: "1", Status: "online",
			LastSeen: now, CreatedAt: now, UpdatedAt: now, Etag: 1,
		}
		if err := s.UpsertClient(ctx, c); err != nil {
			t.Fatalf("UpsertClient: %v", err)
		}
	}

	pair := SyncPair{
		ID: "p1", Name: "p", ClientAID: "ca", PathA: "/a",
		ClientBID: "cb", PathB: "/b", Direction: "bidirectional",
		Enabled: true, CreatedAt: now, UpdatedAt: now, Etag: 1,
	}
	if err := s.UpsertPair(ctx, pair); err != nil {
		t.Fatalf("UpsertPair: %v", err)
	}

	// 100年後の server_mod_time を挿入
	futureSMT := now.Add(100 * 365 * 24 * time.Hour).UnixNano()
	sha := "future"
	size := int64(0)
	mt := now.UnixNano()
	fs := FileState{
		PairID: "p1", Side: "a", RelPath: "future.txt",
		SHA256: &sha, Size: &size, ModTime: &mt,
		ServerModTime: futureSMT, Op: "write",
	}
	if err := s.UpsertFileState(ctx, fs); err != nil {
		t.Fatalf("UpsertFileState future: %v", err)
	}
	s.Close()

	// When: 同じ DB を再度 Open (現在時刻 < futureSMT)
	_, err = Open(context.Background(), dbPath)

	// Then: clock regression エラーで失敗する
	if err == nil {
		t.Fatal("expected error for clock regression, got nil")
	}
}
