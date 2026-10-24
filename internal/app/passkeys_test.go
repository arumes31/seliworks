//go:build integration

package app

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
)

func TestPasskeysBrowser(t *testing.T) {
	if os.Getenv("SELIWORKS_BROWSER_TEST") != "1" {
		t.Skip("set SELIWORKS_BROWSER_TEST=1 with built frontend and Chromium")
	}
	a, u := invitationSecurityFixture(t)
	if _, err := a.DB.Exec(context.Background(), `UPDATE users SET language='de',is_admin=false WHERE id=$1`, u.ID); err != nil {
		t.Fatal(err)
	}
	static, err := filepath.Abs("../../web/dist")
	if err != nil {
		t.Fatal(err)
	}
	a.Config.StaticDir = static
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	a.Config.AppURL = "http://localhost:" + strings.Split(listener.Addr().String(), ":")[1]
	server := httptest.NewUnstartedServer(a.Handler())
	server.Listener.Close()
	server.Listener = listener
	server.Start()
	defer server.Close()
	command := exec.Command("node", "test/passkeys-live.browser.mjs")
	command.Dir = "../../web"
	command.Env = append(os.Environ(), "SELIWORKS_TEST_URL="+a.Config.AppURL, "SELIWORKS_TEST_EMAIL="+u.Email, "SELIWORKS_TEST_PASSWORD="+invitationTestPassword)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("full browser flow: %v\n%s", err, output)
	}
	t.Log(strings.TrimSpace(string(output)))
}

type passkeyTestClient struct {
	t       *testing.T
	a       *App
	cookies map[string]*http.Cookie
	csrf    string
}

func newPasskeyTestClient(t *testing.T, a *App, u User) *passkeyTestClient {
	raw := token()
	csrf := "passkey-test"
	if _, err := a.DB.Exec(context.Background(), `INSERT INTO auth_sessions(token_hash,user_id,csrf_token,expires_at) VALUES($1,$2,$3,clock_timestamp()+interval '1 hour')`, digest(raw), u.ID, csrf); err != nil {
		t.Fatal(err)
	}
	return &passkeyTestClient{t, a, map[string]*http.Cookie{"seliworks_session": {Name: "seliworks_session", Value: raw}}, csrf}
}
func (c *passkeyTestClient) call(method, path string, body any) *httptest.ResponseRecorder {
	c.t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		c.t.Fatal(err)
	}
	r := httptest.NewRequest(method, c.a.Config.AppURL+"/api/v1"+path, bytes.NewReader(data))
	r.Header.Set("Origin", c.a.Config.AppURL)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", c.csrf)
	for _, cookie := range c.cookies {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	c.a.Handler().ServeHTTP(w, r)
	for _, cookie := range w.Result().Cookies() {
		if cookie.MaxAge < 0 {
			delete(c.cookies, cookie.Name)
		} else {
			c.cookies[cookie.Name] = cookie
		}
	}
	if w.Code == 200 && (path == "/auth/passkeys/finish" || path == "/auth/login") {
		var auth struct {
			CSRF string `json:"csrf_token"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &auth)
		c.csrf = auth.CSRF
	}
	return w
}
func passkeyStatus(t *testing.T, w *httptest.ResponseRecorder, status int) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("want %d got %d: %s", status, w.Code, w.Body.String())
	}
}
func passkeyOptions(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	passkeyStatus(t, w, 200)
	var v struct {
		PublicKey map[string]any `json:"publicKey"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	return v.PublicKey
}

type softwarePasskey struct {
	id      []byte
	private *ecdsa.PrivateKey
	handle  string
}

func newSoftwarePasskey(t *testing.T, handle string) *softwarePasskey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return &softwarePasskey{[]byte(token()[:32]), key, handle}
}
func passkeyB64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
func (k *softwarePasskey) response(t *testing.T, challenge, origin, rp string, register, uv bool, counter uint32) map[string]any {
	t.Helper()
	kind := "webauthn.get"
	if register {
		kind = "webauthn.create"
	}
	client, _ := json.Marshal(map[string]any{"type": kind, "challenge": challenge, "origin": origin, "crossOrigin": false})
	hash := sha256.Sum256([]byte(rp))
	auth := append([]byte{}, hash[:]...)
	flags := byte(1)
	if uv {
		flags |= 4
	}
	if register {
		flags |= 64
	}
	auth = append(auth, flags)
	auth = binary.BigEndian.AppendUint32(auth, counter)
	response := map[string]any{"clientDataJSON": passkeyB64(client)}
	if register {
		cose, err := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: k.private.X.FillBytes(make([]byte, 32)), -3: k.private.Y.FillBytes(make([]byte, 32))})
		if err != nil {
			t.Fatal(err)
		}
		auth = append(auth, make([]byte, 16)...)
		auth = binary.BigEndian.AppendUint16(auth, uint16(len(k.id)))
		auth = append(auth, k.id...)
		auth = append(auth, cose...)
		att, err := cbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": auth})
		if err != nil {
			t.Fatal(err)
		}
		response["attestationObject"] = passkeyB64(att)
		response["transports"] = []string{"internal"}
	} else {
		clientHash := sha256.Sum256(client)
		signed := sha256.Sum256(append(auth, clientHash[:]...))
		sig, err := ecdsa.SignASN1(rand.Reader, k.private, signed[:])
		if err != nil {
			t.Fatal(err)
		}
		response["authenticatorData"] = passkeyB64(auth)
		response["signature"] = passkeyB64(sig)
		response["userHandle"] = passkeyB64([]byte(k.handle))
	}
	return map[string]any{"id": passkeyB64(k.id), "rawId": passkeyB64(k.id), "type": "public-key", "response": response, "clientExtensionResults": map[string]any{}}
}
func registerSoftwarePasskey(t *testing.T, c *passkeyTestClient, k *softwarePasskey, name string) string {
	t.Helper()
	opts := passkeyOptions(t, c.call("POST", "/me/passkeys/register/begin", map[string]string{"name": name, "password": invitationTestPassword}))
	selection := opts["authenticatorSelection"].(map[string]any)
	if selection["userVerification"] != "required" || selection["residentKey"] != "required" {
		t.Fatal(selection)
	}
	w := c.call("POST", "/me/passkeys/register/finish", k.response(t, opts["challenge"].(string), c.a.Config.AppURL, "test.example", true, true, 0))
	passkeyStatus(t, w, 201)
	var result map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	return result["id"]
}

