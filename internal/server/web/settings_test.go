package web_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func insertSetting(t *testing.T, s interface {
	SetSetting(ctx context.Context, key, value string) error
}, key, value string) {
	t.Helper()
	if err := s.SetSetting(context.Background(), key, value); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
}

// TestSettings_List_RendersDefaults は GET /settings が全設定キーを含む HTML を返すことを検証する。
func TestSettings_List_RendersDefaults(t *testing.T) {
	// Given
	s := openTestStore(t)
	insertSetting(t, s, "ignore_list", `["+.git",".DS_Store"]`)
	insertSetting(t, s, "blob_gc_age_seconds", "604800")
	insertSetting(t, s, "max_file_size_bytes", "1073741824")

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
	bs := string(body)
	for _, key := range []string{"ignore_list", "blob_gc_age_seconds", "max_file_size_bytes"} {
		if !strings.Contains(bs, key) {
			t.Errorf("want %q in body", key)
		}
	}
}

// TestSettings_Edit_GET_RendersForm は GET /settings/{key}/edit が編集フォームを返すことを検証する。
func TestSettings_Edit_GET_RendersForm(t *testing.T) {
	// Given
	s := openTestStore(t)
	insertSetting(t, s, "ignore_list", `["+.git",".DS_Store"]`)

	h := newHandlerWithStore(t, s)
	r := httptest.NewRequest(http.MethodGet, "/settings/ignore_list/edit", nil)
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
	bs := string(body)
	if !strings.Contains(bs, "<form") {
		t.Errorf("want <form in body")
	}
	// Go の html/template は " と + を HTML エスケープする。
	// フォームの value 属性で現在値が含まれることを確認する。
	if !strings.Contains(bs, `name="value"`) {
		t.Errorf("want value input in form")
	}
	if !strings.Contains(bs, ".git") {
		t.Errorf("want .git in form value")
	}
}

// TestSettings_Update_POST_PersistsAndReturnsRow は POST /settings/{key} が DB 更新後に行 HTML を返すことを検証する。
func TestSettings_Update_POST_PersistsAndReturnsRow(t *testing.T) {
	// Given
	s := openTestStore(t)
	insertSetting(t, s, "ignore_list", `["+.git"]`)

	h := newHandlerWithStore(t, s)
	form := url.Values{"value": {`["foo"]`}, "_csrf": {"test-token"}}
	r := httptest.NewRequest(http.MethodPost, "/settings/ignore_list", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(&http.Cookie{Name: "axion_csrf", Value: "test-token"})
	w := httptest.NewRecorder()

	// When
	h.ServeHTTP(w, r)
	resp := w.Result()
	defer resp.Body.Close()

	// Then: レスポンスに更新値が含まれる
	if resp.StatusCode != http.StatusOK {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	bs := string(body)
	if !strings.Contains(bs, "ignore_list") {
		t.Errorf("want key in returned row HTML")
	}
	// Go の html/template は " を HTML エスケープするため &#34;foo&#34; になる。
	if !strings.Contains(bs, "foo") {
		t.Errorf("want updated value in returned row HTML")
	}

	// DB にも反映されている
	got, err := s.GetSetting(context.Background(), "ignore_list")
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if got != `["foo"]` {
		t.Errorf("want persisted value=%q, got %q", `["foo"]`, got)
	}
}

// TestSettings_Cancel_GET_RendersOriginalRow は GET /settings/{key}/cancel が元の行 HTML を返すことを検証する。
func TestSettings_Cancel_GET_RendersOriginalRow(t *testing.T) {
	// Given
	s := openTestStore(t)
	insertSetting(t, s, "blob_gc_age_seconds", "604800")

	h := newHandlerWithStore(t, s)
	r := httptest.NewRequest(http.MethodGet, "/settings/blob_gc_age_seconds/cancel", nil)
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
	bs := string(body)
	if !strings.Contains(bs, "blob_gc_age_seconds") {
		t.Errorf("want key in row HTML")
	}
	if !strings.Contains(bs, "604800") {
		t.Errorf("want original value in row HTML")
	}
	if strings.Contains(bs, "<form") {
		t.Errorf("want no form in cancelled row")
	}
}

// TestSettings_Update_NewKey_Persists は既存キー以外も SetSetting で書き込めることを検証する。
func TestSettings_Update_NewKey_Persists(t *testing.T) {
	// Given
	s := openTestStore(t)
	h := newHandlerWithStore(t, s)

	form := url.Values{"value": {"42"}, "_csrf": {"test-token"}}
	r := httptest.NewRequest(http.MethodPost, "/settings/new_future_key", strings.NewReader(form.Encode()))
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

	// DB にも反映されている
	got, err := s.GetSetting(context.Background(), "new_future_key")
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if got != "42" {
		t.Errorf("want persisted value=%q, got %q", "42", got)
	}
}
