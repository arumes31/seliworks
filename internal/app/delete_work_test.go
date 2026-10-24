//go:build integration

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func deleteWorkRequest(t *testing.T, a *App, user User, method, path string, body any, csrf bool) *httptest.ResponseRecorder {
	t.Helper()
	raw := token()
	if user.ID != "" {
		_, err := a.DB.Exec(context.Background(), `INSERT INTO auth_sessions(token_hash,user_id,csrf_token,expires_at) VALUES($1,$2,'delete-test',clock_timestamp()+interval '1 hour')`, digest(raw), user.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(method, a.Config.AppURL+"/api/v1"+path, bytes.NewReader(data))
	r.Header.Set("Origin", a.Config.AppURL)
	r.Header.Set("Content-Type", "application/json")
	if csrf {
		r.Header.Set("X-CSRF-Token", "delete-test")
	}
	if user.ID != "" {
		r.AddCookie(&http.Cookie{Name: "seliworks_session", Value: raw})
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	return w
}

func deleteWorkEntry(t *testing.T, a *App, user User, state string) (string, string) {
	t.Helper()
	ctx := context.Background()
	var farm, id string
	if err := a.DB.QueryRow(ctx, `INSERT INTO farms(name) VALUES('Deletion fixture') RETURNING id::text`).Scan(&farm); err != nil {
		t.Fatal(err)
	}
	var stop any = time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	if state == "RUNNING" {
		stop = nil
	}
	err := a.DB.QueryRow(ctx, `INSERT INTO work_sessions(worker_id,farm_id,activity_id,started_at,stopped_at,effective_start,effective_stop,state,description,created_by,updated_at,day_type) VALUES($1,$2,(SELECT id FROM activities LIMIT 1),'2026-01-02T09:00:00Z',$3,'2026-01-02T09:00:00Z',$3,$4,'Fixture work',$1,clock_timestamp(),'REGULAR') RETURNING id::text`, user.ID, farm, stop, state).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id, farm
}

func TestDeleteWorkAuthorizationAndGuards(t *testing.T) {
	for _, tc := range []struct {
		name, state, role, reason string
		version                   int
		csrf                      bool
		status                    int
	}{
		{"anonymous", "SUBMITTED", "anonymous", "Mistake", 1, true, 401},
		{"worker", "SUBMITTED", "worker", "Mistake", 1, true, 403},
		{"csrf", "SUBMITTED", "admin", "Mistake", 1, false, 403},
		{"empty reason", "SUBMITTED", "admin", "  ", 1, true, 422},
		{"long reason", "SUBMITTED", "admin", strings.Repeat("x", 2001), 1, true, 422},
		{"stale version", "SUBMITTED", "admin", "Mistake", 2, true, 409},
		{"running", "RUNNING", "admin", "Mistake", 1, true, 409},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, user := invitationSecurityFixture(t)
			id, _ := deleteWorkEntry(t, a, user, tc.state)
			if tc.role == "worker" {
				if _, err := a.DB.Exec(context.Background(), `UPDATE users SET is_admin=false WHERE id=$1`, user.ID); err != nil {
					t.Fatal(err)
				}
			}
			if tc.role == "anonymous" {
				user = User{}
			}
			w := deleteWorkRequest(t, a, user, "DELETE", "/admin/work-sessions/"+id, map[string]any{"version": tc.version, "reason": tc.reason}, tc.csrf)
			if w.Code != tc.status {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			var state string
			if err := a.DB.QueryRow(context.Background(), `SELECT state FROM work_sessions WHERE id=$1`, id).Scan(&state); err != nil || state != tc.state {
				t.Fatalf("changed record: %s, %v", state, err)
			}
		})
	}
}

func TestDeleteWorkRemovesOrdinaryViewsAndPreservesEvidence(t *testing.T) {
	for _, state := range []string{"STOPPED_PENDING_DETAILS", "SUBMITTED", "REJECTED", "APPROVED"} {
		t.Run(state, func(t *testing.T) {
			a, user := invitationSecurityFixture(t)
			id, farm := deleteWorkEntry(t, a, user, state)
			ctx := context.Background()
			if _, err := a.DB.Exec(ctx, `INSERT INTO breaks(session_id,started_at,stopped_at) VALUES($1,'2026-01-02T09:15:00Z','2026-01-02T09:30:00Z')`, id); err != nil {
				t.Fatal(err)
			}
			var correction string
			if err := a.DB.QueryRow(ctx, `INSERT INTO corrections(session_id,requested_by,source_version,requested_start,requested_stop,reason) VALUES($1,$2,1,'2026-01-02T09:00:00Z','2026-01-02T10:00:00Z','Fix') RETURNING id::text`, id, user.ID).Scan(&correction); err != nil {
				t.Fatal(err)
			}
			w := deleteWorkRequest(t, a, user, "DELETE", "/admin/work-sessions/"+id, map[string]any{"version": 1, "reason": "Duplicate entry"}, true)
			if w.Code != 204 {
				t.Fatalf("delete: %d %s", w.Code, w.Body.String())
			}
			for _, path := range []string{"/work-sessions/" + id, "/admin/work-sessions/" + id + "/pricing"} {
				if got := deleteWorkRequest(t, a, user, "GET", path, nil, true); got.Code != 404 {
					t.Fatalf("visible %s: %d %s", path, got.Code, got.Body.String())
				}
			}
			for _, path := range []string{"/work-sessions", "/work-sessions?state=DELETED", "/admin/corrections"} {
				got := deleteWorkRequest(t, a, user, "GET", path, nil, true)
				var list struct {
					Items []json.RawMessage
					Total int
				}
				if err := json.Unmarshal(got.Body.Bytes(), &list); err != nil || got.Code != 200 || len(list.Items) != 0 || list.Total != 0 {
					t.Fatalf("list %s: %d %s", path, got.Code, got.Body.String())
				}
			}
			start := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
			totals, err := a.periodTotals(ctx, a.DB, user.ID, start, start.Add(24*time.Hour), start.Add(24*time.Hour))
			if err != nil || totals.Net != 0 {
				t.Fatalf("deleted time counted: %+v %v", totals, err)
			}
			var audits, breaks int
			if err = a.DB.QueryRow(ctx, `SELECT (SELECT count(*) FROM audit_events WHERE entity_id=$1 AND action='work.deleted' AND before_value->>'state'=$2 AND after_value->>'reason'='Duplicate entry'),(SELECT count(*) FROM breaks WHERE session_id=$1::uuid)`, id, state).Scan(&audits, &breaks); err != nil || audits != 1 || breaks != 1 {
				t.Fatalf("evidence: %d %d %v", audits, breaks, err)
			}
			got := deleteWorkRequest(t, a, user, "POST", "/admin/corrections/"+correction+"/review", map[string]any{"approve": true, "reason": "Old request"}, true)
			if got.Code != 404 {
				t.Fatalf("correction resurrected deletion: %d %s", got.Code, got.Body.String())
			}
			got = deleteWorkRequest(t, a, user, "DELETE", "/admin/work-sessions/"+id, map[string]any{"version": 1, "reason": "Duplicate entry"}, true)
			if got.Code != 404 {
				t.Fatalf("repeat delete: %d", got.Code)
			}
			// A replacement may reuse the removed interval without losing its audit trail.
			_, err = a.DB.Exec(ctx, `INSERT INTO work_sessions(worker_id,farm_id,started_at,stopped_at,effective_start,effective_stop,state,created_by,updated_at) VALUES($1,$2,'2026-01-02T09:00:00Z','2026-01-02T10:00:00Z','2026-01-02T09:00:00Z','2026-01-02T10:00:00Z','STOPPED_PENDING_DETAILS',$1,clock_timestamp())`, user.ID, farm)
			if err != nil {
				t.Fatalf("replacement interval blocked: %v", err)
			}
		})
	}
}

func TestDeleteWorkBillingReservations(t *testing.T) {
	for _, state := range []string{"draft", "issued", "cancelled", "reversed"} {
		t.Run(state, func(t *testing.T) {
			a, u := invitationSecurityFixture(t)
			id, farm := deleteWorkEntry(t, a, u, "APPROVED")
			ctx := context.Background()
			var invoice string
			err := a.DB.QueryRow(ctx, `INSERT INTO invoices(farm_id,state,number,from_date,to_date,period_start,period_stop,timezone,currency,issuer,customer,net_cents,tax_cents,total_cents,created_by,issued_at,due_on) VALUES($1,$2,'FIXTURE-1','2026-01-01','2026-02-01','2026-01-01Z','2026-02-01Z','Europe/Vienna','EUR','{}','{}',100,0,100,$3,clock_timestamp(),'2026-02-15') RETURNING id::text`, farm, state, u.ID).Scan(&invoice)
			if err != nil {
				t.Fatal(err)
			}
			active := state == "draft" || state == "issued"
			_, err = a.DB.Exec(ctx, `INSERT INTO invoice_allocations(invoice_id,session_id,source_version,started_at,stopped_at,active,net_us,net_cents,snapshot) VALUES($1,$2,1,'2026-01-02T09:00:00Z','2026-01-02T10:00:00Z',$3,3600000000,100,'{}')`, invoice, id, active)
			if err != nil {
				t.Fatal(err)
			}
			w := deleteWorkRequest(t, a, u, "DELETE", "/admin/work-sessions/"+id, map[string]any{"version": 1, "reason": "Duplicate"}, true)
			expected := 204
			if active {
				expected = 409
			}
			if w.Code != expected {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			var count int
			if err = a.DB.QueryRow(ctx, `SELECT count(*) FROM invoice_allocations WHERE session_id=$1`, id).Scan(&count); err != nil || count != 1 {
				t.Fatalf("lost financial evidence: %d %v", count, err)
			}
		})
	}
}
