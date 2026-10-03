// Package finance is the person's money as the agent reads and keeps it for
// them: their finance sources (logins at banks, card issuers, brokerages and
// lenders, linked through a provider), finance accounts, transactions and
// trades, exchange rates, net worth, spending categories and rules, budgets and
// savings targets. Every operation calls the operation of the same name the
// dashboard's Finance page and teanode finance call, so the three agree;
// the tool has no logic of its own beyond saying where a browser is needed.
package finance

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/agent/tools/mailbox"
	"github.com/ziyan/teanode/internal/client"
	financecore "github.com/ziyan/teanode/internal/finance"
)

// financeOperation is one operation of the tool: the finance area's
// operation it calls, what it costs, the arguments it passes on, and
// whether its answer carries text outsiders wrote.
type financeOperation struct {
	// graphqlOperation is the finance area's operation, empty for the
	// operations the tool answers itself or that act on a source.
	graphqlOperation string
	risk             tools.Risk
	arguments        []string
	required         []string

	// isMonthShorthand says the operation takes month as shorthand for
	// the first and last day of that month (or, for cash_flow, for both
	// ends of its range of months).
	isMonthShorthand bool

	// isUntrusted marks an answer with merchant names, descriptions,
	// account or institution names, notes or evidence in it: written by
	// whoever charged the account, the provider, or a web page.
	isUntrusted bool

	// preview is the confirmation card's line for a write. The lookup
	// names what the call acts on the way the person knows it.
	preview func(lookup *previewLookup, call map[string]any) string
}

// The arguments shared by several operations.
var (
	rangeArguments      = []string{"from", "to"}
	assetArguments      = []string{"asset_name", "asset_kind", "currency_code", "valuation_source"}
	spendingRuleFields  = []string{"match_text", "spending_category_id", "finance_account_id", "minimum_amount", "maximum_amount", "rule_priority"}
	savingsTargetFields = []string{"savings_target_name", "target_amount", "currency_code", "target_on", "target_measure", "starting_amount", "started_on", "asset_ids", "finance_account_ids"}
)

// PersonOnlyAssetArguments are the asset settings the tool does not take,
// though the dashboard and teanode finance do: allowing web estimates
// sends what the estimate searches for (often a home address) to a search
// provider and to the pages read, which is the person's choice alone.
var PersonOnlyAssetArguments = []string{"estimate_description", "is_estimate_allowed"}

