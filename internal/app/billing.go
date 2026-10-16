package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type invoiceParty struct {
	Name            string `json:"name"`
	Address         string `json:"address"`
	Email           string `json:"email"`
	Bank            string `json:"bank_details"`
	Timezone        string `json:"timezone"`
	Currency        string `json:"currency"`
	Tax             string `json:"tax_percent"`
	TaxConfirmed    bool   `json:"tax_confirmed"`
	Prefix          string `json:"invoice_prefix"`
	PaymentDays     int    `json:"payment_days"`
	Version         int64  `json:"version"`
	TaxIdentifier   string `json:"tax_identifier"`
	InvoiceFooter   string `json:"invoice_footer"`
	InvoiceLayout   string `json:"invoice_layout"`
	InvoiceAppendix bool   `json:"invoice_appendix"`
}
type invoiceItem struct {
	SessionID   string        `json:"session_id"`
	Description string        `json:"description"`
	Worker      string        `json:"worker_name"`
	Activity    string        `json:"activity_name"`
	Start       time.Time     `json:"started_at"`
	Stop        time.Time     `json:"stopped_at"`
	NetUS       int64         `json:"net_us"`
	Net         string        `json:"net"`
	Pricing     PriceSnapshot `json:"pricing"`
}
type invoiceDocument struct {
	ID           string               `json:"id"`
	Number       *string              `json:"number"`
	State        string               `json:"state"`
	FarmName     string               `json:"farm_name"`
	From         string               `json:"from_date"`
	To           string               `json:"to_date"`
	Timezone     string               `json:"timezone"`
	Currency     string               `json:"currency"`
	Net          string               `json:"net"`
	Tax          string               `json:"tax"`
	Total        string               `json:"total"`
	Paid         string               `json:"paid"`
	Balance      string               `json:"balance"`
	PaymentState string               `json:"payment_state"`
	Overdue      bool                 `json:"overdue"`
	RefundDue    string               `json:"refund_due"`
	Issuer       invoiceParty         `json:"issuer"`
	Customer     invoiceParty         `json:"customer"`
	Items        []invoiceItem        `json:"items"`
	SummaryLines []invoiceSummaryLine `json:"summary_lines"`
	IssuedAt     *time.Time           `json:"issued_at"`
	DueOn        *string              `json:"due_on"`
	PDFPath      *string              `json:"-"`
	Payments     []map[string]any     `json:"payments"`
	Credits      []map[string]any     `json:"credits"`
}

