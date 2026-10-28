package app

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
)

func TestInvoicePDFKeepsDetailBlocksTogether(t *testing.T) {
	for _, layout := range []string{"entries", "activity_rate"} {
		t.Run(layout, func(t *testing.T) {
			d := paginationInvoice(t)
			d.Issuer.InvoiceLayout = layout
			d.Issuer.InvoiceAppendix = true
			raw, err := renderInvoicePDF(d)
			if err != nil {
				t.Fatal(err)
			}
			pages := invoicePageStreams(t, raw)
			if len(pages) < 2 {
				t.Fatal("pagination fixture must span multiple pages")
			}
			for i, item := range d.Items {
				start := pdfMarkerPage(t, pages, item.Worker)
				end := pdfMarkerPage(t, pages, "Base rate / Basissatz: "+item.Pricing.Segments[0].BaseRate+" EUR/h")
				if start != end {
					t.Errorf("entry %d split across pages %d and %d", i+1, start, end)
				}
			}
			writePaginationArtifact(t, layout, raw)
		})
	}
}

func TestInvoicePDFOversizedDetailRemainsComplete(t *testing.T) {
	d := paginationInvoice(t)
	d.Items = d.Items[:1]
	d.Items[0].Description = "LONGDESCRIPTIONSTART\n" + strings.Repeat("Grünland und Fütterung über mehrere Zeilen.\n", 90) + "LONGDESCRIPTIONEND"
	raw, err := renderInvoicePDF(d)
	if err != nil {
		t.Fatal(err)
	}
	pages := invoicePageStreams(t, raw)
	start := pdfMarkerPage(t, pages, "LONGDESCRIPTIONSTART")
	end := pdfMarkerPage(t, pages, "LONGDESCRIPTIONEND")
	if start >= end {
		t.Fatal("oversized detail should span pages")
	}
	pdfMarkerPage(t, pages, "Base rate / Basissatz: "+d.Items[0].Pricing.Segments[0].BaseRate+" EUR/h")
	writePaginationArtifact(t, "oversized", raw)
}

func TestInvoicePDFWrappedDetailBlocksStayTogether(t *testing.T) {
	d := paginationInvoice(t)
	for i := range d.Items {
		d.Items[i].Description = strings.Repeat("Grünlandpflege, Fütterung und längere dokumentierte Arbeitsschritte. ", 3+i%3) + "\nAbschließende Kontrolle."
		d.Items[i].Pricing.DeductionReason += "\nZusätzliche Begründung mit Umlauten äöü."
	}
	raw, err := renderInvoicePDF(d)
	if err != nil {
		t.Fatal(err)
	}
	pages := invoicePageStreams(t, raw)
	for _, item := range d.Items {
		if pdfMarkerPage(t, pages, item.Worker) != pdfMarkerPage(t, pages, "Base rate / Basissatz: "+item.Pricing.Segments[0].BaseRate+" EUR/h") {
			t.Fatalf("wrapped detail for %s split across pages", item.Worker)
		}
	}
	writePaginationArtifact(t, "wrapped", raw)
}

func TestInvoicePDFUnsupportedUnicodeReturnsError(t *testing.T) {
	d := paginationInvoice(t)
	d.Items[0].Description = "Fütterung 😀"
	if _, err := renderInvoicePDF(d); err == nil || !strings.Contains(err.Error(), "character outside the supported range") {
		t.Fatalf("expected unsupported character error, got %v", err)
	}
}

func TestInvoicePDFMissingGlyphsWrap(t *testing.T) {
	for _, char := range []string{"一", "\u200d"} {
		t.Run(fmt.Sprintf("U+%04X", []rune(char)[0]), func(t *testing.T) {
			pdf := newInvoicePDFMeasurer()
			pdf.SetY(40)
			drawInvoiceDetailBlock(pdf, newInvoicePDFMeasurer(), []invoicePDFLine{{9, 5, strings.Repeat(char, 300)}})
			if pdf.Error() != nil {
				t.Fatal(pdf.Error())
			}
			if pdf.GetY() < 40+25+8 {
				t.Fatalf("missing glyph widths were ignored: content ends at %.2f mm", pdf.GetY())
			}
		})
	}
}

