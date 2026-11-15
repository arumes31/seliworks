//go:build integration

package app

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Workers may submit with an empty note, Zeitausgleich and a corrected end time;
// the report CSV lists the entry with date, start, end and activity.
func TestWorkerStampingFieldsAndReportCSV(t *testing.T) {
	a, user := invitationSecurityFixture(t)
	ctx := context.Background()
	id, _ := deleteWorkEntry(t, a, user, "STOPPED_PENDING_DETAILS")
	var activity, name string
	if err := a.DB.QueryRow(ctx, `SELECT s.activity_id::text,a.name FROM work_sessions s JOIN activities a ON a.id=s.activity_id WHERE s.id=$1`, id).Scan(&activity, &name); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DB.Exec(ctx, `UPDATE users SET is_admin=false WHERE id=$1`, user.ID); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"version": 1, "activity_id": activity, "activity_ids": []string{activity}, "description": "", "day_type": "TIME_OFF", "breaks": []any{}, "effective_stop": "2099-01-01T00:00:00Z"}
	if w := deleteWorkRequest(t, a, user, "PATCH", "/work-sessions/"+id+"/details", body, true); w.Code != 422 {
		t.Fatalf("future end accepted: %d %s", w.Code, w.Body.String())
	}
	// Billing-relevant fields are ignored on the worker route.
	body["effective_stop"] = "2026-01-02T09:45:00Z"
	body["non_billable"] = true
	body["additional_deduction_us"] = 600000000
	body["deduction_reason"] = "self-service"
	saved := invitationSecurityOK(t, deleteWorkRequest(t, a, user, "PATCH", "/work-sessions/"+id+"/details", body, true))
	if saved["effective_stop"] != "2026-01-02T09:45:00+00:00" || saved["day_type"] != "TIME_OFF" || saved["stopped_at"] != "2026-01-02T10:00:00+00:00" {
		t.Fatalf("unexpected save: %v", saved)
	}
	if saved["non_billable"] != false || saved["additional_deduction_us"] != float64(0) || saved["deduction_reason"] != "" {
		t.Fatalf("worker changed billing fields: %v", saved)
	}
	invitationSecurityOK(t, deleteWorkRequest(t, a, user, "POST", "/work-sessions/"+id+"/submit", map[string]any{"version": saved["version"]}, true))

	if _, err := a.DB.Exec(ctx, `UPDATE users SET is_admin=true,language='de' WHERE id=$1`, user.ID); err != nil {
		t.Fatal(err)
	}
	w := deleteWorkRequest(t, a, user, "GET", "/admin/reports.csv?from=2026-01-01&to=2026-02-01", nil, false)
	csv := w.Body.String()
	if w.Code != 200 || !strings.Contains(csv, "Datum;Beginn Uhrzeit;Ende Uhrzeit;Tätigkeit") || !strings.Contains(csv, "2026-01-02;") || !strings.Contains(csv, ";"+name+";") || !strings.Contains(csv, ";00:45:00") {
		t.Fatalf("report CSV: %d %s", w.Code, csv)
	}
}

// A worker cannot move the end time of work already reserved for an invoice.
func TestWorkerStampingEndTimeBillingLock(t *testing.T) {
	a, user := invitationSecurityFixture(t)
	ctx := context.Background()
	id, farm := deleteWorkEntry(t, a, user, "STOPPED_PENDING_DETAILS")
	var activity, invoice string
	if err := a.DB.QueryRow(ctx, `SELECT activity_id::text FROM work_sessions WHERE id=$1`, id).Scan(&activity); err != nil {
		t.Fatal(err)
	}
	if err := a.DB.QueryRow(ctx, `INSERT INTO invoices(farm_id,from_date,to_date,period_start,period_stop,timezone,currency,issuer,customer,net_cents,tax_cents,total_cents,created_by) VALUES($1,'2026-01-01','2026-02-01','2026-01-01Z','2026-02-01Z','Europe/Vienna','EUR','{}','{}',100,0,100,$2) RETURNING id::text`, farm, user.ID).Scan(&invoice); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DB.Exec(ctx, `INSERT INTO invoice_allocations(invoice_id,session_id,source_version,started_at,stopped_at,net_us,net_cents,snapshot) VALUES($1,$2,1,'2026-01-02T09:00:00Z','2026-01-02T10:00:00Z',3600000000,100,'{}')`, invoice, id); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DB.Exec(ctx, `UPDATE users SET is_admin=false WHERE id=$1`, user.ID); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"version": 1, "activity_id": activity, "activity_ids": []string{activity}, "day_type": "REGULAR", "breaks": []any{}, "effective_stop": "2026-01-02T09:30:00Z"}
	w := deleteWorkRequest(t, a, user, "PATCH", "/work-sessions/"+id+"/details", body, true)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "BILLING_LOCKED") {
		t.Fatalf("reserved end time changed: %d %s", w.Code, w.Body.String())
	}
}

func TestTimeOffPricedLikeRegularDay(t *testing.T) {
	start := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	regular, err := calculatePolicyPrice(start, start.Add(time.Hour), nil, []priceRule{{ID: "r", Start: start, Rate: "20.00", Version: 1}}, false, 0, "REGULAR", nil)
	if err != nil {
		t.Fatal(err)
	}
	off, err := calculatePolicyPrice(start, start.Add(time.Hour), nil, []priceRule{{ID: "r", Start: start, Rate: "20.00", Version: 1}}, false, 0, "TIME_OFF", nil)
	if err != nil || !off.Priced || off.Net != regular.Net {
		t.Fatalf("time off pricing: %+v %v", off, err)
	}
}
