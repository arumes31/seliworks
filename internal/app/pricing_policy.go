package app

import (
	"fmt"
	"math/big"
	"strings"
)

// effectiveRateScale preserves every decimal produced by a four-decimal base
// rate and a four-decimal percentage, including the division by 100.
const effectiveRateScale int64 = 10000000000

type DayPricingPolicy struct {
	DayType string `json:"day_type"`
	Mode    string `json:"mode"`
	Value   string `json:"value"`
	Version int64  `json:"version"`
}

func validDayPolicy(dayType string, policy *DayPricingPolicy) bool {
	if policy == nil || policy.DayType != dayType || policy.Version < 1 || (policy.Mode != "percentage" && policy.Mode != "hourly") {
		return false
	}
	_, err := decimalUnits(policy.Value, 4)
	return err == nil
}
func effectiveRateUnits(base string, policy *DayPricingPolicy) (*big.Int, error) {
	units, err := decimalUnits(base, 4)
	if err != nil {
		return nil, err
	}
	if policy == nil {
		return units.Mul(units, big.NewInt(1000000)), nil
	}
	extra, err := decimalUnits(policy.Value, 4)
	if err != nil {
		return nil, err
	}
	switch policy.Mode {
	case "percentage":
		return units.Mul(units, new(big.Int).Add(big.NewInt(1000000), extra)), nil
	case "hourly":
		return units.Mul(units.Add(units, extra), big.NewInt(1000000)), nil
	default:
		return nil, fmt.Errorf("unsupported special-day pricing policy")
	}
}
func effectiveRateString(units *big.Int) string {
	whole, fraction := new(big.Int), new(big.Int)
	whole.QuoRem(units, big.NewInt(effectiveRateScale), fraction)
	digits := fmt.Sprintf("%010d", fraction.Int64())
	digits = strings.TrimRight(digits, "0")
	if len(digits) < 4 {
		digits += strings.Repeat("0", 4-len(digits))
	}
	return whole.String() + "." + digits
}
func rateCentsDenominator(scale int64) *big.Int {
	return new(big.Int).Quo(new(big.Int).Mul(big.NewInt(scale), big.NewInt(3600000000)), big.NewInt(100))
}
func segmentExactRate(segment priceSegment) (*big.Int, *big.Int, error) {
	if segment.RateScale == 0 && segment.RateUnits == "" {
		units, err := decimalUnits(segment.Rate, 4)
		return units, rateCentsDenominator(10000), err
	}
	if segment.RateScale != effectiveRateScale {
		return nil, nil, fmt.Errorf("unsupported rate precision")
	}
	units, ok := new(big.Int).SetString(segment.RateUnits, 10)
	if !ok || units.Sign() < 0 {
		return nil, nil, fmt.Errorf("invalid exact rate snapshot")
	}
	return units, rateCentsDenominator(segment.RateScale), nil
}
