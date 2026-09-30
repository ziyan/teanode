package cmd

import (
	"encoding/json"
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
	"sync":              {},
	"disable-source":    {},
	"enable-source":     {},
	"delete-source":     {},
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
