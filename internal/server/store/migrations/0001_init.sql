CREATE TABLE IF NOT EXISTS schema_version (
    version    INTEGER PRIMARY KEY,
    applied_at INTEGER NOT NULL
);

CREATE TABLE clients (
    id              TEXT PRIMARY KEY,
    display_name    TEXT NOT NULL DEFAULT '',
    hostname        TEXT NOT NULL,
    root_path       TEXT NOT NULL,
    version         TEXT NOT NULL,
    proto_version   TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'offline',
    last_seen       INTEGER NOT NULL DEFAULT 0,
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL,
    etag            INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX idx_clients_status ON clients(status);

CREATE TABLE sync_pairs (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    client_a_id     TEXT NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
    path_a          TEXT NOT NULL,
    client_b_id     TEXT NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
    path_b          TEXT NOT NULL,
    direction       TEXT NOT NULL,
    enabled         INTEGER NOT NULL DEFAULT 1,
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL,
    etag            INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX idx_pairs_client_a ON sync_pairs(client_a_id);
CREATE INDEX idx_pairs_client_b ON sync_pairs(client_b_id);
CREATE INDEX idx_pairs_enabled ON sync_pairs(enabled);

CREATE TABLE file_state (
    pair_id         TEXT NOT NULL REFERENCES sync_pairs(id) ON DELETE CASCADE,
    side            TEXT NOT NULL,
    rel_path        TEXT NOT NULL,
    sha256          TEXT,
    size            INTEGER,
    mod_time        INTEGER,
    server_mod_time INTEGER NOT NULL,
    op              TEXT NOT NULL,
    is_dir          INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (pair_id, side, rel_path)
);
CREATE INDEX idx_file_state_pair ON file_state(pair_id);
CREATE INDEX idx_file_state_sha ON file_state(sha256);

CREATE TABLE sync_runs (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    pair_id         TEXT NOT NULL REFERENCES sync_pairs(id) ON DELETE CASCADE,
    src_client_id   TEXT NOT NULL,
    dst_client_id   TEXT NOT NULL,
    rel_path        TEXT NOT NULL,
    sha256          TEXT,
    bytes           INTEGER NOT NULL DEFAULT 0,
    status          TEXT NOT NULL,
    error           TEXT,
    started_at      INTEGER NOT NULL,
    finished_at     INTEGER NOT NULL
);
CREATE INDEX idx_runs_pair_started ON sync_runs(pair_id, started_at DESC);
CREATE INDEX idx_runs_status ON sync_runs(status);

CREATE TABLE audit_log (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    ts              INTEGER NOT NULL,
    kind            TEXT NOT NULL,
    client_id       TEXT,
    pair_id         TEXT,
    rel_path        TEXT,
    detail          TEXT
);
CREATE INDEX idx_audit_ts ON audit_log(ts DESC);
CREATE INDEX idx_audit_kind ON audit_log(kind);
