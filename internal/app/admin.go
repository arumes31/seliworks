package app

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"net/http"
	"net/mail"
	"strings"
	"time"
)

func (a *App) registerAdmin(m *http.ServeMux) {
	m.HandleFunc("GET /api/v1/admin/timesheets.pdf", a.requireAdmin(a.adminTimesheets))
	m.HandleFunc("GET /api/v1/admin/work-sessions/{id}/pricing", a.requireAdmin(a.reviewPricing))
	m.HandleFunc("GET /api/v1/admin/export", a.requireAdmin(a.exportBusinessData))
	m.HandleFunc("GET /api/v1/admin/retention", a.requireAdmin(a.previewRetention))
	m.HandleFunc("POST /api/v1/admin/retention/cleanup", a.requireAdmin(a.cleanupRetention))
	m.HandleFunc("GET /api/v1/admin/workers", a.requireAdmin(a.workers))
	m.HandleFunc("POST /api/v1/admin/workers", a.requireAdmin(a.createWorker))
	m.HandleFunc("PATCH /api/v1/admin/workers/{id}", a.requireAdmin(a.updateWorker))
	m.HandleFunc("POST /api/v1/admin/workers/{id}/invite", a.requireAdmin(a.invite))
	m.HandleFunc("PUT /api/v1/admin/workers/{id}/farms", a.requireAdmin(a.assignFarms))
	for _, kind := range []string{"farms", "activities"} {
		m.HandleFunc("GET /api/v1/admin/"+kind, a.requireAdmin(a.listBusiness(kind)))
		m.HandleFunc("POST /api/v1/admin/"+kind, a.requireAdmin(a.saveBusiness(kind, false)))
		m.HandleFunc("PATCH /api/v1/admin/"+kind+"/{id}", a.requireAdmin(a.saveBusiness(kind, true)))
	}
	m.HandleFunc("POST /api/v1/admin/work-sessions/{id}/approve", a.requireAdmin(a.approve))
	m.HandleFunc("PATCH /api/v1/admin/work-sessions/{id}", a.requireAdmin(a.editWork))
	m.HandleFunc("DELETE /api/v1/admin/work-sessions/{id}", a.requireAdmin(a.deleteWork))
	m.HandleFunc("POST /api/v1/admin/work-sessions/{id}/reject", a.requireAdmin(a.reject))
	m.HandleFunc("POST /api/v1/admin/work-sessions/manual", a.requireAdmin(a.manual))
	m.HandleFunc("GET /api/v1/admin/corrections", a.requireAdmin(a.corrections))
	m.HandleFunc("POST /api/v1/admin/corrections/{id}/review", a.requireAdmin(a.reviewCorrection))
	m.HandleFunc("GET /api/v1/admin/settings", a.requireAdmin(a.settings))
	m.HandleFunc("PATCH /api/v1/admin/settings", a.requireAdmin(a.saveSettings))
	m.HandleFunc("GET /api/v1/admin/audit", a.requireAdmin(a.auditList))
	m.HandleFunc("GET /api/v1/admin/overview", a.requireAdmin(a.overview))
	m.HandleFunc("GET /api/v1/admin/special-day-pricing", a.requireAdmin(a.listDayPricing))
	m.HandleFunc("PATCH /api/v1/admin/special-day-pricing/{day_type}", a.requireAdmin(a.saveDayPricing))
}
func (a *App) workers(w http.ResponseWriter, r *http.Request) {
	items, e := queryItems(r.Context(), a.DB, `SELECT jsonb_build_object('id',u.id,'name',u.name,'email',u.email,'is_admin',u.is_admin,'is_worker',u.is_worker,'timesheet_eligible',`+timesheetEligible+`,'active',u.active,'language',u.language,'theme',u.theme,'version',u.version,'assignment_revision',`+workerAssignmentRevision+`,'farm_ids',coalesce((SELECT jsonb_agg(f.id ORDER BY f.id) FROM farms f WHERE (u.is_worker OR u.is_admin) AND (f.assign_all_workers OR EXISTS(SELECT 1 FROM worker_farms wf WHERE wf.farm_id=f.id AND wf.worker_id=u.id))),'[]'::jsonb),'running',EXISTS(SELECT 1 FROM work_sessions WHERE worker_id=u.id AND stopped_at IS NULL)) FROM users u ORDER BY name`)
	if e != nil {
		handleError(w, r, e)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": len(items)})
}
func validEmail(s string) bool {
	v, e := mail.ParseAddress(s)
	return e == nil && v.Address == s && len(s) <= 254
}
func (a *App) createWorker(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Password string `json:"password"`
		IsAdmin  bool   `json:"is_admin"`
		IsWorker bool   `json:"is_worker"`
	}
	if e := decode(r, &in); e != nil {
		handleError(w, r, e)
		return
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	if !checkName(in.Name) || !validEmail(in.Email) || !in.IsAdmin && !in.IsWorker {
		fail(w, r, 422, "VALIDATION", "Provide a name, email and at least one permission")
		return
	}
	hash := "setup-required$" + token()
	var e error
	if in.Password != "" {
		hash, e = hashPassword(in.Password)
	}
	if e != nil {
		handleError(w, r, e)
		return
	}
	tx, e := a.DB.Begin(r.Context())
	if e != nil {
		handleError(w, r, e)
		return
	}
	defer tx.Rollback(r.Context())
	var id string
	e = tx.QueryRow(r.Context(), `INSERT INTO users(name,email,password_hash,is_admin,is_worker,active) VALUES($1,$2,$3,$4,$5,$6) RETURNING id::text`, in.Name, in.Email, hash, in.IsAdmin, in.IsWorker, in.Password != "").Scan(&id)
	if e == nil {
		e = a.audit(r.Context(), tx, actor(r).ID, "user.create", id, nil, map[string]any{"name": in.Name, "email": in.Email, "is_admin": in.IsAdmin, "is_worker": in.IsWorker})
	}
	if e == nil {
		e = tx.Commit(r.Context())
	}
	if e != nil {
		handleError(w, r, e)
		return
	}
	writeJSON(w, 201, map[string]any{"id": id, "version": 1})
}
func (a *App) updateWorker(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Version  int64   `json:"version"`
		Name     *string `json:"name"`
		Email    *string `json:"email"`
		IsAdmin  *bool   `json:"is_admin"`
		IsWorker *bool   `json:"is_worker"`
		Active   *bool   `json:"active"`
	}
	if e := decode(r, &in); e != nil {
		handleError(w, r, e)
		return
	}
	if in.Name != nil && !checkName(*in.Name) || in.Email != nil && !validEmail(*in.Email) {
		fail(w, r, 422, "VALIDATION", "Invalid name or email")
		return
	}
	id := r.PathValue("id")
	tx, e := a.DB.Begin(r.Context())
	if e != nil {
		handleError(w, r, e)
		return
	}
	defer tx.Rollback(r.Context())
	if _, e = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(736954821)`); e != nil {
		handleError(w, r, e)
		return
	}
	var old User
	e = tx.QueryRow(r.Context(), `SELECT id::text,name,email,is_admin,is_worker,active,language,theme,version FROM users WHERE id=$1 FOR UPDATE`, id).Scan(&old.ID, &old.Name, &old.Email, &old.IsAdmin, &old.IsWorker, &old.Active, &old.Language, &old.Theme, &old.Version)
	if e != nil {
		handleError(w, r, e)
		return
	}
	if old.Version != in.Version {
		handleError(w, r, versionConflict())
		return
	}
	next := old
	if in.Name != nil {
		next.Name = *in.Name
	}
	if in.Email != nil {
		next.Email = strings.ToLower(*in.Email)
	}
	if in.IsAdmin != nil {
		next.IsAdmin = *in.IsAdmin
	}
	if in.IsWorker != nil {
		next.IsWorker = *in.IsWorker
	}
	if in.Active != nil {
		next.Active = *in.Active
	}
	if old.IsAdmin && old.Active && (!next.IsAdmin || !next.Active) {
		var count int
		e = tx.QueryRow(r.Context(), `SELECT count(*) FROM users WHERE is_admin AND active`).Scan(&count)
		if e != nil {
			handleError(w, r, e)
			return
		}
		if count <= 1 {
			fail(w, r, 409, "LAST_ADMIN", "Keep at least one active administrator")
			return
		}
	}
	_, e = tx.Exec(r.Context(), `UPDATE users SET name=$2,email=$3,is_admin=$4,is_worker=$5,active=$6,version=version+1 WHERE id=$1`, id, next.Name, next.Email, next.IsAdmin, next.IsWorker, next.Active)
	if e == nil {
		_, e = tx.Exec(r.Context(), `DELETE FROM auth_sessions WHERE user_id=$1`, id)
	}
	if e == nil && in.Active != nil && !*in.Active {
		_, e = tx.Exec(r.Context(), `DELETE FROM invitations WHERE user_id=$1 AND used_at IS NULL`, id)
	}
	if e == nil {
		e = a.audit(r.Context(), tx, actor(r).ID, "user.update", id, old, next)
	}
	if e == nil {
		e = tx.Commit(r.Context())
	}
	if e != nil {
		handleError(w, r, e)
		return
	}
	next.Version++
	writeJSON(w, 200, next)
}
func (a *App) assignFarms(w http.ResponseWriter, r *http.Request) {
	var in struct {
		FarmIDs                    []string `json:"farm_ids"`
		ExpectedAssignmentRevision *string  `json:"expected_assignment_revision"`
	}
	if e := decode(r, &in); e != nil {
		handleError(w, r, e)
		return
	}
	if e := validateAssignmentIDs(in.FarmIDs); e != nil {
		handleError(w, r, e)
		return
	}
	tx, e := a.DB.Begin(r.Context())
	if e != nil {
		handleError(w, r, e)
		return
	}
	defer tx.Rollback(r.Context())
	var id string
	e = lockFarmAssignments(r.Context(), tx)
	if e == nil {
		e = tx.QueryRow(r.Context(), `SELECT id::text FROM users WHERE id=$1 AND (is_worker OR is_admin) FOR UPDATE`, r.PathValue("id")).Scan(&id)
	}
	if e == nil && in.ExpectedAssignmentRevision != nil {
		var revision string
		e = tx.QueryRow(r.Context(), `SELECT `+workerAssignmentRevision+` FROM users u WHERE u.id=$1`, id).Scan(&revision)
		if e == nil && revision != *in.ExpectedAssignmentRevision {
			e = versionConflict()
		}
	}
	if e == nil {
		var count int
		e = tx.QueryRow(r.Context(), `SELECT count(*) FROM farms WHERE id=ANY($1::uuid[])`, in.FarmIDs).Scan(&count)
		if e == nil && count != len(in.FarmIDs) {
			e = problem(422, "VALIDATION", "Choose existing farms")
		}
	}
	if e == nil {
		// Invalidate open farm editors only where explicit membership actually changes.
		_, e = tx.Exec(r.Context(), `UPDATE farms f SET version=version+1 WHERE NOT f.assign_all_workers AND (EXISTS(SELECT 1 FROM worker_farms wf WHERE wf.farm_id=f.id AND wf.worker_id=$1) <> coalesce(f.id=ANY($2::uuid[]),false))`, id, in.FarmIDs)
	}
	if e == nil {
		_, e = tx.Exec(r.Context(), `DELETE FROM worker_farms WHERE worker_id=$1`, id)
	}
	if e == nil {
		_, e = tx.Exec(r.Context(), `INSERT INTO worker_farms(worker_id,farm_id) SELECT $1,id FROM farms WHERE id=ANY($2::uuid[]) AND NOT assign_all_workers`, id, in.FarmIDs)
	}
	if e == nil {
		e = a.audit(r.Context(), tx, actor(r).ID, "user.assignments", id, nil, in)
	}
	if e == nil {
		e = tx.Commit(r.Context())
	}
	if e != nil {
		handleError(w, r, e)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (a *App) listBusiness(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projection := `to_jsonb(v)`
		if kind == "farms" {
			projection += `||jsonb_build_object('worker_ids',coalesce((SELECT jsonb_agg(worker_id ORDER BY worker_id) FROM worker_farms WHERE farm_id=v.id),'[]'::jsonb))`
		}
		items, e := queryItems(r.Context(), a.DB, `SELECT `+projection+` FROM `+kind+` v ORDER BY name`)
		if e != nil {
			handleError(w, r, e)
			return
		}
		writeJSON(w, 200, map[string]any{"items": items, "total": len(items)})
	}
}
func (a *App) saveBusiness(kind string, update bool) http.HandlerFunc {
	if kind == "farms" {
		return a.saveFarm(update)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Name    string `json:"name"`
			Address string `json:"address"`
			Email   string `json:"email"`
			Active  *bool  `json:"active"`
			Version int64  `json:"version"`
		}
		if e := decode(r, &in); e != nil {
			handleError(w, r, e)
			return
		}
		if !checkName(in.Name) || len(in.Address) > 2000 || len(in.Email) > 254 {
			fail(w, r, 422, "VALIDATION", "Provide a name and valid billing fields")
			return
		}
		tx, e := a.DB.Begin(r.Context())
		if e != nil {
			handleError(w, r, e)
			return
		}
		defer tx.Rollback(r.Context())
		id := r.PathValue("id")
		active := true
		if in.Active != nil {
			active = *in.Active
		}
		var raw []byte
		if update {
			sql := `UPDATE ` + kind + ` SET name=$2,active=$3,version=version+1 WHERE id=$1 AND version=$4 RETURNING to_jsonb(` + kind + `)`
			args := []any{id, in.Name, active, in.Version}
			e = tx.QueryRow(r.Context(), sql, args...).Scan(&raw)
			if e == pgx.ErrNoRows {
				e = versionConflict()
			}
		} else {
			e = tx.QueryRow(r.Context(), `INSERT INTO activities(name,active) VALUES($1,$2) RETURNING to_jsonb(activities)`, in.Name, active).Scan(&raw)
		}
		var v map[string]any
		if e == nil {
			e = json.Unmarshal(raw, &v)
		}
		if e == nil {
			e = a.audit(r.Context(), tx, actor(r).ID, kind+".save", v["id"].(string), nil, v)
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
}
func noReservations(ctx context.Context, tx pgx.Tx, id string) error {
	var reserved bool
	e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM invoice_allocations WHERE session_id=$1 AND active)`, id).Scan(&reserved)
	if e != nil {
		return e
	}
	if reserved {
		return problem(409, "BILLING_LOCKED", "Cancel the draft or reverse the issued invoice through its financial workflow first")
	}
	return nil
}
func (a *App) approve(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Version int64  `json:"version"`
		Reason  string `json:"reason"`
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
		e = versionConflict()
	}
	if e == nil && state != "SUBMITTED" && state != "APPROVED" {
		e = problem(409, "STATE_CONFLICT", "Only submitted work can be approved")
	}
	if e == nil && state == "APPROVED" && strings.TrimSpace(in.Reason) == "" {
		e = problem(422, "REASON_REQUIRED", "Repricing requires an explicit reason and renewed review")
	}
	if e == nil {
		e = noReservations(r.Context(), tx, id)
	}
	var snapshot any
	if e == nil {
		snapshot, e = a.pricingSnapshot(r.Context(), tx, id)
	}
	if e == nil {
		_, e = tx.Exec(r.Context(), `INSERT INTO approvals(session_id,source_version,actor_id,snapshot) VALUES($1,$2,$3,$4)`, id, version+1, actor(r).ID, mustJSON(snapshot))
	}
	if e == nil {
		_, e = tx.Exec(r.Context(), `UPDATE work_sessions SET state='APPROVED',version=version+1,updated_at=clock_timestamp() WHERE id=$1`, id)
	}
	if e == nil {
		e = a.audit(r.Context(), tx, actor(r).ID, "work.approve", id, state, map[string]any{"snapshot": snapshot, "reason": in.Reason})
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
func (a *App) reject(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Version int64  `json:"version"`
		Reason  string `json:"reason"`
	}
	if e := decode(r, &in); e != nil {
		handleError(w, r, e)
		return
	}
	if strings.TrimSpace(in.Reason) == "" || len(in.Reason) > 2000 {
		fail(w, r, 422, "REASON_REQUIRED", "Give feedback for resubmission")
		return
	}
	id := r.PathValue("id")
	tx, e := a.DB.Begin(r.Context())
	if e != nil {
		handleError(w, r, e)
		return
	}
	defer tx.Rollback(r.Context())
	state, v, e := lockOwned(r.Context(), tx, id, actor(r))
	if e == nil && v != in.Version {
		e = versionConflict()
	}
	if e == nil && state != "SUBMITTED" && state != "APPROVED" {
		e = problem(409, "STATE_CONFLICT", "Only submitted or approved work can be rejected")
	}
	if e == nil {
		e = noReservations(r.Context(), tx, id)
	}
	if e == nil {
		_, e = tx.Exec(r.Context(), `UPDATE work_sessions SET state='REJECTED',rejection_reason=$2,version=version+1,updated_at=clock_timestamp() WHERE id=$1`, id, in.Reason)
	}
	if e == nil {
		e = a.audit(r.Context(), tx, actor(r).ID, "work.reject", id, state, in.Reason)
	}
	var result map[string]any
	if e == nil {
		result, e = a.session(r.Context(), tx, id)
	}
	if e == nil {
		e = tx.Commit(r.Context())
	}
	if e != nil {
		handleError(w, r, e)
		return
	}
	writeJSON(w, 200, result)
}
func (a *App) manual(w http.ResponseWriter, r *http.Request) {
	var in struct {
		WorkerID    string    `json:"worker_id"`
		FarmID      string    `json:"farm_id"`
		ActivityID  string    `json:"activity_id"`
		Start       time.Time `json:"started_at"`
		Stop        time.Time `json:"stopped_at"`
		Description string    `json:"description"`
		Reason      string    `json:"reason"`
	}
	if e := decode(r, &in); e != nil {
		handleError(w, r, e)
		return
	}
	if in.Start.IsZero() || !in.Stop.After(in.Start) || strings.TrimSpace(in.Description) == "" || strings.TrimSpace(in.Reason) == "" || len(in.Reason) > 2000 {
		fail(w, r, 422, "VALIDATION", "Provide explicit offset-bearing times, work details and justification")
		return
	}
	tx, e := a.DB.Begin(r.Context())
	if e != nil {
		handleError(w, r, e)
		return
	}
	defer tx.Rollback(r.Context())
	var valid bool
	e = tx.QueryRow(r.Context(), `SELECT is_worker FROM users WHERE id=$1 FOR UPDATE`, in.WorkerID).Scan(&valid)
	if e == nil && !valid {
		e = problem(422, "VALIDATION", "Choose a worker")
	}
	var now time.Time
	if e == nil {
		e = tx.QueryRow(r.Context(), `SELECT clock_timestamp()`).Scan(&now)
	}
	if e == nil && in.Stop.After(now) {
		e = problem(422, "FUTURE_WORK", "Manual work cannot end in the future")
	}
	var id string
	if e == nil {
		e = tx.QueryRow(r.Context(), `INSERT INTO work_sessions(worker_id,farm_id,activity_id,started_at,stopped_at,effective_start,effective_stop,state,description,created_by,updated_at) VALUES($1,$2,$3,$4,$5,$4,$5,'SUBMITTED',$6,$7,$8) RETURNING id::text`, in.WorkerID, in.FarmID, in.ActivityID, in.Start, in.Stop, in.Description, actor(r).ID, now).Scan(&id)
	}
	if e == nil {
		e = a.audit(r.Context(), tx, actor(r).ID, "work.manual", id, nil, in)
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
	writeJSON(w, 201, v)
}
func (a *App) corrections(w http.ResponseWriter, r *http.Request) {
	items, e := queryItems(r.Context(), a.DB, `SELECT to_jsonb(c)||jsonb_build_object('worker_name',u.name,'farm_name',f.name,'original_start',s.started_at,'original_stop',s.stopped_at,'effective_start',s.effective_start,'effective_stop',s.effective_stop) FROM corrections c JOIN work_sessions s ON s.id=c.session_id JOIN users u ON u.id=s.worker_id JOIN farms f ON f.id=s.farm_id WHERE s.state<>'DELETED' ORDER BY c.created_at DESC LIMIT 100`)
	if e != nil {
		handleError(w, r, e)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": len(items)})
}
func (a *App) reviewCorrection(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Approve bool   `json:"approve"`
		Reason  string `json:"reason"`
	}
	if e := decode(r, &in); e != nil {
		handleError(w, r, e)
		return
	}
	if strings.TrimSpace(in.Reason) == "" || len(in.Reason) > 2000 {
		fail(w, r, 422, "REASON_REQUIRED", "Provide a review reason")
		return
	}
	tx, e := a.DB.Begin(r.Context())
	if e != nil {
		handleError(w, r, e)
		return
	}
	defer tx.Rollback(r.Context())
	id := r.PathValue("id")
	var sid, owner, state string
	var version int64
	var start, stop time.Time
	e = tx.QueryRow(r.Context(), `SELECT session_id::text,source_version,requested_start,requested_stop,state FROM corrections WHERE id=$1 FOR UPDATE`, id).Scan(&sid, &version, &start, &stop, &state)
	if e != nil {
		handleError(w, r, e)
		return
	}
	if state != "PENDING" {
		fail(w, r, 409, "STATE_CONFLICT", "Correction already reviewed")
		return
	}
	e = tx.QueryRow(r.Context(), `SELECT worker_id::text FROM work_sessions WHERE id=$1`, sid).Scan(&owner)
	if e == nil {
		_, e = tx.Exec(r.Context(), `SELECT id FROM users WHERE id=$1 FOR UPDATE`, owner)
	}
	var v int64
	if e == nil {
		_, v, e = lockOwned(r.Context(), tx, sid, actor(r))
	}
	if e == nil && in.Approve && v != version {
		e = versionConflict()
	}
	var before, after map[string]any
	if e == nil {
		before, e = a.session(r.Context(), tx, sid)
	}
	if e == nil && in.Approve {
		e = noReservations(r.Context(), tx, sid)
	}
	if e == nil && in.Approve {
		var now time.Time
		e = tx.QueryRow(r.Context(), `SELECT clock_timestamp()`).Scan(&now)
		if e == nil && stop.After(now) {
			e = problem(422, "FUTURE_WORK", "Correction cannot end in the future")
		}
	}
	if e == nil && in.Approve {
		var invalid bool
		e = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM breaks WHERE session_id=$1 AND (started_at<$2 OR stopped_at>$3))`, sid, start, stop).Scan(&invalid)
		if e == nil && invalid {
			e = problem(422, "BREAK_CONFLICT", "Corrected interval would exclude an existing break. Reject and request compatible times.")
		}
	}
	if e == nil && in.Approve {
		_, e = tx.Exec(r.Context(), `UPDATE work_sessions SET effective_start=$2,effective_stop=$3,state=CASE WHEN description<>'' AND activity_id IS NOT NULL THEN 'SUBMITTED' ELSE 'STOPPED_PENDING_DETAILS' END,version=version+1,updated_at=clock_timestamp() WHERE id=$1`, sid, start, stop)
	}
	next := "REJECTED"
	if in.Approve {
		next = "APPROVED"
	}
	if e == nil {
		_, e = tx.Exec(r.Context(), `UPDATE corrections SET state=$2,reviewed_by=$3,review_reason=$4,reviewed_at=clock_timestamp() WHERE id=$1`, id, next, actor(r).ID, in.Reason)
	}
	if e == nil {
		after, e = a.session(r.Context(), tx, sid)
	}
	if e == nil {
		e = a.audit(r.Context(), tx, actor(r).ID, "correction."+strings.ToLower(next), sid, before, map[string]any{"session": after, "reason": in.Reason, "correction_id": id})
	}
	if e == nil {
		e = tx.Commit(r.Context())
	}
	if e != nil {
		handleError(w, r, e)
		return
	}
	writeJSON(w, 200, map[string]any{"id": id, "state": next, "session": after})
}
func (a *App) settings(w http.ResponseWriter, r *http.Request) {
	var raw []byte
	if e := a.DB.QueryRow(r.Context(), `SELECT (to_jsonb(o)-'id')||jsonb_build_object('tax_percent',tax_percent::text) FROM organization o`).Scan(&raw); e != nil {
		handleError(w, r, e)
		return
	}
	writeJSON(w, 200, json.RawMessage(raw))
}
func (a *App) saveSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name                  string  `json:"name"`
		Timezone              string  `json:"timezone"`
		Currency              string  `json:"currency"`
		Address               string  `json:"address"`
		Email                 string  `json:"email"`
		Bank                  string  `json:"bank_details"`
		Tax                   string  `json:"tax_percent"`
		TaxConfirmed          bool    `json:"tax_confirmed"`
		Prefix                string  `json:"invoice_prefix"`
		PaymentDays           int     `json:"payment_days"`
		LongHours             int     `json:"long_session_hours"`
		Version               int64   `json:"version"`
		TaxIdentifier         *string `json:"tax_identifier"`
		InvoiceFooter         *string `json:"invoice_footer"`
		InvoiceLayout         *string `json:"invoice_layout"`
		InvoiceAppendix       *bool   `json:"invoice_appendix"`
		SecurityRetentionDays *int    `json:"security_retention_days"`
		RetentionNotes        *string `json:"retention_notes"`
	}
	if e := decode(r, &in); e != nil {
		handleError(w, r, e)
		return
	}
	if in.TaxIdentifier != nil && len(*in.TaxIdentifier) > 200 || in.InvoiceFooter != nil && len(*in.InvoiceFooter) > 2000 || in.InvoiceLayout != nil && *in.InvoiceLayout != "entries" && *in.InvoiceLayout != "activity_rate" {
		fail(w, r, 422, "VALIDATION", "Check tax identifier, invoice footer and invoice layout")
		return
	}
	if in.SecurityRetentionDays != nil && (*in.SecurityRetentionDays < 1 || *in.SecurityRetentionDays > 3650) || in.RetentionNotes != nil && len(*in.RetentionNotes) > 2000 {
		fail(w, r, 422, "VALIDATION", "Use a security retention period from 1 to 3650 days and policy notes up to 2000 characters")
		return
	}
	if _, e := time.LoadLocation(in.Timezone); e != nil || !checkName(in.Name) || len(in.Address) > 2000 || len(in.Bank) > 2000 || len(in.Prefix) < 1 || len(in.Prefix) > 20 || in.PaymentDays < 0 || in.PaymentDays > 365 || in.LongHours < 1 || in.LongHours > 744 {
		fail(w, r, 422, "VALIDATION", "Check organization settings, timezone and invoice fields")
		return
	}
	tx, e := a.DB.Begin(r.Context())
	if e != nil {
		handleError(w, r, e)
		return
	}
	defer tx.Rollback(r.Context())
	var before []byte
	e = tx.QueryRow(r.Context(), `SELECT to_jsonb(o) FROM organization o FOR UPDATE`).Scan(&before)
	var raw []byte
	if e == nil {
		e = tx.QueryRow(r.Context(), `UPDATE organization SET name=$1,timezone=$2,currency=$3,address=$4,email=$5,bank_details=$6,tax_percent=$7,tax_confirmed=$8,invoice_prefix=$9,payment_days=$10,long_session_hours=$11,tax_identifier=coalesce($13,tax_identifier),invoice_footer=coalesce($14,invoice_footer),invoice_layout=coalesce($15,invoice_layout),invoice_appendix=coalesce($16,invoice_appendix),security_retention_days=coalesce($17,security_retention_days),retention_notes=coalesce($18,retention_notes),version=version+1 WHERE version=$12 RETURNING to_jsonb(organization)`, in.Name, in.Timezone, in.Currency, in.Address, in.Email, in.Bank, in.Tax, in.TaxConfirmed, in.Prefix, in.PaymentDays, in.LongHours, in.Version, in.TaxIdentifier, in.InvoiceFooter, in.InvoiceLayout, in.InvoiceAppendix, in.SecurityRetentionDays, in.RetentionNotes).Scan(&raw)
		if e == pgx.ErrNoRows {
			e = versionConflict()
		}
	}
	if e == nil {
		e = a.audit(r.Context(), tx, actor(r).ID, "organization.update", "organization", json.RawMessage(before), json.RawMessage(raw))
	}
	if e == nil {
		e = tx.Commit(r.Context())
	}
	if e != nil {
		handleError(w, r, e)
		return
	}
	writeJSON(w, 200, json.RawMessage(raw))
}
func (a *App) auditList(w http.ResponseWriter, r *http.Request) {
	p, n := pageParams(r)
	items, e := queryItems(r.Context(), a.DB, `SELECT to_jsonb(a)||jsonb_build_object('actor_name',u.name) FROM audit_events a LEFT JOIN users u ON u.id=a.actor_id ORDER BY a.id DESC LIMIT $1 OFFSET $2`, n, (p-1)*n)
	if e != nil {
		handleError(w, r, e)
		return
	}
	var total int
	if e = a.DB.QueryRow(r.Context(), `SELECT count(*) FROM audit_events`).Scan(&total); e != nil {
		handleError(w, r, e)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": total})
}
