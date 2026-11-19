package app

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	sessionCookie = "seliworks_session"
	// sessionTTL is the idle lifetime: a session stays signed in for 30 days
	// after its last use on the same device.
	sessionTTL = 30 * 24 * time.Hour
	// sessionRenewAfter throttles sliding renewal to at most one database write
	// and Set-Cookie per session per this interval.
	sessionRenewAfter = 12 * time.Hour
	// sessionLifetime is what the database expiry and cookie Max-Age are set to
	// on each renewal. The extra throttle window guarantees that a request which
	// falls inside the throttle (and therefore does not renew) still leaves at
	// least sessionTTL from that request: the effective idle lifetime is between
	// 30 days and 30 days plus 12 hours.
	sessionLifetime = sessionTTL + sessionRenewAfter
)

// deviceClass reduces a User-Agent to a coarse, version-free platform and
// browser family. Sessions are bound to this value so a copied cookie cannot be
// replayed from a different kind of device, while OS and browser updates on
// the same device keep the session alive. Platforms are grouped so the
// "Request Desktop Website" toggles do not change the class: iPhone, iPad and
// Mac are all "apple" (iOS desktop mode reports macOS), and Android and Linux
// are both "linux" (Android desktop mode reports X11 Linux).
func deviceClass(userAgent string) string {
	u := strings.ToLower(userAgent)
	platform := "other"
	switch {
	case strings.Contains(u, "iphone") || strings.Contains(u, "ipod") || strings.Contains(u, "ipad") || strings.Contains(u, "macintosh") || strings.Contains(u, "mac os x"):
		platform = "apple"
	case strings.Contains(u, "; cros "):
		platform = "chromeos"
	case strings.Contains(u, "android") || strings.Contains(u, "linux"):
		platform = "linux"
	case strings.Contains(u, "windows"):
		platform = "windows"
	}
	browser := "other"
	switch {
	case strings.Contains(u, "edg/") || strings.Contains(u, "edga/") || strings.Contains(u, "edgios/"):
		browser = "edge"
	case strings.Contains(u, "opr/") || strings.Contains(u, "opt/"):
		browser = "opera"
	case strings.Contains(u, "samsungbrowser/"):
		browser = "samsung"
	case strings.Contains(u, "firefox/") || strings.Contains(u, "fxios/"):
		browser = "firefox"
	case strings.Contains(u, "crios/") || strings.Contains(u, "chrome/") || strings.Contains(u, "chromium/"):
		browser = "chrome"
	case strings.Contains(u, "applewebkit/"):
		// Safari, and iOS home-screen apps whose User-Agent omits the Safari token.
		browser = "safari"
	}
	return platform + "/" + browser
}

// createSession replaces the request's previous session (if any) with a new
// one bound to the requesting device. The caller commits tx and then calls
// setCookie with the returned token.
func createSession(ctx context.Context, tx pgx.Tx, r *http.Request, userID string) (raw, csrf string, err error) {
	raw, csrf = token(), token()
	if old, e := r.Cookie(sessionCookie); e == nil {
		if _, err = tx.Exec(ctx, `DELETE FROM auth_sessions WHERE token_hash=$1`, digest(old.Value)); err != nil {
			return "", "", err
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO auth_sessions(token_hash,user_id,csrf_token,device,expires_at,renewed_at) VALUES($1,$2,$3,$4,clock_timestamp()+make_interval(secs=>$5),clock_timestamp())`, digest(raw), userID, csrf, deviceClass(r.UserAgent()), sessionLifetime.Seconds())
	return raw, csrf, err
}

func (a *App) setSessionCookie(w http.ResponseWriter, raw string) {
	a.setCookie(w, raw, int(sessionLifetime.Seconds()))
}

// revokeForeignDevice ends a session presented from a different device class
// than the one it was issued to.
func (a *App) revokeForeignDevice(ctx context.Context, tokenHash, userID, issued, presented string) error {
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `DELETE FROM auth_sessions WHERE token_hash=$1`, tokenHash); err != nil {
		return err
	}
	if err = a.audit(ctx, tx, userID, "auth.session_device_mismatch", userID, map[string]string{"device": issued}, map[string]string{"device": presented}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// adoptDevice binds a session created before device binding to the device
// presenting it and returns the stored device. The row lock serializes
// concurrent first requests, so a second device sees the first one's binding
// (and is revoked by the caller) instead of slipping through. Adoption also
// marks the session for immediate renewal so its legacy cookie is reissued.
func (a *App) adoptDevice(ctx context.Context, tokenHash, device string) (string, error) {
	var stored string
	err := a.DB.QueryRow(ctx, `UPDATE auth_sessions SET device=CASE WHEN device='' THEN $2 ELSE device END,renewed_at=CASE WHEN device='' THEN '-infinity'::timestamptz ELSE renewed_at END WHERE token_hash=$1 RETURNING device`, tokenHash, device).Scan(&stored)
	return stored, err
}

// renewSession slides the session expiry forward on use, at most once per
// sessionRenewAfter, and reissues the cookie so the browser keeps it as long
// as the server does.
func (a *App) renewSession(ctx context.Context, w http.ResponseWriter, raw, tokenHash string) error {
	tag, err := a.DB.Exec(ctx, `UPDATE auth_sessions SET expires_at=clock_timestamp()+make_interval(secs=>$2),renewed_at=clock_timestamp() WHERE token_hash=$1 AND renewed_at<clock_timestamp()-make_interval(secs=>$3) AND expires_at>clock_timestamp()`, tokenHash, sessionLifetime.Seconds(), sessionRenewAfter.Seconds())
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 1 {
		a.setSessionCookie(w, raw)
	}
	return nil
}