// operations is every operation of the tool by its name: the finance
// area's operation in snake case, a leading Finance dropped, and the few
// that stand for more than one call.
var operations = map[string]*financeOperation{
	"providers":          {graphqlOperation: "FinanceProviders", risk: tools.RiskRead},
	"sources":            {graphqlOperation: "FinanceSources", risk: tools.RiskRead, isUntrusted: true},
	"accounts":           {graphqlOperation: "FinanceAccounts", risk: tools.RiskRead, arguments: []string{"currency_code"}, isUntrusted: true},
	"credit_usage":       {graphqlOperation: "CreditUsage", risk: tools.RiskRead, arguments: []string{"currency_code"}, isUntrusted: true},
	"reporting_currency": {graphqlOperation: "ReportingCurrency", risk: tools.RiskRead},
	"statement_import":   {graphqlOperation: "StatementImport", risk: tools.RiskRead, isUntrusted: true},
	"regenerate_statement_import_address": {
		graphqlOperation: "RegenerateStatementImportAddress", risk: tools.RiskWrite,
		preview: func(*previewLookup, map[string]any) string {
			return "Give the statement import address a new token; mail to the old address will be refused"
		},
	},
	"transactions": {
		graphqlOperation: "FinanceTransactions", risk: tools.RiskRead, isUntrusted: true, isMonthShorthand: true,
		arguments: append([]string{"finance_account_id", "text", "minimum_amount", "maximum_amount", "provider_category", "spending_category_id", "is_uncategorized", "duplicate_of_transaction_id", "is_duplicate_included", "finance_transaction_ids", "limit", "offset", "after"}, rangeArguments...),
	},
	"trades": {
		graphqlOperation: "FinanceTrades", risk: tools.RiskRead, isUntrusted: true, isMonthShorthand: true,
		arguments: append([]string{"finance_account_id", "finance_security_id", "limit", "offset", "after"}, rangeArguments...),
	},
	"spending_summary": {
		graphqlOperation: "FinanceSpendingSummary", risk: tools.RiskRead, isUntrusted: true, isMonthShorthand: true,
		arguments: append([]string{"group_by", "finance_account_id", "currency_code"}, rangeArguments...),
	},
	"exchange_rate": {
		graphqlOperation: "ExchangeRate", risk: tools.RiskRead,
		arguments: []string{"from_currency_code", "to_currency_code", "rate_on"}, required: []string{"from_currency_code", "to_currency_code"},
	},
	"convert_currency": {
		graphqlOperation: "ConvertCurrency", risk: tools.RiskRead,
		arguments: []string{"amount", "from_currency_code", "to_currency_code", "rate_on"}, required: []string{"amount", "from_currency_code", "to_currency_code"},
	},
	"set_reporting_currency": {
		graphqlOperation: "SetReportingCurrency", risk: tools.RiskWrite, arguments: []string{"currency_code"}, required: []string{"currency_code"},
		preview: func(_ *previewLookup, call map[string]any) string {
			return "Show totals in " + text(call, "currency_code")
		},
	},
	"net_worth": {graphqlOperation: "NetWorth", risk: tools.RiskRead, isMonthShorthand: true, arguments: append([]string{"currency_code"}, rangeArguments...)},
	"assets": {
		graphqlOperation: "Assets", risk: tools.RiskRead, isUntrusted: true,
		arguments: []string{"asset_kind", "text", "finance_account_id", "is_holding"},
	},
	"asset_history": {graphqlOperation: "AssetHistory", risk: tools.RiskRead, arguments: []string{"asset_id"}, required: []string{"asset_id"}, isUntrusted: true},
	"create_asset": {
		graphqlOperation: "CreateAsset", risk: tools.RiskWrite, isUntrusted: true,
		arguments: append([]string{"value", "valued_on"}, assetArguments...), required: []string{"asset_name", "asset_kind", "currency_code"},
		preview: func(_ *previewLookup, call map[string]any) string {
			line := fmt.Sprintf("Add %s (%s, %s) to net worth", tools.Named(text(call, "asset_name"), "an asset"), text(call, "asset_kind"), text(call, "currency_code"))
			if value := text(call, "value"); value != "" {
				line += ", worth " + value + onSuffix(call, "valued_on")
			}
			return line
		},
	},
	"update_asset": {
		graphqlOperation: "UpdateAsset", risk: tools.RiskWrite, isUntrusted: true,
		arguments: append([]string{"asset_id"}, assetArguments...), required: []string{"asset_id"},
		preview: func(lookup *previewLookup, call map[string]any) string {
			return "Change the asset " + lookup.assetName(text(call, "asset_id")) + renamedSuffix(call, "asset_name")
		},
	},
	"close_asset": {
		graphqlOperation: "CloseAsset", risk: tools.RiskWrite, isUntrusted: true,
		arguments: []string{"asset_id", "closed_on", "should_reopen"}, required: []string{"asset_id"},
		preview: func(lookup *previewLookup, call map[string]any) string {
			name := lookup.assetName(text(call, "asset_id"))
			if isTrue(call, "should_reopen") {
				return "Open the asset " + name + " again"
			}
			return "Record the asset " + name + " as sold or paid off" + onSuffix(call, "closed_on")
		},
	},
	"delete_asset": {
		graphqlOperation: "DeleteAsset", risk: tools.RiskDestructive, arguments: []string{"asset_id"}, required: []string{"asset_id"},
		preview: func(lookup *previewLookup, call map[string]any) string {
			return "Delete the asset " + lookup.assetName(text(call, "asset_id")) + " and its whole history"
		},
	},
	"record_valuation": {
		graphqlOperation: "RecordValuation", risk: tools.RiskWrite, isUntrusted: true,
		arguments: []string{"asset_id", "value", "valued_on", "valuation_source", "estimate_low", "estimate_high", "valuation_note", "evidence_urls"},
		required:  []string{"asset_id", "value"},
		preview: func(lookup *previewLookup, call map[string]any) string {
			what := "a value"
			if text(call, "valuation_source") == "agent_estimate" {
				what = "an estimate"
			}
			return fmt.Sprintf("Record %s of %s for the asset %s%s", what, text(call, "value"), lookup.assetName(text(call, "asset_id")), onSuffix(call, "valued_on"))
		},
	},
	"delete_valuation": {
		graphqlOperation: "DeleteValuation", risk: tools.RiskDestructive, arguments: []string{"valuation_id"}, required: []string{"valuation_id"},
		preview: func(*previewLookup, map[string]any) string { return "Delete a value from an asset's history" },
	},
	"spending_categories": {graphqlOperation: "SpendingCategories", risk: tools.RiskRead},
	"create_spending_category": {
		graphqlOperation: "CreateSpendingCategory", risk: tools.RiskWrite,
		arguments: []string{"spending_category_name", "parent_spending_category_id", "is_income", "is_hidden"}, required: []string{"spending_category_name"},
		preview: func(lookup *previewLookup, call map[string]any) string {
			line := "Add the spending category " + tools.Named(text(call, "spending_category_name"), "a spending category")
			if parentId := text(call, "parent_spending_category_id"); parentId != "" {
				line += " under " + lookup.spendingCategoryName(parentId)
			}
			return line
		},
	},
	"update_spending_category": {
		graphqlOperation: "UpdateSpendingCategory", risk: tools.RiskWrite,
		arguments: []string{"spending_category_id", "spending_category_name", "parent_spending_category_id", "is_income", "is_hidden"}, required: []string{"spending_category_id"},
		preview: func(lookup *previewLookup, call map[string]any) string {
			return "Change the spending category " + lookup.spendingCategoryName(text(call, "spending_category_id")) + renamedSuffix(call, "spending_category_name")
		},
	},
	"delete_spending_category": {
		graphqlOperation: "DeleteSpendingCategory", risk: tools.RiskDestructive, arguments: []string{"spending_category_id"}, required: []string{"spending_category_id"},
		preview: func(lookup *previewLookup, call map[string]any) string {
			return "Delete the spending category " + lookup.spendingCategoryName(text(call, "spending_category_id")) +
				", its budgets and its spending rules; its transactions become uncategorized"
		},
	},
	"spending_rules": {graphqlOperation: "SpendingRules", risk: tools.RiskRead, isUntrusted: true},
	"create_spending_rule": {
		graphqlOperation: "CreateSpendingRule", risk: tools.RiskWrite, arguments: spendingRuleFields, required: []string{"match_text", "spending_category_id"}, isUntrusted: true,
		preview: func(lookup *previewLookup, call map[string]any) string {
			return fmt.Sprintf("Add a spending rule for %q that %s, applied to past transactions too", text(call, "match_text"), lookup.ruleEffect(call))
		},
	},
	"update_spending_rule": {
		graphqlOperation: "UpdateSpendingRule", risk: tools.RiskWrite, arguments: append([]string{"spending_rule_id"}, spendingRuleFields...), required: []string{"spending_rule_id"}, isUntrusted: true,
		preview: func(lookup *previewLookup, call map[string]any) string {
			line := "Change the spending rule for " + lookup.spendingRuleMatch(text(call, "spending_rule_id"))
			if _, isGiven := call["spending_category_id"]; isGiven {
				line += " so it " + lookup.ruleEffect(call)
			}
			return line + ", applied again to past transactions"
		},
	},
	"delete_spending_rule": {
		graphqlOperation: "DeleteSpendingRule", risk: tools.RiskDestructive, arguments: []string{"spending_rule_id"}, required: []string{"spending_rule_id"},
		preview: func(lookup *previewLookup, call map[string]any) string {
			return "Delete the spending rule for " + lookup.spendingRuleMatch(text(call, "spending_rule_id"))
		},
	},
	"propose_spending_rules": {
		graphqlOperation: "ProposeSpendingRules", risk: tools.RiskRead, isUntrusted: true,
		arguments: []string{"finance_transaction_ids", "spending_category_id"}, required: []string{"finance_transaction_ids", "spending_category_id"},
	},
	// categorize_transaction stands for CategorizeTransaction, for one
	// finance transaction, and CategorizeTransactions, for several (run
	// tells them apart), so one word does both as on the command line.
	"categorize_transaction": {
		risk: tools.RiskWrite, isUntrusted: true,
		arguments: []string{"finance_transaction_id", "finance_transaction_ids", "spending_category_id", "should_create_spending_rule"},
		preview: func(lookup *previewLookup, call map[string]any) string {
			financeTransactionIds := transactionIds(call)
			if len(financeTransactionIds) > 1 {
				return categorizeSeveralPreview(lookup, call, financeTransactionIds)
			}
			transaction := lookup.transaction(text(call, "finance_transaction_id"))
			if len(financeTransactionIds) == 1 {
				transaction = lookup.transaction(financeTransactionIds[0])
			}
			line := "Categorize " + transaction + " as " + lookup.spendingCategoryName(text(call, "spending_category_id"))
			if lookup.isTransferSpendingCategory(text(call, "spending_category_id")) {
				line = "Mark " + transaction + " as a transfer between their own accounts, neither spending nor income"
			}
			if isTrue(call, "should_create_spending_rule") {
				line += ", and add a spending rule for its merchant, applied to past transactions too"
			}
			return line
		},
	},
	"count_transaction": {
		graphqlOperation: "CountTransaction", risk: tools.RiskWrite, isUntrusted: true,
		arguments: []string{"finance_transaction_id"}, required: []string{"finance_transaction_id"},
		preview: func(lookup *previewLookup, call map[string]any) string {
			return "Count " + lookup.transaction(text(call, "finance_transaction_id")) + " as a real charge of its own, not a mirrored copy of another account's"
		},
	},
	"undo_count_transaction": {
		graphqlOperation: "UndoCountTransaction", risk: tools.RiskWrite, isUntrusted: true,
		arguments: []string{"finance_transaction_id"}, required: []string{"finance_transaction_id"},
		preview: func(lookup *previewLookup, call map[string]any) string {
			return "Let mirror detection decide again whether " + lookup.transaction(text(call, "finance_transaction_id")) + " is a mirrored copy"
		},
	},
	"budgets": {graphqlOperation: "Budgets", risk: tools.RiskRead},
	"set_budget": {
		graphqlOperation: "SetBudget", risk: tools.RiskWrite,
		arguments: []string{"spending_category_id", "monthly_amount", "currency_code", "effective_from"}, required: []string{"spending_category_id", "monthly_amount"},
		preview: func(lookup *previewLookup, call map[string]any) string {
			name := lookup.spendingCategoryName(text(call, "spending_category_id"))
			from := ""
			if month := text(call, "effective_from"); month != "" {
				from = " from " + month
			}
			isIncome := lookup.isIncomeSpendingCategory(text(call, "spending_category_id"))
			if text(call, "monthly_amount") == "0" {
				if isIncome {
					return "Stop expecting income in " + name + from
				}
				return "End the budget for " + name + from
			}
			if isIncome {
				return strings.TrimSpace("Expect a monthly income of "+text(call, "monthly_amount")+" "+text(call, "currency_code")) + " in " + name + from
			}
			return strings.TrimSpace("Set a monthly budget of "+text(call, "monthly_amount")+" "+text(call, "currency_code")) + " for " + name + from
		},
	},
	"budget_status":   {graphqlOperation: "BudgetStatus", risk: tools.RiskRead, arguments: []string{"month", "year"}},
	"saving_summary":  {graphqlOperation: "SavingSummary", risk: tools.RiskRead, arguments: []string{"month", "year", "currency_code"}},
	"spending_by_day": {graphqlOperation: "SpendingByDay", risk: tools.RiskRead, arguments: []string{"month", "compare_month", "currency_code"}},
	"cash_flow":       {graphqlOperation: "CashFlow", risk: tools.RiskRead, isMonthShorthand: true, arguments: []string{"from_month", "to_month", "currency_code"}},
	"savings_targets": {graphqlOperation: "SavingsTargets", risk: tools.RiskRead},
	"create_savings_target": {
		graphqlOperation: "CreateSavingsTarget", risk: tools.RiskWrite, arguments: savingsTargetFields, required: []string{"savings_target_name", "target_amount", "target_on"},
		preview: func(lookup *previewLookup, call map[string]any) string {
			return fmt.Sprintf("Add the savings target %s: %s by %s", tools.Named(text(call, "savings_target_name"), "a savings target"), text(call, "target_amount"), text(call, "target_on")) +
				lookup.targetMeasureSuffix(call)
		},
	},
	"update_savings_target": {
		graphqlOperation: "UpdateSavingsTarget", risk: tools.RiskWrite, arguments: append([]string{"savings_target_id"}, savingsTargetFields...), required: []string{"savings_target_id"},
		preview: func(lookup *previewLookup, call map[string]any) string {
			return "Change the savings target " + lookup.savingsTargetName(text(call, "savings_target_id")) + renamedSuffix(call, "savings_target_name") +
				lookup.targetMeasureSuffix(call)
		},
	},
	"close_savings_target": {
		graphqlOperation: "CloseSavingsTarget", risk: tools.RiskWrite, arguments: []string{"savings_target_id", "closed_on", "should_reopen"}, required: []string{"savings_target_id"},
		preview: func(lookup *previewLookup, call map[string]any) string {
			name := lookup.savingsTargetName(text(call, "savings_target_id"))
			if isTrue(call, "should_reopen") {
				return "Open the savings target " + name + " again"
			}
			return "Close the savings target " + name + onSuffix(call, "closed_on")
		},
	},

	// The ones that stand for more than one call, act on a source, or
	// answer with where to go.
	"link_plaid":        {risk: tools.RiskRead},
	"repair":            {risk: tools.RiskRead, arguments: []string{"source_id"}, required: []string{"source_id"}},
	"link_simplefin":    {risk: tools.RiskRead},
	"import_credential": {risk: tools.RiskRead},
	// ImportStatement with a message's OFX attachments only: a file the
	// person hands over in conversation carries no id the model is shown,
	// so the tool takes the message, which mail_search and mail_read name.
	"import_statement": {
		risk: tools.RiskWrite, arguments: []string{"mailbox_item_id"}, required: []string{"mailbox_item_id"},
		preview: func(*previewLookup, map[string]any) string {
			return "Import the OFX statements attached to a message into their finance accounts"
		},
	},
	// ImportTransactions with what the agent read off pictures, checked
	// here first (transaction_rows.go) so the card says what was checked,
	// and previewed by the server so it says which account and which rows
	// are new. The number is not required: an account named by
	// finance_account_id needs none.
	"import_transactions": {
		graphqlOperation: "ImportTransactions", risk: tools.RiskWrite, isUntrusted: true, arguments: transactionRowsArguments,
		required: []string{"institution_name", "statement_account_kind", "currency_code", "transaction_rows"},
		preview:  importTransactionsPreview,
	},
	"preview_import_transactions": {
		graphqlOperation: "PreviewImportTransactions", risk: tools.RiskRead, isUntrusted: true, arguments: transactionRowsArguments,
		required: []string{"institution_name", "statement_account_kind", "currency_code", "transaction_rows"},
	},
	"rename_statement_account": {
		graphqlOperation: "RenameStatementAccount", risk: tools.RiskWrite, isUntrusted: true,
		arguments: []string{"finance_account_id", "account_name"}, required: []string{"finance_account_id", "account_name"},
		preview: func(lookup *previewLookup, call map[string]any) string {
			return "Rename the imported account " + lookup.financeAccountName(text(call, "finance_account_id")) + " to " + tools.Named(text(call, "account_name"), "a new name") +
				"; later imports keep the name"
		},
	},
	// DeleteStatementAccount is the person's alone: it removes an account's
	// transactions and its net worth history for good, and a model that
	// misread which account a screenshot was of would delete the right one.
	// The tool only says where the person does it.
	"delete_statement_account": {risk: tools.RiskRead, arguments: []string{"finance_account_id"}},
	"sync": {
		risk: tools.RiskWrite, arguments: []string{"source_id"}, required: []string{"source_id"},
		preview: func(lookup *previewLookup, call map[string]any) string {
			return "Sync the finance source " + lookup.sourceName(text(call, "source_id")) + " now"
		},
	},
	"disable_source": {
		risk: tools.RiskWrite, arguments: []string{"source_id"}, required: []string{"source_id"},
		preview: func(lookup *previewLookup, call map[string]any) string {
			return "Stop the finance source " + lookup.sourceName(text(call, "source_id")) + " syncing, keeping what it holds"
		},
	},
	"enable_source": {
		risk: tools.RiskWrite, arguments: []string{"source_id"}, required: []string{"source_id"},
		preview: func(lookup *previewLookup, call map[string]any) string {
			return "Let the finance source " + lookup.sourceName(text(call, "source_id")) + " sync again"
		},
	},
	"delete_source": {
		risk: tools.RiskDestructive, arguments: []string{"source_id"}, required: []string{"source_id"},
		preview: func(lookup *previewLookup, call map[string]any) string {
			return "Delete the finance source " + lookup.sourceName(text(call, "source_id")) + " and its transactions, ending it at its provider"
		},
	},
}

