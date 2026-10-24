package app

import (
	"net/http"
	"strings"
)

// deleteWork retains source evidence while removing the entry from operational views.
func (a *App) deleteWork(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Version int64  `json:"version"`
		Reason  string `json:"reason"`
	}
	if err := decode(r, &in); err != nil {
		handleError(w, r, err)
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if in.Reason == "" || len(in.Reason) > 2000 || in.Version < 1 {
		fail(w, r, 422, "VALIDATION", "A current version and deletion reason (up to 2000 bytes) are required")
		return
	}
	tx, err := a.DB.Begin(r.Context())
	if err != nil {
		handleError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	id := r.PathValue("id")
	state, version, err := lockOwned(r.Context(), tx, id, actor(r))
	if err == nil && version != in.Version {
		err = versionConflict()
	}
	if err == nil && state == "RUNNING" {
		err = problem(409, "STATE_CONFLICT", "Stop running work before deleting it")
	}
	if err == nil {
		err = noReservations(r.Context(), tx, id)
	}
	var before map[string]any
	if err == nil {
		before, err = a.session(r.Context(), tx, id)
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `UPDATE work_sessions SET state='DELETED',version=version+1,updated_at=clock_timestamp() WHERE id=$1`, id)
	}
	if err == nil {
		err = a.audit(r.Context(), tx, actor(r).ID, "work.deleted", id, before, map[string]any{"state": "DELETED", "version": version + 1, "reason": in.Reason})
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		handleError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
