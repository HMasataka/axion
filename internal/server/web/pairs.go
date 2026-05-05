package web

import (
	"context"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	httpsrv "github.com/HMasataka/axion/internal/server/http"
	"github.com/HMasataka/axion/internal/server/store"
)

// PairPublisher は Engine の pair 通知メソッドを抽象化する。
// *syncengine.Engine と fake 実装の両方が満たせるようにする。
type PairPublisher interface {
	PublishPairUpdate(ctx context.Context, pairID string) error
	PublishPairUnsubscribe(ctx context.Context, pairID string) error
}

// PairView は pairs.tmpl で利用するビュー構造体。
type PairView struct {
	store.SyncPair
	ClientAName string
	ClientBName string
	CSRFToken   string
}

// pairEditData は pair_edit_form テンプレートに渡すデータ。
type pairEditData struct {
	Action    string
	Pair      PairView
	Clients   []store.Client
	CSRFToken string
}

func toPairView(p store.SyncPair, clientsByID map[string]store.Client, csrf string) PairView {
	view := PairView{SyncPair: p, CSRFToken: csrf}
	if c, ok := clientsByID[p.ClientAID]; ok {
		view.ClientAName = clientDisplayName(c)
	}
	if c, ok := clientsByID[p.ClientBID]; ok {
		view.ClientBName = clientDisplayName(c)
	}
	return view
}

func clientDisplayName(c store.Client) string {
	if c.DisplayName != "" {
		return c.DisplayName
	}
	return c.Hostname
}

// pairsListHandler は GET /pairs のペア一覧ページを返す。
func pairsListHandler(cfg Config, tmpl *template.Template) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		pairs, clients, err := loadPairsAndClients(r.Context(), cfg.Store)
		if err != nil {
			slog.ErrorContext(r.Context(), "pairs list load", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		csrf := httpsrv.CSRFTokenFromContext(r.Context())
		clientsByID := clientsMap(clients)
		views := make([]PairView, len(pairs))
		for i, p := range pairs {
			views[i] = toPairView(p, clientsByID, csrf)
		}

		renderPage(w, tmpl, map[string]any{
			"Title": "Sync Pairs",
			"Pairs": views,
		})
	}
}

// pairFormHandler は /pairs/ 以下のサブルートを担当する。
func pairFormHandler(cfg Config, tmpl *template.Template) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/pairs/")
		switch {
		case path == "new" && r.Method == http.MethodGet:
			handlePairNewForm(w, r, cfg, tmpl)
		case path == "new/cancel" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
		case path == "new" && r.Method == http.MethodPost:
			handlePairCreate(w, r, cfg, tmpl)
		case strings.HasSuffix(path, "/edit") && r.Method == http.MethodGet:
			id := strings.TrimSuffix(path, "/edit")
			handlePairEditForm(w, r, cfg, tmpl, id)
		case strings.HasSuffix(path, "/cancel") && r.Method == http.MethodGet:
			id := strings.TrimSuffix(path, "/cancel")
			handlePairCancelEdit(w, r, cfg, tmpl, id)
		case r.Method == http.MethodPost:
			handlePairUpdate(w, r, cfg, tmpl, path)
		case r.Method == http.MethodDelete:
			handlePairDelete(w, r, cfg, path)
		default:
			http.NotFound(w, r)
		}
	}
}

