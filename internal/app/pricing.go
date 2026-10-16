package app

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const pricePolicy = "actual-microseconds; proportional-floor-deduction; exact-special-day-hourly-rate; cumulative-half-up-cents; tax-half-up-invoice; v3"

var decimalPattern = regexp.MustCompile(`^[0-9]{1,10}(\.[0-9]{1,4})?$`)

type priceInterval struct {
	Start time.Time `json:"started_at"`
	Stop  time.Time `json:"stopped_at"`
}
type priceRule struct {
	ID       string     `json:"id"`
	Worker   *string    `json:"worker_id"`
	Farm     *string    `json:"farm_id"`
	Activity *string    `json:"activity_id"`
	Rate     string     `json:"hourly_rate"`
	Start    time.Time  `json:"effective_from"`
	Stop     *time.Time `json:"effective_to"`
	Version  int64      `json:"version"`
}
type priceSegment struct {
	Start       time.Time `json:"started_at"`
	Stop        time.Time `json:"stopped_at"`
	NetUS       int64     `json:"net_us"`
	Rate        string    `json:"hourly_rate"`
	BaseRate    string    `json:"base_hourly_rate,omitempty"`
	RateUnits   string    `json:"rate_units,omitempty"`
	RateScale   int64     `json:"rate_scale,omitempty"`
	RuleID      string    `json:"rule_id"`
	RuleVersion int64     `json:"rule_version"`
	Prefix      string    `json:"prefix_units"`
}
type PriceSnapshot struct {
	SourceStart           time.Time         `json:"source_start"`
	SourceStop            time.Time         `json:"source_stop"`
	RawNetUS              int64             `json:"raw_net_us"`
	AdditionalDeductionUS int64             `json:"additional_deduction_us"`
	DeductionReason       string            `json:"deduction_reason"`
	VacationDay           bool              `json:"is_vacation_day"`
	DayType               string            `json:"day_type"`
	SpecialDayPolicy      *DayPricingPolicy `json:"special_day_policy,omitempty"`
	UnpricedReason        string            `json:"unpriced_reason,omitempty"`
	Priced                bool              `json:"priced"`
	NonBillable           bool              `json:"non_billable"`
	Policy                string            `json:"policy"`
	Currency              string            `json:"currency"`
	TaxPercent            string            `json:"tax_percent"`
	Segments              []priceSegment    `json:"segments"`
	Breaks                []priceInterval   `json:"breaks"`
	NetUS                 int64             `json:"net_us"`
	Net                   string            `json:"net"`
}

func decimalUnits(value string, places int) (*big.Int, error) {
	if !decimalPattern.MatchString(value) {
		return nil, fmt.Errorf("invalid decimal")
	}
	parts := strings.Split(value, ".")
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
	}
	if len(fraction) > places {
		return nil, fmt.Errorf("too many decimal places")
	}
	n, ok := new(big.Int).SetString(parts[0]+fraction+strings.Repeat("0", places-len(fraction)), 10)
	if !ok {
		return nil, fmt.Errorf("invalid decimal")
	}
	return n, nil
}
func roundedRatio(n, d *big.Int) *big.Int {
	q, r := new(big.Int), new(big.Int)
	q.QuoRem(n, d, r)
	if new(big.Int).Lsh(r, 1).Cmp(d) >= 0 {
		q.Add(q, big.NewInt(1))
	}
	return q
}
func moneyString(cents int64) string { return fmt.Sprintf("%d.%02d", cents/100, cents%100) }
func ratePriority(r priceRule) int {
	if r.Worker != nil && r.Farm != nil && r.Activity != nil {
		return 5
	}
	if r.Worker != nil && r.Farm != nil {
		return 4
	}
	if r.Worker != nil {
		return 3
	}
	if r.Farm != nil {
		return 2
	}
	return 1
}
func clippedInterval(start, stop, from, to time.Time) (time.Time, time.Time) {
	if start.Before(from) {
		start = from
	}
	if stop.After(to) {
		stop = to
	}
	return start, stop
}
func netMicroseconds(start, stop time.Time, breaks []priceInterval) int64 {
	if !stop.After(start) {
		return 0
	}
	n := stop.Sub(start).Microseconds()
	for _, b := range breaks {
		x, y := clippedInterval(b.Start, b.Stop, start, stop)
		if y.After(x) {
			n -= y.Sub(x).Microseconds()
		}
	}
	return n
}

