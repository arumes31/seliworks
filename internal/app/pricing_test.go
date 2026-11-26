package app

import (
	"testing"
	"time"
)

func priceTime(s string) time.Time {
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return v
}
func TestPricePrecedenceAndBoundaries(t *testing.T) {
	start := priceTime("2026-01-31T22:00:00Z")
	stop := start.Add(4 * time.Hour)
	boundary := start.Add(2 * time.Hour)
	w, f, activity := "worker", "farm", "activity"
	rules := []priceRule{{ID: "org", Rate: "10.0000", Start: start}, {ID: "farm", Farm: &f, Rate: "20", Start: start}, {ID: "worker", Worker: &w, Rate: "30", Start: start}, {ID: "pair", Worker: &w, Farm: &f, Rate: "40", Start: start}, {ID: "specific", Worker: &w, Farm: &f, Activity: &activity, Rate: "60", Start: boundary}}
	breaks := []priceInterval{{Start: boundary.Add(-30 * time.Minute), Stop: boundary.Add(30 * time.Minute)}}
	snapshot, err := calculatePrice(start, stop, breaks, rules, false)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.NetUS != 3*3600000000 || snapshot.Net != "150.00" || !snapshot.Priced {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	if len(snapshot.Segments) != 2 || snapshot.Segments[0].RuleID != "pair" || snapshot.Segments[1].RuleID != "specific" {
		t.Fatalf("precedence: %+v", snapshot.Segments)
	}
	left, lc, err := allocationAmount(snapshot, start, boundary)
	if err != nil {
		t.Fatal(err)
	}
	right, rc, err := allocationAmount(snapshot, boundary, stop)
	if err != nil {
		t.Fatal(err)
	}
	if left+right != snapshot.NetUS || lc+rc != 15000 || lc != 6000 || rc != 9000 {
		t.Fatalf("allocation reconciliation: %d %d %d %d", left, right, lc, rc)
	}
}
func TestPricingAllocationRoundingReconciles(t *testing.T) {
	start := priceTime("2026-01-01T00:00:00Z")
	stop := start.Add(7 * time.Second)
	snapshot, err := calculatePrice(start, stop, nil, []priceRule{{ID: "one", Rate: "25.1234", Start: start}}, false)
	if err != nil {
		t.Fatal(err)
	}
	var sum int64
	for i := 0; i < 7; i++ {
		_, n, e := allocationAmount(snapshot, start.Add(time.Duration(i)*time.Second), start.Add(time.Duration(i+1)*time.Second))
		if e != nil {
			t.Fatal(e)
		}
		sum += n
	}
	_, whole, err := allocationAmount(snapshot, start, stop)
	if err != nil {
		t.Fatal(err)
	}
	if sum != whole || whole != 5 {
		t.Fatalf("parts %d whole %d", sum, whole)
	}
}
func TestMissingRateAndExplicitNonBillable(t *testing.T) {
	start := priceTime("2026-01-01T00:00:00Z")
	for _, nonBillable := range []bool{false, true} {
		s, err := calculatePrice(start, start.Add(time.Hour), nil, nil, nonBillable)
		if err != nil {
			t.Fatal(err)
		}
		if s.Priced != nonBillable || s.NonBillable != nonBillable {
			t.Fatalf("missing vs nonbillable: %+v", s)
		}
	}
}
func TestPriceNoRepeatedRoundingAcrossRateSplits(t *testing.T) {
	start := priceTime("2026-01-01T00:00:00Z")
	boundary := start.Add(time.Second)
	stop := start.Add(2 * time.Second)
	s, err := calculatePrice(start, stop, nil, []priceRule{{ID: "a", Start: start, Stop: &boundary, Rate: "18"}, {ID: "b", Start: boundary, Rate: "18"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if s.Net != "0.01" {
		t.Fatalf("rounding drift %s", s.Net)
	}
	_, a, err := allocationAmount(s, start, boundary)
	if err != nil {
		t.Fatal(err)
	}
	_, b, err := allocationAmount(s, boundary, stop)
	if err != nil {
		t.Fatal(err)
	}
	if a+b != 1 {
		t.Fatalf("split sums %d", a+b)
	}
}

func TestUnclaimedIntervals(t *testing.T) {
	start := priceTime("2026-01-01T00:00:00Z")
	stop := start.Add(4 * time.Hour)
	claims := []priceInterval{{Start: start.Add(-time.Hour), Stop: start.Add(time.Hour)}, {Start: start.Add(2 * time.Hour), Stop: start.Add(3 * time.Hour)}}
	parts := unclaimedIntervals(start, stop, claims)
	if len(parts) != 2 || !parts[0].Start.Equal(start.Add(time.Hour)) || !parts[0].Stop.Equal(start.Add(2*time.Hour)) || !parts[1].Start.Equal(start.Add(3*time.Hour)) || !parts[1].Stop.Equal(stop) {
		t.Fatalf("remaining intervals %+v", parts)
	}
}

func TestAdditionalDeductionRateAndCalendarAllocation(t *testing.T) {
	start := priceTime("2026-01-31T22:00:00Z")
	stop := start.Add(4 * time.Hour)
	boundary := start.Add(2 * time.Hour)
	breaks := []priceInterval{{Start: start.Add(30 * time.Minute), Stop: start.Add(90 * time.Minute)}}
	deduction := int64(37 * time.Minute / time.Microsecond)
	snapshot, err := calculateAdjustedPrice(start, stop, breaks, []priceRule{{ID: "a", Rate: "25", Start: start, Stop: &boundary}, {ID: "b", Rate: "60", Start: boundary}}, false, deduction, false)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.RawNetUS != 10800000000 || snapshot.NetUS != 8580000000 || snapshot.Net != "115.19" {
		t.Fatalf("adjusted source %+v", snapshot)
	}
	var totalNet, totalCents, totalDeduction int64
	for cursor := start; cursor.Before(stop); cursor = cursor.Add(time.Minute) {
		end := cursor.Add(time.Minute)
		net, cents, e := allocationAmount(snapshot, cursor, end)
		if e != nil {
			t.Fatal(e)
		}
		totalNet += net
		totalCents += cents
		totalDeduction += allocatedDeductionUS(start, stop, cursor, end, breaks, deduction)
	}
	if totalNet != snapshot.NetUS || totalCents != 11519 || totalDeduction != deduction {
		t.Fatalf("partition drift net=%d cents=%d deduction=%d", totalNet, totalCents, totalDeduction)
	}
	x, y, err := localDateRange("2026-01-01", "2026-02-01", "Europe/Vienna")
	if err != nil {
		t.Fatal(err)
	}
	net, cents, err := allocationAmount(snapshot, x, y)
	if err != nil {
		t.Fatal(err)
	}
	if net != 1430000000 || cents != 993 {
		t.Fatalf("month clipping %d %d", net, cents)
	}
}
func TestAdditionalDeductionIntegerRemainderAndVacationPolicy(t *testing.T) {
	start := priceTime("2026-01-01T00:00:00Z")
	stop := start.Add(3 * time.Microsecond)
	var sum int64
	for i := 0; i < 3; i++ {
		sum += allocatedDeductionUS(start, stop, start.Add(time.Duration(i)*time.Microsecond), start.Add(time.Duration(i+1)*time.Microsecond), nil, 2)
	}
	if sum != 2 {
		t.Fatalf("integer remainder %d", sum)
	}
	s, err := calculateAdjustedPrice(start, start.Add(time.Hour), nil, []priceRule{{ID: "org", Start: start, Rate: "25"}}, false, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if s.Priced || !s.VacationDay || s.UnpricedReason != "VACATION_POLICY_REQUIRED" {
		t.Fatalf("invented vacation price %+v", s)
	}
	if s, err = calculateAdjustedPrice(start, start.Add(time.Hour), nil, nil, false, 0, true); err != nil || s.Priced || s.UnpricedReason != "" {
		t.Fatalf("missing rate must win over missing vacation policy %+v %v", s, err)
	}
	if _, err = calculateAdjustedPrice(start, stop, nil, nil, false, 4, false); err == nil {
		t.Fatal("excess deduction accepted")
	}
}

func calculatePrice(start, stop time.Time, breaks []priceInterval, rules []priceRule, nonBillable bool) (PriceSnapshot, error) {
	return calculateAdjustedPrice(start, stop, breaks, rules, nonBillable, 0, false)
}
func calculateAdjustedPrice(start, stop time.Time, breaks []priceInterval, rules []priceRule, nonBillable bool, deduction int64, vacation bool) (PriceSnapshot, error) {
	dayType := "REGULAR"
	if vacation {
		dayType = "VACATION"
	}
	return calculatePolicyPrice(start, stop, breaks, rules, nonBillable, deduction, dayType, nil)
}