func TestInvoicePDFMultipleActivityText(t *testing.T) {
	d := paginationInvoice(t)
	d.Items = d.Items[:1]
	d.Items[0].ActivityDetails = "ACTIVITYSTART\n" + strings.Repeat("Weitere Taetigkeit\n", 90) + "ACTIVITYEND"
	raw, err := renderInvoicePDF(d)
	if err != nil {
		t.Fatal(err)
	}
	pages := invoicePageStreams(t, raw)
	if pdfMarkerPage(t, pages, "ACTIVITYSTART") >= pdfMarkerPage(t, pages, "ACTIVITYEND") {
		t.Fatal("additional activities did not continue across pages")
	}
	writePaginationArtifact(t, "multiple-activities", raw)
}

func paginationInvoice(t *testing.T) invoiceDocument {
	t.Helper()
	number := "QA-PAGINATION-2026"
	start := time.Date(2026, 10, 12, 6, 0, 0, 0, time.UTC)
	stop := start.Add(8 * time.Hour)
	d := invoiceDocument{Number: &number, Timezone: "Europe/Vienna", Currency: "EUR", Issuer: invoiceParty{Name: "PDF Regression", Tax: "20"}, Customer: invoiceParty{Name: "Testbetrieb"}, From: "2026-10-12", To: "2026-10-24"}
	for i := 0; i < 30; i++ {
		base := fmt.Sprintf("%d.0000", 20+i)
		p, err := calculatePolicyPrice(start, stop, []priceInterval{{Start: start.Add(4 * time.Hour), Stop: start.Add(4*time.Hour + 30*time.Minute)}}, []priceRule{{ID: "fixture", Start: start, Rate: base, Version: 1}}, false, 900000000, "VACATION", &DayPricingPolicy{DayType: "VACATION", Mode: "percentage", Value: "50", Version: 1})
		if err != nil {
			t.Fatal(err)
		}
		p.DeductionReason = "Vereinbarter Abzug – Anfahrt und Vorbereitung."
		d.Items = append(d.Items, invoiceItem{Worker: fmt.Sprintf("WORKER%03d", i), Activity: "Fütterung und Grünlandpflege", Description: "Fütterung mit dokumentierten Arbeitsschritten äöü.", Start: start, Stop: stop, NetUS: p.NetUS, Net: p.Net, Pricing: p})
	}
	return d
}

// Read only the page-content objects emitted by fpdf, without adding a PDF parser dependency.
func invoicePageStreams(t *testing.T, raw []byte) [][]byte {
	t.Helper()
	refs := regexp.MustCompile(`(?s)/Type /Page\s.*?/Contents (\d+) 0 R`).FindAllSubmatch(raw, -1)
	if len(refs) == 0 {
		t.Fatal("PDF must contain page objects")
	}
	var pages [][]byte
	for _, ref := range refs {
		re := regexp.MustCompile(`(?m)^` + string(ref[1]) + ` 0 obj\s`)
		loc := re.FindIndex(raw)
		if loc == nil {
			t.Fatal("missing page content object")
		}
		obj := raw[loc[1]:]
		lengthMatch := regexp.MustCompile(`/Length (\d+)`).FindSubmatch(obj)
		if lengthMatch == nil {
			t.Fatal("missing stream length")
		}
		n, err := strconv.Atoi(string(lengthMatch[1]))
		if err != nil {
			t.Fatal(err)
		}
		begin := bytes.Index(obj, []byte("stream\n")) + len("stream\n")
		if begin < len("stream\n") || begin+n > len(obj) {
			t.Fatal("invalid content stream")
		}
		reader, err := zlib.NewReader(bytes.NewReader(obj[begin : begin+n]))
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		pages = append(pages, content)
	}
	return pages
}

func pdfMarkerPage(t *testing.T, pages [][]byte, marker string) int {
	t.Helper()
	var needle []byte
	for _, c := range utf16.Encode([]rune(marker)) {
		needle = append(needle, byte(c>>8), byte(c))
	}
	page, count := -1, 0
	for i, content := range pages {
		n := bytes.Count(content, needle)
		if n > 0 {
			page = i + 1
			count += n
		}
	}
	if count != 1 {
		t.Fatalf("marker %q occurs %d times", marker, count)
	}
	return page
}

func writePaginationArtifact(t *testing.T, name string, raw []byte) {
	t.Helper()
	if dir := os.Getenv("TEST_ARTIFACT_DIR"); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "invoice-pagination-"+name+".pdf"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
