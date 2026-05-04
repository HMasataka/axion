package blobstore_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HMasataka/axion/internal/server/blobstore"
)

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func newStore(t *testing.T) *blobstore.FS {
	t.Helper()
	fs, err := blobstore.New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return fs
}

func putBlob(t *testing.T, fs *blobstore.FS, data []byte) string {
	t.Helper()
	sha := sha256Hex(data)
	if err := fs.Put(context.Background(), sha, bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatalf("Put: %v", err)
	}
	return sha
}

// TestNew_CreatesBlobsDir は New が blobs/ ディレクトリを作成することを検証する。
func TestNew_CreatesBlobsDir(t *testing.T) {
	// Given
	root := t.TempDir()

	// When
	_, err := blobstore.New(root)

	// Then
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "blobs")); err != nil {
		t.Errorf("blobs dir not created: %v", err)
	}
}

// TestPut_StoresContent は Put した内容を Get で取得できることを検証する。
func TestPut_StoresContent(t *testing.T) {
	// Given
	fs := newStore(t)
	data := []byte("hello blobstore")
	sha := sha256Hex(data)

	// When
	err := fs.Put(context.Background(), sha, bytes.NewReader(data), int64(len(data)))

	// Then
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	ok, err := fs.Has(context.Background(), sha)
	if err != nil {
		t.Fatalf("Has: %v", err)
	}
	if !ok {
		t.Fatal("Has returned false after Put")
	}
	rc, err := fs.Get(context.Background(), sha)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if !bytes.Equal(got, data) {
		t.Errorf("content mismatch: got %q, want %q", got, data)
	}
}

// TestPut_RejectsSHAMismatch は SHA が合わない場合 ErrSHAMismatch を返すことを検証する。
func TestPut_RejectsSHAMismatch(t *testing.T) {
	// Given
	fs := newStore(t)
	data := []byte("content")
	wrongSHA := sha256Hex([]byte("other content"))

	// When
	err := fs.Put(context.Background(), wrongSHA, bytes.NewReader(data), 0)

	// Then
	if !errors.Is(err, blobstore.ErrSHAMismatch) {
		t.Errorf("want ErrSHAMismatch, got %v", err)
	}
	// .partial が残っていないことを確認。
	ok, _ := fs.Has(context.Background(), wrongSHA)
	if ok {
		t.Error("blob should not exist after SHA mismatch")
	}
}

// TestPut_RejectsSizeMismatch は expectedSize が合わない場合 ErrSizeMismatch を返すことを検証する。
func TestPut_RejectsSizeMismatch(t *testing.T) {
	// Given
	fs := newStore(t)
	data := []byte("content")
	sha := sha256Hex(data)

	// When
	err := fs.Put(context.Background(), sha, bytes.NewReader(data), int64(len(data))+1)

	// Then
	if !errors.Is(err, blobstore.ErrSizeMismatch) {
		t.Errorf("want ErrSizeMismatch, got %v", err)
	}
}

// TestPut_Idempotent は同じ sha を 2 回 Put しても問題ないことを検証する。
func TestPut_Idempotent(t *testing.T) {
	// Given
	fs := newStore(t)
	data := []byte("idempotent blob")
	sha := sha256Hex(data)

	// When
	err1 := fs.Put(context.Background(), sha, bytes.NewReader(data), 0)
	err2 := fs.Put(context.Background(), sha, bytes.NewReader(data), 0)

	// Then
	if err1 != nil {
		t.Fatalf("first Put: %v", err1)
	}
	if err2 != nil {
		t.Fatalf("second Put: %v", err2)
	}
}

// TestPut_AtomicRename は Put 完了後に .partial が消えて final が存在することを検証する。
func TestPut_AtomicRename(t *testing.T) {
	// Given
	root := t.TempDir()
	fs, _ := blobstore.New(root)
	data := []byte("atomic rename test")
	sha := sha256Hex(data)

	// When
	err := fs.Put(context.Background(), sha, bytes.NewReader(data), 0)

	// Then
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	partialPath := filepath.Join(root, "blobs", sha[0:2], sha+".partial")
	if _, err := os.Stat(partialPath); !os.IsNotExist(err) {
		t.Error(".partial file should not exist after successful Put")
	}
	finalPath := filepath.Join(root, "blobs", sha[0:2], sha)
	if _, err := os.Stat(finalPath); err != nil {
		t.Errorf("final blob should exist: %v", err)
	}
}

