package httpsrv_test

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	httpsrv "github.com/HMasataka/axion/internal/server/http"
)

// nopHandler は常に 200 OK を返す。
var nopHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
})

// basicAuthHeader は user:pass を Base64 エンコードした Authorization ヘッダ値を返す。
func basicAuthHeader(user, pass string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
}

// TestBasicAuth_NoCredentials_401 は Authorization ヘッダなしで 401 + WWW-Authenticate を返すことを検証する。
func TestBasicAuth_NoCredentials_401(t *testing.T) {
	// Given
	h := httpsrv.BasicAuth("admin", "secret", nopHandler)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	// When
	h.ServeHTTP(w, r)

	// Then
	if w.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", w.Code)
	}
	if w.Header().Get("WWW-Authenticate") == "" {
		t.Error("want WWW-Authenticate header, got none")
	}
}

// TestBasicAuth_WrongCredentials_401 は不正な user/pass で 401 を返すことを検証する。
func TestBasicAuth_WrongCredentials_401(t *testing.T) {
	// Given
	h := httpsrv.BasicAuth("admin", "secret", nopHandler)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", basicAuthHeader("admin", "wrong"))
	w := httptest.NewRecorder()

	// When
	h.ServeHTTP(w, r)

	// Then
	if w.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", w.Code)
	}
}

// TestBasicAuth_CorrectCredentials_Pass は正しい user/pass で next が実行されることを検証する。
func TestBasicAuth_CorrectCredentials_Pass(t *testing.T) {
	// Given
	h := httpsrv.BasicAuth("admin", "secret", nopHandler)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", basicAuthHeader("admin", "secret"))
	w := httptest.NewRecorder()

	// When
	h.ServeHTTP(w, r)

	// Then
	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d", w.Code)
	}
}

// TestBasicAuth_EmptyConfig_Bypasses は adminUser="" のとき middleware を bypass することを検証する。
func TestBasicAuth_EmptyConfig_Bypasses(t *testing.T) {
	// Given
	h := httpsrv.BasicAuth("", "", nopHandler)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	// When
	h.ServeHTTP(w, r)

	// Then
	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d", w.Code)
	}
}

// TestCSRF_GET_Passes は GET リクエストが通過し cookie が設定されることを検証する。
func TestCSRF_GET_Passes(t *testing.T) {
	// Given
	h := httpsrv.CSRF(nopHandler)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	// When
	h.ServeHTTP(w, r)

	// Then
	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d", w.Code)
	}
	cookies := w.Result().Cookies()
	var found bool
	for _, c := range cookies {
		if c.Name == "axion_csrf" {
			found = true
			if c.Value == "" {
				t.Error("axion_csrf cookie value must not be empty")
			}
		}
	}
	if !found {
		t.Error("want axion_csrf cookie, got none")
	}
}

// TestCSRF_POSTWithoutToken_403 は cookie なし POST で 403 を返すことを検証する。
func TestCSRF_POSTWithoutToken_403(t *testing.T) {
	// Given
	h := httpsrv.CSRF(nopHandler)
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	w := httptest.NewRecorder()

	// When
	h.ServeHTTP(w, r)

	// Then
	if w.Code != http.StatusForbidden {
		t.Errorf("want 403, got %d", w.Code)
	}
}

