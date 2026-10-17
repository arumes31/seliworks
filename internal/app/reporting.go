package app

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type reportRow struct {
	ID        string `json:"id,omitempty"`
	Label     string `json:"label"`
	Gross     int64  `json:"gross_us"`
	Break     int64  `json:"break_us"`
	Deduction int64  `json:"additional_deduction_us"`
	Net       int64  `json:"net_us"`
	Approved  int64  `json:"approved_us"`
	Unbilled  int64  `json:"unbilled_us"`
}

func localDateRange(from, to, zone string) (time.Time, time.Time, error) {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	start, err := time.ParseInLocation("2006-01-02", from, loc)
	if err != nil {
		return start, start, problem(422, "VALIDATION", "From and to require YYYY-MM-DD dates")
	}
	stop, err := time.ParseInLocation("2006-01-02", to, loc)
	if err != nil || !stop.After(start) || stop.After(start.AddDate(10, 0, 0)) {
		return start, stop, problem(422, "VALIDATION", "Select a positive date period of at most ten years; end date is exclusive")
	}
	return start, stop, nil
}
func safeCSV(value string) string {
	v := strings.TrimLeft(value, " \t\r\n")
	if v != "" && strings.ContainsRune("=+-@", rune(v[0])) {
		return "'" + value
	}
	return value
}
func (a *App) report(w http.ResponseWriter, r *http.Request) {
	tx, err := a.DB.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		handleError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	var zone, currency string
	var now time.Time
	err = tx.QueryRow(r.Context(), `SELECT timezone,currency,clock_timestamp() FROM organization WHERE id`).Scan(&zone, &currency, &now)
	if err != nil {
		handleError(w, r, err)
		return
	}
	now = now.UTC()
	loc, err := time.LoadLocation(zone)
	if err != nil {
		handleError(w, r, err)
		return
	}
	from, to := r.URL.Query().Get("from"), r.URL.Query().Get("to")
	if from == "" {
		from = now.In(loc).Format("2006-01") + "-01"
	}
	if to == "" {
		d, e := time.ParseInLocation("2006-01-02", from, loc)
		if e != nil {
			handleError(w, r, problem(422, "VALIDATION", "Invalid from date"))
			return
		}
		to = d.AddDate(0, 1, 0).Format("2006-01-02")
	}
	start, stop, err := localDateRange(from, to, zone)
	if err != nil {
		handleError(w, r, err)
		return
	}
	group := r.URL.Query().Get("group")
	if group == "" {
		group = "day"
	}
	if group != "day" && group != "worker" && group != "farm" && group != "activity" {
		fail(w, r, 422, "VALIDATION", "Group must be worker, farm, activity or day")
		return
	}
	rows, err := tx.Query(r.Context(), `SELECT w.id::text,w.effective_start,coalesce(w.effective_stop,$3),w.state,u.name,f.name,coalesce(a.name,'—'),w.worker_id::text,w.farm_id::text,coalesce(w.activity_id::text,''),w.additional_deduction_us,w.non_billable,
coalesce((SELECT jsonb_agg(jsonb_build_object('started_at',b.started_at,'stopped_at',b.stopped_at) ORDER BY b.started_at) FROM work_break_intervals b WHERE b.session_id=w.id),'[]'::jsonb),
coalesce((SELECT jsonb_agg(jsonb_build_object('started_at',al.started_at,'stopped_at',al.stopped_at)) FROM invoice_allocations al WHERE al.session_id=w.id AND al.active AND al.started_at<least(coalesce(w.effective_stop,$3),$2) AND al.stopped_at>greatest(w.effective_start,$1)),'[]'::jsonb)
FROM work_sessions w JOIN users u ON u.id=w.worker_id JOIN farms f ON f.id=w.farm_id LEFT JOIN activities a ON a.id=w.activity_id WHERE w.effective_start<$2 AND coalesce(w.effective_stop,$3)>$1 AND ($4='' OR w.farm_id::text=$4) AND ($5='' OR w.worker_id::text=$5) ORDER BY w.effective_start`, start, stop, now, r.URL.Query().Get("farm_id"), r.URL.Query().Get("worker_id"))
	if err != nil {
		handleError(w, r, err)
		return
	}
	type entry struct {
		id                            string
		start, stop                   time.Time
		state, worker, farm, activity string
		workerID, farmID, activityID  string
		deduction                     int64
		nonBillable                   bool
		breaks, claims                []priceInterval
	}
	entries := []entry{}
	for rows.Next() {
		var e entry
		var breaks, claims []byte
		if err = rows.Scan(&e.id, &e.start, &e.stop, &e.state, &e.worker, &e.farm, &e.activity, &e.workerID, &e.farmID, &e.activityID, &e.deduction, &e.nonBillable, &breaks, &claims); err != nil {
			break
		}
		if err = json.Unmarshal(breaks, &e.breaks); err != nil {
			break
		}
		if err = json.Unmarshal(claims, &e.claims); err != nil {
			break
		}
		entries = append(entries, e)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		handleError(w, r, err)
		return
	}
	groups := map[string]*reportRow{}
	totals := reportRow{Label: "Total"}
	var provisional int64
	for _, e := range entries {
		breaks := e.breaks
		x, y := clippedInterval(e.start, e.stop, start, stop)
		claims := e.claims
		billable := !e.nonBillable
		for x.Before(y) {
			end := y
			label := e.worker
			key := e.workerID
			switch group {
			case "farm":
				label = e.farm
				key = e.farmID
			case "activity":
				label = e.activity
				key = e.activityID
			case "day":
				label = x.In(loc).Format("2006-01-02")
				key = label
				d := x.In(loc)
				midnight := time.Date(d.Year(), d.Month(), d.Day()+1, 0, 0, 0, 0, loc)
				if midnight.Before(end) {
					end = midnight
				}
			}
			gross := end.Sub(x).Microseconds()
			rawNet := netMicroseconds(x, end, breaks)
			deduction := allocatedDeductionUS(e.start, e.stop, x, end, breaks, e.deduction)
			net := rawNet - deduction
			v := groups[key]
			if v == nil {
				v = &reportRow{ID: key, Label: label}
				groups[key] = v
			}
			v.Gross += gross
			v.Break += gross - rawNet
			v.Deduction += deduction
			v.Net += net
			if e.state == "APPROVED" {
				v.Approved += net
				if billable {
					unbilled := net
					for _, claim := range claims {
						cs, ce := clippedInterval(claim.Start, claim.Stop, x, end)
						unbilled -= billableMicroseconds(e.start, e.stop, cs, ce, breaks, e.deduction)
					}
					v.Unbilled += unbilled
				}
			}
			if e.state == "RUNNING" {
				provisional += net
			}
			x = end
		}
	}
	result := []reportRow{}
	for _, v := range groups {
		result = append(result, *v)
		totals.Gross += v.Gross
		totals.Break += v.Break
		totals.Deduction += v.Deduction
		totals.Net += v.Net
		totals.Approved += v.Approved
		totals.Unbilled += v.Unbilled
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Label < result[j].Label })
	if strings.HasSuffix(r.URL.Path, ".csv") {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="seliworks-report.csv"`)
		if _, err = w.Write([]byte("\xef\xbb\xbf")); err != nil {
			return
		}
		out := csv.NewWriter(w)
		out.Comma = ';'
		headers := []string{"Group", "Gross microseconds", "Break microseconds", "Net microseconds", "Approved microseconds", "Unbilled microseconds", "Additional deduction microseconds"}
		if auth, ok := r.Context().Value(authKey{}).(authContext); ok && auth.User.Language == "de" {
			headers = []string{"Gruppe", "Bruttozeit in Mikrosekunden", "Pausen in Mikrosekunden", "Nettozeit in Mikrosekunden", "Freigegebene Zeit in Mikrosekunden", "Unverrechnete Zeit in Mikrosekunden", "Zusätzlicher Abzug in Mikrosekunden"}
		}
		if err = out.Write(headers); err != nil {
			return
		}
		for _, v := range result {
			if err = out.Write([]string{safeCSV(v.Label), fmt.Sprint(v.Gross), fmt.Sprint(v.Break), fmt.Sprint(v.Net), fmt.Sprint(v.Approved), fmt.Sprint(v.Unbilled), fmt.Sprint(v.Deduction)}); err != nil {
				return
			}
		}
		out.Flush()
		return
	}
	finance, err := reportFinance(r.Context(), tx, start, stop, r.URL.Query().Get("farm_id"), r.URL.Query().Get("worker_id"), currency, totals.Unbilled)
	if err != nil {
		handleError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"rows": result, "totals": totals, "finance": finance, "from": from, "to": to, "timezone": zone, "provisional_us": provisional, "server_now": now})
}