func TestPasskeysLifecycleAndIsolation(t *testing.T) {
	a, u := invitationSecurityFixture(t)
	c := newPasskeyTestClient(t, a, u)
	first := newSoftwarePasskey(t, u.ID)
	second := newSoftwarePasskey(t, u.ID)
	passkeyStatus(t, c.call("POST", "/me/passkeys/reminder", map[string]int{"days": 7}), 200)
	firstID := registerSoftwarePasskey(t, c, first, "Phone")
	me := invitationSecurityOK(t, c.call("GET", "/me", nil))
	if me["user"].(map[string]any)["passkey_remind_after"] != nil {
		t.Fatal("registration did not clear reminder")
	}
	registerSoftwarePasskey(t, c, second, "Security key")
	list := c.call("GET", "/me/passkeys", nil)
	passkeyStatus(t, list, 200)
	if bytes.Contains(list.Body.Bytes(), []byte("credential")) || !bytes.Contains(list.Body.Bytes(), []byte(`"total":2`)) {
		t.Fatal(list.Body.String())
	}
	passkeyStatus(t, c.call("POST", "/auth/logout", map[string]any{}), 200)
	for i, key := range []*softwarePasskey{first, second} {
		opts := passkeyOptions(t, c.call("POST", "/auth/passkeys/begin", map[string]any{}))
		if opts["userVerification"] != "required" {
			t.Fatal(opts)
		}
		response := key.response(t, opts["challenge"].(string), a.Config.AppURL, "test.example", false, true, 1)
		cookie := *c.cookies["seliworks_passkey"]
		w := c.call("POST", "/auth/passkeys/finish", response)
		passkeyStatus(t, w, 200)
		if !bytes.Contains(w.Body.Bytes(), []byte(`"passkey_count":2`)) {
			t.Fatal(w.Body.String())
		}
		c.cookies["seliworks_passkey"] = &cookie
		passkeyStatus(t, c.call("POST", "/auth/passkeys/finish", response), 401)
		if i == 0 {
			passkeyStatus(t, c.call("POST", "/auth/logout", map[string]any{}), 200)
		}
	}
	// Even an authenticated user must reauthenticate to manage credentials.
	passkeyStatus(t, c.call("DELETE", "/me/passkeys/"+firstID, map[string]string{"password": "wrong"}), 403)
	var other string
	if err := a.DB.QueryRow(context.Background(), `INSERT INTO users(name,email,password_hash) VALUES('Other','other@example.test','unused') RETURNING id::text`).Scan(&other); err != nil {
		t.Fatal(err)
	}
	outsider := newPasskeyTestClient(t, a, User{ID: other})
	passkeyStatus(t, outsider.call("GET", "/me/passkeys", nil), 200)
	// Copying a registration ceremony cookie to another authenticated session is rejected.
	opts := passkeyOptions(t, c.call("POST", "/me/passkeys/register/begin", map[string]string{"name": "Third", "password": invitationTestPassword}))
	outsider.cookies["seliworks_passkey"] = c.cookies["seliworks_passkey"]
	passkeyStatus(t, outsider.call("POST", "/me/passkeys/register/finish", first.response(t, opts["challenge"].(string), a.Config.AppURL, "test.example", true, true, 0)), 401)
	passkeyStatus(t, c.call("DELETE", "/me/passkeys/"+firstID, map[string]string{"password": invitationTestPassword}), 204)
	opts = passkeyOptions(t, c.call("POST", "/auth/passkeys/begin", map[string]any{}))
	passkeyStatus(t, c.call("POST", "/auth/passkeys/finish", first.response(t, opts["challenge"].(string), a.Config.AppURL, "test.example", false, true, 2)), 401)
}