// argumentInsteadOf is the argument a model means by one the tool does not
// take, said in the refusal so the retry is right.
var argumentInsteadOf = map[string]string{
	"to_currency_code":   "currency_code",
	"reporting_currency": "currency_code",
	"from_date":          "from",
	"to_date":            "to",
	"start_date":         "from",
	"end_date":           "to",
	"account_id":         "finance_account_id",
	"transaction_id":     "finance_transaction_id",
	"category_id":        "spending_category_id",
	"spending_category":  "spending_category_id",
	"valuation_date":     "valued_on",
	"is_transfer":        "spending_category_id",
}

// valueInsteadOf is the value the argument argumentInsteadOf names takes in
// place of one the tool does not take. A transfer was once a flag beside
// the spending category (is_transfer on a spending rule or a transaction),
// and dropping it as unread let a model say a rule now marked transfers
// when nothing had changed.
var valueInsteadOf = map[string]string{
	"is_transfer": "transfer",
}

// operationNames is every operation's name, in order, for the schema.
func operationNames() []string {
	names := make([]string, 0, len(operations))
	for name := range operations {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// acceptedArguments is every argument an operation reads, in order.
func acceptedArguments(operation *financeOperation) []string {
	seen := map[string]bool{}
	accepted := []string{}
	for _, list := range [][]string{operation.required, operation.arguments} {
		for _, key := range list {
			if !seen[key] {
				seen[key] = true
				accepted = append(accepted, key)
			}
		}
	}
	if operation.isMonthShorthand {
		accepted = append(accepted, "month")
	}
	sort.Strings(accepted)
	return accepted
}

// dropUnreadArguments takes out the arguments the operation does not read,
// as if they had not been sent, except the two kinds checkArguments exists
// to refuse. Some models fill in every argument of the tool on every call:
// the ones they have nothing for as "", [], null, false or 0, an argument
// with a fixed list of values as the list's first value, and sometimes a
// guess (asset_kind vehicle for assets); refusing those refused every call
// such a model made, and it retried until it gave up. What is still refused:
// an argument named the way people misname one the operation does read
// (to_currency_code for currency_code), which once turned a month's
// spending into all of time, and a setting only the person may change, each
// when sent with something in it. An argument the operation reads keeps
// what was sent, since empty may mean something there (an empty spending
// category takes one away), except null, which says nothing anywhere.
func dropUnreadArguments(operation *financeOperation, asked map[string]any) {
	isAccepted := map[string]bool{"operation": true}
	for _, key := range acceptedArguments(operation) {
		isAccepted[key] = true
	}
	isPersonOnly := map[string]bool{}
	for _, key := range PersonOnlyAssetArguments {
		isPersonOnly[key] = true
	}
	for key, value := range asked {
		if value == nil {
			delete(asked, key)
			continue
		}
		if isAccepted[key] {
			continue
		}
		if meant, isKnown := argumentInsteadOf[key]; (isKnown && isAccepted[meant]) || isPersonOnly[key] {
			if !isEmptyArgument(value) {
				continue
			}
		}
		delete(asked, key)
	}
}

// isEmptyArgument says an argument was sent with nothing in it.
func isEmptyArgument(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(typed) == ""
	case []any:
		return len(typed) == 0
	case []string:
		return len(typed) == 0
	case bool:
		return !typed
	case float64:
		return typed == 0
	case int:
		return typed == 0
	}
	return false
}

// checkArguments refuses a call with an argument its operation does not
// read, naming it and the ones the operation takes. An argument quietly
// ignored gives an answer to a question that was not asked: a month's
// spending came back as all of time.
func checkArguments(name string, operation *financeOperation, asked map[string]any) error {
	accepted := acceptedArguments(operation)
	isAccepted := map[string]bool{"operation": true}
	for _, key := range accepted {
		isAccepted[key] = true
	}
	var unknown, instead []string
	for key := range asked {
		if isAccepted[key] {
			continue
		}
		unknown = append(unknown, key)
		if meant, isKnown := argumentInsteadOf[key]; isKnown && isAccepted[meant] {
			if value, hasValue := valueInsteadOf[key]; hasValue {
				meant += " " + value
			}
			instead = append(instead, fmt.Sprintf("%s instead of %s", meant, key))
		}
		for _, personOnly := range PersonOnlyAssetArguments {
			if key == personOnly {
				instead = append(instead, key+" is the person's to set, on the dashboard's Finance page or with teanode finance update-asset")
			}
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	sort.Strings(instead)
	takes := "no other arguments"
	if len(accepted) > 0 {
		takes = strings.Join(accepted, ", ")
	}
	refusal := fmt.Sprintf("%s does not take %s; it takes %s", name, strings.Join(unknown, ", "), takes)
	if len(instead) > 0 {
		refusal += " (" + strings.Join(instead, "; ") + ")"
	}
	return fmt.Errorf("%s", refusal)
}

// monthRange is the first and last day of a month written 2006-01.
func monthRange(month string) (string, string, error) {
	first, err := time.Parse("2006-01", month)
	if err != nil {
		return "", "", fmt.Errorf("month %q is not a month written 2026-09", month)
	}
	return first.Format(time.DateOnly), first.AddDate(0, 1, -1).Format(time.DateOnly), nil
}

// spreadMonth turns month into the range arguments the operation reads,
// refusing it beside the range it stands for.
func spreadMonth(name string, asked map[string]any) error {
	month := text(asked, "month")
	if month == "" {
		delete(asked, "month")
		return nil
	}
	fromKey, toKey := "from", "to"
	if name == "cash_flow" {
		fromKey, toKey = "from_month", "to_month"
	}
	if text(asked, fromKey) != "" || text(asked, toKey) != "" {
		return fmt.Errorf("give month or %s and %s, not both", fromKey, toKey)
	}
	from, to, err := monthRange(month)
	if err != nil {
		return err
	}
	if name == "cash_flow" {
		from, to = month, month
	}
	delete(asked, "month")
	asked[fromKey], asked[toKey] = from, to
	return nil
}

const description = "The person's money: their finance sources (logins at banks, card issuers, brokerages and lenders, linked through a provider), " +
	"finance accounts and transactions, trades in investment accounts, exchange rates, net worth (assets and their valuations), spending categories and rules, budgets and savings targets. " +
	"Pick one `operation` and give only the arguments it takes: an argument the operation does not read is refused with the list it does. Ids come from the listing operations. " +
	"Dates are 2026-09-01, months 2026-09; amounts are decimals, money out negative. " +
	"A holding in an investment account is an asset with a financeSecurity, and its valuations carry heldQuantity, unitPrice and costBasis; the account's own asset holds its cash. " +
	"`trades` lists buys, sells and securities moved in or out, which are never spending or income; dividends, interest, fees, deposits and withdrawals are finance transactions. " +
	"`assets` leaves the holdings out unless is_holding is true or finance_account_id is given, since each position is an asset and there can be hundreds; narrow it with asset_kind or text (words in the name). " +
	"`transactions`, `trades`, `spending_summary`, `net_worth` and `cash_flow` take `month` as shorthand for that whole month; without a range they cover all of time (net_worth the last thirty days, cash_flow twelve months). " +
	"Totals come per currency and converted into the reporting currency (`reporting_currency` says which), or into `currency_code` where given, each amount at its own day's exchange rate, naming any currency left out for want of a rate; never add different currencies yourself.\n" +
	"Linking: `providers` says what the server offers. `link_plaid` gives an address for the person to open in their browser, signed in to the dashboard; `repair` gives the address that signs a finance source in again when `sources` says isSignInRequired. " +
	"A SimpleFIN setup token, or the credential of an existing provider connection (a Plaid access token, a SimpleFIN access URL), is never taken in conversation: `link_simplefin` and `import_credential` say where to give it.`sync`, `disable_source`, `enable_source` and `delete_source` act on a finance source by source_id; a switched-off source is switched on with enable_source before it syncs.\n" +
	"Statements: for an account no provider reaches (a card that only exports OFX, .ofx, .qfx or .qbo), `statement_import` gives the person's statement import address, where mailing the exported file imports it (from a phone's wallet: Export Transactions, then share to Mail), and what the last import did. `import_statement` imports the OFX attachments of a message in their mailbox by mailbox_item_id; a transaction already imported is updated, never added twice. `regenerate_statement_import_address` ends the old address and makes a new one, only when the person asks.\n" +
	"Pictures of transactions: when the person shares screenshots of a bank's or card issuer's app (or a photo of a statement), `import_transactions` imports what you read, one account per call. " +
	"You see pictures only in the turn they are sent, at most eight of them, so read and import in that turn, and ask for more than eight in another message. " +
	"Read every row of every picture yourself and send each transaction once, even where two screenshots overlap. " +
	"Choose the account first, every time: call `accounts`, and when one of the person's imported accounts (providerKind statement) is the account the pictures show, pass its id as finance_account_id. " +
	"A card's export often knows the card by an identifier that is not the card number, so its mask need not match the digits a picture shows; same institution, kind and currency is the sign. " +
	"When you are unsure which account it is, or whether it is one, ask the person before importing. " +
	"An account a provider syncs is refused: its transactions come from its sync. " +
	"Without finance_account_id, use the account number shown, masked digits as they appear (****1234) with is_account_number_partial when only some show; never invent one or put a word in its place. " +
	"The server then uses the account that number or its last digits match; it refuses, naming them, when several could match, or when the institution (or an account whose institution is not known) has an account the digits do not match: ask the person, then pass the id, or is_new_account true when the person says it is an account not imported before. is_new_account never passes over an account whose last digits match: the server names it, and only its id imports into it. " +
	"`preview_import_transactions` takes the same arguments and says, writing nothing, which account the rows would go into and which of them the account holds already. " +
	"Give running_balance_amount on every row that shows one, and monthly_totals exactly as a card's list shows them: the server checks that the balances chain row by row and that each month's rows come to its total, and refuses naming the first row or month that does not add up; read that picture again or ask the person. " +
	"Never guess a row that is cut off: import the months that are complete, leave out a month cut off at the bottom of a picture, and ask for the rest. " +
	"The ledger balance is the newest picture's, with its day and time zone. Send descriptions exactly as shown (the server normalizes half-width katakana, do not), days as 2026-09-30 (26.09.30 is 2026-09-30), amounts signed and without separators. " +
	"Only the rows the account does not hold already are added: the server matches the rows against every stored transaction of the account (from an OFX file or earlier pictures) by day and exact amount, not by description, so overlapping screenshots, or pictures of what a file imported, add only what is new. " +
	"Give each row's posted day as the export would date it; where a list shows both the day of use and the day it posted, use the posted one, since a row dated a day off is not taken as the same transaction (the confirmation card points such rows out). " +
	"The cost of matching by day and amount: a genuine repeat charge of the same amount on the same day as one already stored, sent in a later import, is taken as the stored one. " +
	"`rename_statement_account` renames an imported account; deleting one is the person's to do (`delete_statement_account` says where).\n" +
	"Recipes, followed the same way every time:\n" +
	"- Proposing budgets: `spending_summary` grouped by spendingCategory for each of the last three full months (month 2026-06, then 2026-07, then 2026-08); propose the median of each, rounded, as a list; `set_budget` only what the person accepts. Once they set their first budget, offer a monthly review schedule on the first of the month.\n" +
	"- Expected income: `set_budget` on an income spending category (isIncome) is the income expected each month, not a limit; `budget_status` lists those apart as incomeCategories, with incomePace behind, on_track or ahead.\n" +
	"- Explaining where a month is heading: a spending budget's projectedAmount is spendingAmount, plus fixedChargesDueAmount (repeat charges: merchants that charged the spending category in each of the last three full months and not yet this month, at their median, listed in expectedRepeatCharges), plus the rest of the spending at the rate it has come this month for the days left; name those three amounts and the merchants. An income budget's projectedAmount is the income expected, or what came in when that is more already, since income is not projected on a straight line. The saving summary's projected saving is projected income less projected spending.\n" +
	"- How a month's saving is going: `saving_summary` gives the expected saving (income budgets less spending budgets), the actual saving so far, the projected month-end saving and the difference, with savingPace; quote those numbers rather than working them out. With no income budget, offer to set one from the last three months of `cash_flow` income. " +
	"For a year, it counts only the months with a budget in force (budgetedMonths, budgetedMonthCount, budgetedMonthsElapsedCount): expected, so far and projected are all over those months, so say which months, as in \"over the 4 months with budgets, September to December\". With budgetedMonthCount 0 there were no budgets that year: give the income, spending and what was left, and say nothing was budgeted rather than quoting an expected saving or a pace.\n" +
	"- Credit card usage: `credit_usage` gives what the cards owe against their credit limits, overall (usageShare, 0.25 is 25%) and per card, highest first; quote it rather than working it out from `accounts`. " +
	"Each card's creditLimitSource says whether the limit is the provider's or derived (owed plus available credit); a card with an unknown limit is listed with what it owes and counted in leftOutCardCount, not in the overall share. " +
	"Common guidance: under 30% of the limit is good, 30% to 50% is worth watching, above 50% weighs on a credit score; name the cards that push the total up.\n" +
	"- A savings plan: `cash_flow` for what they save a month now, `savings_targets` for what a target needs a month, `budget_status` and `spending_summary` for which spending categories could close the gap, with numbers.\n" +
	"- A savings target on what they own: target_measure net_worth for everything, or asset_value with finance_account_ids for whole accounts (an investment account counts with every holding, those bought later too) and asset_ids only for assets outside a finance account.\n" +
	"- After the person corrects a transaction's spending category with `categorize_transaction`, offer a spending rule for that merchant (`should_create_spending_rule`), which applies to past transactions too, never over their own choices.\n" +
	"- Categorizing several at once: `categorize_transaction` with finance_transaction_ids (up to 500) gives them all the spending category as the person's choice, all or none; finance_transaction_id still takes one. " +
	"`propose_spending_rules` with the same ids (up to 5000) and spending category lists the spending rules should_create_spending_rule would add, at most 50: one per merchant (or description) unless the rule that applies first already files it there, each with aheadOfSpendingRule (the existing rule it goes ahead of so it takes effect) and changedTransactionCount (other transactions it would recategorize); " +
	"tooGenericMatchTextCount, changingNumberMatchTextCount and overLimitMatchTextCount count what was left out (under four letters or mostly digits, a long number or a date that changes each time, past 50). Name them, with those numbers, when offering; categorize_transaction saves exactly that list.\n" +
	"- Transfers: money moved between the person's own accounts (a card payment, savings) is the built-in spending category transfer (isTransfer in `spending_categories`), neither spending nor income; there is no separate transfer mark. " +
	"Mark one with `categorize_transaction` and spending_category_id transfer; any other spending category takes the mark away. " +
	"A spending rule can assign transfer like any spending category (`create_spending_rule` with match_text ONLINE PAYMENT and spending_category_id transfer), for past and future transactions. " +
	"Pairing a card payment with its checking withdrawal and the provider's own transfer categories assign it too (categorizedBy transfer_detection or provider_category_mapping), and a spending rule does not take those over; the person's choice beats both. " +
	"The transfer category cannot be deleted, made income or budgeted.\n" +
	"- Fits nothing: the built-in spending category other (isOther in `spending_categories`) is for a transaction that fits none of the others, spending like any other; give it with `categorize_transaction` and spending_category_id other. " +
	"No spending category is not a choice: it means not decided yet (is_uncategorized lists those), and categorize_transaction refuses to take a spending category away. " +
	"The other category cannot be deleted, made income, put under a parent or given children; it can be renamed, hidden or budgeted.\n" +
	"- Mirrored copies: some institutions report one charge, such as an account-level fee, once on every account of a connection. " +
	"Only investment accounts within one Plaid connection are grouped: the same day, amount, currency and description on two or more of them, pending or posted, is one charge (a posted copy counts before a pending one): one copy counts, and each other has duplicateOfTransactionId (the counted copy) and is left out of every total, like a transfer. " +
	"A genuinely identical fee on two such accounts (two retirement accounts charged the same fee the same day, say) is marked too, and `count_transaction` is the recourse; copies posted on different days are not matched. " +
	"`transactions` leaves them out unless is_duplicate_included is true, and says how many it left out in leftOutDuplicateCount; say so when it is not zero. When listing them, say they are copies rather than adding them up. `transactions` with duplicate_of_transaction_id lists a counted copy's duplicates. " +
	"When the person says a duplicate is a real charge of its own, `count_transaction` counts it (duplicateDecidedBy person) and detection leaves it alone; `undo_count_transaction` takes that back.\n" +
	"- Tracking an account reachable only through a connected server: `create_asset` with valuation_source agent_reading if there is none (a value read now can go in the same call), then a daily schedule whose prompt calls that server's tool for the account's total and records it with `record_valuation` (valuation_source agent_reading). Never over an asset valued by finance_sync.\n" +
	"- Estimating a house or a car: only for an asset with isEstimateAllowed, which only the person sets (on the dashboard's Finance page or with teanode finance update-asset). Search the web for its estimateDescription, read two to four pages that give a value or comparable sales, and `record_valuation` with valuation_source agent_estimate, estimate_low, estimate_high, the middle as value, the pages as evidence_urls and a valuation_note saying what it rests on. Where estimates are not allowed, say so and say where the person can allow them.\n" +
	"- Converting currencies: `convert_currency` or `exchange_rate`, with from_currency_code, to_currency_code and rate_on for another day; the answer names the published day the rate is from."

func init() {
	tools.Register(func() []*tools.Tool {
		return []*tools.Tool{
			{
				Name: "finance", Family: tools.FamilyFinance, Risk: tools.RiskWrite,
				Description: description,
				Parameters: tools.Object(map[string]any{
					"operation":                   tools.EnumProperty("what to do", operationNames()...),
					"source_id":                   tools.StringProperty("a finance source, by the id sources gives"),
					"mailbox_item_id":             tools.StringProperty("for import_statement: the message whose OFX attachments (.ofx, .qfx, .qbo) to import, by the item_id mail_search or mail_read gives"),
					"finance_account_id":          tools.StringProperty("a finance account, by the id accounts gives; for assets, what it values: its own asset and its holdings; for import_transactions and preview_import_transactions: the imported account the rows are of"),
					"is_new_account":              tools.BooleanProperty("for import_transactions and preview_import_transactions: true only when the person said the rows are of an account not imported before, after the server named accounts at the same institution"),
					"is_holding":                  tools.BooleanProperty("for assets: true lists only the holdings of investment accounts (one asset per position); left out, assets leaves the holdings out unless finance_account_id is given"),
					"finance_transaction_id":      tools.StringProperty("a finance transaction, by the id transactions gives"),
					"finance_transaction_ids":     tools.ArrayProperty("several finance transactions, by the ids transactions gives. For transactions: only these, to read one by its id (a duplicate's counted copy, say). For categorize_transaction: at most 500 categorized together. For propose_spending_rules: at most 5000", tools.StringProperty("a finance transaction id")),
					"finance_security_id":         tools.StringProperty("for trades: a security, by the financeSecurityId an asset or a trade gives"),
					"from":                        tools.StringProperty("for transactions, trades, spending_summary and net_worth: the first day, 2026-09-01"),
					"to":                          tools.StringProperty("for transactions, trades, spending_summary and net_worth: the last day, 2026-09-30"),
					"text":                        tools.StringProperty("for transactions: words within the description or merchant; for assets: words within the asset's name"),
					"minimum_amount":              tools.StringProperty("the least signed amount; money out is negative"),
					"maximum_amount":              tools.StringProperty("the greatest signed amount"),
					"provider_category":           tools.StringProperty("for transactions: the provider's category"),
					"spending_category_id":        tools.StringProperty("a spending category, by its name or by the id spending_categories gives; transfer marks a transfer between the person's own accounts; other is the one for what fits none of the others; categorize_transaction never takes it away"),
					"is_uncategorized":            tools.BooleanProperty("for transactions: only the ones that need a spending category, not decided yet (a transfer has the transfer category, one that fits nothing the other category)"),
					"duplicate_of_transaction_id": tools.StringProperty("for transactions: only the mirrored copies of this finance transaction, its duplicates"),
					"is_duplicate_included":       tools.BooleanProperty("for transactions: list the mirrored copies too; left out, they are left out (and counted in leftOutDuplicateCount), as every total leaves them out"),
					"limit":                       tools.IntegerProperty("for transactions and trades: how many, at most 200"),
					"offset":                      tools.IntegerProperty("for transactions and trades: how many to skip, for the next page (the offset of the page before plus the rows it gave)"),
					"after":                       tools.StringProperty("for transactions and trades: the nextCursor of the page before"),
					"group_by":                    tools.EnumProperty("for spending_summary", "spendingCategory", "providerCategory", "merchant", "month", "financeAccount"),
					"currency_code":               tools.StringProperty("a currency code like EUR. For accounts, credit_usage, spending_summary, net_worth, spending_by_day, cash_flow and saving_summary: convert totals into it instead of the reporting currency. For create_asset, update_asset, set_budget and savings targets: its currency. For set_reporting_currency: the currency to show totals in. For import_transactions: the account's currency"),
					"institution_name":            tools.StringProperty("for import_transactions: the bank or card issuer, as its app or list shows it"),
					"account_name":                tools.StringProperty("for import_transactions: the account's own name or label as shown (Savings, the card's product name), left out when none; for rename_statement_account: the new name"),
					"account_number":              tools.StringProperty("for import_transactions: the account number as shown, digits only with masked ones as they appear (****1234); never invent one, and never put a word such as the card's brand in its place"),
					"is_account_number_partial":   tools.BooleanProperty("for import_transactions: true when only some digits of the account number are shown"),
					"statement_account_kind":      tools.EnumProperty("for import_transactions: bank (checking, savings), card (a credit card) or other (prepaid, electronic money)", "bank", "card", "other"),
					"bank_code":                   tools.StringProperty("for import_transactions: the bank's code or routing number where the list shows one; left out otherwise"),
					"transaction_rows": tools.ArrayProperty("for import_transactions: every row the pictures show, each once, as the list shows it, oldest first or newest first", tools.Object(map[string]any{
						"posted_on":              tools.StringProperty("the day, 2026-09-30 (a list's 26.09.30 is 2026-09-30)"),
						"description":            tools.StringProperty("the description exactly as shown, half-width katakana and all; the server normalizes it"),
						"amount":                 tools.StringProperty("the signed amount as a decimal string without separators: money out negative, money in positive; -4500, 12.34"),
						"transaction_kind":       tools.EnumProperty("what the row is, when the list says", financecore.TransactionKinds()...),
						"running_balance_amount": tools.StringProperty("the balance the list shows after this row, exactly as shown, when it shows one"),
						"total_month":            tools.StringProperty("the month heading the row is listed under, 2026-09, only when the list groups rows by a statement month rather than the day they posted"),
					}, "posted_on", "description", "amount")),
					"ledger_balance_amount":    tools.StringProperty("for import_transactions: the account's balance shown in the newest picture, signed (what a card owes is negative)"),
					"ledger_balance_on":        tools.StringProperty("for import_transactions: the day that balance is as of, 2026-09-30"),
					"ledger_balance_time_zone": tools.StringProperty("for import_transactions: the time zone of that day, Asia/Tokyo; the person's when left out"),
					"monthly_totals": tools.ArrayProperty("for import_transactions: the total a card's list shows for each month, exactly as shown; each month's rows are checked against it", tools.Object(map[string]any{
						"total_month":  tools.StringProperty("the month, 2026-09"),
						"total_amount": tools.StringProperty("the total as shown, a decimal string without separators"),
					}, "total_month", "total_amount")),
					"from_currency_code":          tools.StringProperty("for exchange_rate and convert_currency: the currency converted from"),
					"to_currency_code":            tools.StringProperty("for exchange_rate and convert_currency: the currency converted into"),
					"amount":                      tools.StringProperty("for convert_currency: the amount"),
					"rate_on":                     tools.StringProperty("for exchange_rate and convert_currency: the day of the exchange rate; today when left out"),
					"asset_id":                    tools.StringProperty("an asset, by the id assets gives"),
					"asset_name":                  tools.StringProperty("what the person calls the asset"),
					"asset_kind":                  tools.EnumProperty("what the asset is; the last four are owed. For assets, only this kind; empty is every kind", "", "cash", "investment", "retirement", "property", "vehicle", "other_asset", "credit_card", "loan", "mortgage", "other_liability"),
					"valuation_source":            tools.EnumProperty("for create_asset and update_asset, where its values come from (agent_reading when create_asset is given a value); for record_valuation, agent_reading (read from a connected server, the default) or agent_estimate", "manual", "agent_reading", "agent_estimate"),
					"closed_on":                   tools.StringProperty("the day an asset was sold or paid off, or a savings target closed"),
					"should_reopen":               tools.BooleanProperty("open the asset or savings target again instead of closing it"),
					"value":                       tools.StringProperty("for record_valuation, or create_asset's first value: what the asset is worth, positive for what is owed too"),
					"valued_on":                   tools.StringProperty("for record_valuation and create_asset: the day of the value; today when left out"),
					"estimate_low":                tools.StringProperty("for an estimate: the low end"),
					"estimate_high":               tools.StringProperty("for an estimate: the high end"),
					"valuation_note":              tools.StringProperty("for record_valuation: what the value rests on"),
					"evidence_urls":               tools.ArrayProperty("for an estimate: the pages it rests on", tools.StringProperty("a web address")),
					"valuation_id":                tools.StringProperty("a valuation, by the id asset_history gives"),
					"spending_category_name":      tools.StringProperty("a spending category's name"),
					"parent_spending_category_id": tools.StringProperty("the parent spending category, by its name or id; empty makes it top-level"),
					"is_income":                   tools.BooleanProperty("money in the spending category is income"),
					"is_hidden":                   tools.BooleanProperty("leave the spending category out of lists and charts"),
					"spending_rule_id":            tools.StringProperty("a spending rule, by the id spending_rules gives"),
					"match_text":                  tools.StringProperty("what a spending rule matches within the merchant, or the description when there is none"),
					"rule_priority":               tools.IntegerProperty("the order a spending rule is tried in, lowest first"),
					"should_create_spending_rule": tools.BooleanProperty("for categorize_transaction: also add a spending rule for its merchant, or for several, exactly the rules propose_spending_rules lists for the same ids and spending category; only when the person said yes"),
					"monthly_amount":              tools.StringProperty("for set_budget: the amount a month, or on an income spending category the income expected a month; 0 ends the budget"),
					"effective_from":              tools.StringProperty("for set_budget: the month it starts, 2026-10; this month when left out"),
					"month":                       tools.StringProperty("a month, 2026-09. For budget_status, saving_summary and spending_by_day: the month, this one when left out. For transactions, trades, spending_summary, net_worth and cash_flow: shorthand for that whole month, instead of from and to"),
					"year":                        tools.StringProperty("for budget_status and saving_summary: a calendar year, 2026, instead of month: each month's budgets as they were in force that month, added up, against the year's spending and income (the year to date for this one, with a projection to its end). saving_summary counts only the months with a budget, and says which; each budget_status row says its own months (budgetedMonthCount, firstBudgetedMonth, lastBudgetedMonth)"),
					"compare_month":               tools.StringProperty("for spending_by_day: the month to compare with; the one before when left out"),
					"from_month":                  tools.StringProperty("for cash_flow: the first month"),
					"to_month":                    tools.StringProperty("for cash_flow: the last month"),
					"savings_target_id":           tools.StringProperty("a savings target, by the id savings_targets gives"),
					"savings_target_name":         tools.StringProperty("what the person calls the savings target"),
					"target_amount":               tools.StringProperty("the amount to save"),
					"target_on":                   tools.StringProperty("the day to reach it by"),
					"target_measure":              tools.EnumProperty("how progress is measured: cash_flow (income less spending), asset_value (what asset_ids and finance_account_ids are worth) or net_worth", "cash_flow", "asset_value", "net_worth"),
					"starting_amount":             tools.StringProperty("for asset_value: what the assets were worth at the start; for net_worth: net worth at the start, recorded on started_on when left out"),
					"started_on":                  tools.StringProperty("the day the savings target starts; today when left out"),
					"asset_ids":                   tools.ArrayProperty("for asset_value: single assets it measures", tools.StringProperty("an asset id")),
					"finance_account_ids":         tools.ArrayProperty("for asset_value: finance accounts it measures whole, every asset each values (its cash and its holdings, those bought later too); prefer this to listing an investment account's holdings", tools.StringProperty("a finance account id")),
				}, "operation"),
				Guidance: "finance: for anything about the person's accounts, spending, budgets, savings, net worth or exchange rates, use this and quote its numbers; never add amounts in different currencies yourself. A SimpleFIN setup token or a provider credential pasted in conversation is not used: point to the Finance tab of their agent page in the dashboard, teanode finance link-simplefin or teanode finance import-credential.",
				RiskOf: func(arguments json.RawMessage) tools.Risk {
					var call struct {
						Operation string `json:"operation"`
					}
					if json.Unmarshal(arguments, &call) != nil {
						return ""
					}
					name := strings.ToLower(strings.TrimSpace(call.Operation))
					if name == "import_transactions" && isRefusedTransactionRows(arguments) {
						return tools.RiskRead
					}
					if name == "categorize_transaction" {
						categorizeCall := map[string]any{}
						if json.Unmarshal(arguments, &categorizeCall) == nil && isNoSpendingCategory(categorizeCall) {
							return tools.RiskRead
						}
					}
					if operation, isKnown := operations[name]; isKnown {
						return operation.risk
					}
					if name == "mark_transfer" {
						// Answered with how a transfer is marked now,
						// acting on nothing, so it is not put to the person.
						return tools.RiskRead
					}
					return ""
				},
				PreviewIn: func(ctx context.Context, arguments json.RawMessage) string {
					call := map[string]any{}
					_ = json.Unmarshal(arguments, &call)
					name := strings.ToLower(text(call, "operation"))
					if operation, isKnown := operations[name]; isKnown && operation.preview != nil {
						return operation.preview(newPreviewLookup(ctx), call)
					}
					return "Read their finances: " + strings.ReplaceAll(name, "_", " ")
				},
				Run: run,
			},
		}
	})
}

// text is a string argument, trimmed; empty when absent or not a string.
func text(call map[string]any, key string) string {
	value, _ := call[key].(string)
	return strings.TrimSpace(value)
}

// texts is a list argument's strings, trimmed, blanks left out; nil when
// absent or not a list.
func texts(call map[string]any, key string) []string {
	var found []string
	switch typed := call[key].(type) {
	case []any:
		for _, element := range typed {
			if value, isString := element.(string); isString && strings.TrimSpace(value) != "" {
				found = append(found, strings.TrimSpace(value))
			}
		}
	case []string:
		for _, value := range typed {
			if strings.TrimSpace(value) != "" {
				found = append(found, strings.TrimSpace(value))
			}
		}
	}
	return found
}

// transactionIds is the finance transactions a call names, by
// finance_transaction_ids and finance_transaction_id, each once.
func transactionIds(call map[string]any) []string {
	var financeTransactionIds []string
	isNamed := map[string]bool{}
	for _, financeTransactionId := range append(texts(call, "finance_transaction_ids"), text(call, "finance_transaction_id")) {
		if financeTransactionId != "" && !isNamed[financeTransactionId] {
			isNamed[financeTransactionId] = true
			financeTransactionIds = append(financeTransactionIds, financeTransactionId)
		}
	}
	return financeTransactionIds
}

// previewExampleCount is how many of the finance transactions a call
// categorizes together its confirmation card names.
const previewExampleCount = 3

// categorizeSeveralPreview is the confirmation card's line for
// categorizing several finance transactions together: how many, a few of
// them, and the spending rules that would be added.
func categorizeSeveralPreview(lookup *previewLookup, call map[string]any, financeTransactionIds []string) string {
	transactionCount := len(financeTransactionIds)
	spendingCategoryId := text(call, "spending_category_id")
	line := fmt.Sprintf("Categorize %d transactions as %s", transactionCount, lookup.spendingCategoryName(spendingCategoryId))
	if lookup.isTransferSpendingCategory(spendingCategoryId) {
		line = fmt.Sprintf("Mark %d transactions as transfers between their own accounts, neither spending nor income", transactionCount)
	}
	examples := lookup.transactionLines(financeTransactionIds[:min(previewExampleCount, transactionCount)])
	if len(examples) > 0 {
		line += ": " + strings.Join(examples, "; ")
		if remainingCount := transactionCount - len(examples); remainingCount > 0 {
			line += fmt.Sprintf("; and %d more", remainingCount)
		}
	}
	if !isTrue(call, "should_create_spending_rule") {
		return line
	}
	proposals, leftOutReasons, isRead := lookup.spendingRuleProposals(financeTransactionIds, spendingCategoryId)
	switch {
	case !isRead:
		return line + ", and add a spending rule for each merchant no spending rule already files there, applied to past transactions too"
	case len(proposals) == 0 && len(leftOutReasons) == 0:
		return line + "; no new spending rule, since theirs already file these there"
	case len(proposals) == 0:
		line += "; no new spending rule"
	default:
		line += fmt.Sprintf(", and add %d spending rules, applied to past transactions too: %s", len(proposals), strings.Join(proposals, ", "))
	}
	for _, reason := range leftOutReasons {
		line += "; " + reason
	}
	return line
}

// isNoSpendingCategory says a categorize_transaction call gives no
// spending category, or the word none for one. Neither is a choice: no
// spending category is the state of a transaction not decided yet, and
// what fits nothing is the other category. The call is refused before
// the person is asked to confirm it, since it would be refused after.
// A spending category the person named none is still theirs by its id.
func isNoSpendingCategory(call map[string]any) bool {
	spendingCategoryId := text(call, "spending_category_id")
	return spendingCategoryId == "" || strings.EqualFold(spendingCategoryId, "none")
}

// categorizeTransactions is categorize_transaction: CategorizeTransaction
// for one finance transaction, as it always was, and
// CategorizeTransactions for several, all or none.
func categorizeTransactions(ctx context.Context, executor tools.Operations, name string, asked map[string]any) (*tools.Result, error) {
	financeTransactionIds := transactionIds(asked)
	if len(financeTransactionIds) == 0 {
		return nil, fmt.Errorf("%s needs finance_transaction_id, or finance_transaction_ids for several", name)
	}
	variables := map[string]any{"spendingCategoryId": text(asked, "spending_category_id")}
	graphqlOperation := "CategorizeTransaction"
	if len(financeTransactionIds) == 1 {
		variables["financeTransactionId"] = financeTransactionIds[0]
		if shouldCreate, isGiven := asked["should_create_spending_rule"]; isGiven {
			variables["shouldCreateSpendingRule"] = shouldCreate
		}
	} else {
		graphqlOperation = "CategorizeTransactions"
		variables["financeTransactionIds"] = financeTransactionIds
		// The rules saved are the ones ProposeSpendingRules proposes, the
		// list the confirmation card showed, sent back to be saved as they
		// are.
		if isTrue(asked, "should_create_spending_rule") {
			var proposals *client.SpendingRuleProposals
			proposeVariables := map[string]any{"financeTransactionIds": financeTransactionIds, "spendingCategoryId": variables["spendingCategoryId"]}
			if err := client.RunFinance(ctx, executor, "ProposeSpendingRules", proposeVariables, &proposals); err != nil {
				return nil, err
			}
			variables["spendingRules"] = proposals.Confirmed()
		}
	}
	var answered any
	if err := client.RunFinance(ctx, executor, graphqlOperation, variables, &answered); err != nil {
		return nil, err
	}
	result, err := tools.JSONResult(map[string]any{name: answered})
	if err != nil {
		return nil, err
	}
	result.Untrusted = true
	result.Note = strings.ReplaceAll(name, "_", " ")
	return result, nil
}

// isTrue says a boolean argument is true.
func isTrue(call map[string]any, key string) bool {
	value, _ := call[key].(bool)
	return value
}

// renamedSuffix is ", renaming it <name>" when a new name is given.
func renamedSuffix(call map[string]any, key string) string {
	if name := text(call, key); name != "" {
		return ", renaming it " + tools.Named(name, "")
	}
	return ""
}

// onSuffix is " on <day>" when the argument is given.
func onSuffix(call map[string]any, key string) string {
	if day := text(call, key); day != "" {
		return " on " + day
	}
	return ""
}

// camelCase is an argument's name as the finance area spells it:
// finance_account_id is financeAccountId.
func camelCase(snake string) string {
	var builder strings.Builder
	isUpperNext := false
	for _, letter := range snake {
		if letter == '_' {
			isUpperNext = true
			continue
		}
		if isUpperNext {
			builder.WriteRune(unicode.ToUpper(letter))
			isUpperNext = false
			continue
		}
		builder.WriteRune(letter)
	}
	return builder.String()
}

func run(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	asked := map[string]any{}
	if err := json.Unmarshal(tools.SettledArguments(call.Arguments), &asked); err != nil {
		return nil, fmt.Errorf("the arguments are not a JSON object: %w", err)
	}
	current, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	name := strings.ToLower(text(asked, "operation"))
	if name == "mark_transfer" {
		// An operation from before transfer was a spending category, still
		// asked for by a model that learned it then. Whatever came with it
		// is not acted on: the answer says how a transfer is marked now.
		return tools.TextResult("mark_transfer is gone: a transfer is a spending category now. " +
			"Mark a transaction a transfer with categorize_transaction and spending_category_id transfer; " +
			"any other spending category takes the mark away. A spending rule marks transfers with spending_category_id transfer too."), nil
	}
	operation, isKnown := operations[name]
	if !isKnown {
		return nil, fmt.Errorf("%q is not an operation; the operations are %s", name, strings.Join(operationNames(), ", "))
	}
	if name == "link_simplefin" {
		// Whatever came with it, a setup token included, is neither used
		// nor repeated back: the answer only says where to paste it.
		return tools.TextResult("A SimpleFIN setup token is never taken in a conversation: it would stay in the transcript and go to the model provider. " +
			"The person pastes it on the Finance tab of their agent page in the dashboard, or runs `teanode finance link-simplefin`, which reads it without echoing. " +
			"They make the token on the SimpleFIN Bridge's website."), nil
	}
	if name == "delete_statement_account" {
		return tools.TextResult("Deleting an account of imported statements removes its transactions and its net worth history for good, so it is the person's to do: " +
			"on the dashboard's Finance page, under Accounts, with the trash icon on the account's row, or with `teanode finance delete-statement-account <finance-account-id>`. " +
			"`rename_statement_account` renames one, which needs no delete."), nil
	}
	if name == "import_credential" {
		// Likewise for the credential of a connection made elsewhere,
		// which opens the person's accounts for as long as it lives.
		return tools.TextResult("A provider credential (a Plaid access token or a SimpleFIN access URL) is never taken in a conversation: it would stay in the transcript and go to the model provider. " +
			"To bring an existing connection in instead of linking again, the person uses Bring an existing connection in the Link an institution dialog on the Finance tab of their agent page, " +
			"or runs `teanode finance import-credential --provider plaid` (or `simplefin`), which reads it from a file or without echoing. " +
			"For Plaid, only a link made with this server's Plaid keys can be brought in."), nil
	}
	dropUnreadArguments(operation, asked)
	if name == "assets" {
		if _, isAsked := asked["is_holding"]; !isAsked && text(asked, "finance_account_id") == "" {
			asked["is_holding"] = false
		}
	}
	if err := checkArguments(name, operation, asked); err != nil {
		return nil, err
	}
	if operation.isMonthShorthand {
		if err := spreadMonth(name, asked); err != nil {
			return nil, err
		}
	}
	for _, key := range operation.required {
		if value, isGiven := asked[key]; !isGiven || value == nil || value == "" {
			return nil, fmt.Errorf("%s needs %s", name, key)
		}
	}
	// Before the names are read, so the word none is refused as RiskOf
	// judged it, whatever the person has named a spending category.
	if name == "categorize_transaction" && isNoSpendingCategory(asked) {
		return nil, fmt.Errorf("%s needs a spending category: no spending category means not decided yet and is not a choice; for one that fits none of them, give spending_category_id other", name)
	}
	// A spending category may be named rather than given by id, as on the
	// command line. A name that is none of the person's is passed on as
	// given, for the API to refuse as it refuses an unknown id.
	lookup := newPreviewLookup(ctx)
	for _, key := range []string{"spending_category_id", "parent_spending_category_id"} {
		if spendingCategory := lookup.spendingCategoryFor(text(asked, key)); spendingCategory != nil {
			asked[key] = spendingCategory.ID
		}
	}
	executor := current.Operations()
	switch name {
	case "link_plaid":
		return linkAddress(current, "", "Give the person this address to open in their browser, signed in to the dashboard, to link an institution through Plaid. The new finance source appears in sources once they finish, and syncs within minutes."), nil
	case "repair":
		return linkAddress(current, text(asked, "source_id"), "Give the person this address to open in their browser, signed in to the dashboard, to sign in to the institution again. The same finance source syncs again once they finish."), nil
	case "sync", "disable_source", "enable_source", "delete_source":
		return sourceOperation(ctx, executor, name, text(asked, "source_id"))
	case "categorize_transaction":
		return categorizeTransactions(ctx, executor, name, asked)
	case "import_transactions", "preview_import_transactions":
		return importTransactions(ctx, executor, name, asked)
	case "import_statement":
		// Only a message in a mailbox the person granted, as mail_read
		// reads: a mailbox kept back from the agent stays kept back.
		itemId := text(asked, "mailbox_item_id")
		views, err := mailbox.GrantedMailboxes(ctx, executor)
		if err != nil {
			return nil, err
		}
		if _, _, err := mailbox.MailboxOfItem(ctx, executor, views, itemId); err != nil {
			return nil, err
		}
		var imported any
		if err := client.RunFinance(ctx, executor, "ImportStatement", map[string]any{"mailboxItemId": itemId}, &imported); err != nil {
			return nil, err
		}
		result, err := tools.JSONResult(map[string]any{name: imported})
		if err != nil {
			return nil, err
		}
		result.Untrusted = true
		result.Note = "import statement"
		return result, nil
	}

	variables := map[string]any{}
	for _, key := range operation.arguments {
		if value, isGiven := asked[key]; isGiven && value != nil {
			variables[camelCase(key)] = value
		}
	}
	if limit, isNumber := variables["limit"].(float64); isNumber {
		variables["limit"] = int(limit)
	}
	if offset, isNumber := variables["offset"].(float64); isNumber {
		variables["offset"] = int(offset)
	}
	if priority, isNumber := variables["rulePriority"].(float64); isNumber {
		variables["rulePriority"] = int(priority)
	}
	switch name {
	case "record_valuation":
		// What the agent records is a reading or an estimate, never the
		// person's own value, which wins over both on the same day.
		if err := agentValuationSource(name, text(asked, "valuation_source"), variables); err != nil {
			return nil, err
		}
	case "create_asset":
		// A first value given here is the agent's too: a reading, unless
		// the asset is one the agent estimates.
		if text(asked, "value") != "" {
			if err := agentValuationSource(name, text(asked, "valuation_source"), variables); err != nil {
				return nil, err
			}
		}
	}
	var answered any
	if err := client.RunFinance(ctx, executor, operation.graphqlOperation, variables, &answered); err != nil {
		return nil, err
	}
	payload := map[string]any{name: answered}
	if hint := emptyHint(name, answered); hint != "" {
		payload["hint"] = hint
	}
	if hint := pageHint(name, variables, answered); hint != "" {
		payload["hint"] = hint
	}
	result, err := tools.JSONResult(payload)
	if err != nil {
		return nil, err
	}
	result.Untrusted = operation.isUntrusted
	if operation.risk != tools.RiskRead {
		result.Note = strings.ReplaceAll(name, "_", " ")
	}
	return result, nil
}

// agentValuationSource sets the valuation source of a value the agent
// records: agent_reading when none is given, agent_estimate when asked,
// and never manual, which is the person's own.
func agentValuationSource(name, valuationSource string, variables map[string]any) error {
	switch valuationSource {
	case "":
		variables["valuationSource"] = "agent_reading"
	case "agent_reading", "agent_estimate":
	default:
		return fmt.Errorf("%s with a value from here is agent_reading or agent_estimate; a value the person gives is theirs to record on the dashboard's Finance page or with teanode finance record-valuation", name)
	}
	return nil
}

// emptyHint is what to tell the person when a listing came back empty
// because nothing is linked or set up yet.
func emptyHint(name string, answered any) string {
	list, isList := answered.([]any)
	if !isList || len(list) > 0 {
		return ""
	}
	switch name {
	case "providers":
		return "this server offers no provider to link an institution through; the operator turns them on in the agent settings"
	case "sources", "accounts":
		return "nothing is linked yet: providers says what the server offers, link_plaid gives the address to link through Plaid, and a SimpleFIN setup token goes on the Finance tab of their agent page in the dashboard or teanode finance link-simplefin"
	case "assets":
		return "no assets yet; create_asset adds one, and linking a finance source adds one per finance account"
	case "budgets":
		return "no budgets yet; propose some from the last three months, and set only what the person accepts"
	}
	return ""
}

// pageHint says which rows of how many a page of transactions or trades
// holds, so the answer can say "50 of 1,234" rather than leave the rest
// unmentioned, and how to read the next page. Empty when the page holds
// every row.
func pageHint(name string, variables map[string]any, answered any) string {
	listKey := map[string]string{"transactions": "financeTransactions", "trades": "financeTrades"}[name]
	page, isPage := answered.(map[string]any)
	if listKey == "" || !isPage {
		return ""
	}
	rows, _ := page[listKey].([]any)
	totalCount, _ := page["totalCount"].(float64)
	offset, _ := variables["offset"].(int)
	if offset == 0 && len(rows) >= int(totalCount) {
		return ""
	}
	if _, isAfterGiven := variables["after"]; isAfterGiven {
		return fmt.Sprintf("%d of %d shown, read from the cursor given; tell the person both numbers", len(rows), int(totalCount))
	}
	// An offset past the last row holds nothing, and "rows 1001 to 1000"
	// would say a range that is not there.
	if len(rows) == 0 && offset >= int(totalCount) {
		return fmt.Sprintf("offset %d is past the end, there are %d in all; tell the person how many there are", offset, int(totalCount))
	}
	hint := fmt.Sprintf("rows %d to %d of %d shown; tell the person both numbers", offset+1, offset+len(rows), int(totalCount))
	if offset+len(rows) < int(totalCount) {
		hint += fmt.Sprintf(", and offset %d reads the next page", offset+len(rows))
	}
	return hint
}

// linkAddress is the dashboard's linking page for the person to open.
func linkAddress(current tools.Run, sourceId, instruction string) *tools.Result {
	address := client.FinanceLinkPagePath
	if base := current.Configuration().DashboardBase(); base != "" {
		address = base + client.FinanceLinkPagePath
	}
	if sourceId != "" {
		address += "?source=" + sourceId
	}
	result, err := tools.JSONResult(map[string]any{"linkAddress": address, "instruction": instruction})
	if err != nil {
		return tools.TextResult("%s: %s", instruction, address)
	}
	return result
}

// sourceOperation syncs, switches or deletes a finance source through the
// operations every source has, after checking it is one of the person's
// finance sources: the finance tool acts on nothing else. What it answers
// names the source, which is the institution's name, so it is untrusted.
func sourceOperation(ctx context.Context, executor tools.Operations, name, sourceId string) (*tools.Result, error) {
	var sources []*client.FinanceSource
	if err := client.RunFinance(ctx, executor, "FinanceSources", nil, &sources); err != nil {
		return nil, err
	}
	var source *client.FinanceSource
	for _, candidate := range sources {
		if candidate.ID == sourceId {
			source = candidate
		}
	}
	if source == nil {
		return nil, fmt.Errorf("there is no finance source %q; sources lists them", sourceId)
	}
	switch name {
	case "sync":
		if source.ProviderKind == "statement" {
			return nil, fmt.Errorf("the finance source %q holds imported statements and has nothing to sync; import_statement imports a statement mailed to the person", sourceId)
		}
		// Syncing a switched-off source would switch it on again, which
		// is its own decision.
		if !source.IsEnabled {
			return nil, fmt.Errorf("the finance source %q is switched off; switch it on with enable_source first, which syncs it", sourceId)
		}
		if err := executor.Execute(ctx, client.DocumentSyncAgentKnowledgeSource, map[string]any{"sourceId": sourceId}, nil); err != nil {
			return nil, err
		}
		return noted("syncing "+source.Name+"; it starts within the minute", "syncing "+source.Name), nil
	case "disable_source", "enable_source":
		isEnabled := name == "enable_source"
		if err := executor.Execute(ctx, client.DocumentSaveAgentKnowledgeSource, map[string]any{"sourceId": sourceId, "enabled": isEnabled}, nil); err != nil {
			return nil, err
		}
		if isEnabled {
			return noted(source.Name+" is on again and syncs within the minute", "switched on "+source.Name), nil
		}
		return noted(source.Name+" is off; what it holds is kept", "switched off "+source.Name), nil
	default:
		if err := executor.Execute(ctx, client.DocumentDeleteAgentKnowledgeSource, map[string]any{"sourceId": sourceId}, nil); err != nil {
			return nil, err
		}
		return noted("deleted "+source.Name+" and its transactions, and ended it at its provider; its assets keep their history as manual ones and no longer count from today", "deleted "+source.Name), nil
	}
}

// noted is an untrusted text result with a line for the drawer.
func noted(content, note string) *tools.Result {
	result := tools.TextResult("%s", content)
	result.Note = note
	result.Untrusted = true
	return result
}
