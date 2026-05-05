package web

import (
	"html/template"
	"net/http"
	"strconv"

	"github.com/HMasataka/axion/internal/server/store"
)

const defaultRunsLimit = 100
const maxRunsLimit = 1000

// RunView は表示用に変換された SyncRun。
type RunView struct {
	ID               int64
	PairID           string
	PairName         string
	SrcClientID      string
	DstClientID      string
	RelPath          string
	SHA256Short      string
	Bytes            int64
	Status           string
	Error            string
	StartedAtString  string
	FinishedAtString string
	DurationMs       int64
}

type runsPageData struct {
	Title          string
	Runs           []RunView
	Pairs          []store.SyncPair
	SelectedPairID string
	SelectedStatus string
	Limit          int
}

func runsListHandler(cfg Config, tmpl *template.Template) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/runs" {
			http.NotFound(w, r)
			return
		}

		limit := parseRunsLimit(r.URL.Query().Get("limit"))
		pairFilter := r.URL.Query().Get("pair_id")
		statusFilter := r.URL.Query().Get("status")

		runs, err := cfg.Store.ListRecentSyncRuns(r.Context(), pairFilter, limit)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if statusFilter != "" {
			runs = filterRunsByStatus(runs, statusFilter)
		}

		pairs, err := cfg.Store.ListPairs(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		pairsByID := indexPairsByID(pairs)
		viewRuns := toRunViews(runs, pairsByID)

		renderPage(w, tmpl, runsPageData{
			Title:          "Sync Runs",
			Runs:           viewRuns,
			Pairs:          pairs,
			SelectedPairID: pairFilter,
			SelectedStatus: statusFilter,
			Limit:          limit,
		})
	}
}

func parseRunsLimit(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return defaultRunsLimit
	}
	if n > maxRunsLimit {
		return maxRunsLimit
	}
	return n
}

func filterRunsByStatus(runs []store.SyncRun, status string) []store.SyncRun {
	filtered := make([]store.SyncRun, 0, len(runs))
	for _, r := range runs {
		if r.Status == status {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

func indexPairsByID(pairs []store.SyncPair) map[string]store.SyncPair {
	m := make(map[string]store.SyncPair, len(pairs))
	for _, p := range pairs {
		m[p.ID] = p
	}
	return m
}

func toRunViews(runs []store.SyncRun, pairsByID map[string]store.SyncPair) []RunView {
	views := make([]RunView, 0, len(runs))
	for _, r := range runs {
		views = append(views, toRunView(r, pairsByID))
	}
	return views
}

func toRunView(r store.SyncRun, pairsByID map[string]store.SyncPair) RunView {
	v := RunView{
		ID:               r.ID,
		PairID:           r.PairID,
		SrcClientID:      r.SrcClientID,
		DstClientID:      r.DstClientID,
		RelPath:          r.RelPath,
		Bytes:            r.Bytes,
		Status:           r.Status,
		StartedAtString:  r.StartedAt.Format("2006-01-02 15:04:05"),
		FinishedAtString: r.FinishedAt.Format("2006-01-02 15:04:05"),
		DurationMs:       r.FinishedAt.Sub(r.StartedAt).Milliseconds(),
	}
	if p, ok := pairsByID[r.PairID]; ok {
		v.PairName = p.Name
	}
	if r.SHA256 != nil && len(*r.SHA256) >= 8 {
		v.SHA256Short = (*r.SHA256)[:8]
	}
	if r.Error != nil {
		v.Error = *r.Error
	}
	return v
}
