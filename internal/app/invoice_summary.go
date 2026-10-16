package app

import (
	"encoding/json"
	"fmt"
	"math"
)

type invoiceSummaryLine struct {
	Activity         string            `json:"activity_name"`
	Rate             string            `json:"hourly_rate"`
	DayType          string            `json:"day_type"`
	SpecialDayPolicy *DayPricingPolicy `json:"special_day_policy,omitempty"`
	NetUS            int64             `json:"net_us"`
	Net              string            `json:"net"`
}

// Summaries add the exact source-prefix allocations, never displayed hours.
// Different day policies remain separate even when their effective rates match.
func summarizeInvoiceItems(items []invoiceItem) ([]invoiceSummaryLine, error) {
	lines := []invoiceSummaryLine{}
	indexes := map[string]int{}
	amounts := []int64{}
	for _, item := range items {
		var sourceUS, sourceCents int64
		for _, segment := range item.Pricing.Segments {
			start, stop := clippedInterval(segment.Start, segment.Stop, item.Start, item.Stop)
			if !stop.After(start) {
				continue
			}
			netUS, cents, err := allocationAmount(item.Pricing, start, stop)
			if err != nil {
				return nil, err
			}
			key, err := json.Marshal([]any{item.Activity, segment.Rate, item.Pricing.DayType, item.Pricing.SpecialDayPolicy})
			if err != nil {
				return nil, err
			}
			index, exists := indexes[string(key)]
			if !exists {
				index = len(lines)
				indexes[string(key)] = index
				lines = append(lines, invoiceSummaryLine{Activity: item.Activity, Rate: segment.Rate, DayType: item.Pricing.DayType, SpecialDayPolicy: item.Pricing.SpecialDayPolicy})
				amounts = append(amounts, 0)
			}
			if netUS < 0 || cents < 0 || netUS > math.MaxInt64-lines[index].NetUS || cents > math.MaxInt64-amounts[index] || netUS > math.MaxInt64-sourceUS || cents > math.MaxInt64-sourceCents {
				return nil, problem(422, "AMOUNT_TOO_LARGE", "Invoice summary exceeds the supported range")
			}
			lines[index].NetUS += netUS
			amounts[index] += cents
			sourceUS += netUS
			sourceCents += cents
		}
		if sourceUS != item.NetUS || moneyString(sourceCents) != item.Net {
			return nil, fmt.Errorf("invoice summary does not reconcile with source allocation")
		}
	}
	for index := range lines {
		lines[index].Net = moneyString(amounts[index])
	}
	return lines, nil
}
