package httpsrv_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/HMasataka/axion/internal/server/blobstore"
	httpsrv "github.com/HMasataka/axion/internal/server/http"
	"github.com/HMasataka/axion/internal/server/store"
)

func openTestBlobStore(t *testing.T) *blobstore.FS {
	t.Helper()
	fs, err := blobstore.New(t.TempDir())
	if err != nil {
		t.Fatalf("blobstore.New: %v", err)
	}
	return fs
}

func blobSHA(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func startBlobServer(t *testing.T, bs *blobstore.FS, maxSize, quota int64) *httptest.Server {
	t.Helper()
	s := openTestStore(t)
	return startBlobServerWithStore(t, s, bs, maxSize, quota)
}

func startBlobServerWithStore(t *testing.T, s store.Store, bs *blobstore.FS, maxSize, quota int64) *httptest.Server {
	t.Helper()
	cfg := httpsrv.Config{
		Store:               s,
		Hub:                 newTestHub(),
		BlobStore:           bs,
		PSK:                 testPSK,
		MaxFileSizeBytes:    maxSize,
		PerClientQuotaBytes: quota,
	}
	srv := httptest.NewServer(httpsrv.NewRouter(cfg))
	t.Cleanup(srv.Close)
	return srv
}

func blobPUT(t *testing.T, client *http.Client, url, sha string, data []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPut, url+"/v1/blobs/"+sha, bytes.NewReader(data))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+testPSK)
	req.Header.Set("X-Axion-Client-ID", "client-blob-test")
	req.ContentLength = int64(len(data))
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	return resp
}

func blobPUTWithClientID(t *testing.T, client *http.Client, url, sha, clientID string, data []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPut, url+"/v1/blobs/"+sha, bytes.NewReader(data))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+testPSK)
	req.Header.Set("X-Axion-Client-ID", clientID)
	req.ContentLength = int64(len(data))
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	return resp
}

func blobHEAD(t *testing.T, client *http.Client, url, sha string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodHead, url+"/v1/blobs/"+sha, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+testPSK)
	req.Header.Set("X-Axion-Client-ID", "client-blob-test")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("HEAD: %v", err)
	}
	return resp
}

func blobGET(t *testing.T, client *http.Client, url, sha string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url+"/v1/blobs/"+sha, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+testPSK)
	req.Header.Set("X-Axion-Client-ID", "client-blob-test")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	return resp
}

func blobGETRange(t *testing.T, client *http.Client, url, sha, rangeHeader string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url+"/v1/blobs/"+sha, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+testPSK)
	req.Header.Set("X-Axion-Client-ID", "client-blob-test")
	req.Header.Set("Range", rangeHeader)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET range: %v", err)
	}
	return resp
}

// TestBlobsHandler_PutAndGet は PUT した blob を GET で取得できることを検証する。
func TestBlobsHandler_PutAndGet(t *testing.T) {
	// Given
	bs := openTestBlobStore(t)
	srv := startBlobServer(t, bs, 10*1024*1024, 100*1024*1024)
	client := &http.Client{}

	data := []byte("hello blob handler")
	sha := blobSHA(data)

	// When
	putResp := blobPUT(t, client, srv.URL, sha, data)
	defer putResp.Body.Close()

	// Then
	if putResp.StatusCode != http.StatusCreated {
		t.Errorf("PUT: want 201, got %d", putResp.StatusCode)
	}

	getResp := blobGET(t, client, srv.URL, sha)
	defer getResp.Body.Close()

	if getResp.StatusCode != http.StatusOK {
		t.Errorf("GET: want 200, got %d", getResp.StatusCode)
	}
	got, _ := io.ReadAll(getResp.Body)
	if !bytes.Equal(got, data) {
		t.Errorf("content mismatch: got %q, want %q", got, data)
	}
}

// TestBlobsHandler_GetNotFound は存在しない sha に 404 を返すことを検証する。
func TestBlobsHandler_GetNotFound(t *testing.T) {
	// Given
	bs := openTestBlobStore(t)
	srv := startBlobServer(t, bs, 10*1024*1024, 100*1024*1024)
	client := &http.Client{}

	sha := blobSHA([]byte("nonexistent"))

	// When
	resp := blobGET(t, client, srv.URL, sha)
	defer resp.Body.Close()

	// Then
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("want 404, got %d", resp.StatusCode)
	}
}

