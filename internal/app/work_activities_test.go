//go:build integration

package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWorkActivitiesRoundTrip(t *testing.T) {
	a, user := invitationSecurityFixture(t)
	ctx := context.Background()
	id, farm := deleteWorkEntry(t, a, user, "STOPPED_PENDING_DETAILS")
	var primary, extra string
	if err := a.DB.QueryRow(ctx, `SELECT activity_id::text FROM work_sessions WHERE id=$1`, id).Scan(&primary); err != nil {
		t.Fatal(err)
	}
	if err := a.DB.QueryRow(ctx, `INSERT INTO activities(name) VALUES('EXTRAACTIVITY') RETURNING id::text`).Scan(&extra); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"version": 1, "activity_id": primary, "activity_ids": []string{primary, extra}, "additional_activities": "  Zaun repariert\nWasser kontrolliert  ", "description": "Daily work", "day_type": "REGULAR", "breaks": []any{}}
	if _, err := a.DB.Exec(ctx, `UPDATE users SET is_admin=false WHERE id=$1`, user.ID); err != nil {
		t.Fatal(err)
	}
	saved := invitationSecurityOK(t, deleteWorkRequest(t, a, user, "PATCH", "/work-sessions/"+id+"/details", body, true))
	assertWorkActivities(t, saved, primary, extra, "Zaun repariert\nWasser kontrolliert")
	// Old clients must not silently erase the newly added evidence.
	delete(body, "activity_ids")
	delete(body, "additional_activities")
	body["version"] = saved["version"]
	saved = invitationSecurityOK(t, deleteWorkRequest(t, a, user, "PATCH", "/work-sessions/"+id+"/details", body, true))
	assertWorkActivities(t, saved, primary, extra, "Zaun repariert\nWasser kontrolliert")
	if _, err := a.DB.Exec(ctx, `UPDATE users SET is_admin=true WHERE id=$1`, user.ID); err != nil {
		t.Fatal(err)
	}
	body = map[string]any{"version": saved["version"], "worker_id": user.ID, "farm_id": farm, "activity_id": extra, "activity_ids": []string{primary, extra}, "additional_activities": "TEXTMARKER", "description": "Daily work", "day_type": "REGULAR", "effective_start": "2026-01-02T09:00:00Z", "effective_stop": "2026-01-02T10:00:00Z", "reason": "Correct activities"}
	saved = invitationSecurityOK(t, deleteWorkRequest(t, a, user, "PATCH", "/admin/work-sessions/"+id, body, true))
	assertWorkActivities(t, saved, extra, primary, "TEXTMARKER")
	if saved["net_us"] != float64(3600000000) {
		t.Fatalf("time counted more than once: %v", saved["net_us"])
	}
	for _, path := range []string{"/work-sessions/" + id, "/work-sessions"} {
		response := deleteWorkRequest(t, a, user, "GET", path, nil, false)
		if response.Code != 200 || !strings.Contains(response.Body.String(), "TEXTMARKER") || !strings.Contains(response.Body.String(), "EXTRAACTIVITY") {
			t.Fatalf("missing evidence from %s: %s", path, response.Body.String())
		}
	}
	for _, path := range []string{"/me/timesheet.pdf?from=2026-01-02&to=2026-01-02", "/admin/timesheets.pdf?from=2026-01-02&to=2026-01-02&all=true"} {
		response := deleteWorkRequest(t, a, user, "GET", path, nil, false)
		if response.Code != 200 {
			t.Fatalf("PDF: %d %s", response.Code, response.Body.String())
		}
		pages := invoicePageStreams(t, response.Body.Bytes())
		pdfMarkerPage(t, pages, "TEXTMARKER")
		pdfMarkerPage(t, pages, "EXTRAACTIVITY")
	}
	body["version"] = saved["version"]
	body["activity_ids"] = []string{extra}
	body["additional_activities"] = ""
	saved = invitationSecurityOK(t, deleteWorkRequest(t, a, user, "PATCH", "/admin/work-sessions/"+id, body, true))
	ids, _ := saved["activity_ids"].([]any)
	if len(ids) != 1 || saved["additional_activities"] != "" {
		t.Fatalf("explicit removal failed: %v", saved)
	}
	// Manual creation accepts the same activity fields.
	body["started_at"] = "2026-01-03T09:00:00Z"
	body["stopped_at"] = "2026-01-03T10:00:00Z"
	for _, key := range []string{"version", "effective_start", "effective_stop", "day_type"} {
		delete(body, key)
	}
	body["activity_ids"] = []string{extra, primary}
	body["additional_activities"] = "Manual text"
	saved = invitationSecurityOK(t, deleteWorkRequest(t, a, user, "POST", "/admin/work-sessions/manual", body, true))
	assertWorkActivities(t, saved, extra, primary, "Manual text")
}

