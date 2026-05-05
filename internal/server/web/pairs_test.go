package web_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	httpsrv "github.com/HMasataka/axion/internal/server/http"
	"github.com/HMasataka/axion/internal/server/store"
	"github.com/HMasataka/axion/internal/server/web"
)

// fakePairPublisher は pairPublisher を spy する。
type fakePairPublisher struct {
	updatedIDs      []string
	unsubscribedIDs []string
}

func (f *fakePairPublisher) PublishPairUpdate(_ context.Context, pairID string) error {
	f.updatedIDs = append(f.updatedIDs, pairID)
	return nil
}

func (f *fakePairPublisher) PublishPairUnsubscribe(_ context.Context, pairID string) error {
	f.unsubscribedIDs = append(f.unsubscribedIDs, pairID)
	return nil
}

func newHandlerWithPublisher(t *testing.T, s store.Store, pub *fakePairPublisher) http.Handler {
	t.Helper()
	return httpsrv.CSRF(web.Handler(web.Config{Store: s, Publisher: pub}))
}

func newHandlerWithCSRF(t *testing.T, s store.Store) http.Handler {
	t.Helper()
	return httpsrv.CSRF(web.Handler(web.Config{Store: s}))
}

func insertPair(t *testing.T, s store.Store, id, name, clientAID, clientBID string, enabled bool) store.SyncPair {
	t.Helper()
	now := time.Now()
	p := store.SyncPair{
		ID:        id,
		Name:      name,
		ClientAID: clientAID,
		PathA:     "/data/a",
		ClientBID: clientBID,
		PathB:     "/data/b",
		Direction: "bidirectional",
		Enabled:   enabled,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.UpsertPair(context.Background(), p); err != nil {
		t.Fatalf("UpsertPair: %v", err)
	}
	got, err := s.GetPair(context.Background(), id)
	if err != nil {
		t.Fatalf("GetPair after insert: %v", err)
	}
	return *got
}

func csrfToken(t *testing.T, h http.Handler) (token, rawCookie string) {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/pairs", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	for _, c := range w.Result().Cookies() {
		if c.Name == "axion_csrf" {
			return c.Value, c.Name + "=" + c.Value
		}
	}
	t.Fatal("no CSRF cookie in GET /pairs response (make sure handler is wrapped with CSRF middleware)")
	return "", ""
}

// TestPairs_List_RendersAll は GET /pairs がペア一覧を含む HTML を返すことを検証する。
func TestPairs_List_RendersAll(t *testing.T) {
	// Given
	s := openTestStore(t)
	insertClient(t, s, "ca", "host-a", "Alice")
	insertClient(t, s, "cb", "host-b", "Bob")
	insertPair(t, s, "p1", "alpha-sync", "ca", "cb", true)
	insertPair(t, s, "p2", "beta-sync", "ca", "cb", false)

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
	bs := string(body)
	for _, want := range []string{"alpha-sync", "beta-sync", "Alice", "Bob"} {
		if !strings.Contains(bs, want) {
			t.Errorf("want %q in pairs list HTML", want)
		}
	}
}

// TestPairs_New_GET_RendersForm は GET /pairs/new が新規作成フォームを返すことを検証する。
func TestPairs_New_GET_RendersForm(t *testing.T) {
	// Given
	s := openTestStore(t)
	insertClient(t, s, "ca", "host-a", "Alice")
	insertClient(t, s, "cb", "host-b", "Bob")

	h := newHandlerWithStore(t, s)
	r := httptest.NewRequest(http.MethodGet, "/pairs/new", nil)
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
	for _, want := range []string{`<form`, `name="name"`, `name="client_a_id"`, `name="client_b_id"`, `name="direction"`} {
		if !strings.Contains(bs, want) {
			t.Errorf("want %q in form HTML, got:\n%s", want, bs)
		}
	}
}