// TestBlobsHandler_RejectsOversizedBlob は maxFileSizeBytes を超えた PUT を拒否することを検証する。
func TestBlobsHandler_RejectsOversizedBlob(t *testing.T) {
	// Given
	bs := openTestBlobStore(t)
	srv := startBlobServer(t, bs, 10, 100*1024*1024) // maxSize=10 bytes
	client := &http.Client{}

	data := []byte("this is larger than 10 bytes")
	sha := blobSHA(data)

	// When
	resp := blobPUT(t, client, srv.URL, sha, data)
	defer resp.Body.Close()

	// Then
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("want 413, got %d", resp.StatusCode)
	}
}

// TestBlobs_HEADExisting_Returns200 は存在する blob への HEAD が 200 を返すことを検証する。
func TestBlobs_HEADExisting_Returns200(t *testing.T) {
	// Given
	bs := openTestBlobStore(t)
	srv := startBlobServer(t, bs, 10*1024*1024, 100*1024*1024)
	client := &http.Client{}

	data := bytes.Repeat([]byte("x"), 100)
	sha := blobSHA(data)

	putResp := blobPUT(t, client, srv.URL, sha, data)
	defer putResp.Body.Close()
	if putResp.StatusCode != http.StatusCreated {
		t.Fatalf("PUT: expected 201, got %d", putResp.StatusCode)
	}

	// When
	resp := blobHEAD(t, client, srv.URL, sha)
	defer resp.Body.Close()

	// Then
	if resp.StatusCode != http.StatusOK {
		t.Errorf("HEAD existing: expected 200, got %d", resp.StatusCode)
	}
}

// TestBlobs_HEADMissing_Returns404 は存在しない blob への HEAD が 404 を返すことを検証する。
func TestBlobs_HEADMissing_Returns404(t *testing.T) {
	// Given
	bs := openTestBlobStore(t)
	srv := startBlobServer(t, bs, 10*1024*1024, 100*1024*1024)
	client := &http.Client{}

	sha := blobSHA([]byte("not stored at all"))

	// When
	resp := blobHEAD(t, client, srv.URL, sha)
	defer resp.Body.Close()

	// Then
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("HEAD missing: expected 404, got %d", resp.StatusCode)
	}
}

// TestBlobs_PUT_Stores は 100 byte の PUT が 201 を返し Has が true になることを検証する。
func TestBlobs_PUT_Stores(t *testing.T) {
	// Given
	bs := openTestBlobStore(t)
	srv := startBlobServer(t, bs, 10*1024*1024, 100*1024*1024)
	client := &http.Client{}

	data := bytes.Repeat([]byte("a"), 100)
	sha := blobSHA(data)

	// When
	resp := blobPUT(t, client, srv.URL, sha, data)
	defer resp.Body.Close()

	// Then
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("PUT: expected 201, got %d", resp.StatusCode)
	}
	exists, err := bs.Has(context.Background(), sha)
	if err != nil {
		t.Fatalf("Has: %v", err)
	}
	if !exists {
		t.Error("PUT: blob not found after upload")
	}
}

// TestBlobs_PUT_LargeFile は 1MB の PUT が 201 を返すことを検証する。
func TestBlobs_PUT_LargeFile(t *testing.T) {
	// Given
	bs := openTestBlobStore(t)
	srv := startBlobServer(t, bs, 10*1024*1024, 100*1024*1024)
	client := &http.Client{}

	data := bytes.Repeat([]byte("b"), 1*1024*1024)
	sha := blobSHA(data)

	// When
	resp := blobPUT(t, client, srv.URL, sha, data)
	defer resp.Body.Close()

	// Then
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("PUT large: expected 201, got %d", resp.StatusCode)
	}
}

// TestBlobs_PUT_RejectsTooLarge は maxFileSizeBytes=1024 で 2KB 送信すると 413 を返すことを検証する。
func TestBlobs_PUT_RejectsTooLarge(t *testing.T) {
	// Given
	bs := openTestBlobStore(t)
	srv := startBlobServer(t, bs, 1024, 100*1024*1024)
	client := &http.Client{}

	data := bytes.Repeat([]byte("c"), 2*1024)
	sha := blobSHA(data)

	// When
	resp := blobPUT(t, client, srv.URL, sha, data)
	defer resp.Body.Close()

	// Then
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("PUT too large: expected 413, got %d", resp.StatusCode)
	}
}

