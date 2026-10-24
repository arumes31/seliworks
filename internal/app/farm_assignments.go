package app

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Assignment writers serialize before taking row locks. Starts take the shared
// lock first, so a committed revocation cannot race a new work session.
const farmAssignmentLock int64 = 7351041

func validateAssignmentIDs(ids []string) error {
	if len(ids) > 1000 {
		return problem(422, "VALIDATION", "Too many assignments")
	}
	seen := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		parsed, err := uuid.Parse(id)
		if err != nil || parsed.String() != strings.ToLower(id) || seen[parsed] {
			return problem(422, "VALIDATION", "Choose distinct valid assignments")
		}
		seen[parsed] = true
	}
	return nil
}

func lockFarmAssignments(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, farmAssignmentLock)
	return err
}

// Includes inheritance mode so a switch from all to selected invalidates an
// employee editor even if the employee still has access to the same farms.
const workerAssignmentRevision = `md5(coalesce((SELECT string_agg(f.id::text||':'||f.assign_all_workers::text,',' ORDER BY f.id) FROM farms f WHERE u.is_worker AND (f.assign_all_workers OR EXISTS(SELECT 1 FROM worker_farms wf WHERE wf.farm_id=f.id AND wf.worker_id=u.id))),''))`

const farmWithWorkers = `SELECT to_jsonb(f)||jsonb_build_object('worker_ids',coalesce((SELECT jsonb_agg(worker_id ORDER BY worker_id) FROM worker_farms WHERE farm_id=f.id),'[]'::jsonb)) FROM farms f WHERE f.id=$1`

func (a *App) saveFarm(update bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Name             string    `json:"name"`
			Address          string    `json:"address"`
			Email            string    `json:"email"`
			Active           *bool     `json:"active"`
			Version          int64     `json:"version"`
			AssignAllWorkers *bool     `json:"assign_all_workers"`
			WorkerIDs        *[]string `json:"worker_ids"`
		}
		if err := decode(r, &in); err != nil {
			handleError(w, r, err)
			return
		}
		if !checkName(in.Name) || len(in.Address) > 2000 || len(in.Email) > 254 {
			fail(w, r, 422, "VALIDATION", "Provide a name and valid billing fields")
			return
		}
		if in.WorkerIDs != nil {
			if err := validateAssignmentIDs(*in.WorkerIDs); err != nil {
				handleError(w, r, err)
				return
			}
		}
		tx, err := a.DB.Begin(r.Context())
		if err != nil {
			handleError(w, r, err)
			return
		}
		defer tx.Rollback(r.Context())
		err = lockFarmAssignments(r.Context(), tx)
		if err == nil && in.WorkerIDs != nil {
			var count int
			err = tx.QueryRow(r.Context(), `SELECT count(*) FROM users WHERE id=ANY($1::uuid[]) AND is_worker`, *in.WorkerIDs).Scan(&count)
			if err == nil && count != len(*in.WorkerIDs) {
				err = problem(422, "VALIDATION", "Choose existing workers")
			}
		}
		id := r.PathValue("id")
		var before json.RawMessage
		if err == nil && update {
			err = tx.QueryRow(r.Context(), farmWithWorkers, id).Scan(&before)
		}
		if err == nil {
			if update {
				err = tx.QueryRow(r.Context(), `UPDATE farms SET name=$2,address=$3,email=$4,active=coalesce($5,active),assign_all_workers=coalesce($6,assign_all_workers),version=version+1 WHERE id=$1 AND version=$7 RETURNING id::text`, id, in.Name, in.Address, in.Email, in.Active, in.AssignAllWorkers, in.Version).Scan(&id)
				if err == pgx.ErrNoRows {
					err = versionConflict()
				}
			} else {
				err = tx.QueryRow(r.Context(), `INSERT INTO farms(name,address,email,active,assign_all_workers) VALUES($1,$2,$3,coalesce($4,true),coalesce($5,true)) RETURNING id::text`, in.Name, in.Address, in.Email, in.Active, in.AssignAllWorkers).Scan(&id)
			}
		}
		if err == nil && (in.AssignAllWorkers != nil || in.WorkerIDs != nil || !update) {
			// All-worker farms have no redundant explicit memberships.
			_, err = tx.Exec(r.Context(), `DELETE FROM worker_farms WHERE farm_id=$1 AND ($2 OR (SELECT assign_all_workers FROM farms WHERE id=$1))`, id, in.WorkerIDs != nil)
			if err == nil && in.WorkerIDs != nil {
				_, err = tx.Exec(r.Context(), `INSERT INTO worker_farms(worker_id,farm_id) SELECT u.id,f.id FROM users u CROSS JOIN farms f WHERE u.id=ANY($2::uuid[]) AND f.id=$1 AND NOT f.assign_all_workers ON CONFLICT DO NOTHING`, id, *in.WorkerIDs)
			}
		}
		var result json.RawMessage
		if err == nil {
			err = tx.QueryRow(r.Context(), farmWithWorkers, id).Scan(&result)
		}
		if err == nil {
			err = a.audit(r.Context(), tx, actor(r).ID, "farms.save", id, before, result)
		}
		if err == nil {
			err = tx.Commit(r.Context())
		}
		if err != nil {
			handleError(w, r, err)
			return
		}
		writeJSON(w, 200, result)
	}
}
