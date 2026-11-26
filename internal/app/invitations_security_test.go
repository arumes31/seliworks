//go:build integration

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const invitationTestPassword = "isolated-invitation-test-password"

// This fixture is self-contained: the published security regressions do not
// depend on the project's unpublished local test helpers.
func invitationSecurityFixture(t *testing.T) (*App, User) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Fatal("TEST_DATABASE_URL is required for invitation security regressions")
	}
	ctx := context.Background()
	base, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(base.Close)
	schema := "invitation_security_" + token()[:12]
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err = base.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS btree_gist`); err != nil {
		t.Fatal(err)
	}
	if _, err = base.Exec(ctx, `CREATE SCHEMA `+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := base.Exec(ctx, `DROP SCHEMA `+quoted+` CASCADE`); err != nil {
			t.Error(err)
		}
	})
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err = Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	a := &App{DB: db, Config: Config{AppURL: "https://test.example", DataDir: t.TempDir(), TOTPKey: bytes.Repeat([]byte{47}, 32)}}
	hash, err := hashPassword(invitationTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	var user User
	if err = db.QueryRow(ctx, `INSERT INTO users(name,email,password_hash,is_admin) VALUES('Security fixture','security@example.test',$1,true) RETURNING id::text,name,email,is_admin,is_worker,active,version`, hash).Scan(&user.ID, &user.Name, &user.Email, &user.IsAdmin, &user.IsWorker, &user.Active, &user.Version); err != nil {
		t.Fatal(err)
	}
	return a, user
}

func invitationSecurityCall(a *App, actorUser User, handler http.HandlerFunc, id string, body any) *httptest.ResponseRecorder {
	data, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	r := httptest.NewRequest(http.MethodPost, a.Config.AppURL+"/api/v1/test", bytes.NewReader(data))
	r.Header.Set("Origin", a.Config.AppURL)
	r.SetPathValue("id", id)
	r = r.WithContext(context.WithValue(r.Context(), authKey{}, authContext{User: actorUser}))
	w := httptest.NewRecorder()
	handler(w, r)
	return w
}

func invitationSecurityOK(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Fatalf("request failed: status %d: %s", w.Code, w.Body.String())
	}
	var data map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	return data
}

func invitationSecurityIssue(t *testing.T, a *App, admin User, id string) string {
	t.Helper()
	data := invitationSecurityOK(t, invitationSecurityCall(a, admin, a.invite, id, struct{}{}))
	return setupTokenFromURL(data["setup_url"].(string))
}

func invitationSecurityRedeem(a *App, raw string) *httptest.ResponseRecorder {
	return invitationSecurityCall(a, User{}, a.setupAccount, "", map[string]string{"token": raw, "password": "replacement-invitation-password"})
}

func TestInvitationSecurityRevocation(t *testing.T) {
	for _, action := range []string{"deactivate", "deactivate_pending", "password_change", "assisted_reset", "enable_mfa"} {
		t.Run(action, func(t *testing.T) {
			a, user := invitationSecurityFixture(t)
			ctx := context.Background()
			if strings.HasPrefix(action, "deactivate") {
				if _, err := a.DB.Exec(ctx, `INSERT INTO users(name,email,password_hash,is_admin) VALUES('Other admin','other@example.test','not-a-login',true)`); err != nil {
					t.Fatal(err)
				}
			}
			if action == "deactivate_pending" {
				if _, err := a.DB.Exec(ctx, `UPDATE users SET active=false WHERE id=$1`, user.ID); err != nil {
					t.Fatal(err)
				}
			}
			raw := invitationSecurityIssue(t, a, user, user.ID)
			switch action {
			case "deactivate", "deactivate_pending":
				invitationSecurityOK(t, invitationSecurityCall(a, user, a.updateWorker, user.ID, map[string]any{"version": user.Version, "active": false}))
			case "password_change":
				invitationSecurityOK(t, invitationSecurityCall(a, user, a.password, "", map[string]string{"current_password": invitationTestPassword, "new_password": "changed-invitation-password"}))
			case "assisted_reset":
				p := filepath.Join(t.TempDir(), "reset-password")
				if err := os.WriteFile(p, []byte("reset-invitation-password"), 0600); err != nil {
					t.Fatal(err)
				}
				t.Setenv("BOOTSTRAP_EMAIL", user.Email)
				t.Setenv("BOOTSTRAP_PASSWORD_FILE", p)
				if err := a.Bootstrap(ctx, true); err != nil {
					t.Fatal(err)
				}
			case "enable_mfa":
				enrollment := invitationSecurityOK(t, invitationSecurityCall(a, user, a.enrollTOTP, "", map[string]string{"password": invitationTestPassword}))
				var now time.Time
				if err := a.DB.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
					t.Fatal(err)
				}
				invitationSecurityOK(t, invitationSecurityCall(a, user, a.confirmTOTP, "", map[string]string{"code": totpAt(enrollment["secret"].(string), now.Unix()/30)}))
			}
			var beforeHash, afterHash string
			if err := a.DB.QueryRow(ctx, `SELECT password_hash FROM users WHERE id=$1`, user.ID).Scan(&beforeHash); err != nil {
				t.Fatal(err)
			}
			w := invitationSecurityRedeem(a, raw)
			if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "INVALID_INVITATION") {
				t.Fatalf("stale setup credential accepted after %s: status %d", action, w.Code)
			}
			var active, mfa bool
			if err := a.DB.QueryRow(ctx, `SELECT password_hash,active,EXISTS(SELECT 1 FROM user_totp WHERE user_id=$1 AND enabled) FROM users WHERE id=$1`, user.ID).Scan(&afterHash, &active, &mfa); err != nil {
				t.Fatal(err)
			}
			if beforeHash != afterHash || active == strings.HasPrefix(action, "deactivate") || mfa != (action == "enable_mfa") {
				t.Fatal("rejected setup changed the account's security state")
			}
			fresh := invitationSecurityIssue(t, a, user, user.ID)
			invitationSecurityOK(t, invitationSecurityRedeem(a, fresh))
			if w = invitationSecurityRedeem(a, fresh); w.Code != http.StatusUnprocessableEntity {
				t.Fatal("fresh token replay accepted")
			}
		})
	}
}

func TestInvitationSecurityPreservesOnboarding(t *testing.T) {
	a, admin := invitationSecurityFixture(t)
	created := invitationSecurityOK(t, invitationSecurityCall(a, admin, a.createWorker, "", map[string]any{"name": "Pending worker", "email": "pending@example.test", "is_worker": true, "is_admin": false}))
	id := created["id"].(string)
	raw := invitationSecurityIssue(t, a, admin, id)
	invitationSecurityOK(t, invitationSecurityCall(a, admin, a.updateWorker, id, map[string]any{"version": created["version"], "name": "Corrected worker"}))
	invitationSecurityOK(t, invitationSecurityRedeem(a, raw))
	var active bool
	if err := a.DB.QueryRow(context.Background(), `SELECT active FROM users WHERE id=$1`, id).Scan(&active); err != nil || !active {
		t.Fatalf("fresh inactive onboarding failed: %v", err)
	}
}

func TestInvitationSecurityFailedChangesKeepLink(t *testing.T) {
	for _, action := range []string{"wrong_password", "invalid_mfa", "version_conflict", "last_admin"} {
		t.Run(action, func(t *testing.T) {
			a, user := invitationSecurityFixture(t)
			raw := invitationSecurityIssue(t, a, user, user.ID)
			var w *httptest.ResponseRecorder
			switch action {
			case "wrong_password":
				w = invitationSecurityCall(a, user, a.password, "", map[string]string{"current_password": "incorrect-password", "new_password": "changed-invitation-password"})
			case "invalid_mfa":
				invitationSecurityOK(t, invitationSecurityCall(a, user, a.enrollTOTP, "", map[string]string{"password": invitationTestPassword}))
				w = invitationSecurityCall(a, user, a.confirmTOTP, "", map[string]string{"code": "not-a-code"})
			case "version_conflict":
				w = invitationSecurityCall(a, user, a.updateWorker, user.ID, map[string]any{"version": user.Version + 1, "active": false})
			case "last_admin":
				w = invitationSecurityCall(a, user, a.updateWorker, user.ID, map[string]any{"version": user.Version, "active": false})
			}
			if w.Code != 403 && w.Code != 409 && w.Code != 422 {
				t.Fatalf("invalid security transition was accepted: %d", w.Code)
			}
			invitationSecurityOK(t, invitationSecurityRedeem(a, raw))
		})
	}
}

func TestInvitationSecurityMFAWaitsForUserLock(t *testing.T) {
	a, user := invitationSecurityFixture(t)
	enrollment := invitationSecurityOK(t, invitationSecurityCall(a, user, a.enrollTOTP, "", map[string]string{"password": invitationTestPassword}))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	var id string
	if err = tx.QueryRow(ctx, `SELECT id::text FROM users WHERE id=$1 FOR UPDATE`, user.ID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		t.Fatal(err)
	}
	// Observe an actual PostgreSQL lock wait rather than relying on a sleep to
	// guess whether the concurrent handler has reached the protected operation.
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		result <- invitationSecurityCall(a, user, a.confirmTOTP, "", map[string]string{"code": totpAt(enrollment["secret"].(string), now.Unix()/30)})
	}()
	t.Cleanup(func() { <-result })
	for {
		var waiting bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1::int=ANY(pg_blocking_pids(pid)))`, tx.Conn().PgConn().PID()).Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case w := <-result:
			result <- w
			t.Fatalf("MFA confirmation bypassed the user lock: %d", w.Code)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	w := <-result
	result <- w
	invitationSecurityOK(t, w)
}