// TestBlobs_PUT_QuotaExceeded はクライアントの使用量が上限に達すると 507 + audit_log に quota_exceeded を検証する。
func TestBlobs_PUT_QuotaExceeded(t *testing.T) {
	// Given
	s := openTestStore(t)
	bs := openTestBlobStore(t)

	quota := int64(500)
	srv := startBlobServerWithStore(t, s, bs, 10*1024*1024, quota)
	client := &http.Client{}

	clientID := "client-quota"
	pairID := "pair-quota-001"
	now := time.Now()

	if err := s.UpsertClient(context.Background(), store.Client{
		ID: clientID, DisplayName: "test", Hostname: "h",
		RootPath: "/", Version: "1", ProtoVersion: "1",
		Status: "online", LastSeen: now, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("UpsertClient: %v", err)
	}
	if err := s.UpsertClient(context.Background(), store.Client{
		ID: "client-b-quota", DisplayName: "test-b", Hostname: "h-b",
		RootPath: "/", Version: "1", ProtoVersion: "1",
		Status: "online", LastSeen: now, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("UpsertClient client-b: %v", err)
	}
	if err := s.UpsertPair(context.Background(), store.SyncPair{
		ID: pairID, Name: "p", ClientAID: clientID, PathA: "/", ClientBID: "client-b-quota", PathB: "/",
		Direction: "a_to_b", Enabled: true, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("UpsertPair: %v", err)
	}
	existingSize := int64(400)
	if err := s.UpsertFileState(context.Background(), store.FileState{
		PairID: pairID, Side: "a", RelPath: "existing.txt",
		Size: &existingSize, ServerModTime: now.UnixNano(), Op: "write",
	}); err != nil {
		t.Fatalf("UpsertFileState: %v", err)
	}

	// 200 bytes 追加しようとすると合計 400+200=600 > quota=500 になる
	data := bytes.Repeat([]byte("d"), 200)
	sha := blobSHA(data)

	// When
	resp := blobPUTWithClientID(t, client, srv.URL, sha, clientID, data)
	defer resp.Body.Close()

	// Then
	if resp.StatusCode != http.StatusInsufficientStorage {
		t.Errorf("PUT quota exceeded: expected 507, got %d", resp.StatusCode)
	}
	if !hasAuditKind(t, s, "quota_exceeded") {
		t.Error("expected audit_log entry with kind=quota_exceeded")
	}
}

// TestBlobs_PUT_SHAMismatch は内容と sha 不一致で 422 を返すことを検証する。
func TestBlobs_PUT_SHAMismatch(t *testing.T) {
	// Given
	bs := openTestBlobStore(t)
	srv := startBlobServer(t, bs, 10*1024*1024, 100*1024*1024)
	client := &http.Client{}

	data := bytes.Repeat([]byte("e"), 100)
	wrongSHA := blobSHA([]byte("completely different content"))

	// When
	resp := blobPUT(t, client, srv.URL, wrongSHA, data)
	defer resp.Body.Close()

	// Then
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("PUT sha mismatch: expected 422, got %d", resp.StatusCode)
	}
}

// TestBlobs_GET_Full は PUT 後の GET で全内容を取得できることを検証する。
func TestBlobs_GET_Full(t *testing.T) {
	// Given
	bs := openTestBlobStore(t)
	srv := startBlobServer(t, bs, 10*1024*1024, 100*1024*1024)
	client := &http.Client{}

	data := bytes.Repeat([]byte("f"), 100)
	sha := blobSHA(data)

	putResp := blobPUT(t, client, srv.URL, sha, data)
	defer putResp.Body.Close()
	if putResp.StatusCode != http.StatusCreated {
		t.Fatalf("PUT: expected 201, got %d", putResp.StatusCode)
	}

	// When
	resp := blobGET(t, client, srv.URL, sha)
	defer resp.Body.Close()

	// Then
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET: expected 200, got %d", resp.StatusCode)
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("GET: content mismatch: got %d bytes, want %d bytes", len(got), len(data))
	}
}

// TestBlobs_GET_Range は 100 byte PUT 後に Range: bytes=10-49 で 40 byte + 206 + Content-Range を検証する。
func TestBlobs_GET_Range(t *testing.T) {
	// Given
	bs := openTestBlobStore(t)
	srv := startBlobServer(t, bs, 10*1024*1024, 100*1024*1024)
	client := &http.Client{}

	data := make([]byte, 100)
	for i := range data {
		data[i] = byte(i)
	}
	sha := blobSHA(data)

	putResp := blobPUT(t, client, srv.URL, sha, data)
	defer putResp.Body.Close()
	if putResp.StatusCode != http.StatusCreated {
		t.Fatalf("PUT: expected 201, got %d", putResp.StatusCode)
	}

	// When
	resp := blobGETRange(t, client, srv.URL, sha, "bytes=10-49")
	defer resp.Body.Close()

	// Then
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("GET range: expected 206, got %d", resp.StatusCode)
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(got) != 40 {
		t.Errorf("GET range: expected 40 bytes, got %d", len(got))
	}
	if !bytes.Equal(got, data[10:50]) {
		t.Error("GET range: content mismatch")
	}
	cr := resp.Header.Get("Content-Range")
	expected := fmt.Sprintf("bytes 10-49/%d", len(data))
	if cr != expected {
		t.Errorf("GET range: Content-Range expected %q, got %q", expected, cr)
	}
}

// TestBlobs_GET_NotFound は未保存の sha への GET が 404 を返すことを検証する。
func TestBlobs_GET_NotFound(t *testing.T) {
	// Given
	bs := openTestBlobStore(t)
	srv := startBlobServer(t, bs, 10*1024*1024, 100*1024*1024)
	client := &http.Client{}

	sha := blobSHA([]byte("not stored blob"))

	// When
	resp := blobGET(t, client, srv.URL, sha)
	defer resp.Body.Close()

	// Then
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET not found: expected 404, got %d", resp.StatusCode)
	}
}

