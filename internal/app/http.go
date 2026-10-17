package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type App struct {
	DB     *pgxpool.Pool
	Config Config
}
type Problem struct {
	Status        int
	Code, Message string
}

func (p *Problem) Error() string                 { return p.Message }
func problem(status int, code, msg string) error { return &Problem{status, code, msg} }
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Debug("response write failed")
	}
}
func fail(w http.ResponseWriter, r *http.Request, status int, code, msg string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": msg}, "request_id": w.Header().Get("X-Request-ID")})
}
func handleError(w http.ResponseWriter, r *http.Request, err error) {
	var p *Problem
	var pe *pgconn.PgError
	switch {
	case errors.As(err, &p):
		fail(w, r, p.Status, p.Code, p.Message)
	case errors.Is(err, pgx.ErrNoRows):
		fail(w, r, 404, "NOT_FOUND", "Record not found")
	case errors.As(err, &pe) && pe.Code == "23P01":
		fail(w, r, 409, "INTERVAL_CONFLICT", "A time interval overlaps an existing record. Review the interval before saving.")
	case errors.As(err, &pe) && (pe.Code == "23505" || pe.Code == "40001" || pe.Code == "40P01"):
		fail(w, r, 409, "CONFLICT", "Another operation conflicts with this change. Refresh and review the record.")
	case errors.As(err, &pe) && (pe.Code == "23514" || pe.Code == "23503" || pe.Code == "22P02" || pe.Code == "22007"):
		fail(w, r, 422, "VALIDATION", "Invalid record, interval or referenced value")
	default:
		level := slog.LevelError
		if errors.Is(err, context.Canceled) && errors.Is(r.Context().Err(), context.Canceled) {
			level = slog.LevelDebug
		} else if errors.Is(err, context.DeadlineExceeded) {
			level = slog.LevelWarn
		}
		// Route patterns and error types identify the failing operation without
		// exposing record IDs, query parameters or error text containing secrets.
		slog.Log(r.Context(), level, "request failed",
			"request_id", w.Header().Get("X-Request-ID"),
			"method", r.Method, "route", r.Pattern,
			"type", errorClass(err), "error_type", fmt.Sprintf("%T", err))
		fail(w, r, 503, "UNAVAILABLE", "The operation could not be confirmed. Reconnect and reconcile before retrying.")
	}
}
func errorClass(err error) string {
	if errors.Is(err, context.Canceled) {
		return "context_canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	var p *pgconn.PgError
	if errors.As(err, &p) {
		return p.Code
	}
	return "database_or_internal"
}
func decode(r *http.Request, dst any) error {
	d := json.NewDecoder(io.LimitReader(r.Body, 65537))
	d.DisallowUnknownFields()
	if e := d.Decode(dst); e != nil {
		return problem(422, "VALIDATION", "A valid JSON request with supported fields is required")
	}
	if e := d.Decode(new(any)); e != io.EOF {
		return problem(422, "VALIDATION", "Only one JSON object is permitted")
	}
	return nil
}
func token() string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "alive"}) })
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, c := context.WithTimeout(r.Context(), 2*time.Second)
		defer c()
		if e := a.DB.Ping(ctx); e != nil {
			fail(w, r, 503, "NOT_READY", "Database unavailable")
			return
		}
		if e := SchemaReady(ctx, a.DB); e != nil {
			fail(w, r, 503, "SCHEMA_UPGRADE_REQUIRED", "Database migrations must be applied before serving traffic")
			return
		}
		writeJSON(w, 200, map[string]string{"status": "ready"})
	})
	mux.HandleFunc("POST /api/v1/auth/login", a.login)
	mux.HandleFunc("POST /api/v1/auth/setup", a.setupAccount)
	a.registerCore(mux)
	a.registerTOTP(mux)
	a.registerAdmin(mux)
	a.registerBilling(mux)
	a.registerMail(mux)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { fail(w, r, 404, "NOT_FOUND", "API route not found") })
	mux.HandleFunc("/", a.static)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-ID", token()[:16])
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Permissions-Policy", "geolocation=(), camera=(), microphone=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		if strings.HasPrefix(a.Config.AppURL, "https://") {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
			r.Body = http.MaxBytesReader(w, r.Body, 65536)
		}
		ctx, c := context.WithTimeout(r.Context(), 25*time.Second)
		defer c()
		defer func() {
			if x := recover(); x != nil {
				slog.Error("request panic", "request_id", w.Header().Get("X-Request-ID"))
				fail(w, r, 500, "INTERNAL", "Unexpected server error")
			}
		}()
		mux.ServeHTTP(w, r.WithContext(ctx))
	})
}
func (a *App) static(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		http.Error(w, "Method not allowed", 405)
		return
	}
	p := filepath.Clean(filepath.FromSlash(strings.TrimPrefix(r.URL.Path, "/")))
	if !filepath.IsLocal(p) && p != "." {
		http.NotFound(w, r)
		return
	}
	root, e := os.OpenRoot(a.Config.StaticDir)
	if e != nil {
		http.Error(w, "Frontend not built. Build web/ first.", 503)
		return
	}
	defer root.Close()
	f, e := root.Open(p)
	if e != nil || p == "." {
		if f != nil {
			f.Close()
		}
		if p != "." && strings.Contains(filepath.Base(p), ".") {
			http.NotFound(w, r)
			return
		}
		f, e = root.Open("index.html")
		p = "index.html"
	}
	if e != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	s, e := f.Stat()
	if e != nil || s.IsDir() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	if strings.HasPrefix(p, "assets") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	http.ServeContent(w, r, p, s.ModTime(), f)
}
func (a *App) audit(ctx context.Context, tx pgx.Tx, userID, action, entityID string, before, after any) error {
	b, e := json.Marshal(before)
	if e != nil {
		return e
	}
	v, e := json.Marshal(after)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO audit_events(actor_id,action,entity_id,before_value,after_value) VALUES(NULLIF($1,'')::uuid,$2,$3,$4,$5)`, userID, action, entityID, b, v)
	return e
}

type querier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func unmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }
func queryItems(ctx context.Context, q querier, sql string, args ...any) ([]map[string]any, error) {
	rows, e := q.Query(ctx, sql, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var raw []byte
		if e = rows.Scan(&raw); e != nil {
			return nil, e
		}
		var v map[string]any
		if e = json.Unmarshal(raw, &v); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
