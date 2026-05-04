package blobstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// FS はローカルファイルシステム上の blob ストア。
// レイアウト: <root>/blobs/<sha[0:2]>/<sha>          (完成品)
//
//	<root>/blobs/<sha[0:2]>/<sha>.partial  (書き込み中)
type FS struct {
	root string
	// v0.2.2 ではグローバルロックで調停。per-sha lock は将来課題。
	mu sync.Mutex
}

// New は <root>/blobs/ ディレクトリを作成して FS を返す。
func New(root string) (*FS, error) {
	blobsDir := filepath.Join(root, "blobs")
	if err := os.MkdirAll(blobsDir, 0755); err != nil {
		return nil, fmt.Errorf("blobstore: mkdirall: %w", err)
	}
	return &FS{root: root}, nil
}

func validateSHA(sha string) error {
	if len(sha) != 64 {
		return ErrInvalidSHA
	}
	for _, c := range sha {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return ErrInvalidSHA
		}
	}
	return nil
}

func (fs *FS) path(sha string) string {
	return filepath.Join(fs.root, "blobs", sha[0:2], sha)
}

func (fs *FS) partialPath(sha string) string {
	return filepath.Join(fs.root, "blobs", sha[0:2], sha+".partial")
}

func (fs *FS) ensureDir(sha string) error {
	dir := filepath.Join(fs.root, "blobs", sha[0:2])
	return os.MkdirAll(dir, 0755)
}

// Has は指定 sha の blob が完全な状態で存在するかを返す。
func (fs *FS) Has(_ context.Context, sha string) (bool, error) {
	if err := validateSHA(sha); err != nil {
		return false, err
	}
	_, err := os.Stat(fs.path(sha))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("blobstore: stat: %w", err)
}

// Stat は指定 sha の blob のサイズと作成時刻を返す。
func (fs *FS) Stat(_ context.Context, sha string) (int64, time.Time, error) {
	if err := validateSHA(sha); err != nil {
		return 0, time.Time{}, err
	}
	info, err := os.Stat(fs.path(sha))
	if err != nil {
		if os.IsNotExist(err) {
			return 0, time.Time{}, ErrNotFound
		}
		return 0, time.Time{}, fmt.Errorf("blobstore: stat: %w", err)
	}
	return info.Size(), info.ModTime(), nil
}

// Put は r の内容を <sha>.partial に書き、SHA 検証後に <sha> へ atomic rename する。
func (fs *FS) Put(_ context.Context, sha string, r io.Reader, expectedSize int64) error {
	if err := validateSHA(sha); err != nil {
		return err
	}

	fs.mu.Lock()
	defer fs.mu.Unlock()

	// idempotent: 既存であれば読み捨てて nil を返す。
	if _, err := os.Stat(fs.path(sha)); err == nil {
		return nil
	}

	if err := fs.ensureDir(sha); err != nil {
		return fmt.Errorf("blobstore: mkdir: %w", err)
	}

	partial := fs.partialPath(sha)
	f, err := os.OpenFile(partial, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("blobstore: open partial: %w", err)
	}

	written, digest, copyErr := copyWithHash(f, r)
	closeErr := f.Close()

	if copyErr != nil {
		os.Remove(partial)
		return fmt.Errorf("blobstore: copy: %w", copyErr)
	}
	if closeErr != nil {
		os.Remove(partial)
		return fmt.Errorf("blobstore: close partial: %w", closeErr)
	}

	if expectedSize > 0 && written != expectedSize {
		os.Remove(partial)
		return ErrSizeMismatch
	}

	if got := hex.EncodeToString(digest); got != strings.ToLower(sha) {
		os.Remove(partial)
		return ErrSHAMismatch
	}

	if err := os.Rename(partial, fs.path(sha)); err != nil {
		os.Remove(partial)
		return fmt.Errorf("blobstore: rename: %w", err)
	}
	return nil
}

