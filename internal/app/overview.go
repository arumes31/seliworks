package app

import (
	"net/http"
	"time"
)

func (a *App) overview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var zone string
	var currency string
	var now time.Time
	if e := a.DB.QueryRow(ctx, `SELECT timezone,currency,clock_timestamp() FROM organization`).Scan(&zone, &currency, &now); e != nil {
		handleError(w, r, e)
		return
	}
	loc, e := time.LoadLocation(zone)
	if e != nil {
		handleError(w, r, e)
		return
	}
	local := now.In(loc)
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	month := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, loc)
	year := time.Date(local.Year(), 1, 1, 0, 0, 0, 0, loc)
	out := map[string]any{"server_now": now.UTC(), "timezone": zone, "running_provisional": true}
	for k, from := range map[string]time.Time{"today_us": day, "month_us": month, "year_us": year} {
		total, err := a.periodTotals(ctx, a.DB, "", from, now, now)
		e = err
		if e != nil {
			handleError(w, r, e)
			return
		}
		out[k] = total.Net
	}
	var pending, approvals int
	var unbilled int64
	e = a.DB.QueryRow(ctx, `SELECT count(*) FILTER(WHERE state='STOPPED_PENDING_DETAILS'),count(*) FILTER(WHERE state='SUBMITTED'),coalesce(sum((extract(epoch FROM (s.effective_stop-s.effective_start))*1000000)::bigint-coalesce((SELECT sum((extract(epoch FROM (stopped_at-started_at))*1000000)::bigint) FROM breaks WHERE session_id=s.id),0)-s.additional_deduction_us-coalesce((SELECT sum(net_us) FROM invoice_allocations WHERE session_id=s.id AND active),0)) FILTER(WHERE state='APPROVED' AND NOT non_billable),0)::bigint FROM work_sessions s`).Scan(&pending, &approvals, &unbilled)
	if e != nil {
		handleError(w, r, e)
		return
	}
	out["pending_details"] = pending
	out["pending_approvals"] = approvals
	out["unbilled_us"] = unbilled
	balances, e := queryItems(ctx, a.DB, `SELECT jsonb_build_object('currency',i.currency,'outstanding',round(sum(i.total_cents-coalesce((SELECT sum(amount_cents) FROM payments WHERE invoice_id=i.id),0))/100,2)::text) FROM invoices i WHERE state='issued' GROUP BY i.currency ORDER BY i.currency`)
	if e != nil {
		handleError(w, r, e)
		return
	}
	if len(balances) == 0 {
		balances = []map[string]any{{"currency": currency, "outstanding": "0.00"}}
	}
	out["outstanding_by_currency"] = balances
	out["outstanding"], out["outstanding_currency"] = nil, nil
	if len(balances) == 1 {
		out["outstanding"], out["outstanding_currency"] = balances[0]["outstanding"], balances[0]["currency"]
	}
	running, e := queryItems(ctx, a.DB, sessionSelect+` WHERE s.state='RUNNING' ORDER BY s.effective_start`)
	if e != nil {
		handleError(w, r, e)
		return
	}
	out["running"] = running
	daily := []map[string]any{}
	for offset := -6; offset <= 0; offset++ {
		from := day.AddDate(0, 0, offset)
		to := from.AddDate(0, 0, 1)
		if to.After(now) {
			to = now
		}
		v, e := a.periodTotals(ctx, a.DB, "", from, to, now)
		if e != nil {
			handleError(w, r, e)
			return
		}
		daily = append(daily, map[string]any{"date": from.Format("2006-01-02"), "net_us": v.Net})
	}
	out["daily"] = daily
	farmRecords, e := queryItems(ctx, a.DB, `SELECT jsonb_build_object('id',id,'name',name) FROM farms ORDER BY name`)
	if e != nil {
		handleError(w, r, e)
		return
	}
	farms := []map[string]any{}
	for _, f := range farmRecords {
		v, e := a.filteredTotals(ctx, a.DB, "", f["id"].(string), month, now, now)
		if e != nil {
			handleError(w, r, e)
			return
		}
		farms = append(farms, map[string]any{"name": f["name"], "net_us": v.Net})
	}
	out["farms"] = farms
	writeJSON(w, 200, out)
}
