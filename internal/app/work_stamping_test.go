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
	body["effective_stop"] = "2026-01-02T09:45:00Z"
	saved := invitationSecurityOK(t, deleteWorkRequest(t, a, user, "PATCH", "/work-sessions/"+id+"/details", body, true))
	if saved["effective_stop"] != "2026-01-02T09:45:00+00:00" || saved["day_type"] != "TIME_OFF" || saved["stopped_at"] != "2026-01-02T10:00:00+00:00" {
		t.Fatalf("unexpected save: %v", saved)
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
