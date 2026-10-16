package app

import (
	"net/http"
	"strings"
	"time"
)

// editWork appends audited corrections to effective evidence. Original recording
// and financial snapshots stay intact; every completed edit requires fresh review.
func (a *App) editWork(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Version         int64        `json:"version"`
		WorkerID        string       `json:"worker_id"`
		FarmID          string       `json:"farm_id"`
		ActivityID      string       `json:"activity_id"`
		Start           time.Time    `json:"effective_start"`
		Stop            time.Time    `json:"effective_stop"`
		Description     string       `json:"description"`
		Location        string       `json:"location"`
		NonBillable     bool         `json:"non_billable"`
		Breaks          []BreakInput `json:"breaks"`
		Vacation        *bool        `json:"is_vacation_day"`
		DayType         string       `json:"day_type"`
		Deduction       int64        `json:"additional_deduction_us"`
		DeductionReason string       `json:"deduction_reason"`
		Reason          string       `json:"reason"`
	}
	if e := decode(r, &in); e != nil {
		handleError(w, r, e)
		return
	}
	dayType, dayError := resolveDayAnswer(in.Vacation, in.DayType)
	if dayError != nil {
		handleError(w, r, dayError)
		return
	}
	if in.Start.IsZero() || !in.Stop.After(in.Start) || strings.TrimSpace(in.Reason) == "" || len(in.Reason) > 2000 || len(in.Description) > 4000 || len(in.Location) > 300 || len(in.Breaks) > 100 || in.Deduction < 0 || len(in.DeductionReason) > 2000 || in.Deduction > 0 && strings.TrimSpace(in.DeductionReason) == "" {
		fail(w, r, 422, "VALIDATION", "Provide valid effective times, the vacation-day answer, deductions and a reason for this correction")
		return
	}
	id := r.PathValue("id")
	tx, e := a.DB.Begin(r.Context())
	if e != nil {
		handleError(w, r, e)
		return
	}
	defer tx.Rollback(r.Context())
	var previousWorker string
	e = tx.QueryRow(r.Context(), `SELECT worker_id::text FROM work_sessions WHERE id=$1`, id).Scan(&previousWorker)
	if e != nil {
		handleError(w, r, e)
		return
	}
	if in.WorkerID == "" {
		in.WorkerID = previousWorker
	}
	_, e = tx.Exec(r.Context(), `SELECT id FROM users WHERE id IN($1::uuid,$2::uuid) ORDER BY id FOR UPDATE`, previousWorker, in.WorkerID)
	if e != nil {
		handleError(w, r, e)
		return
	}
	state, version, e := lockOwned(r.Context(), tx, id, actor(r))
	if e == nil && version != in.Version {
		e = versionConflict()
	}
	if e == nil && state == "RUNNING" {
		e = problem(409, "STATE_CONFLICT", "Stop the running session explicitly before editing its completed evidence")
	}
	if e == nil {
		e = noReservations(r.Context(), tx, id)
	}
	if e != nil {
		handleError(w, r, e)
		return
	}
	before, e := a.session(r.Context(), tx, id)
	if e != nil {
		handleError(w, r, e)
		return
	}
	if before["worker_id"] != previousWorker {
		handleError(w, r, versionConflict())
		return
	}
	var valid bool
	e = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND is_worker) AND EXISTS(SELECT 1 FROM farms WHERE id=$2) AND ($3='' OR EXISTS(SELECT 1 FROM activities WHERE id=NULLIF($3,'')::uuid))`, in.WorkerID, in.FarmID, in.ActivityID).Scan(&valid)
	if e != nil {
		handleError(w, r, e)
		return
	}
	if !valid {
		fail(w, r, 422, "VALIDATION", "Choose valid worker, farm and activity records")
		return
	}
	var now time.Time
	if e = tx.QueryRow(r.Context(), `SELECT clock_timestamp()`).Scan(&now); e != nil {
		handleError(w, r, e)
		return
	}
	if in.Stop.After(now) {
		fail(w, r, 422, "FUTURE_WORK", "Effective completed work cannot end in the future")
		return
	}
	nextState := "SUBMITTED"
	if strings.TrimSpace(in.Description) == "" || in.ActivityID == "" {
		nextState = "STOPPED_PENDING_DETAILS"
	}
	_, e = tx.Exec(r.Context(), `DELETE FROM breaks WHERE session_id=$1`, id)
	if e == nil {
		_, e = tx.Exec(r.Context(), `UPDATE work_sessions SET worker_id=$2,farm_id=$3,activity_id=NULLIF($4,'')::uuid,effective_start=$5,effective_stop=$6,description=$7,location=$8,non_billable=$9,is_vacation_day=$10,additional_deduction_us=$11,deduction_reason=$12,state=$13,version=version+1,updated_at=$14,day_type=$15 WHERE id=$1`, id, in.WorkerID, in.FarmID, in.ActivityID, in.Start, in.Stop, strings.TrimSpace(in.Description), in.Location, in.NonBillable, dayType == "VACATION", in.Deduction, strings.TrimSpace(in.DeductionReason), nextState, now, dayType)
	}
	for _, b := range in.Breaks {
		if e != nil {
			break
		}
		_, e = tx.Exec(r.Context(), `INSERT INTO breaks(session_id,started_at,stopped_at) VALUES($1,$2,$3)`, id, b.StartedAt, b.StoppedAt)
	}
	var after map[string]any
	if e == nil {
		after, e = a.session(r.Context(), tx, id)
	}
	if e == nil {
		e = a.audit(r.Context(), tx, actor(r).ID, "work.admin_corrected", id, before, map[string]any{"session": after, "reason": in.Reason})
	}
	if e == nil {
		e = tx.Commit(r.Context())
	}
	if e != nil {
		handleError(w, r, e)
		return
	}
	writeJSON(w, 200, after)
}
