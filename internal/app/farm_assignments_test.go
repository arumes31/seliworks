//go:build integration

package app

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func farmWorker(t *testing.T, a *App, name string) User {
	t.Helper()
	var u User
	if err := a.DB.QueryRow(context.Background(), `INSERT INTO users(name,email,password_hash) VALUES($1,$1||'@example.test','setup-required') RETURNING id::text`, name).Scan(&u.ID); err != nil {
		t.Fatal(err)
	}
	u.IsWorker = true
	return u
}

func farmStart(t *testing.T, a *App, u User, id string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", a.Config.AppURL+"/api/v1/work-sessions/start", strings.NewReader(`{"farm_id":"`+id+`"}`))
	r.Header.Set("Idempotency-Key", token())
	r = r.WithContext(context.WithValue(r.Context(), authKey{}, authContext{User: u}))
	w := httptest.NewRecorder()
	a.start(w, r)
	return w
}

func TestFarmAssignmentsDefaultIncludesFutureWorkers(t *testing.T) {
	a, admin := invitationSecurityFixture(t)
	current := farmWorker(t, a, "current")
	farm := invitationSecurityOK(t, deleteWorkRequest(t, a, admin, "POST", "/admin/farms", map[string]any{"name": "Everyone"}, true))
	if farm["assign_all_workers"] != true {
		t.Fatalf("default assignment: %v", farm)
	}
	id := farm["id"].(string)
	future := farmWorker(t, a, "future")
	for _, u := range []User{current, future} {
		w := deleteWorkRequest(t, a, u, "GET", "/farms", nil, true)
		if w.Code != 200 || !strings.Contains(w.Body.String(), id) || strings.Contains(w.Body.String(), "worker_ids") || strings.Contains(w.Body.String(), "email") {
			t.Fatalf("worker farm projection: %s", w.Body.String())
		}
		invitationSecurityOK(t, farmStart(t, a, u, id))
	}
	// Removing explicit memberships cannot revoke an all-workers farm.
	invitationSecurityOK(t, deleteWorkRequest(t, a, admin, "PUT", "/admin/workers/"+future.ID+"/farms", map[string]any{"farm_ids": []string{}}, true))
	w := deleteWorkRequest(t, a, admin, "GET", "/admin/workers", nil, true)
	if !strings.Contains(w.Body.String(), id) {
		t.Fatalf("implicit assignments missing: %s", w.Body.String())
	}
}

func TestFarmAssignmentsSelectedAndVersionedChanges(t *testing.T) {
	a, admin := invitationSecurityFixture(t)
	selected := farmWorker(t, a, "selected")
	other := farmWorker(t, a, "other")
	farm := invitationSecurityOK(t, deleteWorkRequest(t, a, admin, "POST", "/admin/farms", map[string]any{"name": "Selected", "assign_all_workers": false, "worker_ids": []string{selected.ID}}, true))
	id := farm["id"].(string)
	if w := farmStart(t, a, other, id); w.Code != 403 {
		t.Fatalf("unassigned start: %d %s", w.Code, w.Body.String())
	}
	invitationSecurityOK(t, farmStart(t, a, selected, id))
	// An older client updating billing fields must not change assignments.
	farm = invitationSecurityOK(t, deleteWorkRequest(t, a, admin, "PATCH", "/admin/farms/"+id, map[string]any{"name": "Renamed", "version": farm["version"]}, true))
	if farm["assign_all_workers"] != false || len(farm["worker_ids"].([]any)) != 1 {
		t.Fatalf("assignments lost: %v", farm)
	}
	// Worker-side assignment changes invalidate a stale farm editor.
	invitationSecurityOK(t, deleteWorkRequest(t, a, admin, "PUT", "/admin/workers/"+other.ID+"/farms", map[string]any{"farm_ids": []string{id}}, true))
	if w := deleteWorkRequest(t, a, admin, "PATCH", "/admin/farms/"+id, map[string]any{"name": "Stale", "version": farm["version"], "assign_all_workers": true}, true); w.Code != 409 {
		t.Fatalf("stale farm: %d %s", w.Code, w.Body.String())
	}
	var version int64
	if err := a.DB.QueryRow(context.Background(), `SELECT version FROM farms WHERE id=$1`, id).Scan(&version); err != nil {
		t.Fatal(err)
	}
	farm = invitationSecurityOK(t, deleteWorkRequest(t, a, admin, "PATCH", "/admin/farms/"+id, map[string]any{"name": "Everyone now", "version": version, "assign_all_workers": true}, true))
	farm = invitationSecurityOK(t, deleteWorkRequest(t, a, admin, "PATCH", "/admin/farms/"+id, map[string]any{"name": "Restricted again", "version": farm["version"], "assign_all_workers": false, "worker_ids": []string{selected.ID}}, true))
	if w := farmStart(t, a, other, id); w.Code != 403 {
		t.Fatalf("revoked start: %d %s", w.Code, w.Body.String())
	}
	if w := deleteWorkRequest(t, a, other, "GET", "/farms", nil, true); strings.Contains(w.Body.String(), id) {
		t.Fatal("revoked farm still listed")
	}
}

