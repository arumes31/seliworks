//go:build integration

package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const (
	renewalPhoneUA   = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148"
	renewalDesktopUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:133.0) Gecko/20100101 Firefox/133.0"
)

func sessionMe(a *App, raw, userAgent string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, a.Config.AppURL+"/api/v1/me", nil)
	r.Header.Set("User-Agent", userAgent)
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: raw})
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	return w
}

func sessionCookieFrom(w *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookie {
			return c
		}
	}
	return nil
}

func insertRenewalSession(t *testing.T, a *App, userID, device string, expiresIn, renewedAgo time.Duration) string {
	t.Helper()
	raw := token()
	if _, err := a.DB.Exec(context.Background(), `INSERT INTO auth_sessions(token_hash,user_id,csrf_token,device,expires_at,renewed_at) VALUES($1,$2,'renewal-test',$3,clock_timestamp()+make_interval(secs=>$4),clock_timestamp()-make_interval(secs=>$5))`, digest(raw), userID, device, expiresIn.Seconds(), renewedAgo.Seconds()); err != nil {
		t.Fatal(err)
	}
	return raw
}

func sessionRemaining(t *testing.T, a *App, raw string) time.Duration {
	t.Helper()
	var seconds float64
	if err := a.DB.QueryRow(context.Background(), `SELECT extract(epoch FROM expires_at-clock_timestamp()) FROM auth_sessions WHERE token_hash=$1`, digest(raw)).Scan(&seconds); err != nil {
		t.Fatal(err)
	}
	return time.Duration(seconds * float64(time.Second))
}

func TestSessionSlidingRenewal(t *testing.T) {
	a, user := invitationSecurityFixture(t)
	phone := deviceClass(renewalPhoneUA)

	t.Run("stale session is extended to 30 days and the cookie reissued", func(t *testing.T) {
		raw := insertRenewalSession(t, a, user.ID, phone, 2*24*time.Hour, 13*time.Hour)
		w := sessionMe(a, raw, renewalPhoneUA)
		if w.Code != http.StatusOK {
			t.Fatalf("status %d: %s", w.Code, w.Body)
		}
		c := sessionCookieFrom(w)
		if c == nil || c.Value != raw || c.MaxAge != int(sessionTTL.Seconds()) || c.SameSite != http.SameSiteLaxMode {
			t.Fatalf("renewal cookie not reissued: %v", c)
		}
		if left := sessionRemaining(t, a, raw); left < sessionTTL-time.Minute {
			t.Fatalf("session expires in %s, want about %s", left, sessionTTL)
		}
	})

	t.Run("recently renewed session is not rewritten", func(t *testing.T) {
		raw := insertRenewalSession(t, a, user.ID, phone, 20*24*time.Hour, time.Hour)
		w := sessionMe(a, raw, renewalPhoneUA)
		if w.Code != http.StatusOK {
			t.Fatalf("status %d: %s", w.Code, w.Body)
		}
		if c := sessionCookieFrom(w); c != nil {
			t.Fatalf("unexpected cookie on throttled request: %v", c)
		}
		if left := sessionRemaining(t, a, raw); left > 21*24*time.Hour {
			t.Fatalf("throttled session was extended to %s", left)
		}
	})

	t.Run("legacy session adopts the device on first use", func(t *testing.T) {
		raw := insertRenewalSession(t, a, user.ID, "", 5*24*time.Hour, 0)
		if w := sessionMe(a, raw, renewalPhoneUA); w.Code != http.StatusOK || sessionCookieFrom(w) == nil {
			t.Fatalf("legacy session not adopted: %d %v", w.Code, sessionCookieFrom(w))
		}
		var device string
		if err := a.DB.QueryRow(context.Background(), `SELECT device FROM auth_sessions WHERE token_hash=$1`, digest(raw)).Scan(&device); err != nil {
			t.Fatal(err)
		}
		if device != phone {
			t.Fatalf("device %q, want %q", device, phone)
		}
	})

	t.Run("cookie replayed from another device is revoked", func(t *testing.T) {
		raw := insertRenewalSession(t, a, user.ID, phone, 20*24*time.Hour, 0)
		w := sessionMe(a, raw, renewalDesktopUA)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("foreign device status %d, want 401", w.Code)
		}
		if c := sessionCookieFrom(w); c == nil || c.MaxAge >= 0 {
			t.Fatalf("foreign device cookie not cleared: %v", c)
		}
		if w := sessionMe(a, raw, renewalPhoneUA); w.Code != http.StatusUnauthorized {
			t.Fatalf("revoked session still valid on original device: %d", w.Code)
		}
		var audited int
		if err := a.DB.QueryRow(context.Background(), `SELECT count(*) FROM audit_events WHERE action='auth.session_device_mismatch'`).Scan(&audited); err != nil {
			t.Fatal(err)
		}
		if audited != 1 {
			t.Fatalf("device mismatch audit events %d, want 1", audited)
		}
	})

	t.Run("expired session is not renewed", func(t *testing.T) {
		raw := insertRenewalSession(t, a, user.ID, phone, -time.Minute, 2*24*time.Hour)
		if w := sessionMe(a, raw, renewalPhoneUA); w.Code != http.StatusUnauthorized || sessionCookieFrom(w) != nil {
			t.Fatalf("expired session: %d %v", w.Code, sessionCookieFrom(w))
		}
	})
}
