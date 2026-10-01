package cmd

import (
	"encoding/json"
	"github.com/ziyan/teanode/internal/client"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode"

	"github.com/ziyan/teanode/internal/api/v1api/apigraph"
)

// financeOperationWords is a finance operation's name as words, with a
// leading Finance dropped: FinanceSpendingSummary is spending and summary.
func financeOperationWords(operation string) []string {
	var words []string
	runes := []rune(strings.TrimPrefix(operation, "Finance"))
	start := 0
	for index := 1; index < len(runes); index++ {
		isBoundary := unicode.IsUpper(runes[index]) &&
			(unicode.IsLower(runes[index-1]) || (index+1 < len(runes) && unicode.IsLower(runes[index+1])))
		if isBoundary {
			words = append(words, strings.ToLower(string(runes[start:index])))
			start = index
		}
	}
	return append(words, strings.ToLower(string(runes[start:])))
}

// financeSubcommandsSpanningOperations are the subcommands that stand for
// more than one operation, or for the operations every source has: named
// for what the person does rather than by the rule.
var financeSubcommandsSpanningOperations = map[string][]string{
	"link-plaid":     {"CreateFinanceLinkToken", "CompleteFinanceLink"},
	"repair":         {"CreateFinanceLinkToken", "CompleteFinanceRepair"},
	"link-simplefin": {"LinkSimpleFIN"},
	// ImportFinanceCredential by the rule is import-finance-credential; the
	// word finance says nothing inside teanode finance.
	"import-credential": {"ImportFinanceCredential"},
	// One transaction or several: categorize-transactions would be a
	// second subcommand doing the same thing.
	"categorize-transaction": {"CategorizeTransaction", "CategorizeTransactions"},
	"sync":                   {},
	"disable-source":         {},
	"enable-source":          {},
	"delete-source":          {},
}

// Every operation of the finance area has a teanode finance subcommand
// named by the rule -- the operation's name in kebab case, a leading
// Finance dropped -- or is one of the few that span two calls, and there
// is no subcommand the API does not back. The rule is what keeps the
// dashboard, the command line and the tool naming one thing one way.
func TestFinanceParityWithTheCommandLine(test *testing.T) {
	test.Parallel()

	subcommands := map[string]bool{}
	for _, subcommand := range NewFinanceCommand().Commands {
		subcommands[subcommand.Name] = true
	}
	covered := map[string]bool{}
	for subcommand, operations := range financeSubcommandsSpanningOperations {
		if !subcommands[subcommand] {
			test.Errorf("teanode finance has no %s", subcommand)
		}
		for _, operation := range operations {
			covered[operation] = true
		}
	}

	operations := map[string]bool{}
	for _, interfaceType := range []reflect.Type{reflect.TypeFor[apigraph.FinanceQuery](), reflect.TypeFor[apigraph.FinanceMutation]()} {
		for index := 0; index < interfaceType.NumMethod(); index++ {
			operation := interfaceType.Method(index).Name
			operations[operation] = true
			if covered[operation] {
				continue
			}
			wanted := strings.Join(financeOperationWords(operation), "-")
			if !subcommands[wanted] {
				test.Errorf("%s has no subcommand teanode finance %s", operation, wanted)
			}
			covered[operation] = true
		}
	}
	if len(operations) == 0 {
		test.Fatal("the finance area has no operations to check")
	}

	byRule := map[string]string{}
	for operation := range operations {
		byRule[strings.Join(financeOperationWords(operation), "-")] = operation
	}
	var unbacked []string
	for subcommand := range subcommands {
		if _, isSpanning := financeSubcommandsSpanningOperations[subcommand]; isSpanning {
			continue
		}
		if _, isBacked := byRule[subcommand]; !isBacked {
			unbacked = append(unbacked, subcommand)
		}
	}
	sort.Strings(unbacked)
	for _, subcommand := range unbacked {
		test.Errorf("teanode finance %s has no operation in the finance area behind it", subcommand)
	}
}