func TestFarmAssignmentsValidationAndAuthorization(t *testing.T) {
	a, admin := invitationSecurityFixture(t)
	worker := farmWorker(t, a, "worker")
	for _, ids := range [][]string{{"invalid"}, {"00000000-0000-0000-0000-000000000000"}, {worker.ID, worker.ID}} {
		w := deleteWorkRequest(t, a, admin, "POST", "/admin/farms", map[string]any{"name": "Invalid", "assign_all_workers": false, "worker_ids": ids}, true)
		if w.Code != 422 {
			t.Fatalf("invalid selection %v: %d %s", ids, w.Code, w.Body.String())
		}
	}
	for _, tc := range []struct {
		user   User
		csrf   bool
		status int
	}{{worker, true, 403}, {admin, false, 403}, {User{}, true, 401}} {
		w := deleteWorkRequest(t, a, tc.user, "POST", "/admin/farms", map[string]any{"name": "Forbidden"}, tc.csrf)
		if w.Code != tc.status {
			t.Fatalf("authorization: %d %s", w.Code, w.Body.String())
		}
	}
	var count int
	if err := a.DB.QueryRow(context.Background(), `SELECT count(*) FROM farms`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial farm writes: %d, %v", count, err)
	}
	// Existing/default SQL-created farms remain restricted after migration.
	var legacy string
	if err := a.DB.QueryRow(context.Background(), `INSERT INTO farms(name) VALUES('Legacy') RETURNING id::text`).Scan(&legacy); err != nil {
		t.Fatal(err)
	}
	w := deleteWorkRequest(t, a, worker, "GET", "/farms", nil, true)
	var list struct{ Items []json.RawMessage }
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list.Items) != 0 {
		t.Fatalf("legacy access expanded: %s", w.Body.String())
	}
}

func TestFarmAssignmentsEmployeeEditorConflictAndNoOp(t *testing.T) {
	a, admin := invitationSecurityFixture(t)
	u := farmWorker(t, a, "worker")
	ctx := context.Background()
	farm := invitationSecurityOK(t, deleteWorkRequest(t, a, admin, "POST", "/admin/farms", map[string]any{"name": "Everyone"}, true))
	id := farm["id"].(string)
	var revision string
	if err := a.DB.QueryRow(ctx, `SELECT `+workerAssignmentRevision+` FROM users u WHERE id=$1`, u.ID).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	// The effective farm set stays the same, but its mode has changed.
	farm = invitationSecurityOK(t, deleteWorkRequest(t, a, admin, "PATCH", "/admin/farms/"+id, map[string]any{"name": "Selected", "version": farm["version"], "assign_all_workers": false, "worker_ids": []string{u.ID}}, true))
	w := deleteWorkRequest(t, a, admin, "PUT", "/admin/workers/"+u.ID+"/farms", map[string]any{"farm_ids": []string{}, "expected_assignment_revision": revision}, true)
	if w.Code != 409 {
		t.Fatalf("stale employee editor: %d %s", w.Code, w.Body.String())
	}
	if err := a.DB.QueryRow(ctx, `SELECT `+workerAssignmentRevision+` FROM users u WHERE id=$1`, u.ID).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	invitationSecurityOK(t, deleteWorkRequest(t, a, admin, "PUT", "/admin/workers/"+u.ID+"/farms", map[string]any{"farm_ids": []string{id}, "expected_assignment_revision": revision}, true))
	var version int64
	if err := a.DB.QueryRow(ctx, `SELECT version FROM farms WHERE id=$1`, id).Scan(&version); err != nil || version != int64(farm["version"].(float64)) {
		t.Fatalf("no-op bumped farm: %d %v", version, err)
	}
}

