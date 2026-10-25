package app

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/argon2"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

type User struct {
	ID                 string     `json:"id"`
	Name               string     `json:"name"`
	Email              string     `json:"email"`
	IsAdmin            bool       `json:"is_admin"`
	IsWorker           bool       `json:"is_worker"`
	Active             bool       `json:"active"`
	Language           string     `json:"language"`
	Theme              string     `json:"theme"`
	Version            int64      `json:"version"`
	PasskeyCount       int        `json:"passkey_count"`
	PasskeyRemindAfter *time.Time `json:"passkey_remind_after"`
	TOTPEnabled        bool       `json:"totp_enabled"`
}
type authContext struct {
	User            User
	CSRF, TokenHash string
}
type authKey struct{}

func actor(r *http.Request) User { v, _ := r.Context().Value(authKey{}).(authContext); return v.User }
func hashPassword(p string) (string, error) {
	if len(p) < 12 || len(p) > 256 {
		return "", problem(422, "PASSWORD_POLICY", "Use a password between 12 and 256 characters")
	}
	salt := []byte(token()[:32])
	h := argon2.IDKey([]byte(p), salt, 3, 64*1024, 2, 32)
	return fmt.Sprintf("$argon2id$v=19$m=65536,t=3,p=2$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(h)), nil
}
func verifyPassword(encoded, p string) bool {
	if len(p) > 256 {
		return false
	}
	s := strings.Split(encoded, "$")
	if len(s) != 6 || s[1] != "argon2id" || s[2] != "v=19" || s[3] != "m=65536,t=3,p=2" {
		return false
	}
	salt, e := base64.RawStdEncoding.DecodeString(s[4])
	if e != nil || len(salt) != 32 {
		return false
	}
	expected, e := base64.RawStdEncoding.DecodeString(s[5])
	if e != nil || len(expected) != 32 {
		return false
	}
	h := argon2.IDKey([]byte(p), salt, 3, 64*1024, 2, 32)
	return subtle.ConstantTimeCompare(h, expected) == 1
}
func digest(s string) string                 { v := sha256.Sum256([]byte(s)); return hex.EncodeToString(v[:]) }
func (a *App) originOK(r *http.Request) bool { return r.Header.Get("Origin") == a.Config.AppURL }
func (a *App) clientIP(r *http.Request) string {
	h, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		return "unknown"
	}
	ip, e := netip.ParseAddr(h)
	if e != nil {
		return "unknown"
	}
	trusted := false
	for _, p := range a.Config.TrustedProxies {
		if p.Contains(ip) {
			trusted = true
			break
		}
	}
	if trusted {
		chain := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
		for i := len(chain) - 1; i >= 0; i-- {
			next, e := netip.ParseAddr(strings.TrimSpace(chain[i]))
			if e != nil {
				continue
			}
			isProxy := false
			for _, p := range a.Config.TrustedProxies {
				if p.Contains(next) {
					isProxy = true
					break
				}
			}
			if !isProxy {
				return next.String()
			}
		}
	}
	return ip.String()
}
func (a *App) limit(ctx context.Context, bucket string, maximum int) error {
	var attempts int
	e := a.DB.QueryRow(ctx, `INSERT INTO auth_limits(bucket,window_start,attempts) VALUES($1,clock_timestamp(),1) ON CONFLICT(bucket) DO UPDATE SET attempts=CASE WHEN auth_limits.window_start<clock_timestamp()-interval '15 minutes' THEN 1 ELSE auth_limits.attempts+1 END,window_start=CASE WHEN auth_limits.window_start<clock_timestamp()-interval '15 minutes' THEN clock_timestamp() ELSE auth_limits.window_start END RETURNING attempts`, digest(bucket)).Scan(&attempts)
	if e != nil {
		return e
	}
	if attempts > maximum {
		return problem(429, "RATE_LIMIT", "Too many attempts. Try again in 15 minutes.")
	}
	return nil
}
func (a *App) login(w http.ResponseWriter, r *http.Request) {
	if !a.originOK(r) {
		fail(w, r, 403, "CSRF", "Request origin is not permitted")
		return
	}
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		TOTPCode string `json:"totp_code"`
	}
	if e := decode(r, &in); e != nil {
		handleError(w, r, e)
		return
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	if e := a.limit(r.Context(), "login-ip:"+a.clientIP(r), 30); e != nil {
		handleError(w, r, e)
		return
	}
	// Hard-block guessing per account and source; the account-wide bucket only stops distributed guessing,
	// so a single client knowing an email address cannot lock that user out from their own network.
	if e := a.limit(r.Context(), "login-account-ip:"+in.Email+"|"+a.clientIP(r), 15); e != nil {
		handleError(w, r, e)
		return
	}
	if e := a.limit(r.Context(), "login-account:"+in.Email, 100); e != nil {
		handleError(w, r, e)
		return
	}
	var u User
	var hash string
	e := a.DB.QueryRow(r.Context(), `SELECT id::text,name,email,is_admin,is_worker,active,language,theme,version,password_hash FROM users WHERE email=$1`, in.Email).Scan(&u.ID, &u.Name, &u.Email, &u.IsAdmin, &u.IsWorker, &u.Active, &u.Language, &u.Theme, &u.Version, &hash)
	if e != nil && e != pgx.ErrNoRows {
		handleError(w, r, e)
		return
	}
	if hash == "" {
		hash = "$argon2id$v=19$m=65536,t=3,p=2$MDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDA$MDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDA"
	}
	valid := verifyPassword(hash, in.Password)
	if !valid || !u.Active {
		fail(w, r, 401, "INVALID_CREDENTIALS", "Email or password is incorrect")
		return
	}
	tx, e := a.DB.Begin(r.Context())
	if e != nil {
		handleError(w, r, e)
		return
	}
	defer tx.Rollback(r.Context())
	// Serialize with deactivation and password changes so a concurrent login cannot escape revocation.
	var stillActive bool
	var currentHash string
	e = tx.QueryRow(r.Context(), `SELECT active,password_hash FROM users WHERE id=$1 FOR UPDATE`, u.ID).Scan(&stillActive, &currentHash)
	if e != nil {
		handleError(w, r, e)
		return
	}
	if !stillActive || currentHash != hash {
		fail(w, r, 401, "INVALID_CREDENTIALS", "Email or password is incorrect")
		return
	}
	if e = a.verifyMFA(r.Context(), tx, u.ID, in.TOTPCode); e != nil {
		handleError(w, r, e)
		return
	}
	raw, csrf := token(), token()
	if old, e := r.Cookie("seliworks_session"); e == nil {
		if _, e = tx.Exec(r.Context(), `DELETE FROM auth_sessions WHERE token_hash=$1`, digest(old.Value)); e != nil {
			handleError(w, r, e)
			return
		}
	}
	_, e = tx.Exec(r.Context(), `INSERT INTO auth_sessions(token_hash,user_id,csrf_token,expires_at) VALUES($1,$2,$3,clock_timestamp()+interval '14 days')`, digest(raw), u.ID, csrf)
	if e == nil {
		e = a.audit(r.Context(), tx, u.ID, "auth.login", u.ID, nil, nil)
	}
	if e == nil {
		e = tx.Commit(r.Context())
	}
	if e != nil {
		handleError(w, r, e)
		return
	}
	a.setCookie(w, raw, 14*86400)
	a.writeMe(w, r, authContext{u, csrf, digest(raw)})
}
func (a *App) setCookie(w http.ResponseWriter, value string, age int) {
	http.SetCookie(w, &http.Cookie{Name: "seliworks_session", Value: value, Path: "/", Secure: !a.Config.insecureDevelopmentHTTP(), HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: age})
}
func (a *App) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, e := r.Cookie("seliworks_session")
		if e != nil || len(cookie.Value) != 64 {
			fail(w, r, 401, "UNAUTHENTICATED", "Please sign in")
			return
		}
		var v authContext
		v.TokenHash = digest(cookie.Value)
		e = a.DB.QueryRow(r.Context(), `SELECT u.id::text,u.name,u.email,u.is_admin,u.is_worker,u.active,u.language,u.theme,u.version,s.csrf_token FROM auth_sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=$1 AND s.expires_at>clock_timestamp() AND u.active`, v.TokenHash).Scan(&v.User.ID, &v.User.Name, &v.User.Email, &v.User.IsAdmin, &v.User.IsWorker, &v.User.Active, &v.User.Language, &v.User.Theme, &v.User.Version, &v.CSRF)
		if e == pgx.ErrNoRows {
			fail(w, r, 401, "UNAUTHENTICATED", "Session expired. Please sign in.")
			return
		}
		if e != nil {
			handleError(w, r, e)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			if !a.originOK(r) || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(v.CSRF)) != 1 {
				fail(w, r, 403, "CSRF", "Refresh your session before saving")
				return
			}
		}
		next(w, r.WithContext(context.WithValue(r.Context(), authKey{}, v)))
	}
}
func (a *App) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		if !actor(r).IsAdmin {
			fail(w, r, 403, "FORBIDDEN", "Administrator permission required")
			return
		}
		next(w, r)
	})
}
func (a *App) writeMe(w http.ResponseWriter, r *http.Request, v authContext) {
	a.me(w, r.WithContext(context.WithValue(r.Context(), authKey{}, v)))
}
func (a *App) me(w http.ResponseWriter, r *http.Request) {
	v := r.Context().Value(authKey{}).(authContext)
	if e := a.DB.QueryRow(r.Context(), `SELECT coalesce((SELECT enabled FROM user_totp WHERE user_id=$1),false),(SELECT count(*) FROM passkeys WHERE user_id=$1),passkey_remind_after FROM users WHERE id=$1`, v.User.ID).Scan(&v.User.TOTPEnabled, &v.User.PasskeyCount, &v.User.PasskeyRemindAfter); e != nil {
		handleError(w, r, e)
		return
	}
	var org map[string]any
	var raw []byte
	if e := a.DB.QueryRow(r.Context(), `SELECT (to_jsonb(o)-'id')||jsonb_build_object('tax_percent',tax_percent::text) FROM organization o`).Scan(&raw); e != nil {
		handleError(w, r, e)
		return
	}
	if e := unmarshal(raw, &org); e != nil {
		handleError(w, r, e)
		return
	}
	if !v.User.IsAdmin {
		org = map[string]any{"name": org["name"], "timezone": org["timezone"], "currency": org["currency"], "long_session_hours": org["long_session_hours"]}
	}
	writeJSON(w, 200, map[string]any{"user": v.User, "csrf_token": v.CSRF, "organization": org, "totp_available": len(a.Config.TOTPKey) == 32, "smtp_available": a.Config.SMTPHost != ""})
}
func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	v := r.Context().Value(authKey{}).(authContext)
	if _, e := a.DB.Exec(r.Context(), `DELETE FROM auth_sessions WHERE token_hash=$1`, v.TokenHash); e != nil {
		handleError(w, r, e)
		return
	}
	a.setCookie(w, "", -1)
	w.Header().Set("Clear-Site-Data", "\"cache\", \"storage\"")
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (a *App) updateMe(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Language string `json:"language"`
		Theme    string `json:"theme"`
	}
	if e := decode(r, &in); e != nil {
		handleError(w, r, e)
		return
	}
	if in.Language != "en" && in.Language != "de" || in.Theme != "system" && in.Theme != "light" && in.Theme != "dark" {
		fail(w, r, 422, "VALIDATION", "Unsupported language or theme")
		return
	}
	if _, e := a.DB.Exec(r.Context(), `UPDATE users SET language=$2,theme=$3,version=version+1 WHERE id=$1`, actor(r).ID, in.Language, in.Theme); e != nil {
		handleError(w, r, e)
		return
	}
	v := r.Context().Value(authKey{}).(authContext)
	v.User.Language = in.Language
	v.User.Theme = in.Theme
	v.User.Version++
	r = r.WithContext(context.WithValue(r.Context(), authKey{}, v))
	a.me(w, r)
}
func (a *App) password(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if e := decode(r, &in); e != nil {
		handleError(w, r, e)
		return
	}
	if e := a.limit(r.Context(), "password:"+actor(r).ID, 10); e != nil {
		handleError(w, r, e)
		return
	}
	hash, e := hashPassword(in.New)
	if e != nil {
		handleError(w, r, e)
		return
	}
	tx, e := a.DB.Begin(r.Context())
	if e != nil {
		handleError(w, r, e)
		return
	}
	defer tx.Rollback(r.Context())
	var old string
	if e = tx.QueryRow(r.Context(), `SELECT password_hash FROM users WHERE id=$1 FOR UPDATE`, actor(r).ID).Scan(&old); e != nil {
		handleError(w, r, e)
		return
	}
	if !verifyPassword(old, in.Current) {
		fail(w, r, 403, "INVALID_PASSWORD", "Current password is incorrect")
		return
	}
	_, e = tx.Exec(r.Context(), `UPDATE users SET password_hash=$2,version=version+1 WHERE id=$1`, actor(r).ID, hash)
	if e == nil {
		_, e = tx.Exec(r.Context(), `DELETE FROM auth_sessions WHERE user_id=$1`, actor(r).ID)
	}
	if e == nil {
		_, e = tx.Exec(r.Context(), `DELETE FROM invitations WHERE user_id=$1 AND used_at IS NULL`, actor(r).ID)
	}
	if e == nil {
		e = a.audit(r.Context(), tx, actor(r).ID, "auth.password_changed", actor(r).ID, nil, nil)
	}
	if e == nil {
		e = tx.Commit(r.Context())
	}
	if e != nil {
		handleError(w, r, e)
		return
	}
	a.setCookie(w, "", -1)
	writeJSON(w, 200, map[string]bool{"ok": true})
}
