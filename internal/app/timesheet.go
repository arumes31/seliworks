package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type timesheetEntry struct {
	Farm, Activity, Description, State, DayType, DeductionReason string
	Start, Stop                                                  time.Time
	GrossUS, BreakUS, DeductionUS, NetUS                         int64
}
type timesheetDocument struct {
	Organization, Worker, From, To, Timezone, Language string
	Items                                              []timesheetEntry
}

func timesheetDateRange(from, to, zone string) (time.Time, time.Time, error) {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	start, e1 := time.ParseInLocation("2006-01-02", from, loc)
	end, e2 := time.ParseInLocation("2006-01-02", to, loc)
	stop := end.AddDate(0, 0, 1)
	if e1 != nil || e2 != nil || end.Before(start) || stop.After(start.AddDate(1, 0, 0)) || end.Year() > 9998 || start.Year() < 1 {
		return time.Time{}, time.Time{}, problem(422, "TIMESHEET_PERIOD", "Select valid from/to dates covering at most one year; both dates are included")
	}
	return start, stop, nil
}

func (a *App) personalTimesheet(w http.ResponseWriter, r *http.Request) {
	a.serveTimesheet(w, r, false)
}

func (a *App) adminTimesheets(w http.ResponseWriter, r *http.Request) {
	a.serveTimesheet(w, r, true)
}

const timesheetEligible = `(u.is_worker OR EXISTS(SELECT 1 FROM work_sessions s WHERE s.worker_id=u.id))`

