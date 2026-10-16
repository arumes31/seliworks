package app

import (
	"encoding/json"
	"net/http"
)

func resolveDayAnswer(legacy *bool, kind string) (string, error) {
	if kind == "" && legacy != nil {
		if *legacy {
			return "VACATION", nil
		}
		return "REGULAR", nil
	}
	if kind != "REGULAR" && kind != "VACATION" && kind != "HOLIDAY" {
		return "", problem(422, "DAY_TYPE_REQUIRED", "Choose a regular workday, vacation day or public holiday")
	}
	if legacy != nil && *legacy != (kind == "VACATION") {
		return "", problem(422, "DAY_TYPE_CONFLICT", "Vacation flag and explicit day type must agree")
	}
	return kind, nil
}
func (a *App) listDayPricing(w http.ResponseWriter, r *http.Request) {
	items, e := queryItems(r.Context(), a.DB, `SELECT jsonb_build_object('day_type',day_type,'mode',mode,'value',value::text,'version',version) FROM special_day_pricing ORDER BY CASE day_type WHEN 'VACATION' THEN 0 ELSE 1 END`)
	if e != nil {
		handleError(w, r, e)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": len(items)})
}
func (a *App) saveDayPricing(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Version int64   `json:"version"`
		Mode    *string `json:"mode"`
		Value   *string `json:"value"`
	}
	if e := decode(r, &in); e != nil {
		handleError(w, r, e)
		return
	}
	kind := r.PathValue("day_type")
	if kind != "VACATION" && kind != "HOLIDAY" {
		fail(w, r, 404, "NOT_FOUND", "Surcharge policy not found")
		return
	}
	if in.Mode == nil {
		if in.Value != nil {
			fail(w, r, 422, "VALIDATION", "Clearing a surcharge requires null mode and value")
			return
		}
	} else {
		if (*in.Mode != "percentage" && *in.Mode != "hourly") || in.Value == nil {
			fail(w, r, 422, "VALIDATION", "Select percentage or additional amount per hour with a decimal value")
			return
		}
		if _, e := decimalUnits(*in.Value, 4); e != nil {
			fail(w, r, 422, "VALIDATION", "Use a nonnegative decimal with at most four fractional digits")
			return
		}
	}
	tx, e := a.DB.Begin(r.Context())
	if e != nil {
		handleError(w, r, e)
		return
	}
	defer tx.Rollback(r.Context())
	var before []byte
	var version int64
	e = tx.QueryRow(r.Context(), `SELECT to_jsonb(p)||jsonb_build_object('value',value::text),version FROM special_day_pricing p WHERE day_type=$1 FOR UPDATE`, kind).Scan(&before, &version)
	if e != nil {
		handleError(w, r, e)
		return
	}
	if version != in.Version {
		handleError(w, r, versionConflict())
		return
	}
	var after []byte
	e = tx.QueryRow(r.Context(), `UPDATE special_day_pricing SET mode=$2,value=$3,version=version+1 WHERE day_type=$1 RETURNING jsonb_build_object('day_type',day_type,'mode',mode,'value',value::text,'version',version)`, kind, in.Mode, in.Value).Scan(&after)
	if e == nil {
		e = a.audit(r.Context(), tx, actor(r).ID, "pricing.special_day_changed", kind, json.RawMessage(before), json.RawMessage(after))
	}
	if e == nil {
		e = tx.Commit(r.Context())
	}
	if e != nil {
		handleError(w, r, e)
		return
	}
	writeJSON(w, 200, json.RawMessage(after))
}
