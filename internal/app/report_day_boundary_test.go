package app

import (
	"testing"
	"time"
)

func TestReportDayBoundaryAdvancesAcrossMidnightDSTGap(t *testing.T) {
	cases := []struct {
		zone string
		at   string // local wall time inside the last hour before a DST gap at 00:00
		want string // first instant of the next local day
	}{
		{"America/Santiago", "2024-09-07T23:30:00-04:00", "2024-09-08T01:00:00-03:00"},
		{"America/Havana", "2014-03-08T23:30:00-05:00", "2014-03-09T01:00:00-04:00"},
		{"Asia/Amman", "2021-10-28T23:30:00+03:00", "2021-10-29T00:00:00+03:00"},
		{"Europe/Berlin", "2024-03-30T23:30:00+01:00", "2024-03-31T00:00:00+01:00"},
		{"UTC", "2024-12-31T12:00:00Z", "2025-01-01T00:00:00Z"},
	}
	for _, tc := range cases {
		loc, err := time.LoadLocation(tc.zone)
		if err != nil {
			t.Skipf("time zone data unavailable: %v", err)
		}
		at, _ := time.Parse(time.RFC3339, tc.at)
		want, _ := time.Parse(time.RFC3339, tc.want)
		got := nextLocalMidnight(at, loc)
		if !got.After(at) {
			t.Fatalf("%s: boundary %s does not advance past %s", tc.zone, got, at)
		}
		if !got.Equal(want) {
			t.Fatalf("%s: next day starts at %s, want %s", tc.zone, got.In(loc), want)
		}
	}
}

func TestReportDayBoundaryAlwaysAdvancesForEveryHour(t *testing.T) {
	for _, zone := range []string{"America/Santiago", "America/Havana", "Asia/Beirut", "America/Asuncion", "Asia/Amman", "Europe/Vienna"} {
		loc, err := time.LoadLocation(zone)
		if err != nil {
			t.Skipf("time zone data unavailable: %v", err)
		}
		at := time.Date(2010, 1, 1, 0, 0, 0, 0, time.UTC)
		stop := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		for ; at.Before(stop); at = at.Add(30 * time.Minute) {
			next := nextLocalMidnight(at, loc)
			if !next.After(at) || next.Sub(at) > 25*time.Hour {
				t.Fatalf("%s: boundary %s for %s is not the following day", zone, next, at)
			}
			if last := next.Add(-time.Nanosecond).In(loc); last.YearDay() != at.In(loc).YearDay() {
				t.Fatalf("%s: boundary %s for %s skips the start of the next local day", zone, next.In(loc), at.In(loc))
			}
			if next.In(loc).YearDay() == at.In(loc).YearDay() {
				t.Fatalf("%s: boundary %s for %s stays on the same local day", zone, next.In(loc), at.In(loc))
			}
		}
	}
}