func TestWorkActivitiesValidation(t *testing.T) {
	a, user := invitationSecurityFixture(t)
	ctx := context.Background()
	id, farm := deleteWorkEntry(t, a, user, "STOPPED_PENDING_DETAILS")
	var primary, inactive string
	if err := a.DB.QueryRow(ctx, `SELECT activity_id::text FROM work_sessions WHERE id=$1`, id).Scan(&primary); err != nil {
		t.Fatal(err)
	}
	if err := a.DB.QueryRow(ctx, `INSERT INTO activities(name,active) VALUES('Inactive',false) RETURNING id::text`).Scan(&inactive); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		ids  []string
		text string
	}{
		{"empty selection", []string{}, ""},
		{"duplicate", []string{primary, strings.ToUpper(primary)}, ""},
		{"invalid UUID", []string{primary, "bad"}, ""},
		{"unknown", []string{primary, "00000000-0000-0000-0000-000000000000"}, ""},
		{"inactive", []string{primary, inactive}, ""},
		{"missing primary", []string{inactive}, ""},
		{"oversized text", []string{primary}, strings.Repeat("ä", 1001)},
		{"NUL text", []string{primary}, "a\x00b"},
		{"too many", make([]string, 51), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, path := range []string{"/work-sessions/" + id + "/details", "/admin/work-sessions/" + id} {
				body := map[string]any{"version": 1, "activity_id": primary, "activity_ids": tc.ids, "additional_activities": tc.text, "description": "Work", "day_type": "REGULAR"}
				if strings.Contains(path, "/admin/") {
					body["worker_id"] = user.ID
					body["farm_id"] = farm
					body["effective_start"] = "2026-01-02T09:00:00Z"
					body["effective_stop"] = "2026-01-02T10:00:00Z"
					body["reason"] = "Edit"
				}
				if w := deleteWorkRequest(t, a, user, "PATCH", path, body, true); w.Code != 422 {
					t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
				}
			}
		})
	}
	var version int
	if err := a.DB.QueryRow(ctx, `SELECT version FROM work_sessions WHERE id=$1`, id).Scan(&version); err != nil || version != 1 {
		t.Fatalf("invalid request changed version: %d %v", version, err)
	}
	// Existing inactive selections can still be retained while completing work.
	if _, err := a.DB.Exec(ctx, `UPDATE activities SET active=false WHERE id=$1`, primary); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"version": 1, "activity_id": primary, "activity_ids": []string{primary}, "additional_activities": strings.Repeat("ä", 1000), "description": "Work", "day_type": "REGULAR"}
	invitationSecurityOK(t, deleteWorkRequest(t, a, user, "PATCH", "/work-sessions/"+id+"/details", body, true))
	if w := deleteWorkRequest(t, a, user, "PATCH", "/work-sessions/"+id+"/details", body, true); w.Code != 409 {
		t.Fatalf("stale write: %d", w.Code)
	}
}

