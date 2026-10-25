package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type passkeyUser struct {
	User
	Credentials []webauthn.Credential
}

func (u passkeyUser) WebAuthnID() []byte                         { return []byte(u.ID) }
func (u passkeyUser) WebAuthnName() string                       { return u.Email }
func (u passkeyUser) WebAuthnDisplayName() string                { return u.Name }
func (u passkeyUser) WebAuthnCredentials() []webauthn.Credential { return u.Credentials }

func (a *App) webAuthn() (*webauthn.WebAuthn, error) {
	u, err := url.Parse(a.Config.AppURL)
	if err != nil {
		return nil, err
	}
	return webauthn.New(&webauthn.Config{
		RPID: u.Hostname(), RPDisplayName: "SeliWorks", RPOrigins: []string{a.Config.AppURL},
		AttestationPreference:  protocol.PreferNoAttestation,
		AuthenticatorSelection: protocol.AuthenticatorSelection{ResidentKey: protocol.ResidentKeyRequirementRequired, UserVerification: protocol.VerificationRequired},
		Timeouts: webauthn.TimeoutsConfig{
			Login:        webauthn.TimeoutConfig{Enforce: true, Timeout: 5 * time.Minute},
			Registration: webauthn.TimeoutConfig{Enforce: true, Timeout: 5 * time.Minute},
		},
	})
}

func (a *App) registerPasskeys(m *http.ServeMux) {
	m.HandleFunc("GET /api/v1/me/passkeys", a.requireAuth(a.listPasskeys))
	m.HandleFunc("POST /api/v1/me/passkeys/reminder", a.requireAuth(a.snoozePasskeyReminder))
	m.HandleFunc("POST /api/v1/me/passkeys/register/begin", a.requireAuth(a.beginPasskeyRegistration))
	m.HandleFunc("POST /api/v1/me/passkeys/register/finish", a.requireAuth(a.finishPasskeyRegistration))
	m.HandleFunc("DELETE /api/v1/me/passkeys/{id}", a.requireAuth(a.deletePasskey))
	m.HandleFunc("POST /api/v1/auth/passkeys/begin", a.beginPasskeyLogin)
	m.HandleFunc("POST /api/v1/auth/passkeys/finish", a.finishPasskeyLogin)
}

func (a *App) snoozePasskeyReminder(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Days int `json:"days"`
	}
	if err := decode(r, &in); err != nil {
		handleError(w, r, err)
		return
	}
	if in.Days < 3 || in.Days > 7 {
		fail(w, r, 422, "VALIDATION", "Choose a reminder interval from 3 to 7 days")
		return
	}
	if _, err := a.DB.Exec(r.Context(), `UPDATE users SET passkey_remind_after=clock_timestamp()+$2::int*interval '24 hours' WHERE id=$1`, actor(r).ID, in.Days); err != nil {
		handleError(w, r, err)
		return
	}
	a.me(w, r)
}

// All credential mutations and logins take the user lock first, serializing
// counter updates, account recovery, revocation and registration limits.
func loadPasskeyUser(ctx context.Context, tx pgx.Tx, id string) (passkeyUser, error) {
	var u passkeyUser
	if _, err := uuid.Parse(id); err != nil {
		return u, pgx.ErrNoRows
	}
	err := tx.QueryRow(ctx, `SELECT id::text,name,email,is_admin,is_worker,active,language,theme,version FROM users WHERE id=$1 AND active FOR UPDATE`, id).Scan(&u.ID, &u.Name, &u.Email, &u.IsAdmin, &u.IsWorker, &u.Active, &u.Language, &u.Theme, &u.Version)
	if err != nil {
		return u, err
	}
	rows, err := tx.Query(ctx, `SELECT credential FROM passkeys WHERE user_id=$1 ORDER BY id`, u.ID)
	if err != nil {
		return u, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		var c webauthn.Credential
		if err = rows.Scan(&raw); err != nil {
			return u, err
		}
		if err = json.Unmarshal(raw, &c); err != nil {
			return u, err
		}
		u.Credentials = append(u.Credentials, c)
	}
	return u, rows.Err()
}