func (a *App) billingCommand(w http.ResponseWriter, r *http.Request, operation string, payload any, fn func(pgx.Tx) (any, error)) {
	key := r.Header.Get("Idempotency-Key")
	if len(key) < 8 || len(key) > 200 {
		fail(w, r, 422, "IDEMPOTENCY_REQUIRED", "Send an Idempotency-Key containing 8 to 200 characters")
		return
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		handleError(w, r, err)
		return
	}
	sum := sha256.Sum256(raw)
	fingerprint := hex.EncodeToString(sum[:])
	ctx := r.Context()
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		handleError(w, r, err)
		return
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, actor(r).ID+":"+operation+":"+key)
	if err != nil {
		handleError(w, r, err)
		return
	}
	var previous string
	var result []byte
	err = tx.QueryRow(ctx, `SELECT fingerprint,result FROM commands WHERE actor_id=$1 AND operation=$2 AND key=$3`, actor(r).ID, operation, key).Scan(&previous, &result)
	if err == nil {
		if previous != fingerprint {
			fail(w, r, 409, "IDEMPOTENCY_CONFLICT", "This key was already used for different request content")
			return
		}
		writeJSON(w, 200, json.RawMessage(result))
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		handleError(w, r, err)
		return
	}
	value, err := fn(tx)
	if err == nil {
		result, err = json.Marshal(value)
	}
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO commands(actor_id,operation,key,fingerprint,result) VALUES($1,$2,$3,$4,$5)`, actor(r).ID, operation, key, fingerprint, result)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		handleError(w, r, err)
		return
	}
	writeJSON(w, 200, value)
}
func invoiceFrom(ctx context.Context, q querier, id string) (invoiceDocument, error) {
	var d invoiceDocument
	var issuer, customer []byte
	var net, tax, total, paid int64
	err := q.QueryRow(ctx, `SELECT i.id::text,i.number,i.state,i.customer->>'name',i.from_date::text,i.to_date::text,i.timezone,i.currency,i.net_cents,i.tax_cents,i.total_cents,coalesce((SELECT sum(amount_cents) FROM payments p WHERE p.invoice_id=i.id),0)::bigint,i.issuer,i.customer,i.issued_at,i.due_on::text,i.pdf_path,(i.state='issued' AND i.total_cents>coalesce((SELECT sum(amount_cents) FROM payments p WHERE p.invoice_id=i.id),0) AND i.due_on<(clock_timestamp() AT TIME ZONE i.timezone)::date) FROM invoices i WHERE i.id=$1`, id).Scan(&d.ID, &d.Number, &d.State, &d.FarmName, &d.From, &d.To, &d.Timezone, &d.Currency, &net, &tax, &total, &paid, &issuer, &customer, &d.IssuedAt, &d.DueOn, &d.PDFPath, &d.Overdue)
	if err != nil {
		return d, err
	}
	if d.IssuedAt != nil {
		v := d.IssuedAt.UTC()
		d.IssuedAt = &v
	}
	if err = json.Unmarshal(issuer, &d.Issuer); err != nil {
		return d, err
	}
	if err = json.Unmarshal(customer, &d.Customer); err != nil {
		return d, err
	}
	d.Net = moneyString(net)
	d.Tax = moneyString(tax)
	d.Total = moneyString(total)
	d.Paid = moneyString(paid)
	d.PaymentState = invoicePaymentState(d.State, total, paid)
	d.RefundDue = "0.00"
	if d.PaymentState == "credit_balance" {
		d.RefundDue = moneyString(paid)
	}
	d.Balance = moneyString(total - paid)
	if d.State == "reversed" {
		d.Balance = "0.00"
	}
	d.Items = []invoiceItem{}
	rows, err := q.Query(ctx, `SELECT snapshot FROM invoice_allocations WHERE invoice_id=$1 ORDER BY started_at,session_id`, id)
	if err != nil {
		return d, err
	}
	for rows.Next() {
		var raw []byte
		var item invoiceItem
		if err = rows.Scan(&raw); err != nil {
			break
		}
		if err = json.Unmarshal(raw, &item); err != nil {
			break
		}
		d.Items = append(d.Items, item)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return d, err
	}
	if d.Issuer.InvoiceLayout == "activity_rate" {
		d.SummaryLines, err = summarizeInvoiceItems(d.Items)
		if err != nil {
			return d, err
		}
	}
	d.Payments, err = queryItems(ctx, q, `SELECT jsonb_build_object('id',id,'amount',to_char(amount_cents::numeric/100,'FM9999999999999990.00'),'paid_on',paid_on,'reference',reference,'created_at',created_at) FROM payments WHERE invoice_id=$1 ORDER BY created_at`, id)
	if err != nil {
		return d, err
	}
	d.Credits, err = queryItems(ctx, q, `SELECT jsonb_build_object('id',id,'number',number,'total',to_char(total_cents::numeric/100,'FM9999999999999990.00'),'reason',reason,'created_at',created_at) FROM invoice_credits WHERE invoice_id=$1`, id)
	return d, err
}
func (a *App) listInvoices(w http.ResponseWriter, r *http.Request) {
	items, err := queryItems(r.Context(), a.DB, `SELECT jsonb_build_object('id',i.id,'number',i.number,'state',i.state,'payment_state',`+invoicePaymentStateSQL+`,'overdue',(i.state='issued' AND i.total_cents>coalesce(p.paid,0) AND i.due_on<(clock_timestamp() AT TIME ZONE i.timezone)::date),'farm_name',i.customer->>'name','from_date',i.from_date,'to_date',i.to_date,'currency',i.currency,'net',round(i.net_cents::numeric/100,2)::text,'tax',round(i.tax_cents::numeric/100,2)::text,'total',round(i.total_cents::numeric/100,2)::text,'paid',round(coalesce(p.paid,0)::numeric/100,2)::text,'balance',round(CASE WHEN i.state='reversed' THEN 0 ELSE (i.total_cents-coalesce(p.paid,0))::numeric/100 END,2)::text,'due_on',i.due_on) FROM invoices i LEFT JOIN LATERAL(SELECT sum(amount_cents) paid FROM payments WHERE invoice_id=i.id)p ON true ORDER BY i.created_at DESC LIMIT 1000`)
	if err != nil {
		handleError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": len(items)})
}
func (a *App) getInvoice(w http.ResponseWriter, r *http.Request) {
	d, err := invoiceFrom(r.Context(), a.DB, r.PathValue("id"))
	if err != nil {
		handleError(w, r, err)
		return
	}
	writeJSON(w, 200, d)
}

func (a *App) createInvoice(w http.ResponseWriter, r *http.Request) {
	var in struct {
		FarmID string `json:"farm_id"`
		From   string `json:"from"`
		To     string `json:"to"`
	}
	if err := decode(r, &in); err != nil {
		handleError(w, r, err)
		return
	}
	a.billingCommand(w, r, "invoice.create", in, func(tx pgx.Tx) (any, error) {
		ctx := r.Context()
		var issuerRaw, customerRaw []byte
		var org invoiceParty
		if err := tx.QueryRow(ctx, `SELECT to_jsonb(o)||jsonb_build_object('tax_percent',tax_percent::text) FROM organization o WHERE id FOR SHARE`).Scan(&issuerRaw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(issuerRaw, &org); err != nil {
			return nil, err
		}
		if !org.TaxConfirmed {
			return nil, problem(422, "TAX_SETUP_REQUIRED", "Confirm the organization's tax treatment before billing")
		}
		start, stop, err := localDateRange(in.From, in.To, org.Timezone)
		if err != nil {
			return nil, err
		}
		if err = tx.QueryRow(ctx, `SELECT to_jsonb(f) FROM farms f WHERE id=$1 FOR UPDATE`, in.FarmID).Scan(&customerRaw); err != nil {
			return nil, err
		}
		rows, err := tx.Query(ctx, `SELECT w.id::text,w.version,w.effective_start,w.effective_stop,w.description,u.name,ac.name,p.snapshot FROM work_sessions w JOIN users u ON u.id=w.worker_id JOIN activities ac ON ac.id=w.activity_id JOIN approvals p ON p.session_id=w.id AND p.source_version=w.version WHERE w.farm_id=$1 AND w.state='APPROVED' AND NOT w.non_billable AND w.effective_start<$3 AND w.effective_stop>$2 ORDER BY w.id FOR UPDATE OF w`, in.FarmID, start, stop)
		if err != nil {
			return nil, err
		}
		type source struct {
			id                            string
			version                       int64
			start, stop                   time.Time
			description, worker, activity string
			pricing                       []byte
		}
		sources := []source{}
		for rows.Next() {
			var s source
			if err = rows.Scan(&s.id, &s.version, &s.start, &s.stop, &s.description, &s.worker, &s.activity, &s.pricing); err != nil {
				break
			}
			s.start, s.stop = s.start.UTC(), s.stop.UTC()
			sources = append(sources, s)
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return nil, err
		}
		if len(sources) == 0 {
			return nil, problem(422, "NO_ELIGIBLE_WORK", "No approved billable work exists in this period")
		}
		var id string
		err = tx.QueryRow(ctx, `INSERT INTO invoices(farm_id,from_date,to_date,period_start,period_stop,timezone,currency,issuer,customer,net_cents,tax_cents,total_cents,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,0,0,0,$10) RETURNING id::text`, in.FarmID, in.From, in.To, start, stop, org.Timezone, org.Currency, issuerRaw, customerRaw, actor(r).ID).Scan(&id)
		if err != nil {
			return nil, err
		}
		var total int64
		allocationCount := 0
		approvalTax, approvalCurrency := "", ""
		for _, s := range sources {
			x, y := clippedInterval(s.start, s.stop, start, stop)
			claimedRows, e := tx.Query(ctx, `SELECT started_at,stopped_at FROM invoice_allocations WHERE session_id=$1 AND active AND started_at<$3 AND stopped_at>$2 ORDER BY started_at`, s.id, x, y)
			if e != nil {
				return nil, e
			}
			claims := []priceInterval{}
			for claimedRows.Next() {
				var c priceInterval
				if e = claimedRows.Scan(&c.Start, &c.Stop); e != nil {
					break
				}
				claims = append(claims, c)
			}
			if e == nil {
				e = claimedRows.Err()
			}
			claimedRows.Close()
			if e != nil {
				return nil, e
			}
			parts := unclaimedIntervals(x, y, claims)
			if len(parts) == 0 {
				continue
			}
			var pricing PriceSnapshot
			if err = json.Unmarshal(s.pricing, &pricing); err != nil {
				return nil, err
			}
			if !pricing.Priced {
				if pricing.UnpricedReason == "VACATION_POLICY_REQUIRED" {
					return nil, problem(422, "VACATION_POLICY_REQUIRED", "Vacation-day billing is awaiting an explicitly configured surcharge policy. Configure and review that policy before billing this work")
				}
				if pricing.UnpricedReason == "HOLIDAY_POLICY_REQUIRED" {
					return nil, problem(422, "HOLIDAY_POLICY_REQUIRED", "Public-holiday billing is awaiting an explicitly configured surcharge policy. Configure and review that policy before billing this work")
				}
				return nil, problem(422, "RATE_REQUIRED", "Approved work needs pricing. Add the missing rate and explicitly approve it again")
			}
			if approvalTax == "" {
				approvalTax, approvalCurrency = pricing.TaxPercent, pricing.Currency
				org.Tax, org.Currency = approvalTax, approvalCurrency
			}
			if pricing.Currency != approvalCurrency || pricing.TaxPercent != approvalTax {
				return nil, problem(409, "PRICING_REVIEW_REQUIRED", "Selected approvals use different currencies or tax treatments. Select separate service periods for each treatment")
			}
			for _, part := range parts {
				net, cents, e := allocationAmount(pricing, part.Start, part.Stop)
				if e != nil {
					return nil, e
				}
				item := invoiceItem{SessionID: s.id, Description: s.description, Worker: s.worker, Activity: s.activity, Start: part.Start, Stop: part.Stop, NetUS: net, Net: moneyString(cents), Pricing: pricing}
				raw, e := json.Marshal(item)
				if e != nil {
					return nil, e
				}
				_, err = tx.Exec(ctx, `INSERT INTO invoice_allocations(invoice_id,session_id,source_version,started_at,stopped_at,net_us,net_cents,snapshot) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, id, s.id, s.version, part.Start, part.Stop, net, cents, raw)
				if err != nil {
					return nil, err
				}
				if cents > 9223372036854775807-total {
					return nil, problem(422, "AMOUNT_TOO_LARGE", "Invoice total exceeds the supported range")
				}
				total += cents
				allocationCount++
			}
		}
		if allocationCount == 0 {
			return nil, problem(409, "WORK_RESERVED", "All eligible work in this period is already reserved or invoiced")
		}
		taxUnits, err := decimalUnits(org.Tax, 4)
		if err != nil {
			return nil, err
		}
		tax := roundedRatio(new(big.Int).Mul(big.NewInt(total), taxUnits), big.NewInt(1000000))
		if !tax.IsInt64() || tax.Int64() > 9223372036854775807-total {
			return nil, problem(422, "AMOUNT_TOO_LARGE", "Invoice total exceeds the supported range")
		}
		issuerRaw, err = json.Marshal(org)
		if err != nil {
			return nil, err
		}
		_, err = tx.Exec(ctx, `UPDATE invoices SET net_cents=$2,tax_cents=$3,total_cents=$2::bigint+$3::bigint,currency=$4,issuer=$5 WHERE id=$1`, id, total, tax.Int64(), org.Currency, issuerRaw)
		if err == nil {
			err = a.audit(ctx, tx, actor(r).ID, "invoice.draft_created", id, nil, in)
		}
		if err != nil {
			return nil, err
		}
		return invoiceFrom(ctx, tx, id)
	})
}

