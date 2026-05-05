package httpsrv

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/HMasataka/axion/internal/proto"
	"github.com/HMasataka/axion/internal/server/hub"
	"github.com/HMasataka/axion/internal/server/store"
)

// ListDirHandler は GET /v1/clients/{id}/listdir?path=... を処理する。
// Hub 経由で対象クライアントへ ListDirRequest を WS push、correlation_id で応答待ち (5s)、
// 結果を JSON で返す。
type ListDirHandler struct {
	hub   *hub.Hub
	store store.Store
}

func NewListDirHandler(h *hub.Hub, s store.Store) *ListDirHandler {
	return &ListDirHandler{hub: h, store: s}
}

func (h *ListDirHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clientID, ok := h.parseClientID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}

	relPath := r.URL.Query().Get("path")
	if relPath == "" {
		relPath = "."
	}

	if !h.hub.IsOnline(clientID) {
		http.Error(w, "client offline", http.StatusServiceUnavailable)
		return
	}

	respEnv, err := h.sendListDirRequest(r.Context(), clientID, relPath)
	if err != nil {
		if errors.Is(err, hub.ErrTimeout) {
			http.Error(w, "client did not respond", http.StatusGatewayTimeout)
		} else {
			http.Error(w, err.Error(), http.StatusBadGateway)
		}
		return
	}

	var resp proto.ListDirResponse
	if err := proto.UnmarshalPayload(respEnv.Payload, &resp); err != nil {
		http.Error(w, "invalid client response: "+err.Error(), http.StatusBadGateway)
		return
	}

	if resp.Error != "" {
		h.handleClientError(r.Context(), w, clientID, resp.Error)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// parseClientID は /v1/clients/{id}/listdir からクライアント ID を抽出する。
func (h *ListDirHandler) parseClientID(r *http.Request) (string, bool) {
	path := strings.TrimPrefix(r.URL.Path, "/v1/clients/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 || parts[1] != "listdir" {
		return "", false
	}
	return parts[0], true
}

// sendListDirRequest は Hub 経由でクライアントに ListDirRequest を送り応答を待つ。
func (h *ListDirHandler) sendListDirRequest(ctx context.Context, clientID, relPath string) (proto.Envelope, error) {
	req := proto.ListDirRequest{RelPath: relPath}
	payload, err := proto.MarshalPayload(req)
	if err != nil {
		return proto.Envelope{}, err
	}
	corrID := uuid.NewString()
	env := proto.Envelope{
		Type:          proto.TypeListDirRequest,
		CorrelationID: corrID,
		Payload:       payload,
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return h.hub.SendAndWait(ctx, clientID, env, 5*time.Second)
}

// handleClientError はクライアント側エラーを HTTP レスポンスに変換し、path escape の場合は audit_log に記録する。
func (h *ListDirHandler) handleClientError(ctx context.Context, w http.ResponseWriter, clientID, errMsg string) {
	if strings.HasPrefix(errMsg, "path escape:") {
		_ = h.store.AppendAuditLog(ctx, store.AuditEntry{
			TS:       time.Now(),
			Kind:     "path_escape_blocked",
			ClientID: &clientID,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(map[string]string{"error": errMsg})
}