func TestPasskeysRejectInvalidAssertions(t *testing.T) {
	a, u := invitationSecurityFixture(t)
	c := newPasskeyTestClient(t, a, u)
	key := newSoftwarePasskey(t, u.ID)
	registerSoftwarePasskey(t, c, key, "Phone")
	for _, scenario := range []string{"origin", "rp", "challenge", "uv", "signature", "handle", "expired", "disabled"} {
		t.Run(scenario, func(t *testing.T) {
			opts := passkeyOptions(t, c.call("POST", "/auth/passkeys/begin", map[string]any{}))
			challenge := opts["challenge"].(string)
			origin := a.Config.AppURL
			rp := "test.example"
			uv := true
			switch scenario {
			case "origin":
				origin = "https://evil.example"
			case "rp":
				rp = "evil.example"
			case "challenge":
				challenge = passkeyB64([]byte(token()))
			case "uv":
				uv = false
			case "expired":
				_, err := a.DB.Exec(context.Background(), `UPDATE passkey_ceremonies SET expires_at=clock_timestamp()-interval '1 minute'`)
				if err != nil {
					t.Fatal(err)
				}
			case "disabled":
				_, err := a.DB.Exec(context.Background(), `UPDATE users SET active=false WHERE id=$1`, u.ID)
				if err != nil {
					t.Fatal(err)
				}
			}
			response := key.response(t, challenge, origin, rp, false, uv, 1)
			if scenario == "signature" {
				response["response"].(map[string]any)["signature"] = passkeyB64([]byte("invalid"))
			}
			if scenario == "handle" {
				response["response"].(map[string]any)["userHandle"] = passkeyB64([]byte("other"))
			}
			passkeyStatus(t, c.call("POST", "/auth/passkeys/finish", response), 401)
		})
	}
}

func TestPasskeysRegistrationGuards(t *testing.T) {
	a, u := invitationSecurityFixture(t)
	c := newPasskeyTestClient(t, a, u)
	key := newSoftwarePasskey(t, u.ID)
	passkeyStatus(t, c.call("POST", "/me/passkeys/register/begin", map[string]string{"name": "Phone", "password": "wrong"}), 403)
	opts := passkeyOptions(t, c.call("POST", "/me/passkeys/register/begin", map[string]string{"name": "Phone", "password": invitationTestPassword}))
	passkeyStatus(t, c.call("POST", "/me/passkeys/register/finish", key.response(t, opts["challenge"].(string), a.Config.AppURL, "test.example", true, false, 0)), 422)
	opts = passkeyOptions(t, c.call("POST", "/me/passkeys/register/begin", map[string]string{"name": "Phone", "password": invitationTestPassword}))
	if _, err := a.DB.Exec(context.Background(), `DELETE FROM auth_sessions WHERE user_id=$1`, u.ID); err != nil {
		t.Fatal(err)
	}
	passkeyStatus(t, c.call("POST", "/me/passkeys/register/finish", key.response(t, opts["challenge"].(string), a.Config.AppURL, "test.example", true, true, 0)), 401)
	var count int
	if err := a.DB.QueryRow(context.Background(), `SELECT count(*) FROM passkeys`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("unexpected credential: %d %v", count, err)
	}
}

