package finance_test

import "testing"

// assets leaves the holdings out unless asked about them, since each
// position is an asset and a brokerage has hundreds; asked for one finance
// account, it keeps both; an empty kind is every kind.
func TestFinanceToolAssetsLeaveHoldingsOutUnlessAsked(test *testing.T) {
	test.Parallel()
	operations := &fakeOperations{answers: map[string]string{"Assets": `[]`}}
	for _, testCase := range []struct {
		call      string
		isHolding any
		assetKind any
	}{
		{`{"operation":"assets"}`, false, nil},
		{`{"operation":"assets","asset_kind":"","text":"house"}`, false, ""},
		{`{"operation":"assets","is_holding":true}`, true, nil},
		{`{"operation":"assets","finance_account_id":"account-one"}`, nil, nil},
	} {
		if _, err := call(test, operations, testCase.call); err != nil {
			test.Fatalf("%s: %v", testCase.call, err)
		}
		sent := operations.variables[len(operations.variables)-1]
		if sent["isHolding"] != testCase.isHolding || sent["assetKind"] != testCase.assetKind {
			test.Errorf("%s sent %v", testCase.call, sent)
		}
	}
}
