package web

import (
	"context"
	"html/template"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/HMasataka/axion/internal/proto"
	httpsrv "github.com/HMasataka/axion/internal/server/http"
)

// HubSender はクライアントへの送受信機能を抽象化する。
type HubSender interface {
	IsOnline(clientID string) bool
	SendAndWait(ctx context.Context, clientID string, env proto.Envelope, timeout time.Duration) (proto.Envelope, error)
}

// browseData はテンプレートに渡すデータ。
type browseData struct {
	Title       string
	ClientID    string
	ClientLabel string
	CurrentPath string
	Parent      string
	Entries     []proto.DirEntry
	Error       string
	CSRFToken   string
}

func browseHandler(cfg Config, tmpl *template.Template) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clientID, ok := parseBrowsePath(r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}

		c, err := cfg.Store.GetClient(r.Context(), clientID)
		if err != nil || c == nil {
			http.NotFound(w, r)
			return
		}

		curPath := cleanBrowsePath(r.URL.Query().Get("path"))
		isHTMX := r.Header.Get("HX-Request") == "true"

		data := browseData{
			Title:       "Browse: " + c.DisplayName + " " + c.RootPath,
			ClientID:    clientID,
			ClientLabel: clientLabel(c.DisplayName, c.Hostname, c.RootPath),
			CurrentPath: curPath,
			Parent:      parentPath(curPath),
			CSRFToken:   httpsrv.CSRFTokenFromContext(r.Context()),
		}

		if cfg.Hub == nil || !cfg.Hub.IsOnline(clientID) {
			data.Error = "client offline"
			renderBrowse(w, tmpl, data, isHTMX)
			return
		}

		payload, err := proto.MarshalPayload(proto.ListDirRequest{RelPath: curPath})
		if err != nil {
			data.Error = err.Error()
			renderBrowse(w, tmpl, data, isHTMX)
			return
		}
		env := proto.Envelope{
			Type:          proto.TypeListDirRequest,
			CorrelationID: uuid.NewString(),
			Payload:       payload,
		}

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		respEnv, err := cfg.Hub.SendAndWait(ctx, clientID, env, 5*time.Second)
		if err != nil {
			data.Error = "client did not respond: " + err.Error()
			renderBrowse(w, tmpl, data, isHTMX)
			return
		}

		var resp proto.ListDirResponse
		if err := proto.UnmarshalPayload(respEnv.Payload, &resp); err != nil {
			data.Error = "bad response: " + err.Error()
			renderBrowse(w, tmpl, data, isHTMX)
			return
		}
		if resp.Error != "" {
			data.Error = resp.Error
		}
		data.Entries = resp.Entries
		renderBrowse(w, tmpl, data, isHTMX)
	}
}

func renderBrowse(w http.ResponseWriter, tmpl *template.Template, data browseData, isHTMX bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if isHTMX {
		if err := tmpl.ExecuteTemplate(w, "browse_listing", data); err != nil {
			http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}
	if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
	}
}

// parseBrowsePath は /clients/{id}/browse から clientID を取り出す。
func parseBrowsePath(urlPath string) (clientID string, ok bool) {
	p := strings.TrimPrefix(urlPath, "/clients/")
	parts := strings.SplitN(p, "/", 2)
	if len(parts) != 2 || parts[1] != "browse" {
		return "", false
	}
	return parts[0], true
}

// cleanBrowsePath は path パラメータを正規化する。空の場合は "." を返す。
func cleanBrowsePath(raw string) string {
	if raw == "" {
		return "."
	}
	cleaned := path.Clean(raw)
	if cleaned == "" {
		return "."
	}
	return cleaned
}

func clientLabel(displayName, hostname, rootPath string) string {
	name := displayName
	if name == "" {
		name = hostname
	}
	return name + " (" + rootPath + ")"
}

func parentPath(p string) string {
	if p == "." || p == "/" {
		return "."
	}
	d := path.Dir(p)
	if d == "" {
		return "."
	}
	return d
}
