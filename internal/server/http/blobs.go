package httpsrv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/HMasataka/axion/internal/server/blobstore"
	"github.com/HMasataka/axion/internal/server/store"
)

// BlobsHandler は /v1/blobs/{sha} の HEAD/PUT/GET/Range を処理する。
type BlobsHandler struct {
	bs             blobstore.BlobStore
	store          store.Store
	maxFileSizeBytes int64
	perClientQuota int64
}

func NewBlobsHandler(bs blobstore.BlobStore, s store.Store, maxFileSize, perClientQuota int64) *BlobsHandler {
	return &BlobsHandler{
		bs:               bs,
		store:            s,
		maxFileSizeBytes: maxFileSize,
		perClientQuota:   perClientQuota,
	}
}

func (h *BlobsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sha := strings.TrimPrefix(r.URL.Path, "/v1/blobs/")
	if sha == "" {
		writeError(w, http.StatusBadRequest, errors.New("sha required"))
		return
	}

	switch r.Method {
	case http.MethodHead:
		h.handleHEAD(w, r, sha)
	case http.MethodPut:
		h.handlePUT(w, r, sha)
	case http.MethodGet:
		h.handleGET(w, r, sha)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (h *BlobsHandler) handleHEAD(w http.ResponseWriter, r *http.Request, sha string) {
	exists, err := h.bs.Has(r.Context(), sha)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if !exists {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	size, _, _ := h.bs.Stat(r.Context(), sha)
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.WriteHeader(http.StatusOK)
}

func (h *BlobsHandler) handlePUT(w http.ResponseWriter, r *http.Request, sha string) {
	clientID := ClientIDFromContext(r.Context())
	contentLength := r.ContentLength
	if contentLength <= 0 {
		writeError(w, http.StatusLengthRequired, errors.New("Content-Length required"))
		return
	}
	if contentLength > h.maxFileSizeBytes {
		writeError(w, http.StatusRequestEntityTooLarge,
			fmt.Errorf("file too large: size=%d limit=%d", contentLength, h.maxFileSizeBytes))
		return
	}

	used, err := h.computeClientUsage(r.Context(), clientID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if used+contentLength > h.perClientQuota {
		auditQuotaExceeded(r.Context(), h.store, clientID, sha, used, contentLength, h.perClientQuota)
		writeError(w, http.StatusInsufficientStorage,
			fmt.Errorf("quota exceeded: used=%d limit=%d", used, h.perClientQuota))
		return
	}

	if cr := r.Header.Get("Content-Range"); cr != "" {
		offset, totalSize, err := parseContentRange(cr)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := h.bs.PutRange(r.Context(), sha, offset, r.Body, totalSize); err != nil {
			h.handlePutError(w, r.Context(), err, clientID, sha)
			return
		}
	} else {
		if err := h.bs.Put(r.Context(), sha, r.Body, contentLength); err != nil {
			h.handlePutError(w, r.Context(), err, clientID, sha)
			return
		}
	}
	w.WriteHeader(http.StatusCreated)
}

func (h *BlobsHandler) handleGET(w http.ResponseWriter, r *http.Request, sha string) {
	rangeHeader := r.Header.Get("Range")
	if rangeHeader == "" {
		rc, err := h.bs.Get(r.Context(), sha)
		if errors.Is(err, blobstore.ErrNotFound) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		defer rc.Close()
		size, _, _ := h.bs.Stat(r.Context(), sha)
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		io.Copy(w, rc) //nolint:errcheck
		return
	}

	offset, length, err := parseRangeHeader(rangeHeader)
	if err != nil {
		writeError(w, http.StatusRequestedRangeNotSatisfiable, err)
		return
	}
	rc, err := h.bs.GetRange(r.Context(), sha, offset, length)
	if errors.Is(err, blobstore.ErrNotFound) {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer rc.Close()

	size, _, _ := h.bs.Stat(r.Context(), sha)
	end := offset + length - 1
	if length < 0 {
		end = size - 1
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", offset, end, size))
	if length >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
	}
	w.WriteHeader(http.StatusPartialContent)
	io.Copy(w, rc) //nolint:errcheck
}

func (h *BlobsHandler) handlePutError(w http.ResponseWriter, ctx context.Context, err error, clientID, sha string) {
	switch {
	case errors.Is(err, blobstore.ErrSHAMismatch), errors.Is(err, blobstore.ErrSizeMismatch):
		writeError(w, http.StatusUnprocessableEntity, err)
	case errors.Is(err, blobstore.ErrIncomplete):
		writeError(w, http.StatusPartialContent, err)
	case isDiskFullError(err):
		auditDiskFull(ctx, h.store, clientID, sha)
		writeError(w, http.StatusInsufficientStorage, err)
	default:
		writeError(w, http.StatusInternalServerError, err)
	}
}

func (h *BlobsHandler) computeClientUsage(ctx context.Context, clientID string) (int64, error) {
	pairs, err := h.store.ListPairsForClient(ctx, clientID)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, p := range pairs {
		side := "a"
		if p.ClientBID == clientID {
			side = "b"
		}
		states, err := h.store.ListFileStates(ctx, p.ID, side)
		if err != nil {
			return 0, err
		}
		for _, s := range states {
			if s.Size != nil {
				total += *s.Size
			}
		}
	}
	return total, nil
}

func isDiskFullError(err error) bool {
	var e *os.PathError
	if errors.As(err, &e) {
		return e.Err == syscall.ENOSPC
	}
	return false
}

func auditQuotaExceeded(ctx context.Context, s store.Store, clientID, sha string, used, requested, limit int64) {
	detail, _ := json.Marshal(map[string]any{
		"used":      used,
		"requested": requested,
		"limit":     limit,
		"sha":       sha,
	})
	d := string(detail)
	_ = s.AppendAuditLog(ctx, store.AuditEntry{
		TS:       time.Now(),
		Kind:     "quota_exceeded",
		ClientID: &clientID,
		Detail:   &d,
	})
}

func auditDiskFull(ctx context.Context, s store.Store, clientID, sha string) {
	d := fmt.Sprintf(`{"sha":%q}`, sha)
	_ = s.AppendAuditLog(ctx, store.AuditEntry{
		TS:       time.Now(),
		Kind:     "disk_full",
		ClientID: &clientID,
		Detail:   &d,
	})
}

// parseRangeHeader は "bytes=10-49" や "bytes=10-" をパースする。
// open end (length=-1) の場合は length=-1 を返す。
func parseRangeHeader(header string) (offset, length int64, err error) {
	if !strings.HasPrefix(header, "bytes=") {
		return 0, 0, fmt.Errorf("unsupported range unit: %q", header)
	}
	spec := strings.TrimPrefix(header, "bytes=")
	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid range: %q", header)
	}
	offset, err = strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid range offset: %w", err)
	}
	if parts[1] == "" {
		return offset, -1, nil
	}
	end, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid range end: %w", err)
	}
	return offset, end - offset + 1, nil
}

// parseContentRange は "bytes 0-1048575/52428800" をパースして offset と totalSize を返す。
func parseContentRange(header string) (offset, totalSize int64, err error) {
	if !strings.HasPrefix(header, "bytes ") {
		return 0, 0, fmt.Errorf("unsupported content-range unit: %q", header)
	}
	spec := strings.TrimPrefix(header, "bytes ")
	slashIdx := strings.Index(spec, "/")
	if slashIdx < 0 {
		return 0, 0, fmt.Errorf("invalid content-range: %q", header)
	}
	rangePart := spec[:slashIdx]
	totalPart := spec[slashIdx+1:]

	parts := strings.SplitN(rangePart, "-", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid content-range range: %q", header)
	}
	offset, err = strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid content-range offset: %w", err)
	}
	totalSize, err = strconv.ParseInt(totalPart, 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid content-range total: %w", err)
	}
	return offset, totalSize, nil
}

// writeError は JSON エラーレスポンスを書く。
func writeError(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()}) //nolint:errcheck
}
