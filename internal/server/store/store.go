package store

import (
	"context"
	"errors"
	"time"
)

// ErrEtagMismatch は楽観ロックで期待した etag と実際の etag が不一致のときに返される。
var ErrEtagMismatch = errors.New("store: etag mismatch")

// Store は永続化層の抽象。
type Store interface {
	Close() error

	// Clients
	UpsertClient(ctx context.Context, c Client) error
	GetClient(ctx context.Context, id string) (*Client, error)
	ListClients(ctx context.Context) ([]Client, error)
	UpdateClientStatus(ctx context.Context, id, status string, lastSeen time.Time) error
	UpdateClientDisplayName(ctx context.Context, id, displayName string, expectedEtag int64) (newEtag int64, err error)

	// Sync pairs
	UpsertPair(ctx context.Context, p SyncPair) error
	GetPair(ctx context.Context, id string) (*SyncPair, error)
	ListPairs(ctx context.Context) ([]SyncPair, error)
	ListPairsForClient(ctx context.Context, clientID string) ([]SyncPair, error)
	DeletePair(ctx context.Context, id string) error

	// File state
	UpsertFileState(ctx context.Context, fs FileState) error
	GetFileState(ctx context.Context, pairID, side, relPath string) (*FileState, error)
	ListFileStates(ctx context.Context, pairID, side string) ([]FileState, error)
	DeleteFileState(ctx context.Context, pairID, side, relPath string) error

	// Sync runs
	InsertSyncRun(ctx context.Context, r SyncRun) (int64, error)
	ListRecentSyncRuns(ctx context.Context, pairID string, limit int) ([]SyncRun, error)

	// Audit log
	AppendAuditLog(ctx context.Context, e AuditEntry) error
	ListRecentAuditLog(ctx context.Context, limit int) ([]AuditEntry, error)

	// Settings
	GetSetting(ctx context.Context, key string) (string, error)
	SetSetting(ctx context.Context, key, value string) error
	LoadAllSettings(ctx context.Context) (map[string]string, error)
}

type Client struct {
	ID           string
	DisplayName  string
	Hostname     string
	RootPath     string
	Version      string
	ProtoVersion string
	Status       string // "online" | "offline"
	LastSeen     time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Etag         int64
}

type SyncPair struct {
	ID        string
	Name      string
	ClientAID string
	PathA     string
	ClientBID string
	PathB     string
	Direction string // "bidirectional" | "a_to_b" | "b_to_a"
	Enabled   bool
	CreatedAt time.Time
	UpdatedAt time.Time
	Etag      int64
}

type FileState struct {
	PairID        string
	Side          string  // "a" | "b"
	RelPath       string
	SHA256        *string // NULL = 削除
	Size          *int64
	ModTime       *int64 // unix nano
	ServerModTime int64  // unix nano (LWW のソース・オブ・トゥルース)
	Op            string // "write" | "delete"
	IsDir         bool
}

type SyncRun struct {
	ID          int64
	PairID      string
	SrcClientID string
	DstClientID string
	RelPath     string
	SHA256      *string
	Bytes       int64
	Status      string // "ok" | "failed" | "conflict" | "skipped"
	Error       *string
	StartedAt   time.Time
	FinishedAt  time.Time
}

type AuditEntry struct {
	TS       time.Time
	Kind     string  // "client_register", "pair_create", etc.
	ClientID *string
	PairID   *string
	RelPath  *string
	Detail   *string // JSON
}
