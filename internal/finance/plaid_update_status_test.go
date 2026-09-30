package finance

import (
	"context"
	"testing"
)

// Plaid's update status on the last page says whether more of the history
// is on its way: incomplete while it gathers, complete after, and complete
// when Plaid does not say.
func TestPlaidSyncSaysWhetherHistoryIsComplete(t *testing.T) {
	for _, testCase := range []struct {
		updateStatus        string
		isHistoryIncomplete bool
	}{
		{`"NOT_READY"`, true},
		{`"INITIAL_UPDATE_COMPLETE"`, true},
		{`"HISTORICAL_UPDATE_COMPLETE"`, false},
		{`null`, false},
	} {
		testServer := newPlaidTestServer(t, func(path string, body map[string]any) (int, string) {
			return 200, `{"added":[],"modified":[],"removed":[],"accounts":` + plaidAccountsJson + `,
				"next_cursor":"cursor-next","has_more":false,"transactions_update_status":` + testCase.updateStatus + `}`
		})
		result, err := newTestPlaid(t, testServer).Sync(context.Background(), "access-example", "")
		if err != nil {
			t.Fatal(err)
		}
		if result.IsHistoryIncomplete != testCase.isHistoryIncomplete {
			t.Errorf("update status %s: history incomplete %v, want %v", testCase.updateStatus, result.IsHistoryIncomplete, testCase.isHistoryIncomplete)
		}
	}
}