// PutRange は <sha>.partial に offset から書く（レジューム用）。
func (fs *FS) PutRange(_ context.Context, sha string, offset int64, r io.Reader, totalSize int64) error {
	if err := validateSHA(sha); err != nil {
		return err
	}

	fs.mu.Lock()
	defer fs.mu.Unlock()

	if err := fs.ensureDir(sha); err != nil {
		return fmt.Errorf("blobstore: mkdir: %w", err)
	}

	partial := fs.partialPath(sha)
	f, err := os.OpenFile(partial, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("blobstore: open partial: %w", err)
	}

	if _, err := f.Seek(offset, 0); err != nil {
		f.Close()
		return fmt.Errorf("blobstore: seek: %w", err)
	}

	written, _, copyErr := copyWithHash(f, r)
	closeErr := f.Close()

	if copyErr != nil {
		return fmt.Errorf("blobstore: copy: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("blobstore: close partial: %w", closeErr)
	}

	currentSize := offset + written
	if currentSize < totalSize {
		return ErrIncomplete
	}
	if currentSize != totalSize {
		os.Remove(partial)
		return ErrSizeMismatch
	}

	// 全体を再 hash して SHA を検証する。
	digest, err := hashFile(partial)
	if err != nil {
		return fmt.Errorf("blobstore: hash partial: %w", err)
	}
	if got := hex.EncodeToString(digest); got != strings.ToLower(sha) {
		os.Remove(partial)
		return ErrSHAMismatch
	}

	if err := os.Rename(partial, fs.path(sha)); err != nil {
		os.Remove(partial)
		return fmt.Errorf("blobstore: rename: %w", err)
	}
	return nil
}

// Get は指定 sha の blob を io.ReadCloser として返す。
func (fs *FS) Get(_ context.Context, sha string) (io.ReadCloser, error) {
	if err := validateSHA(sha); err != nil {
		return nil, err
	}
	f, err := os.Open(fs.path(sha))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("blobstore: open: %w", err)
	}
	return f, nil
}

// GetRange は offset から length バイト読み取れる ReadCloser を返す。
// length=-1 で end-of-file まで。
func (fs *FS) GetRange(_ context.Context, sha string, offset, length int64) (io.ReadCloser, error) {
	if err := validateSHA(sha); err != nil {
		return nil, err
	}
	f, err := os.Open(fs.path(sha))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("blobstore: open: %w", err)
	}
	if _, err := f.Seek(offset, 0); err != nil {
		f.Close()
		return nil, fmt.Errorf("blobstore: seek: %w", err)
	}
	if length < 0 {
		return f, nil
	}
	return &limitReadCloser{r: io.LimitReader(f, length), c: f}, nil
}

// Delete は指定 sha の blob を削除する（GC 用）。
func (fs *FS) Delete(_ context.Context, sha string) error {
	if err := validateSHA(sha); err != nil {
		return err
	}
	if err := os.Remove(fs.path(sha)); err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
		}
		return fmt.Errorf("blobstore: remove: %w", err)
	}
	return nil
}

// List は全 sha のリストを返す（GC 用）。
func (fs *FS) List(_ context.Context) ([]BlobInfo, error) {
	blobsDir := filepath.Join(fs.root, "blobs")
	var infos []BlobInfo

	err := filepath.WalkDir(blobsDir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		// .partial ファイルは完成品ではないのでスキップ。
		name := d.Name()
		if strings.HasSuffix(name, ".partial") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		infos = append(infos, BlobInfo{
			SHA:       name,
			Size:      info.Size(),
			CreatedAt: info.ModTime(),
		})
		return nil
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("blobstore: walkdir: %w", err)
	}
	return infos, nil
}

// copyWithHash は src から dst へコピーしながら SHA256 を計算して返す。
func copyWithHash(dst io.Writer, src io.Reader) (int64, []byte, error) {
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(dst, h), src)
	return n, h.Sum(nil), err
}

// hashFile はファイル全体の SHA256 を計算して返す。
func hashFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

// limitReadCloser は LimitReader に Close を付与する。
type limitReadCloser struct {
	r io.Reader
	c io.Closer
}

func (l *limitReadCloser) Read(p []byte) (int, error) {
	return l.r.Read(p)
}

func (l *limitReadCloser) Close() error {
	return l.c.Close()
}