// The rule splits acronyms and drops only a leading Finance.
func TestFinanceOperationWords(test *testing.T) {
	test.Parallel()
	for operation, wanted := range map[string]string{
		"FinanceSpendingSummary": "spending-summary",
		"FinanceAccounts":        "accounts",
		"CreateFinanceLinkToken": "create-finance-link-token",
		"ExchangeRate":           "exchange-rate",
		"LinkSimpleFIN":          "link-simple-fin",
		"SetBudget":              "set-budget",
	} {
		if have := strings.Join(financeOperationWords(operation), "-"); have != wanted {
			test.Errorf("%s: %q, not %q", operation, have, wanted)
		}
	}
}

// Each subcommand calls the operation its name says by the rule: the
// operation its action runs is the one its metadata names.
func TestFinanceSubcommandsCallTheirOperation(test *testing.T) {
	test.Parallel()
	for _, subcommand := range NewFinanceCommand().Commands {
		if _, isSpanning := financeSubcommandsSpanningOperations[subcommand.Name]; isSpanning {
			continue
		}
		operation := operationOf(subcommand)
		if operation == "" {
			test.Errorf("teanode finance %s names no operation", subcommand.Name)
			continue
		}
		if byRule := strings.Join(financeOperationWords(operation), "-"); byRule != subcommand.Name {
			test.Errorf("teanode finance %s calls %s, whose subcommand by the rule is %s", subcommand.Name, operation, byRule)
		}
	}
	for name := range financeSubcommandOperations {
		found := false
		for _, subcommand := range NewFinanceCommand().Commands {
			found = found || subcommand.Name == name
		}
		if !found {
			test.Errorf("%s names an operation for a subcommand there is not", name)
		}
	}
}

// A span reaches back from now; anything else, a day included, is refused
// with the forms it takes.
func TestFinanceDayBack(test *testing.T) {
	test.Parallel()
	now := time.Date(2026, time.March, 31, 12, 0, 0, 0, time.UTC)
	for span, wanted := range map[string]string{
		"30d": "2026-03-01", "2w": "2026-03-17", "1m": "2026-03-03", "1y": "2025-03-31",
	} {
		have, err := dayBack(span, now)
		if err != nil || have != wanted {
			test.Errorf("%s: %q %v, not %q", span, have, err, wanted)
		}
	}
	for _, refused := range []string{"", "d", "thirty days", "-3d", "3x", "2026-01-15", "2026-01"} {
		_, err := dayBack(refused, now)
		if err == nil {
			test.Errorf("%q was taken as a span", refused)
			continue
		}
		if !strings.Contains(err.Error(), "30d, 12w, 6m or 1y") {
			test.Errorf("%q: the refusal does not give the forms: %s", refused, err)
		}
	}
}

// A month is its first and last day, February's last day included, and
// anything that is not a month is refused with the form it takes.
func TestFinanceMonthDays(test *testing.T) {
	test.Parallel()
	for month, wanted := range map[string][2]string{
		"2026-09": {"2026-09-01", "2026-09-30"},
		"2028-02": {"2028-02-01", "2028-02-29"},
		"2026-12": {"2026-12-01", "2026-12-31"},
	} {
		firstDay, lastDay, err := monthDays(month)
		if err != nil || firstDay != wanted[0] || lastDay != wanted[1] {
			test.Errorf("%s: %s to %s %v, not %s to %s", month, firstDay, lastDay, err, wanted[0], wanted[1])
		}
	}
	for _, refused := range []string{"", "2026-13", "2026-09-01", "September"} {
		if _, _, err := monthDays(refused); err == nil || !strings.Contains(err.Error(), "2026-09") {
			test.Errorf("%q was taken as a month, or refused without the form: %v", refused, err)
		}
	}
}