func TestPasskeysAccountRecoveryAndMFA(t *testing.T) {
	for _, mode := range []string{"invitation", "operator_reset", "mfa"} {
		t.Run(mode, func(t *testing.T) {
			a, u := invitationSecurityFixture(t)
			c := newPasskeyTestClient(t, a, u)
			key := newSoftwarePasskey(t, u.ID)
			id := registerSoftwarePasskey(t, c, key, "Existing device")
			switch mode {
			case "invitation":
				raw := invitationSecurityIssue(t, a, u, u.ID)
				invitationSecurityOK(t, invitationSecurityRedeem(a, raw))
			case "operator_reset":
				file := filepath.Join(t.TempDir(), "password")
				if err := os.WriteFile(file, []byte(invitationTestPassword), 0600); err != nil {
					t.Fatal(err)
				}
				t.Setenv("BOOTSTRAP_EMAIL", u.Email)
				t.Setenv("BOOTSTRAP_PASSWORD_FILE", file)
				if err := a.Bootstrap(context.Background(), true); err != nil {
					t.Fatal(err)
				}
			case "mfa":
				const secret = "JBSWY3DPEHPK3PXP"
				encrypted, err := sealSecret(a.Config.TOTPKey, secret)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = a.DB.Exec(context.Background(), `INSERT INTO user_totp(user_id,secret,enabled) VALUES($1,$2,true)`, u.ID, encrypted); err != nil {
					t.Fatal(err)
				}
				passkeyStatus(t, c.call("POST", "/me/passkeys/register/begin", map[string]string{"name": "New", "password": invitationTestPassword}), 401)
				passkeyStatus(t, c.call("DELETE", "/me/passkeys/"+id, map[string]string{"password": invitationTestPassword}), 401)
				var now time.Time
				if err = a.DB.QueryRow(context.Background(), `SELECT clock_timestamp()`).Scan(&now); err != nil {
					t.Fatal(err)
				}
				passkeyStatus(t, c.call("DELETE", "/me/passkeys/"+id, map[string]string{"password": invitationTestPassword, "totp_code": totpAt(secret, now.Unix()/30)}), 204)
			}
			var count int
			if err := a.DB.QueryRow(context.Background(), `SELECT count(*) FROM passkeys WHERE user_id=$1`, u.ID).Scan(&count); err != nil || count != 0 {
				t.Fatalf("passkeys survived recovery: %d %v", count, err)
			}
			opts := passkeyOptions(t, c.call("POST", "/auth/passkeys/begin", map[string]any{}))
			passkeyStatus(t, c.call("POST", "/auth/passkeys/finish", key.response(t, opts["challenge"].(string), a.Config.AppURL, "test.example", false, true, 1)), 401)
		})
	}
}

func TestPasskeysReminderPersistenceAndValidation(t *testing.T) {
	a, user := invitationSecurityFixture(t)
	for _, days := range []int{3, 4, 5, 6, 7} {
		var before, after time.Time
		if err := a.DB.QueryRow(context.Background(), `SELECT clock_timestamp()`).Scan(&before); err != nil {
			t.Fatal(err)
		}
		response := deleteWorkRequest(t, a, user, "POST", "/me/passkeys/reminder", map[string]any{"days": days}, true)
		me := invitationSecurityOK(t, response)
		until, err := time.Parse(time.RFC3339Nano, me["user"].(map[string]any)["passkey_remind_after"].(string))
		if clockErr := a.DB.QueryRow(context.Background(), `SELECT clock_timestamp()`).Scan(&after); clockErr != nil {
			t.Fatal(clockErr)
		}
		if err != nil || until.Before(before.Add(time.Duration(days)*24*time.Hour)) || until.After(after.Add(time.Duration(days)*24*time.Hour)) {
			t.Fatalf("incorrect reminder date: %s %v", until, err)
		}
		// A new authenticated session (another device) receives the persisted value.
		reloaded := invitationSecurityOK(t, deleteWorkRequest(t, a, user, "GET", "/me", nil, true))
		if reloaded["user"].(map[string]any)["passkey_remind_after"] != me["user"].(map[string]any)["passkey_remind_after"] {
			t.Fatal("reminder not persisted across sessions")
		}
	}
	for _, days := range []any{0, 2, 8, -1, 3.5, "3"} {
		w := deleteWorkRequest(t, a, user, "POST", "/me/passkeys/reminder", map[string]any{"days": days}, true)
		if w.Code != 422 {
			t.Fatalf("invalid days %v: %d", days, w.Code)
		}
	}
	for _, tc := range []struct {
		user   User
		csrf   bool
		status int
	}{{user, false, 403}, {User{}, true, 401}} {
		w := deleteWorkRequest(t, a, tc.user, "POST", "/me/passkeys/reminder", map[string]any{"days": 3}, tc.csrf)
		if w.Code != tc.status {
			t.Fatalf("authorization: %d", w.Code)
		}
	}
	other := farmWorker(t, a, "reminder-other")
	otherMe := invitationSecurityOK(t, deleteWorkRequest(t, a, other, "GET", "/me", nil, true))
	if otherMe["user"].(map[string]any)["passkey_remind_after"] != nil {
		t.Fatal("reminder leaked to another account")
	}
}