func handlePairNewForm(w http.ResponseWriter, r *http.Request, cfg Config, tmpl *template.Template) {
	clients, err := cfg.Store.ListClients(r.Context())
	if err != nil {
		slog.ErrorContext(r.Context(), "list clients for new pair form", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	csrf := httpsrv.CSRFTokenFromContext(r.Context())
	data := pairEditData{
		Action:    "new",
		Pair:      PairView{CSRFToken: csrf},
		Clients:   clients,
		CSRFToken: csrf,
	}
	render(w, tmpl, "pair_edit_form", data)
}

func handlePairCreate(w http.ResponseWriter, r *http.Request, cfg Config, tmpl *template.Template) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	p := store.SyncPair{
		ID:        uuid.NewString(),
		Name:      r.PostFormValue("name"),
		ClientAID: r.PostFormValue("client_a_id"),
		PathA:     r.PostFormValue("path_a"),
		ClientBID: r.PostFormValue("client_b_id"),
		PathB:     r.PostFormValue("path_b"),
		Direction: r.PostFormValue("direction"),
		Enabled:   r.PostFormValue("enabled") == "1",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := cfg.Store.UpsertPair(r.Context(), p); err != nil {
		slog.ErrorContext(r.Context(), "upsert pair on create", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	auditPairAction(r.Context(), cfg.Store, "pair_create", p.ID)
	publishPairUpdate(r.Context(), cfg.Publisher, p.ID)

	pairs, clients, err := loadPairsAndClients(r.Context(), cfg.Store)
	if err != nil {
		slog.ErrorContext(r.Context(), "reload pairs after create", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	csrf := httpsrv.CSRFTokenFromContext(r.Context())
	clientsByID := clientsMap(clients)
	views := make([]PairView, len(pairs))
	for i, ep := range pairs {
		views[i] = toPairView(ep, clientsByID, csrf)
	}

	render(w, tmpl, "pairs_content", map[string]any{
		"Pairs": views,
	})
}

func handlePairEditForm(w http.ResponseWriter, r *http.Request, cfg Config, tmpl *template.Template, id string) {
	pair, err := cfg.Store.GetPair(r.Context(), id)
	if err != nil || pair == nil {
		http.NotFound(w, r)
		return
	}

	clients, err := cfg.Store.ListClients(r.Context())
	if err != nil {
		slog.ErrorContext(r.Context(), "list clients for edit form", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	csrf := httpsrv.CSRFTokenFromContext(r.Context())
	clientsByID := clientsMap(clients)
	data := pairEditData{
		Action:    id,
		Pair:      toPairView(*pair, clientsByID, csrf),
		Clients:   clients,
		CSRFToken: csrf,
	}
	render(w, tmpl, "pair_edit_form", data)
}

func handlePairCancelEdit(w http.ResponseWriter, r *http.Request, cfg Config, tmpl *template.Template, id string) {
	pair, err := cfg.Store.GetPair(r.Context(), id)
	if err != nil || pair == nil {
		http.NotFound(w, r)
		return
	}

	clients, err := cfg.Store.ListClients(r.Context())
	if err != nil {
		slog.ErrorContext(r.Context(), "list clients for cancel edit", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	csrf := httpsrv.CSRFTokenFromContext(r.Context())
	clientsByID := clientsMap(clients)
	view := toPairView(*pair, clientsByID, csrf)
	render(w, tmpl, "pair_row", view)
}

func handlePairUpdate(w http.ResponseWriter, r *http.Request, cfg Config, tmpl *template.Template, id string) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	existing, err := cfg.Store.GetPair(r.Context(), id)
	if err != nil || existing == nil {
		http.NotFound(w, r)
		return
	}

	etagStr := r.PostFormValue("etag")
	etag, err := strconv.ParseInt(etagStr, 10, 64)
	if err != nil || etag != existing.Etag {
		http.Error(w, "precondition failed: etag mismatch", http.StatusPreconditionFailed)
		return
	}

	wasEnabled := existing.Enabled
	p := store.SyncPair{
		ID:        id,
		Name:      r.PostFormValue("name"),
		ClientAID: r.PostFormValue("client_a_id"),
		PathA:     r.PostFormValue("path_a"),
		ClientBID: r.PostFormValue("client_b_id"),
		PathB:     r.PostFormValue("path_b"),
		Direction: r.PostFormValue("direction"),
		Enabled:   r.PostFormValue("enabled") == "1",
		CreatedAt: existing.CreatedAt,
		UpdatedAt: time.Now(),
	}

	if err := cfg.Store.UpsertPair(r.Context(), p); err != nil {
		slog.ErrorContext(r.Context(), "upsert pair on update", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	auditPairAction(r.Context(), cfg.Store, "pair_update", p.ID)

	if p.Enabled {
		// TODO(v0.2.7): enabled が false→true に切り替わった場合は ListFilesRequest も配信する
		publishPairUpdate(r.Context(), cfg.Publisher, p.ID)
	} else if wasEnabled {
		publishPairUnsubscribe(r.Context(), cfg.Publisher, p.ID)
	}

	updated, err := cfg.Store.GetPair(r.Context(), id)
	if err != nil || updated == nil {
		slog.ErrorContext(r.Context(), "get updated pair", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	clients, err := cfg.Store.ListClients(r.Context())
	if err != nil {
		slog.ErrorContext(r.Context(), "list clients after update", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	csrf := httpsrv.CSRFTokenFromContext(r.Context())
	clientsByID := clientsMap(clients)
	view := toPairView(*updated, clientsByID, csrf)
	render(w, tmpl, "pair_row", view)
}

func handlePairDelete(w http.ResponseWriter, r *http.Request, cfg Config, id string) {
	if err := cfg.Store.DeletePair(r.Context(), id); err != nil {
		slog.ErrorContext(r.Context(), "delete pair", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	auditPairAction(r.Context(), cfg.Store, "pair_delete", id)
	publishPairUnsubscribe(r.Context(), cfg.Publisher, id)

	w.WriteHeader(http.StatusOK)
}

// auditPairAction は pair 操作を audit_log に記録する。
func auditPairAction(ctx context.Context, s store.Store, kind, pairID string) {
	d := fmt.Sprintf(`{"pair_id":%q}`, pairID)
	if err := s.AppendAuditLog(ctx, store.AuditEntry{
		TS:     time.Now(),
		Kind:   kind,
		PairID: &pairID,
		Detail: &d,
	}); err != nil {
		slog.ErrorContext(ctx, "append audit log", "kind", kind, "pair_id", pairID, "error", err)
	}
}

func publishPairUpdate(ctx context.Context, pub PairPublisher, pairID string) {
	if pub == nil {
		return
	}
	if err := pub.PublishPairUpdate(ctx, pairID); err != nil {
		slog.WarnContext(ctx, "publish pair update", "pair_id", pairID, "error", err)
	}
}

func publishPairUnsubscribe(ctx context.Context, pub PairPublisher, pairID string) {
	if pub == nil {
		return
	}
	if err := pub.PublishPairUnsubscribe(ctx, pairID); err != nil {
		slog.WarnContext(ctx, "publish pair unsubscribe", "pair_id", pairID, "error", err)
	}
}

func loadPairsAndClients(ctx context.Context, s store.Store) ([]store.SyncPair, []store.Client, error) {
	pairs, err := s.ListPairs(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("list pairs: %w", err)
	}
	clients, err := s.ListClients(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("list clients: %w", err)
	}
	return pairs, clients, nil
}

func clientsMap(clients []store.Client) map[string]store.Client {
	m := make(map[string]store.Client, len(clients))
	for _, c := range clients {
		m[c.ID] = c
	}
	return m
}