// Amounts read as money: the places the currency is written with, halves
// away from zero, none for the yen.
func TestFinanceMoney(test *testing.T) {
	test.Parallel()
	for _, example := range []struct{ amount, currencyCode, wanted string }{
		{"-42.1700", "USD", "-42.17 USD"},
		{"18000", "USD", "18000.00 USD"},
		{"15599.272", "EUR", "15599.27 EUR"},
		{"139286.605", "USD", "139286.61 USD"},
		{"0.1250", "USD", "0.13 USD"},
		{"-0.001", "USD", "0.00 USD"},
		{"1500.4", "JPY", "1500 JPY"},
		{"12.3456", "KWD", "12.346 KWD"},
		{"5.5", "", "5.50"},
		{"", "USD", ""},
	} {
		if have := money(example.amount, example.currencyCode); have != example.wanted {
			test.Errorf("%q %s: %q, not %q", example.amount, example.currencyCode, have, example.wanted)
		}
	}
}

// A spending summary's grouping heads its column in words.
func TestFinanceGroupByHeader(test *testing.T) {
	test.Parallel()
	for groupBy, wanted := range map[string]string{
		"spendingCategory": "spending category", "providerCategory": "provider category",
		"merchant": "merchant", "month": "month", "financeAccount": "finance account",
	} {
		if have := groupByHeader(groupBy); have != wanted {
			test.Errorf("%s: %q, not %q", groupBy, have, wanted)
		}
	}
}

