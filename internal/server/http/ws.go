package httpsrv

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"github.com/HMasataka/axion/internal/proto"
	"github.com/HMasataka/axion/internal/server/hub"
	"github.com/HMasataka/axion/internal/server/store"
	"github.com/HMasataka/axion/pkg/logctx"
)

var (
	errUnexpectedMessageType = errors.New("ws: expected text message")
	errUnexpectedMessageKind = errors.New("ws: expected register_request")
)

const registerTimeout = 5 * time.Second

// WSHandler は /v1/ws のハンドラ。
type WSHandler struct {
	store store.Store
	hub   *hub.Hub
}

func NewWSHandler(s store.Store, h *hub.Hub) *WSHandler {
	return &WSHandler{store: s, hub: h}
}

func (h *WSHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// テストとローカル環境でオリジン検証をスキップする。
		// プロダクションでは AllowedOrigins を設定する。
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}

	req, err := h.receiveRegisterRequest(r.Context(), ws)
	if err != nil {
		return
	}

	if !protoVersionCompatible(req.ProtoVersion, proto.ProtoVersion) {
		h.recordAudit(r.Context(), "proto_version_mismatch", &req.ClientID)
		ws.Close(4001, "proto version mismatch")
		return
	}

	ctx := logctx.With(r.Context(), logctx.ClientID(req.ClientID))

	now := time.Now()
	if err := h.store.UpsertClient(ctx, store.Client{
		ID:           req.ClientID,
		DisplayName:  req.Hostname,
		Hostname:     req.Hostname,
		RootPath:     req.RootPath,
		Version:      req.Version,
		ProtoVersion: req.ProtoVersion,
		Status:       "online",
		LastSeen:     now,
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		ws.Close(websocket.StatusInternalError, "store error")
		return
	}

	h.appendClientRegisterDetail(ctx, req)

	settings, _ := h.store.LoadAllSettings(ctx)
	if err := h.sendRegisterResponse(ctx, ws, settings); err != nil {
		return
	}

	conn := hub.NewConn(req.ClientID, ws, h.hub)
	h.hub.Register(conn)
	conn.Run(ctx)
}

func (h *WSHandler) receiveRegisterRequest(ctx context.Context, ws *websocket.Conn) (*proto.RegisterRequest, error) {
	readCtx, cancel := context.WithTimeout(ctx, registerTimeout)
	defer cancel()

	typ, data, err := ws.Read(readCtx)
	if err != nil {
		ws.Close(4002, "read error")
		return nil, err
	}

	if typ != websocket.MessageText {
		ws.Close(4003, "expected text message")
		return nil, errUnexpectedMessageType
	}

	env, err := proto.Unmarshal(data)
	if err != nil {
		ws.Close(4002, "malformed envelope")
		return nil, err
	}

	if env.Type != proto.TypeRegisterRequest {
		ws.Close(4004, "expected register_request")
		return nil, errUnexpectedMessageKind
	}

	var req proto.RegisterRequest
	if err := proto.UnmarshalPayload(env.Payload, &req); err != nil {
		ws.Close(4002, "malformed register_request")
		return nil, err
	}

	return &req, nil
}

func (h *WSHandler) sendRegisterResponse(ctx context.Context, ws *websocket.Conn, settings map[string]string) error {
	payload, err := proto.MarshalPayload(proto.RegisterResponse{
		OK:         true,
		ServerTime: time.Now().UnixNano(),
		Settings:   settings,
	})
	if err != nil {
		return err
	}

	data, err := proto.Marshal(proto.Envelope{
		Type:    proto.TypeRegisterResponse,
		Payload: payload,
	})
	if err != nil {
		return err
	}

	return ws.Write(ctx, websocket.MessageText, data)
}

func (h *WSHandler) recordAudit(ctx context.Context, kind string, clientID *string) {
	_ = h.store.AppendAuditLog(ctx, store.AuditEntry{
		TS:       time.Now(),
		Kind:     kind,
		ClientID: clientID,
	})
}

func (h *WSHandler) appendClientRegisterDetail(ctx context.Context, req *proto.RegisterRequest) {
	detail, _ := json.Marshal(map[string]any{
		"hostname":      req.Hostname,
		"version":       req.Version,
		"proto_version": req.ProtoVersion,
		"root_path":     req.RootPath,
	})
	detailStr := string(detail)
	_ = h.store.AppendAuditLog(ctx, store.AuditEntry{
		TS:       time.Now(),
		Kind:     "client_register",
		ClientID: &req.ClientID,
		Detail:   &detailStr,
	})
}
