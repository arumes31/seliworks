package app

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const sessionSelect = `SELECT to_jsonb(s)||jsonb_build_object('worker_name',u.name,'farm_name',f.name,'activity_name',coalesce(c.name,''),'recorded_farm_name',(SELECT name FROM farms WHERE id=s.recorded_farm_id),'recorded_worker_name',(SELECT name FROM users WHERE id=s.recorded_worker_id),'server_now',sample.now,'gross_us',(extract(epoch FROM (coalesce(s.effective_stop,sample.now)-s.effective_start))*1000000)::bigint,'elapsed_us',(extract(epoch FROM (coalesce(s.effective_stop,sample.now)-s.effective_start))*1000000)::bigint-coalesce(b.us,0)-s.additional_deduction_us,'break_us',coalesce(b.us,0),'net_us',(extract(epoch FROM (coalesce(s.effective_stop,sample.now)-s.effective_start))*1000000)::bigint-coalesce(b.us,0)-s.additional_deduction_us,'breaks',coalesce(b.items,'[]'::jsonb),'active_pause',CASE WHEN s.state='RUNNING' THEN (SELECT jsonb_build_object('id',p.id,'started_at',p.started_at,'planned_stop',p.planned_stop) FROM work_pauses p WHERE p.session_id=s.id AND p.resumed_at IS NULL AND (p.planned_stop IS NULL OR p.planned_stop>sample.now)) ELSE NULL END) FROM work_sessions s JOIN users u ON u.id=s.worker_id JOIN farms f ON f.id=s.farm_id LEFT JOIN activities c ON c.id=s.activity_id CROSS JOIN (SELECT clock_timestamp() AS now) sample LEFT JOIN LATERAL (SELECT sum((extract(epoch FROM (least(stopped_at,coalesce(s.effective_stop,sample.now))-started_at))*1000000)::bigint)::bigint AS us,jsonb_agg(jsonb_build_object('started_at',started_at,'stopped_at',least(stopped_at,coalesce(s.effective_stop,sample.now))) ORDER BY started_at) AS items FROM work_break_intervals WHERE session_id=s.id) b ON true `

