package connected

import "testing"

// The bill is told beside the estimate only for a row it was read for.
func TestDescribeRowsShowsWhatWasBilled(test *testing.T) {
	billed := 4.5
	rows := describeRows([]*usageRow{
		{Key: "2026-03-01", Cost: 4, Currency: "USD", BilledAmount: &billed, BilledCurrencyCode: "usd"},
		{Key: "2026-03-02", Cost: 1, Currency: "USD"},
	})
	if rows[0]["billed"] != "4.50" || rows[0]["billed_currency"] != "USD" || rows[0]["cost"] != "4.00" {
		test.Errorf("billed row described as %v", rows[0])
	}
	if _, hasBilled := rows[1]["billed"]; hasBilled {
		test.Errorf("a row with no bill was described as billed: %v", rows[1])
	}
}
