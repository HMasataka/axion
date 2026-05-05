package httpsrv

import (
	"net/http"

	"github.com/HMasataka/axion/internal/server/blobstore"
	"github.com/HMasataka/axion/internal/server/hub"
	"github.com/HMasataka/axion/internal/server/store"
)

// Config は Router 構築時のオプション。
type Config struct {
	Store               store.Store
	Hub                 *hub.Hub
	BlobStore           blobstore.BlobStore
	PSK                 string
	AdminUser           string
	AdminPassword       string
	MaxFileSizeBytes    int64
	PerClientQuotaBytes int64
	WebHandler          http.Handler // nil の場合 Web UI は無効
}

// NewRouter は HTTP ServeMux を構築する。
// /v1/ws, /v1/blobs/ は専用ハンドラが処理し、それ以外は WebHandler に委譲する。
// Go の ServeMux はより長いプレフィックスが優先されるため /v1/ が / より先に match する。
func NewRouter(cfg Config) http.Handler {
	mux := http.NewServeMux()

	wsHandler := NewWSHandler(cfg.Store, cfg.Hub)
	mux.Handle("/v1/ws", AccessLog(AuthBearer(cfg.Store, cfg.PSK, wsHandler)))

	if cfg.BlobStore != nil {
		blobsHandler := NewBlobsHandler(cfg.BlobStore, cfg.Store, cfg.MaxFileSizeBytes, cfg.PerClientQuotaBytes)
		mux.Handle("/v1/blobs/", AccessLog(AuthBearerWithClientID(cfg.Store, cfg.PSK, blobsHandler)))
	}

	if cfg.WebHandler != nil {
		webUI := AccessLog(BasicAuth(cfg.AdminUser, cfg.AdminPassword, CSRF(cfg.WebHandler)))
		mux.Handle("/", webUI)
	}

	return mux
}