func (a *App) issueInvoice(w http.ResponseWriter, r *http.Request) {
	var in struct{}
	if err := decode(r, &in); err != nil {
		handleError(w, r, err)
		return
	}
	id := r.PathValue("id")
	a.billingCommand(w, r, "invoice.issue:"+id, in, func(tx pgx.Tx) (any, error) {
		ctx := r.Context()
		var state string
		if err := tx.QueryRow(ctx, `SELECT state FROM invoices WHERE id=$1 FOR UPDATE`, id).Scan(&state); err != nil {
			return nil, err
		}
		if state == "issued" {
			return invoiceFrom(ctx, tx, id)
		}
		if state != "draft" {
			return nil, problem(409, "INVALID_STATE", "Only draft invoices can be issued")
		}
		rows, err := tx.Query(ctx, `SELECT w.id::text,w.state,w.version,al.source_version,al.active FROM invoice_allocations al JOIN work_sessions w ON w.id=al.session_id WHERE al.invoice_id=$1 ORDER BY w.id FOR UPDATE OF w`, id)
		if err != nil {
			return nil, err
		}
		count := 0
		stale := false
		for rows.Next() {
			var sid, st string
			var version, source int64
			var active bool
			if err = rows.Scan(&sid, &st, &version, &source, &active); err != nil {
				break
			}
			count++
			if st != "APPROVED" || version != source || !active {
				stale = true
			}
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return nil, err
		}
		if stale || count == 0 {
			return nil, problem(409, "STALE_DRAFT", "Source work changed. Cancel this draft and review a new draft")
		}
		d, err := invoiceFrom(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		var now time.Time
		if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			return nil, err
		}
		loc, err := time.LoadLocation(d.Timezone)
		if err != nil {
			return nil, err
		}
		year := now.In(loc).Year()
		var serial int64
		err = tx.QueryRow(ctx, `INSERT INTO invoice_series(prefix,year,next_number) VALUES($1,$2,2) ON CONFLICT(prefix,year) DO UPDATE SET next_number=invoice_series.next_number+1 RETURNING next_number-1`, d.Issuer.Prefix, year).Scan(&serial)
		if err != nil {
			return nil, err
		}
		number := fmt.Sprintf("%s-%d-%05d", d.Issuer.Prefix, year, serial)
		due := now.In(loc).AddDate(0, 0, d.Issuer.PaymentDays).Format("2006-01-02")
		_, err = tx.Exec(ctx, `UPDATE invoices SET state='issued',number=$2,series=$3,year=$4,serial=$5,issued_at=$6,due_on=$7 WHERE id=$1`, id, number, d.Issuer.Prefix, year, serial, now, due)
		if err != nil {
			return nil, err
		}
		d, err = invoiceFrom(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		path, err := a.persistInvoicePDF(d)
		if err != nil {
			return nil, fmt.Errorf("generate invoice PDF: %w", err)
		}
		_, err = tx.Exec(ctx, `UPDATE invoices SET pdf_path=$2 WHERE id=$1`, id, path)
		if err == nil {
			err = a.audit(ctx, tx, actor(r).ID, "invoice.issued", id, nil, map[string]any{"number": number, "total": d.Total})
		}
		return d, err
	})
}
func (a *App) cancelInvoice(w http.ResponseWriter, r *http.Request) {
	var in struct{}
	if err := decode(r, &in); err != nil {
		handleError(w, r, err)
		return
	}
	a.invoiceTransition(w, r, "cancelled", "", false)
}
func (a *App) reverseInvoice(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Reason string `json:"reason"`
	}
	if err := decode(r, &in); err != nil {
		handleError(w, r, err)
		return
	}
	if len(strings.TrimSpace(in.Reason)) < 3 || len(in.Reason) > 2000 {
		fail(w, r, 422, "VALIDATION", "A reversal reason is required")
		return
	}
	a.invoiceTransition(w, r, "reversed", in.Reason, true)
}
func (a *App) invoiceTransition(w http.ResponseWriter, r *http.Request, target, reason string, credit bool) {
	ctx := r.Context()
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		handleError(w, r, err)
		return
	}
	defer tx.Rollback(ctx)
	id := r.PathValue("id")
	var state string
	err = tx.QueryRow(ctx, `SELECT state FROM invoices WHERE id=$1 FOR UPDATE`, id).Scan(&state)
	if err == nil && state == target {
		d, e := invoiceFrom(ctx, tx, id)
		if e != nil {
			handleError(w, r, e)
			return
		}
		writeJSON(w, 200, d)
		return
	}
	expected := "draft"
	if credit {
		expected = "issued"
	}
	if err == nil && state != expected {
		err = problem(409, "INVALID_STATE", "This invoice cannot make the requested transition")
	}
	if err == nil && credit {
		_, err = tx.Exec(ctx, `INSERT INTO invoice_credits(invoice_id,number,net_cents,tax_cents,total_cents,reason,actor_id) SELECT id,'CREDIT-'||number,net_cents,tax_cents,total_cents,$2,$3 FROM invoices WHERE id=$1`, id, reason, actor(r).ID)
	}
	if err == nil {
		_, err = tx.Exec(ctx, `UPDATE invoices SET state=$2,reversed_at=CASE WHEN $2='reversed' THEN clock_timestamp() END,reversal_reason=$3 WHERE id=$1`, id, target, reason)
	}
	if err == nil {
		_, err = tx.Exec(ctx, `UPDATE invoice_allocations SET active=false WHERE invoice_id=$1`, id)
	}
	if err == nil {
		err = a.audit(ctx, tx, actor(r).ID, "invoice."+target, id, map[string]string{"state": state}, map[string]string{"state": target, "reason": reason})
	}
	var result invoiceDocument
	if err == nil {
		result, err = invoiceFrom(ctx, tx, id)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		handleError(w, r, err)
		return
	}
	writeJSON(w, 200, result)
}
func (a *App) addPayment(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Amount    string `json:"amount"`
		PaidOn    string `json:"paid_on"`
		Reference string `json:"reference"`
	}
	if err := decode(r, &in); err != nil {
		handleError(w, r, err)
		return
	}
	cents, err := decimalUnits(in.Amount, 2)
	if err != nil || !cents.IsInt64() || cents.Sign() <= 0 {
		fail(w, r, 422, "VALIDATION", "Payment must be a positive decimal with at most two decimal places")
		return
	}
	if _, err = time.Parse("2006-01-02", in.PaidOn); err != nil || len(in.Reference) > 500 {
		fail(w, r, 422, "VALIDATION", "Payment requires a valid date and reference of at most 500 characters")
		return
	}
	id := r.PathValue("id")
	a.billingCommand(w, r, "invoice.payment:"+id, in, func(tx pgx.Tx) (any, error) {
		ctx := r.Context()
		var state string
		var total, paid int64
		if err := tx.QueryRow(ctx, `SELECT state,total_cents FROM invoices WHERE id=$1 FOR UPDATE`, id).Scan(&state, &total); err != nil {
			return nil, err
		}
		if state != "issued" {
			return nil, problem(409, "INVALID_STATE", "Payments require an issued invoice")
		}
		if err := tx.QueryRow(ctx, `SELECT coalesce(sum(amount_cents),0)::bigint FROM payments WHERE invoice_id=$1`, id).Scan(&paid); err != nil {
			return nil, err
		}
		if cents.Int64() > total-paid {
			return nil, problem(422, "EXCESS_PAYMENT", "Payment exceeds the outstanding balance")
		}
		_, err := tx.Exec(ctx, `INSERT INTO payments(invoice_id,amount_cents,paid_on,reference,actor_id) VALUES($1,$2,$3,$4,$5)`, id, cents.Int64(), in.PaidOn, in.Reference, actor(r).ID)
		if err == nil {
			err = a.audit(ctx, tx, actor(r).ID, "invoice.payment", id, nil, in)
		}
		if err != nil {
			return nil, err
		}
		return invoiceFrom(ctx, tx, id)
	})
}