// TestCSRF_POSTWithMatchingHeader_Pass は cookie + X-CSRF-Token 一致で通過することを検証する。
func TestCSRF_POSTWithMatchingHeader_Pass(t *testing.T) {
	// Given: GET で cookie を取得する
	h := httpsrv.CSRF(nopHandler)
	getReq := httptest.NewRequest(http.MethodGet, "/", nil)
	getW := httptest.NewRecorder()
	h.ServeHTTP(getW, getReq)

	var csrfToken string
	var rawCookie string
	for _, c := range getW.Result().Cookies() {
		if c.Name == "axion_csrf" {
			csrfToken = c.Value
			rawCookie = c.Name + "=" + c.Value
		}
	}
	if csrfToken == "" {
		t.Fatal("failed to obtain CSRF token from GET")
	}

	// When: cookie と X-CSRF-Token ヘッダを送る
	postReq := httptest.NewRequest(http.MethodPost, "/", nil)
	postReq.Header.Set("Cookie", rawCookie)
	postReq.Header.Set("X-CSRF-Token", csrfToken)
	postW := httptest.NewRecorder()
	h.ServeHTTP(postW, postReq)

	// Then
	if postW.Code != http.StatusOK {
		t.Errorf("want 200, got %d", postW.Code)
	}
}

// TestCSRF_POSTWithMatchingForm_Pass は cookie + _csrf フォーム値一致で通過することを検証する。
func TestCSRF_POSTWithMatchingForm_Pass(t *testing.T) {
	// Given: GET で cookie を取得する
	h := httpsrv.CSRF(nopHandler)
	getReq := httptest.NewRequest(http.MethodGet, "/", nil)
	getW := httptest.NewRecorder()
	h.ServeHTTP(getW, getReq)

	var csrfToken string
	var rawCookie string
	for _, c := range getW.Result().Cookies() {
		if c.Name == "axion_csrf" {
			csrfToken = c.Value
			rawCookie = c.Name + "=" + c.Value
		}
	}
	if csrfToken == "" {
		t.Fatal("failed to obtain CSRF token from GET")
	}

	// When: form body に _csrf を含む POST を送る
	form := url.Values{"_csrf": {csrfToken}}
	postReq := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq.Header.Set("Cookie", rawCookie)
	postW := httptest.NewRecorder()
	h.ServeHTTP(postW, postReq)

	// Then
	if postW.Code != http.StatusOK {
		t.Errorf("want 200, got %d", postW.Code)
	}
}

// TestCSRF_POSTWithMismatchedToken_403 は cookie と X-CSRF-Token が不一致で 403 を返すことを検証する。
func TestCSRF_POSTWithMismatchedToken_403(t *testing.T) {
	// Given: GET で cookie を取得する
	h := httpsrv.CSRF(nopHandler)
	getReq := httptest.NewRequest(http.MethodGet, "/", nil)
	getW := httptest.NewRecorder()
	h.ServeHTTP(getW, getReq)

	var rawCookie string
	for _, c := range getW.Result().Cookies() {
		if c.Name == "axion_csrf" {
			rawCookie = c.Name + "=" + c.Value
		}
	}

	// When: 異なるトークンを X-CSRF-Token に送る
	postReq := httptest.NewRequest(http.MethodPost, "/", nil)
	postReq.Header.Set("Cookie", rawCookie)
	postReq.Header.Set("X-CSRF-Token", "invalid-token-that-does-not-match")
	postW := httptest.NewRecorder()
	h.ServeHTTP(postW, postReq)

	// Then
	if postW.Code != http.StatusForbidden {
		t.Errorf("want 403, got %d", postW.Code)
	}
}

// TestCSRF_TokenAvailableInContext は GET 後に CSRFTokenFromContext でトークンを取得できることを検証する。
func TestCSRF_TokenAvailableInContext(t *testing.T) {
	// Given
	var capturedToken string
	captureHandler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		capturedToken = httpsrv.CSRFTokenFromContext(r.Context())
	})
	h := httpsrv.CSRF(captureHandler)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	// When
	h.ServeHTTP(w, r)

	// Then
	if capturedToken == "" {
		t.Error("want non-empty CSRF token in context, got empty")
	}
	var cookieToken string
	for _, c := range w.Result().Cookies() {
		if c.Name == "axion_csrf" {
			cookieToken = c.Value
		}
	}
	if capturedToken != cookieToken {
		t.Errorf("context token %q != cookie token %q", capturedToken, cookieToken)
	}
}
