package web_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/HMasataka/axion/internal/server/store"
)

func insertSyncRun(t *testing.T, s store.Store, pairID, status string) int64 {
	t.Helper()
	now := time.Now()
	sha := "abcdef1234567890"
	r := store.SyncRun{
		PairID:      pairID,
		SrcClientID: "src-client",
		DstClientID: "dst-client",
		RelPath:     "docs/file.txt",
		SHA256:      &sha,
		Bytes:       1024,
		Status:      status,
		StartedAt:   now,
		FinishedAt:  now.Add(100 * time.Millisecond),
	}
	id, err := s.InsertSyncRun(context.Background(), r)
	if err != nil {
		t.Fatalf("InsertSyncRun: %v", err)
	}
	return id
}

func ensureClients(t *testing.T, s store.Store) {
	t.Helper()
	insertClient(t, s, "client-a", "host-a", "Client A")
	insertClient(t, s, "client-b", "host-b", "Client B")
}

func insertSyncPair(t *testing.T, s store.Store, id, name string) {
	t.Helper()
	ensureClients(t, s)
	now := time.Now()
	p := store.SyncPair{
		ID:        id,
		Name:      name,
		ClientAID: "client-a",
		PathA:     "/data/a",
		ClientBID: "client-b",
		PathB:     "/data/b",
		Direction: "bidirectional",
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
		Etag:      1,
	}
	if err := s.UpsertPair(context.Background(), p); err != nil {
		t.Fatalf("UpsertPair: %v", err)
	}
}

// TestRuns_Empty_RendersNoRuns は runs が 0 件のときに "No sync runs yet" が表示されることを検証する。
func TestRuns_Empty_RendersNoRuns(t *testing.T) {
	// Given
	s := openTestStore(t)
	h := newHandlerWithStore(t, s)
	r := httptest.NewRequest(http.MethodGet, "/runs", nil)
	w := httptest.NewRecorder()

	// When
	h.ServeHTTP(w, r)
	resp := w.Result()
	defer resp.Body.Close()

	// Then
	if resp.StatusCode != http.StatusOK {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !strings.Contains(string(body), "No sync runs yet") {
		t.Errorf("want 'No sync runs yet' in body, got:\n%s", body)
	}
}

// TestRuns_RendersAll は 3 件の run を投入後に全件が表示されることを検証する。
func TestRuns_RendersAll(t *testing.T) {
	// Given
	s := openTestStore(t)
	insertSyncPair(t, s, "pair-1", "Pair One")
	insertSyncRun(t, s, "pair-1", "ok")
	insertSyncRun(t, s, "pair-1", "failed")
	insertSyncRun(t, s, "pair-1", "skipped")

	h := newHandlerWithStore(t, s)
	r := httptest.NewRequest(http.MethodGet, "/runs", nil)
	w := httptest.NewRecorder()

	// When
	h.ServeHTTP(w, r)
	resp := w.Result()
	defer resp.Body.Close()

	// Then
	if resp.StatusCode != http.StatusOK {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	bodyStr := string(body)
	for _, want := range []string{"ok", "failed", "skipped", "Pair One"} {
		if !strings.Contains(bodyStr, want) {
			t.Errorf("want %q in body, got:\n%s", want, bodyStr)
		}
	}
}

// TestRuns_FilterByPair は pair_id クエリで絞り込みが機能することを検証する。
func TestRuns_FilterByPair(t *testing.T) {
	// Given
	s := openTestStore(t)
	insertSyncPair(t, s, "pair-a", "Pair Alpha")
	insertSyncPair(t, s, "pair-b", "Pair Beta")
	insertSyncRun(t, s, "pair-a", "ok")
	insertSyncRun(t, s, "pair-b", "ok")

	h := newHandlerWithStore(t, s)
	r := httptest.NewRequest(http.MethodGet, "/runs?pair_id=pair-a", nil)
	w := httptest.NewRecorder()

	// When
	h.ServeHTTP(w, r)
	resp := w.Result()
	defer resp.Body.Close()

	// Then
	if resp.StatusCode != http.StatusOK {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	bodyStr := string(body)
	// pair-a の run のペア名が表示されていること
	if !strings.Contains(bodyStr, "Pair Alpha") {
		t.Errorf("want 'Pair Alpha' in body, got:\n%s", bodyStr)
	}
	// pair-b の run は table 行に含まれないこと (ドロップダウンには全ペアが表示されるため tbody のみを検証)
	tbodyStart := strings.Index(bodyStr, "<tbody>")
	tbodyEnd := strings.Index(bodyStr, "</tbody>")
	if tbodyStart < 0 || tbodyEnd < 0 {
		t.Fatalf("tbody not found in body")
	}
	tbody := bodyStr[tbodyStart:tbodyEnd]
	if strings.Contains(tbody, "Pair Beta") {
		t.Errorf("want 'Pair Beta' NOT in tbody when filtered by pair-a, got:\n%s", tbody)
	}
}

// TestRuns_FilterByStatus は status クエリで絞り込みが機能することを検証する。
func TestRuns_FilterByStatus(t *testing.T) {
	// Given
	s := openTestStore(t)
	insertSyncPair(t, s, "pair-s", "Pair Status")
	insertSyncRun(t, s, "pair-s", "ok")
	insertSyncRun(t, s, "pair-s", "failed")

	h := newHandlerWithStore(t, s)
	r := httptest.NewRequest(http.MethodGet, "/runs?status=failed", nil)
	w := httptest.NewRecorder()

	// When
	h.ServeHTTP(w, r)
	resp := w.Result()
	defer resp.Body.Close()

	// Then
	if resp.StatusCode != http.StatusOK {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	bodyStr := string(body)
	if !strings.Contains(bodyStr, "failed") {
		t.Errorf("want 'failed' in body, got:\n%s", bodyStr)
	}
	// "ok" のみの行は存在しないはずだが、"ok" という文字列はボタン等に出現しうるため
	// より具体的な検証は行わない。行数は1行のみのはず。
	if strings.Count(bodyStr, "<tr>") > 2 {
		t.Errorf("want at most 1 data row (thead + 1 tbody tr), got more:\n%s", bodyStr)
	}
}

// TestRuns_LimitClamped は limit=2000 を指定すると max 1000 に制限されることを検証する。
func TestRuns_LimitClamped(t *testing.T) {
	// Given
	s := openTestStore(t)
	h := newHandlerWithStore(t, s)
	r := httptest.NewRequest(http.MethodGet, "/runs?limit=2000", nil)
	w := httptest.NewRecorder()

	// When
	h.ServeHTTP(w, r)
	resp := w.Result()
	defer resp.Body.Close()

	// Then: 2000 は max 1000 にクランプされるが 200 で返る
	if resp.StatusCode != http.StatusOK {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	// limit=1000 がフォームに反映されていること
	if !strings.Contains(string(body), `value="1000"`) {
		t.Errorf("want limit clamped to 1000 in form, got:\n%s", body)
	}
}
