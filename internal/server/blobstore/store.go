package blobstore

import (
	"context"
	"errors"
	"io"
	"time"
)

// BlobStore は SHA256 で identify される binary object のストア。
type BlobStore interface {
	// Has は指定 sha の blob が完全な状態で存在するかを返す。
	Has(ctx context.Context, sha string) (bool, error)

	// Stat は指定 sha の blob のサイズと作成時刻を返す。
	Stat(ctx context.Context, sha string) (size int64, createdAt time.Time, err error)

	// Put は r の内容を <sha>.partial に書き、SHA 検証後に <sha> へ atomic rename する。
	// expectedSize > 0 なら一致しない場合 ErrSizeMismatch。
	// 既に同 sha が存在する場合は r を捨てて nil を返す（idempotent）。
	Put(ctx context.Context, sha string, r io.Reader, expectedSize int64) error

	// PutRange は <sha>.partial に offset から書く（レジューム用）。
	// totalSize は全体サイズ（HTTP Content-Range の "/total" 部分）。
	// 全 byte 受信 (= totalSize) かつ SHA 検証 OK で atomic rename。
	// 部分書き込みの場合は ErrIncomplete を返し、.partial を残す。
	PutRange(ctx context.Context, sha string, offset int64, r io.Reader, totalSize int64) error

	// Get は指定 sha の blob を io.ReadCloser として返す。
	Get(ctx context.Context, sha string) (io.ReadCloser, error)

	// GetRange は offset から length バイト読み取れる ReadCloser を返す。
	// length=-1 で end-of-file まで。
	GetRange(ctx context.Context, sha string, offset, length int64) (io.ReadCloser, error)

	// Delete は指定 sha の blob を削除する（GC 用）。
	Delete(ctx context.Context, sha string) error

	// List は全 sha のリストを返す（GC 用）。
	List(ctx context.Context) ([]BlobInfo, error)
}

// BlobInfo は List の戻り値。
type BlobInfo struct {
	SHA       string
	Size      int64
	CreatedAt time.Time
}

var (
	ErrNotFound     = errors.New("blobstore: not found")
	ErrSizeMismatch = errors.New("blobstore: size mismatch")
	ErrSHAMismatch  = errors.New("blobstore: sha256 mismatch")
	ErrIncomplete   = errors.New("blobstore: write incomplete")
	ErrInvalidSHA   = errors.New("blobstore: invalid sha format")
)