func (a *App) session(ctx context.Context, q querier, id string) (map[string]any, error) {
	var raw []byte
	if e := q.QueryRow(ctx, sessionSelect+`WHERE s.id=$1 AND s.state<>'DELETED'`, id).Scan(&raw); e != nil {
		return nil, e
	}
	var v map[string]any
	e := json.Unmarshal(raw, &v)
	return v, e
}
func (a *App) registerCore(m *http.ServeMux) {
	m.HandleFunc("GET /api/v1/me", a.requireAuth(a.me))
	m.HandleFunc("PATCH /api/v1/me", a.requireAuth(a.updateMe))
	m.HandleFunc("POST /api/v1/me/password", a.requireAuth(a.password))
	m.HandleFunc("POST /api/v1/auth/logout", a.requireAuth(a.logout))
	m.HandleFunc("GET /api/v1/farms", a.requireAuth(a.farms))
	m.HandleFunc("GET /api/v1/activities", a.requireAuth(a.activities))
	m.HandleFunc("GET /api/v1/me/active-work-session", a.requireAuth(a.activeSession))
	m.HandleFunc("GET /api/v1/me/summary", a.requireAuth(a.summary))
	m.HandleFunc("GET /api/v1/work-sessions", a.requireAuth(a.listSessions))
	m.HandleFunc("GET /api/v1/work-sessions/{id}", a.requireAuth(a.getSession))
	m.HandleFunc("POST /api/v1/work-sessions/start", a.requireAuth(a.start))
	m.HandleFunc("POST /api/v1/work-sessions/{id}/stop", a.requireAuth(a.stop))
	m.HandleFunc("POST /api/v1/work-sessions/{id}/pause", a.requireAuth(a.pause))
	m.HandleFunc("POST /api/v1/work-sessions/{id}/resume", a.requireAuth(a.resume))
	m.HandleFunc("PATCH /api/v1/work-sessions/{id}/details", a.requireAuth(a.details))
	m.HandleFunc("POST /api/v1/work-sessions/{id}/submit", a.requireAuth(a.submit))
	m.HandleFunc("POST /api/v1/work-sessions/{id}/corrections", a.requireAuth(a.requestCorrection))
}
func (a *App) farms(w http.ResponseWriter, r *http.Request) {
	u := actor(r)
	projection := `jsonb_build_object('id',f.id,'name',f.name,'active',f.active,'version',f.version)`
	if u.IsAdmin {
		projection = `to_jsonb(f)`
	}
	items, e := queryItems(r.Context(), a.DB, `SELECT `+projection+` FROM farms f WHERE f.active AND ($2 OR f.assign_all_workers OR EXISTS(SELECT 1 FROM worker_farms wf WHERE wf.farm_id=f.id AND wf.worker_id=$1)) ORDER BY f.name`, u.ID, u.IsAdmin)
	if e != nil {
		handleError(w, r, e)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": len(items)})
}
func (a *App) activities(w http.ResponseWriter, r *http.Request) {
	items, e := queryItems(r.Context(), a.DB, `SELECT to_jsonb(c) FROM activities c WHERE active ORDER BY name`)
	if e != nil {
		handleError(w, r, e)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": len(items)})
}
func (a *App) activeSession(w http.ResponseWriter, r *http.Request) {
	var id string
	e := a.DB.QueryRow(r.Context(), `SELECT id::text FROM work_sessions WHERE worker_id=$1 AND stopped_at IS NULL`, actor(r).ID).Scan(&id)
	if e == pgx.ErrNoRows {
		writeJSON(w, 200, nil)
		return
	}
	if e != nil {
		handleError(w, r, e)
		return
	}
	v, e := a.session(r.Context(), a.DB, id)
	if e != nil {
		handleError(w, r, e)
		return
	}
	if v["gross_us"].(float64) < 0 {
		fail(w, r, 409, "CLOCK_ANOMALY", "Database clock precedes recorded work; administrator review required")
		return
	}
	writeJSON(w, 200, v)
}
func pageParams(r *http.Request) (int, int) {
	p, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if p < 1 {
		p = 1
	}
	n, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if n < 1 {
		n = 25
	}
	if n > 100 {
		n = 100
	}
	if p > 100000 {
		p = 100000
	}
	return p, n
}
func (a *App) listSessions(w http.ResponseWriter, r *http.Request) {
	u := actor(r)
	p, n := pageParams(r)
	q := r.URL.Query()
	if !u.IsAdmin && q.Get("billing") != "" {
		fail(w, r, 403, "FORBIDDEN", "Billing filters require administrator access")
		return
	}
	where := ` WHERE s.state<>'DELETED' AND ($1 OR s.worker_id=$2) AND ($3='' OR s.state=$3) AND ($4='' OR s.farm_id=NULLIF($4,'')::uuid) AND ($5='' OR s.worker_id=NULLIF($5,'')::uuid) AND ($6='' OR coalesce(s.effective_stop,'infinity')>(NULLIF($6,'')::date::timestamp AT TIME ZONE o.timezone)) AND ($7='' OR s.effective_start<(NULLIF($7,'')::date::timestamp AT TIME ZONE o.timezone)) AND ($8='' OR s.description ILIKE '%'||$8||'%' OR u.name ILIKE '%'||$8||'%' OR f.name ILIKE '%'||$8||'%')`
	args := []any{u.IsAdmin, u.ID, q.Get("state"), q.Get("farm_id"), q.Get("worker_id"), q.Get("from"), q.Get("to"), q.Get("search")}
	// Filter and sort before pagination; identifiers and clauses come only from this allow-list.
	switch q.Get("billing") {
	case "":
	case "billable":
		where += ` AND NOT s.non_billable`
	case "nonbillable":
		where += ` AND s.non_billable`
	case "unbilled":
		where += ` AND s.state='APPROVED' AND NOT s.non_billable AND (extract(epoch FROM(s.effective_stop-s.effective_start))*1000000)::bigint-coalesce((SELECT sum((extract(epoch FROM(b.stopped_at-b.started_at))*1000000)::bigint) FROM work_break_intervals b WHERE b.session_id=s.id),0)-s.additional_deduction_us>coalesce((SELECT sum(ia.net_us) FROM invoice_allocations ia WHERE ia.session_id=s.id AND ia.active),0)`
	case "reserved":
		where += ` AND EXISTS(SELECT 1 FROM invoice_allocations ia JOIN invoices i ON i.id=ia.invoice_id WHERE ia.session_id=s.id AND ia.active AND i.state='draft')`
	case "issued":
		where += ` AND EXISTS(SELECT 1 FROM invoice_allocations ia JOIN invoices i ON i.id=ia.invoice_id WHERE ia.session_id=s.id AND ia.active AND i.state='issued')`
	default:
		fail(w, r, 422, "VALIDATION", "Unsupported billing filter")
		return
	}
	order := `s.effective_start DESC,s.id`
	switch q.Get("sort") {
	case "", "newest":
	case "oldest":
		order = `s.effective_start,s.id`
	case "worker":
		order = `u.name,s.effective_start DESC,s.id`
	default:
		fail(w, r, 422, "VALIDATION", "Unsupported sort order")
		return
	}
	var total int
	e := a.DB.QueryRow(r.Context(), `SELECT count(*) FROM work_sessions s JOIN users u ON u.id=s.worker_id JOIN farms f ON f.id=s.farm_id CROSS JOIN organization o`+where, args...).Scan(&total)
	if e != nil {
		handleError(w, r, e)
		return
	}
	args = append(args, n, (p-1)*n)
	selection := sessionSelect
	if u.IsAdmin {
		selection = "SELECT " + adminPricingStatusSQL + "||" + strings.TrimPrefix(sessionSelect, "SELECT ")
	}
	items, e := queryItems(r.Context(), a.DB, selection+` CROSS JOIN organization o `+where+` ORDER BY `+order+` LIMIT $9 OFFSET $10`, args...)
	if e != nil {
		handleError(w, r, e)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": total})
}
func (a *App) getSession(w http.ResponseWriter, r *http.Request) {
	v, e := a.session(r.Context(), a.DB, r.PathValue("id"))
	if e != nil {
		handleError(w, r, e)
		return
	}
	u := actor(r)
	if v["worker_id"] != u.ID && !u.IsAdmin {
		fail(w, r, 404, "NOT_FOUND", "Record not found")
		return
	}
	writeJSON(w, 200, v)
}

// command serializes each durable command key independently of process or replica.
func (a *App) command(r *http.Request, operation string, payload any, transition func(pgx.Tx) (any, error)) (any, error) {
	key := r.Header.Get("Idempotency-Key")
	if len(key) < 8 || len(key) > 200 {
		return nil, problem(422, "IDEMPOTENCY_KEY", "Use an Idempotency-Key between 8 and 200 characters")
	}
	b, e := json.Marshal(payload)
	if e != nil {
		return nil, e
	}
	fingerprint := digest(string(b))
	u := actor(r)
	for attempt := 0; attempt < 3; attempt++ {
		tx, e := a.DB.Begin(r.Context())
		if e != nil {
			return nil, e
		}
		_, e = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, u.ID+":"+operation+":"+key)
		var fp string
		var raw []byte
		if e == nil {
			e = tx.QueryRow(r.Context(), `SELECT fingerprint,result FROM commands WHERE actor_id=$1 AND operation=$2 AND key=$3`, u.ID, operation, key).Scan(&fp, &raw)
		}
		if e == nil {
			tx.Rollback(r.Context())
			if fp != fingerprint {
				return nil, problem(409, "KEY_REUSE", "This command key was already used with a different request")
			}
			return json.RawMessage(raw), nil
		}
		if e != pgx.ErrNoRows {
			tx.Rollback(r.Context())
			return nil, e
		}
		v, e := transition(tx)
		if e == nil {
			raw, e = json.Marshal(v)
		}
		if e == nil {
			_, e = tx.Exec(r.Context(), `INSERT INTO commands(actor_id,operation,key,fingerprint,result) VALUES($1,$2,$3,$4,$5)`, u.ID, operation, key, fingerprint, raw)
		}
		if e == nil {
			e = tx.Commit(r.Context())
		} else {
			tx.Rollback(r.Context())
		}
		if e == nil {
			return v, nil
		}
		tx.Rollback(r.Context())
		if !retryable(e) {
			return nil, e
		}
	}
	return nil, problem(409, "CONFLICT", "Concurrent operation. Reconcile and retry the same command.")
}
func retryable(e error) bool { return errorClass(e) == "40001" || errorClass(e) == "40P01" }
func (a *App) start(w http.ResponseWriter, r *http.Request) {
	var in struct {
		FarmID string `json:"farm_id"`
	}
	if e := decode(r, &in); e != nil {
		handleError(w, r, e)
		return
	}
	u := actor(r)
	if !u.IsWorker {
		fail(w, r, 403, "FORBIDDEN", "Worker permission required")
		return
	}
	v, e := a.command(r, "work.start", in, func(tx pgx.Tx) (any, error) {
		if _, e := tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock_shared($1)`, farmAssignmentLock); e != nil {
			return nil, e
		}
		var active, worker bool
		if e := tx.QueryRow(r.Context(), `SELECT active,is_worker FROM users WHERE id=$1 FOR UPDATE`, u.ID).Scan(&active, &worker); e != nil {
			return nil, e
		}
		if !active || !worker {
			return nil, problem(403, "FORBIDDEN", "Worker is inactive")
		}
		var allowed bool
		if e := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM farms f WHERE f.id=$1 AND f.active AND (f.assign_all_workers OR EXISTS(SELECT 1 FROM worker_farms wf WHERE wf.farm_id=f.id AND wf.worker_id=$2)))`, in.FarmID, u.ID).Scan(&allowed); e != nil {
			return nil, e
		}
		if !allowed {
			return nil, problem(403, "FARM_NOT_ASSIGNED", "Choose an assigned active farm")
		}
		var existing bool
		if e := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM work_sessions WHERE worker_id=$1 AND stopped_at IS NULL)`, u.ID).Scan(&existing); e != nil {
			return nil, e
		}
		if existing {
			return nil, problem(409, "ALREADY_RUNNING", "A work session is already running. Refresh to recover it.")
		}
		var now time.Time
		if e := tx.QueryRow(r.Context(), `SELECT clock_timestamp()`).Scan(&now); e != nil {
			return nil, e
		}
		var anomaly bool
		if e := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM work_sessions WHERE worker_id=$1 AND state<>'DELETED' AND effective_stop>$2)`, u.ID, now).Scan(&anomaly); e != nil {
			return nil, e
		}
		if anomaly {
			return nil, problem(409, "CLOCK_ANOMALY", "Recorded work ends after the database clock. Administrator review required.")
		}
		var id string
		if e := tx.QueryRow(r.Context(), `INSERT INTO work_sessions(worker_id,farm_id,started_at,effective_start,created_by,updated_at,is_vacation_day) VALUES($1,$2,$3,$3,$1,$3,NULL) RETURNING id::text`, u.ID, in.FarmID, now).Scan(&id); e != nil {
			return nil, e
		}
		v, e := a.session(r.Context(), tx, id)
		if e != nil {
			return nil, e
		}
		if e = a.audit(r.Context(), tx, u.ID, "work.start", id, nil, v); e != nil {
			return nil, e
		}
		return v, nil
	})
	if e != nil {
		handleError(w, r, e)
		return
	}
	writeJSON(w, 200, v)
}
func lockOwned(ctx context.Context, tx pgx.Tx, id string, u User) (string, int64, error) {
	var owner, state string
	var version int64
	e := tx.QueryRow(ctx, `SELECT worker_id::text,state,version FROM work_sessions WHERE id=$1 AND state<>'DELETED' FOR UPDATE`, id).Scan(&owner, &state, &version)
	if e != nil {
		return "", 0, e
	}
	if owner != u.ID && !u.IsAdmin {
		return "", 0, problem(404, "NOT_FOUND", "Record not found")
	}
	return state, version, nil
}
func (a *App) stop(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Reason string `json:"reason"`
	}
	if e := decode(r, &in); e != nil {
		handleError(w, r, e)
		return
	}
	id := r.PathValue("id")
	u := actor(r)
	v, e := a.command(r, "work.stop:"+id, in, func(tx pgx.Tx) (any, error) {
		state, _, e := lockOwned(r.Context(), tx, id, u)
		if e != nil {
			return nil, e
		}
		var owner string
		if e = tx.QueryRow(r.Context(), `SELECT worker_id::text FROM work_sessions WHERE id=$1`, id).Scan(&owner); e != nil {
			return nil, e
		}
		if owner != u.ID && strings.TrimSpace(in.Reason) == "" {
			return nil, problem(422, "REASON_REQUIRED", "A reason is required to stop another worker's session")
		}
		if state != "RUNNING" {
			return a.session(r.Context(), tx, id)
		}
		var now, start time.Time
		if e = tx.QueryRow(r.Context(), `SELECT clock_timestamp(),started_at FROM work_sessions WHERE id=$1`, id).Scan(&now, &start); e != nil {
			return nil, e
		}
		if now.Before(start) {
			return nil, problem(409, "CLOCK_ANOMALY", "Database clock precedes start. No stop was recorded.")
		}
		var anomaly bool
		if e = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM work_pauses WHERE session_id=$1 AND (started_at>$2 OR resumed_at>$2))`, id, now).Scan(&anomaly); e != nil {
			return nil, e
		}
		if anomaly {
			return nil, problem(409, "CLOCK_ANOMALY", "Database clock precedes pause evidence. No stop was recorded.")
		}
		if _, e = tx.Exec(r.Context(), `UPDATE work_pauses SET resumed_at=least(coalesce(planned_stop,$2),$2) WHERE session_id=$1 AND resumed_at IS NULL`, id, now); e != nil {
			return nil, e
		}
		_, e = tx.Exec(r.Context(), `UPDATE work_sessions SET stopped_at=$2,effective_stop=$2,state='STOPPED_PENDING_DETAILS',version=version+1,updated_at=$2 WHERE id=$1 AND stopped_at IS NULL`, id, now)
		if e != nil {
			return nil, e
		}
		if _, e = tx.Exec(r.Context(), `INSERT INTO breaks(session_id,started_at,stopped_at) SELECT session_id,started_at,resumed_at FROM work_pauses WHERE session_id=$1 AND resumed_at>started_at`, id); e != nil {
			return nil, e
		}
		v, e := a.session(r.Context(), tx, id)
		if e != nil {
			return nil, e
		}
		if e = a.audit(r.Context(), tx, u.ID, "work.stop", id, nil, map[string]any{"session": v, "reason": in.Reason}); e != nil {
			return nil, e
		}
		return v, nil
	})
	if e != nil {
		handleError(w, r, e)
		return
	}
	writeJSON(w, 200, v)
}

type BreakInput struct {
	StartedAt time.Time `json:"started_at"`
	StoppedAt time.Time `json:"stopped_at"`
}
type DetailsInput struct {
	Version         int64        `json:"version"`
	ActivityID      string       `json:"activity_id"`
	Description     string       `json:"description"`
	Location        string       `json:"location"`
	NonBillable     bool         `json:"non_billable"`
	Breaks          []BreakInput `json:"breaks"`
	Vacation        *bool        `json:"is_vacation_day"`
	DayType         string       `json:"day_type"`
	Deduction       int64        `json:"additional_deduction_us"`
	DeductionReason string       `json:"deduction_reason"`
}

func (a *App) details(w http.ResponseWriter, r *http.Request) {
	var in DetailsInput
	if e := decode(r, &in); e != nil {
		handleError(w, r, e)
		return
	}
	dayType, dayError := resolveDayAnswer(in.Vacation, in.DayType)
	if dayError != nil {
		handleError(w, r, dayError)
		return
	}
	if strings.TrimSpace(in.Description) == "" || len(in.Description) > 4000 || len(in.Location) > 300 || len(in.Breaks) > 100 || in.Deduction < 0 || len(in.DeductionReason) > 2000 || in.Deduction > 0 && strings.TrimSpace(in.DeductionReason) == "" {
		fail(w, r, 422, "VALIDATION", "Choose an activity and describe the work (up to 4000 characters)")
		return
	}
	id := r.PathValue("id")
	tx, e := a.DB.Begin(r.Context())
	if e != nil {
		handleError(w, r, e)
		return
	}
	defer tx.Rollback(r.Context())
	state, version, e := lockOwned(r.Context(), tx, id, actor(r))
	if e == nil && version != in.Version {
		e = problem(409, "VERSION_CONFLICT", "This entry was changed elsewhere. Refresh before editing.")
	}
	if e == nil && state != "STOPPED_PENDING_DETAILS" && state != "REJECTED" {
		e = problem(409, "STATE_CONFLICT", "Only stopped or rejected work can be edited")
	}
	if e != nil {
		handleError(w, r, e)
		return
	}
	var valid bool
	e = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM activities WHERE id=$1 AND active)`, in.ActivityID).Scan(&valid)
	if e != nil {
		handleError(w, r, e)
		return
	}
	if !valid {
		fail(w, r, 422, "ACTIVITY_REQUIRED", "Choose an active activity")
		return
	}
	before, e := a.session(r.Context(), tx, id)
	if e != nil {
		handleError(w, r, e)
		return
	}
	_, e = tx.Exec(r.Context(), `UPDATE work_sessions SET activity_id=$2,description=$3,location=$4,non_billable=$5,is_vacation_day=$6,additional_deduction_us=$7,deduction_reason=$8,day_type=$9,version=version+1,updated_at=clock_timestamp() WHERE id=$1`, id, in.ActivityID, strings.TrimSpace(in.Description), in.Location, in.NonBillable, dayType == "VACATION", in.Deduction, strings.TrimSpace(in.DeductionReason), dayType)
	if e == nil {
		_, e = tx.Exec(r.Context(), `DELETE FROM breaks WHERE session_id=$1`, id)
	}
	for _, b := range in.Breaks {
		if e != nil {
			break
		}
		_, e = tx.Exec(r.Context(), `INSERT INTO breaks(session_id,started_at,stopped_at) VALUES($1,$2,$3)`, id, b.StartedAt, b.StoppedAt)
	}
	var v map[string]any
	if e == nil {
		v, e = a.session(r.Context(), tx, id)
	}
	if e == nil {
		e = a.audit(r.Context(), tx, actor(r).ID, "work.details", id, before, v)
	}
	if e == nil {
		e = tx.Commit(r.Context())
	}
	if e != nil {
		handleError(w, r, e)
		return
	}
	writeJSON(w, 200, v)
}
func (a *App) submit(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Version int64 `json:"version"`
	}
	if e := decode(r, &in); e != nil {
		handleError(w, r, e)
		return
	}
	id := r.PathValue("id")
	tx, e := a.DB.Begin(r.Context())
	if e != nil {
		handleError(w, r, e)
		return
	}
	defer tx.Rollback(r.Context())
	state, version, e := lockOwned(r.Context(), tx, id, actor(r))
	if e == nil && version != in.Version {
		e = problem(409, "VERSION_CONFLICT", "Entry changed. Refresh before submitting.")
	}
	if e == nil && state != "STOPPED_PENDING_DETAILS" && state != "REJECTED" {
		e = problem(409, "STATE_CONFLICT", "Entry cannot be submitted from this state")
	}
	if e == nil {
		_, e = tx.Exec(r.Context(), `UPDATE work_sessions SET state='SUBMITTED',version=version+1,updated_at=clock_timestamp() WHERE id=$1`, id)
	}
	if e == nil {
		e = a.audit(r.Context(), tx, actor(r).ID, "work.submit", id, state, "SUBMITTED")
	}
	var v map[string]any
	if e == nil {
		v, e = a.session(r.Context(), tx, id)
	}
	if e == nil {
		e = tx.Commit(r.Context())
	}
	if e != nil {
		handleError(w, r, e)
		return
	}
	writeJSON(w, 200, v)
}
func (a *App) requestCorrection(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Version int64     `json:"version"`
		Start   time.Time `json:"requested_start"`
		Stop    time.Time `json:"requested_stop"`
		Reason  string    `json:"reason"`
	}
	if e := decode(r, &in); e != nil {
		handleError(w, r, e)
		return
	}
	if strings.TrimSpace(in.Reason) == "" || len(in.Reason) > 2000 || in.Start.IsZero() || !in.Stop.After(in.Start) {
		fail(w, r, 422, "VALIDATION", "Provide explicit start/stop times and a reason")
		return
	}
	tx, e := a.DB.Begin(r.Context())
	if e != nil {
		handleError(w, r, e)
		return
	}
	defer tx.Rollback(r.Context())
	state, v, e := lockOwned(r.Context(), tx, r.PathValue("id"), actor(r))
	if e == nil && (state == "RUNNING" || v != in.Version) {
		e = problem(409, "STATE_CONFLICT", "Stop recording and refresh before requesting a correction")
	}
	var id string
	if e == nil {
		e = tx.QueryRow(r.Context(), `INSERT INTO corrections(session_id,requested_by,source_version,requested_start,requested_stop,reason) VALUES($1,$2,$3,$4,$5,$6) RETURNING id::text`, r.PathValue("id"), actor(r).ID, v, in.Start, in.Stop, in.Reason).Scan(&id)
	}
	if e == nil {
		e = a.audit(r.Context(), tx, actor(r).ID, "correction.request", id, nil, in)
	}
	if e == nil {
		e = tx.Commit(r.Context())
	}
	if e != nil {
		handleError(w, r, e)
		return
	}
	writeJSON(w, 201, map[string]any{"id": id, "state": "PENDING"})
}
func (a *App) summary(w http.ResponseWriter, r *http.Request) {
	var zone string
	var now time.Time
	if e := a.DB.QueryRow(r.Context(), `SELECT timezone,clock_timestamp() FROM organization`).Scan(&zone, &now); e != nil {
		handleError(w, r, e)
		return
	}
	loc, e := time.LoadLocation(zone)
	if e != nil {
		handleError(w, r, e)
		return
	}
	local := now.In(loc)
	out := map[string]any{"server_now": now.UTC(), "running_provisional": true}
	for key, from := range map[string]time.Time{"today_us": time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc), "month_us": time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, loc), "year_us": time.Date(local.Year(), 1, 1, 0, 0, 0, 0, loc)} {
		v, e := a.periodTotals(r.Context(), a.DB, actor(r).ID, from, now, now)
		if e != nil {
			handleError(w, r, e)
			return
		}
		out[key] = v.Net
	}
	writeJSON(w, 200, out)
}
func versionConflict() error {
	return problem(409, "VERSION_CONFLICT", "This record changed. Refresh and review before saving.")
}
func checkName(s string) bool { return strings.TrimSpace(s) != "" && len(s) <= 200 }
func mustJSON(v any) []byte {
	b, e := json.Marshal(v)
	if e != nil {
		panic(fmt.Sprintf("invalid internal JSON: %v", e))
	}
	return b
}