// TestBlobs_PUT_Resume は Content-Range で部分 PUT して 206、残り PUT して 201 を検証する。
func TestBlobs_PUT_Resume(t *testing.T) {
	// Given
	bs := openTestBlobStore(t)
	srv := startBlobServer(t, bs, 10*1024*1024, 100*1024*1024)
	client := &http.Client{}

	total := 200
	data := make([]byte, total)
	for i := range data {
		data[i] = byte(i % 256)
	}
	sha := blobSHA(data)
	first := data[:100]
	second := data[100:]

	// When: 最初の 100 byte を Content-Range で送る
	url := srv.URL + "/v1/blobs/" + sha
	req1, err := http.NewRequestWithContext(context.Background(), http.MethodPut, url, bytes.NewReader(first))
	if err != nil {
		t.Fatalf("NewRequest part1: %v", err)
	}
	req1.ContentLength = int64(len(first))
	req1.Header.Set("Authorization", "Bearer "+testPSK)
	req1.Header.Set("X-Axion-Client-ID", "client-blob-test")
	req1.Header.Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(first)-1, total))
	resp1, err := client.Do(req1)
	if err != nil {
		t.Fatalf("PUT part1: %v", err)
	}
	defer resp1.Body.Close()

	// Then: 206 Partial Content
	if resp1.StatusCode != http.StatusPartialContent {
		t.Errorf("PUT resume part1: expected 206, got %d", resp1.StatusCode)
	}

	// When: 残り 100 byte を送る
	req2, err := http.NewRequestWithContext(context.Background(), http.MethodPut, url, bytes.NewReader(second))
	if err != nil {
		t.Fatalf("NewRequest part2: %v", err)
	}
	req2.ContentLength = int64(len(second))
	req2.Header.Set("Authorization", "Bearer "+testPSK)
	req2.Header.Set("X-Axion-Client-ID", "client-blob-test")
	req2.Header.Set("Content-Range", fmt.Sprintf("bytes 100-%d/%d", total-1, total))
	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatalf("PUT part2: %v", err)
	}
	defer resp2.Body.Close()

	// Then: 201 Created
	if resp2.StatusCode != http.StatusCreated {
		t.Errorf("PUT resume part2: expected 201, got %d", resp2.StatusCode)
	}
	exists, err := bs.Has(context.Background(), sha)
	if err != nil {
		t.Fatalf("Has: %v", err)
	}
	if !exists {
		t.Error("PUT resume: blob not found after complete upload")
	}
}
