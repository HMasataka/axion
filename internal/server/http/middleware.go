package httpsrv

import (
	"bufio"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/HMasataka/axion/internal/proto"
	"github.com/HMasataka/axion/internal/server/store"
)

// responseWriter は http.ResponseWriter をラップしてステータスコードを記録する。
// WebSocket upgrade のために http.Hijacker も委譲する。
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// Hijack は WebSocket upgrade で必要な http.Hijacker を委譲する。
func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return rw.ResponseWriter.(http.Hijacker).Hijack()
}

// AuthBearer は Authorization: Bearer <psk> を検証する。
// 不一致は 401 + audit_log.kind = "psk_auth_failed" を Store に記録する。
func AuthBearer(s store.Store, psk string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := extractBearer(r.Header.Get("Authorization"))
		if token != psk {
			_ = s.AppendAuditLog(r.Context(), store.AuditEntry{
				TS:   time.Now(),
				Kind: "psk_auth_failed",
			})
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func extractBearer(header string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	return strings.TrimPrefix(header, prefix)
}

// AccessLog は slog でリクエストの method/path/status/duration を出力する。
func AccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		slog.InfoContext(r.Context(), "http access",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", rw.status),
			slog.Duration("duration", time.Since(start)),
		)
	})
}

// ProtoVersionHeader は HTTP リクエストの "Axion-Proto-Version" ヘッダを検証する。
// メジャー一致で OK、不一致で 426 Upgrade Required + audit_log.kind = "proto_version_mismatch"。
func ProtoVersionHeader(s store.Store, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientVersion := r.Header.Get("Axion-Proto-Version")
		if !protoVersionCompatible(clientVersion, proto.ProtoVersion) {
			_ = s.AppendAuditLog(r.Context(), store.AuditEntry{
				TS:   time.Now(),
				Kind: "proto_version_mismatch",
			})
			http.Error(w, "proto version mismatch", http.StatusUpgradeRequired)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// protoVersionCompatible はクライアントとサーバのメジャーバージョンの一致を確認する。
func protoVersionCompatible(client, server string) bool {
	cmajor := strings.SplitN(client, ".", 2)[0]
	smajor := strings.SplitN(server, ".", 2)[0]
	return cmajor == smajor && cmajor != ""
}
