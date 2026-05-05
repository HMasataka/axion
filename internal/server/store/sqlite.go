package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// SQLite は Store の SQLite 実装。
type SQLite struct {
	db *sql.DB
}

// Open は SQLite ファイルを開き、migrations を適用する。
// system clock が DB に記録された最大 server_mod_time より古い場合は LWW 正確性を守るため abort する。
func Open(ctx context.Context, dbPath string) (*SQLite, error) {
	dsn := dbPath + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// SQLite の WAL モードで読み取り/書き込みの可視性を保証するため単一コネクションに固定する。
	// これにより upsert 後の GetFileState が必ず最新データを参照できる。
	db.SetMaxOpenConns(1)

	if err := applyMigrations(ctx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply migrations: %w", err)
	}

	if err := checkMonotonic(ctx, db); err != nil {
		db.Close()
		return nil, err
	}

	return &SQLite{db: db}, nil
}

func checkMonotonic(ctx context.Context, db *sql.DB) error {
	var maxSMT sql.NullInt64
	if err := db.QueryRowContext(ctx, "SELECT MAX(server_mod_time) FROM file_state").Scan(&maxSMT); err != nil {
		return fmt.Errorf("monotonic check failed: %w", err)
	}

	if maxSMT.Valid && time.Now().UnixNano() < maxSMT.Int64 {
		return fmt.Errorf(
			"system clock regression detected: now=%d, max_server_mod_time=%d. "+
				"axion server cannot start with backwards clock to preserve LWW correctness",
			time.Now().UnixNano(), maxSMT.Int64,
		)
	}

	return nil
}

func (s *SQLite) Close() error {
	return s.db.Close()
}

// UpsertClient はクライアントを挿入または更新する。
// ON CONFLICT で同一 ID のレコードを置き換える。
func (s *SQLite) UpsertClient(ctx context.Context, c Client) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO clients (id, display_name, hostname, root_path, version, proto_version, status, last_seen, created_at, updated_at, etag)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			display_name  = excluded.display_name,
			hostname      = excluded.hostname,
			root_path     = excluded.root_path,
			version       = excluded.version,
			proto_version = excluded.proto_version,
			status        = excluded.status,
			last_seen     = excluded.last_seen,
			updated_at    = excluded.updated_at,
			etag          = etag + 1
	`,
		c.ID, c.DisplayName, c.Hostname, c.RootPath, c.Version, c.ProtoVersion,
		c.Status, c.LastSeen.UnixNano(), c.CreatedAt.UnixNano(), c.UpdatedAt.UnixNano(), c.Etag,
	)
	if err != nil {
		return fmt.Errorf("upsert client: %w", err)
	}

	return nil
}

func (s *SQLite) GetClient(ctx context.Context, id string) (*Client, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, display_name, hostname, root_path, version, proto_version, status, last_seen, created_at, updated_at, etag
		FROM clients WHERE id = ?
	`, id)

	return scanClient(row)
}

func (s *SQLite) ListClients(ctx context.Context) ([]Client, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, display_name, hostname, root_path, version, proto_version, status, last_seen, created_at, updated_at, etag
		FROM clients ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list clients: %w", err)
	}
	defer rows.Close()

	return scanClients(rows)
}

func (s *SQLite) UpdateClientStatus(ctx context.Context, id, status string, lastSeen time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE clients SET status = ?, last_seen = ?, updated_at = ? WHERE id = ?
	`, status, lastSeen.UnixNano(), time.Now().UnixNano(), id)
	if err != nil {
		return fmt.Errorf("update client status: %w", err)
	}

	return nil
}

func (s *SQLite) UpdateClientDisplayName(ctx context.Context, id, displayName string, expectedEtag int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE clients SET display_name = ?, etag = etag + 1, updated_at = ?
		WHERE id = ? AND etag = ?
	`, displayName, time.Now().UnixNano(), id, expectedEtag)
	if err != nil {
		return 0, fmt.Errorf("update client display name: %w", err)
	}

	n, _ := res.RowsAffected()
	if n == 0 {
		return 0, ErrEtagMismatch
	}

	var newEtag int64
	if err := s.db.QueryRowContext(ctx, `SELECT etag FROM clients WHERE id = ?`, id).Scan(&newEtag); err != nil {
		return 0, fmt.Errorf("fetch new etag: %w", err)
	}

	return newEtag, nil
}

func (s *SQLite) UpsertPair(ctx context.Context, p SyncPair) error {
	enabled := 0
	if p.Enabled {
		enabled = 1
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sync_pairs (id, name, client_a_id, path_a, client_b_id, path_b, direction, enabled, created_at, updated_at, etag)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name       = excluded.name,
			path_a     = excluded.path_a,
			path_b     = excluded.path_b,
			direction  = excluded.direction,
			enabled    = excluded.enabled,
			updated_at = excluded.updated_at,
			etag       = etag + 1
	`,
		p.ID, p.Name, p.ClientAID, p.PathA, p.ClientBID, p.PathB,
		p.Direction, enabled, p.CreatedAt.UnixNano(), p.UpdatedAt.UnixNano(), p.Etag,
	)
	if err != nil {
		return fmt.Errorf("upsert pair: %w", err)
	}

	return nil
}

func (s *SQLite) GetPair(ctx context.Context, id string) (*SyncPair, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, client_a_id, path_a, client_b_id, path_b, direction, enabled, created_at, updated_at, etag
		FROM sync_pairs WHERE id = ?
	`, id)

	return scanPair(row)
}

