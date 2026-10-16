package app

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

var retentionRelations = []struct{ table, timestamp string }{
	{"auth_sessions", "expires_at"},
	{"invitations", "expires_at"},
	{"auth_limits", "window_start"},
}

func retentionState(ctx context.Context, q querier) (map[string]any, error) {
	var days int
	var version int64
	var now time.Time
	if err := q.QueryRow(ctx, `SELECT security_retention_days,version,clock_timestamp() FROM organization`).Scan(&days, &version, &now); err != nil {
		return nil, err
	}
	cutoff := now.UTC().AddDate(0, 0, -days)
	counts := map[string]int64{}
	for _, relation := range retentionRelations {
		var count int64
		// Identifiers are constants above, never request input.
		if err := q.QueryRow(ctx, `SELECT count(*) FROM `+relation.table+` WHERE `+relation.timestamp+`<$1`, cutoff).Scan(&count); err != nil {
			return nil, err
		}
		counts[relation.table] = count
	}
	return map[string]any{"version": version, "security_retention_days": days, "cutoff": cutoff, "candidates": counts}, nil
}

func (a *App) previewRetention(w http.ResponseWriter, r *http.Request) {
	tx, err := a.DB.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		handleError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	value, err := retentionState(r.Context(), tx)
	if err != nil {
		handleError(w, r, err)
		return
	}
	writeJSON(w, 200, value)
}

func (a *App) cleanupRetention(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Version   int64 `json:"version"`
		Confirmed bool  `json:"confirmed"`
	}
	if err := decode(r, &in); err != nil {
		handleError(w, r, err)
		return
	}
	if !in.Confirmed {
		fail(w, r, 422, "CONFIRMATION_REQUIRED", "Confirm cleanup of expired security data after reviewing the preview")
		return
	}
	value, err := a.command(r, "retention.cleanup", in, func(tx pgx.Tx) (any, error) {
		var version int64
		if err := tx.QueryRow(r.Context(), `SELECT version FROM organization FOR SHARE`).Scan(&version); err != nil {
			return nil, err
		}
		if version != in.Version {
			return nil, versionConflict()
		}
		state, err := retentionState(r.Context(), tx)
		if err != nil {
			return nil, err
		}
		deleted := map[string]int64{}
		for _, relation := range retentionRelations {
			tag, err := tx.Exec(r.Context(), `DELETE FROM `+relation.table+` WHERE `+relation.timestamp+`<$1`, state["cutoff"])
			if err != nil {
				return nil, err
			}
			deleted[relation.table] = tag.RowsAffected()
		}
		state["deleted"] = deleted
		if err = a.audit(r.Context(), tx, actor(r).ID, "retention.security_cleaned", "organization", nil, state); err != nil {
			return nil, err
		}
		return state, nil
	})
	if err != nil {
		handleError(w, r, err)
		return
	}
	writeJSON(w, 200, value)
}
