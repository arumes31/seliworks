package app

import (
	"github.com/go-pdf/fpdf"
	"golang.org/x/image/font/gofont/goregular"
)

type invoicePDFLine struct {
	fontSize, height float64
	text             string
}

func newInvoicePDFMeasurer() *fpdf.Fpdf {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.AddUTF8FontFromBytes("Go", "", goregular.TTF)
	pdf.SetMargins(16, 15, 16)
	pdf.SetAutoPageBreak(false, 20)
	pdf.AddPage()
	return pdf
}

func drawInvoiceDetailBlock(pdf, measure *fpdf.Fpdf, paragraphs []invoicePDFLine) {
	const width = 178
	// Continuation headers finish at 40 mm; keep the separator with the entry.
	const contentTop, separatorHeight = 40, 8
	_, pageHeight := pdf.GetPageSize()
	_, _, _, bottom := pdf.GetMargins()
	// Measure with the same renderer so Unicode widths and line breaks match.
	measure.SetXY(16, 0)
	for _, paragraph := range paragraphs {
		measure.SetFont("Go", "", paragraph.fontSize)
		measure.MultiCell(width, paragraph.height, paragraph.text, "", "L", false)
	}
	if err := measure.Error(); err != nil {
		pdf.SetError(err)
		return
	}
	height := measure.GetY() + separatorHeight
	// Oversized entries must still flow across pages through the normal page break.
	if height <= pageHeight-bottom-contentTop && pdf.GetY()+height > pageHeight-bottom {
		pdf.AddPage()
	}
	pdf.SetTextColor(38, 34, 27)
	for _, line := range paragraphs {
		pdf.SetFont("Go", "", line.fontSize)
		pdf.MultiCell(width, line.height, line.text, "", "L", false)
	}
	pdf.Ln(4)
	pdf.SetDrawColor(227, 220, 208)
	pdf.Line(16, pdf.GetY(), 194, pdf.GetY())
	pdf.Ln(4)
}
