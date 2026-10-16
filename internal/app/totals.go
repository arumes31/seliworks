package app

import (
	"context"
	"encoding/json"
	"time"
)

type timeTotals struct{ Gross, Break, Deduction, Net int64 }

func (a *App) periodTotals(ctx context.Context, q querier, worker string, from, to, now time.Time) (timeTotals, error) {
	return a.filteredTotals(ctx, q, worker, "", from, to, now)
}
func (a *App) filteredTotals(ctx context.Context, q querier, worker, farm string, from, to, now time.Time) (timeTotals, error) {
	rows, e := q.Query(ctx, `SELECT s.effective_start,coalesce(s.effective_stop,$4),s.additional_deduction_us,coalesce((SELECT jsonb_agg(jsonb_build_object('started_at',b.started_at,'stopped_at',b.stopped_at)) FROM work_break_intervals b WHERE b.session_id=s.id),'[]'::jsonb) FROM work_sessions s WHERE ($1='' OR s.worker_id=NULLIF($1,'')::uuid) AND s.effective_start<$3 AND coalesce(s.effective_stop,$4)>$2 AND($5='' OR s.farm_id=NULLIF($5,'')::uuid)`, worker, from, to, now, farm)
	if e != nil {
		return timeTotals{}, e
	}
	defer rows.Close()
	var out timeTotals
	for rows.Next() {
		var start, stop time.Time
		var deduction int64
		var raw []byte
		if e = rows.Scan(&start, &stop, &deduction, &raw); e != nil {
			return out, e
		}
		if stop.Before(start) {
			return out, problem(409, "CLOCK_ANOMALY", "Database clock precedes recorded work")
		}
		var breaks []priceInterval
		if e = json.Unmarshal(raw, &breaks); e != nil {
			return out, e
		}
		x, y := clippedInterval(start, stop, from, to)
		if !y.After(x) {
			continue
		}
		gross := y.Sub(x).Microseconds()
		rawNet := netMicroseconds(x, y, breaks)
		d := allocatedDeductionUS(start, stop, x, y, breaks, deduction)
		out.Gross += gross
		out.Break += gross - rawNet
		out.Deduction += d
		out.Net += rawNet - d
	}
	return out, rows.Err()
}