func (s *SQLite) ListPairs(ctx context.Context) ([]SyncPair, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, client_a_id, path_a, client_b_id, path_b, direction, enabled, created_at, updated_at, etag
		FROM sync_pairs ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list pairs: %w", err)
	}
	defer rows.Close()

	return scanPairs(rows)
}

func (s *SQLite) ListPairsForClient(ctx context.Context, clientID string) ([]SyncPair, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, client_a_id, path_a, client_b_id, path_b, direction, enabled, created_at, updated_at, etag
		FROM sync_pairs WHERE client_a_id = ? OR client_b_id = ? ORDER BY created_at ASC
	`, clientID, clientID)
	if err != nil {
		return nil, fmt.Errorf("list pairs for client: %w", err)
	}
	defer rows.Close()

	return scanPairs(rows)
}

func (s *SQLite) DeletePair(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sync_pairs WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete pair: %w", err)
	}

	return nil
}

const upsertFileStateSQL = `
INSERT INTO file_state (pair_id, side, rel_path, sha256, size, mod_time, server_mod_time, op, is_dir)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(pair_id, side, rel_path)
DO UPDATE SET sha256=excluded.sha256, size=excluded.size, mod_time=excluded.mod_time,
              server_mod_time=excluded.server_mod_time, op=excluded.op, is_dir=excluded.is_dir
WHERE excluded.server_mod_time > file_state.server_mod_time
`

func (s *SQLite) UpsertFileState(ctx context.Context, fs FileState) error {
	isDir := 0
	if fs.IsDir {
		isDir = 1
	}

	_, err := s.db.ExecContext(ctx, upsertFileStateSQL,
		fs.PairID, fs.Side, fs.RelPath, fs.SHA256, fs.Size, fs.ModTime, fs.ServerModTime, fs.Op, isDir,
	)
	if err != nil {
		return fmt.Errorf("upsert file state: %w", err)
	}

	return nil
}

func (s *SQLite) GetFileState(ctx context.Context, pairID, side, relPath string) (*FileState, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT pair_id, side, rel_path, sha256, size, mod_time, server_mod_time, op, is_dir
		FROM file_state WHERE pair_id = ? AND side = ? AND rel_path = ?
	`, pairID, side, relPath)

	return scanFileState(row)
}

func (s *SQLite) ListFileStates(ctx context.Context, pairID, side string) ([]FileState, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT pair_id, side, rel_path, sha256, size, mod_time, server_mod_time, op, is_dir
		FROM file_state WHERE pair_id = ? AND side = ? ORDER BY rel_path ASC
	`, pairID, side)
	if err != nil {
		return nil, fmt.Errorf("list file states: %w", err)
	}
	defer rows.Close()

	return scanFileStates(rows)
}

func (s *SQLite) DeleteFileState(ctx context.Context, pairID, side, relPath string) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM file_state WHERE pair_id = ? AND side = ? AND rel_path = ?
	`, pairID, side, relPath)
	if err != nil {
		return fmt.Errorf("delete file state: %w", err)
	}

	return nil
}

func (s *SQLite) InsertSyncRun(ctx context.Context, r SyncRun) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO sync_runs (pair_id, src_client_id, dst_client_id, rel_path, sha256, bytes, status, error, started_at, finished_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		r.PairID, r.SrcClientID, r.DstClientID, r.RelPath, r.SHA256, r.Bytes,
		r.Status, r.Error, r.StartedAt.UnixNano(), r.FinishedAt.UnixNano(),
	)
	if err != nil {
		return 0, fmt.Errorf("insert sync run: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("last insert id: %w", err)
	}

	return id, nil
}

func (s *SQLite) ListRecentSyncRuns(ctx context.Context, pairID string, limit int) ([]SyncRun, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, pair_id, src_client_id, dst_client_id, rel_path, sha256, bytes, status, error, started_at, finished_at
		FROM sync_runs WHERE pair_id = ? ORDER BY started_at DESC LIMIT ?
	`, pairID, limit)
	if err != nil {
		return nil, fmt.Errorf("list recent sync runs: %w", err)
	}
	defer rows.Close()

	return scanSyncRuns(rows)
}

func (s *SQLite) AppendAuditLog(ctx context.Context, e AuditEntry) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO audit_log (ts, kind, client_id, pair_id, rel_path, detail)
		VALUES (?, ?, ?, ?, ?, ?)
	`, e.TS.UnixNano(), e.Kind, e.ClientID, e.PairID, e.RelPath, e.Detail)
	if err != nil {
		return fmt.Errorf("append audit log: %w", err)
	}

	return nil
}

