package httpsrv

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/HMasataka/axion/internal/proto"
	"github.com/HMasataka/axion/internal/server/store"
)

const csrfCookieName = "axion_csrf"
const csrfHeaderName = "X-CSRF-Token"
const csrfFormField = "_csrf"
const csrfTokenLen = 32 // bytes

type ctxKey int

const ctxKeyClientID ctxKey = iota

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

// AuthBearerWithClientID は Bearer PSK 検証に加えて、X-Axion-Client-ID ヘッダから
// クライアント ID を取り出して context に詰める。
func AuthBearerWithClientID(s store.Store, psk string, next http.Handler) http.Handler {
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
		clientID := r.Header.Get("X-Axion-Client-ID")
		ctx := context.WithValue(r.Context(), ctxKeyClientID, clientID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ClientIDFromContext は context から ClientID を取り出す。
func ClientIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyClientID).(string)
	return v
}

// BasicAuth は HTTP Basic 認証で Web UI を保護する。
// adminUser/adminPassword が空の場合は middleware を bypass する。
// これは開発環境での認証なし起動を明示的に許容する仕様であり、フォールバックではない。
// 認証失敗で WWW-Authenticate ヘッダ + 401。
func BasicAuth(adminUser, adminPassword string, next http.Handler) http.Handler {
	if adminUser == "" || adminPassword == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="axion admin"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		userOK := subtle.ConstantTimeCompare([]byte(user), []byte(adminUser)) == 1
		passOK := subtle.ConstantTimeCompare([]byte(pass), []byte(adminPassword)) == 1
		if !userOK || !passOK {
			w.Header().Set("WWW-Authenticate", `Basic realm="axion admin"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type csrfCtxKey struct{}

// CSRF は double-submit cookie 方式で CSRF を防御する。
// GET/HEAD/OPTIONS は通過。POST/PUT/PATCH/DELETE は X-CSRF-Token ヘッダか
// _csrf フォーム値が cookie と一致するか検証する。不一致は 403 Forbidden。
func CSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := readOrIssueToken(w, r)
		ctx := context.WithValue(r.Context(), csrfCtxKey{}, token)
		r = r.WithContext(ctx)

		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}

		submitted := r.Header.Get(csrfHeaderName)
		if submitted == "" {
			if err := r.ParseForm(); err == nil {
				submitted = r.PostFormValue(csrfFormField)
			}
		}
		if submitted == "" || subtle.ConstantTimeCompare([]byte(submitted), []byte(token)) != 1 {
			http.Error(w, "CSRF token mismatch", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// readOrIssueToken は cookie から CSRF トークンを取り出す。無ければ生成して set する。
func readOrIssueToken(w http.ResponseWriter, r *http.Request) string {
	if c, err := r.Cookie(csrfCookieName); err == nil && c.Value != "" {
		return c.Value
	}
	buf := make([]byte, csrfTokenLen)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand の失敗は OS レベルの致命的障害であり、継続不可能。
		panic("csrf: rand.Read failed: " + err.Error())
	}
	tok := hex.EncodeToString(buf)
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    tok,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(24 * time.Hour / time.Second),
	})
	return tok
}

// CSRFTokenFromContext は現在のリクエストの CSRF トークンを返す。
// テンプレートで {{.CSRFToken}} として埋め込むために context に詰める。
func CSRFTokenFromContext(ctx context.Context) string {
	v, _ := ctx.Value(csrfCtxKey{}).(string)
	return v
}
