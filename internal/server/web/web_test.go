package web_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HMasataka/axion/internal/server/store"
	"github.com/HMasataka/axion/internal/server/web"
)

func newHandler(t *testing.T) http.Handler {
	t.Helper()
	return web.Handler(web.Config{})
}

func openTestStore(t *testing.T) store.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func insertClient(t *testing.T, s store.Store, id, hostname, displayName string) {
	t.Helper()
	now := time.Now()
	c := store.Client{
		ID:           id,
		DisplayName:  displayName,
		Hostname:     hostname,
		RootPath:     "/data",
		Version:      "1.0",
		ProtoVersion: "1",
		Status:       "online",
		LastSeen:     now,
		CreatedAt:    now,
		UpdatedAt:    now,
		Etag:         1,
	}
	if err := s.UpsertClient(context.Background(), c); err != nil {
		t.Fatalf("UpsertClient: %v", err)
	}
}

func newHandlerWithStore(t *testing.T, s store.Store) http.Handler {
	t.Helper()
	return web.Handler(web.Config{Store: s})
}

// TestStaticAssets_PicoCSS_Served は GET /static/pico.css が 200 + text/css を返すことを検証する。
func TestStaticAssets_PicoCSS_Served(t *testing.T) {
	// Given
	h := newHandler(t)
	r := httptest.NewRequest(http.MethodGet, "/static/pico.css", nil)
	w := httptest.NewRecorder()

	// When
	h.ServeHTTP(w, r)
	resp := w.Result()
	defer resp.Body.Close()

	// Then
	if resp.StatusCode != http.StatusOK {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/css") {
		t.Errorf("want Content-Type text/css, got %q", ct)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(body) == 0 {
		t.Error("want non-empty body for pico.css")
	}
}

// TestStaticAssets_HTMX_Served は GET /static/htmx.min.js が 200 + JavaScript を返すことを検証する。
func TestStaticAssets_HTMX_Served(t *testing.T) {
	// Given
	h := newHandler(t)
	r := httptest.NewRequest(http.MethodGet, "/static/htmx.min.js", nil)
	w := httptest.NewRecorder()

	// When
	h.ServeHTTP(w, r)
	resp := w.Result()
	defer resp.Body.Close()

	// Then
	if resp.StatusCode != http.StatusOK {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "javascript") {
		t.Errorf("want Content-Type containing javascript, got %q", ct)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(body) == 0 {
		t.Error("want non-empty body for htmx.min.js")
	}
}

// TestStaticAssets_NotFound は GET /static/nonexistent が 404 を返すことを検証する。
func TestStaticAssets_NotFound(t *testing.T) {
	// Given
	h := newHandler(t)
	r := httptest.NewRequest(http.MethodGet, "/static/nonexistent", nil)
	w := httptest.NewRecorder()

	// When
	h.ServeHTTP(w, r)
	resp := w.Result()
	defer resp.Body.Close()

	// Then
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("want 404, got %d", resp.StatusCode)
	}
}

// TestRoot_RendersClientsPage は GET / が 200 + <title>Clients を含む HTML を返すことを検証する。
func TestRoot_RendersClientsPage(t *testing.T) {
	// Given
	s := openTestStore(t)
	h := newHandlerWithStore(t, s)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
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
	if !strings.Contains(string(body), "<title>Clients") {
		t.Errorf("want <title>Clients in body, got:\n%s", body)
	}
}

// TestPairs_RendersPage は GET /pairs が 200 + <title>Sync Pairs を含む HTML を返すことを検証する。
func TestPairs_RendersPage(t *testing.T) {
	// Given
	s := openTestStore(t)
	h := newHandlerWithStore(t, s)
	r := httptest.NewRequest(http.MethodGet, "/pairs", nil)
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
	if !strings.Contains(string(body), "<title>Sync Pairs") {
		t.Errorf("want <title>Sync Pairs in body, got:\n%s", body)
	}
}

// TestSettings_RendersPage は GET /settings が 200 + <title>Settings を含む HTML を返すことを検証する。
func TestSettings_RendersPage(t *testing.T) {
	// Given
	s := openTestStore(t)
	h := newHandlerWithStore(t, s)
	r := httptest.NewRequest(http.MethodGet, "/settings", nil)
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
	if !strings.Contains(string(body), "<title>Settings") {
		t.Errorf("want <title>Settings in body, got:\n%s", body)
	}
}

// TestRoot_OtherPathReturns404 は GET /unknown が 404 を返すことを検証する。
func TestRoot_OtherPathReturns404(t *testing.T) {
	// Given
	s := openTestStore(t)
	h := newHandlerWithStore(t, s)
	r := httptest.NewRequest(http.MethodGet, "/unknown", nil)
	w := httptest.NewRecorder()

	// When
	h.ServeHTTP(w, r)
	resp := w.Result()
	defer resp.Body.Close()

	// Then
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("want 404, got %d", resp.StatusCode)
	}
}

