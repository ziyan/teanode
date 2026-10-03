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

// rowsServer answers ImportTransactions, RenameStatementAccount and
// DeleteStatementAccount with invented results, recording what it was
// sent.
func rowsServer(test *testing.T) (*httptest.Server, func() []map[string]any) {
	test.Helper()
	var mutex sync.Mutex
	var asked []map[string]any
	answers := map[string]string{
		"ImportTransactions(":     `{"ImportTransactions":{"importedAt":"2026-10-01T10:00:00Z","statementImportOrigin":"transaction_rows","statementFileNames":[],"addedTransactionCount":2,"updatedTransactionCount":0,"unchangedTransactionCount":1,"skippedTransactionCount":0,"transactionWithoutFitIdCount":3,"financeAccountIds":["account-invented"],"financeAccountNames":["Example Bank ··4567"]}}`,
		"RenameStatementAccount(": `{"RenameStatementAccount":{"id":"account-invented","sourceId":"source-invented","providerKind":"statement","accountName":"Rainy day fund","accountKind":"depository","currencyCode":"JPY","isSignInRequired":false,"createdAt":"2026-10-01T10:00:00Z","modifiedAt":"2026-10-01T10:00:00Z"}}`,
		"DeleteStatementAccount(": `{"DeleteStatementAccount":{"financeAccountId":"account-invented","deletedTransactionCount":3,"deletedAssetCount":1}}`,
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
	server, asked := rowsServer(test)
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