func (a *App) serveTimesheet(w http.ResponseWriter, r *http.Request, admin bool) {
	q := r.URL.Query()
	for key, values := range q {
		if admin && key == "worker_id" {
			continue
		}
		if (key != "from" && key != "to" && key != "lang" && !(admin && key == "all")) || len(values) != 1 {
			fail(w, r, 422, "VALIDATION", "Unsupported or repeated export parameter")
			return
		}
	}
	ids := []string{actor(r).ID}
	all := false
	if admin {
		ids = q["worker_id"]
		all = q.Get("all") == "true"
		if (q.Has("all") && !all) || (all && len(ids) != 0) || (!all && len(ids) == 0) || len(ids) > 100 {
			fail(w, r, 422, "TIMESHEET_WORKERS", "Choose all employees or up to 100 distinct employees")
			return
		}
		seen := make(map[uuid.UUID]bool)
		for i, id := range ids {
			parsed, e := uuid.Parse(id)
			if e != nil || parsed.String() != strings.ToLower(id) || seen[parsed] {
				fail(w, r, 422, "TIMESHEET_WORKERS", "Choose distinct valid employees")
				return
			}
			seen[parsed] = true
			ids[i] = parsed.String()
		}
	}
	lang := q.Get("lang")
	if lang == "" {
		lang = actor(r).Language
	}
	if lang == "" {
		lang = "de"
	}
	if lang != "de" && lang != "en" {
		fail(w, r, 422, "VALIDATION", "Language must be de or en")
		return
	}
	tx, err := a.DB.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		handleError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	d := timesheetDocument{Worker: actor(r).Name, From: q.Get("from"), To: q.Get("to"), Language: lang}
	if err = tx.QueryRow(r.Context(), `SELECT name,timezone FROM organization WHERE id`).Scan(&d.Organization, &d.Timezone); err != nil {
		handleError(w, r, err)
		return
	}
	start, stop, err := timesheetDateRange(d.From, d.To, d.Timezone)
	if err != nil {
		handleError(w, r, err)
		return
	}
	docs := []timesheetDocument{d}
	indices := map[string]int{actor(r).ID: 0}
	if admin {
		workers, e := tx.Query(r.Context(), `SELECT u.id::text,u.name,u.email FROM users u WHERE `+timesheetEligible+` AND ($1 OR u.id=ANY($2::uuid[])) ORDER BY u.name,u.id LIMIT 101`, all, ids)
		if e != nil {
			handleError(w, r, e)
			return
		}
		docs = nil
		indices = make(map[string]int)
		selected := []string{}
		for workers.Next() {
			var id, name, email string
			if e = workers.Scan(&id, &name, &email); e != nil {
				break
			}
			section := d
			section.Worker = name + " (" + email + ")"
			indices[id] = len(docs)
			docs = append(docs, section)
			selected = append(selected, id)
		}
		if e == nil {
			e = workers.Err()
		}
		workers.Close()
		if e != nil {
			handleError(w, r, e)
			return
		}
		if len(docs) == 0 || len(docs) > 100 || (!all && len(docs) != len(ids)) {
			fail(w, r, 422, "TIMESHEET_WORKERS", "Choose between 1 and 100 existing employees")
			return
		}
		ids = selected
	}
	rows, err := tx.Query(r.Context(), `SELECT s.worker_id::text,f.name,concat_ws(E'\n',array_to_string(wa.names,', '),nullif(s.additional_activities,'')),s.description,s.state,coalesce(s.day_type,''),s.deduction_reason,s.effective_start,s.effective_stop,s.additional_deduction_us,
 coalesce((SELECT jsonb_agg(jsonb_build_object('started_at',b.started_at,'stopped_at',b.stopped_at)) FROM work_break_intervals b WHERE b.session_id=s.id),'[]'::jsonb)
 FROM work_sessions s JOIN farms f ON f.id=s.farm_id ` + workActivitiesJoin + `
 WHERE s.worker_id=ANY($1::uuid[]) AND s.state NOT IN ('RUNNING','DELETED') AND s.effective_stop IS NOT NULL AND s.effective_start<$3 AND s.effective_stop>$2
 ORDER BY s.effective_start,s.id LIMIT 2001`, ids, start, stop)
	if err != nil {
		handleError(w, r, err)
		return
	}
	defer rows.Close()
	textBytes := 0
	entryCount := 0
	for rows.Next() {
		var e timesheetEntry
		var workerID string
		var sourceStart, sourceStop time.Time
		var deduction int64
		var raw []byte
		var breaks []priceInterval
		if err = rows.Scan(&workerID, &e.Farm, &e.Activity, &e.Description, &e.State, &e.DayType, &e.DeductionReason, &sourceStart, &sourceStop, &deduction, &raw); err != nil {
			handleError(w, r, err)
			return
		}
		if err = json.Unmarshal(raw, &breaks); err != nil {
			handleError(w, r, err)
			return
		}
		e.Start, e.Stop = clippedInterval(sourceStart, sourceStop, start, stop)
		e.GrossUS = e.Stop.Sub(e.Start).Microseconds()
		rawNet := netMicroseconds(e.Start, e.Stop, breaks)
		e.BreakUS = e.GrossUS - rawNet
		e.DeductionUS = allocatedDeductionUS(sourceStart, sourceStop, e.Start, e.Stop, breaks, deduction)
		e.NetUS = rawNet - e.DeductionUS
		textBytes += len(e.Farm) + len(e.Activity) + len(e.Description) + len(e.DeductionReason)
		index := indices[workerID]
		docs[index].Items = append(docs[index].Items, e)
		entryCount++
		if entryCount > 2000 || textBytes > 1_000_000 {
			fail(w, r, 422, "TIMESHEET_TOO_LARGE", "Too many entries or too much text; select a shorter period")
			return
		}
	}
	if err = rows.Err(); err != nil {
		handleError(w, r, err)
		return
	}
	rows.Close()
	if err = tx.Commit(r.Context()); err != nil {
		handleError(w, r, err)
		return
	}
	if entryCount == 0 && !admin {
		fail(w, r, 422, "TIMESHEET_EMPTY", "No completed work entries in this period")
		return
	}
	raw, err := renderTimesheetPDFs(docs, admin)
	if err != nil {
		handleError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="timesheet-%s-%s.pdf"`, d.From, d.To))
	w.Write(raw)
}
