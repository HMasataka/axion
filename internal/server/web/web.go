package web

import (
	"embed"
	"errors"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	httpsrv "github.com/HMasataka/axion/internal/server/http"
	"github.com/HMasataka/axion/internal/server/store"
)

// Server は Web UI Handler とその依存を保持する。
// app.go から循環参照なしに NotifyClientChange を呼ぶためのエントリポイント。
type Server struct {
	cfg       Config
	templates map[string]*template.Template
}

// NewServer は Server を初期化する。
func NewServer(cfg Config) *Server {
	return &Server{cfg: cfg, templates: mustParseTemplates()}
}

// Handler は Web UI HTTP Handler を構築する。
func (s *Server) Handler() http.Handler {
	return buildHandler(s.cfg, s.templates)
}

// NotifyClientChange は client の変化を全 WS subscriber に push する。
func (s *Server) NotifyClientChange(c store.Client) {
	if s.cfg.Broadcaster == nil {
		return
	}
	s.cfg.Broadcaster.PublishClientStatus(s.templates["clients"], toClientView(c))
}

//go:embed assets/static assets/templates
var assets embed.FS

// Config は Web UI Handler の設定。
type Config struct {
	Store       store.Store
	Publisher   PairPublisher // nil の場合 PublishPairUpdate などは skip
	Hub         HubSender    // browse handler で使用。nil の場合 browse 機能は無効
	Broadcaster *Broadcaster // nil の場合 /admin/ws は登録されない
}

// ClientView は表示用に変換された Client。
type ClientView struct {
	store.Client
	LastSeenString string
}

// Handler は Web UI HTTP Handler を構築する。
//   - GET /                               → クライアント一覧
//   - GET /clients/{id}/edit              → クライアント編集フォーム (HTMX partial)
//   - POST /clients/{id}/display-name     → display name 更新 (HTMX partial)
//   - GET /clients/{id}/cancel            → 編集キャンセル (HTMX partial)
//   - GET /clients/{id}/browse?path=<p>   → ディレクトリブラウザ
//   - GET /pairs                          → ペア一覧
//   - GET /settings                       → 設定
//   - GET /static/*                       → 埋め込み静的アセット
//   - GET /admin/ws                       → WebSocket (Broadcaster が設定されている場合)
func Handler(cfg Config) http.Handler {
	return buildHandler(cfg, mustParseTemplates())
}

func buildHandler(cfg Config, templates map[string]*template.Template) http.Handler {
	mux := http.NewServeMux()

	staticFS, err := fs.Sub(assets, "assets/static")
	if err != nil {
		panic(err)
	}
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServerFS(staticFS)))

	mux.HandleFunc("/", clientsListHandler(cfg, templates["clients"]))
	mux.HandleFunc("/clients/", func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/clients/")
		parts := strings.SplitN(p, "/", 2)
		if len(parts) != 2 {
			http.NotFound(w, r)
			return
		}
		if parts[1] == "browse" {
			browseHandler(cfg, templates["browse"])(w, r)
			return
		}
		clientRowHandler(cfg, templates["clients"])(w, r)
	})
	mux.HandleFunc("/pairs", pairsListHandler(cfg, templates["pairs"]))
	mux.HandleFunc("/pairs/", pairFormHandler(cfg, templates["pairs"]))
	mux.HandleFunc("/settings", settingsListHandler(cfg, templates["settings"]))
	mux.HandleFunc("/settings/", settingRowHandler(cfg, templates["settings"]))
	mux.HandleFunc("/runs", runsListHandler(cfg, templates["runs"]))

	if cfg.Broadcaster != nil {
		mux.Handle("/admin/ws", adminWSHandler(cfg.Broadcaster))
	}

	return mux
}

