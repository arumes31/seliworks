package app

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
	"golang.org/x/image/font/gofont/goregular"
)

func timesheetDuration(us int64) string {
	seconds := us / 1_000_000
	return fmt.Sprintf("%02d:%02d:%02d", seconds/3600, seconds/60%60, seconds%60)
}

func renderTimesheetPDF(d timesheetDocument) ([]byte, error) {
	loc, err := time.LoadLocation(d.Timezone)
	if err != nil {
		return nil, err
	}
	tr := func(de, en string) string {
		if d.Language == "en" {
			return en
		}
		return de
	}
	title := tr("Persönlicher Zeitnachweis", "Personal timesheet")
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.AddUTF8FontFromBytes("Go", "", goregular.TTF)
	pdf.SetMargins(16, 15, 16)
	pdf.SetAutoPageBreak(true, 20)
	pdf.AliasNbPages("")
	pdf.SetTitle(title, true)
	pdf.SetCreator("SeliWorks", true)
	pdf.SetHeaderFunc(func() {
		pdf.SetXY(16, 15)
		pdf.SetFont("Go", "", 18)
		pdf.SetTextColor(111, 66, 12)
		pdf.CellFormat(178, 10, "SeliWorks", "", 1, "L", false, 0, "")
		pdf.Line(16, 27, 194, 27)
		pdf.SetXY(16, 32)
		pdf.SetFont("Go", "", 10)
		pdf.SetTextColor(38, 34, 27)
		pdf.CellFormat(178, 8, title, "B", 1, "L", false, 0, "")
	})
	pdf.SetFooterFunc(func() {
		pdf.SetY(-14)
		pdf.SetFont("Go", "", 8)
		pdf.CellFormat(178, 5, fmt.Sprintf("%s %d / {nb}", tr("Seite", "Page"), pdf.PageNo()), "", 0, "C", false, 0, "")
	})
	pdf.AddPage()
	pdf.SetFont("Go", "", 11)
	pdf.MultiCell(178, 6, d.Worker+"\n"+d.Organization, "", "L", false)
	pdf.SetFont("Go", "", 9)
	pdf.MultiCell(178, 5, d.From+" – "+d.To+" · "+d.Timezone, "", "L", false)
	pdf.MultiCell(178, 5, tr("Beide Tage eingeschlossen. Nur beendete Einträge; Zeiten an Zeitraumgrenzen sind anteilig. Dauer: Stunden:Minuten:Sekunden.", "Both dates included. Completed entries only; times at period boundaries are prorated. Duration: hours:minutes:seconds."), "", "L", false)
	pdf.Ln(4)
	measure := newInvoicePDFMeasurer()
	totals := timeTotals{}
	states := map[string]string{"STOPPED_PENDING_DETAILS": tr("Details ausständig", "Details needed"), "SUBMITTED": tr("In Prüfung", "In review"), "REJECTED": tr("Änderungen angefordert", "Changes requested"), "APPROVED": tr("Genehmigt", "Approved")}
	for i, e := range d.Items {
		day := tr("Normaler Arbeitstag", "Regular workday")
		if e.DayType == "" {
			day = tr("Tagestyp ausständig", "Day type pending")
		}
		if e.DayType == "VACATION" {
			day = tr("Urlaubstag", "Vacation day")
		}
		if e.DayType == "HOLIDAY" {
			day = tr("Feiertag", "Public holiday")
		}
		activity := e.Activity
		if activity == "" {
			activity = tr("Keine Tätigkeit", "No activity")
		}
		lines := []invoicePDFLine{{10, 6, fmt.Sprintf("%d. %s", i+1, e.Farm)}, {9, 5, activity + " · " + states[e.State] + " · " + day}, {9, 5, e.Start.In(loc).Format("02.01.2006 15:04:05 -07:00") + " → " + e.Stop.In(loc).Format("02.01.2006 15:04:05 -07:00")}}
		if strings.TrimSpace(e.Description) != "" {
			lines = append(lines, invoicePDFLine{9, 5, e.Description})
		}
		lines = append(lines, invoicePDFLine{9, 5, tr("Brutto", "Gross") + ": " + timesheetDuration(e.GrossUS) + "   " + tr("Pausen", "Breaks") + ": " + timesheetDuration(e.BreakUS) + "   " + tr("Zusätzlicher Abzug", "Additional deduction") + ": " + timesheetDuration(e.DeductionUS)})
		if e.DeductionUS > 0 && e.DeductionReason != "" {
			lines = append(lines, invoicePDFLine{9, 5, e.DeductionReason})
		}
		lines = append(lines, invoicePDFLine{10, 6, tr("Nettozeit", "Net time") + ": " + timesheetDuration(e.NetUS)})
		drawInvoiceDetailBlock(pdf, measure, lines)
		totals.Gross += e.GrossUS
		totals.Break += e.BreakUS
		totals.Deduction += e.DeductionUS
		totals.Net += e.NetUS
	}
	drawInvoiceDetailBlock(pdf, measure, []invoicePDFLine{{11, 7, tr("Gesamt", "Total")}, {9, 5, fmt.Sprintf("%d %s", len(d.Items), tr("Einträge", "entries"))}, {9, 5, tr("Brutto", "Gross") + ": " + timesheetDuration(totals.Gross)}, {9, 5, tr("Pausen", "Breaks") + ": " + timesheetDuration(totals.Break)}, {9, 5, tr("Zusätzlicher Abzug", "Additional deduction") + ": " + timesheetDuration(totals.Deduction)}, {11, 7, tr("Nettozeit", "Net time") + ": " + timesheetDuration(totals.Net)}})
	var out bytes.Buffer
	if err = pdf.Output(&out); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
