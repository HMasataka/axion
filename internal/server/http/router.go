package httpsrv

import (
	"net/http"

	"github.com/HMasataka/axion/internal/server/hub"
	"github.com/HMasataka/axion/internal/server/store"
)

// Config は Router 構築時のオプション。
type Config struct {
	Store         store.Store
	Hub           *hub.Hub
	PSK           string
	AdminUser     string
	AdminPassword string
}

// NewRouter は HTTP ServeMux を構築する。
func NewRouter(cfg Config) http.Handler {
	mux := http.NewServeMux()

	wsHandler := NewWSHandler(cfg.Store, cfg.Hub)
	mux.Handle("/v1/ws", AccessLog(AuthBearer(cfg.Store, cfg.PSK, wsHandler)))

	return mux
}
