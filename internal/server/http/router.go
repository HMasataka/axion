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
}

// NewRouter は HTTP ServeMux を構築する。
func NewRouter(cfg Config) http.Handler {
	mux := http.NewServeMux()

	wsHandler := NewWSHandler(cfg.Store, cfg.Hub)
	mux.Handle("/v1/ws", AccessLog(AuthBearer(cfg.Store, cfg.PSK, wsHandler)))

	if cfg.BlobStore != nil {
		blobsHandler := NewBlobsHandler(cfg.BlobStore, cfg.Store, cfg.MaxFileSizeBytes, cfg.PerClientQuotaBytes)
		mux.Handle("/v1/blobs/", AccessLog(AuthBearerWithClientID(cfg.Store, cfg.PSK, blobsHandler)))
	}

	return mux
}
