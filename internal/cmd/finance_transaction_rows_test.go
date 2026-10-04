package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// rowsServer answers ImportTransactions, PreviewImportTransactions,
// RenameStatementAccount and DeleteStatementAccount with invented results,
// recording what it was sent and which operations.
func rowsServer(test *testing.T) (*httptest.Server, func() []map[string]any, func() []string) {
	test.Helper()
	var mutex sync.Mutex
	var asked []map[string]any
	var operationsAsked []string
	// Each key is the field with the space before it, so the import is not
	// taken for its preview.
	answers := map[string]string{
		" PreviewImportTransactions(": `{"PreviewImportTransactions":{"financeAccountId":"account-invented","accountName":"Example Bank ··4567","isNewAccount":false,"accountMatch":"account_mask","currencyCode":"JPY","newTransactionRows":[{"rowNumber":3,"postedOn":"2026-09-30","description":"デンキダイ","amount":"-4500.0000","hasNearbyStoredTransaction":true}],"presentTransactionRows":[{"rowNumber":1,"postedOn":"2026-09-01","description":"サンプル商店","amount":"-1500.0000","hasNearbyStoredTransaction":false},{"rowNumber":2,"postedOn":"2026-09-15","description":"キユウヨ","amount":"200000.0000","hasNearbyStoredTransaction":false}],"firstPostedOn":"2026-09-30","lastPostedOn":"2026-09-30","moneyInAmount":"0.0000","moneyOutAmount":"-4500.0000","verificationSummary":"running balances chained on 3 rows, from 150000 to 344000"}}`,
		" ImportTransactions(":        `{"ImportTransactions":{"importedAt":"2026-10-01T10:00:00Z","statementImportOrigin":"transaction_rows","statementFileNames":[],"addedTransactionCount":2,"updatedTransactionCount":0,"unchangedTransactionCount":1,"skippedTransactionCount":0,"transactionWithoutFitIdCount":3,"financeAccountIds":["account-invented"],"financeAccountNames":["Example Bank ··4567"]}}`,
		"RenameStatementAccount(":     `{"RenameStatementAccount":{"id":"account-invented","sourceId":"source-invented","providerKind":"statement","accountName":"Rainy day fund","accountKind":"depository","currencyCode":"JPY","isSignInRequired":false,"createdAt":"2026-10-01T10:00:00Z","modifiedAt":"2026-10-01T10:00:00Z"}}`,
		"DeleteStatementAccount(":     `{"DeleteStatementAccount":{"financeAccountId":"account-invented","deletedTransactionCount":3,"deletedAssetCount":1}}`,
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var document struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(request.Body).Decode(&document); err != nil {
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		for operation, answer := range answers {
			if strings.Contains(document.Query, operation) {
				mutex.Lock()
				asked = append(asked, document.Variables)
				operationsAsked = append(operationsAsked, strings.TrimSuffix(strings.TrimSpace(operation), "("))
				mutex.Unlock()
				response.Header().Set("Content-Type", "application/json")
				_, _ = response.Write([]byte(`{"data":` + answer + `}`))
				return
			}
		}
		response.WriteHeader(http.StatusBadRequest)
	}))
	test.Cleanup(server.Close)
	return server, func() []map[string]any {
			mutex.Lock()
			defer mutex.Unlock()
			return append([]map[string]any(nil), asked...)
		}, func() []string {
			mutex.Lock()
			defer mutex.Unlock()
			return append([]string(nil), operationsAsked...)
		}
}

// inventedRowsFile is an invented account's rows as a JSON file, one
// amount written as a number.
const inventedRowsFile = `{"institutionName":"Example Bank","accountNumber":"1234567","statementAccountKind":"bank","currencyCode":"JPY",
"transactionRows":[
 {"postedOn":"2026-09-01","description":"サンプル商店","amount":-1500,"runningBalanceAmount":"148500"},
 {"postedOn":"2026-09-15","description":"ｷﾕｳﾖ","amount":"200000","transactionKind":"deposit","runningBalanceAmount":"348500"},
 {"postedOn":"2026-09-30","description":"ﾃﾞﾝｷﾀﾞｲ","amount":"-4500","runningBalanceAmount":"344000"}
]}`