func TestFarmAssignmentsStartWaitsForRevocation(t *testing.T) {
	a, admin := invitationSecurityFixture(t)
	u := farmWorker(t, a, "worker")
	farm := invitationSecurityOK(t, deleteWorkRequest(t, a, admin, "POST", "/admin/farms", map[string]any{"name": "Everyone"}, true))
	id := farm["id"].(string)
	ctx := context.Background()
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err = lockFarmAssignments(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE farms SET assign_all_workers=false WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	finished := make(chan *httptest.ResponseRecorder, 1)
	go func() { finished <- farmStart(t, a, u, id) }()
	select {
	case w := <-finished:
		t.Fatalf("start bypassed assignment lock: %d", w.Code)
	case <-time.After(100 * time.Millisecond):
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case w := <-finished:
		if w.Code != 403 {
			t.Fatalf("start ignored committed revocation: %d %s", w.Code, w.Body.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("start blocked after commit")
	}
}

func TestFarmAssignmentsMigrationPreservesExistingAccess(t *testing.T) {
	a, admin := invitationSecurityFixture(t)
	ctx := context.Background()
	var id string
	if err := a.DB.QueryRow(ctx, `INSERT INTO farms(name) VALUES('Existing') RETURNING id::text`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DB.Exec(ctx, `INSERT INTO worker_farms(worker_id,farm_id) VALUES($1,$2)`, admin.ID, id); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DB.Exec(ctx, `ALTER TABLE farms DROP COLUMN assign_all_workers; DROP INDEX worker_farms_farm; DELETE FROM schema_migrations WHERE name='migrations/015_farm_assignments.sql'`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, a.DB); err != nil {
		t.Fatal(err)
	}
	var valid bool
	if err := a.DB.QueryRow(ctx, `SELECT NOT assign_all_workers AND EXISTS(SELECT 1 FROM worker_farms WHERE farm_id=$1 AND worker_id=$2) FROM farms WHERE id=$1`, id, admin.ID).Scan(&valid); err != nil || !valid {
		t.Fatalf("migration changed access: %v %v", valid, err)
	}
}

func TestFarmAssignmentsAdministrators(t *testing.T) {
	a, admin := invitationSecurityFixture(t)
	ctx := context.Background()
	other := farmWorker(t, a, "other-admin")
	if _, err := a.DB.Exec(ctx, `UPDATE users SET is_admin=true,is_worker=false WHERE id=$1`, other.ID); err != nil {
		t.Fatal(err)
	}
	other.IsAdmin, other.IsWorker = true, false
	farm := invitationSecurityOK(t, deleteWorkRequest(t, a, admin, "POST", "/admin/farms", map[string]any{"name": "Selected admin", "assign_all_workers": false, "worker_ids": []string{other.ID}}, true))
	id := farm["id"].(string)
	var revision string
	if err := a.DB.QueryRow(ctx, `SELECT `+workerAssignmentRevision+` FROM users u WHERE id=$1`, other.ID).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	list := invitationSecurityOK(t, deleteWorkRequest(t, a, admin, "GET", "/admin/workers", nil, true))
	found := false
	for _, item := range list["items"].([]any) {
		u := item.(map[string]any)
		found = found || u["id"] == other.ID
		if u["id"] == other.ID && (len(u["farm_ids"].([]any)) != 1 || u["farm_ids"].([]any)[0] != id) {
			t.Fatalf("admin assignment missing: %v", u)
		}
	}
	if !found {
		t.Fatal("assigned admin missing from user list")
	}
	invitationSecurityOK(t, deleteWorkRequest(t, a, admin, "PUT", "/admin/workers/"+other.ID+"/farms", map[string]any{"farm_ids": []string{}, "expected_assignment_revision": revision}, true))
	if w := deleteWorkRequest(t, a, admin, "PUT", "/admin/workers/"+other.ID+"/farms", map[string]any{"farm_ids": []string{id}, "expected_assignment_revision": revision}, true); w.Code != 409 {
		t.Fatalf("stale admin assignment accepted: %d %s", w.Code, w.Body.String())
	}
	invitationSecurityOK(t, deleteWorkRequest(t, a, admin, "PUT", "/admin/workers/"+other.ID+"/farms", map[string]any{"farm_ids": []string{id}}, true))
	if w := farmStart(t, a, other, id); w.Code != 403 {
		t.Fatalf("assignment granted worker permission: %d", w.Code)
	}
	everyone := invitationSecurityOK(t, deleteWorkRequest(t, a, admin, "POST", "/admin/farms", map[string]any{"name": "Everyone"}, true))
	future := invitationSecurityOK(t, deleteWorkRequest(t, a, admin, "POST", "/admin/workers", map[string]any{"name": "Future admin", "email": "future-admin@example.test", "is_admin": true, "is_worker": false}, true))
	list = invitationSecurityOK(t, deleteWorkRequest(t, a, admin, "GET", "/admin/workers", nil, true))
	found = false
	for _, item := range list["items"].([]any) {
		u := item.(map[string]any)
		found = found || u["id"] == future["id"]
		if u["id"] == future["id"] && (len(u["farm_ids"].([]any)) != 1 || u["farm_ids"].([]any)[0] != everyone["id"]) {
			t.Fatalf("future admin inheritance missing: %v", u)
		}
	}
	if !found {
		t.Fatal("future admin missing from user list")
	}
	worker := farmWorker(t, a, "ordinary-worker")
	if w := deleteWorkRequest(t, a, worker, "PUT", "/admin/workers/"+other.ID+"/farms", map[string]any{"farm_ids": []string{}}, true); w.Code != 403 {
		t.Fatalf("worker can assign admin: %d", w.Code)
	}
}

func TestFarmAssignmentsWorkTransferPreservesEvidence(t *testing.T) {
	for _, former := range []bool{false, true} {
		t.Run(map[bool]string{false: "worker", true: "former-worker-admin"}[former], func(t *testing.T) {
			a, admin := invitationSecurityFixture(t)
			ctx := context.Background()
			id, original := deleteWorkEntry(t, a, admin, "APPROVED")
			if former {
				if _, err := a.DB.Exec(ctx, `UPDATE users SET is_worker=false WHERE id=$1`, admin.ID); err != nil {
					t.Fatal(err)
				}
			}
			farm := invitationSecurityOK(t, deleteWorkRequest(t, a, admin, "POST", "/admin/farms", map[string]any{"name": "Transfer destination", "assign_all_workers": false}, true))
			var activity string
			if err := a.DB.QueryRow(ctx, `SELECT activity_id::text FROM work_sessions WHERE id=$1`, id).Scan(&activity); err != nil {
				t.Fatal(err)
			}
			body := map[string]any{"version": 1, "worker_id": admin.ID, "farm_id": farm["id"], "activity_id": activity, "effective_start": "2026-01-02T09:00:00Z", "effective_stop": "2026-01-02T10:00:00Z", "description": "Transferred work", "day_type": "REGULAR", "reason": "Correct wrong farm"}
			saved := invitationSecurityOK(t, deleteWorkRequest(t, a, admin, "PATCH", "/admin/work-sessions/"+id, body, true))
			if saved["farm_id"] != farm["id"] || saved["state"] != "SUBMITTED" || saved["worker_id"] != admin.ID {
				t.Fatalf("transfer failed: %v", saved)
			}
			var recorded string
			if err := a.DB.QueryRow(ctx, `SELECT recorded_farm_id::text FROM work_sessions WHERE id=$1`, id).Scan(&recorded); err != nil || recorded != original {
				t.Fatalf("original evidence lost: %s %v", recorded, err)
			}
			if w := deleteWorkRequest(t, a, admin, "PATCH", "/admin/work-sessions/"+id, body, true); w.Code != 409 {
				t.Fatalf("stale transfer: %d", w.Code)
			}
			other := farmWorker(t, a, "admin-only-target")
			if _, err := a.DB.Exec(ctx, `UPDATE users SET is_admin=true,is_worker=false WHERE id=$1`, other.ID); err != nil {
				t.Fatal(err)
			}
			body["version"], body["worker_id"] = saved["version"], other.ID
			if w := deleteWorkRequest(t, a, admin, "PATCH", "/admin/work-sessions/"+id, body, true); w.Code != 422 {
				t.Fatalf("new non-worker owner accepted: %d", w.Code)
			}
			var invoice string
			if err := a.DB.QueryRow(ctx, `INSERT INTO invoices(farm_id,state,number,from_date,to_date,period_start,period_stop,timezone,currency,issuer,customer,net_cents,tax_cents,total_cents,created_by,issued_at,due_on) VALUES($1,'issued','TRANSFER-1','2026-01-01','2026-02-01','2026-01-01Z','2026-02-01Z','Europe/Vienna','EUR','{}','{}',100,0,100,$2,clock_timestamp(),'2026-02-15') RETURNING id::text`, farm["id"], admin.ID).Scan(&invoice); err != nil {
				t.Fatal(err)
			}
			if _, err := a.DB.Exec(ctx, `INSERT INTO invoice_allocations(invoice_id,session_id,source_version,started_at,stopped_at,active,net_us,net_cents,snapshot) VALUES($1,$2,2,'2026-01-02T09:00:00Z','2026-01-02T10:00:00Z',true,3600000000,100,'{}')`, invoice, id); err != nil {
				t.Fatal(err)
			}
			body["worker_id"], body["farm_id"] = admin.ID, original
			if w := deleteWorkRequest(t, a, admin, "PATCH", "/admin/work-sessions/"+id, body, true); w.Code != 409 {
				t.Fatalf("billed work moved: %d %s", w.Code, w.Body.String())
			}
			var current string
			if err := a.DB.QueryRow(ctx, `SELECT farm_id::text FROM work_sessions WHERE id=$1`, id).Scan(&current); err != nil || current != farm["id"] {
				t.Fatalf("failed transfer changed farm: %s %v", current, err)
			}
		})
	}
}
