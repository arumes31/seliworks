package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCookieSecurityConfig(t *testing.T) {
	for _, name := range []string{"DATABASE_URL_FILE", "TOTP_KEY_FILE", "SMTP_PASSWORD_FILE", "SMTP_HOST", "TRUSTED_PROXY_CIDRS", "HSTS_PROXY_MANAGED"} {
		t.Setenv(name, "")
	}
	t.Setenv("DATABASE_URL", "postgres://example.invalid/seliworks")
	for _, tc := range []struct {
		name, environment, origin, wantOrigin string
		secure                                bool
	}{
		{"production", "production", "https://work.example.com", "https://work.example.com", true},
		{"default environment", "", "https://work.example.com/", "https://work.example.com", true},
		{"uppercase scheme", "production", "HTTPS://work.example.com/", "https://work.example.com", true},
		{"mixed scheme", "production", "hTtPs://work.example.com:8443", "https://work.example.com:8443", true},
		{"uppercase hostname", "production", "HTTPS://WORK.EXAMPLE.COM:8443/", "https://work.example.com:8443", true},
		{"development HTTPS", "development", "https://localhost:16540", "https://localhost:16540", true},
		{"development localhost", "development", "http://localhost:16540", "http://localhost:16540", false},
		{"development uppercase localhost", "development", "HTTP://LOCALHOST:16540", "http://localhost:16540", false},
		{"development IPv4", "development", "http://127.0.0.1:16540", "http://127.0.0.1:16540", false},
		{"development IPv6", "development", "http://[::1]:16540", "http://[::1]:16540", false},
		{"production HTTP", "production", "http://work.example.com", "", false},
		{"default HTTP", "", "http://localhost:16540", "", false},
		{"production loopback", "production", "http://127.0.0.1", "", false},
		{"development public HTTP", "development", "http://work.example.com", "", false},
		{"loopback lookalike", "development", "http://localhost.example.com", "", false},
		{"credentials", "development", "http://user@localhost", "", false},
		{"path", "development", "http://localhost/path", "", false},
		{"query", "development", "http://localhost?x=1", "", false},
		{"fragment", "development", "http://localhost/#fragment", "", false},
		{"missing scheme", "production", "work.example.com", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("APP_ENV", tc.environment)
			t.Setenv("APP_URL", tc.origin)
			cfg, err := LoadConfig()
			if tc.wantOrigin == "" {
				if err == nil {
					t.Fatal("unsafe origin accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.AppURL != tc.wantOrigin {
				t.Errorf("AppURL = %q, want %q", cfg.AppURL, tc.wantOrigin)
			}
			a := &App{Config: cfg}
			assertSecurityCookies(t, a, tc.secure)
			r := httptest.NewRequest("GET", tc.wantOrigin+"/api/not-found", nil)
			r.Header.Set("Origin", tc.wantOrigin)
			if !a.originOK(r) {
				t.Error("canonical browser origin rejected")
			}
			w := httptest.NewRecorder()
			a.Handler().ServeHTTP(w, r)
			if got := w.Header().Get("Strict-Transport-Security") != ""; got != tc.secure {
				t.Errorf("HSTS present = %v, want %v", got, tc.secure)
			}
		})
	}
}

func TestCookieSecurityDefaults(t *testing.T) {
	// Cookie issuance remains secure even if a caller bypasses LoadConfig.
	for _, cfg := range []Config{
		{},
		{AppURL: "http://localhost", Environment: "production"},
		{AppURL: "http://work.example.com", Environment: "development"},
		{AppURL: "http://user@localhost", Environment: "development"},
		{AppURL: "http://localhost/path", Environment: "development"},
		{AppURL: "://", Environment: "development"},
	} {
		assertSecurityCookies(t, &App{Config: cfg}, true)
	}
}

func assertSecurityCookies(t *testing.T, a *App, secure bool) {
	t.Helper()
	for _, tc := range []struct {
		name, path string
		sameSite   http.SameSite
		set        func(http.ResponseWriter, string, int)
	}{
		{"seliworks_session", "/", http.SameSiteStrictMode, a.setCookie},
		{"seliworks_passkey", "/api/v1", http.SameSiteStrictMode, a.passkeyCookie},
	} {
		for _, age := range []int{300, -1} {
			w := httptest.NewRecorder()
			tc.set(w, "fixture", age)
			cookies := w.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatalf("%s: got %d cookies", tc.name, len(cookies))
			}
			c := cookies[0]
			if c.Name != tc.name || c.Path != tc.path || c.Value != "fixture" || c.MaxAge != age || c.Domain != "" || c.Secure != secure || !c.HttpOnly || c.SameSite != tc.sameSite {
				t.Errorf("%s: unexpected cookie flags: %s (want Secure=%v)", tc.name, c.String(), secure)
			}
		}
	}
}

func TestCookieSecurityHSTSProxyManaged(t *testing.T) {
	for _, name := range []string{"DATABASE_URL_FILE", "TOTP_KEY_FILE", "SMTP_PASSWORD_FILE", "SMTP_HOST", "TRUSTED_PROXY_CIDRS", "APP_ENV"} {
		t.Setenv(name, "")
	}
	t.Setenv("DATABASE_URL", "postgres://example.invalid/seliworks")
	t.Setenv("APP_URL", "https://work.example.com")
	for _, tc := range []struct {
		value string
		hsts  bool
		valid bool
	}{
		{"", true, true},
		{"false", true, true},
		{"true", false, true},
		{"yes", false, false},
	} {
		t.Run("value="+tc.value, func(t *testing.T) {
			t.Setenv("HSTS_PROXY_MANAGED", tc.value)
			cfg, err := LoadConfig()
			if !tc.valid {
				if err == nil {
					t.Fatal("invalid HSTS_PROXY_MANAGED accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			(&App{Config: cfg}).Handler().ServeHTTP(w, httptest.NewRequest("GET", cfg.AppURL+"/api/not-found", nil))
			if got := w.Header().Get("Strict-Transport-Security") != ""; got != tc.hsts {
				t.Errorf("HSTS present = %v, want %v", got, tc.hsts)
			}
			// Proxy-managed HSTS never weakens session cookie flags.
			assertSecurityCookies(t, &App{Config: cfg}, true)
		})
	}
}

func TestCookieSecurityRetryAfter(t *testing.T) {
	w := httptest.NewRecorder()
	handleError(w, httptest.NewRequest("POST", "/api/v1/auth/login", nil), &Problem{Status: 429, Code: "RATE_LIMIT", Message: "slow down", RetryAfter: 120})
	if w.Code != 429 || w.Header().Get("Retry-After") != "120" {
		t.Fatalf("status %d Retry-After %q", w.Code, w.Header().Get("Retry-After"))
	}
	w = httptest.NewRecorder()
	handleError(w, httptest.NewRequest("GET", "/", nil), problem(409, "CONFLICT", "x"))
	if w.Header().Get("Retry-After") != "" {
		t.Fatal("Retry-After sent without a retry hint")
	}
}

// setupTokenFromURL is untagged so integration and local unit tests share it.
//
//nolint:unused // untagged callers are local, unpublished tests
func setupTokenFromURL(s string) string {
	parts := strings.Split(s, "/#setup/")
	if len(parts) != 2 {
		return ""
	}
	return parts[1]
}
