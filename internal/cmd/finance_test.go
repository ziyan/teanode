package cmd

import (
	"reflect"
	"sort"
	"strings"
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
	"sync":           {},
	"disable-source": {},
	"enable-source":  {},
	"delete-source":  {},
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

// A span reaches back from now; a day is taken as it is.
func TestFinanceDayBack(test *testing.T) {
	test.Parallel()
	now := time.Date(2026, time.March, 31, 12, 0, 0, 0, time.UTC)
	for span, wanted := range map[string]string{
		"30d": "2026-03-01", "2w": "2026-03-17", "1m": "2026-03-03", "1y": "2025-03-31", "2026-01-15": "2026-01-15",
	} {
		have, err := dayBack(span, now)
		if err != nil || have != wanted {
			test.Errorf("%s: %q %v, not %q", span, have, err, wanted)
		}
	}
	for _, refused := range []string{"", "d", "thirty days", "-3d", "3x"} {
		if _, err := dayBack(refused, now); err == nil {
			test.Errorf("%q was taken as a span", refused)
		}
	}
}

// Amounts read as money: two places unless there are more that matter.
func TestFinanceMoney(test *testing.T) {
	test.Parallel()
	for amount, wanted := range map[string]string{
		"-42.1700": "-42.17 USD", "18000": "18000.00 USD", "0.1250": "0.125 USD", "5.5": "5.50 USD", "": "",
	} {
		if have := money(amount, map[bool]string{true: "", false: "USD"}[amount == ""]); have != wanted {
			test.Errorf("%q: %q, not %q", amount, have, wanted)
		}
	}
}
