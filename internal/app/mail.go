package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const maxMailPDF = 20 * 1024 * 1024

type mailJob struct {
	ID, InvoiceID, Sender, Recipient, MessageID, PDFPath, PDFHash, Lease string
	Attempts, TotalAttempts                                              int
	CreatedAt                                                            time.Time
}
type mailOutcome struct {
	Accepted, Uncertain, Permanent bool
	Code                           string
}

func (a *App) registerMail(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/admin/invoices/{id}/send", a.requireAdmin(a.queueInvoiceMail))
	mux.HandleFunc("GET /api/v1/admin/invoices/{id}/deliveries", a.requireAdmin(a.listInvoiceMail))
	mux.HandleFunc("POST /api/v1/admin/deliveries/{id}/retry", a.requireAdmin(a.retryInvoiceMail))
}
func validMailAddress(s string) bool {
	if len(s) > 254 || strings.ContainsAny(s, "\r\n") {
		return false
	}
	for _, c := range s {
		if c < 33 || c > 126 {
			return false
		}
	}
	parsed, err := mail.ParseAddress(s)
	return err == nil && parsed.Address == s
}
func (a *App) queueInvoiceMail(w http.ResponseWriter, r *http.Request) {
	var in struct{}
	if err := decode(r, &in); err != nil {
		handleError(w, r, err)
		return
	}
	if a.Config.SMTPHost == "" || !validMailAddress(a.Config.SMTPFrom) {
		fail(w, r, 422, "SMTP_NOT_CONFIGURED", "Configure an SMTP server and valid sender address before sending invoices")
		return
	}
	invoiceID := r.PathValue("id")
	a.billingCommand(w, r, "invoice.send:"+invoiceID, in, func(tx pgx.Tx) (any, error) {
		ctx := r.Context()
		var state string
		if err := tx.QueryRow(ctx, `SELECT state FROM invoices WHERE id=$1 FOR UPDATE`, invoiceID).Scan(&state); err != nil {
			return nil, err
		}
		if state != "issued" {
			return nil, problem(409, "INVALID_STATE", "Only issued invoices can be sent")
		}
		var id, deliveryState string
		err := tx.QueryRow(ctx, `SELECT id::text,state FROM invoice_deliveries WHERE invoice_id=$1`, invoiceID).Scan(&id, &deliveryState)
		if err == nil {
			return map[string]any{"id": id, "state": deliveryState}, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		d, err := invoiceFrom(ctx, tx, invoiceID)
		if err != nil {
			return nil, err
		}
		if !validMailAddress(d.Customer.Email) {
			return nil, problem(422, "RECIPIENT_REQUIRED", "The frozen customer invoice snapshot needs a valid email address")
		}
		if d.PDFPath == nil {
			return nil, problem(503, "PDF_UNAVAILABLE", "The retained invoice PDF is unavailable")
		}
		_, hash, err := a.readMailPDF(*d.PDFPath)
		if err != nil {
			return nil, problem(503, "PDF_UNAVAILABLE", "The retained invoice PDF is unavailable or exceeds the 20 MiB email limit")
		}
		var created time.Time
		err = tx.QueryRow(ctx, `INSERT INTO invoice_deliveries(invoice_id,sender,recipient,message_id,pdf_path,pdf_sha256,created_by) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id::text,created_at`, invoiceID, a.Config.SMTPFrom, d.Customer.Email, "<seliworks-"+token()+"@"+a.Config.SMTPFrom[strings.LastIndex(a.Config.SMTPFrom, "@")+1:]+">", *d.PDFPath, hash, actor(r).ID).Scan(&id, &created)
		if err == nil {
			err = a.audit(ctx, tx, actor(r).ID, "invoice.delivery_queued", invoiceID, nil, map[string]any{"delivery_id": id})
		}
		return map[string]any{"id": id, "state": "queued"}, err
	})
}
func (a *App) listInvoiceMail(w http.ResponseWriter, r *http.Request) {
	var exists bool
	if err := a.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM invoices WHERE id=$1)`, r.PathValue("id")).Scan(&exists); err != nil {
		handleError(w, r, err)
		return
	}
	if !exists {
		fail(w, r, 404, "NOT_FOUND", "Invoice not found")
		return
	}
	items, err := queryItems(r.Context(), a.DB, `SELECT jsonb_build_object('id',d.id,'invoice_id',d.invoice_id,'state',d.state,'recipient',d.recipient,'message_id',d.message_id,'attempts',d.attempts,'total_attempts',d.total_attempts,'next_attempt_at',d.next_attempt_at,'last_error',d.last_error,'created_at',d.created_at,'sent_at',d.sent_at,'attempt_history',coalesce((SELECT jsonb_agg(jsonb_build_object('attempt',a.attempt,'state',a.state,'error_code',a.error_code,'started_at',a.started_at,'finished_at',a.finished_at) ORDER BY a.attempt) FROM invoice_delivery_attempts a WHERE a.delivery_id=d.id),'[]'::jsonb)) FROM invoice_deliveries d WHERE invoice_id=$1 ORDER BY created_at DESC`, r.PathValue("id"))
	if err != nil {
		handleError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": len(items)})
}
func (a *App) retryInvoiceMail(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Confirm bool `json:"confirm_duplicate_risk"`
	}
	if err := decode(r, &in); err != nil {
		handleError(w, r, err)
		return
	}
	id := r.PathValue("id")
	a.billingCommand(w, r, "invoice.delivery_retry:"+id, in, func(tx pgx.Tx) (any, error) {
		ctx := r.Context()
		var state, invoiceState string
		err := tx.QueryRow(ctx, `SELECT d.state,i.state FROM invoice_deliveries d JOIN invoices i ON i.id=d.invoice_id WHERE d.id=$1 FOR UPDATE OF d,i`, id).Scan(&state, &invoiceState)
		if err != nil {
			return nil, err
		}
		if invoiceState != "issued" {
			return nil, problem(409, "INVALID_STATE", "Only issued invoice deliveries can be retried")
		}
		if state != "failed" && state != "uncertain" && state != "retry" {
			return nil, problem(409, "INVALID_STATE", "Delivery cannot be retried from its current state")
		}
		if state == "uncertain" && !in.Confirm {
			return nil, problem(422, "DUPLICATE_CONFIRMATION_REQUIRED", "SMTP may already have accepted this message. Confirm the duplicate delivery risk before retrying")
		}
		_, err = tx.Exec(ctx, `UPDATE invoice_deliveries SET state='queued',attempts=0,next_attempt_at=clock_timestamp(),lease_token=NULL,lease_until=NULL,last_error='',updated_at=clock_timestamp() WHERE id=$1`, id)
		if err == nil {
			err = a.audit(ctx, tx, actor(r).ID, "invoice.delivery_retry", id, map[string]any{"state": state}, map[string]any{"duplicate_risk_confirmed": in.Confirm})
		}
		return map[string]any{"id": id, "state": "queued"}, err
	})
}
func (a *App) readMailPDF(path string) ([]byte, string, error) {
	root, err := os.OpenRoot(a.Config.DataDir)
	if err != nil {
		return nil, "", err
	}
	defer root.Close()
	file, err := root.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxMailPDF+1))
	if err != nil {
		return nil, "", err
	}
	if len(raw) > maxMailPDF || !bytes.HasPrefix(raw, []byte("%PDF-")) {
		return nil, "", errors.New("invalid retained PDF")
	}
	sum := sha256.Sum256(raw)
	return raw, hex.EncodeToString(sum[:]), nil
}

// RunMail drains the durable outbox. Leases and retry dates are PostgreSQL state;
// this ticker only wakes the worker and is unrelated to work-session recording.
func (a *App) RunMail(ctx context.Context) {
	if a.Config.SMTPHost == "" {
		return
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		processed, err := a.processMail(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			slog.Error("mail worker database operation failed", "type", errorClass(err))
		}
		if processed && err == nil {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (a *App) claimMail(ctx context.Context) (mailJob, bool, error) {
	var job mailJob
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		return job, false, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `UPDATE invoice_delivery_attempts a SET state='uncertain',error_code='LEASE_EXPIRED',finished_at=clock_timestamp() FROM invoice_deliveries d WHERE d.id=a.delivery_id AND d.lease_token=a.lease_token AND d.state='sending' AND d.lease_until<=clock_timestamp() AND a.state='sending'`); err != nil {
		return job, false, err
	}
	if _, err = tx.Exec(ctx, `UPDATE invoice_deliveries SET state='uncertain',last_error='LEASE_EXPIRED',lease_until=NULL,lease_token=NULL,updated_at=clock_timestamp() WHERE state='sending' AND lease_until<=clock_timestamp()`); err != nil {
		return job, false, err
	}
	err = tx.QueryRow(ctx, `SELECT id::text,invoice_id::text,sender,recipient,message_id,pdf_path,pdf_sha256,attempts,total_attempts,created_at FROM invoice_deliveries WHERE state IN ('queued','retry') AND attempts<5 AND next_attempt_at<=clock_timestamp() ORDER BY next_attempt_at,created_at FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&job.ID, &job.InvoiceID, &job.Sender, &job.Recipient, &job.MessageID, &job.PDFPath, &job.PDFHash, &job.Attempts, &job.TotalAttempts, &job.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return job, false, tx.Commit(ctx)
	}
	if err != nil {
		return job, false, err
	}
	job.Lease = token()
	job.Attempts++
	job.TotalAttempts++
	_, err = tx.Exec(ctx, `UPDATE invoice_deliveries SET state='sending',attempts=$2,total_attempts=$3,lease_token=$4,lease_until=clock_timestamp()+interval '2 minutes',updated_at=clock_timestamp() WHERE id=$1`, job.ID, job.Attempts, job.TotalAttempts, job.Lease)
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO invoice_delivery_attempts(delivery_id,attempt,lease_token,state) VALUES($1,$2,$3,'sending')`, job.ID, job.TotalAttempts, job.Lease)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	return job, err == nil, err
}
func (a *App) processMail(ctx context.Context) (bool, error) {
	job, found, err := a.claimMail(ctx)
	if err != nil || !found {
		return found, err
	}
	d, err := invoiceFrom(ctx, a.DB, job.InvoiceID)
	outcome := mailOutcome{}
	state := ""
	if err != nil {
		outcome.Code = "INVOICE_UNAVAILABLE"
	} else if d.State != "issued" {
		state = "cancelled"
		outcome.Code = "INVOICE_NOT_ISSUED"
	} else {
		raw, hash, e := a.readMailPDF(job.PDFPath)
		if e != nil {
			outcome.Code = "PDF_UNAVAILABLE"
			outcome.Permanent = true
		} else if hash != job.PDFHash {
			outcome.Code = "PDF_CHANGED"
			outcome.Permanent = true
		} else {
			message, e := buildInvoiceMail(job, d, raw)
			if e != nil {
				outcome.Code = "MESSAGE_INVALID"
				outcome.Permanent = true
			} else {
				sendCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
				outcome = sendSMTP(sendCtx, a.Config, job.Sender, job.Recipient, message)
				cancel()
			}
		}
	}
	if state == "" {
		switch {
		case outcome.Accepted:
			state = "sent"
		case outcome.Uncertain:
			state = "uncertain"
		case outcome.Permanent || job.Attempts >= 5:
			state = "failed"
		default:
			state = "retry"
		}
	}
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	tx, err := a.DB.Begin(finishCtx)
	if err != nil {
		return true, err
	}
	defer tx.Rollback(finishCtx)
	delay := int64(30) << uint(job.Attempts-1)
	tag, err := tx.Exec(finishCtx, `UPDATE invoice_deliveries SET state=$3,last_error=$4,next_attempt_at=clock_timestamp()+($5::bigint*interval '1 second'),sent_at=CASE WHEN $3='sent' THEN clock_timestamp() ELSE sent_at END,lease_token=NULL,lease_until=NULL,updated_at=clock_timestamp() WHERE id=$1 AND lease_token=$2 AND state='sending'`, job.ID, job.Lease, state, outcome.Code, delay)
	if err != nil {
		return true, err
	}
	if tag.RowsAffected() == 0 {
		return true, nil
	}
	_, err = tx.Exec(finishCtx, `UPDATE invoice_delivery_attempts SET state=$2,error_code=$3,finished_at=clock_timestamp() WHERE lease_token=$1`, job.Lease, state, outcome.Code)
	if err == nil {
		err = a.audit(finishCtx, tx, "", "invoice.delivery_"+state, job.ID, nil, map[string]any{"attempt": job.TotalAttempts, "error_code": outcome.Code})
	}
	if err == nil {
		err = tx.Commit(finishCtx)
	}
	return true, err
}

func mailFilename(number string) string {
	return strings.Map(func(c rune) rune {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			return c
		}
		return '_'
	}, number) + ".pdf"
}
func buildInvoiceMail(job mailJob, d invoiceDocument, pdf []byte) ([]byte, error) {
	if !validMailAddress(job.Sender) || !validMailAddress(job.Recipient) || strings.ContainsAny(job.MessageID, "\r\n") {
		return nil, errors.New("invalid mail envelope")
	}
	number := ""
	if d.Number != nil {
		number = *d.Number
	}
	number = strings.Join(strings.Fields(number), " ")
	var buf bytes.Buffer
	boundary := "seliworks_" + strings.ReplaceAll(job.ID, "-", "")
	fmt.Fprintf(&buf, "From: %s\r\nTo: %s\r\nDate: %s\r\nMessage-ID: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: %s\r\n\r\n", job.Sender, job.Recipient, job.CreatedAt.UTC().Format(time.RFC1123Z), job.MessageID, mime.QEncoding.Encode("UTF-8", "SeliWorks Rechnung / Invoice "+number), mime.FormatMediaType("multipart/mixed", map[string]string{"boundary": boundary}))
	writer := multipart.NewWriter(&buf)
	if err := writer.SetBoundary(boundary); err != nil {
		return nil, err
	}
	part, err := writer.CreatePart(textproto.MIMEHeader{"Content-Type": {"text/plain; charset=UTF-8"}, "Content-Transfer-Encoding": {"quoted-printable"}})
	if err != nil {
		return nil, err
	}
	text := quotedprintable.NewWriter(part)
	_, err = fmt.Fprintf(text, "Guten Tag,\r\n\r\nanbei erhalten Sie die Rechnung %s von %s.\r\nGesamtbetrag: %s %s.\r\n\r\nHello,\r\n\r\nPlease find attached invoice %s from %s.\r\nTotal: %s %s.\r\n\r\n%s\r\n", number, d.Issuer.Name, d.Total, d.Currency, number, d.Issuer.Name, d.Total, d.Currency, d.Issuer.Name)
	if err != nil {
		return nil, err
	}
	if err = text.Close(); err != nil {
		return nil, err
	}
	part, err = writer.CreatePart(textproto.MIMEHeader{"Content-Type": {"application/pdf"}, "Content-Disposition": {mime.FormatMediaType("attachment", map[string]string{"filename": mailFilename(number)})}, "Content-Transfer-Encoding": {"base64"}})
	if err != nil {
		return nil, err
	}
	for len(pdf) > 0 {
		n := 57
		if len(pdf) < n {
			n = len(pdf)
		}
		if _, err = fmt.Fprintf(part, "%s\r\n", base64.StdEncoding.EncodeToString(pdf[:n])); err != nil {
			return nil, err
		}
		pdf = pdf[n:]
	}
	if err = writer.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
func smtpError(err error, afterData bool) mailOutcome {
	if err == nil {
		return mailOutcome{Accepted: true}
	}
	var response *textproto.Error
	if errors.As(err, &response) {
		return mailOutcome{Permanent: response.Code >= 500, Code: fmt.Sprintf("SMTP_%d", response.Code)}
	}
	if afterData {
		return mailOutcome{Uncertain: true, Code: "SMTP_OUTCOME_UNKNOWN"}
	}
	return mailOutcome{Code: "SMTP_CONNECTION_FAILED"}
}
func sendSMTP(ctx context.Context, c Config, from, to string, message []byte) mailOutcome {
	if !validMailAddress(from) || !validMailAddress(to) {
		return mailOutcome{Permanent: true, Code: "ENVELOPE_INVALID"}
	}
	port := c.SMTPPort
	if port == "" {
		port = "587"
	}
	address := net.JoinHostPort(c.SMTPHost, port)
	tlsConfig := &tls.Config{ServerName: c.SMTPHost, MinVersion: tls.VersionTLS12}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	var conn net.Conn
	var err error
	if c.SMTPImplicitTLS {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: tlsConfig}).DialContext(ctx, "tcp", address)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return smtpError(err, false)
	}
	defer conn.Close()
	deadline := time.Now().Add(45 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err = conn.SetDeadline(deadline); err != nil {
		return smtpError(err, false)
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	client, err := smtp.NewClient(conn, c.SMTPHost)
	if err != nil {
		return smtpError(err, false)
	}
	defer client.Close()
	hostname := "localhost"
	if parsed, e := url.Parse(c.AppURL); e == nil && parsed.Hostname() != "" {
		hostname = parsed.Hostname()
	}
	if err = client.Hello(hostname); err != nil {
		return smtpError(err, false)
	}
	if !c.SMTPImplicitTLS {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err = client.StartTLS(tlsConfig); err != nil {
				return mailOutcome{Permanent: true, Code: "SMTP_TLS_FAILED"}
			}
		} else {
			ip := net.ParseIP(c.SMTPHost)
			if !c.SMTPAllowInsecure || c.Environment == "production" || ip == nil || !ip.IsLoopback() {
				return mailOutcome{Permanent: true, Code: "SMTP_TLS_REQUIRED"}
			}
		}
	}
	if c.SMTPUsername != "" {
		if err = client.Auth(smtp.PlainAuth("", c.SMTPUsername, c.SMTPPassword, c.SMTPHost)); err != nil {
			return smtpError(err, false)
		}
	}
	if err = client.Mail(from); err != nil {
		return smtpError(err, false)
	}
	if err = client.Rcpt(to); err != nil {
		return smtpError(err, false)
	}
	body, err := client.Data()
	if err != nil {
		return smtpError(err, false)
	}
	if _, err = body.Write(message); err != nil {
		return smtpError(err, false)
	}
	if err = body.Close(); err != nil {
		return smtpError(err, true)
	}
	client.Quit()
	return mailOutcome{Accepted: true}
}