func (s *SQLite) ListRecentAuditLog(ctx context.Context, limit int) ([]AuditEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT ts, kind, client_id, pair_id, rel_path, detail
		FROM audit_log ORDER BY ts DESC LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("list recent audit log: %w", err)
	}
	defer rows.Close()

	return scanAuditEntries(rows)
}

func (s *SQLite) GetSetting(ctx context.Context, key string) (string, error) {
	var value string
	if err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&value); err != nil {
		return "", fmt.Errorf("get setting %q: %w", key, err)
	}

	return value, nil
}

func (s *SQLite) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO settings(key, value, updated_at) VALUES(?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
	`, key, value, time.Now().UnixNano())
	if err != nil {
		return fmt.Errorf("set setting %q: %w", key, err)
	}

	return nil
}

func (s *SQLite) LoadAllSettings(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, fmt.Errorf("load all settings: %w", err)
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("scan setting: %w", err)
		}
		result[k] = v
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate settings: %w", err)
	}

	return result, nil
}

// scanner はシングル行スキャンと複数行スキャンで共通の行インターフェース。
type scanner interface {
	Scan(dest ...any) error
}

func scanClient(row scanner) (*Client, error) {
	var c Client
	var lastSeen, createdAt, updatedAt int64

	err := row.Scan(
		&c.ID, &c.DisplayName, &c.Hostname, &c.RootPath, &c.Version, &c.ProtoVersion,
		&c.Status, &lastSeen, &createdAt, &updatedAt, &c.Etag,
	)
	if err != nil {
		return nil, fmt.Errorf("scan client: %w", err)
	}

	c.LastSeen = time.Unix(0, lastSeen)
	c.CreatedAt = time.Unix(0, createdAt)
	c.UpdatedAt = time.Unix(0, updatedAt)

	return &c, nil
}

func scanClients(rows *sql.Rows) ([]Client, error) {
	var clients []Client
	for rows.Next() {
		c, err := scanClient(rows)
		if err != nil {
			return nil, err
		}
		clients = append(clients, *c)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate clients: %w", err)
	}

	return clients, nil
}

func scanPair(row scanner) (*SyncPair, error) {
	var p SyncPair
	var enabled int
	var createdAt, updatedAt int64

	err := row.Scan(
		&p.ID, &p.Name, &p.ClientAID, &p.PathA, &p.ClientBID, &p.PathB,
		&p.Direction, &enabled, &createdAt, &updatedAt, &p.Etag,
	)
	if err != nil {
		return nil, fmt.Errorf("scan pair: %w", err)
	}

	p.Enabled = enabled != 0
	p.CreatedAt = time.Unix(0, createdAt)
	p.UpdatedAt = time.Unix(0, updatedAt)

	return &p, nil
}

func scanPairs(rows *sql.Rows) ([]SyncPair, error) {
	var pairs []SyncPair
	for rows.Next() {
		p, err := scanPair(rows)
		if err != nil {
			return nil, err
		}
		pairs = append(pairs, *p)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pairs: %w", err)
	}

	return pairs, nil
}

func scanFileState(row scanner) (*FileState, error) {
	var fs FileState
	var isDir int

	err := row.Scan(
		&fs.PairID, &fs.Side, &fs.RelPath, &fs.SHA256, &fs.Size, &fs.ModTime,
		&fs.ServerModTime, &fs.Op, &isDir,
	)
	if err != nil {
		return nil, fmt.Errorf("scan file state: %w", err)
	}

	fs.IsDir = isDir != 0

	return &fs, nil
}

func scanFileStates(rows *sql.Rows) ([]FileState, error) {
	var states []FileState
	for rows.Next() {
		fs, err := scanFileState(rows)
		if err != nil {
			return nil, err
		}
		states = append(states, *fs)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate file states: %w", err)
	}

	return states, nil
}

func scanSyncRun(row scanner) (*SyncRun, error) {
	var r SyncRun
	var startedAt, finishedAt int64

	err := row.Scan(
		&r.ID, &r.PairID, &r.SrcClientID, &r.DstClientID, &r.RelPath, &r.SHA256,
		&r.Bytes, &r.Status, &r.Error, &startedAt, &finishedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan sync run: %w", err)
	}

	r.StartedAt = time.Unix(0, startedAt)
	r.FinishedAt = time.Unix(0, finishedAt)

	return &r, nil
}

func scanSyncRuns(rows *sql.Rows) ([]SyncRun, error) {
	var runs []SyncRun
	for rows.Next() {
		r, err := scanSyncRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, *r)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sync runs: %w", err)
	}

	return runs, nil
}

func scanAuditEntry(row scanner) (*AuditEntry, error) {
	var e AuditEntry
	var ts int64

	if err := row.Scan(&ts, &e.Kind, &e.ClientID, &e.PairID, &e.RelPath, &e.Detail); err != nil {
		return nil, fmt.Errorf("scan audit entry: %w", err)
	}

	e.TS = time.Unix(0, ts)

	return &e, nil
}

func scanAuditEntries(rows *sql.Rows) ([]AuditEntry, error) {
	var entries []AuditEntry
	for rows.Next() {
		e, err := scanAuditEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, *e)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit entries: %w", err)
	}

	return entries, nil
}
