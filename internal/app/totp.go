package app

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func sealSecret(key []byte, secret string) ([]byte, error) {
	b, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	g, e := cipher.NewGCM(b)
	if e != nil {
		return nil, e
	}
	nonce := make([]byte, g.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return nil, e
	}
	return g.Seal(nonce, nonce, []byte(secret), []byte("seliworks-totp-v1")), nil
}
func openSecret(key, encrypted []byte) (string, error) {
	b, e := aes.NewCipher(key)
	if e != nil {
		return "", e
	}
	g, e := cipher.NewGCM(b)
	if e != nil {
		return "", e
	}
	if len(encrypted) < g.NonceSize() {
		return "", errors.New("invalid encrypted secret")
	}
	plain, e := g.Open(nil, encrypted[:g.NonceSize()], encrypted[g.NonceSize():], []byte("seliworks-totp-v1"))
	return string(plain), e
}
func totpAt(secret string, counter int64) string {
	b, e := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if e != nil {
		return ""
	}
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, uint64(counter))
	h := hmac.New(sha1.New, b)
	h.Write(buf)
	sum := h.Sum(nil)
	offset := sum[len(sum)-1] & 15
	n := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", n%1000000)
}
func matchedCounter(secret, code string, now time.Time, last int64) (int64, bool) {
	if len(code) != 6 {
		return 0, false
	}
	for delta := int64(-1); delta <= 1; delta++ {
		c := now.Unix()/30 + delta
		if c > last && subtle.ConstantTimeCompare([]byte(totpAt(secret, c)), []byte(code)) == 1 {
			return c, true
		}
	}
	return 0, false
}
func (a *App) verifyMFA(ctx context.Context, tx pgx.Tx, userID, code string) error {
	var enabled bool
	var encrypted []byte
	var last int64
	e := tx.QueryRow(ctx, `SELECT enabled,secret,last_counter FROM user_totp WHERE user_id=$1 FOR UPDATE`, userID).Scan(&enabled, &encrypted, &last)
	if e == pgx.ErrNoRows {
		return nil
	}
	if e != nil {
		return e
	}
	if !enabled {
		return nil
	}
	secret, e := openSecret(a.Config.TOTPKey, encrypted)
	if e != nil {
		return problem(503, "MFA_UNAVAILABLE", "Authenticator key unavailable; contact your deployment administrator")
	}
	var now time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return e
	}
	if counter, ok := matchedCounter(secret, strings.TrimSpace(code), now, last); ok {
		_, e = tx.Exec(ctx, `UPDATE user_totp SET last_counter=$2 WHERE user_id=$1`, userID, counter)
		return e
	}
	tag, e := tx.Exec(ctx, `DELETE FROM recovery_codes WHERE user_id=$1 AND code_hash=$2`, userID, digest(strings.ReplaceAll(strings.TrimSpace(code), "-", "")))
	if e != nil {
		return e
	}
	if tag.RowsAffected() == 1 {
		return nil
	}
	return problem(401, "MFA_REQUIRED", "Enter a current authenticator code or an unused recovery code")
}
func (a *App) registerTOTP(m *http.ServeMux) {
	m.HandleFunc("POST /api/v1/me/totp/enroll", a.requireAuth(a.enrollTOTP))
	m.HandleFunc("POST /api/v1/me/totp/confirm", a.requireAuth(a.confirmTOTP))
	m.HandleFunc("POST /api/v1/me/totp/disable", a.requireAuth(a.disableTOTP))
}
func (a *App) enrollTOTP(w http.ResponseWriter, r *http.Request) {
	if len(a.Config.TOTPKey) != 32 {
		fail(w, r, 422, "MFA_UNAVAILABLE", "Configure the deployment TOTP encryption key first")
		return
	}
	var in struct {
		Password string `json:"password"`
	}
	if e := decode(r, &in); e != nil {
		handleError(w, r, e)
		return
	}
	if e := a.limit(r.Context(), "totp:"+actor(r).ID, 10); e != nil {
		handleError(w, r, e)
		return
	}
	tx, e := a.DB.Begin(r.Context())
	if e != nil {
		handleError(w, r, e)
		return
	}
	defer tx.Rollback(r.Context())
	var hash string
	if e = tx.QueryRow(r.Context(), `SELECT password_hash FROM users WHERE id=$1 FOR UPDATE`, actor(r).ID).Scan(&hash); e != nil {
		handleError(w, r, e)
		return
	}
	if !verifyPassword(hash, in.Password) {
		fail(w, r, 403, "INVALID_PASSWORD", "Current password is incorrect")
		return
	}
	var enabled bool
	e = tx.QueryRow(r.Context(), `SELECT coalesce((SELECT enabled FROM user_totp WHERE user_id=$1),false)`, actor(r).ID).Scan(&enabled)
	if e != nil {
		handleError(w, r, e)
		return
	}
	if enabled {
		fail(w, r, 409, "MFA_ENABLED", "Disable the current authenticator before replacing it")
		return
	}
	seed := make([]byte, 20)
	if _, e = rand.Read(seed); e != nil {
		handleError(w, r, e)
		return
	}
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(seed)
	encrypted, e := sealSecret(a.Config.TOTPKey, secret)
	if e == nil {
		_, e = tx.Exec(r.Context(), `INSERT INTO user_totp(user_id,secret,pending_secret) VALUES($1,$2,$2) ON CONFLICT(user_id) DO UPDATE SET pending_secret=$2,updated_at=clock_timestamp()`, actor(r).ID, encrypted)
	}
	if e == nil {
		e = a.audit(r.Context(), tx, actor(r).ID, "auth.totp_enrollment", actor(r).ID, nil, nil)
	}
	if e == nil {
		e = tx.Commit(r.Context())
	}
	if e != nil {
		handleError(w, r, e)
		return
	}
	uri := "otpauth://totp/" + url.PathEscape("SeliWorks:"+actor(r).Email) + "?secret=" + secret + "&issuer=SeliWorks&algorithm=SHA1&digits=6&period=30"
	writeJSON(w, 200, map[string]string{"secret": secret, "otpauth_url": uri})
}
func (a *App) confirmTOTP(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code string `json:"code"`
	}
	if e := decode(r, &in); e != nil {
		handleError(w, r, e)
		return
	}
	if e := a.limit(r.Context(), "totp:"+actor(r).ID, 10); e != nil {
		handleError(w, r, e)
		return
	}
	tx, e := a.DB.Begin(r.Context())
	if e != nil {
		handleError(w, r, e)
		return
	}
	defer tx.Rollback(r.Context())
	// Match setup/password lock order so MFA activation and setup revocation are atomic.
	var id string
	if e = tx.QueryRow(r.Context(), `SELECT id::text FROM users WHERE id=$1 FOR UPDATE`, actor(r).ID).Scan(&id); e != nil {
		handleError(w, r, e)
		return
	}
	var encrypted []byte
	var now time.Time
	e = tx.QueryRow(r.Context(), `SELECT pending_secret,clock_timestamp() FROM user_totp WHERE user_id=$1 AND NOT enabled AND updated_at>clock_timestamp()-interval '15 minutes' FOR UPDATE`, actor(r).ID).Scan(&encrypted, &now)
	if e != nil {
		handleError(w, r, e)
		return
	}
	secret, e := openSecret(a.Config.TOTPKey, encrypted)
	if e != nil {
		handleError(w, r, e)
		return
	}
	counter, ok := matchedCounter(secret, in.Code, now, -1)
	if !ok {
		fail(w, r, 422, "INVALID_CODE", "Authenticator code is incorrect")
		return
	}
	_, e = tx.Exec(r.Context(), `UPDATE user_totp SET secret=pending_secret,pending_secret=NULL,enabled=true,last_counter=$2 WHERE user_id=$1`, actor(r).ID, counter)
	codes := []string{}
	if e == nil {
		_, e = tx.Exec(r.Context(), `DELETE FROM recovery_codes WHERE user_id=$1`, actor(r).ID)
	}
	for i := 0; i < 10 && e == nil; i++ {
		raw := token()[:20]
		codes = append(codes, raw[:10]+"-"+raw[10:])
		_, e = tx.Exec(r.Context(), `INSERT INTO recovery_codes(user_id,code_hash) VALUES($1,$2)`, actor(r).ID, digest(raw))
	}
	v := r.Context().Value(authKey{}).(authContext)
	if e == nil {
		_, e = tx.Exec(r.Context(), `DELETE FROM auth_sessions WHERE user_id=$1 AND token_hash<>$2`, actor(r).ID, v.TokenHash)
	}
	if e == nil {
		_, e = tx.Exec(r.Context(), `DELETE FROM invitations WHERE user_id=$1 AND used_at IS NULL`, actor(r).ID)
	}
	if e == nil {
		e = a.audit(r.Context(), tx, actor(r).ID, "auth.totp_enabled", actor(r).ID, nil, nil)
	}
	if e == nil {
		e = tx.Commit(r.Context())
	}
	if e != nil {
		handleError(w, r, e)
		return
	}
	writeJSON(w, 200, map[string]any{"recovery_codes": codes})
}
func (a *App) disableTOTP(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if e := decode(r, &in); e != nil {
		handleError(w, r, e)
		return
	}
	if e := a.limit(r.Context(), "totp:"+actor(r).ID, 10); e != nil {
		handleError(w, r, e)
		return
	}
	tx, e := a.DB.Begin(r.Context())
	if e != nil {
		handleError(w, r, e)
		return
	}
	defer tx.Rollback(r.Context())
	var hash string
	e = tx.QueryRow(r.Context(), `SELECT password_hash FROM users WHERE id=$1 FOR UPDATE`, actor(r).ID).Scan(&hash)
	if e != nil {
		handleError(w, r, e)
		return
	}
	if !verifyPassword(hash, in.Password) {
		fail(w, r, 403, "INVALID_PASSWORD", "Current password is incorrect")
		return
	}
	e = a.verifyMFA(r.Context(), tx, actor(r).ID, in.Code)
	if e == nil {
		_, e = tx.Exec(r.Context(), `DELETE FROM user_totp WHERE user_id=$1`, actor(r).ID)
	}
	if e == nil {
		_, e = tx.Exec(r.Context(), `DELETE FROM recovery_codes WHERE user_id=$1`, actor(r).ID)
	}
	if e == nil {
		e = a.audit(r.Context(), tx, actor(r).ID, "auth.totp_disabled", actor(r).ID, nil, nil)
	}
	if e == nil {
		e = tx.Commit(r.Context())
	}
	if e != nil {
		handleError(w, r, e)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
