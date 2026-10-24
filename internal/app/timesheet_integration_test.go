//go:build integration

package app

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestTimesheetAuthorizationAndClippedTotals(t *testing.T) {
	a, owner := invitationSecurityFixture(t)
	ctx := context.Background()
	var other User
	if err := a.DB.QueryRow(ctx, `INSERT INTO users(name,email,password_hash,is_admin) SELECT 'FOREIGNSECRET','other@example.test',password_hash,false FROM users WHERE id=$1 RETURNING id::text`, owner.ID).Scan(&other.ID); err != nil {
		t.Fatal(err)
	}
	var farm string
	if err := a.DB.QueryRow(ctx, `INSERT INTO farms(name,active) VALUES('OWNFARM',false) RETURNING id::text`).Scan(&farm); err != nil {
		t.Fatal(err)
	}
	insert := func(worker, start, stop, state, description string) string {
		t.Helper()
		var id string
		err := a.DB.QueryRow(ctx, `INSERT INTO work_sessions(worker_id,farm_id,activity_id,started_at,stopped_at,effective_start,effective_stop,state,description,created_by,updated_at) VALUES($1,$2,CASE WHEN $5 IN ('SUBMITTED','APPROVED') THEN (SELECT id FROM activities LIMIT 1) END,$3::timestamptz,NULLIF($4,'')::timestamptz,$3::timestamptz,NULLIF($4,'')::timestamptz,$5,$6,$1,clock_timestamp()) RETURNING id::text`, worker, farm, start, stop, state, description).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	id := insert(owner.ID, "2026-10-24T20:00:00Z", "2026-10-25T06:00:00Z", "SUBMITTED", "OWNCLIPPED")
	if _, err := a.DB.Exec(ctx, `INSERT INTO breaks(session_id,started_at,stopped_at) VALUES($1,'2026-10-24T22:30:00Z','2026-10-24T23:30:00Z');`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DB.Exec(ctx, `UPDATE work_sessions SET additional_deduction_us=3600000000,deduction_reason='OWNDEDUCTION' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	pending := insert(owner.ID, "2026-10-25T08:00:00Z", "2026-10-25T09:00:00Z", "STOPPED_PENDING_DETAILS", "OWNPENDING")
	if _, err := a.DB.Exec(ctx, `UPDATE work_sessions SET is_vacation_day=NULL,day_type=NULL WHERE id=$1`, pending); err != nil {
		t.Fatal(err)
	}
	insert(owner.ID, "2026-10-25T10:00:00Z", "", "RUNNING", "RUNNINGSECRET")
	insert(owner.ID, "2026-10-25T03:00:00Z", "2026-10-25T04:00:00Z", "DELETED", "DELETEDSECRET")
	insert(owner.ID, "2026-10-24T12:00:00Z", "2026-10-24T13:00:00Z", "SUBMITTED", "OUTSIDERANGE")
	insert(other.ID, "2026-10-25T01:00:00Z", "2026-10-25T02:00:00Z", "SUBMITTED", "FOREIGNSECRET")
	path := "/me/timesheet.pdf?from=2026-10-25&to=2026-10-25&lang=de"
	for _, admin := range []bool{false, true} {
		t.Run(fmt.Sprintf("admin=%v", admin), func(t *testing.T) {
			if _, err := a.DB.Exec(ctx, `UPDATE users SET is_admin=$2 WHERE id=$1`, owner.ID, admin); err != nil {
				t.Fatal(err)
			}
			w := deleteWorkRequest(t, a, owner, "GET", path, nil, false)
			if w.Code != 200 {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			if w.Header().Get("Content-Type") != "application/pdf" || w.Header().Get("Cache-Control") != "private, no-store" {
				t.Fatal(w.Header())
			}
			pages := invoicePageStreams(t, w.Body.Bytes())
			pdfMarkerPage(t, pages, "OWNCLIPPED")
			pdfMarkerPage(t, pages, "OWNPENDING")
			pdfMarkerPage(t, pages, "Tagestyp ausständig")
			pdfMarkerPage(t, pages, "06:13:20")
			pdfMarkerPage(t, pages, "07:13:20")
			// PDF page content stores UTF-16BE; inspect forbidden strings in that encoding.
			for _, secret := range []string{"FOREIGNSECRET", "RUNNINGSECRET", "DELETEDSECRET", "OUTSIDERANGE"} {
				var encoded strings.Builder
				for _, c := range secret {
					encoded.WriteByte(0)
					encoded.WriteByte(byte(c))
				}
				for _, p := range pages {
					if strings.Contains(string(p), encoded.String()) {
						t.Fatalf("leaked %s", secret)
					}
				}
			}
		})
	}
	for _, tc := range []struct {
		name, path string
		user       User
		status     int
	}{
		{"anonymous", path, User{}, 401},
		{"foreign selector", path + "&worker_id=" + other.ID, owner, 422},
		{"missing dates", "/me/timesheet.pdf", owner, 422},
		{"bad range", "/me/timesheet.pdf?from=2026-10-26&to=2026-10-25", owner, 422},
		{"empty", "/me/timesheet.pdf?from=2020-01-01&to=2020-01-01", owner, 422},
		{"duplicate", path + "&from=2026-10-26", owner, 422},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := deleteWorkRequest(t, a, tc.user, "GET", tc.path, nil, false)
			if w.Code != tc.status {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
		})
	}
}
