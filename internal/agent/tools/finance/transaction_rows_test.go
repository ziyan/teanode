package finance_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
)

// inventedBankRows is an invented bank account's list as read off a
// screenshot, newest first as the app shows it, with half-width katakana
// as the app writes it and a running balance on every row.
const inventedBankRows = `{"operation":"import_transactions","institution_name":"Example Bank","account_name":"Savings",
"account_number":"1234567","statement_account_kind":"bank","currency_code":"JPY",
"transaction_rows":[
 {"posted_on":"2026-09-30","description":"ﾃﾞﾝｷﾀﾞｲ","amount":"-4500","running_balance_amount":"344000"},
 {"posted_on":"2026-09-15","description":"ｷﾕｳﾖ ｻﾝﾌﾟﾙ","amount":"200000","transaction_kind":"deposit","running_balance_amount":"348500"},
 {"posted_on":"2026-09-01","description":"サンプル商店","amount":-1500,"running_balance_amount":"148500"}
],
"ledger_balance_amount":"344000","ledger_balance_on":"2026-09-30","ledger_balance_time_zone":"Asia/Tokyo"}`

// The card names the account, the rows, the days, money in and out, what
// was checked and some of the rows, with the descriptions as they will be
// kept.
func TestFinanceToolPreviewsAnImportOfTransactions(test *testing.T) {
	test.Parallel()
	ctx := tools.WithRun(context.Background(), &fakeRun{operations: &fakeOperations{}})
	line := financeTool(test).PreviewLine(ctx, json.RawMessage(inventedBankRows))
	for _, said := range []string{
		`"Example Bank Savings"`, "3 transactions", "account ending 4567", "JPY", "2026-09-01 to 2026-09-30",
		"money in 200000", "money out -6000", "running balances chained on 3 rows, from 150000 to 344000",
		"balance 344000 on 2026-09-30 (Asia/Tokyo)", `"デンキダイ"`, `"サンプル商店"`, "not added again",
	} {
		if !strings.Contains(line, said) {
			test.Errorf("the card %q does not say %s", line, said)
		}
	}
}

// Rows that do not add up are not put to the person: the call is judged a
// read, and running it answers with the row to read again and sends
// nothing.
func TestFinanceToolRefusesRowsThatDoNotAddUp(test *testing.T) {
	test.Parallel()
	misread := strings.Replace(inventedBankRows, `"amount":"200000"`, `"amount":"20000"`, 1)
	tool := financeTool(test)
	if risk := tool.RiskFor(json.RawMessage(misread)); risk != tools.RiskRead {
		test.Errorf("rows that do not add up are %s", risk)
	}
	operations := &fakeOperations{}
	_, err := call(test, operations, misread)
	if err == nil || !strings.Contains(err.Error(), "row 2 (2026-09-15") {
		test.Errorf("the refusal does not name the row: %v", err)
	}
	if len(operations.documents) != 0 {
		test.Errorf("rows that do not add up were sent: %v", operations.documents)
	}
	// A field a row does not take is refused the same way, rather than
	// dropped and the balance left unchecked.
	misnamed := strings.Replace(inventedBankRows, `"running_balance_amount":"344000"`, `"balance":"344000"`, 1)
	if risk := tool.RiskFor(json.RawMessage(misnamed)); risk != tools.RiskRead {
		test.Errorf("a misnamed field is %s", risk)
	}
	if _, err := call(test, &fakeOperations{}, misnamed); err == nil || !strings.Contains(err.Error(), "balance") {
		test.Errorf("a misnamed field answered %v", err)
	}
}

// What is sent is the API's names, the rows' fields included, amounts as
// text even when a model sent a number; what comes back is untrusted and
// says what was checked.
func TestFinanceToolImportsTransactions(test *testing.T) {
	test.Parallel()
	operations := &fakeOperations{answers: map[string]string{
		"ImportTransactions": `{"addedTransactionCount":3,"financeAccountNames":["Example Bank Savings ··4567"]}`,
	}}
	result, err := call(test, operations, inventedBankRows)
	if err != nil {
		test.Fatal(err)
	}
	if !result.Untrusted || !strings.Contains(result.Content, "running balances chained") || !strings.Contains(result.Content, "Example Bank Savings") {
		test.Errorf("%+v", result)
	}
	sent := operations.variables[len(operations.variables)-1]
	if sent["institutionName"] != "Example Bank" || sent["statementAccountKind"] != "bank" || sent["ledgerBalanceTimeZone"] != "Asia/Tokyo" {
		test.Errorf("sent %v", sent)
	}
	rows, _ := sent["transactionRows"].([]map[string]any)
	if len(rows) != 3 || rows[2]["amount"] != "-1500" || rows[1]["runningBalanceAmount"] != "348500" || rows[1]["transactionKind"] != "deposit" {
		test.Errorf("rows sent %v", sent["transactionRows"])
	}
}

// Renaming goes through; deleting is answered with where the person does
// it, and nothing is sent.
func TestFinanceToolRenamesButDoesNotDeleteAStatementAccount(test *testing.T) {
	test.Parallel()
	operations := &fakeOperations{answers: map[string]string{
		"FinanceAccounts":        `[{"id":"account-one","accountName":"Example Card"}]`,
		"RenameStatementAccount": `{"id":"account-one","accountName":"Everyday card"}`,
	}}
	ctx := tools.WithRun(context.Background(), &fakeRun{operations: operations})
	line := financeTool(test).PreviewLine(ctx, json.RawMessage(`{"operation":"rename_statement_account","finance_account_id":"account-one","account_name":"Everyday card"}`))
	if !strings.Contains(line, `"Example Card"`) || !strings.Contains(line, `"Everyday card"`) {
		test.Errorf("the card %q", line)
	}
	if _, err := call(test, operations, `{"operation":"rename_statement_account","finance_account_id":"account-one","account_name":"Everyday card"}`); err != nil {
		test.Fatal(err)
	}
	sent := operations.variables[len(operations.variables)-1]
	if sent["financeAccountId"] != "account-one" || sent["accountName"] != "Everyday card" {
		test.Errorf("sent %v", sent)
	}

	quiet := &fakeOperations{}
	result, err := call(test, quiet, `{"operation":"delete_statement_account","finance_account_id":"account-one"}`)
	if err != nil || !strings.Contains(result.Content, "delete-statement-account") || !strings.Contains(result.Content, "trash") {
		test.Errorf("%+v %v", result, err)
	}
	if len(quiet.documents) != 0 {
		test.Errorf("a delete was sent: %v", quiet.documents)
	}
}
