package app

import (
	"github.com/jackc/pgx/v5"
	"net/http"
	"time"
)

func (a *App) pause(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Minutes *int `json:"duration_minutes"`
	}
	if e := decode(r, &in); e != nil {
		handleError(w, r, e)
		return
	}
	if in.Minutes != nil && (*in.Minutes < 1 || *in.Minutes > 1440) {
		fail(w, r, 422, "VALIDATION", "Pause duration must be between 1 and 1440 minutes")
		return
	}
	id := r.PathValue("id")
	u := actor(r)
	v, e := a.command(r, "work.pause:"+id, in, func(tx pgx.Tx) (any, error) {
		state, _, e := lockOwned(r.Context(), tx, id, u)
		if e != nil {
			return nil, e
		}
		if state != "RUNNING" {
			return nil, problem(409, "STATE_CONFLICT", "Only running work can be paused")
		}
		var now time.Time
		if e = tx.QueryRow(r.Context(), `SELECT clock_timestamp()`).Scan(&now); e != nil {
			return nil, e
		}
		var previous string
		var planned *time.Time
		var started time.Time
		e = tx.QueryRow(r.Context(), `SELECT id::text,started_at,planned_stop FROM work_pauses WHERE session_id=$1 AND resumed_at IS NULL FOR UPDATE`, id).Scan(&previous, &started, &planned)
		if e == nil {
			if planned == nil || planned.After(now) {
				return nil, problem(409, "ALREADY_PAUSED", "This work session is already paused")
			}
			if now.Before(started) {
				return nil, problem(409, "CLOCK_ANOMALY", "Database clock precedes the pause")
			}
			if _, e = tx.Exec(r.Context(), `UPDATE work_pauses SET resumed_at=planned_stop WHERE id=$1`, previous); e != nil {
				return nil, e
			}
		} else if e != pgx.ErrNoRows {
			return nil, e
		}
		var last time.Time
		if e = tx.QueryRow(r.Context(), `SELECT greatest(effective_start,coalesce((SELECT max(coalesce(resumed_at,planned_stop,started_at)) FROM work_pauses WHERE session_id=$1),effective_start)) FROM work_sessions WHERE id=$1`, id).Scan(&last); e != nil {
			return nil, e
		}
		if now.Before(last) {
			return nil, problem(409, "CLOCK_ANOMALY", "Database clock precedes recorded pause evidence")
		}
		var until *time.Time
		if in.Minutes != nil {
			x := now.Add(time.Duration(*in.Minutes) * time.Minute)
			until = &x
		}
		var pid string
		e = tx.QueryRow(r.Context(), `INSERT INTO work_pauses(session_id,started_at,planned_stop,actor_id) VALUES($1,$2,$3,$4) RETURNING id::text`, id, now, until, u.ID).Scan(&pid)
		if e != nil {
			return nil, e
		}
		if _, e = tx.Exec(r.Context(), `UPDATE work_sessions SET version=version+1,updated_at=$2 WHERE id=$1`, id, now); e != nil {
			return nil, e
		}
		if e = a.audit(r.Context(), tx, u.ID, "work.pause", id, nil, map[string]any{"pause_id": pid, "started_at": now.UTC(), "planned_stop": until}); e != nil {
			return nil, e
		}
		return a.session(r.Context(), tx, id)
	})
	if e != nil {
		handleError(w, r, e)
		return
	}
	writeJSON(w, 200, v)
}
func (a *App) resume(w http.ResponseWriter, r *http.Request) {
	var in struct {
		PauseID string `json:"pause_id"`
	}
	if e := decode(r, &in); e != nil {
		handleError(w, r, e)
		return
	}
	if in.PauseID == "" {
		fail(w, r, 422, "VALIDATION", "Resume must identify the pause")
		return
	}
	id := r.PathValue("id")
	u := actor(r)
	v, e := a.command(r, "work.resume:"+id, in, func(tx pgx.Tx) (any, error) {
		state, _, e := lockOwned(r.Context(), tx, id, u)
		if e != nil {
			return nil, e
		}
		var start time.Time
		var planned, resumed *time.Time
		if e = tx.QueryRow(r.Context(), `SELECT started_at,planned_stop,resumed_at FROM work_pauses WHERE id=$1 AND session_id=$2 FOR UPDATE`, in.PauseID, id).Scan(&start, &planned, &resumed); e != nil {
			return nil, e
		}
		if resumed != nil {
			return a.session(r.Context(), tx, id)
		}
		if state != "RUNNING" {
			return nil, problem(409, "STATE_CONFLICT", "This work session has already stopped")
		}
		var now time.Time
		if e = tx.QueryRow(r.Context(), `SELECT clock_timestamp()`).Scan(&now); e != nil {
			return nil, e
		}
		if now.Before(start) {
			return nil, problem(409, "CLOCK_ANOMALY", "Database clock precedes pause. Resume was not recorded.")
		}
		stop := now
		if planned != nil && planned.Before(stop) {
			stop = *planned
		}
		if _, e = tx.Exec(r.Context(), `UPDATE work_pauses SET resumed_at=$2 WHERE id=$1`, in.PauseID, stop); e != nil {
			return nil, e
		}
		if _, e = tx.Exec(r.Context(), `UPDATE work_sessions SET version=version+1,updated_at=$2 WHERE id=$1`, id, now); e != nil {
			return nil, e
		}
		if e = a.audit(r.Context(), tx, u.ID, "work.resume", id, nil, map[string]any{"pause_id": in.PauseID, "resumed_at": stop.UTC()}); e != nil {
			return nil, e
		}
		return a.session(r.Context(), tx, id)
	})
	if e != nil {
		handleError(w, r, e)
		return
	}
	writeJSON(w, 200, v)
}