// allocatedDeductionUS uses cumulative integer floors so any disjoint partition
// of a source interval receives exactly the original unsited deduction in total.
func allocatedDeductionUS(start, stop, from, to time.Time, breaks []priceInterval, deduction int64) int64 {
	if deduction <= 0 {
		return 0
	}
	x, y := clippedInterval(start, stop, from, to)
	if !y.After(x) {
		return 0
	}
	total := netMicroseconds(start, stop, breaks)
	if total <= 0 {
		return 0
	}
	prefix := func(end time.Time) *big.Int {
		return new(big.Int).Quo(new(big.Int).Mul(big.NewInt(deduction), big.NewInt(netMicroseconds(start, end, breaks))), big.NewInt(total))
	}
	return new(big.Int).Sub(prefix(y), prefix(x)).Int64()
}
func billableMicroseconds(start, stop, from, to time.Time, breaks []priceInterval, deduction int64) int64 {
	x, y := clippedInterval(start, stop, from, to)
	return netMicroseconds(x, y, breaks) - allocatedDeductionUS(start, stop, x, y, breaks, deduction)
}

// unclaimedIntervals subtracts ordered, disjoint active billing claims from the
// requested half-open interval. The exclusion constraint also guards concurrent inserts.
func unclaimedIntervals(start, stop time.Time, claims []priceInterval) []priceInterval {
	parts := []priceInterval{}
	cursor := start
	for _, c := range claims {
		if !c.Stop.After(cursor) {
			continue
		}
		if !c.Start.Before(stop) {
			break
		}
		if c.Start.After(cursor) {
			parts = append(parts, priceInterval{Start: cursor, Stop: c.Start})
		}
		if c.Stop.After(cursor) {
			cursor = c.Stop
		}
		if !cursor.Before(stop) {
			break
		}
	}
	if cursor.Before(stop) {
		parts = append(parts, priceInterval{Start: cursor, Stop: stop})
	}
	return parts
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
func calculatePolicyPrice(start, stop time.Time, breaks []priceInterval, rules []priceRule, nonBillable bool, deduction int64, dayType string, policy *DayPricingPolicy) (PriceSnapshot, error) {
	if dayType == "" {
		dayType = "REGULAR"
	}
	if dayType != "REGULAR" && dayType != "VACATION" && dayType != "HOLIDAY" {
		return PriceSnapshot{}, problem(422, "INVALID_DAY_TYPE", "Choose regular work, work on a vacation day, or work on a public holiday")
	}
	rawNet := netMicroseconds(start, stop, breaks)
	s := PriceSnapshot{SourceStart: start, SourceStop: stop, RawNetUS: rawNet, AdditionalDeductionUS: deduction, VacationDay: dayType == "VACATION", DayType: dayType, Priced: true, NonBillable: nonBillable, Policy: pricePolicy, Breaks: breaks, Segments: []priceSegment{}, NetUS: rawNet - deduction, Net: "0.00"}
	var appliedPolicy *DayPricingPolicy
	if dayType != "REGULAR" && validDayPolicy(dayType, policy) {
		copy := *policy
		appliedPolicy = &copy
		s.SpecialDayPolicy = appliedPolicy
	}
	if deduction < 0 || deduction > rawNet {
		return s, problem(422, "INVALID_DEDUCTION", "Additional time deduction must be between zero and the duration remaining after breaks")
	}
	if nonBillable {
		return s, nil
	}
	points := []time.Time{start, stop}
	for _, r := range rules {
		if r.Start.After(start) && r.Start.Before(stop) {
			points = append(points, r.Start)
		}
		if r.Stop != nil && r.Stop.After(start) && r.Stop.Before(stop) {
			points = append(points, *r.Stop)
		}
	}
	sort.Slice(points, func(i, j int) bool { return points[i].Before(points[j]) })
	prefix := new(big.Int)
	for i := 0; i < len(points)-1; i++ {
		x, y := points[i], points[i+1]
		if !y.After(x) {
			continue
		}
		n := billableMicroseconds(start, stop, x, y, breaks, deduction)
		var chosen *priceRule
		for j := range rules {
			r := &rules[j]
			if !r.Start.After(x) && (r.Stop == nil || r.Stop.After(x)) && (chosen == nil || ratePriority(*r) > ratePriority(*chosen)) {
				chosen = r
			}
		}
		if chosen == nil {
			if n > 0 {
				s.Priced = false
			}
			continue
		}
		units, err := effectiveRateUnits(chosen.Rate, appliedPolicy)
		if err != nil {
			return s, err
		}
		s.Segments = append(s.Segments, priceSegment{Start: x, Stop: y, NetUS: n, Rate: effectiveRateString(units), BaseRate: chosen.Rate, RateUnits: units.String(), RateScale: effectiveRateScale, RuleID: chosen.ID, RuleVersion: chosen.Version, Prefix: prefix.String()})
		prefix.Add(prefix, new(big.Int).Mul(units, big.NewInt(n)))
	}
	cents := roundedRatio(prefix, rateCentsDenominator(effectiveRateScale))
	if !cents.IsInt64() {
		return s, problem(422, "AMOUNT_TOO_LARGE", "Calculated charges exceed the supported range. Review the hourly rate and surcharge")
	}
	s.Net = moneyString(cents.Int64())
	if dayType != "REGULAR" && appliedPolicy == nil {
		s.Priced = false
		s.UnpricedReason = dayType + "_POLICY_REQUIRED"
	}
	return s, nil
}
func allocationAmount(s PriceSnapshot, from, to time.Time) (int64, int64, error) {
	var total, net int64
	for _, seg := range s.Segments {
		x, y := clippedInterval(seg.Start, seg.Stop, from, to)
		if !y.After(x) {
			continue
		}
		n := netMicroseconds(x, y, s.Breaks)
		if s.AdditionalDeductionUS > 0 {
			n = billableMicroseconds(s.SourceStart, s.SourceStop, x, y, s.Breaks, s.AdditionalDeductionUS)
		}
		net += n
		rate, denominator, err := segmentExactRate(seg)
		if err != nil {
			return 0, 0, err
		}
		prefix, ok := new(big.Int).SetString(seg.Prefix, 10)
		if !ok {
			return 0, 0, fmt.Errorf("invalid price snapshot")
		}
		before := netMicroseconds(seg.Start, x, s.Breaks)
		if s.AdditionalDeductionUS > 0 {
			before = billableMicroseconds(s.SourceStart, s.SourceStop, seg.Start, x, s.Breaks, s.AdditionalDeductionUS)
		}
		u := new(big.Int).Mul(rate, big.NewInt(before))
		u.Add(u, prefix)
		v := new(big.Int).Add(u, new(big.Int).Mul(rate, big.NewInt(n)))
		amount := new(big.Int).Sub(roundedRatio(v, denominator), roundedRatio(u, denominator))
		if !amount.IsInt64() {
			return 0, 0, fmt.Errorf("amount exceeds supported range")
		}
		total += amount.Int64()
	}
	return net, total, nil
}
func (a *App) pricingSnapshot(ctx context.Context, tx pgx.Tx, id string) (any, error) {
	var worker, farm string
	var activity *string
	var start, stop time.Time
	var nonBillable bool
	var dayType string
	var deductionReason string
	var deduction int64
	if err := tx.QueryRow(ctx, `SELECT worker_id::text,farm_id::text,activity_id::text,effective_start,effective_stop,non_billable,additional_deduction_us,coalesce(day_type,CASE WHEN is_vacation_day THEN 'VACATION' ELSE 'REGULAR' END),deduction_reason FROM work_sessions WHERE id=$1`, id).Scan(&worker, &farm, &activity, &start, &stop, &nonBillable, &deduction, &dayType, &deductionReason); err != nil {
		return nil, err
	}
	start, stop = start.UTC(), stop.UTC()
	breaks, err := loadPriceBreaks(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT id::text,worker_id::text,farm_id::text,activity_id::text,hourly_rate::text,effective_from,effective_to,version FROM rates WHERE (worker_id IS NULL OR worker_id=$1) AND (farm_id IS NULL OR farm_id=$2) AND (activity_id IS NULL OR activity_id=$3) AND effective_from<$5 AND (effective_to IS NULL OR effective_to>$4)`, worker, farm, activity, start, stop)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	rules := []priceRule{}
	for rows.Next() {
		var r priceRule
		if err = rows.Scan(&r.ID, &r.Worker, &r.Farm, &r.Activity, &r.Rate, &r.Start, &r.Stop, &r.Version); err != nil {
			return nil, err
		}
		r.Start = r.Start.UTC()
		if r.Stop != nil {
			v := r.Stop.UTC()
			r.Stop = &v
		}
		rules = append(rules, r)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	var dayPolicy *DayPricingPolicy
	if dayType != "REGULAR" && !nonBillable {
		var p DayPricingPolicy
		err = tx.QueryRow(ctx, `SELECT day_type,coalesce(mode,''),coalesce(value::text,''),version FROM special_day_pricing WHERE day_type=$1 FOR SHARE`, dayType).Scan(&p.DayType, &p.Mode, &p.Value, &p.Version)
		if err == nil {
			dayPolicy = &p
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
	}
	snapshot, err := calculatePolicyPrice(start, stop, breaks, rules, nonBillable, deduction, dayType, dayPolicy)
	if err != nil {
		return nil, err
	}
	snapshot.DeductionReason = deductionReason
	err = tx.QueryRow(ctx, `SELECT currency,tax_percent::text FROM organization WHERE id`).Scan(&snapshot.Currency, &snapshot.TaxPercent)
	return snapshot, err
}
func loadPriceBreaks(ctx context.Context, q querier, id string) ([]priceInterval, error) {
	rows, err := q.Query(ctx, `SELECT started_at,stopped_at FROM work_break_intervals WHERE session_id=$1 ORDER BY started_at`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []priceInterval{}
	for rows.Next() {
		var b priceInterval
		if err = rows.Scan(&b.Start, &b.Stop); err != nil {
			return nil, err
		}
		b.Start, b.Stop = b.Start.UTC(), b.Stop.UTC()
		out = append(out, b)
	}
	return out, rows.Err()
}