// trades sends its flags in the API's spelling, a month spread into its
// first and last day, and says when nothing matches.
func TestFinanceTradesSendsItsFlags(test *testing.T) {
	test.Parallel()
	var mutex sync.Mutex
	var asked []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var document struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(request.Body).Decode(&document); err != nil || !strings.Contains(document.Query, "FinanceTrades(") {
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		mutex.Lock()
		asked = append(asked, document.Variables)
		mutex.Unlock()
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"data":{"FinanceTrades":{"financeTrades":[],"nextCursor":null}}}`))
	}))
	test.Cleanup(server.Close)

	printed, err := runFinanceAgainst(test, server, "trades", "--month", "2026-02", "--finance-account", "account-one",
		"--finance-security", "security-one", "--limit", "20", "--after", "cursor-one")
	if err != nil {
		test.Fatal(err)
	}
	if !strings.Contains(printed, "no trades match") {
		test.Errorf("printed %q", printed)
	}
	mutex.Lock()
	defer mutex.Unlock()
	if len(asked) != 1 {
		test.Fatalf("sent %v", asked)
	}
	sent := asked[0]
	if sent["from"] != "2026-02-01" || sent["to"] != "2026-02-28" || sent["financeAccountId"] != "account-one" ||
		sent["financeSecurityId"] != "security-one" || sent["limit"] != float64(20) || sent["after"] != "cursor-one" {
		test.Errorf("sent %v", sent)
	}
}

// A holding's quantity and price read without the zeros their columns pad
// them with, a price keeping the places finer than a cent it has.
func TestFinanceHoldingDecimals(test *testing.T) {
	test.Parallel()
	for amount, wanted := range map[string]string{
		"3.00000000": "3", "0.12500000": "0.125", "120": "120", "-2.50000000": "-2.5", "": "", "not a number.0": "not a number.0",
	} {
		if have := decimal(amount); have != wanted {
			test.Errorf("decimal %q: %q, not %q", amount, have, wanted)
		}
	}
	for _, example := range []struct{ amount, currencyCode, wanted string }{
		{"101.25000000", "USD", "101.25 USD"},
		{"101.00000000", "USD", "101.00 USD"},
		{"0.01234500", "USD", "0.012345 USD"},
		{"1500.00000000", "JPY", "1500 JPY"},
		{"12.50000000", "", "12.50"},
		{"", "USD", ""},
	} {
		if have := unitPrice(example.amount, example.currencyCode); have != example.wanted {
			test.Errorf("unitPrice %q %s: %q, not %q", example.amount, example.currencyCode, have, example.wanted)
		}
	}
}

// --spending-category transfer is the transfer category, even beside a
// spending category the person named transfer themselves, which the
// built-in one was named around. Their own is still theirs by its id.
func TestFinanceTransferIsTheTransferCategory(test *testing.T) {
	test.Parallel()
	var mutex sync.Mutex
	var asked []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var document struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(request.Body).Decode(&document); err != nil {
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(document.Query, "SpendingCategories"):
			_, _ = response.Write([]byte(`{"data":{"SpendingCategories":[` +
				`{"id":"category-own-transfer","spendingCategoryName":"transfer"},` +
				`{"id":"category-transfer","spendingCategoryName":"transfer between own accounts","isTransfer":true}]}}`))
		case strings.Contains(document.Query, "FinanceTransactions("):
			mutex.Lock()
			asked = append(asked, document.Variables)
			mutex.Unlock()
			_, _ = response.Write([]byte(`{"data":{"FinanceTransactions":{"financeTransactions":[],"nextCursor":null}}}`))
		default:
			response.WriteHeader(http.StatusBadRequest)
		}
	}))
	test.Cleanup(server.Close)

	for _, wanted := range []string{"Transfer", "category-own-transfer"} {
		if _, err := runFinanceAgainst(test, server, "transactions", "--month", "2026-02", "--spending-category", wanted); err != nil {
			test.Fatal(err)
		}
	}
	mutex.Lock()
	defer mutex.Unlock()
	if len(asked) != 2 {
		test.Fatalf("sent %v", asked)
	}
	if asked[0]["spendingCategoryId"] != "category-transfer" {
		test.Errorf("transfer sent %v", asked[0])
	}
	if asked[1]["spendingCategoryId"] != "category-own-transfer" {
		test.Errorf("the person's own transfer by its id sent %v", asked[1])
	}
}

// A mirrored copy is listed marked, naming the copy that counts, and
// count-transaction counts it. The rows are read rather than the table,
// which goes to the terminal.
func TestFinanceTransactionsMarkMirroredCopies(test *testing.T) {
	test.Parallel()
	var mutex sync.Mutex
	var asked []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var document struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(request.Body).Decode(&document); err != nil {
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		mutex.Lock()
		asked = append(asked, document.Variables)
		mutex.Unlock()
		response.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(document.Query, "FinanceTransactions("):
			_, _ = response.Write([]byte(`{"data":{"FinanceTransactions":{"financeTransactions":[` +
				`{"id":"fee-counted","financeAccountId":"one","postedOn":"2026-09-15","amount":"-25","currencyCode":"USD","description":"ACCOUNT FEE","isPending":false},` +
				`{"id":"fee-copy","financeAccountId":"two","postedOn":"2026-09-15","amount":"-25","currencyCode":"USD","description":"ACCOUNT FEE","isPending":false,"duplicateOfTransactionId":"fee-counted","duplicateDecidedBy":"mirror_detection"}` +
				`],"nextCursor":null}}}`))
		case strings.Contains(document.Query, "CountTransaction("):
			_, _ = response.Write([]byte(`{"data":{"CountTransaction":{"id":"fee-copy","financeAccountId":"two","postedOn":"2026-09-15","amount":"-25","currencyCode":"USD","description":"ACCOUNT FEE","isPending":false,"duplicateDecidedBy":"person"}}}`))
		default:
			_, _ = response.Write([]byte(`{"data":{"SpendingCategories":[]}}`))
		}
	}))
	test.Cleanup(server.Close)

	if _, err := runFinanceAgainst(test, server, "transactions", "--duplicate-of", "fee-counted"); err != nil {
		test.Fatal(err)
	}
	rows, duplicateCount := financeTransactionRows([]*client.FinanceTransaction{
		{ID: "fee-counted", PostedOn: "2026-09-15", Amount: "-25", CurrencyCode: "USD", Description: "ACCOUNT FEE"},
		{ID: "fee-copy", PostedOn: "2026-09-15", Amount: "-25", CurrencyCode: "USD", Description: "ACCOUNT FEE", DuplicateOfTransactionID: "fee-counted"},
	}, map[string]string{})
	if duplicateCount != 1 || rows[0][2] != "ACCOUNT FEE" || rows[1][2] != "ACCOUNT FEE (duplicate of fee-counted, not counted)" {
		test.Errorf("the rows are %v, %d of them duplicates", rows, duplicateCount)
	}
	printed, err := runFinanceAgainst(test, server, "count-transaction", "fee-copy")
	if err != nil {
		test.Fatal(err)
	}
	if !strings.Contains(printed, "fee-copy: counted") {
		test.Errorf("printed %q", printed)
	}
	mutex.Lock()
	defer mutex.Unlock()
	if asked[0]["duplicateOfTransactionId"] != "fee-counted" || asked[len(asked)-1]["financeTransactionId"] != "fee-copy" {
		test.Errorf("sent %v", asked)
	}
}

// categorize-transaction with one id calls CategorizeTransaction as it
// always has; with several, CategorizeTransactions with every id and the
// rule flag in its plural. propose-spending-rules sends the ids and the
// spending category, named or by id.
func TestFinanceCategorizesSeveralTransactions(test *testing.T) {
	test.Parallel()
	var mutex sync.Mutex
	asked := map[string][]map[string]any{}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var document struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(request.Body).Decode(&document); err != nil {
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		record := func(operation string) {
			mutex.Lock()
			asked[operation] = append(asked[operation], document.Variables)
			mutex.Unlock()
		}
		switch {
		case strings.Contains(document.Query, "SpendingCategories"):
			_, _ = response.Write([]byte(`{"data":{"SpendingCategories":[{"id":"category-dining","spendingCategoryName":"Dining"}]}}`))
		case strings.Contains(document.Query, "CategorizeTransactions("):
			record("CategorizeTransactions")
			_, _ = response.Write([]byte(`{"data":{"CategorizeTransactions":{"financeTransactions":[{"id":"transaction-one"},{"id":"transaction-two"}],` +
				`"spendingRules":[{"id":"rule-one","matchText":"Invented Bistro","spendingCategoryId":"category-dining"}]}}}`))
		case strings.Contains(document.Query, "CategorizeTransaction("):
			record("CategorizeTransaction")
			_, _ = response.Write([]byte(`{"data":{"CategorizeTransaction":{"financeTransaction":{"id":"transaction-one"}}}}`))
		case strings.Contains(document.Query, "ProposeSpendingRules("):
			record("ProposeSpendingRules")
			_, _ = response.Write([]byte(`{"data":{"ProposeSpendingRules":{"spendingRuleProposals":[` +
				`{"matchText":"Invented Bistro","spendingCategoryId":"category-dining","financeTransactionCount":2,"changedTransactionCount":3,` +
				`"aheadOfSpendingRule":{"id":"rule-bistro","matchText":"bistro","spendingCategoryId":"category-groceries","rulePriority":0}}],` +
				`"tooGenericMatchTextCount":1,"changingNumberMatchTextCount":2,"overLimitMatchTextCount":0}}}`))
		default:
			response.WriteHeader(http.StatusBadRequest)
		}
	}))
	test.Cleanup(server.Close)

	if _, err := runFinanceAgainst(test, server, "categorize-transaction", "transaction-one", "dining"); err != nil {
		test.Fatal(err)
	}
	printed, err := runFinanceAgainst(test, server, "categorize-transaction", "--create-spending-rule", "transaction-one", "transaction-two", "Dining")
	if err != nil {
		test.Fatal(err)
	}
	if !strings.Contains(printed, "2 transactions categorized") || !strings.Contains(printed, `"Invented Bistro"`) ||
		!strings.Contains(printed, "2 left out: they hold a number that changes each time") {
		test.Errorf("printed %q", printed)
	}
	printed, err = runFinanceAgainst(test, server, "propose-spending-rules", "transaction-one", "transaction-two", "category-dining")
	if err != nil {
		test.Fatal(err)
	}
	if !strings.Contains(printed, "1 left out: too short or too generic") || !strings.Contains(printed, "2 left out: they hold a number") {
		test.Errorf("propose-spending-rules printed %q", printed)
	}
	if _, err := runFinanceAgainst(test, server, "categorize-transaction", "--create-spending-rule", "transaction-one", "transaction-two", "none"); err == nil {
		test.Error("spending rules with no spending category were asked for")
	}
	if _, err := runFinanceAgainst(test, server, "categorize-transaction", "dining"); err == nil {
		test.Error("a spending category with no transaction was taken")
	}
	mutex.Lock()
	defer mutex.Unlock()
	if single := asked["CategorizeTransaction"]; len(single) != 1 || single[0]["financeTransactionId"] != "transaction-one" || single[0]["spendingCategoryId"] != "category-dining" {
		test.Errorf("one id sent %v", single)
	}
	bulk := asked["CategorizeTransactions"]
	if len(bulk) != 1 || !reflect.DeepEqual(bulk[0]["financeTransactionIds"], []any{"transaction-one", "transaction-two"}) ||
		bulk[0]["spendingCategoryId"] != "category-dining" || bulk[0]["shouldCreateSpendingRules"] != nil {
		test.Errorf("several ids sent %v", bulk)
	}
	// The rules saved are the ones proposed, as proposed.
	confirmed := []any{map[string]any{"matchText": "Invented Bistro", "spendingCategoryId": "category-dining", "aheadOfSpendingRuleId": "rule-bistro"}}
	if len(bulk) == 1 && !reflect.DeepEqual(bulk[0]["spendingRules"], confirmed) {
		test.Errorf("the rules sent to be saved were %v", bulk[0]["spendingRules"])
	}
	proposed := asked["ProposeSpendingRules"]
	if len(proposed) != 2 || !reflect.DeepEqual(proposed[0]["financeTransactionIds"], []any{"transaction-one", "transaction-two"}) || proposed[0]["spendingCategoryId"] != "category-dining" {
		test.Errorf("propose-spending-rules sent %v", proposed)
	}
}

// budget-status and saving-summary send --year as the year, and head a
// year's figures with how far into it they are.
func TestFinanceBudgetStatusSendsTheYear(test *testing.T) {
	test.Parallel()
	var mutex sync.Mutex
	var asked []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var document struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(request.Body).Decode(&document); err != nil {
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		mutex.Lock()
		asked = append(asked, document.Variables)
		mutex.Unlock()
		response.Header().Set("Content-Type", "application/json")
		year := `"year":"2026","asOf":"2026-09-15","dayOfMonth":15,"daysInMonth":30,"monthsElapsedCount":9,"dayOfYear":258,"daysInYear":365`
		switch {
		case strings.Contains(document.Query, "BudgetStatus("):
			_, _ = response.Write([]byte(`{"data":{"BudgetStatus":{` + year + `,"spendingCategories":[{"spendingCategoryId":"category-one","spendingCategoryName":"groceries",` +
				`"budgetAmount":"6000.0000","currencyCode":"USD","budgetToDateAmount":"3900.0000","budgetedMonthCount":12,"spendingAmount":"2550.0000",` +
				`"projectedAmount":"3600.0000","budgetPace":"under","expectedRepeatCharges":[],"unconvertedSpending":[]}],"incomeCategories":[]}}}`))
		case strings.Contains(document.Query, "SavingSummary("):
			_, _ = response.Write([]byte(`{"data":{"SavingSummary":{` + year + `,"reportingCurrencyCode":"USD","incomeBudgetCount":1,"spendingBudgetCount":1,` +
				`"budgetedMonths":["2026-01","2026-02","2026-03","2026-04","2026-05","2026-06","2026-07","2026-08","2026-09","2026-10","2026-11","2026-12"],` +
				`"budgetedMonthCount":12,"budgetedMonthsElapsedCount":9,"savingPace":"on_track","unconvertedCurrencyCodes":[]}}}`))
		default:
			response.WriteHeader(http.StatusBadRequest)
		}
	}))
	test.Cleanup(server.Close)

	for _, arguments := range [][]string{{"budget-status", "--year", "2026"}, {"saving-summary", "--year", "2026"}} {
		printed, err := runFinanceAgainst(test, server, arguments...)
		if err != nil {
			test.Fatalf("%v: %s", arguments, err)
		}
		if !strings.Contains(printed, "2026, 2026-01-01 to 2026-09-15, day 258 of 365") {
			test.Errorf("%v printed %q", arguments, printed)
		}
	}
	mutex.Lock()
	defer mutex.Unlock()
	for _, sent := range asked {
		if _, hasMonth := sent["month"]; sent["year"] != "2026" || hasMonth {
			test.Errorf("sent %v", sent)
		}
	}
}

// A year's saving says which months it counts: only those with budgets,
// or, with none at all, the year's income and spending and nothing
// expected.
func TestFinanceSavingSummaryYearSaysItsMonths(test *testing.T) {
	test.Parallel()
	year := `"year":"2026","asOf":"2026-10-01","dayOfMonth":1,"daysInMonth":31,"monthsElapsedCount":10,"dayOfYear":274,"daysInYear":365,"reportingCurrencyCode":"USD"`
	for _, testCase := range []struct {
		name      string
		answer    string
		wanted    []string
		notWanted []string
	}{
		{
			name: "budgets from September",
			answer: year + `,"incomeBudgetCount":1,"spendingBudgetCount":2,"budgetedMonths":["2026-09","2026-10","2026-11","2026-12"],"budgetedMonthCount":4,` +
				`"budgetedMonthsElapsedCount":2,"expectedSavingAmount":"8000","savingAmount":"3900","savingPace":"on_track","unconvertedCurrencyCodes":[]`,
			wanted: []string{"counting only the 4 months with budgets, 2026-09 to 2026-12 (2 begun)", "on track"},
		},
		{
			name: "no budgets",
			answer: year + `,"incomeBudgetCount":0,"spendingBudgetCount":0,"budgetedMonths":[],"budgetedMonthCount":0,"budgetedMonthsElapsedCount":0,` +
				`"incomeAmount":"41000","spendingAmount":"30500","savingAmount":"10500","savingPace":"on_track","unconvertedCurrencyCodes":[]`,
			wanted:    []string{"no budgets in any month of 2026, so nothing is expected"},
			notWanted: []string{"on track", "counting"},
		},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				response.Header().Set("Content-Type", "application/json")
				_, _ = response.Write([]byte(`{"data":{"SavingSummary":{` + testCase.answer + `}}}`))
			}))
			test.Cleanup(server.Close)
			printed, err := runFinanceAgainst(test, server, "saving-summary", "--year", "2026")
			if err != nil {
				test.Fatalf("saving-summary: %s", err)
			}
			for _, wanted := range testCase.wanted {
				if !strings.Contains(printed, wanted) {
					test.Errorf("printed %q, without %q", printed, wanted)
				}
			}
			for _, notWanted := range testCase.notWanted {
				if strings.Contains(printed, notWanted) {
					test.Errorf("printed %q, with %q", printed, notWanted)
				}
			}
		})
	}
}

// A year's heading says whether it has begun, is over, or how far in it is.
func TestFinancePeriodLine(test *testing.T) {
	test.Parallel()
	for wanted, line := range map[string]string{
		"2026-09, day 15 of 30":                              periodLine("2026-09", "", "2026-09-15", 15, 30, 0, 0, 0),
		"2026, 2026-01-01 to 2026-09-15, day 258 of 365":     periodLine("", "2026", "2026-09-15", 15, 30, 9, 258, 365),
		"2025, the whole year":                               periodLine("", "2025", "2025-12-31", 31, 31, 12, 365, 365),
		"2027, not begun: the budgets in force for it today": periodLine("", "2027", "2027-01-01", 1, 31, 0, 0, 365),
	} {
		if line != wanted {
			test.Errorf("got %q, want %q", line, wanted)
		}
	}
}
