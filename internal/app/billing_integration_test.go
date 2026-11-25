package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func billingTestApp(t *testing.T) (*App, User, string) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is required for real PostgreSQL billing tests")
	}
	ctx := context.Background()
	base, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	schema := "billing_test_" + token()[:12]
	if _, err = base.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS btree_gist`); err != nil {
		base.Close()
		t.Fatal(err)
	}
	if _, err = base.Exec(ctx, `CREATE SCHEMA `+pgx.Identifier{schema}.Sanitize()); err != nil {
		base.Close()
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		if _, err := base.Exec(ctx, `DROP SCHEMA `+pgx.Identifier{schema}.Sanitize()+` CASCADE`); err != nil {
			t.Error(err)
		}
		base.Close()
	})
	if err = Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	a := &App{DB: pool, Config: Config{DataDir: t.TempDir(), AppURL: "https://test.example"}}
	var user User
	user.IsAdmin = true
	user.Active = true
	if err = pool.QueryRow(ctx, `INSERT INTO users(email,name,password_hash,is_admin) VALUES('billing@example.test','Bäuerin','unused',true) RETURNING id::text`).Scan(&user.ID); err != nil {
		t.Fatal(err)
	}
	var farm string
	if err = pool.QueryRow(ctx, `INSERT INTO farms(name,address) VALUES('Hof Groß','Wiesenstraße 1') RETURNING id::text`).Scan(&farm); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE organization SET tax_percent=20`); err != nil {
		t.Fatal(err)
	}
	return a, user, farm
}
func billingRequest(t *testing.T, a *App, user User, handler http.HandlerFunc, path, id, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Idempotency-Key", key)
	req.SetPathValue("id", id)
	req = req.WithContext(context.WithValue(req.Context(), authKey{}, authContext{User: user}))
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}
func seedBillable(t *testing.T, a *App, user User, farm string) string {
	t.Helper()
	ctx := context.Background()
	var sid string
	err := a.DB.QueryRow(ctx, `INSERT INTO work_sessions(worker_id,farm_id,activity_id,started_at,stopped_at,effective_start,effective_stop,state,description,version,created_by,updated_at) SELECT $1,$2,id,'2026-01-31 22:30Z','2026-02-01 00:30Z','2026-01-31 22:30Z','2026-02-01 00:30Z','APPROVED','Fütterung',2,$1,clock_timestamp() FROM activities LIMIT 1 RETURNING work_sessions.id::text`, user.ID, farm).Scan(&sid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.DB.Exec(ctx, `INSERT INTO rates(hourly_rate,effective_from) VALUES(25,'2020-01-01Z')`); err != nil {
		t.Fatal(err)
	}
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	snapshot, err := a.pricingSnapshot(ctx, tx, sid)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO approvals(session_id,source_version,actor_id,snapshot) VALUES($1,2,$2,$3)`, sid, user.ID, raw); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return sid
}
func TestPostgresBillingReservationsIssuancePaymentsAndReversal(t *testing.T) {
	a, user, farm := billingTestApp(t)
	seedBillable(t, a, user, farm)
	body := fmt.Sprintf(`{"farm_id":%q,"from":"2026-01-01","to":"2026-02-01"}`, farm)
	var wg sync.WaitGroup
	results := make(chan *httptest.ResponseRecorder, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			results <- billingRequest(t, a, user, a.createInvoice, "/api/v1/admin/invoices", "", fmt.Sprintf("draft-key-%d", n), body)
		}(i)
	}
	wg.Wait()
	close(results)
	var draft invoiceDocument
	success, conflict := 0, 0
	for rec := range results {
		switch rec.Code {
		case 200:
			success++
			if err := json.Unmarshal(rec.Body.Bytes(), &draft); err != nil {
				t.Fatal(err)
			}
		case 409:
			conflict++
		default:
			t.Fatalf("create %d %s", rec.Code, rec.Body.String())
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("concurrent reservations success=%d conflicts=%d", success, conflict)
	}
	if draft.Net != "12.50" || draft.Total != "15.00" || draft.Items[0].NetUS != 1800000000 {
		t.Fatalf("month boundary allocation %+v", draft)
	}
	// A storage failure must leave the invoice as a draft and commit no numbering or command result.
	goodDir := a.Config.DataDir
	badPath := filepath.Join(goodDir, "file")
	if err := os.WriteFile(badPath, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	a.Config.DataDir = badPath
	rec := billingRequest(t, a, user, a.issueInvoice, "/issue", draft.ID, "issue-one-key", "{}")
	if rec.Code != 503 {
		t.Fatalf("expected honest PDF failure, got %d %s", rec.Code, rec.Body.String())
	}
	var state string
	var issued int
	if err := a.DB.QueryRow(t.Context(), `SELECT state,(SELECT count(*) FROM invoice_series)::int FROM invoices WHERE id=$1`, draft.ID).Scan(&state, &issued); err != nil {
		t.Fatal(err)
	}
	if state != "draft" || issued != 0 {
		t.Fatalf("failed generation consumed state/number: %s %d", state, issued)
	}
	a.Config.DataDir = goodDir
	rec = billingRequest(t, a, user, a.issueInvoice, "/issue", draft.ID, "issue-one-key", "{}")
	if rec.Code != 200 {
		t.Fatalf("issue %d %s", rec.Code, rec.Body.String())
	}
	var invoice invoiceDocument
	if err := json.Unmarshal(rec.Body.Bytes(), &invoice); err != nil {
		t.Fatal(err)
	}
	again := billingRequest(t, a, user, a.issueInvoice, "/issue", draft.ID, "issue-one-key", "{}")
	var retried invoiceDocument
	if err := json.Unmarshal(again.Body.Bytes(), &retried); err != nil {
		t.Fatal(err)
	}
	if again.Code != 200 || retried.Number == nil || invoice.Number == nil || *retried.Number != *invoice.Number || retried.Total != invoice.Total {
		t.Fatalf("issue retry differs: %d %s", again.Code, again.Body.String())
	}
	if invoice.Number == nil || !strings.HasSuffix(*invoice.Number, "00001") {
		t.Fatalf("number %+v", invoice.Number)
	}
	if _, err := os.Stat(filepath.Join(goodDir, "invoices", draft.ID+".pdf")); err != nil {
		t.Fatal(err)
	}
	// Editing ordinary rate data cannot change the already approved or issued snapshot.
	if _, err := a.DB.Exec(t.Context(), `UPDATE rates SET hourly_rate=99,version=version+1`); err != nil {
		t.Fatal(err)
	}
	frozen, err := invoiceFrom(t.Context(), a.DB, draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	if frozen.Net != "12.50" || frozen.Items[0].Pricing.Segments[0].Rate != "25.0000" {
		t.Fatalf("snapshot repriced %+v", frozen)
	}
	// A larger later service period invoices only the remaining interval, using
	// the approved tax treatment even when organization settings have changed.
	if _, err := a.DB.Exec(t.Context(), `UPDATE organization SET tax_percent=25`); err != nil {
		t.Fatal(err)
	}
	remainderBody := fmt.Sprintf(`{"farm_id":%q,"from":"2026-01-01","to":"2026-03-01"}`, farm)
	remainderResult := billingRequest(t, a, user, a.createInvoice, "/invoices", "", "remainder-draft-key", remainderBody)
	if remainderResult.Code != 200 {
		t.Fatalf("remainder %d %s", remainderResult.Code, remainderResult.Body.String())
	}
	var remainder invoiceDocument
	if err := json.Unmarshal(remainderResult.Body.Bytes(), &remainder); err != nil {
		t.Fatal(err)
	}
	if remainder.Net != "37.50" || remainder.Total != "45.00" || remainder.Issuer.Tax != "20.0000" || len(remainder.Items) != 1 || remainder.Items[0].NetUS != 5400000000 {
		t.Fatalf("remaining allocation %+v", remainder)
	}
	rec = billingRequest(t, a, user, a.addPayment, "/payments", draft.ID, "payment-one-key", `{"amount":"10.00","paid_on":"2026-02-01","reference":"Überweisung"}`)
	if rec.Code != 200 {
		t.Fatalf("payment %d %s", rec.Code, rec.Body.String())
	}
	financeRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/reports?from=2026-01-31&to=2026-02-01&group=day&worker_id="+user.ID, nil)
	financeResponse := httptest.NewRecorder()
	a.report(financeResponse, financeRequest)
	if financeResponse.Code != 200 {
		t.Fatalf("finance %d %s", financeResponse.Code, financeResponse.Body.String())
	}
	var report struct {
		Finance struct {
			Currency string `json:"currency"`
			Invoiced string `json:"invoiced_net"`
			Paid     string `json:"paid"`
			Balance  string `json:"balance"`
			Unbilled int64  `json:"unbilled_us"`
			Scope    string `json:"balance_scope"`
		} `json:"finance"`
	}
	if err := json.Unmarshal(financeResponse.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Finance.Currency != "EUR" || report.Finance.Invoiced != "12.50" || report.Finance.Paid != "10.00" || report.Finance.Balance != "5.00" || report.Finance.Unbilled != 0 || report.Finance.Scope != "whole overlapping issued invoices" {
		t.Fatalf("financial totals %+v", report.Finance)
	}
	rec = billingRequest(t, a, user, a.addPayment, "/payments", draft.ID, "payment-excess-key", `{"amount":"6.00","paid_on":"2026-02-01","reference":"excess"}`)
	if rec.Code != 422 {
		t.Fatalf("excess %d %s", rec.Code, rec.Body.String())
	}
	rec = billingRequest(t, a, user, a.reverseInvoice, "/reverse", draft.ID, "", `{"reason":"Correct customer billing period"}`)
	if rec.Code != 200 {
		t.Fatalf("reverse %d %s", rec.Code, rec.Body.String())
	}
	frozen, err = invoiceFrom(t.Context(), a.DB, draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	if frozen.State != "reversed" || frozen.Paid != "10.00" || len(frozen.Credits) != 1 || len(frozen.Items) != 1 {
		t.Fatalf("reversal lost evidence %+v", frozen)
	}
	var active bool
	if err = a.DB.QueryRow(t.Context(), `SELECT bool_or(active) FROM invoice_allocations WHERE invoice_id=$1`, draft.ID).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active {
		t.Fatal("reversal did not release claims")
	}
}

func TestPostgresReportMonthBoundaryAndCSV(t *testing.T) {
	a, user, farm := billingTestApp(t)
	seedBillable(t, a, user, farm)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/reports?from=2026-01-31&to=2026-02-02&group=day", nil)
	rec := httptest.NewRecorder()
	a.report(rec, req)
	if rec.Code != 200 {
		t.Fatalf("report %d %s", rec.Code, rec.Body.String())
	}
	var result struct {
		Rows   []reportRow `json:"rows"`
		Totals reportRow   `json:"totals"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 2 || result.Rows[0].Net != 1800000000 || result.Rows[1].Net != 5400000000 || result.Totals.Net != 7200000000 || result.Totals.Approved != 7200000000 {
		t.Fatalf("report boundary %+v", result)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/reports.csv?from=2026-01-31&to=2026-02-02&group=day", nil)
	rec = httptest.NewRecorder()
	a.report(rec, req)
	// One row per entry in the organization timezone; the entry crosses midnight in Vienna.
	csvBody := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(csvBody, "\n2026-01-31;23:30;2026-02-01 01:30;") || !strings.Contains(csvBody, ";Bäuerin;Hof Groß;00:00:00;02:00:00") {
		t.Fatalf("csv mismatch %d %s", rec.Code, rec.Body.String())
	}
}
func TestPostgresRateOverlapConstraint(t *testing.T) {
	a, _, _ := billingTestApp(t)
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := a.DB.Exec(t.Context(), `INSERT INTO rates(hourly_rate,effective_from) VALUES(25,$1)`, start); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DB.Exec(t.Context(), `INSERT INTO rates(hourly_rate,effective_from) VALUES(30,$1)`, start.Add(time.Hour)); err == nil {
		t.Fatal("overlapping organization rate accepted")
	}
}
