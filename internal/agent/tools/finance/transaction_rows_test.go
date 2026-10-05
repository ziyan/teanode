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

// With no run to ask the server through, the card names the account as the
// rows do, the rows, the days, money in and out, what was checked and some
// of the rows, with the descriptions as they will be kept.
func TestFinanceToolPreviewsAnImportOfTransactionsFromTheRowsAlone(test *testing.T) {
	test.Parallel()
	line := financeTool(test).PreviewLine(context.Background(), json.RawMessage(inventedBankRows))
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

// inventedPreviewOfAnExistingAccount is the server's preview of rows into
// an existing invented account found by its last digits: five new rows,
// one of them near a stored transaction of the same amount, and two the
// account holds already.
const inventedPreviewOfAnExistingAccount = `{"financeAccountId":"account-invented","accountName":"Example Card ··0042","isNewAccount":false,
"accountMatch":"account_mask","currencyCode":"JPY",
"newTransactionRows":[
 {"rowNumber":1,"postedOn":"2026-09-02","description":"Example Books","amount":"-1200","hasNearbyStoredTransaction":false},
 {"rowNumber":2,"postedOn":"2026-09-05","description":"Example Cafe","amount":"-600","hasNearbyStoredTransaction":true},
 {"rowNumber":4,"postedOn":"2026-09-10","description":"Example Market","amount":"-3000","hasNearbyStoredTransaction":false},
 {"rowNumber":5,"postedOn":"2026-09-12","description":"Example Refund","amount":"500","hasNearbyStoredTransaction":false},
 {"rowNumber":7,"postedOn":"2026-09-20","description":"Example Station","amount":"-200","hasNearbyStoredTransaction":false}],
"presentTransactionRows":[
 {"rowNumber":3,"postedOn":"2026-09-08","description":"Example Shop","amount":"-800","hasNearbyStoredTransaction":false},
 {"rowNumber":6,"postedOn":"2026-09-15","description":"Example Shop","amount":"-800","hasNearbyStoredTransaction":false}],
"firstPostedOn":"2026-09-02","lastPostedOn":"2026-09-20","moneyInAmount":"500","moneyOutAmount":"-5000",
"verificationSummary":"monthly totals matched for 2026-09"}`

// The card is the server's preview: the existing account and how it was
// found, how many rows are new and how many already there, a few of the
// new rows and how many more, their days and money, a new row that may be
// a stored one dated differently, and what was checked. What is sent for
// the preview is what the import sends. A new account is said to be new,
// and an account the server refuses makes a card that says so.
func TestFinanceToolPreviewsAnImportFromTheServer(test *testing.T) {
	test.Parallel()
	operations := &fakeOperations{answers: map[string]string{"PreviewImportTransactions": inventedPreviewOfAnExistingAccount}}
	ctx := tools.WithRun(context.Background(), &fakeRun{operations: operations})
	line := financeTool(test).PreviewLine(ctx, json.RawMessage(inventedBankRows))
	for _, said := range []string{
		`the existing account "Example Card ··0042" (found by the last digits shown)`, "5 new, 2 already there",
		`2026-09-02 "Example Books" -1200`, `2026-09-10 "Example Market" -3000`, "and 2 more",
		"From 2026-09-02 to 2026-09-20, money in 500, money out -5000",
		"1 new row has a stored transaction of the same amount within 3 days", "monthly totals matched for 2026-09",
	} {
		if !strings.Contains(line, said) {
			test.Errorf("the card %q does not say %s", line, said)
		}
	}
	if strings.Contains(line, "Example Station") || strings.Contains(line, "Example Shop") {
		test.Errorf("the card %q names more than the first new rows, or a row already there", line)
	}
	if len(operations.documents) != 1 || !strings.Contains(operations.documents[0], "PreviewImportTransactions(") {
		test.Fatalf("documents %v", operations.documents)
	}
	if sent := operations.variables[0]; sent["institutionName"] != "Example Bank" || sent["accountNumber"] != "1234567" {
		test.Errorf("sent %v", sent)
	}

	fresh := &fakeOperations{answers: map[string]string{"PreviewImportTransactions": `{"accountName":"Example Bank Savings ··4567","isNewAccount":true,
"accountMatch":"new_account","currencyCode":"JPY","newTransactionRows":[{"rowNumber":1,"postedOn":"2026-09-01","description":"サンプル商店","amount":"-1500"}],
"presentTransactionRows":[],"firstPostedOn":"2026-09-01","lastPostedOn":"2026-09-01","moneyInAmount":"0","moneyOutAmount":"-1500",
"verificationSummary":"running balances chained on 3 rows, from 150000 to 344000"}`}}
	line = financeTool(test).PreviewLine(tools.WithRun(context.Background(), &fakeRun{operations: fresh}), json.RawMessage(inventedBankRows))
	if !strings.Contains(line, `a new account "Example Bank Savings ··4567": 1 new, 0 already there`) {
		test.Errorf("the card for a new account %q", line)
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
	// An account number sent as a number could have lost digits, so it is
	// refused rather than stored on the wrong ones.
	numbered := strings.Replace(inventedBankRows, `"account_number":"1234567"`, `"account_number":12345678901234567`, 1)
	if _, err := call(test, &fakeOperations{}, numbered); err == nil || !strings.Contains(err.Error(), "account_number as a string") {
		test.Errorf("an account number sent as a number answered %v", err)
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

	// The last digits alone: no name is needed, the card names the
	// number, and an empty one is said as taking it back.
	line = financeTool(test).PreviewLine(ctx, json.RawMessage(`{"operation":"rename_statement_account","finance_account_id":"account-one","account_mask":"4821"}`))
	if !strings.Contains(line, `"Example Card"`) || !strings.Contains(line, `number ending "4821"`) || strings.Contains(line, "Rename") {
		test.Errorf("the card for a number %q", line)
	}
	line = financeTool(test).PreviewLine(ctx, json.RawMessage(`{"operation":"rename_statement_account","finance_account_id":"account-one","account_mask":""}`))
	if !strings.Contains(line, "Take back the number") || strings.Contains(line, "keep") {
		test.Errorf("the card for taking a number back %q", line)
	}
	if _, err := call(test, operations, `{"operation":"rename_statement_account","finance_account_id":"account-one","account_mask":"4821"}`); err != nil {
		test.Fatal(err)
	}
	sent = operations.variables[len(operations.variables)-1]
	if _, hasName := sent["accountName"]; hasName || sent["accountMask"] != "4821" {
		test.Errorf("a number alone sent %v", sent)
	}
	// account_number is import_transactions' argument; sent to a rename
	// it is refused with what the rename reads, not dropped.
	if _, err := call(test, operations, `{"operation":"rename_statement_account","finance_account_id":"account-one","account_number":"4821"}`); err == nil || !strings.Contains(err.Error(), "account_mask") {
		test.Errorf("account_number on a rename answered %v", err)
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
