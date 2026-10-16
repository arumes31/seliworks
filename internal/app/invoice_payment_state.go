package app

func invoicePaymentState(state string, total, paid int64) string {
	if state == "reversed" && paid > 0 {
		return "credit_balance"
	}
	if state != "issued" {
		return "not_due"
	}
	if paid >= total {
		return "paid"
	}
	if paid > 0 {
		return "partially_paid"
	}
	return "unpaid"
}

const invoicePaymentStateSQL = `CASE WHEN i.state='reversed' AND coalesce(p.paid,0)>0 THEN 'credit_balance' WHEN i.state<>'issued' THEN 'not_due' WHEN coalesce(p.paid,0)>=i.total_cents THEN 'paid' WHEN coalesce(p.paid,0)>0 THEN 'partially_paid' ELSE 'unpaid' END`