// TestPairs_Create_POST_InsertsAndAudits は POST /pairs/new がペアを作成し audit_log に記録することを検証する。
func TestPairs_Create_POST_InsertsAndAudits(t *testing.T) {
	// Given
	s := openTestStore(t)
	insertClient(t, s, "ca", "host-a", "Alice")
	insertClient(t, s, "cb", "host-b", "Bob")

	pub := &fakePairPublisher{}
	h := newHandlerWithPublisher(t, s, pub)

	tok, rawCookie := csrfToken(t, h)

	form := url.Values{
		"name":        {"new-pair"},
		"client_a_id": {"ca"},
		"path_a":      {"/src"},
		"client_b_id": {"cb"},
		"path_b":      {"/dst"},
		"direction":   {"bidirectional"},
		"enabled":     {"1"},
		"_csrf":       {tok},
	}
	r := httptest.NewRequest(http.MethodPost, "/pairs/new", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Cookie", rawCookie)
	w := httptest.NewRecorder()

	// When
	h.ServeHTTP(w, r)
	resp := w.Result()
	defer resp.Body.Close()

	// Then: 200 + ペアが表示されている
	if resp.StatusCode != http.StatusOK {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !strings.Contains(string(body), "new-pair") {
		t.Errorf("want pair name in response, got:\n%s", body)
	}

	// DB に記録されている
	pairs, err := s.ListPairs(context.Background())
	if err != nil {
		t.Fatalf("ListPairs: %v", err)
	}
	if len(pairs) != 1 {
		t.Errorf("want 1 pair in DB, got %d", len(pairs))
	}
	if pairs[0].Name != "new-pair" {
		t.Errorf("want pair name=new-pair, got %q", pairs[0].Name)
	}

	// audit_log に記録されている
	logs, err := s.ListRecentAuditLog(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListRecentAuditLog: %v", err)
	}
	var foundAudit bool
	for _, l := range logs {
		if l.Kind == "pair_create" {
			foundAudit = true
		}
	}
	if !foundAudit {
		t.Error("want pair_create in audit_log")
	}

	// PublishPairUpdate が呼ばれている
	if len(pub.updatedIDs) != 1 {
		t.Errorf("want 1 PublishPairUpdate call, got %d", len(pub.updatedIDs))
	}
}

// TestPairs_Edit_GET_PrefilledForm は GET /pairs/{id}/edit が既存値が入ったフォームを返すことを検証する。
func TestPairs_Edit_GET_PrefilledForm(t *testing.T) {
	// Given
	s := openTestStore(t)
	insertClient(t, s, "ca", "host-a", "Alice")
	insertClient(t, s, "cb", "host-b", "Bob")
	p := insertPair(t, s, "p-edit", "edit-me", "ca", "cb", true)

	h := newHandlerWithStore(t, s)
	r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/pairs/%s/edit", p.ID), nil)
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
	for _, want := range []string{`<form`, "edit-me", `name="etag"`, `name="_csrf"`} {
		if !strings.Contains(bs, want) {
			t.Errorf("want %q in edit form HTML, got:\n%s", want, bs)
		}
	}
}

// TestPairs_Update_POST_UpdatesAndAudits は POST /pairs/{id} がペアを更新し audit_log に記録することを検証する。
func TestPairs_Update_POST_UpdatesAndAudits(t *testing.T) {
	// Given
	s := openTestStore(t)
	insertClient(t, s, "ca", "host-a", "Alice")
	insertClient(t, s, "cb", "host-b", "Bob")
	p := insertPair(t, s, uuid.NewString(), "original-name", "ca", "cb", true)

	pub := &fakePairPublisher{}
	h := newHandlerWithPublisher(t, s, pub)

	tok, rawCookie := csrfToken(t, h)

	form := url.Values{
		"name":        {"updated-name"},
		"client_a_id": {"ca"},
		"path_a":      {"/src"},
		"client_b_id": {"cb"},
		"path_b":      {"/dst"},
		"direction":   {"a_to_b"},
		"enabled":     {"1"},
		"etag":        {fmt.Sprintf("%d", p.Etag)},
		"_csrf":       {tok},
	}
	r := httptest.NewRequest(http.MethodPost, "/pairs/"+p.ID, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Cookie", rawCookie)
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
	if !strings.Contains(string(body), "updated-name") {
		t.Errorf("want updated name in response, got:\n%s", body)
	}

	// DB が更新されている
	got, err := s.GetPair(context.Background(), p.ID)
	if err != nil {
		t.Fatalf("GetPair: %v", err)
	}
	if got.Name != "updated-name" {
		t.Errorf("want Name=updated-name, got %q", got.Name)
	}

	// audit_log に記録されている
	logs, err := s.ListRecentAuditLog(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListRecentAuditLog: %v", err)
	}
	var foundAudit bool
	for _, l := range logs {
		if l.Kind == "pair_update" {
			foundAudit = true
		}
	}
	if !foundAudit {
		t.Error("want pair_update in audit_log")
	}

	// PublishPairUpdate が呼ばれている
	if len(pub.updatedIDs) != 1 {
		t.Errorf("want 1 PublishPairUpdate call, got %d", len(pub.updatedIDs))
	}
}

// TestPairs_Update_POST_EtagMismatch_412 は古い etag を送ると 412 が返ることを検証する。
func TestPairs_Update_POST_EtagMismatch_412(t *testing.T) {
	// Given
	s := openTestStore(t)
	insertClient(t, s, "ca", "host-a", "Alice")
	insertClient(t, s, "cb", "host-b", "Bob")
	p := insertPair(t, s, uuid.NewString(), "original", "ca", "cb", true)

	h := newHandlerWithCSRF(t, s)
	tok, rawCookie := csrfToken(t, h)

	form := url.Values{
		"name":        {"should-not-apply"},
		"client_a_id": {"ca"},
		"path_a":      {"/src"},
		"client_b_id": {"cb"},
		"path_b":      {"/dst"},
		"direction":   {"bidirectional"},
		"enabled":     {"1"},
		"etag":        {"9999"}, // 古い etag
		"_csrf":       {tok},
	}
	r := httptest.NewRequest(http.MethodPost, "/pairs/"+p.ID, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Cookie", rawCookie)
	w := httptest.NewRecorder()

	// When
	h.ServeHTTP(w, r)
	resp := w.Result()
	defer resp.Body.Close()

	// Then: 412 Precondition Failed
	if resp.StatusCode != http.StatusPreconditionFailed {
		t.Errorf("want 412, got %d", resp.StatusCode)
	}
}

// TestPairs_Delete_DELETE_RemovesAndAudits は DELETE /pairs/{id} がペアを削除し audit_log に記録することを検証する。
func TestPairs_Delete_DELETE_RemovesAndAudits(t *testing.T) {
	// Given
	s := openTestStore(t)
	insertClient(t, s, "ca", "host-a", "Alice")
	insertClient(t, s, "cb", "host-b", "Bob")
	p := insertPair(t, s, uuid.NewString(), "to-delete", "ca", "cb", true)

	pub := &fakePairPublisher{}
	h := newHandlerWithPublisher(t, s, pub)

	tok, rawCookie := csrfToken(t, h)

	r := httptest.NewRequest(http.MethodDelete, "/pairs/"+p.ID, nil)
	r.Header.Set("X-CSRF-Token", tok)
	r.Header.Set("Cookie", rawCookie)
	w := httptest.NewRecorder()

	// When
	h.ServeHTTP(w, r)
	resp := w.Result()
	defer resp.Body.Close()

	// Then: 200
	if resp.StatusCode != http.StatusOK {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}

	// DB から削除されている
	pairs, err := s.ListPairs(context.Background())
	if err != nil {
		t.Fatalf("ListPairs: %v", err)
	}
	for _, pp := range pairs {
		if pp.ID == p.ID {
			t.Error("want pair deleted from DB")
		}
	}

	// audit_log に記録されている
	logs, err := s.ListRecentAuditLog(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListRecentAuditLog: %v", err)
	}
	var foundAudit bool
	for _, l := range logs {
		if l.Kind == "pair_delete" {
			foundAudit = true
		}
	}
	if !foundAudit {
		t.Error("want pair_delete in audit_log")
	}

	// PublishPairUnsubscribe が呼ばれている
	if len(pub.unsubscribedIDs) != 1 {
		t.Errorf("want 1 PublishPairUnsubscribe call, got %d", len(pub.unsubscribedIDs))
	}
}

// TestPairs_Update_DisabledPair_PublishesUnsubscribe は enabled=true→false 更新時に
// PublishPairUnsubscribe が呼ばれることを検証する。
func TestPairs_Update_DisabledPair_PublishesUnsubscribe(t *testing.T) {
	// Given
	s := openTestStore(t)
	insertClient(t, s, "ca", "host-a", "Alice")
	insertClient(t, s, "cb", "host-b", "Bob")
	p := insertPair(t, s, uuid.NewString(), "disable-me", "ca", "cb", true) // enabled=true

	pub := &fakePairPublisher{}
	h := newHandlerWithPublisher(t, s, pub)

	tok, rawCookie := csrfToken(t, h)

	form := url.Values{
		"name":        {"disable-me"},
		"client_a_id": {"ca"},
		"path_a":      {"/src"},
		"client_b_id": {"cb"},
		"path_b":      {"/dst"},
		"direction":   {"bidirectional"},
		// enabled は送らない → false
		"etag":  {fmt.Sprintf("%d", p.Etag)},
		"_csrf": {tok},
	}
	r := httptest.NewRequest(http.MethodPost, "/pairs/"+p.ID, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Cookie", rawCookie)
	w := httptest.NewRecorder()

	// When
	h.ServeHTTP(w, r)
	resp := w.Result()
	defer resp.Body.Close()

	// Then: 200
	if resp.StatusCode != http.StatusOK {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}

	// PublishPairUnsubscribe が呼ばれている (enabled: true→false)
	if len(pub.unsubscribedIDs) != 1 {
		t.Errorf("want 1 PublishPairUnsubscribe call, got %d", len(pub.unsubscribedIDs))
	}
	if len(pub.updatedIDs) != 0 {
		t.Errorf("want 0 PublishPairUpdate calls, got %d", len(pub.updatedIDs))
	}
}

// TestPairs_Create_PublishesPairUpdate は POST /pairs/new で PublishPairUpdate が呼ばれることを検証する。
func TestPairs_Create_PublishesPairUpdate(t *testing.T) {
	// Given
	s := openTestStore(t)
	insertClient(t, s, "ca", "host-a", "Alice")
	insertClient(t, s, "cb", "host-b", "Bob")

	pub := &fakePairPublisher{}
	h := newHandlerWithPublisher(t, s, pub)

	tok, rawCookie := csrfToken(t, h)

	form := url.Values{
		"name":        {"publish-test"},
		"client_a_id": {"ca"},
		"path_a":      {"/src"},
		"client_b_id": {"cb"},
		"path_b":      {"/dst"},
		"direction":   {"bidirectional"},
		"enabled":     {"1"},
		"_csrf":       {tok},
	}
	r := httptest.NewRequest(http.MethodPost, "/pairs/new", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Cookie", rawCookie)
	w := httptest.NewRecorder()

	// When
	h.ServeHTTP(w, r)

	// Then: PublishPairUpdate が呼ばれている
	if len(pub.updatedIDs) != 1 {
		t.Errorf("want 1 PublishPairUpdate call, got %d", len(pub.updatedIDs))
	}
}