func TestWorkActivitiesPricingInvoiceAndExport(t *testing.T) {
	a, user := invitationSecurityFixture(t)
	ctx := context.Background()
	id, farm := deleteWorkEntry(t, a, user, "SUBMITTED")
	var primary, extra string
	if err := a.DB.QueryRow(ctx, `SELECT activity_id::text FROM work_sessions WHERE id=$1`, id).Scan(&primary); err != nil {
		t.Fatal(err)
	}
	if err := a.DB.QueryRow(ctx, `INSERT INTO activities(name) VALUES('EXTRABILLING') RETURNING id::text`).Scan(&extra); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{`UPDATE organization SET tax_confirmed=true,invoice_appendix=true`, `INSERT INTO rates(hourly_rate,effective_from) VALUES(25,'2020-01-01Z')`} {
		if _, err := a.DB.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.DB.Exec(ctx, `INSERT INTO rates(worker_id,farm_id,activity_id,hourly_rate,effective_from) VALUES($1,$2,$3,100,'2020-01-01Z')`, user.ID, farm, extra); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"version": 1, "worker_id": user.ID, "farm_id": farm, "activity_id": primary, "activity_ids": []string{primary, extra}, "additional_activities": "BILLINGTEXT", "description": "Daily work", "day_type": "REGULAR", "effective_start": "2026-01-02T09:00:00Z", "effective_stop": "2026-01-02T10:00:00Z", "reason": "Multiple activities"}
	saved := invitationSecurityOK(t, deleteWorkRequest(t, a, user, "PATCH", "/admin/work-sessions/"+id, body, true))
	approved := invitationSecurityOK(t, deleteWorkRequest(t, a, user, "POST", "/admin/work-sessions/"+id+"/approve", map[string]any{"version": saved["version"]}, true))
	raw, _ := json.Marshal(map[string]any{"farm_id": farm, "from": "2026-01-02", "to": "2026-01-03"})
	r := httptest.NewRequest("POST", "/api/v1/admin/invoices", bytes.NewReader(raw))
	r.Header.Set("Idempotency-Key", "multi-activities-invoice")
	r = r.WithContext(context.WithValue(r.Context(), authKey{}, authContext{User: user}))
	w := httptest.NewRecorder()
	a.createInvoice(w, r)
	created := invitationSecurityOK(t, w)
	doc, err := invoiceFrom(ctx, a.DB, created["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Net != "25.00" || len(doc.Items) != 1 || doc.Items[0].NetUS != 3600000000 || !strings.Contains(doc.Items[0].ActivityDetails, "EXTRABILLING") || !strings.Contains(doc.Items[0].ActivityDetails, "BILLINGTEXT") {
		t.Fatalf("incorrect invoice: %+v", doc)
	}
	// Catalog changes after drafting do not change the recorded invoice evidence.
	if _, err = a.DB.Exec(ctx, `UPDATE activities SET name='Changed later' WHERE id=$1`, extra); err != nil {
		t.Fatal(err)
	}
	frozen, err := invoiceFrom(ctx, a.DB, doc.ID)
	if err != nil || frozen.Items[0].ActivityDetails != doc.Items[0].ActivityDetails {
		t.Fatal("invoice details not frozen", err)
	}
	body["version"] = approved["version"]
	body["additional_activities"] = "Must not change"
	if w := deleteWorkRequest(t, a, user, "PATCH", "/admin/work-sessions/"+id, body, true); w.Code != 409 {
		t.Fatalf("reserved invoice work edited: %d %s", w.Code, w.Body.String())
	}
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var archive bytes.Buffer
	if err = writeBusinessArchive(ctx, tx, &archive); err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(archive.Bytes()), int64(archive.Len()))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, file := range z.File {
		if file.Name == "work_session_activities.jsonl" && file.UncompressedSize64 > 0 {
			found = true
		}
	}
	if !found {
		t.Fatal("export omitted activities")
	}
}

func assertWorkActivities(t *testing.T, entry map[string]any, primary, extra, text string) {
	t.Helper()
	ids, _ := entry["activity_ids"].([]any)
	if entry["activity_id"] != primary || len(ids) != 2 || ids[0] != primary || ids[1] != extra || entry["additional_activities"] != text {
		raw, _ := json.Marshal(entry)
		t.Fatalf("activities did not round-trip: %s", raw)
	}
}
