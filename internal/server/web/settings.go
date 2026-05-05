package web

import (
	"html/template"
	"net/http"
	"sort"
	"strings"

	httpsrv "github.com/HMasataka/axion/internal/server/http"
)

// SettingView は設定の表示用データ。
type SettingView struct {
	Key       string
	Value     string
	CSRFToken string
}

func settingsListHandler(cfg Config, tmpl *template.Template) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/settings" {
			http.NotFound(w, r)
			return
		}
		all, err := cfg.Store.LoadAllSettings(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		csrf := httpsrv.CSRFTokenFromContext(r.Context())
		settings := make([]SettingView, 0, len(all))
		for k, v := range all {
			settings = append(settings, SettingView{Key: k, Value: v, CSRFToken: csrf})
		}
		sort.Slice(settings, func(i, j int) bool { return settings[i].Key < settings[j].Key })
		renderPage(w, tmpl, map[string]any{
			"Title":    "Settings",
			"Settings": settings,
		})
	}
}

func settingRowHandler(cfg Config, tmpl *template.Template) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/settings/")
		csrf := httpsrv.CSRFTokenFromContext(r.Context())

		if r.Method == http.MethodPost {
			key := strings.TrimSuffix(path, "/")
			value := r.PostFormValue("value")
			if err := cfg.Store.SetSetting(r.Context(), key, value); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			render(w, tmpl, "setting_row", SettingView{Key: key, Value: value, CSRFToken: csrf})
			return
		}

		parts := strings.SplitN(path, "/", 2)
		if len(parts) != 2 {
			http.NotFound(w, r)
			return
		}
		key, action := parts[0], parts[1]

		v, err := cfg.Store.GetSetting(r.Context(), key)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		switch action {
		case "edit":
			render(w, tmpl, "setting_edit_row", SettingView{Key: key, Value: v, CSRFToken: csrf})
		case "cancel":
			render(w, tmpl, "setting_row", SettingView{Key: key, Value: v, CSRFToken: csrf})
		default:
			http.NotFound(w, r)
		}
	}
}
