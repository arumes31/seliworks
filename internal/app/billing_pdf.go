package app

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
	"golang.org/x/image/font/gofont/goregular"
)

func renderInvoicePDF(d invoiceDocument) ([]byte, error) {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.AddUTF8FontFromBytes("Go", "", goregular.TTF)
	pdf.SetFont("Go", "", 10)
	pdf.SetMargins(16, 15, 16)
	pdf.SetAutoPageBreak(true, 20)
	pdf.AliasNbPages("")
	number := "DRAFT"
	if d.Number != nil {
		number = *d.Number
	}
	pdf.SetTitle("Invoice "+number, true)
	pdf.SetAuthor(d.Issuer.Name, true)
	pdf.SetCreator("SeliWorks", true)
	sectionTitle := "Work details / Leistungsübersicht"
	if d.Issuer.InvoiceLayout == "activity_rate" {
		sectionTitle = "Activity / rate summary · Tätigkeit / Stundensatz"
	}
	pdf.SetHeaderFunc(func() {
		pdf.SetXY(16, 15)
		pdf.SetTextColor(111, 66, 12)
		pdf.SetFont("Go", "", 18)
		pdf.CellFormat(120, 10, "SeliWorks", "", 0, "L", false, 0, "")
		pdf.SetFont("Go", "", 10)
		pdf.SetTextColor(38, 34, 27)
		pdf.CellFormat(58, 10, number, "", 1, "R", false, 0, "")
		pdf.SetDrawColor(245, 158, 11)
		pdf.Line(16, 27, 194, 27)
		pdf.SetXY(16, 32)
		if pdf.PageNo() > 1 {
			pdf.SetFont("Go", "", 9)
			pdf.CellFormat(178, 8, sectionTitle+" (continued / Fortsetzung)", "B", 1, "L", false, 0, "")
		}
	})
	pdf.SetFooterFunc(func() {
		pdf.SetY(-14)
		pdf.SetFont("Go", "", 8)
		pdf.SetTextColor(90, 85, 78)
		pdf.CellFormat(178, 5, fmt.Sprintf("%s  ·  %s  ·  %d / {nb}", d.Issuer.Name, number, pdf.PageNo()), "", 0, "C", false, 0, "")
	})
	pdf.AddPage()
	pdf.SetFont("Go", "", 14)
	pdf.MultiCell(178, 7, d.Issuer.Name, "", "L", false)
	pdf.SetFont("Go", "", 10)
	pdf.MultiCell(178, 5, d.Issuer.Address+"\n"+d.Issuer.Email, "", "L", false)
	if d.Issuer.TaxIdentifier != "" {
		pdf.MultiCell(178, 5, "Tax ID / Steuerkennung: "+d.Issuer.TaxIdentifier, "", "L", false)
	}
	pdf.Ln(7)
	pdf.SetFont("Go", "", 13)
	pdf.CellFormat(178, 8, "Invoice / Rechnung", "", 1, "L", false, 0, "")
	pdf.SetFont("Go", "", 10)
	pdf.MultiCell(178, 5, d.Customer.Name+"\n"+d.Customer.Address+"\n"+d.Customer.Email, "", "L", false)
	pdf.Ln(5)
	issue := "Draft / Entwurf"
	if d.IssuedAt != nil {
		issue = d.IssuedAt.UTC().Format("2006-01-02")
	}
	due := "—"
	if d.DueOn != nil {
		due = *d.DueOn
	}
	pdf.MultiCell(178, 6, fmt.Sprintf("Issued / Ausgestellt: %s     Due / Fällig: %s\nService period / Leistungszeitraum: %s – %s (end exclusive)\nTimezone / Zeitzone: %s", issue, due, d.From, d.To, d.Timezone), "", "L", false)
	pdf.Ln(6)
	pdf.SetFillColor(255, 244, 218)
	pdf.CellFormat(178, 8, sectionTitle, "", 1, "L", true, 0, "")
	loc, err := time.LoadLocation(d.Timezone)
	if err != nil {
		return nil, err
	}
	details := func() {
		measure := newInvoicePDFMeasurer()
		for i, item := range d.Items {
			var lines []invoicePDFLine
			fontSize := 10.0
			lines = append(lines, invoicePDFLine{fontSize, 6, fmt.Sprintf("%d. %s · %s", i+1, item.Activity, item.Worker)})
			fontSize = 9
			lines = append(lines, invoicePDFLine{fontSize, 5, item.Start.In(loc).Format("02.01.2006 15:04 -07:00") + " → " + item.Stop.In(loc).Format("02.01.2006 15:04 -07:00")})
			lines = append(lines, invoicePDFLine{fontSize, 5, item.Description})
			if p := item.Pricing.SpecialDayPolicy; p != nil {
				label := "Urlaubstag / Vacation day"
				if p.DayType == "HOLIDAY" {
					label = "Feiertag / Public holiday"
				}
				extra := "+" + p.Value + "%"
				if p.Mode == "hourly" {
					extra = "+" + p.Value + " " + d.Currency + "/h"
				}
				lines = append(lines, invoicePDFLine{fontSize, 5, label + ": " + extra})
			}
			hours := newDecimalHours(item.NetUS)
			lines = append(lines, invoicePDFLine{fontSize, 5, fmt.Sprintf("Actual net / Nettozeit: %s h     %s %s", hours, item.Net, d.Currency)})
			if item.Pricing.AdditionalDeductionUS > 0 {
				deduction := allocatedDeductionUS(item.Pricing.SourceStart, item.Pricing.SourceStop, item.Start, item.Stop, item.Pricing.Breaks, item.Pricing.AdditionalDeductionUS)
				lines = append(lines, invoicePDFLine{fontSize, 4, "Additional deduction / Zusätzlicher Abzug: " + newDecimalHours(deduction) + " h"})
				if item.Pricing.DeductionReason != "" {
					lines = append(lines, invoicePDFLine{fontSize, 4, item.Pricing.DeductionReason})
				}
			}
			for _, seg := range item.Pricing.Segments {
				x, y := clippedInterval(seg.Start, seg.Stop, item.Start, item.Stop)
				if !y.After(x) {
					continue
				}
				net := netMicroseconds(x, y, item.Pricing.Breaks)
				if item.Pricing.AdditionalDeductionUS > 0 {
					net = billableMicroseconds(item.Pricing.SourceStart, item.Pricing.SourceStop, x, y, item.Pricing.Breaks, item.Pricing.AdditionalDeductionUS)
				}
				lines = append(lines, invoicePDFLine{fontSize, 4, fmt.Sprintf("  %s – %s: %s h × %s %s/h", x.In(loc).Format("02.01 15:04"), y.In(loc).Format("02.01 15:04"), newDecimalHours(net), seg.Rate, d.Currency)})
				if item.Pricing.SpecialDayPolicy != nil && seg.BaseRate != "" {
					lines = append(lines, invoicePDFLine{fontSize, 4, "  Base rate / Basissatz: " + seg.BaseRate + " " + d.Currency + "/h"})
				}
			}
			drawInvoiceDetailBlock(pdf, measure, lines)
		}
	}
	if d.Issuer.InvoiceLayout == "activity_rate" {
		lines, err := summarizeInvoiceItems(d.Items)
		if err != nil {
			return nil, err
		}
		for i, line := range lines {
			if pdf.GetY() > 245 {
				pdf.AddPage()
			}
			pdf.SetFont("Go", "", 10)
			pdf.MultiCell(178, 6, fmt.Sprintf("%d. %s", i+1, line.Activity), "", "L", false)
			pdf.SetFont("Go", "", 9)
			if line.SpecialDayPolicy != nil {
				day := "Urlaubstag / Vacation day"
				if line.DayType == "HOLIDAY" {
					day = "Feiertag / Public holiday"
				}
				pdf.MultiCell(178, 5, day, "", "L", false)
			}
			pdf.MultiCell(178, 5, fmt.Sprintf("%s h × %s %s/h     Net / Netto: %s %s", newDecimalHours(line.NetUS), line.Rate, d.Currency, line.Net, d.Currency), "", "L", false)
			pdf.Ln(4)
		}
	} else {
		details()
	}
	if pdf.GetY() > 232 {
		pdf.AddPage()
	}
	pdf.Ln(5)
	pdf.SetFont("Go", "", 11)
	pdf.MultiCell(178, 7, fmt.Sprintf("Net / Netto: %s %s\nTax / Steuer (%s%%): %s %s\nTotal / Gesamt: %s %s", d.Net, d.Currency, d.Issuer.Tax, d.Tax, d.Currency, d.Total, d.Currency), "", "R", false)
	pdf.Ln(5)
	pdf.SetFont("Go", "", 9)
	pdf.MultiCell(178, 5, "Payment reference / Zahlungsreferenz: "+number+"\n"+d.Issuer.Bank, "", "L", false)
	if d.Issuer.InvoiceFooter != "" {
		pdf.Ln(4)
		pdf.MultiCell(178, 5, d.Issuer.InvoiceFooter, "", "L", false)
	}
	pdf.Ln(4)
	pdf.SetFont("Go", "", 7)
	pdf.MultiCell(178, 4, "Actual net duration; no time rounding. Displayed hours are informational.\nAmounts are allocated consistently and rounded to cents; tax is rounded once per invoice.\nTatsächliche Nettozeit ohne Zeitrundung. Beträge werden auf Cent gerundet, die Steuer einmal je Rechnung.", "", "L", false)
	if d.Issuer.InvoiceLayout == "activity_rate" && d.Issuer.InvoiceAppendix {
		sectionTitle = "Timesheet appendix / Zeitnachweis"
		pdf.AddPage()
		pdf.SetFont("Go", "", 12)
		pdf.MultiCell(178, 8, sectionTitle, "", "L", false)
		details()
	}
	var out bytes.Buffer
	if err = pdf.Output(&out); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
func newDecimalHours(us int64) string {
	whole := us / 3600000000
	fraction := (us % 3600000000) * 10000 / 3600000000
	return fmt.Sprintf("%d.%04d", whole, fraction)
}
func (a *App) persistInvoicePDF(d invoiceDocument) (string, error) {
	raw, err := renderInvoicePDF(d)
	if err != nil {
		return "", err
	}
	if err = os.MkdirAll(a.Config.DataDir, 0700); err != nil {
		return "", err
	}
	root, err := os.OpenRoot(a.Config.DataDir)
	if err != nil {
		return "", err
	}
	defer root.Close()
	if err = root.MkdirAll("invoices", 0700); err != nil {
		return "", err
	}
	name := "invoices/" + d.ID + ".pdf"
	temp := name + "." + token()[:16] + ".tmp"
	file, err := root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	defer root.Remove(temp)
	if _, err = file.Write(raw); err != nil {
		file.Close()
		return "", err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return "", err
	}
	if err = file.Close(); err != nil {
		return "", err
	}
	if err = root.Rename(temp, name); err != nil {
		return "", err
	}
	return name, nil
}
func (a *App) invoicePDF(w http.ResponseWriter, r *http.Request) {
	d, err := invoiceFrom(r.Context(), a.DB, r.PathValue("id"))
	if err != nil {
		handleError(w, r, err)
		return
	}
	if d.State != "issued" && d.State != "reversed" {
		fail(w, r, 409, "NOT_ISSUED", "Issue the invoice before downloading its retained PDF")
		return
	}
	if d.PDFPath == nil {
		fail(w, r, 503, "PDF_UNAVAILABLE", "The retained invoice document is unavailable; restore its private-file backup")
		return
	}
	root, err := os.OpenRoot(a.Config.DataDir)
	if err != nil {
		handleError(w, r, err)
		return
	}
	defer root.Close()
	file, err := root.Open(*d.PDFPath)
	if err != nil {
		handleError(w, r, err)
		return
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		handleError(w, r, err)
		return
	}
	name := "invoice"
	if d.Number != nil {
		name = strings.Map(func(c rune) rune {
			if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' {
				return c
			}
			return '_'
		}, *d.Number)
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.pdf"`, name))
	http.ServeContent(w, r, name+".pdf", stat.ModTime(), file)
}
