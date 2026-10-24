package app

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestTimesheetInclusiveDates(t *testing.T) {
	for _, tc := range []struct {
		name, from, to string
		hours          int
		invalid        bool
	}{
		{"one day", "2026-10-24", "2026-10-24", 24, false},
		{"DST autumn", "2026-10-25", "2026-10-25", 25, false},
		{"DST spring", "2026-03-29", "2026-03-29", 23, false},
		{"missing", "", "2026-10-25", 0, true},
		{"reversed", "2026-10-25", "2026-10-24", 0, true},
		{"invalid day", "2026-02-30", "2026-03-01", 0, true},
		{"too long", "2025-01-01", "2026-01-01", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start, stop, err := timesheetDateRange(tc.from, tc.to, "Europe/Vienna")
			if tc.invalid {
				if err == nil {
					t.Fatal("expected validation error")
				}
				return
			}
			if err != nil || stop.Sub(start) != time.Duration(tc.hours)*time.Hour {
				t.Fatalf("range: %v %v %v", start, stop, err)
			}
		})
	}
}

func TestTimesheetPDFPagination(t *testing.T) {
	for _, lang := range []string{"de", "en"} {
		t.Run(lang, func(t *testing.T) {
			d := timesheetDocument{Worker: "Own worker", Organization: "SeliWorks", From: "2026-10-01", To: "2026-10-31", Timezone: "Europe/Vienna", Language: lang}
			for i := 0; i < 30; i++ {
				start := time.Date(2026, 10, 1+i, 6, 0, 0, 0, time.UTC)
				d.Items = append(d.Items, timesheetEntry{Farm: fmt.Sprintf("FARM%03d", i), Activity: "Grünlandpflege", Description: strings.Repeat("Längere Beschreibung mit Umlauten äöü. ", i%4+1), State: "SUBMITTED", Start: start, Stop: start.Add(8 * time.Hour), NetUS: int64(i+1) * 3600000000, DayType: "REGULAR"})
			}
			raw, err := renderTimesheetPDF(d)
			if err != nil {
				t.Fatal(err)
			}
			pages := invoicePageStreams(t, raw)
			for i, item := range d.Items {
				a := pdfMarkerPage(t, pages, item.Farm)
				label := "Nettozeit: "
				if lang == "en" {
					label = "Net time: "
				}
				b := pdfMarkerPage(t, pages, label+fmt.Sprintf("%02d:00:00", i+1))
				if a != b {
					t.Fatalf("split detail %d: %d/%d", i, a, b)
				}
			}
			writePaginationArtifact(t, "timesheet-"+lang, raw)
		})
	}
}

func TestTimesheetPDFOversized(t *testing.T) {
	d := timesheetDocument{Worker: "Own worker", Timezone: "Europe/Vienna", Language: "de", From: "2026-10-01", To: "2026-10-01", Items: []timesheetEntry{{Farm: "Farm", Description: "LONGSTART\n" + strings.Repeat("Langer Text\n", 90) + "LONGEND", State: "APPROVED", Start: time.Date(2026, 10, 1, 6, 0, 0, 0, time.UTC), Stop: time.Date(2026, 10, 1, 14, 0, 0, 0, time.UTC), NetUS: 8 * 3600000000}}}
	raw, err := renderTimesheetPDF(d)
	if err != nil {
		t.Fatal(err)
	}
	pages := invoicePageStreams(t, raw)
	if pdfMarkerPage(t, pages, "LONGSTART") >= pdfMarkerPage(t, pages, "LONGEND") {
		t.Fatal("oversized detail missing continuation")
	}
	writePaginationArtifact(t, "timesheet-oversized", raw)
}