// mustParseTemplates は各ページテンプレートを base.tmpl と組み合わせてパースする。
// 各エントリは "base" テンプレートを起点に実行できる独立したセット。
// clients セットには client_row / client_edit_row も含まれる。
func mustParseTemplates() map[string]*template.Template {
	pages := []string{"clients", "pairs", "settings", "browse", "runs"}
	result := make(map[string]*template.Template, len(pages))

	for _, name := range pages {
		tmpl, err := template.ParseFS(
			assets,
			"assets/templates/base.tmpl",
			"assets/templates/"+name+".tmpl",
		)
		if err != nil {
			panic(err)
		}
		result[name] = tmpl
	}

	return result
}

func clientsListHandler(cfg Config, tmpl *template.Template) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		clients, err := cfg.Store.ListClients(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		viewClients := make([]ClientView, 0, len(clients))
		for _, c := range clients {
			viewClients = append(viewClients, toClientView(c))
		}
		renderPage(w, tmpl, map[string]any{
			"Title":     "Clients",
			"Clients":   viewClients,
			"CSRFToken": httpsrv.CSRFTokenFromContext(r.Context()),
		})
	}
}

func clientRowHandler(cfg Config, tmpl *template.Template) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/clients/")
		parts := strings.SplitN(path, "/", 2)
		if len(parts) != 2 {
			http.NotFound(w, r)
			return
		}
		id, action := parts[0], parts[1]

		switch action {
		case "edit":
			handleClientEdit(cfg, tmpl, w, r, id)
		case "cancel":
			handleClientCancel(cfg, tmpl, w, r, id)
		case "display-name":
			handleClientDisplayName(cfg, tmpl, w, r, id)
		default:
			http.NotFound(w, r)
		}
	}
}

func handleClientEdit(cfg Config, tmpl *template.Template, w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	c, err := cfg.Store.GetClient(r.Context(), id)
	if err != nil || c == nil {
		http.NotFound(w, r)
		return
	}
	render(w, tmpl, "client_edit_row", map[string]any{
		"Client":    toClientView(*c),
		"CSRFToken": httpsrv.CSRFTokenFromContext(r.Context()),
	})
}

func handleClientCancel(cfg Config, tmpl *template.Template, w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	c, err := cfg.Store.GetClient(r.Context(), id)
	if err != nil || c == nil {
		http.NotFound(w, r)
		return
	}
	render(w, tmpl, "client_row", toClientView(*c))
}

func handleClientDisplayName(cfg Config, tmpl *template.Template, w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	etagStr := r.PostFormValue("etag")
	etag, err := strconv.ParseInt(etagStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid etag", http.StatusBadRequest)
		return
	}
	displayName := r.PostFormValue("display_name")
	_, err = cfg.Store.UpdateClientDisplayName(r.Context(), id, displayName, etag)
	if errors.Is(err, store.ErrEtagMismatch) {
		w.WriteHeader(http.StatusPreconditionFailed)
		_, _ = w.Write([]byte(`<td colspan="6">Edit conflict: client was modified by another user. <a href="/">Refresh</a></td>`))
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	c, err := cfg.Store.GetClient(r.Context(), id)
	if err != nil || c == nil {
		http.Error(w, "client not found after update", http.StatusInternalServerError)
		return
	}
	render(w, tmpl, "client_row", toClientView(*c))
}

// toClientView は store.Client を表示用の ClientView に変換する。
func toClientView(c store.Client) ClientView {
	lastSeen := "never"
	if !c.LastSeen.IsZero() && c.LastSeen.Year() > 1970 {
		lastSeen = c.LastSeen.Format("2006-01-02 15:04:05")
	}
	return ClientView{Client: c, LastSeenString: lastSeen}
}

// render はテンプレートの名前付きブロックを直接実行する (partial用)。
func render(w http.ResponseWriter, tmpl *template.Template, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, name, data); err != nil {
		slog.Error("render template", "name", name, "error", err)
	}
}

// renderPage は "base" テンプレートを起点にフルページをレンダリングする。
func renderPage(w http.ResponseWriter, tmpl *template.Template, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
		slog.Error("render page", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}