func TestInvitationSecurityConcurrentDeactivation(t *testing.T) {
	a, admin := invitationSecurityFixture(t)
	created := invitationSecurityOK(t, invitationSecurityCall(a, admin, a.createWorker, "", map[string]any{"name": "Concurrent worker", "email": "race@example.test", "is_worker": true, "is_admin": false, "password": invitationTestPassword}))
	id := created["id"].(string)
	raw := invitationSecurityIssue(t, a, admin, id)
	var wg sync.WaitGroup
	var deactivation, redemption *httptest.ResponseRecorder
	wg.Go(func() {
		deactivation = invitationSecurityCall(a, admin, a.updateWorker, id, map[string]any{"version": created["version"], "active": false})
	})
	wg.Go(func() { redemption = invitationSecurityRedeem(a, raw) })
	wg.Wait()
	switch {
	case deactivation.Code == http.StatusOK && redemption.Code == http.StatusUnprocessableEntity:
	case deactivation.Code == http.StatusConflict && redemption.Code == http.StatusOK:
	default:
		t.Fatalf("invalid race outcome: deactivation=%d redemption=%d", deactivation.Code, redemption.Code)
	}
	// If setup wins it increments the version. Apply the administrator's intent
	// with the current version, as the UI does after an optimistic conflict.
	var version int64
	if err := a.DB.QueryRow(context.Background(), `SELECT version FROM users WHERE id=$1`, id).Scan(&version); err != nil {
		t.Fatal(err)
	}
	invitationSecurityOK(t, invitationSecurityCall(a, admin, a.updateWorker, id, map[string]any{"version": version, "active": false}))
	if w := invitationSecurityRedeem(a, raw); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("token survived completed revocation: %d", w.Code)
	}
	var active bool
	if err := a.DB.QueryRow(context.Background(), `SELECT active FROM users WHERE id=$1`, id).Scan(&active); err != nil || active {
		t.Fatal(fmt.Errorf("concurrent setup restored deactivated user: %v", err))
	}
}