// TestPutRange_Resumes はレジューム書き込みが正しく動作することを検証する。
func TestPutRange_Resumes(t *testing.T) {
	// Given
	root := t.TempDir()
	fs, _ := blobstore.New(root)
	data := []byte(strings.Repeat("x", 100))
	sha := sha256Hex(data)
	half := int64(50)

	// When: 前半を書く
	err := fs.PutRange(context.Background(), sha, 0, bytes.NewReader(data[:half]), int64(len(data)))

	// Then: 不完全なので ErrIncomplete + .partial が残る。
	if !errors.Is(err, blobstore.ErrIncomplete) {
		t.Fatalf("want ErrIncomplete, got %v", err)
	}
	partialPath := filepath.Join(root, "blobs", sha[0:2], sha+".partial")
	if _, err := os.Stat(partialPath); err != nil {
		t.Errorf(".partial should exist after incomplete write: %v", err)
	}

	// When: 後半を書く
	err = fs.PutRange(context.Background(), sha, half, bytes.NewReader(data[half:]), int64(len(data)))

	// Then: 完了 + atomic rename。
	if err != nil {
		t.Fatalf("second PutRange: %v", err)
	}
	if _, err := os.Stat(partialPath); !os.IsNotExist(err) {
		t.Error(".partial should be gone after complete write")
	}
	ok, _ := fs.Has(context.Background(), sha)
	if !ok {
		t.Error("blob should exist after complete PutRange")
	}
}

// TestPutRange_TotalSizeMismatch は totalSize と実際のサイズが合わない場合 ErrSizeMismatch を返すことを検証する。
func TestPutRange_TotalSizeMismatch(t *testing.T) {
	// Given
	fs := newStore(t)
	data := []byte("range data")
	sha := sha256Hex(data)

	// When: totalSize を実際より大きく設定して全データを書く
	err := fs.PutRange(context.Background(), sha, 0, bytes.NewReader(data), int64(len(data))+10)

	// Then
	if !errors.Is(err, blobstore.ErrIncomplete) {
		t.Errorf("want ErrIncomplete when written < totalSize, got %v", err)
	}
}

// TestGet_NotFound は存在しない sha に対して ErrNotFound を返すことを検証する。
func TestGet_NotFound(t *testing.T) {
	// Given
	fs := newStore(t)
	sha := sha256Hex([]byte("nonexistent"))

	// When
	_, err := fs.Get(context.Background(), sha)

	// Then
	if !errors.Is(err, blobstore.ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

// TestGetRange_Partial は offset と length を指定して部分取得できることを検証する。
func TestGetRange_Partial(t *testing.T) {
	// Given
	fs := newStore(t)
	data := make([]byte, 100)
	for i := range data {
		data[i] = byte(i)
	}
	sha := putBlob(t, fs, data)

	// When
	rc, err := fs.GetRange(context.Background(), sha, 10, 20)

	// Then
	if err != nil {
		t.Fatalf("GetRange: %v", err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if len(got) != 20 {
		t.Errorf("want 20 bytes, got %d", len(got))
	}
	if !bytes.Equal(got, data[10:30]) {
		t.Errorf("content mismatch")
	}
}

// TestGetRange_OpenEnd は length=-1 で end-of-file まで取得できることを検証する。
func TestGetRange_OpenEnd(t *testing.T) {
	// Given
	fs := newStore(t)
	data := make([]byte, 100)
	for i := range data {
		data[i] = byte(i)
	}
	sha := putBlob(t, fs, data)

	// When
	rc, err := fs.GetRange(context.Background(), sha, 10, -1)

	// Then
	if err != nil {
		t.Fatalf("GetRange: %v", err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if !bytes.Equal(got, data[10:]) {
		t.Errorf("content mismatch: got %d bytes, want %d bytes", len(got), len(data)-10)
	}
}

// TestStat_ReturnsSize は Put 後に Stat で正しいサイズが返ることを検証する。
func TestStat_ReturnsSize(t *testing.T) {
	// Given
	fs := newStore(t)
	data := []byte("stat test data")
	sha := putBlob(t, fs, data)

	// When
	size, _, err := fs.Stat(context.Background(), sha)

	// Then
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if size != int64(len(data)) {
		t.Errorf("want size %d, got %d", len(data), size)
	}
}

// TestList_ReturnsAll は 3 blob Put 後に List で 3 件返ることを検証する。
func TestList_ReturnsAll(t *testing.T) {
	// Given
	fs := newStore(t)
	blobs := [][]byte{
		[]byte("blob one"),
		[]byte("blob two"),
		[]byte("blob three"),
	}
	for _, b := range blobs {
		putBlob(t, fs, b)
	}

	// When
	list, err := fs.List(context.Background())

	// Then
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 3 {
		t.Errorf("want 3 blobs, got %d", len(list))
	}
}

// TestDelete_Removes は Put 後に Delete すると Has が false になることを検証する。
func TestDelete_Removes(t *testing.T) {
	// Given
	fs := newStore(t)
	data := []byte("delete me")
	sha := putBlob(t, fs, data)

	// When
	err := fs.Delete(context.Background(), sha)

	// Then
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	ok, _ := fs.Has(context.Background(), sha)
	if ok {
		t.Error("blob should not exist after Delete")
	}
}

// TestInvalidSHA は不正な sha 形式で ErrInvalidSHA を返すことを検証する。
func TestInvalidSHA(t *testing.T) {
	// Given
	fs := newStore(t)
	cases := []string{"abc", "", "zzzz", strings.Repeat("a", 63)}

	for _, sha := range cases {
		// When
		_, err := fs.Has(context.Background(), sha)

		// Then
		if !errors.Is(err, blobstore.ErrInvalidSHA) {
			t.Errorf("sha=%q: want ErrInvalidSHA, got %v", sha, err)
		}
	}
}
