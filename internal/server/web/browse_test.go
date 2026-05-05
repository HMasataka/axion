package web_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/HMasataka/axion/internal/proto"
	"github.com/HMasataka/axion/internal/server/store"
	"github.com/HMasataka/axion/internal/server/web"
)

// fakeHub は HubSender の fake 実装。
type fakeHub struct {
	online   bool
	response proto.Envelope
	err      error
}

func (f *fakeHub) IsOnline(_ string) bool {
	return f.online
}

func (f *fakeHub) SendAndWait(_ context.Context, _ string, _ proto.Envelope, _ time.Duration) (proto.Envelope, error) {
	return f.response, f.err
}

func mustMarshalPayload(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := proto.MarshalPayload(v)
	if err != nil {
		t.Fatalf("MarshalPayload: %v", err)
	}
	return raw
}

func listDirResponseEnvelope(t *testing.T, resp proto.ListDirResponse) proto.Envelope {
	t.Helper()
	return proto.Envelope{
		Type:    proto.TypeListDirResponse,
		Payload: mustMarshalPayload(t, resp),
	}
}

func newBrowseHandler(t *testing.T, s store.Store, h web.HubSender) http.Handler {
	t.Helper()
	return web.Handler(web.Config{Store: s, Hub: h})
}

// TestBrowse_NotFoundClient は存在しない clientID で 404 が返ることを検証する。
func TestBrowse_NotFoundClient(t *testing.T) {
	// Given
	s := openTestStore(t)
	h := newBrowseHandler(t, s, nil)
	r := httptest.NewRequest(http.MethodGet, "/clients/no-such-id/browse?path=/", nil)
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

// TestBrowse_OfflineClient_RendersError は hub.IsOnline=false のとき "client offline" が表示されることを検証する。
func TestBrowse_OfflineClient_RendersError(t *testing.T) {
	// Given
	s := openTestStore(t)
	insertClient(t, s, "c-offline", "host-offline", "Offline Client")

	hub := &fakeHub{online: false}
	h := newBrowseHandler(t, s, hub)
	r := httptest.NewRequest(http.MethodGet, "/clients/c-offline/browse?path=/", nil)
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
	if !strings.Contains(string(body), "client offline") {
		t.Errorf("want 'client offline' in body, got:\n%s", body)
	}
}

// TestBrowse_OnlineClient_RendersEntries は hub から ListDirResponse が返ったとき entries が HTML に含まれることを検証する。
func TestBrowse_OnlineClient_RendersEntries(t *testing.T) {
	// Given
	s := openTestStore(t)
	insertClient(t, s, "c-online", "host-online", "Online Client")

	fakeResp := proto.ListDirResponse{
		Entries: []proto.DirEntry{
			{Name: "subdir", IsDir: true},
			{Name: "readme.txt", IsDir: false, Size: 42},
		},
	}
	hub := &fakeHub{
		online:   true,
		response: listDirResponseEnvelope(t, fakeResp),
	}
	h := newBrowseHandler(t, s, hub)
	r := httptest.NewRequest(http.MethodGet, "/clients/c-online/browse?path=.", nil)
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
	for _, want := range []string{"subdir/", "readme.txt"} {
		if !strings.Contains(bodyStr, want) {
			t.Errorf("want %q in body, got:\n%s", want, bodyStr)
		}
	}
}

// TestBrowse_HTMXRequest_RendersListingOnly は HX-Request: true のとき base レイアウトなしの部分 HTML が返ることを検証する。
func TestBrowse_HTMXRequest_RendersListingOnly(t *testing.T) {
	// Given
	s := openTestStore(t)
	insertClient(t, s, "c-htmx", "host-htmx", "HTMX Client")

	fakeResp := proto.ListDirResponse{
		Entries: []proto.DirEntry{
			{Name: "file.go", IsDir: false, Size: 100},
		},
	}
	hub := &fakeHub{
		online:   true,
		response: listDirResponseEnvelope(t, fakeResp),
	}
	h := newBrowseHandler(t, s, hub)
	r := httptest.NewRequest(http.MethodGet, "/clients/c-htmx/browse?path=.", nil)
	r.Header.Set("HX-Request", "true")
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
	if strings.Contains(bodyStr, "<html") {
		t.Errorf("HTMX response should not contain full <html> layout, got:\n%s", bodyStr)
	}
	if !strings.Contains(bodyStr, "file.go") {
		t.Errorf("want 'file.go' in HTMX partial body, got:\n%s", bodyStr)
	}
}
