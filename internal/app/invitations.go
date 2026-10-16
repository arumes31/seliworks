package app

import (
	"net/http"
	"strings"
	"time"
)

func (a *App) invite(w http.ResponseWriter, r *http.Request) {
	var in struct{}
	if e := decode(r, &in); e != nil {
		handleError(w, r, e)
		return
	}
	if e := a.limit(r.Context(), "invite:"+actor(r).ID, 30); e != nil {
		handleError(w, r, e)
		return
	}
	tx, e := a.DB.Begin(r.Context())
	if e != nil {
		handleError(w, r, e)
		return
	}
	defer tx.Rollback(r.Context())
	var id string
	e = tx.QueryRow(r.Context(), `SELECT id::text FROM users WHERE id=$1 FOR UPDATE`, r.PathValue("id")).Scan(&id)
	if e != nil {
		handleError(w, r, e)
		return
	}
	var now time.Time
	e = tx.QueryRow(r.Context(), `SELECT clock_timestamp()`).Scan(&now)
	if e == nil {
		_, e = tx.Exec(r.Context(), `DELETE FROM invitations WHERE user_id=$1 AND used_at IS NULL`, id)
	}
	raw := token()
	expires := now.Add(24 * time.Hour)
	if e == nil {
		_, e = tx.Exec(r.Context(), `INSERT INTO invitations(token_hash,user_id,created_by,created_at,expires_at) VALUES($1,$2,$3,$4,$5)`, digest(raw), id, actor(r).ID, now, expires)
	}
	if e == nil {
		e = a.audit(r.Context(), tx, actor(r).ID, "auth.invitation_created", id, nil, map[string]any{"expires_at": expires.UTC()})
	}
	if e == nil {
		e = tx.Commit(r.Context())
	}
	if e != nil {
		handleError(w, r, e)
		return
	}
	writeJSON(w, 201, map[string]any{"setup_url": a.Config.AppURL + "/#setup/" + raw, "expires_at": expires.UTC()})
}
func (a *App) setupAccount(w http.ResponseWriter, r *http.Request) {
	if !a.originOK(r) {
		fail(w, r, 403, "CSRF", "Request origin is not permitted")
		return
	}
	if e := a.limit(r.Context(), "setup:"+a.clientIP(r), 20); e != nil {
		handleError(w, r, e)
		return
	}
	var in struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if e := decode(r, &in); e != nil {
		handleError(w, r, e)
		return
	}
	if len(in.Token) != 64 {
		fail(w, r, 422, "INVALID_INVITATION", "Invitation is invalid or expired")
		return
	}
	hash, e := hashPassword(in.Password)
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
	var id string
	// Lock user first, matching login/password operations; revalidate invitation after lock.
	e = tx.QueryRow(r.Context(), `SELECT u.id::text FROM users u JOIN invitations i ON i.user_id=u.id WHERE i.token_hash=$1 AND i.used_at IS NULL AND i.expires_at>clock_timestamp() FOR UPDATE OF u`, digest(in.Token)).Scan(&id)
	if e != nil {
		fail(w, r, 422, "INVALID_INVITATION", "Invitation is invalid or expired")
		return
	}
	var valid bool
	e = tx.QueryRow(r.Context(), `SELECT used_at IS NULL AND expires_at>clock_timestamp() FROM invitations WHERE token_hash=$1 FOR UPDATE`, digest(in.Token)).Scan(&valid)
	if e != nil || !valid {
		fail(w, r, 422, "INVALID_INVITATION", "Invitation is invalid or expired")
		return
	}
	_, e = tx.Exec(r.Context(), `UPDATE users SET password_hash=$2,active=true,version=version+1 WHERE id=$1`, id, hash)
	if e == nil {
		_, e = tx.Exec(r.Context(), `UPDATE invitations SET used_at=clock_timestamp() WHERE user_id=$1 AND used_at IS NULL`, id)
	}
	if e == nil {
		_, e = tx.Exec(r.Context(), `DELETE FROM auth_sessions WHERE user_id=$1`, id)
	}
	if e == nil {
		_, e = tx.Exec(r.Context(), `DELETE FROM user_totp WHERE user_id=$1`, id)
	}
	if e == nil {
		_, e = tx.Exec(r.Context(), `DELETE FROM recovery_codes WHERE user_id=$1`, id)
	}
	if e == nil {
		e = a.audit(r.Context(), tx, id, "auth.invitation_redeemed", id, nil, nil)
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
func setupTokenFromURL(s string) string {
	parts := strings.Split(s, "/#setup/")
	if len(parts) != 2 {
		return ""
	}
	return parts[1]
}
