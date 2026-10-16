package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func (a *App) registerBilling(mux *http.ServeMux) {
	routes := map[string]http.HandlerFunc{
		"GET /api/v1/admin/rates": a.listRates, "POST /api/v1/admin/rates": a.saveRate, "PATCH /api/v1/admin/rates/{id}": a.saveRate,
		"GET /api/v1/admin/reports": a.report, "GET /api/v1/admin/reports.csv": a.report,
		"GET /api/v1/admin/invoices": a.listInvoices, "POST /api/v1/admin/invoices": a.createInvoice, "GET /api/v1/admin/invoices/{id}": a.getInvoice,
		"POST /api/v1/admin/invoices/{id}/issue": a.issueInvoice, "POST /api/v1/admin/invoices/{id}/cancel": a.cancelInvoice, "POST /api/v1/admin/invoices/{id}/reverse": a.reverseInvoice,
		"POST /api/v1/admin/invoices/{id}/payments": a.addPayment, "GET /api/v1/admin/invoices/{id}/pdf": a.invoicePDF,
	}
	for path, h := range routes {
		mux.HandleFunc(path, a.requireAdmin(h))
	}
}
func (a *App) listRates(w http.ResponseWriter, r *http.Request) {
	items, err := queryItems(r.Context(), a.DB, `SELECT jsonb_build_object('id',r.id,'worker_id',worker_id,'farm_id',farm_id,'activity_id',activity_id,'hourly_rate',hourly_rate::text,'effective_from',effective_from,'effective_to',effective_to,'version',version) FROM rates r ORDER BY effective_from DESC LIMIT 1000`)
	if err != nil {
		handleError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": len(items)})
}
func (a *App) saveRate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Worker   *string `json:"worker_id"`
		Farm     *string `json:"farm_id"`
		Activity *string `json:"activity_id"`
		Rate     string  `json:"hourly_rate"`
		From     string  `json:"effective_from"`
		To       *string `json:"effective_to"`
		Version  int64   `json:"version"`
	}
	if err := decode(r, &in); err != nil {
		handleError(w, r, err)
		return
	}
	units, err := decimalUnits(in.Rate, 4)
	if err != nil || units.Sign() < 0 {
		fail(w, r, 422, "VALIDATION", "Rate requires a nonnegative decimal with at most four decimal places")
		return
	}
	start, err := time.Parse(time.RFC3339Nano, in.From)
	if err != nil {
		fail(w, r, 422, "VALIDATION", "Effective start requires an explicit timezone offset")
		return
	}
	var stop *time.Time
	if in.To != nil && strings.TrimSpace(*in.To) != "" {
		v, e := time.Parse(time.RFC3339Nano, *in.To)
		if e != nil || !v.After(start) {
			fail(w, r, 422, "VALIDATION", "Effective end must follow start")
			return
		}
		stop = &v
	}
	if in.Activity != nil && (in.Worker == nil || in.Farm == nil) {
		fail(w, r, 422, "VALIDATION", "Activity rates require both a worker and farm")
		return
	}
	tx, err := a.DB.Begin(r.Context())
	if err != nil {
		handleError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	id := r.PathValue("id")
	var before []byte
	if id == "" {
		err = tx.QueryRow(r.Context(), `INSERT INTO rates(worker_id,farm_id,activity_id,hourly_rate,effective_from,effective_to) VALUES($1,$2,$3,$4,$5,$6) RETURNING id::text`, in.Worker, in.Farm, in.Activity, in.Rate, start, stop).Scan(&id)
	} else {
		err = tx.QueryRow(r.Context(), `SELECT to_jsonb(r) FROM rates r WHERE id=$1 FOR UPDATE`, id).Scan(&before)
		if err == nil {
			var version int64
			err = tx.QueryRow(r.Context(), `UPDATE rates SET worker_id=$2,farm_id=$3,activity_id=$4,hourly_rate=$5,effective_from=$6,effective_to=$7,version=version+1 WHERE id=$1 AND version=$8 RETURNING version`, id, in.Worker, in.Farm, in.Activity, in.Rate, start, stop, in.Version).Scan(&version)
			if errors.Is(err, pgx.ErrNoRows) {
				err = problem(409, "VERSION_CONFLICT", "Rate changed; refresh before editing")
			}
		}
	}
	if err == nil {
		var prior any
		if len(before) > 0 {
			err = json.Unmarshal(before, &prior)
		}
		if err == nil {
			err = a.audit(r.Context(), tx, actor(r).ID, "rate.saved", id, prior, in)
		}
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		handleError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"id": id, "ok": true})
}