// TestClientsList_RendersWithClients は2クライアントを投入後 GET / でそれらが表示されることを検証する。
func TestClientsList_RendersWithClients(t *testing.T) {
	// Given
	s := openTestStore(t)
	insertClient(t, s, "c-1", "host-alpha", "Alice")
	insertClient(t, s, "c-2", "host-beta", "Bob")

	h := newHandlerWithStore(t, s)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
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
	for _, want := range []string{"Alice", "host-alpha", "Bob", "host-beta"} {
		if !strings.Contains(bodyStr, want) {
			t.Errorf("want %q in body", want)
		}
	}
}

// TestClientsList_ShowsDisplayName は display_name が設定されている場合にそれが表示されることを検証する。
func TestClientsList_ShowsDisplayName(t *testing.T) {
	// Given
	s := openTestStore(t)
	insertClient(t, s, "c-3", "host-gamma", "Custom Name")

	h := newHandlerWithStore(t, s)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	// When
	h.ServeHTTP(w, r)
	resp := w.Result()
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	// Then: display_name が表示されている
	if !strings.Contains(string(body), "Custom Name") {
		t.Errorf("want display_name %q in body, got:\n%s", "Custom Name", body)
	}
}

// TestClientEdit_GET_RendersForm は GET /clients/{id}/edit が編集フォームを返すことを検証する。
func TestClientEdit_GET_RendersForm(t *testing.T) {
	// Given
	s := openTestStore(t)
	insertClient(t, s, "c-edit", "host-edit", "Edit Me")

	h := newHandlerWithStore(t, s)
	r := httptest.NewRequest(http.MethodGet, "/clients/c-edit/edit", nil)
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
	for _, want := range []string{`<form`, `name="display_name"`, `name="etag"`, `name="_csrf"`} {
		if !strings.Contains(bodyStr, want) {
			t.Errorf("want %q in form HTML, got:\n%s", want, bodyStr)
		}
	}
}