// import-transactions sends the file's rows as the API names them, says
// what was checked, and refuses rows that do not add up before sending
// anything, naming the row; rename and delete send the account and say
// what happened.
func TestFinanceImportTransactionsAndStatementAccounts(test *testing.T) {
	test.Parallel()
	server, asked, _ := rowsServer(test)
	directory := test.TempDir()
	rowsPath := filepath.Join(directory, "rows.json")
	if err := os.WriteFile(rowsPath, []byte(inventedRowsFile), 0o600); err != nil {
		test.Fatal(err)
	}
	printed, err := runFinanceAgainst(test, server, "import-transactions", rowsPath)
	if err != nil {
		test.Fatalf("import-transactions: %s", err)
	}
	if !strings.Contains(printed, "Example Bank ··4567: 2 added") || !strings.Contains(printed, "running balances chained on 3 rows, from 150000 to 344000") {
		test.Errorf("printed %q", printed)
	}
	sent := asked()
	if len(sent) != 1 || sent[0]["institutionName"] != "Example Bank" || sent[0]["statementAccountKind"] != "bank" {
		test.Fatalf("sent %v", sent)
	}
	rows, _ := sent[0]["transactionRows"].([]any)
	if first, _ := rows[0].(map[string]any); len(rows) != 3 || first["amount"] != "-1500" || first["runningBalanceAmount"] != "148500" {
		test.Errorf("rows sent %v", sent[0]["transactionRows"])
	}

	misread := strings.Replace(inventedRowsFile, `"amount":"200000"`, `"amount":"20000"`, 1)
	misreadPath := filepath.Join(directory, "misread.json")
	if err := os.WriteFile(misreadPath, []byte(misread), 0o600); err != nil {
		test.Fatal(err)
	}
	if _, err := runFinanceAgainst(test, server, "import-transactions", misreadPath); err == nil || !strings.Contains(err.Error(), "row 2 (2026-09-15") {
		test.Errorf("rows that do not add up answered %v", err)
	}
	unknownPath := filepath.Join(directory, "unknown.json")
	if err := os.WriteFile(unknownPath, []byte(strings.Replace(inventedRowsFile, `"runningBalanceAmount":"148500"`, `"balance":"148500"`, 1)), 0o600); err != nil {
		test.Fatal(err)
	}
	if _, err := runFinanceAgainst(test, server, "import-transactions", unknownPath); err == nil || !strings.Contains(err.Error(), "balance") {
		test.Errorf("a field the file does not take answered %v", err)
	}
	if sent := asked(); len(sent) != 1 {
		test.Errorf("refused rows were sent: %v", sent)
	}

	printed, err = runFinanceAgainst(test, server, "rename-statement-account", "account-invented", "Rainy day fund")
	if err != nil || !strings.Contains(printed, "renamed Rainy day fund") {
		test.Errorf("rename printed %q %v", printed, err)
	}
	printed, err = runFinanceAgainst(test, server, "delete-statement-account", "--force", "account-invented")
	if err != nil || !strings.Contains(printed, "deleted, with 3 transactions and 1 assets") {
		test.Errorf("delete printed %q %v", printed, err)
	}
	sent = asked()
	if len(sent) != 3 || sent[1]["accountName"] != "Rainy day fund" || sent[2]["financeAccountId"] != "account-invented" {
		test.Errorf("sent %v", sent)
	}
}

// import-transactions --dry-run asks for the preview and never the import,
// sending the account id the file names, and prints the account and how
// it was found, the counts, each new row with a mark where a stored
// transaction of its amount is near, and what was checked.
func TestFinanceImportTransactionsDryRun(test *testing.T) {
	test.Parallel()
	server, asked, operationsAsked := rowsServer(test)
	rowsPath := filepath.Join(test.TempDir(), "rows.json")
	named := strings.Replace(inventedRowsFile, `"accountNumber":"1234567"`, `"financeAccountId":"account-invented"`, 1)
	if err := os.WriteFile(rowsPath, []byte(named), 0o600); err != nil {
		test.Fatal(err)
	}
	printed, err := runFinanceAgainst(test, server, "import-transactions", "--dry-run", rowsPath)
	if err != nil {
		test.Fatalf("import-transactions --dry-run: %s", err)
	}
	for _, said := range []string{
		"would import into Example Bank ··4567 (account-invented), found by the last digits of its number: 1 new, 2 already there; nothing was written",
		"new  2026-09-30  -4500  デンキダイ  (a stored transaction of the same amount is within 3 days)",
		"from 2026-09-30 to 2026-09-30, money in 0, money out -4500", "checked: running balances chained on 3 rows",
	} {
		if !strings.Contains(printed, said) {
			test.Errorf("printed %q, which does not say %q", printed, said)
		}
	}
	if strings.Contains(printed, "サンプル商店") {
		test.Errorf("printed a row already there as new: %q", printed)
	}
	if operations := operationsAsked(); len(operations) != 1 || operations[0] != "PreviewImportTransactions" {
		test.Fatalf("asked %v", operations)
	}
	if sent := asked(); sent[0]["financeAccountId"] != "account-invented" || sent[0]["accountNumber"] != nil {
		test.Errorf("sent %v", sent[0])
	}

	// PrintJSON writes to the process's standard output, which the
	// fixture does not hold; that the dry run stays a dry run is what is
	// checked.
	if _, err := runFinanceAgainst(test, server, "import-transactions", "--dry-run", "--json", rowsPath); err != nil {
		test.Errorf("--dry-run --json: %v", err)
	}
	for _, operation := range operationsAsked() {
		if operation != "PreviewImportTransactions" {
			test.Errorf("a dry run asked %s", operation)
		}
	}
}
