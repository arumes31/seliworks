package app

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"time"
)

func loadReportClaims(ctx context.Context, q querier, id string, from, to time.Time) ([]priceInterval, error) {
	rows, err := q.Query(ctx, `SELECT started_at,stopped_at FROM invoice_allocations WHERE session_id=$1 AND active AND started_at<$3 AND stopped_at>$2`, id, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []priceInterval{}
	for rows.Next() {
		var p priceInterval
		if err = rows.Scan(&p.Start, &p.Stop); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func formatBigCents(value *big.Int) string {
	q, r := new(big.Int), new(big.Int)
	q.QuoRem(value, big.NewInt(100), r)
	return fmt.Sprintf("%s.%02d", q.String(), r.Int64())
}
func reportFinance(ctx context.Context, q querier, from, to time.Time, farmID, workerID, defaultCurrency string, unbilled int64) (map[string]any, error) {
	type amounts struct{ net, paid, balance big.Int }
	currencies := map[string]*amounts{}
	invoices := map[string]string{}
	rows, err := q.Query(ctx, `SELECT al.invoice_id::text,al.started_at,al.stopped_at,al.snapshot,i.currency FROM invoice_allocations al JOIN invoices i ON i.id=al.invoice_id JOIN work_sessions w ON w.id=al.session_id WHERE al.active AND i.state='issued' AND al.started_at<$2 AND al.stopped_at>$1 AND ($3='' OR w.farm_id::text=$3) AND ($4='' OR w.worker_id::text=$4)`, from, to, farmID, workerID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, currency string
		var start, stop time.Time
		var raw []byte
		if err = rows.Scan(&id, &start, &stop, &raw, &currency); err != nil {
			break
		}
		var item invoiceItem
		if err = json.Unmarshal(raw, &item); err != nil {
			break
		}
		x, y := clippedInterval(start, stop, from, to)
		_, cents, e := allocationAmount(item.Pricing, x, y)
		if e != nil {
			err = e
			break
		}
		v := currencies[currency]
		if v == nil {
			v = &amounts{}
			currencies[currency] = v
		}
		v.net.Add(&v.net, big.NewInt(cents))
		invoices[id] = currency
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	for id, currency := range invoices {
		var total, paid int64
		err = q.QueryRow(ctx, `SELECT total_cents,coalesce((SELECT sum(amount_cents) FROM payments WHERE invoice_id=i.id),0)::bigint FROM invoices i WHERE id=$1`, id).Scan(&total, &paid)
		if err != nil {
			return nil, err
		}
		v := currencies[currency]
		v.paid.Add(&v.paid, big.NewInt(paid))
		v.balance.Add(&v.balance, big.NewInt(total-paid))
	}
	if len(currencies) == 0 {
		currencies[defaultCurrency] = &amounts{}
	}
	labels := []string{}
	for currency := range currencies {
		labels = append(labels, currency)
	}
	sort.Strings(labels)
	byCurrency := []map[string]any{}
	for _, currency := range labels {
		v := currencies[currency]
		byCurrency = append(byCurrency, map[string]any{"currency": currency, "invoiced_net": formatBigCents(&v.net), "paid": formatBigCents(&v.paid), "balance": formatBigCents(&v.balance)})
	}
	result := map[string]any{"currency": nil, "invoiced_net": nil, "paid": nil, "balance": nil, "unbilled_us": unbilled, "balance_scope": "whole overlapping issued invoices", "by_currency": byCurrency}
	if len(byCurrency) == 1 {
		for k, v := range byCurrency[0] {
			result[k] = v
		}
	}
	return result, nil
}
