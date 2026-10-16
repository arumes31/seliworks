package app

import (
	"archive/zip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
)

// Business exports deliberately exclude credentials, authentication state,
// idempotency keys and SMTP lease tokens. Operator backups remain the restore format.
var businessExportTables = []struct{ name, projection string }{
	{"organization", `to_jsonb(t)-'id'`},
	{"users", `to_jsonb(t)-'password_hash'`},
	{"farms", `to_jsonb(t)`}, {"worker_farms", `to_jsonb(t)`}, {"activities", `to_jsonb(t)`},
	{"work_sessions", `to_jsonb(t)`}, {"work_pauses", `to_jsonb(t)`}, {"breaks", `to_jsonb(t)`},
	{"corrections", `to_jsonb(t)`}, {"approvals", `to_jsonb(t)`}, {"rates", `to_jsonb(t)`},
	{"special_day_pricing", `to_jsonb(t)`}, {"invoice_series", `to_jsonb(t)`},
	{"invoices", `to_jsonb(t)-'pdf_path'`}, {"invoice_allocations", `to_jsonb(t)`},
	{"payments", `to_jsonb(t)`}, {"invoice_credits", `to_jsonb(t)`},
	{"invoice_deliveries", `to_jsonb(t)-ARRAY['lease_token','pdf_path']`},
	{"invoice_delivery_attempts", `to_jsonb(t)-'lease_token'`},
	{"audit_events", `to_jsonb(t)`}, {"schema_migrations", `to_jsonb(t)`},
}

func writeBusinessArchive(ctx context.Context, tx pgx.Tx, destination io.Writer) (err error) {
	archive := zip.NewWriter(destination)
	defer func() {
		if closeErr := archive.Close(); err == nil {
			err = closeErr
		}
	}()
	if _, err = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); err != nil {
		return err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return err
	}
	counts := map[string]int64{}
	for _, table := range businessExportTables {
		file, e := archive.Create(table.name + ".jsonl")
		if e != nil {
			return e
		}
		// Convert top-level numeric scalars to strings without a floating-point
		// round trip. Nested pricing snapshots already store monetary decimals.
		rows, e := tx.Query(ctx, `SELECT (SELECT jsonb_object_agg(key,CASE WHEN jsonb_typeof(value)='number' THEN to_jsonb(value#>>'{}') ELSE value END) FROM jsonb_each(`+table.projection+`)) FROM `+table.name+` t`)
		if e != nil {
			return e
		}
		var count int64
		for rows.Next() {
			var raw []byte
			if e = rows.Scan(&raw); e != nil {
				break
			}
			if _, e = file.Write(append(raw, '\n')); e != nil {
				break
			}
			count++
		}
		if e == nil {
			e = rows.Err()
		}
		rows.Close()
		if e != nil {
			return e
		}
		counts[table.name] = count
	}
	manifest, e := archive.Create("manifest.json")
	if e != nil {
		return e
	}
	return json.NewEncoder(manifest).Encode(map[string]any{
		"format": "seliworks-business-export-v1", "complete": true, "exported_at": now.UTC(), "tables": counts,
		"numeric_values": "Top-level numeric values are decimal strings. Nested snapshots retain their exact original JSON representation.",
		"excluded":       "Authentication credentials, sessions, invitations, MFA/recovery secrets, command keys and private PDF files. Download PDFs from Invoices; use matched operator backups for restoration.",
	})
}

func (a *App) exportBusinessData(w http.ResponseWriter, r *http.Request) {
	if err := a.limit(r.Context(), "business-export:"+actor(r).ID, 10); err != nil {
		handleError(w, r, err)
		return
	}
	if _, err := a.DB.Exec(r.Context(), `INSERT INTO audit_events(actor_id,action,entity_id) VALUES($1,'data.export_started','organization')`, actor(r).ID); err != nil {
		handleError(w, r, err)
		return
	}
	if err := os.MkdirAll(a.Config.DataDir, 0700); err != nil {
		handleError(w, r, err)
		return
	}
	root, err := os.OpenRoot(a.Config.DataDir)
	if err != nil {
		handleError(w, r, err)
		return
	}
	defer root.Close()
	if err = root.MkdirAll("exports", 0700); err != nil {
		handleError(w, r, err)
		return
	}
	name := "exports/" + token() + ".zip"
	file, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		handleError(w, r, err)
		return
	}
	defer root.Remove(name)
	defer file.Close()
	tx, err := a.DB.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		handleError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	if err = writeBusinessArchive(r.Context(), tx, file); err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		handleError(w, r, err)
		return
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		handleError(w, r, err)
		return
	}
	stat, err := file.Stat()
	if err != nil {
		handleError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="seliworks-business-data.zip"`)
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeContent(w, r, "seliworks-business-data.zip", stat.ModTime(), file)
}
