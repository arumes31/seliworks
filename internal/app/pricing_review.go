package app

import (
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5"
)

func (a *App) reviewPricing(w http.ResponseWriter, r *http.Request) {
	tx, err := a.DB.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		handleError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	id := r.PathValue("id")
	var state string
	var version int64
	var activity, dayType *string
	if err = tx.QueryRow(r.Context(), `SELECT state,version,activity_id::text,day_type FROM work_sessions WHERE id=$1 AND state<>'DELETED' FOR SHARE`, id).Scan(&state, &version, &activity, &dayType); err != nil {
		handleError(w, r, err)
		return
	}
	value := map[string]any{"source_version": version, "state": state, "frozen": state == "APPROVED", "pricing": nil}
	if state == "RUNNING" || activity == nil || dayType == nil {
		value["exclusion_reason"] = "DETAILS_REQUIRED"
		writeJSON(w, 200, value)
		return
	}
	var snapshot any
	if state == "APPROVED" {
		var raw []byte
		err = tx.QueryRow(r.Context(), `SELECT snapshot FROM approvals WHERE session_id=$1 AND source_version=$2`, id, version).Scan(&raw)
		if err == pgx.ErrNoRows {
			err = problem(422, "PRICING_REVIEW_REQUIRED", "Approved work needs a matching frozen pricing snapshot. Review and approve it again")
		}
		if err == nil {
			snapshot = json.RawMessage(raw)
		}
	} else {
		snapshot, err = a.pricingSnapshot(r.Context(), tx, id)
	}
	if err != nil {
		handleError(w, r, err)
		return
	}
	value["pricing"] = snapshot
	claims, err := queryItems(r.Context(), tx, `SELECT jsonb_build_object('invoice_id',i.id,'number',i.number,'state',i.state,'started_at',al.started_at,'stopped_at',al.stopped_at) FROM invoice_allocations al JOIN invoices i ON i.id=al.invoice_id WHERE al.session_id=$1 AND al.active ORDER BY al.started_at`, id)
	if err != nil {
		handleError(w, r, err)
		return
	}
	value["reservations"] = claims
	writeJSON(w, 200, value)
}

// This projection is added only to administrator lists. Worker records keep the
// common non-financial session contract and never receive approval pricing.
const adminPricingStatusSQL = `jsonb_build_object('pricing_status',CASE WHEN s.non_billable THEN 'NON_BILLABLE' WHEN s.state<>'APPROVED' THEN 'APPROVAL_REQUIRED' WHEN (SELECT p.snapshot->>'priced' FROM approvals p WHERE p.session_id=s.id AND p.source_version=s.version)='true' THEN 'PRICED' ELSE coalesce((SELECT nullif(p.snapshot->>'unpriced_reason','') FROM approvals p WHERE p.session_id=s.id AND p.source_version=s.version),'RATE_REQUIRED') END)`