func currentPasskeySession(r *http.Request, tx pgx.Tx) error {
	v := r.Context().Value(authKey{}).(authContext)
	var valid bool
	if err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM auth_sessions WHERE token_hash=$1 AND user_id=$2 AND expires_at>clock_timestamp())`, v.TokenHash, v.User.ID).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return problem(401, "UNAUTHENTICATED", "Sign in again")
	}
	return nil
}

type passkeyAuthorization struct {
	Name     string `json:"name"`
	Password string `json:"password"`
	TOTPCode string `json:"totp_code"`
}

func (a *App) authorizePasskey(r *http.Request, tx pgx.Tx, in passkeyAuthorization) error {
	if err := currentPasskeySession(r, tx); err != nil {
		return err
	}
	var hash string
	if err := tx.QueryRow(r.Context(), `SELECT password_hash FROM users WHERE id=$1`, actor(r).ID).Scan(&hash); err != nil {
		return err
	}
	if !verifyPassword(hash, in.Password) {
		return problem(403, "INVALID_PASSWORD", "Current password is incorrect")
	}
	return a.verifyMFA(r.Context(), tx, actor(r).ID, in.TOTPCode)
}

func (a *App) passkeyCookie(w http.ResponseWriter, value string, age int) {
	http.SetCookie(w, &http.Cookie{Name: "seliworks_passkey", Value: value, Path: "/api/v1", HttpOnly: true, Secure: !a.Config.insecureDevelopmentHTTP(), SameSite: http.SameSiteStrictMode, MaxAge: age})
}

func savePasskeyCeremony(r *http.Request, tx pgx.Tx, kind, name string, data *webauthn.SessionData) (string, error) {
	raw := token()
	var authHash, userID any
	if kind == "registration" {
		v := r.Context().Value(authKey{}).(authContext)
		authHash = v.TokenHash
		userID = v.User.ID
	}
	// Superseded ceremonies are removed only for the same browser binding.
	if old, err := r.Cookie("seliworks_passkey"); err == nil {
		if _, err = tx.Exec(r.Context(), `DELETE FROM passkey_ceremonies WHERE token_hash=$1`, digest(old.Value)); err != nil {
			return "", err
		}
	}
	_, err := tx.Exec(r.Context(), `INSERT INTO passkey_ceremonies(token_hash,kind,auth_hash,user_id,name,data,expires_at) VALUES($1,$2,$3,$4,$5,$6,clock_timestamp()+interval '5 minutes')`, digest(raw), kind, authHash, userID, name, mustJSON(data))
	return raw, err
}

// Consume outside the verification transaction: even an invalid attempt cannot
// reuse a challenge. Registration is also bound to the authenticated session.
func (a *App) consumePasskeyCeremony(w http.ResponseWriter, r *http.Request, kind string) (webauthn.SessionData, string, error) {
	var data webauthn.SessionData
	cookie, err := r.Cookie("seliworks_passkey")
	if err != nil || len(cookie.Value) != 64 {
		return data, "", problem(401, "PASSKEY_INVALID", "Passkey request expired; try again")
	}
	a.passkeyCookie(w, "", -1)
	authHash := ""
	if kind == "registration" {
		authHash = r.Context().Value(authKey{}).(authContext).TokenHash
	}
	var raw []byte
	var name string
	err = a.DB.QueryRow(r.Context(), `DELETE FROM passkey_ceremonies WHERE token_hash=$1 AND kind=$2 AND coalesce(auth_hash,'')=$3 AND expires_at>clock_timestamp() RETURNING data,name`, digest(cookie.Value), kind, authHash).Scan(&raw, &name)
	if err == pgx.ErrNoRows {
		return data, "", problem(401, "PASSKEY_INVALID", "Passkey request expired; try again")
	}
	if err == nil {
		err = json.Unmarshal(raw, &data)
	}
	return data, name, err
}

func (a *App) listPasskeys(w http.ResponseWriter, r *http.Request) {
	items, err := queryItems(r.Context(), a.DB, `SELECT jsonb_build_object('id',id,'name',name,'created_at',created_at,'last_used_at',last_used_at) FROM passkeys WHERE user_id=$1 ORDER BY created_at,id`, actor(r).ID)
	if err != nil {
		handleError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": len(items)})
}

func (a *App) beginPasskeyRegistration(w http.ResponseWriter, r *http.Request) {
	var in passkeyAuthorization
	if err := decode(r, &in); err != nil {
		handleError(w, r, err)
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 100 {
		fail(w, r, 422, "VALIDATION", "Name must be 1 to 100 bytes")
		return
	}
	if err := a.limit(r.Context(), "passkey-manage:"+actor(r).ID, 10); err != nil {
		handleError(w, r, err)
		return
	}
	wa, err := a.webAuthn()
	if err != nil {
		handleError(w, r, err)
		return
	}
	tx, err := a.DB.Begin(r.Context())
	if err != nil {
		handleError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	u, err := loadPasskeyUser(r.Context(), tx, actor(r).ID)
	if err == nil {
		err = a.authorizePasskey(r, tx, in)
	}
	if err == nil && len(u.Credentials) >= 20 {
		err = problem(422, "PASSKEY_LIMIT", "Remove an unused passkey before adding another")
	}
	var creation *protocol.CredentialCreation
	var session *webauthn.SessionData
	if err == nil {
		exclusions := make([]protocol.CredentialDescriptor, 0, len(u.Credentials))
		for _, c := range u.Credentials {
			exclusions = append(exclusions, c.Descriptor())
		}
		creation, session, err = wa.BeginRegistration(u, webauthn.WithExclusions(exclusions))
	}
	var cookie string
	if err == nil {
		cookie, err = savePasskeyCeremony(r, tx, "registration", in.Name, session)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		handleError(w, r, err)
		return
	}
	a.passkeyCookie(w, cookie, 300)
	writeJSON(w, 200, creation)
}

func (a *App) finishPasskeyRegistration(w http.ResponseWriter, r *http.Request) {
	session, name, err := a.consumePasskeyCeremony(w, r, "registration")
	if err != nil {
		handleError(w, r, err)
		return
	}
	wa, err := a.webAuthn()
	if err != nil {
		handleError(w, r, err)
		return
	}
	tx, err := a.DB.Begin(r.Context())
	if err != nil {
		handleError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	u, err := loadPasskeyUser(r.Context(), tx, actor(r).ID)
	if err == nil {
		err = currentPasskeySession(r, tx)
	}
	if err == nil && len(u.Credentials) >= 20 {
		err = problem(422, "PASSKEY_LIMIT", "Passkey limit reached")
	}
	var credential *webauthn.Credential
	if err == nil {
		credential, err = wa.FinishRegistration(u, session, r)
		if err != nil {
			err = problem(422, "PASSKEY_INVALID", "Passkey registration could not be verified")
		}
	}
	var id string
	if err == nil {
		err = tx.QueryRow(r.Context(), `INSERT INTO passkeys(user_id,credential_id,credential,name) VALUES($1,$2,$3,$4) RETURNING id::text`, u.ID, credential.ID, mustJSON(credential), name).Scan(&id)
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `UPDATE users SET passkey_remind_after=NULL WHERE id=$1`, u.ID)
	}
	if err == nil {
		err = a.audit(r.Context(), tx, u.ID, "auth.passkey_added", id, nil, map[string]any{"name": name})
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		handleError(w, r, err)
		return
	}
	writeJSON(w, 201, map[string]string{"id": id, "name": name})
}

func (a *App) deletePasskey(w http.ResponseWriter, r *http.Request) {
	var in passkeyAuthorization
	if err := decode(r, &in); err != nil {
		handleError(w, r, err)
		return
	}
	if err := a.limit(r.Context(), "passkey-manage:"+actor(r).ID, 10); err != nil {
		handleError(w, r, err)
		return
	}
	tx, err := a.DB.Begin(r.Context())
	if err != nil {
		handleError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	_, err = loadPasskeyUser(r.Context(), tx, actor(r).ID)
	if err == nil {
		err = a.authorizePasskey(r, tx, in)
	}
	var name string
	if err == nil {
		err = tx.QueryRow(r.Context(), `DELETE FROM passkeys WHERE id=$1 AND user_id=$2 RETURNING name`, r.PathValue("id"), actor(r).ID).Scan(&name)
	}
	if err == nil {
		v := r.Context().Value(authKey{}).(authContext)
		_, err = tx.Exec(r.Context(), `DELETE FROM auth_sessions WHERE user_id=$1 AND token_hash<>$2`, actor(r).ID, v.TokenHash)
	}
	if err == nil {
		err = a.audit(r.Context(), tx, actor(r).ID, "auth.passkey_removed", r.PathValue("id"), map[string]any{"name": name}, nil)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		handleError(w, r, err)
		return
	}
	w.WriteHeader(204)
}

func (a *App) allowPasskeyLogin(w http.ResponseWriter, r *http.Request) bool {
	if !a.originOK(r) {
		fail(w, r, 403, "CSRF", "Request origin is not permitted")
		return false
	}
	if err := a.limit(r.Context(), "passkey-login:"+a.clientIP(r), 60); err != nil {
		handleError(w, r, err)
		return false
	}
	return true
}

func (a *App) beginPasskeyLogin(w http.ResponseWriter, r *http.Request) {
	if !a.allowPasskeyLogin(w, r) {
		return
	}
	wa, err := a.webAuthn()
	if err != nil {
		handleError(w, r, err)
		return
	}
	options, session, err := wa.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		handleError(w, r, err)
		return
	}
	tx, err := a.DB.Begin(r.Context())
	if err != nil {
		handleError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	cookie, err := savePasskeyCeremony(r, tx, "login", "", session)
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		handleError(w, r, err)
		return
	}
	a.passkeyCookie(w, cookie, 300)
	writeJSON(w, 200, options)
}

func (a *App) finishPasskeyLogin(w http.ResponseWriter, r *http.Request) {
	if !a.allowPasskeyLogin(w, r) {
		return
	}
	session, _, err := a.consumePasskeyCeremony(w, r, "login")
	if err != nil {
		handleError(w, r, err)
		return
	}
	wa, err := a.webAuthn()
	if err != nil {
		handleError(w, r, err)
		return
	}
	tx, err := a.DB.Begin(r.Context())
	if err != nil {
		handleError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	var u passkeyUser
	var lookupErr error
	_, credential, err := wa.FinishPasskeyLogin(func(rawID, handle []byte) (webauthn.User, error) {
		u, lookupErr = loadPasskeyUser(r.Context(), tx, string(handle))
		if lookupErr != nil {
			return nil, lookupErr
		}
		for _, c := range u.Credentials {
			if bytes.Equal(c.ID, rawID) {
				return u, nil
			}
		}
		return nil, pgx.ErrNoRows
	}, session, r)
	if lookupErr != nil && lookupErr != pgx.ErrNoRows {
		handleError(w, r, lookupErr)
		return
	}
	if err != nil || credential.Authenticator.CloneWarning {
		fail(w, r, 401, "PASSKEY_INVALID", "Passkey login could not be verified")
		return
	}
	_, err = tx.Exec(r.Context(), `UPDATE passkeys SET credential=$2,last_used_at=clock_timestamp() WHERE credential_id=$1 AND user_id=$3`, credential.ID, mustJSON(credential), u.ID)
	raw, csrf := token(), token()
	if old, e := r.Cookie("seliworks_session"); e == nil && err == nil {
		_, err = tx.Exec(r.Context(), `DELETE FROM auth_sessions WHERE token_hash=$1`, digest(old.Value))
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO auth_sessions(token_hash,user_id,csrf_token,expires_at) VALUES($1,$2,$3,clock_timestamp()+interval '14 days')`, digest(raw), u.ID, csrf)
	}
	if err == nil {
		err = a.audit(r.Context(), tx, u.ID, "auth.passkey_login", u.ID, nil, nil)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		handleError(w, r, err)
		return
	}
	a.setCookie(w, raw, 14*86400)
	a.writeMe(w, r, authContext{u.User, csrf, digest(raw)})
}