// TestClientDisplayName_POST_UpdatesAndReturnsRow は POST /clients/{id}/display-name が
// DB を更新してクライアント行 HTML を返すことを検証する。
func TestClientDisplayName_POST_UpdatesAndReturnsRow(t *testing.T) {
	// Given
	s := openTestStore(t)
	insertClient(t, s, "c-upd", "host-upd", "Original")

	h := newHandlerWithStore(t, s)

	form := url.Values{}
	form.Set("etag", "1")
	form.Set("display_name", "Updated Name")
	form.Set("_csrf", "test-token")

	r := httptest.NewRequest(http.MethodPost, "/clients/c-upd/display-name", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(&http.Cookie{Name: "axion_csrf", Value: "test-token"})
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
	if !strings.Contains(string(body), "Updated Name") {
		t.Errorf("want %q in response body, got:\n%s", "Updated Name", body)
	}

	// DB にも反映されている
	c, err := s.GetClient(context.Background(), "c-upd")
	if err != nil {
		t.Fatalf("GetClient: %v", err)
	}
	if c.DisplayName != "Updated Name" {
		t.Errorf("want DisplayName=%q, got %q", "Updated Name", c.DisplayName)
	}
}

// TestClientDisplayName_POST_EtagMismatch_412 は古い etag を送ると 412 が返ることを検証する。
func TestClientDisplayName_POST_EtagMismatch_412(t *testing.T) {
	// Given
	s := openTestStore(t)
	insertClient(t, s, "c-etag", "host-etag", "Before")

	h := newHandlerWithStore(t, s)

	form := url.Values{}
	form.Set("etag", "999") // 古い etag
	form.Set("display_name", "Should Not Apply")
	form.Set("_csrf", "test-token")

	r := httptest.NewRequest(http.MethodPost, "/clients/c-etag/display-name", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(&http.Cookie{Name: "axion_csrf", Value: "test-token"})
	w := httptest.NewRecorder()

	// When
	h.ServeHTTP(w, r)
	resp := w.Result()
	defer resp.Body.Close()

	// Then: 412 Precondition Failed
	if resp.StatusCode != http.StatusPreconditionFailed {
		t.Errorf("want 412, got %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !strings.Contains(string(body), "conflict") {
		t.Errorf("want conflict message in body, got:\n%s", body)
	}
}

// TestClientDisplayName_POST_NonExistent_404 は存在しない id に PATCH すると適切なエラーを返すことを検証する。
func TestClientDisplayName_POST_NonExistent_404(t *testing.T) {
	// Given
	s := openTestStore(t)
	h := newHandlerWithStore(t, s)

	form := url.Values{}
	form.Set("etag", "1")
	form.Set("display_name", "Ghost")
	form.Set("_csrf", "test-token")

	r := httptest.NewRequest(http.MethodPost, "/clients/no-such-id/display-name", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(&http.Cookie{Name: "axion_csrf", Value: "test-token"})
	w := httptest.NewRecorder()

	// When
	h.ServeHTTP(w, r)
	resp := w.Result()
	defer resp.Body.Close()

	// Then: ErrEtagMismatch (0件更新) → 412
	if resp.StatusCode != http.StatusPreconditionFailed {
		t.Errorf("want 412 (etag mismatch for missing client), got %d", resp.StatusCode)
	}
}

// TestClientCancel_GET_ReturnsRow は GET /clients/{id}/cancel が元の行 HTML を返すことを検証する。
func TestClientCancel_GET_ReturnsRow(t *testing.T) {
	// Given
	s := openTestStore(t)
	insertClient(t, s, "c-cancel", "host-cancel", "Cancel Me")

	h := newHandlerWithStore(t, s)
	r := httptest.NewRequest(http.MethodGet, "/clients/c-cancel/cancel", nil)
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
	if !strings.Contains(string(body), "Cancel Me") {
		t.Errorf("want hostname in row HTML, got:\n%s", body)
	}
	// フォームは含まれない
	if strings.Contains(string(body), `<form`) {
		t.Errorf("cancel response should not contain form, got:\n%s", body)
	}
}

// TestClientEdit_GET_NonExistent は存在しない id で GET /clients/{id}/edit が 404 を返すことを検証する。
func TestClientEdit_GET_NonExistent(t *testing.T) {
	// Given
	s := openTestStore(t)
	h := newHandlerWithStore(t, s)
	r := httptest.NewRequest(http.MethodGet, "/clients/no-such-id/edit", nil)
	w := httptest.NewRecorder()

	// When
	h.ServeHTTP(w, r)
	resp := w.Result()
	defer resp.Body.Close()

	// Then
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("want 404, got %d", resp.StatusCode)
	}
}

// TestClientRow_MethodNotAllowed は不正なメソッドで 405 を返すことを検証する。
func TestClientRow_MethodNotAllowed(t *testing.T) {
	// Given
	s := openTestStore(t)
	insertClient(t, s, "c-method", "host-method", "Method Test")
	h := newHandlerWithStore(t, s)

	tests := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/clients/c-method/edit"},
		{http.MethodGet, "/clients/c-method/display-name"},
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("%s %s", tc.method, tc.path), func(t *testing.T) {
			// Given
			r := httptest.NewRequest(tc.method, tc.path, nil)
			w := httptest.NewRecorder()

			// When
			h.ServeHTTP(w, r)
			resp := w.Result()
			defer resp.Body.Close()

			// Then
			if resp.StatusCode != http.StatusMethodNotAllowed {
				t.Errorf("want 405, got %d", resp.StatusCode)
			}
		})
	}
}
