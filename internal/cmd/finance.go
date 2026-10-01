package cmd

import (
	"context"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/finance/ofx"
)

// teanode finance: the person's finance sources, accounts and transactions,
// exchange rates, net worth, spending categories and rules, budgets and
// savings targets. One subcommand per operation of the finance area, named
// by the rule the parity test checks (the operation's name in kebab case,
// with a leading Finance dropped), calling the same operation the
// dashboard's Finance page and the agent's finance tool call.

// financeWaitPoll is how often link-plaid and repair look for the result
// of what the person does in the browser.
const financeWaitPoll = 3 * time.Second

func NewFinanceCommand() *cli.Command {
	rangeFlags := func() []cli.Flag {
		return []cli.Flag{
			&cli.StringFlag{Name: "from", Usage: "the first day, as 2026-09-01"},
			&cli.StringFlag{Name: "to", Usage: "the last day, as 2026-09-30; today by default"},
			&cli.StringFlag{Name: "since", Usage: "instead of --from: a span back from today, as 30d, 12w, 6m or 1y"},
			&cli.StringFlag{Name: "month", Usage: "instead of --from and --to: one whole month, as 2026-09"},
		}
	}
	currencyFlag := func() cli.Flag {
		return &cli.StringFlag{Name: "currency", Usage: "convert totals into this currency, a code like EUR, instead of the reporting currency"}
	}
	forceFlags := func() []cli.Flag { return []cli.Flag{JSONFlag(), ForceFlag()} }
	assetFlags := func() []cli.Flag {
		return []cli.Flag{
			JSONFlag(),
			&cli.StringFlag{Name: "kind", Usage: "cash, investment, retirement, property, vehicle, other_asset, credit_card, loan, mortgage or other_liability"},
			&cli.StringFlag{Name: "currency", Usage: "its currency, a code like USD"},
			&cli.StringFlag{Name: "valuation-source", Usage: "where its values come from: manual, agent_reading or agent_estimate"},
			&cli.StringFlag{Name: "estimate-description", Usage: "what the agent may search the web with to estimate it"},
			&cli.BoolFlag{Name: "is-estimate-allowed", Usage: "let the agent estimate it from the web"},
		}
	}
	spendingRuleFlags := func() []cli.Flag {
		return []cli.Flag{
			JSONFlag(),
			&cli.StringFlag{Name: "spending-category", Usage: "the spending category it assigns, by id or name; transfer marks what it matches as transfers"},
			&cli.StringFlag{Name: "finance-account", Usage: "only this finance account's transactions, by id"},
			&cli.StringFlag{Name: "minimum-amount", Usage: "only amounts at least this; money out is negative"},
			&cli.StringFlag{Name: "maximum-amount", Usage: "only amounts at most this"},
			&cli.IntFlag{Name: "priority", Usage: "the order it is tried in, lowest first; after every rule by default"},
		}
	}
	savingsTargetFlags := func() []cli.Flag {
		return []cli.Flag{
			JSONFlag(),
			&cli.StringFlag{Name: "target-on", Usage: "the day to reach it by, as 2027-06-30"},
			&cli.StringFlag{Name: "currency", Usage: "its currency; the reporting currency by default"},
			&cli.StringFlag{
				Name:  "measure",
				Usage: "cash_flow (income less spending, the default), asset_value (what --asset and --finance-account are worth) or net_worth",
			},
			&cli.StringFlag{
				Name:  "starting-amount",
				Usage: "what the assets, or net worth, were at the start; for net_worth, net worth on --started-on by default",
			},
			&cli.StringFlag{Name: "started-on", Usage: "the day it starts; today by default"},
			&cli.StringSliceFlag{Name: "asset", Usage: "an asset it measures, by id; repeatable"},
			&cli.StringSliceFlag{
				Name:  "finance-account",
				Usage: "a finance account it measures whole, by id: every asset it values, holdings bought later too; repeatable",
			},
		}
	}
	waitFlags := func() []cli.Flag {
		return []cli.Flag{
			JSONFlag(),
			&cli.BoolFlag{Name: "no-wait", Usage: "print the address and return, rather than wait for it to finish"},
			&cli.DurationFlag{Name: "timeout", Usage: "how long to wait", Value: 15 * time.Minute},
		}
	}
	finance := &cli.Command{
		Name:  "finance",
		Usage: "your finance sources, accounts, transactions, net worth, budgets and savings targets",
		Commands: []*cli.Command{
			{Name: "providers", Usage: "the providers this server lets you link an institution through", Flags: []cli.Flag{JSONFlag()}, Action: runFinanceProviders},
			{Name: "link-plaid", Usage: "link an institution through Plaid: prints the address to open, then waits for it to appear", Flags: waitFlags(), Action: runFinanceLinkPlaid},
			{
				Name: "link-simplefin", Usage: "link an institution through SimpleFIN with a setup token from the SimpleFIN Bridge",
				ArgsUsage: "[<setup-token> | -]",
				Description: "The setup token is read without echoing when it is not given or is -, or from standard\n" +
					"input when that is not a terminal. A token on the command line stays in the shell's history.",
				Flags: []cli.Flag{JSONFlag()}, Action: runFinanceLinkSimpleFin,
			},
			{
				Name: "import-credential", Usage: "bring a provider connection made elsewhere in as a finance source, instead of linking again",
				ArgsUsage: "[- | <file>]",
				Description: "For Plaid the credential is the access token of a link made with this server's Plaid keys, which\n" +
					"saves spending a Plaid slot on linking again; for SimpleFIN it is an access URL already claimed from\n" +
					"a setup token. It is read from the file given, from standard input with - or when that is not a\n" +
					"terminal, or at a prompt without echoing. It is never taken on the command line, where it would\n" +
					"stay in the shell's history.",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "provider", Usage: "plaid or simplefin"},
					&cli.StringFlag{Name: "institution-name", Usage: "what to call the finance source; the provider is asked when left out"},
				},
				Action: runFinanceImportCredential,
			},
			{
				Name: "import-statement", Usage: "import an OFX statement file (.ofx, .qfx or .qbo), such as a card's exported transactions",
				ArgsUsage: "<file>",
				Description: "A transaction already imported, by its account and the institution's FITID, is updated rather\n" +
					"added again, so importing overlapping statements adds nothing twice. Use - to read the file from\n" +
					"standard input.",
				Flags: []cli.Flag{JSONFlag()}, Action: runFinanceImportStatement,
			},
			{Name: "statement-import", Usage: "the address to mail OFX statements to, and what the last import did", Flags: []cli.Flag{JSONFlag()}, Action: runFinanceStatementImport},
			{
				Name: "regenerate-statement-import-address", Usage: "give the statement import address a new token; the old address stops taking mail",
				Flags: forceFlags(), Action: runFinanceRegenerateStatementImportAddress,
			},
			{Name: "repair", Usage: "sign in to a finance source's institution again: prints the address to open, then waits", ArgsUsage: "<source-id>", Flags: waitFlags(), Action: runFinanceRepair},
			{Name: "sources", Usage: "your finance sources, their state and their accounts", Flags: []cli.Flag{JSONFlag()}, Action: runFinanceSources},
			{Name: "sync", Usage: "sync a finance source now", ArgsUsage: "<source-id>", Flags: []cli.Flag{JSONFlag()}, Action: runFinanceSync},
			{Name: "disable-source", Usage: "stop a finance source syncing, keeping what it holds", ArgsUsage: "<source-id>", Flags: []cli.Flag{JSONFlag()}, Action: runFinanceDisableSource},
			{Name: "enable-source", Usage: "let a finance source sync again", ArgsUsage: "<source-id>", Flags: []cli.Flag{JSONFlag()}, Action: runFinanceEnableSource},
			{Name: "delete-source", Usage: "delete a finance source and its transactions, ending it at its provider", ArgsUsage: "<source-id>", Flags: forceFlags(), Action: runFinanceDeleteSource},
			{Name: "accounts", Usage: "your finance accounts and their balances", Flags: []cli.Flag{JSONFlag(), currencyFlag()}, Action: runFinanceAccounts},
			{Name: "credit-usage", Usage: "what your credit cards owe against their credit limits, overall and per card", Flags: []cli.Flag{JSONFlag(), currencyFlag()}, Action: runFinanceCreditUsage},
			{
				Name: "transactions", Usage: "your finance transactions, newest first",
				Flags: append(rangeFlags(), JSONFlag(),
					&cli.StringFlag{Name: "finance-account", Usage: "only this finance account's, by id"},
					&cli.StringFlag{Name: "text", Usage: "only those whose description or merchant contains this"},
					&cli.StringFlag{Name: "minimum-amount", Usage: "only amounts at least this; money out is negative"},
					&cli.StringFlag{Name: "maximum-amount", Usage: "only amounts at most this"},
					&cli.StringFlag{Name: "provider-category", Usage: "only this provider category"},
					&cli.StringFlag{Name: "spending-category", Usage: "only this spending category, by id or name"},
					&cli.BoolFlag{Name: "is-uncategorized", Usage: "only those with no spending category that are not transfers"},
					&cli.StringFlag{Name: "duplicate-of", Usage: "only the mirrored copies of this transaction, by id"},
					&cli.IntFlag{Name: "limit", Usage: "how many, at most 200", Value: 50},
					&cli.StringFlag{Name: "after", Usage: "the next page: the cursor the page before printed"},
				),
				Action: runFinanceTransactions,
			},
			{
				Name: "trades", Usage: "your trades in investment accounts, newest first: buys, sells and securities moved in or out",
				Flags: append(rangeFlags(), JSONFlag(),
					&cli.StringFlag{Name: "finance-account", Usage: "only this finance account's, by id"},
					&cli.StringFlag{Name: "finance-security", Usage: "only this security's, by the financeSecurityId assets --json gives"},
					&cli.IntFlag{Name: "limit", Usage: "how many, at most 200", Value: 50},
					&cli.StringFlag{Name: "after", Usage: "the next page: the cursor the page before printed"},
				),
				Action: runFinanceTrades,
			},
			{
				Name: "spending-summary", Usage: "money out and money in by spending category, merchant, month or account",
				Flags: append(rangeFlags(), JSONFlag(), currencyFlag(),
					&cli.StringFlag{Name: "group-by", Usage: "spendingCategory (the default), providerCategory, merchant, month or financeAccount"},
					&cli.StringFlag{Name: "finance-account", Usage: "only this finance account, by id"},
				),
				Action: runFinanceSpendingSummary,
			},
			{Name: "exchange-rate", Usage: "what one unit of a currency bought in another on a day", ArgsUsage: "<from> <to>", Flags: []cli.Flag{JSONFlag(), &cli.StringFlag{Name: "on", Usage: "the day; today by default"}}, Action: runFinanceExchangeRate},
			{Name: "convert-currency", Usage: "an amount in another currency at a day's rate", ArgsUsage: "<amount> <from> <to>", Flags: []cli.Flag{JSONFlag(), &cli.StringFlag{Name: "on", Usage: "the day; today by default"}}, Action: runFinanceConvertCurrency},
			{Name: "reporting-currency", Usage: "the currency totals are shown in, and whether you chose it", Flags: []cli.Flag{JSONFlag()}, Action: runFinanceReportingCurrency},
			{Name: "set-reporting-currency", Usage: "the one currency totals are shown in", ArgsUsage: "<currency>", Flags: []cli.Flag{JSONFlag(), &cli.BoolFlag{Name: "clear", Usage: "go back to the currency of your first finance account"}}, Action: runFinanceSetReportingCurrency},
			{Name: "net-worth", Usage: "your net worth per day", Flags: append(rangeFlags(), JSONFlag(), currencyFlag()), Action: runFinanceNetWorth},
			{
				Name: "assets", Usage: "what you own and owe, with each one's latest value", Action: runFinanceAssets,
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "kind", Usage: "only this kind: cash, investment, retirement, property, vehicle, other_asset, credit_card, loan, mortgage or other_liability"},
					&cli.StringFlag{Name: "text", Usage: "only those whose name holds these words"},
					&cli.StringFlag{Name: "finance-account", Usage: "only what this finance account values, by id: its own asset and its holdings"},
					&cli.BoolFlag{Name: "is-holding", Usage: "only the holdings of investment accounts; --is-holding=false leaves them out"},
					JSONFlag(),
				},
			},
			{Name: "asset-history", Usage: "an asset's values, newest first", ArgsUsage: "<asset-id>", Flags: []cli.Flag{JSONFlag()}, Action: runFinanceAssetHistory},
			{
				Name: "create-asset", Usage: "add something you own or owe, with its first value if you give one", ArgsUsage: "<name>",
				Flags: append(assetFlags(),
					&cli.StringFlag{Name: "value", Usage: "its first value, positive for what is owed too"},
					&cli.StringFlag{Name: "on", Usage: "the day of the first value; today by default"},
				),
				Action: runFinanceCreateAsset,
			},
			{Name: "update-asset", Usage: "change an asset", ArgsUsage: "<asset-id>", Flags: append(assetFlags(), &cli.StringFlag{Name: "name", Usage: "its new name"}), Action: runFinanceUpdateAsset},
			{
				Name: "close-asset", Usage: "record the day an asset was sold or paid off", ArgsUsage: "<asset-id>",
				Flags:  []cli.Flag{JSONFlag(), &cli.StringFlag{Name: "on", Usage: "the day; today by default"}, &cli.BoolFlag{Name: "reopen", Usage: "open it again instead"}},
				Action: runFinanceCloseAsset,
			},
			{Name: "delete-asset", Usage: "delete an asset and its whole history", ArgsUsage: "<asset-id>", Flags: forceFlags(), Action: runFinanceDeleteAsset},
			{
				Name: "record-valuation", Usage: "record what an asset is worth on a day", ArgsUsage: "<asset-id> <value>",
				Flags:  []cli.Flag{JSONFlag(), &cli.StringFlag{Name: "on", Usage: "the day; today by default"}, &cli.StringFlag{Name: "note", Usage: "what the value rests on"}},
				Action: runFinanceRecordValuation,
			},
			{Name: "delete-valuation", Usage: "delete one value from an asset's history", ArgsUsage: "<valuation-id>", Flags: forceFlags(), Action: runFinanceDeleteValuation},
			{Name: "spending-categories", Usage: "your spending categories", Flags: []cli.Flag{JSONFlag()}, Action: runFinanceSpendingCategories},
			{
				Name: "create-spending-category", Usage: "add a spending category", ArgsUsage: "<name>",
				Flags: []cli.Flag{
					JSONFlag(), &cli.StringFlag{Name: "parent", Usage: "its parent spending category, by id or name"},
					&cli.BoolFlag{Name: "is-income", Usage: "money in it is income"}, &cli.BoolFlag{Name: "is-hidden", Usage: "leave it out of lists and charts"},
				},
				Action: runFinanceCreateSpendingCategory,
			},
			{
				Name: "update-spending-category", Usage: "change a spending category", ArgsUsage: "<spending-category>",
				Flags: []cli.Flag{
					JSONFlag(), &cli.StringFlag{Name: "name", Usage: "its new name"},
					&cli.StringFlag{Name: "parent", Usage: "its parent spending category, by id or name; empty makes it top-level"},
					&cli.BoolFlag{Name: "is-income", Usage: "money in it is income"}, &cli.BoolFlag{Name: "is-hidden", Usage: "leave it out of lists and charts"},
				},
				Action: runFinanceUpdateSpendingCategory,
			},
			{Name: "delete-spending-category", Usage: "delete a spending category; its transactions become uncategorized", ArgsUsage: "<spending-category>", Flags: forceFlags(), Action: runFinanceDeleteSpendingCategory},
			{Name: "spending-rules", Usage: "your spending rules, in the order they are tried", Flags: []cli.Flag{JSONFlag()}, Action: runFinanceSpendingRules},
			{
				Name: "propose-spending-rules", Usage: "the spending rules categorize-transaction --create-spending-rule would save for several transactions (up to 5000): each with the rule it goes ahead of and how many other transactions it would change, and what was left out; saves nothing",
				ArgsUsage: "<transaction-id>... <spending-category>", Flags: []cli.Flag{JSONFlag()}, Action: runFinanceProposeSpendingRules,
			},
			{Name: "create-spending-rule", Usage: "add a spending rule, applied at once to past transactions except the ones you chose", ArgsUsage: "<match-text>", Flags: spendingRuleFlags(), Action: runFinanceCreateSpendingRule},
			{Name: "update-spending-rule", Usage: "change a spending rule", ArgsUsage: "<spending-rule-id>", Flags: append(spendingRuleFlags(), &cli.StringFlag{Name: "match-text", Usage: "what it matches in the merchant or description"}), Action: runFinanceUpdateSpendingRule},
			{Name: "delete-spending-rule", Usage: "delete a spending rule", ArgsUsage: "<spending-rule-id>", Flags: forceFlags(), Action: runFinanceDeleteSpendingRule},
			{
				Name: "categorize-transaction", Usage: "give one or more transactions a spending category, which nothing overwrites; transfer marks them transfers between your own accounts", ArgsUsage: "<transaction-id>... <spending-category | none>",
				Flags: []cli.Flag{JSONFlag(), &cli.BoolFlag{
					Name: "create-spending-rule", Usage: "and a spending rule for its merchant; for several transactions, saves what propose-spending-rules lists and prints it",
				}},
				Action: runFinanceCategorizeTransaction,
			},
			{
				Name: "count-transaction", Usage: "count a mirrored copy, the same charge reported again on another account, as a real charge of its own", ArgsUsage: "<transaction-id>",
				Flags: []cli.Flag{JSONFlag()}, Action: runFinanceCountTransaction,
			},
			{
				Name: "undo-count-transaction", Usage: "take back count-transaction, so mirror detection decides again whether it is a duplicate", ArgsUsage: "<transaction-id>",
				Flags: []cli.Flag{JSONFlag()}, Action: runFinanceCountTransaction,
			},
			{Name: "budgets", Usage: "your budgets, by spending category and month", Flags: []cli.Flag{JSONFlag()}, Action: runFinanceBudgets},
			{
				Name: "set-budget", Usage: "set a spending category's monthly budget from a month on, or the income expected of an income category; 0 ends it", ArgsUsage: "<spending-category> <monthly-amount>",
				Flags:  []cli.Flag{JSONFlag(), &cli.StringFlag{Name: "currency", Usage: "its currency; the reporting currency by default"}, &cli.StringFlag{Name: "from", Usage: "the month it starts, as 2026-10; this month by default"}},
				Action: runFinanceSetBudget,
			},
			{Name: "budget-status", Usage: "each spending category against its budget this month, and each income category against the income expected", Flags: []cli.Flag{JSONFlag(), &cli.StringFlag{Name: "month", Usage: "another month, as 2026-08"}}, Action: runFinanceBudgetStatus},
			{
				Name: "saving-summary", Usage: "this month's saving: income budgets less spending budgets, against income less spending so far and projected",
				Flags:  []cli.Flag{JSONFlag(), &cli.StringFlag{Name: "month", Usage: "another month, as 2026-08"}, &cli.StringFlag{Name: "currency", Usage: "convert into this currency instead of the reporting currency"}},
				Action: runFinanceSavingSummary,
			},
			{
				Name: "spending-by-day", Usage: "this month's spending day by day against last month's",
				Flags: []cli.Flag{
					JSONFlag(), currencyFlag(), &cli.StringFlag{Name: "month", Usage: "another month, as 2026-08"},
					&cli.StringFlag{Name: "compare-month", Usage: "the month to compare with; the one before by default"},
				},
				Action: runFinanceSpendingByDay,
			},
			{
				Name: "cash-flow", Usage: "income, spending and what was left, month by month",
				Flags: []cli.Flag{
					JSONFlag(), currencyFlag(), &cli.StringFlag{Name: "from-month", Usage: "the first month; eleven months back by default"},
					&cli.StringFlag{Name: "to-month", Usage: "the last month; this one by default"},
				},
				Action: runFinanceCashFlow,
			},
			{Name: "savings-targets", Usage: "your savings targets and how each stands", Flags: []cli.Flag{JSONFlag()}, Action: runFinanceSavingsTargets},
			{Name: "create-savings-target", Usage: "add an amount to save by a day", ArgsUsage: "<name> <target-amount>", Flags: savingsTargetFlags(), Action: runFinanceCreateSavingsTarget},
			{
				Name: "update-savings-target", Usage: "change a savings target", ArgsUsage: "<savings-target-id>",
				Flags:  append(savingsTargetFlags(), &cli.StringFlag{Name: "name", Usage: "its new name"}, &cli.StringFlag{Name: "target-amount", Usage: "its new amount"}),
				Action: runFinanceUpdateSavingsTarget,
			},
			{
				Name: "close-savings-target", Usage: "close a savings target", ArgsUsage: "<savings-target-id>",
				Flags:  []cli.Flag{JSONFlag(), &cli.StringFlag{Name: "on", Usage: "the day; today by default"}, &cli.BoolFlag{Name: "reopen", Usage: "open it again instead"}},
				Action: runFinanceCloseSavingsTarget,
			},
		},
	}
	for _, subcommand := range finance.Commands {
		if operation := financeSubcommandOperations[subcommand.Name]; operation != "" {
			subcommand.Metadata = map[string]any{financeOperationKey: operation}
		}
	}
	return finance
}

// financeOperationKey is where a subcommand's metadata names the finance
// operation its action calls, read back by operationOf.
const financeOperationKey = "operation"

// financeSubcommandOperations is the finance operation each subcommand
// calls. The subcommand's action calls the one named here (operationOf),
// so the parity test that checks these names checks what runs.
var financeSubcommandOperations = map[string]string{
	"providers": "FinanceProviders", "link-simplefin": "LinkSimpleFIN", "import-credential": "ImportFinanceCredential",
	"sources": "FinanceSources", "accounts": "FinanceAccounts", "credit-usage": "CreditUsage", "transactions": "FinanceTransactions", "trades": "FinanceTrades", "spending-summary": "FinanceSpendingSummary",
	"exchange-rate": "ExchangeRate", "convert-currency": "ConvertCurrency", "reporting-currency": "ReportingCurrency",
	"set-reporting-currency": "SetReportingCurrency", "net-worth": "NetWorth", "assets": "Assets",
	"asset-history": "AssetHistory", "create-asset": "CreateAsset", "update-asset": "UpdateAsset",
	"close-asset": "CloseAsset", "delete-asset": "DeleteAsset", "record-valuation": "RecordValuation",
	"delete-valuation": "DeleteValuation", "spending-categories": "SpendingCategories",
	"create-spending-category": "CreateSpendingCategory", "update-spending-category": "UpdateSpendingCategory",
	"delete-spending-category": "DeleteSpendingCategory", "spending-rules": "SpendingRules", "propose-spending-rules": "ProposeSpendingRules",
	"create-spending-rule": "CreateSpendingRule", "update-spending-rule": "UpdateSpendingRule",
	"delete-spending-rule": "DeleteSpendingRule", "categorize-transaction": "CategorizeTransaction",
	"count-transaction": "CountTransaction", "undo-count-transaction": "UndoCountTransaction",
	"budgets": "Budgets", "set-budget": "SetBudget", "budget-status": "BudgetStatus",
	"saving-summary": "SavingSummary", "spending-by-day": "SpendingByDay", "cash-flow": "CashFlow", "savings-targets": "SavingsTargets",
	"create-savings-target": "CreateSavingsTarget", "update-savings-target": "UpdateSavingsTarget",
	"close-savings-target": "CloseSavingsTarget", "import-statement": "ImportStatement", "statement-import": "StatementImport",
	"regenerate-statement-import-address": "RegenerateStatementImportAddress",
}

// operationOf is the finance operation a subcommand calls.
func operationOf(command *cli.Command) string {
	operation, _ := command.Metadata[financeOperationKey].(string)
	return operation
}

// --- helpers ------------------------------------------------------------

// financeCall runs one finance operation as the signed-in person.
func financeCall(ctx context.Context, command *cli.Command, operation string, variables map[string]any, result any) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	return describeError(command, client.RunFinance(ctx, connection, operation, variables, result))
}

// financeArgument is the argument at an index, or an error saying what is
// missing.
func financeArgument(command *cli.Command, index int, what string) (string, error) {
	value := strings.TrimSpace(command.Args().Get(index))
	if value == "" {
		return "", usage("give " + what)
	}
	return value, nil
}

// setString puts a flag's value into the variables when it was given.
func setString(command *cli.Command, variables map[string]any, flag, key string) {
	if command.IsSet(flag) {
		variables[key] = strings.TrimSpace(command.String(flag))
	}
}

// setBool puts a boolean flag into the variables when it was given.
func setBool(command *cli.Command, variables map[string]any, flag, key string) {
	if command.IsSet(flag) {
		variables[key] = command.Bool(flag)
	}
}

// rangeOf reads --from, --to, --since and --month into days, "2006-01-02".
// A month is the shorthand the finance tool takes too, so a range asked
// for either way means the same days.
func rangeOf(command *cli.Command) (string, string, error) {
	from, to := strings.TrimSpace(command.String("from")), strings.TrimSpace(command.String("to"))
	since := strings.TrimSpace(command.String("since"))
	if month := strings.TrimSpace(command.String("month")); month != "" {
		if from != "" || to != "" || since != "" {
			return "", "", usage("give --month, or --from, --to and --since, not both")
		}
		return monthDays(month)
	}
	if since == "" {
		return from, to, nil
	}
	if from != "" {
		return "", "", usage("give --from or --since, not both")
	}
	day, err := dayBack(since, time.Now())
	if err != nil {
		return "", "", err
	}
	return day, to, nil
}

// monthDays is the first and last day of a month given as "2006-01".
func monthDays(month string) (string, string, error) {
	firstDay, err := time.Parse("2006-01", month)
	if err != nil {
		return "", "", usage(fmt.Sprintf("--month %q is not a month; give it as 2026-09", month))
	}
	return firstDay.Format(time.DateOnly), firstDay.AddDate(0, 1, -1).Format(time.DateOnly), nil
}

// dayBack is the day a span such as 30d, 12w, 6m or 1y reaches back from
// now. Anything else is refused, a day included: taken as a span, a day
// once gave a range of one day with nothing said.
func dayBack(span string, now time.Time) (string, error) {
	if _, err := time.Parse(time.DateOnly, span); err == nil {
		return "", usage(fmt.Sprintf("--since takes a span back from today, as 30d, 12w, 6m or 1y; for a range from the day %s give --from %s", span, span))
	}
	if len(span) >= 2 {
		count, err := strconv.Atoi(span[:len(span)-1])
		if err == nil && count >= 0 {
			switch span[len(span)-1] {
			case 'd':
				return now.AddDate(0, 0, -count).Format(time.DateOnly), nil
			case 'w':
				return now.AddDate(0, 0, -7*count).Format(time.DateOnly), nil
			case 'm':
				return now.AddDate(0, -count, 0).Format(time.DateOnly), nil
			case 'y':
				return now.AddDate(-count, 0, 0).Format(time.DateOnly), nil
			}
		}
	}
	return "", usage(fmt.Sprintf("--since %q is not a span; give days, weeks, months or years back from today, as 30d, 12w, 6m or 1y", span))
}

// currencyMinorUnits is how many decimal places a currency is written
// with, where ISO 4217 says other than two.
var currencyMinorUnits = map[string]int{
	"BIF": 0, "CLP": 0, "DJF": 0, "GNF": 0, "ISK": 0, "JPY": 0, "KMF": 0, "KRW": 0, "PYG": 0,
	"RWF": 0, "UGX": 0, "UYI": 0, "VND": 0, "VUV": 0, "XAF": 0, "XOF": 0, "XPF": 0,
	"BHD": 3, "IQD": 3, "JOD": 3, "KWD": 3, "LYD": 3, "OMR": 3, "TND": 3,
}

// money is an amount with its currency, as a person reads it: rounded to
// the places the currency is written with, two for most and none for the
// yen. A converted total carries more places than that, which read as
// noise in a column; --json keeps them all.
func money(amount, currencyCode string) string {
	if amount == "" {
		return ""
	}
	places, isListed := currencyMinorUnits[strings.ToUpper(currencyCode)]
	if !isListed {
		places = 2
	}
	written := amount
	if value, isNumber := new(big.Rat).SetString(amount); isNumber {
		// Halves round away from zero, as the amounts are stored.
		written = value.FloatString(places)
		if strings.Trim(written, "-0.") == "" {
			written = strings.TrimPrefix(written, "-")
		}
	}
	if currencyCode == "" {
		return written
	}
	return written + " " + currencyCode
}

// decimal is a stored decimal without the zeros its column pads it with:
// a quantity of 3.00000000 reads as 3.
func decimal(amount string) string {
	if !strings.Contains(amount, ".") {
		return amount
	}
	if _, isNumber := new(big.Rat).SetString(amount); !isNumber {
		return amount
	}
	return strings.TrimSuffix(strings.TrimRight(amount, "0"), ".")
}

// unitPrice is the price of one unit with its currency: at least the
// places the currency is written with, and more where the price has them,
// since a fund or a coin is priced finer than a cent.
func unitPrice(amount, currencyCode string) string {
	if amount == "" {
		return ""
	}
	written := decimal(amount)
	places, isListed := currencyMinorUnits[strings.ToUpper(currencyCode)]
	if !isListed {
		places = 2
	}
	if _, isNumber := new(big.Rat).SetString(written); isNumber {
		wholePart, fractionPart, _ := strings.Cut(written, ".")
		if len(fractionPart) < places {
			fractionPart += strings.Repeat("0", places-len(fractionPart))
		}
		written = wholePart
		if fractionPart != "" {
			written += "." + fractionPart
		}
	}
	if currencyCode == "" {
		return written
	}
	return written + " " + currencyCode
}

// securityLabel is how a security reads in a column: its ticker symbol,
// else its name; empty for none.
func securityLabel(security *client.FinanceSecurity) string {
	if security == nil {
		return ""
	}
	if security.TickerSymbol != "" {
		return security.TickerSymbol
	}
	return truncate(security.SecurityName, 32)
}

// dayOf is a time as the day it fell on here, or "never".
func dayOf(value *time.Time) string {
	if value == nil || value.IsZero() {
		return "never"
	}
	return value.Local().Format("2006-01-02 15:04")
}

// spendingCategoryNamed finds a spending category by id or, ignoring case,
// by name. Transfer is the transfer category whatever it is called, ahead
// of a person's own spending category named transfer, which the built-in
// one was named around.
func spendingCategoryNamed(ctx context.Context, command *cli.Command, wanted string) (*client.SpendingCategory, error) {
	var spendingCategories []*client.SpendingCategory
	if err := financeCall(ctx, command, "SpendingCategories", nil, &spendingCategories); err != nil {
		return nil, err
	}
	for _, spendingCategory := range spendingCategories {
		if spendingCategory.ID == wanted {
			return spendingCategory, nil
		}
	}
	if strings.EqualFold(wanted, "transfer") {
		for _, spendingCategory := range spendingCategories {
			if spendingCategory.IsTransfer {
				return spendingCategory, nil
			}
		}
	}
	for _, spendingCategory := range spendingCategories {
		if strings.EqualFold(spendingCategory.SpendingCategoryName, wanted) {
			return spendingCategory, nil
		}
	}
	return nil, usage(fmt.Sprintf("there is no spending category %q; teanode finance spending-categories lists them", wanted))
}

// spendingCategoryNames is every spending category's name by its id.
func spendingCategoryNames(ctx context.Context, command *cli.Command) (map[string]string, error) {
	var spendingCategories []*client.SpendingCategory
	if err := financeCall(ctx, command, "SpendingCategories", nil, &spendingCategories); err != nil {
		return nil, err
	}
	names := map[string]string{}
	for _, spendingCategory := range spendingCategories {
		names[spendingCategory.ID] = spendingCategory.SpendingCategoryName
	}
	return names, nil
}

// printDone says what was done, or prints the result as JSON.
func printDone(command *cli.Command, result any, line string) error {
	if command.Bool("json") {
		return PrintJSON(result)
	}
	_, _ = fmt.Fprintln(command.Writer, line)
	return nil
}

// --- providers, linking and sources ---------------------------------------

func runFinanceProviders(ctx context.Context, command *cli.Command) error {
	var providers []*client.FinanceProvider
	if err := financeCall(ctx, command, operationOf(command), nil, &providers); err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(providers)
	}
	if len(providers) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "this server offers no provider to link an institution through; ask the operator")
		return nil
	}
	rows := make([][]string, 0, len(providers))
	for _, provider := range providers {
		how := "teanode finance link-simplefin, with a setup token from the SimpleFIN Bridge"
		if provider.IsBrowserRequired {
			how = "teanode finance link-plaid, in a browser"
		}
		rows = append(rows, []string{provider.ProviderKind, how})
	}
	return printTable([]string{"provider", "how to link"}, rows)
}

// financeLinkAddress is the dashboard's linking page on the server this
// command talks to.
func financeLinkAddress(command *cli.Command, sourceId string) (string, error) {
	connection, err := openClient(command)
	if err != nil {
		return "", err
	}
	address := strings.TrimSuffix(connection.URL(), "/") + client.FinanceLinkPagePath
	if sourceId != "" {
		address += "?source=" + sourceId
	}
	return address, nil
}

// listFinanceSources is the person's finance sources.
func listFinanceSources(ctx context.Context, command *cli.Command) ([]*client.FinanceSource, error) {
	var sources []*client.FinanceSource
	err := financeCall(ctx, command, "FinanceSources", nil, &sources)
	return sources, err
}

// waitForFinanceSource looks at the finance sources until one passes the
// check, or the timeout passes.
func waitForFinanceSource(ctx context.Context, command *cli.Command, check func(*client.FinanceSource) bool) (*client.FinanceSource, error) {
	deadline := time.Now().Add(command.Duration("timeout"))
	for {
		sources, err := listFinanceSources(ctx, command)
		if err != nil {
			return nil, err
		}
		for _, source := range sources {
			if check(source) {
				return source, nil
			}
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("gave up waiting after %s; teanode finance sources shows whether it finished", command.Duration("timeout"))
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(financeWaitPoll):
		}
	}
}

func runFinanceLinkPlaid(ctx context.Context, command *cli.Command) error {
	before, err := listFinanceSources(ctx, command)
	if err != nil {
		return err
	}
	address, err := financeLinkAddress(command, "")
	if err != nil {
		return err
	}
	if command.Bool("no-wait") {
		return printDone(command, map[string]string{"linkAddress": address}, "Open this address, signed in to the dashboard, to link an institution through Plaid:\n  "+address)
	}
	fmt.Fprintf(os.Stderr, "Open this address, signed in to the dashboard, to link an institution through Plaid:\n  %s\nWaiting for the new finance source (Ctrl-C to stop waiting)...\n", address)
	isKnown := map[string]bool{}
	for _, source := range before {
		isKnown[source.ID] = true
	}
	// A SimpleFIN link made meanwhile is not the one being waited for.
	linked, err := waitForFinanceSource(ctx, command, func(source *client.FinanceSource) bool {
		return !isKnown[source.ID] && source.ProviderKind == "plaid"
	})
	if err != nil {
		return err
	}
	return printFinanceSources(command, []*client.FinanceSource{linked})
}

func runFinanceRepair(ctx context.Context, command *cli.Command) error {
	sourceId, err := financeArgument(command, 0, "the finance source's id; teanode finance sources lists them")
	if err != nil {
		return err
	}
	address, err := financeLinkAddress(command, sourceId)
	if err != nil {
		return err
	}
	if command.Bool("no-wait") {
		return printDone(command, map[string]string{"linkAddress": address}, "Open this address, signed in to the dashboard, to sign in to the institution again:\n  "+address)
	}
	fmt.Fprintf(os.Stderr, "Open this address, signed in to the dashboard, to sign in to the institution again:\n  %s\nWaiting for the finance source to be repaired (Ctrl-C to stop waiting)...\n", address)
	// Done when the sign-in flag is seen cleared after being seen set, or,
	// for a source that was not waiting for a sign-in, when it next syncs
	// without asking for one. The first look is taken before the person
	// has done anything, so an unflagged source there proves nothing.
	startedAt := time.Now()
	isFlagSeen := false
	repaired, err := waitForFinanceSource(ctx, command, func(source *client.FinanceSource) bool {
		if source.ID != sourceId {
			return false
		}
		if source.IsSignInRequired {
			isFlagSeen = true
			return false
		}
		if isFlagSeen {
			return true
		}
		return source.LastRunAt != nil && source.LastRunAt.After(startedAt) && source.LastError == ""
	})
	if err != nil {
		return err
	}
	return printFinanceSources(command, []*client.FinanceSource{repaired})
}

func runFinanceLinkSimpleFin(ctx context.Context, command *cli.Command) error {
	setupToken := strings.TrimSpace(command.Args().First())
	if setupToken == "" || setupToken == "-" {
		typed, err := ReadSecret("setup token: ")
		if err != nil {
			return err
		}
		setupToken = typed
	}
	if setupToken == "" {
		return usage("give the setup token from the SimpleFIN Bridge")
	}
	var linked *client.FinanceSource
	if err := financeCall(ctx, command, operationOf(command), map[string]any{"setupToken": setupToken}, &linked); err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(linked)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s: linked through SimpleFIN; its first sync starts within the minute\n", linked.ID)
	return nil
}

// financeCredentialFileByteLimit is the most read from a credential file:
// a credential is a line, and a file far larger is the wrong file.
const financeCredentialFileByteLimit = 64 << 10

// looksLikeCredential says an argument has the shape of a Plaid credential
// or a SimpleFIN one rather than of a file name.
func looksLikeCredential(argument string) bool {
	lowered := strings.ToLower(argument)
	return strings.HasPrefix(lowered, "access-") || strings.Contains(lowered, "://") || strings.Contains(lowered, "@")
}

// readFinanceCredential reads the credential import-credential brings in:
// from the file the argument names, or from standard input or a prompt
// without echoing when there is no argument or it is -. An argument that
// is itself a credential is refused, and a file that cannot be read is
// reported without repeating the argument, which may be a credential of
// a shape looksLikeCredential does not know.
func readFinanceCredential(command *cli.Command) (string, error) {
	if command.Args().Len() > 1 {
		return "", usage("give at most one argument: - or the file holding the credential")
	}
	argument := strings.TrimSpace(command.Args().First())
	if argument == "" || argument == "-" {
		return ReadSecret("credential: ")
	}
	if looksLikeCredential(argument) {
		return "", usage("the credential is not taken on the command line, where it would stay in the shell's history; " +
			"give a file holding it, or - to paste it without echoing")
	}
	file, err := os.Open(argument)
	if err != nil {
		return "", usage("the argument is neither - nor a file that can be read; give a file holding the credential, or - to paste it without echoing")
	}
	defer func() { _ = file.Close() }()
	content, err := io.ReadAll(io.LimitReader(file, financeCredentialFileByteLimit+1))
	if err != nil {
		return "", fmt.Errorf("cannot read the credential file: %w", err)
	}
	if len(content) > financeCredentialFileByteLimit {
		return "", usage("the credential file is far larger than a credential; give the file holding only the credential")
	}
	return strings.TrimSpace(string(content)), nil
}

func runFinanceImportCredential(ctx context.Context, command *cli.Command) error {
	providerKind := strings.ToLower(strings.TrimSpace(command.String("provider")))
	if providerKind != "plaid" && providerKind != "simplefin" {
		return usage("give --provider plaid or --provider simplefin")
	}
	credential, err := readFinanceCredential(command)
	if err != nil {
		return err
	}
	if credential == "" {
		return usage("give the credential: for Plaid its access token, for SimpleFIN its access URL")
	}
	variables := map[string]any{"providerKind": providerKind, "credential": credential}
	setString(command, variables, "institution-name", "institutionName")
	var imported *client.FinanceSource
	if err := financeCall(ctx, command, operationOf(command), variables, &imported); err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(imported)
	}
	name := imported.InstitutionName
	if name == "" {
		name = imported.Name
	}
	_, _ = fmt.Fprintf(command.Writer, "%s: %s, brought in through %s; its first sync starts within the minute\n", imported.ID, name, imported.ProviderKind)
	return nil
}

// --- imported statements ------------------------------------------------

func runFinanceImportStatement(ctx context.Context, command *cli.Command) error {
	path, err := financeArgument(command, 0, "the OFX file: teanode finance import-statement transactions.ofx")
	if err != nil {
		return err
	}
	var content []byte
	fileName := filepath.Base(path)
	if path == "-" {
		fileName = "statement.ofx"
		content, err = io.ReadAll(io.LimitReader(os.Stdin, ofx.MaximumFileBytes+1))
	} else {
		content, err = readLimitedFile(path, ofx.MaximumFileBytes+1)
	}
	if err != nil {
		return err
	}
	if len(content) > ofx.MaximumFileBytes {
		return usage(fmt.Sprintf("%s is larger than %d MB, more than any statement", fileName, ofx.MaximumFileBytes/(1024*1024)))
	}
	if !ofx.IsOFX(content) {
		return usage(fmt.Sprintf("%s is not an OFX file; export the transactions as OFX, QFX or QBO", fileName))
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	// Up the way a file for the agent goes, then named by id, as the
	// dashboard does it: bytes are not a GraphQL argument.
	attachment, err := client.UploadAgentAttachment(ctx, connection, fileName, content)
	if err != nil {
		return describeError(command, err)
	}
	var imported *client.FinanceStatementImport
	if err := describeError(command, client.RunFinance(ctx, connection, operationOf(command), map[string]any{"agentAttachmentId": attachment.ID}, &imported)); err != nil {
		return err
	}
	return printDone(command, imported, statementImportLine(imported))
}

// readLimitedFile reads at most limit bytes of a file.
func readLimitedFile(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	return io.ReadAll(io.LimitReader(file, limit))
}

// statementImportLine says what an import did.
func statementImportLine(imported *client.FinanceStatementImport) string {
	if imported == nil {
		return "nothing was imported"
	}
	if len(imported.FinanceAccountNames) == 0 {
		return "nothing was imported: " + imported.ImportErrorMessage
	}
	line := fmt.Sprintf("into %s: %d added, %d updated, %d already here", strings.Join(imported.FinanceAccountNames, ", "),
		imported.AddedTransactionCount, imported.UpdatedTransactionCount, imported.UnchangedTransactionCount)
	if imported.SkippedTransactionCount > 0 {
		line += fmt.Sprintf(", %d skipped", imported.SkippedTransactionCount)
	}
	if imported.TransactionWithoutFITIDCount > 0 {
		line += fmt.Sprintf("; %d had no FITID and are known by their day, amount and name", imported.TransactionWithoutFITIDCount)
	}
	if imported.ImportErrorMessage != "" {
		line += "; not imported: " + imported.ImportErrorMessage
	}
	return line
}

func runFinanceStatementImport(ctx context.Context, command *cli.Command) error {
	var statementImport *client.StatementImport
	if err := financeCall(ctx, command, operationOf(command), nil, &statementImport); err != nil {
		return err
	}
	return printStatementImport(command, statementImport)
}

func printStatementImport(command *cli.Command, statementImport *client.StatementImport) error {
	if command.Bool("json") {
		return PrintJSON(statementImport)
	}
	address := statementImport.ImportAddress
	if address == "" {
		address = "none: you have no mailbox address to receive statements; teanode finance import-statement takes a file"
	}
	_, _ = fmt.Fprintf(command.Writer, "import address: %s\n", address)
	if !statementImport.IsEnabled {
		_, _ = fmt.Fprintf(command.Writer, "importing is off: teanode finance enable-source %s switches it on\n", statementImport.SourceID)
	}
	if last := statementImport.LastStatementImport; last != nil {
		_, _ = fmt.Fprintf(command.Writer, "last import: %s, %s\n", last.ImportedAt.Local().Format("2006-01-02 15:04"), statementImportLine(last))
	} else {
		_, _ = fmt.Fprintln(command.Writer, "nothing imported yet")
	}
	return nil
}

func runFinanceRegenerateStatementImportAddress(ctx context.Context, command *cli.Command) error {
	if err := confirm(command, "Give the statement import address a new token? Mail to the old address will be refused."); err != nil {
		return err
	}
	var statementImport *client.StatementImport
	if err := financeCall(ctx, command, operationOf(command), nil, &statementImport); err != nil {
		return err
	}
	return printStatementImport(command, statementImport)
}

func runFinanceSources(ctx context.Context, command *cli.Command) error {
	sources, err := listFinanceSources(ctx, command)
	if err != nil {
		return err
	}
	return printFinanceSources(command, sources)
}

func printFinanceSources(command *cli.Command, sources []*client.FinanceSource) error {
	if command.Bool("json") {
		return PrintJSON(sources)
	}
	if len(sources) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "no finance sources yet; teanode finance providers says how to link one")
		return nil
	}
	rows := [][]string{}
	for _, source := range sources {
		state := "on"
		switch {
		case source.IsSignInRequired:
			state = "sign in again: teanode finance repair " + source.ID
		case source.LastError != "":
			state = "failed: " + truncate(source.LastError, 60)
		case !source.IsEnabled:
			state = "off"
		}
		institution := source.InstitutionName
		if institution == "" {
			institution = source.Name
		}
		rows = append(rows, []string{source.ID, institution, source.ProviderKind, dayOf(source.LastRunAt), state, strconv.Itoa(len(source.FinanceAccounts))})
	}
	return printTable([]string{"id", "institution", "provider", "last sync", "state", "accounts"}, rows)
}

// financeSourceArgument is the finance source the first argument names,
// looked up among the person's finance sources: the operations every
// source has act on any source, and teanode finance acts on finance
// sources only.
func financeSourceArgument(ctx context.Context, command *cli.Command) (*client.FinanceSource, error) {
	sourceId, err := financeArgument(command, 0, "the finance source's id; teanode finance sources lists them")
	if err != nil {
		return nil, err
	}
	sources, err := listFinanceSources(ctx, command)
	if err != nil {
		return nil, err
	}
	for _, source := range sources {
		if source.ID == sourceId {
			return source, nil
		}
	}
	return nil, usage(fmt.Sprintf("there is no finance source %q; teanode finance sources lists them", sourceId))
}

func runFinanceSync(ctx context.Context, command *cli.Command) error {
	source, err := financeSourceArgument(ctx, command)
	if err != nil {
		return err
	}
	sourceId := source.ID
	// Syncing would switch it on again, which is enable-source's to do.
	if !source.IsEnabled {
		return usage(fmt.Sprintf("finance source %s is switched off; teanode finance enable-source %s switches it on and syncs it", sourceId, sourceId))
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := client.SyncAgentKnowledgeSource(ctx, connection, sourceId); err != nil {
		return describeError(command, err)
	}
	return printDone(command, map[string]any{"sourceId": sourceId, "isSyncing": true}, sourceId+": syncing; it starts within the minute")
}

func runFinanceDisableSource(ctx context.Context, command *cli.Command) error {
	return switchFinanceSource(ctx, command, false)
}

func runFinanceEnableSource(ctx context.Context, command *cli.Command) error {
	return switchFinanceSource(ctx, command, true)
}

func switchFinanceSource(ctx context.Context, command *cli.Command, isEnabled bool) error {
	source, err := financeSourceArgument(ctx, command)
	if err != nil {
		return err
	}
	sourceId := source.ID
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if _, err := client.SaveAgentKnowledgeSource(ctx, connection, map[string]any{"sourceId": sourceId, "enabled": isEnabled}); err != nil {
		return describeError(command, err)
	}
	result := map[string]any{"sourceId": sourceId, "isEnabled": isEnabled}
	if isEnabled {
		return printDone(command, result, sourceId+": on, and syncing within the minute")
	}
	return printDone(command, result, sourceId+": off; what it holds is kept")
}

func runFinanceDeleteSource(ctx context.Context, command *cli.Command) error {
	source, err := financeSourceArgument(ctx, command)
	if err != nil {
		return err
	}
	sourceId := source.ID
	name := source.InstitutionName
	if name == "" {
		name = source.Name
	}
	if err := confirm(command, "Delete finance source "+sourceId+" ("+forTerminal(name)+")? Its accounts and transactions are deleted, it is ended at its provider, and some Plaid plans count a deleted link against their limit."); err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := client.DeleteAgentKnowledgeSource(ctx, connection, sourceId); err != nil {
		return describeError(command, err)
	}
	return printDone(command, map[string]any{"sourceId": sourceId, "isDeleted": true}, sourceId+": deleted")
}

// --- accounts, transactions and totals -----------------------------------

func runFinanceAccounts(ctx context.Context, command *cli.Command) error {
	variables := map[string]any{}
	setString(command, variables, "currency", "currencyCode")
	var accounts []*client.FinanceAccount
	if err := financeCall(ctx, command, operationOf(command), variables, &accounts); err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(accounts)
	}
	if len(accounts) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "no finance accounts yet")
		return nil
	}
	rows := make([][]string, 0, len(accounts))
	for _, account := range accounts {
		name := account.AccountName
		if account.AccountMask != "" {
			name += " ••" + account.AccountMask
		}
		rows = append(rows, []string{
			account.ID, account.InstitutionName, name, account.AccountKind, money(account.CurrentBalance, account.CurrencyCode),
			money(account.ConvertedCurrentBalance, account.ReportingCurrencyCode), dayOf(account.BalanceAt),
		})
	}
	return printTable([]string{"id", "institution", "account", "kind", "balance", "converted", "as of"}, rows)
}

func runFinanceCreditUsage(ctx context.Context, command *cli.Command) error {
	variables := map[string]any{}
	setString(command, variables, "currency", "currencyCode")
	var usage *client.CreditUsage
	if err := financeCall(ctx, command, operationOf(command), variables, &usage); err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(usage)
	}
	if len(usage.CreditCards) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "no credit cards among your finance accounts")
		return nil
	}
	currencyCode := usage.ReportingCurrencyCode
	if usage.UsageShare != nil {
		_, _ = fmt.Fprintf(command.Writer, "%s owed of %s, %s used\n", money(usage.TotalOwedAmount, currencyCode),
			money(usage.TotalCreditLimitAmount, currencyCode), usageShareWords(usage.UsageShare))
	}
	rows := make([][]string, 0, len(usage.CreditCards))
	for _, card := range usage.CreditCards {
		name := card.AccountName
		if card.AccountMask != "" {
			name += " ••" + card.AccountMask
		}
		rows = append(rows, []string{
			card.FinanceAccountID, card.InstitutionName, name, money(card.OwedAmount, card.CurrencyCode),
			money(card.CreditLimitAmount, card.CurrencyCode), card.CreditLimitSource, usageShareWords(card.UsageShare),
		})
	}
	if err := printTable([]string{"id", "institution", "card", "owed", "limit", "limit from", "used"}, rows); err != nil {
		return err
	}
	if usage.LeftOutCardCount > 0 {
		_, _ = fmt.Fprintf(command.Writer, "left out of the total, with no known limit or balance: %d card(s) owing %s\n",
			usage.LeftOutCardCount, money(usage.LeftOutOwedAmount, currencyCode))
	}
	if len(usage.UnconvertedCurrencyCodes) > 0 {
		_, _ = fmt.Fprintf(command.Writer, "left out, with no exchange rate into %s: %s\n", currencyCode, strings.Join(usage.UnconvertedCurrencyCodes, ", "))
	}
	return nil
}

// usageShareWords is a share of a credit limit as a percent, "34%";
// empty when it is not known.
func usageShareWords(usageShare *float64) string {
	if usageShare == nil {
		return ""
	}
	return strconv.FormatFloat(*usageShare*100, 'f', 0, 64) + "%"
}

func runFinanceTransactions(ctx context.Context, command *cli.Command) error {
	from, to, err := rangeOf(command)
	if err != nil {
		return err
	}
	variables := map[string]any{"from": from, "to": to, "limit": int(command.Int("limit"))}
	setString(command, variables, "finance-account", "financeAccountId")
	setString(command, variables, "text", "text")
	setString(command, variables, "minimum-amount", "minimumAmount")
	setString(command, variables, "maximum-amount", "maximumAmount")
	setString(command, variables, "provider-category", "providerCategory")
	setString(command, variables, "after", "after")
	setBool(command, variables, "is-uncategorized", "isUncategorized")
	setString(command, variables, "duplicate-of", "duplicateOfTransactionId")
	if wanted := strings.TrimSpace(command.String("spending-category")); wanted != "" {
		spendingCategory, err := spendingCategoryNamed(ctx, command, wanted)
		if err != nil {
			return err
		}
		variables["spendingCategoryId"] = spendingCategory.ID
	}
	var page *client.FinanceTransactionPage
	if err := financeCall(ctx, command, operationOf(command), variables, &page); err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(page)
	}
	if len(page.FinanceTransactions) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "no finance transactions match")
		return nil
	}
	names, err := spendingCategoryNames(ctx, command)
	if err != nil {
		return err
	}
	rows, duplicateCount := financeTransactionRows(page.FinanceTransactions, names)
	if err := printTable([]string{"posted", "amount", "merchant", "spending category", "id"}, rows); err != nil {
		return err
	}
	if duplicateCount > 0 {
		fmt.Fprintf(os.Stderr, "note: %d are mirrored copies, the same charge reported again on another account, left out of every total; "+
			"teanode finance count-transaction <id> counts one that is real\n", duplicateCount)
	}
	if page.NextCursor != "" {
		fmt.Fprintf(os.Stderr, "note: there are more; add --after %s for the next page\n", page.NextCursor)
	}
	return nil
}

// financeTransactionRows is the transactions table's rows, and how many
// of them are mirrored copies, each marked with the copy that counts.
func financeTransactionRows(financeTransactions []*client.FinanceTransaction, names map[string]string) ([][]string, int) {
	rows := make([][]string, 0, len(financeTransactions))
	duplicateCount := 0
	for _, financeTransaction := range financeTransactions {
		what := financeTransaction.MerchantName
		if what == "" {
			what = financeTransaction.Description
		}
		if financeTransaction.IsPending {
			what += " (pending)"
		}
		what = truncate(what, 48)
		// After the cut, so the mark is never what is cut off.
		if financeTransaction.DuplicateOfTransactionID != "" {
			what += " (duplicate of " + financeTransaction.DuplicateOfTransactionID + ", not counted)"
			duplicateCount++
		}
		rows = append(rows, []string{
			financeTransaction.PostedOn, money(financeTransaction.Amount, financeTransaction.CurrencyCode), what,
			names[financeTransaction.SpendingCategoryID], financeTransaction.ID,
		})
	}
	return rows, duplicateCount
}

func runFinanceTrades(ctx context.Context, command *cli.Command) error {
	from, to, err := rangeOf(command)
	if err != nil {
		return err
	}
	variables := map[string]any{"from": from, "to": to, "limit": int(command.Int("limit"))}
	setString(command, variables, "finance-account", "financeAccountId")
	setString(command, variables, "finance-security", "financeSecurityId")
	setString(command, variables, "after", "after")
	var page *client.FinanceTradePage
	if err := financeCall(ctx, command, operationOf(command), variables, &page); err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(page)
	}
	if len(page.FinanceTrades) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "no trades match")
		return nil
	}
	rows := make([][]string, 0, len(page.FinanceTrades))
	for _, trade := range page.FinanceTrades {
		tradeKind := trade.TradeKind
		if trade.TradeSubkind != "" && trade.TradeSubkind != trade.TradeKind {
			tradeKind += " (" + trade.TradeSubkind + ")"
		}
		rows = append(rows, []string{
			trade.TradedOn, tradeKind, securityLabel(trade.FinanceSecurity), decimal(trade.TradedQuantity),
			unitPrice(trade.UnitPrice, trade.CurrencyCode), money(trade.TradeAmount, trade.CurrencyCode),
			money(trade.FeeAmount, trade.CurrencyCode), truncate(trade.Description, 48), trade.ID,
		})
	}
	if err := printTable([]string{"traded", "kind", "security", "quantity", "unit price", "amount", "fee", "description", "id"}, rows); err != nil {
		return err
	}
	if page.NextCursor != "" {
		fmt.Fprintf(os.Stderr, "note: there are more; add --after %s for the next page\n", page.NextCursor)
	}
	return nil
}

func runFinanceSpendingSummary(ctx context.Context, command *cli.Command) error {
	from, to, err := rangeOf(command)
	if err != nil {
		return err
	}
	variables := map[string]any{"from": from, "to": to}
	setString(command, variables, "group-by", "groupBy")
	setString(command, variables, "finance-account", "financeAccountId")
	setString(command, variables, "currency", "currencyCode")
	var summary *client.FinanceSpendingSummary
	if err := financeCall(ctx, command, operationOf(command), variables, &summary); err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(summary)
	}
	if len(summary.SpendingSummaryRows) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "no finance transactions in that range")
		return nil
	}
	label := func(key, label string) string {
		if label != "" {
			return label
		}
		if key == "" {
			return "(none)"
		}
		return key
	}
	rows := [][]string{}
	if summary.ReportingCurrencyCode != "" {
		for _, row := range summary.ConvertedSpendingSummaryRows {
			rows = append(rows, []string{label(row.GroupKey, row.GroupLabel), money(row.MoneyOut, summary.ReportingCurrencyCode), money(row.MoneyIn, summary.ReportingCurrencyCode)})
		}
		rows = append(rows, []string{"total", money(summary.ConvertedMoneyOut, summary.ReportingCurrencyCode), money(summary.ConvertedMoneyIn, summary.ReportingCurrencyCode)})
	} else {
		for _, row := range summary.SpendingSummaryRows {
			rows = append(rows, []string{label(row.GroupKey, row.GroupLabel), money(row.MoneyOut, row.CurrencyCode), money(row.MoneyIn, row.CurrencyCode)})
		}
	}
	if err := printTable([]string{groupByHeader(summary.GroupBy), "money out", "money in"}, rows); err != nil {
		return err
	}
	if len(summary.CurrencyTotals) > 1 {
		for _, total := range summary.CurrencyTotals {
			fmt.Fprintf(os.Stderr, "in %s: %s out, %s in\n", total.CurrencyCode, money(total.MoneyOut, total.CurrencyCode), money(total.MoneyIn, total.CurrencyCode))
		}
	}
	if len(summary.UnconvertedCurrencyCodes) > 0 {
		fmt.Fprintf(os.Stderr, "note: left out of the %s totals for want of an exchange rate: %s\n", summary.ReportingCurrencyCode, strings.Join(summary.UnconvertedCurrencyCodes, ", "))
	}
	return nil
}

// groupByHeader is a spending summary's grouping as a column's header:
// spendingCategory is "spending category".
func groupByHeader(groupBy string) string {
	var words []string
	start := 0
	for index, letter := range groupBy {
		if unicode.IsUpper(letter) {
			words = append(words, strings.ToLower(groupBy[start:index]))
			start = index
		}
	}
	return strings.Join(append(words, strings.ToLower(groupBy[start:])), " ")
}

func runFinanceExchangeRate(ctx context.Context, command *cli.Command) error {
	fromCurrencyCode, err := financeArgument(command, 0, "the two currencies: teanode finance exchange-rate USD JPY")
	if err != nil {
		return err
	}
	toCurrencyCode, err := financeArgument(command, 1, "the currency to convert into")
	if err != nil {
		return err
	}
	variables := map[string]any{"fromCurrencyCode": fromCurrencyCode, "toCurrencyCode": toCurrencyCode}
	setString(command, variables, "on", "rateOn")
	var rate *client.CurrencyPairRate
	if err := financeCall(ctx, command, operationOf(command), variables, &rate); err != nil {
		return err
	}
	return printDone(command, rate, fmt.Sprintf("1 %s = %s %s on %s (%s)", rate.FromCurrencyCode, rate.Rate, rate.ToCurrencyCode, rate.RateOn, rate.RateSource))
}

func runFinanceConvertCurrency(ctx context.Context, command *cli.Command) error {
	amount, err := financeArgument(command, 0, "an amount and two currencies: teanode finance convert-currency 100 USD JPY")
	if err != nil {
		return err
	}
	fromCurrencyCode, err := financeArgument(command, 1, "the amount's currency")
	if err != nil {
		return err
	}
	toCurrencyCode, err := financeArgument(command, 2, "the currency to convert into")
	if err != nil {
		return err
	}
	variables := map[string]any{"amount": amount, "fromCurrencyCode": fromCurrencyCode, "toCurrencyCode": toCurrencyCode}
	setString(command, variables, "on", "rateOn")
	var conversion *client.CurrencyConversion
	if err := financeCall(ctx, command, operationOf(command), variables, &conversion); err != nil {
		return err
	}
	return printDone(command, conversion, fmt.Sprintf("%s = %s at %s on %s (%s)",
		money(conversion.Amount, conversion.FromCurrencyCode), money(conversion.ConvertedAmount, conversion.ToCurrencyCode),
		conversion.Rate, conversion.RateOn, conversion.RateSource))
}

func runFinanceReportingCurrency(ctx context.Context, command *cli.Command) error {
	var reporting *client.ReportingCurrency
	if err := financeCall(ctx, command, operationOf(command), nil, &reporting); err != nil {
		return err
	}
	switch {
	case reporting.ReportingCurrencyCode == "":
		return printDone(command, reporting, "no reporting currency yet: you chose none, and there is no finance account or asset to take one from")
	case reporting.IsChosen:
		return printDone(command, reporting, "totals are shown in "+reporting.ReportingCurrencyCode+", which you chose")
	}
	return printDone(command, reporting, "totals are shown in "+reporting.ReportingCurrencyCode+", the currency of your first finance account or asset; teanode finance set-reporting-currency chooses another")
}

func runFinanceSetReportingCurrency(ctx context.Context, command *cli.Command) error {
	currencyCode := strings.TrimSpace(command.Args().First())
	if command.Bool("clear") {
		currencyCode = ""
	} else if currencyCode == "" {
		return usage("give a currency code like EUR, or --clear")
	}
	var saved string
	if err := financeCall(ctx, command, operationOf(command), map[string]any{"currencyCode": currencyCode}, &saved); err != nil {
		return err
	}
	if saved == "" {
		return printDone(command, map[string]string{"reportingCurrencyCode": saved}, "totals are shown in the currency of your first finance account")
	}
	return printDone(command, map[string]string{"reportingCurrencyCode": saved}, "totals are shown in "+saved)
}

// --- net worth and assets -------------------------------------------------

func runFinanceNetWorth(ctx context.Context, command *cli.Command) error {
	from, to, err := rangeOf(command)
	if err != nil {
		return err
	}
	variables := map[string]any{"from": from, "to": to}
	setString(command, variables, "currency", "currencyCode")
	var netWorth *client.NetWorth
	if err := financeCall(ctx, command, operationOf(command), variables, &netWorth); err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(netWorth)
	}
	rows := [][]string{}
	if netWorth.ReportingCurrencyCode != "" {
		for _, point := range netWorth.ConvertedNetWorthPoints {
			rows = append(rows, []string{point.NetWorthOn, money(point.NetWorthAmount, netWorth.ReportingCurrencyCode)})
		}
	} else {
		for _, point := range netWorth.NetWorthPoints {
			rows = append(rows, []string{point.NetWorthOn, money(point.NetWorthAmount, point.CurrencyCode)})
		}
	}
	if len(rows) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "nothing valued in that range yet; teanode finance create-asset adds something")
		return nil
	}
	if err := printTable([]string{"day", "net worth"}, rows); err != nil {
		return err
	}
	if len(netWorth.UnconvertedCurrencyCodes) > 0 {
		fmt.Fprintf(os.Stderr, "note: left out of the %s totals for want of an exchange rate: %s\n", netWorth.ReportingCurrencyCode, strings.Join(netWorth.UnconvertedCurrencyCodes, ", "))
	}
	return nil
}

func runFinanceAssets(ctx context.Context, command *cli.Command) error {
	variables := map[string]any{}
	for flag, variable := range map[string]string{"kind": "assetKind", "text": "text", "finance-account": "financeAccountId"} {
		if value := strings.TrimSpace(command.String(flag)); value != "" {
			variables[variable] = value
		}
	}
	if command.IsSet("is-holding") {
		variables["isHolding"] = command.Bool("is-holding")
	}
	var assets []*client.Asset
	if err := financeCall(ctx, command, operationOf(command), variables, &assets); err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(assets)
	}
	if len(assets) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "no assets yet; teanode finance create-asset adds one")
		return nil
	}
	rows := make([][]string, 0, len(assets))
	for _, asset := range assets {
		value, valuedOn := "", ""
		heldQuantity, heldUnitPrice, costBasis := "", "", ""
		if asset.LatestValuation != nil {
			amount := asset.LatestValuation.Value
			if asset.IsLiability {
				// What is owed subtracts; a card in credit adds.
				if negative, isNegative := strings.CutPrefix(amount, "-"); isNegative {
					amount = negative
				} else {
					amount = "-" + amount
				}
			}
			value, valuedOn = money(amount, asset.LatestValuation.CurrencyCode), asset.LatestValuation.ValuedOn
			heldQuantity, heldUnitPrice, costBasis = holdingColumns(asset.LatestValuation)
		}
		closed := ""
		if asset.ClosedOn != "" {
			closed = "closed " + asset.ClosedOn
		}
		rows = append(rows, []string{
			asset.ID, asset.AssetName, asset.AssetKind, securityLabel(asset.FinanceSecurity), value, heldQuantity, heldUnitPrice, costBasis,
			valuedOn, asset.ValuationSource, closed,
		})
	}
	headers := []string{"id", "asset", "kind", "security", "value", "quantity", "unit price", "cost basis", "valued on", "valuation source", ""}
	isAnyHolding := false
	for _, asset := range assets {
		isAnyHolding = isAnyHolding || asset.FinanceSecurity != nil
	}
	if !isAnyHolding {
		headers, rows = withoutColumns(headers, rows, 3, 5, 6, 7)
	}
	return printTable(headers, rows)
}

// withoutColumns drops columns by index from a table's headers and rows:
// the holding columns, for someone who holds no securities.
func withoutColumns(headers []string, rows [][]string, columnIndexes ...int) ([]string, [][]string) {
	isDropped := map[int]bool{}
	for _, columnIndex := range columnIndexes {
		isDropped[columnIndex] = true
	}
	keep := func(cells []string) []string {
		kept := make([]string, 0, len(cells))
		for cellIndex, cell := range cells {
			if !isDropped[cellIndex] {
				kept = append(kept, cell)
			}
		}
		return kept
	}
	keptRows := make([][]string, 0, len(rows))
	for _, row := range rows {
		keptRows = append(keptRows, keep(row))
	}
	return keep(headers), keptRows
}

// holdingColumns are a holding's quantity, unit price and cost basis on a
// valuation's day, as its columns read; empty for any other asset.
func holdingColumns(valuation *client.AssetValuation) (string, string, string) {
	return decimal(valuation.HeldQuantity), unitPrice(valuation.UnitPrice, valuation.CurrencyCode), money(valuation.CostBasis, valuation.CurrencyCode)
}

func runFinanceAssetHistory(ctx context.Context, command *cli.Command) error {
	assetId, err := financeArgument(command, 0, "the asset's id; teanode finance assets lists them")
	if err != nil {
		return err
	}
	var history *client.AssetHistory
	if err := financeCall(ctx, command, operationOf(command), map[string]any{"assetId": assetId}, &history); err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(history)
	}
	rows := make([][]string, 0, len(history.AssetValuations))
	for _, valuation := range history.AssetValuations {
		heldQuantity, heldUnitPrice, costBasis := holdingColumns(valuation)
		rows = append(rows, []string{
			valuation.ValuedOn, money(valuation.Value, valuation.CurrencyCode), heldQuantity, heldUnitPrice, costBasis,
			valuation.ValuationSource, truncate(valuation.ValuationNote, 60), valuation.ID,
		})
	}
	what := history.Asset.AssetKind
	if security := history.Asset.FinanceSecurity; security != nil {
		what += ", " + security.SecurityName
		if security.TickerSymbol != "" {
			what += " " + security.TickerSymbol
		}
	}
	_, _ = fmt.Fprintf(command.Writer, "%s (%s)\n", history.Asset.AssetName, forTerminal(what))
	headers := []string{"valued on", "value", "quantity", "unit price", "cost basis", "valuation source", "note", "id"}
	if history.Asset.FinanceSecurity == nil {
		headers, rows = withoutColumns(headers, rows, 2, 3, 4)
	}
	return printTable(headers, rows)
}

func runFinanceCreateAsset(ctx context.Context, command *cli.Command) error {
	name, err := financeArgument(command, 0, "the asset's name: teanode finance create-asset \"the car\" --kind vehicle --currency USD")
	if err != nil {
		return err
	}
	variables := map[string]any{"assetName": name, "assetKind": strings.TrimSpace(command.String("kind")), "currencyCode": strings.TrimSpace(command.String("currency"))}
	if variables["assetKind"] == "" || variables["currencyCode"] == "" {
		return usage("give --kind and --currency")
	}
	setString(command, variables, "valuation-source", "valuationSource")
	setString(command, variables, "estimate-description", "estimateDescription")
	setBool(command, variables, "is-estimate-allowed", "isEstimateAllowed")
	setString(command, variables, "value", "value")
	setString(command, variables, "on", "valuedOn")
	var asset *client.Asset
	if err := financeCall(ctx, command, operationOf(command), variables, &asset); err != nil {
		return err
	}
	if valuation := asset.LatestValuation; valuation != nil {
		return printDone(command, asset, fmt.Sprintf("%s: added %s, worth %s on %s", asset.ID, asset.AssetName, money(valuation.Value, valuation.CurrencyCode), valuation.ValuedOn))
	}
	return printDone(command, asset, fmt.Sprintf("%s: added %s; teanode finance record-valuation %s <value> gives it a value", asset.ID, asset.AssetName, asset.ID))
}

func runFinanceUpdateAsset(ctx context.Context, command *cli.Command) error {
	assetId, err := financeArgument(command, 0, "the asset's id; teanode finance assets lists them")
	if err != nil {
		return err
	}
	variables := map[string]any{"assetId": assetId}
	setString(command, variables, "name", "assetName")
	setString(command, variables, "kind", "assetKind")
	setString(command, variables, "currency", "currencyCode")
	setString(command, variables, "valuation-source", "valuationSource")
	setString(command, variables, "estimate-description", "estimateDescription")
	setBool(command, variables, "is-estimate-allowed", "isEstimateAllowed")
	var asset *client.Asset
	if err := financeCall(ctx, command, operationOf(command), variables, &asset); err != nil {
		return err
	}
	return printDone(command, asset, asset.ID+": changed")
}

func runFinanceCloseAsset(ctx context.Context, command *cli.Command) error {
	assetId, err := financeArgument(command, 0, "the asset's id; teanode finance assets lists them")
	if err != nil {
		return err
	}
	variables := map[string]any{"assetId": assetId}
	setString(command, variables, "on", "closedOn")
	setBool(command, variables, "reopen", "shouldReopen")
	var asset *client.Asset
	if err := financeCall(ctx, command, operationOf(command), variables, &asset); err != nil {
		return err
	}
	if asset.ClosedOn == "" {
		return printDone(command, asset, asset.ID+": open")
	}
	return printDone(command, asset, asset.ID+": closed on "+asset.ClosedOn)
}

func runFinanceDeleteAsset(ctx context.Context, command *cli.Command) error {
	assetId, err := financeArgument(command, 0, "the asset's id; teanode finance assets lists them")
	if err != nil {
		return err
	}
	if err := confirm(command, "Delete asset "+assetId+" and its whole history?"); err != nil {
		return err
	}
	if err := financeCall(ctx, command, operationOf(command), map[string]any{"assetId": assetId}, nil); err != nil {
		return err
	}
	return printDone(command, map[string]any{"assetId": assetId, "isDeleted": true}, assetId+": deleted")
}

func runFinanceRecordValuation(ctx context.Context, command *cli.Command) error {
	assetId, err := financeArgument(command, 0, "the asset's id and its value: teanode finance record-valuation <asset-id> 16500")
	if err != nil {
		return err
	}
	value, err := financeArgument(command, 1, "the value")
	if err != nil {
		return err
	}
	// What a person records here is theirs: manual, which wins over any
	// other valuation of the same day.
	variables := map[string]any{"assetId": assetId, "value": value, "valuationSource": "manual"}
	setString(command, variables, "on", "valuedOn")
	setString(command, variables, "note", "valuationNote")
	var valuation *client.AssetValuation
	if err := financeCall(ctx, command, operationOf(command), variables, &valuation); err != nil {
		return err
	}
	return printDone(command, valuation, fmt.Sprintf("%s: %s on %s", valuation.ID, money(valuation.Value, valuation.CurrencyCode), valuation.ValuedOn))
}

func runFinanceDeleteValuation(ctx context.Context, command *cli.Command) error {
	valuationId, err := financeArgument(command, 0, "the valuation's id; teanode finance asset-history lists them")
	if err != nil {
		return err
	}
	if err := confirm(command, "Delete valuation "+valuationId+"?"); err != nil {
		return err
	}
	if err := financeCall(ctx, command, operationOf(command), map[string]any{"valuationId": valuationId}, nil); err != nil {
		return err
	}
	return printDone(command, map[string]any{"valuationId": valuationId, "isDeleted": true}, valuationId+": deleted")
}

// --- spending categories and rules ---------------------------------------

func runFinanceSpendingCategories(ctx context.Context, command *cli.Command) error {
	var spendingCategories []*client.SpendingCategory
	if err := financeCall(ctx, command, operationOf(command), nil, &spendingCategories); err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(spendingCategories)
	}
	names := map[string]string{}
	for _, spendingCategory := range spendingCategories {
		names[spendingCategory.ID] = spendingCategory.SpendingCategoryName
	}
	// Every column has a header and every cell a word, so an empty parent
	// cannot make "income" read as the parent's name.
	yesOrNo := map[bool]string{true: "yes", false: "no"}
	rows := make([][]string, 0, len(spendingCategories))
	for _, spendingCategory := range spendingCategories {
		parent := names[spendingCategory.ParentSpendingCategoryID]
		if parent == "" {
			parent = "-"
		}
		rows = append(rows, []string{
			spendingCategory.ID, spendingCategory.SpendingCategoryName, parent,
			yesOrNo[spendingCategory.IsIncome], yesOrNo[spendingCategory.IsHidden], yesOrNo[spendingCategory.IsTransfer],
		})
	}
	return printTable([]string{"id", "spending category", "parent", "income", "hidden", "transfer"}, rows)
}

func runFinanceCreateSpendingCategory(ctx context.Context, command *cli.Command) error {
	name, err := financeArgument(command, 0, "the spending category's name")
	if err != nil {
		return err
	}
	variables := map[string]any{"spendingCategoryName": name}
	if parent := strings.TrimSpace(command.String("parent")); parent != "" {
		found, err := spendingCategoryNamed(ctx, command, parent)
		if err != nil {
			return err
		}
		variables["parentSpendingCategoryId"] = found.ID
	}
	setBool(command, variables, "is-income", "isIncome")
	setBool(command, variables, "is-hidden", "isHidden")
	var spendingCategory *client.SpendingCategory
	if err := financeCall(ctx, command, operationOf(command), variables, &spendingCategory); err != nil {
		return err
	}
	return printDone(command, spendingCategory, spendingCategory.ID+": added "+spendingCategory.SpendingCategoryName)
}

func runFinanceUpdateSpendingCategory(ctx context.Context, command *cli.Command) error {
	wanted, err := financeArgument(command, 0, "the spending category, by id or name")
	if err != nil {
		return err
	}
	found, err := spendingCategoryNamed(ctx, command, wanted)
	if err != nil {
		return err
	}
	variables := map[string]any{"spendingCategoryId": found.ID}
	setString(command, variables, "name", "spendingCategoryName")
	if command.IsSet("parent") {
		parentId := ""
		if parent := strings.TrimSpace(command.String("parent")); parent != "" {
			parentFound, err := spendingCategoryNamed(ctx, command, parent)
			if err != nil {
				return err
			}
			parentId = parentFound.ID
		}
		variables["parentSpendingCategoryId"] = parentId
	}
	setBool(command, variables, "is-income", "isIncome")
	setBool(command, variables, "is-hidden", "isHidden")
	var spendingCategory *client.SpendingCategory
	if err := financeCall(ctx, command, operationOf(command), variables, &spendingCategory); err != nil {
		return err
	}
	return printDone(command, spendingCategory, spendingCategory.ID+": changed")
}

func runFinanceDeleteSpendingCategory(ctx context.Context, command *cli.Command) error {
	wanted, err := financeArgument(command, 0, "the spending category, by id or name")
	if err != nil {
		return err
	}
	found, err := spendingCategoryNamed(ctx, command, wanted)
	if err != nil {
		return err
	}
	if err := confirm(command, "Delete spending category "+found.SpendingCategoryName+"? Its budgets and the spending rules that assign it go too, and its transactions become uncategorized."); err != nil {
		return err
	}
	if err := financeCall(ctx, command, operationOf(command), map[string]any{"spendingCategoryId": found.ID}, nil); err != nil {
		return err
	}
	return printDone(command, map[string]any{"spendingCategoryId": found.ID, "isDeleted": true}, found.ID+": deleted")
}

func runFinanceSpendingRules(ctx context.Context, command *cli.Command) error {
	var spendingRules []*client.SpendingRule
	if err := financeCall(ctx, command, operationOf(command), nil, &spendingRules); err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(spendingRules)
	}
	names, err := spendingCategoryNames(ctx, command)
	if err != nil {
		return err
	}
	rows := make([][]string, 0, len(spendingRules))
	for _, spendingRule := range spendingRules {
		assigns := names[spendingRule.SpendingCategoryID]
		bounds := ""
		if spendingRule.MinimumAmount != "" || spendingRule.MaximumAmount != "" {
			bounds = spendingRule.MinimumAmount + " to " + spendingRule.MaximumAmount
		}
		rows = append(rows, []string{spendingRule.ID, strconv.Itoa(spendingRule.RulePriority), spendingRule.MatchText, assigns, spendingRule.FinanceAccountID, bounds})
	}
	return printTable([]string{"id", "priority", "matches", "assigns", "finance account", "amounts"}, rows)
}

// spendingRuleVariables reads the flags a spending rule shares between
// create and update.
func spendingRuleVariables(ctx context.Context, command *cli.Command, variables map[string]any) error {
	if command.IsSet("spending-category") {
		spendingCategoryId := ""
		if wanted := strings.TrimSpace(command.String("spending-category")); wanted != "" {
			found, err := spendingCategoryNamed(ctx, command, wanted)
			if err != nil {
				return err
			}
			spendingCategoryId = found.ID
		}
		variables["spendingCategoryId"] = spendingCategoryId
	}
	setString(command, variables, "finance-account", "financeAccountId")
	setString(command, variables, "minimum-amount", "minimumAmount")
	setString(command, variables, "maximum-amount", "maximumAmount")
	if command.IsSet("priority") {
		variables["rulePriority"] = int(command.Int("priority"))
	}
	return nil
}

func runFinanceCreateSpendingRule(ctx context.Context, command *cli.Command) error {
	matchText, err := financeArgument(command, 0, "what the rule matches in the merchant or description")
	if err != nil {
		return err
	}
	if strings.TrimSpace(command.String("spending-category")) == "" {
		return usage("a spending rule needs --spending-category, the spending category it assigns; transfer marks what it matches as transfers")
	}
	variables := map[string]any{"matchText": matchText}
	if err := spendingRuleVariables(ctx, command, variables); err != nil {
		return err
	}
	var spendingRule *client.SpendingRule
	if err := financeCall(ctx, command, operationOf(command), variables, &spendingRule); err != nil {
		return err
	}
	return printDone(command, spendingRule, spendingRule.ID+": added, and applied to past transactions except the ones you chose")
}

func runFinanceUpdateSpendingRule(ctx context.Context, command *cli.Command) error {
	spendingRuleId, err := financeArgument(command, 0, "the spending rule's id; teanode finance spending-rules lists them")
	if err != nil {
		return err
	}
	variables := map[string]any{"spendingRuleId": spendingRuleId}
	setString(command, variables, "match-text", "matchText")
	if err := spendingRuleVariables(ctx, command, variables); err != nil {
		return err
	}
	var spendingRule *client.SpendingRule
	if err := financeCall(ctx, command, operationOf(command), variables, &spendingRule); err != nil {
		return err
	}
	return printDone(command, spendingRule, spendingRule.ID+": changed, and applied again")
}

func runFinanceDeleteSpendingRule(ctx context.Context, command *cli.Command) error {
	spendingRuleId, err := financeArgument(command, 0, "the spending rule's id; teanode finance spending-rules lists them")
	if err != nil {
		return err
	}
	if err := confirm(command, "Delete spending rule "+spendingRuleId+"?"); err != nil {
		return err
	}
	if err := financeCall(ctx, command, operationOf(command), map[string]any{"spendingRuleId": spendingRuleId}, nil); err != nil {
		return err
	}
	return printDone(command, map[string]any{"spendingRuleId": spendingRuleId, "isDeleted": true}, spendingRuleId+": deleted")
}

// transactionsAndSpendingCategory reads the arguments of a subcommand that
// takes one or more transaction ids and then a spending category: the ids,
// and the spending category's id, empty for none when none is allowed.
func transactionsAndSpendingCategory(ctx context.Context, command *cli.Command, isNoneAllowed bool) ([]string, string, error) {
	arguments := command.Args().Slice()
	var financeTransactionIds []string
	for _, argument := range arguments[:max(len(arguments)-1, 0)] {
		if financeTransactionId := strings.TrimSpace(argument); financeTransactionId != "" {
			financeTransactionIds = append(financeTransactionIds, financeTransactionId)
		}
	}
	if len(financeTransactionIds) == 0 {
		return nil, "", usage("give the transactions' ids and then a spending category: teanode finance " + command.Name + " <id>... dining")
	}
	wanted := strings.TrimSpace(arguments[len(arguments)-1])
	if isNoneAllowed && strings.EqualFold(wanted, "none") {
		return financeTransactionIds, "", nil
	}
	found, err := spendingCategoryNamed(ctx, command, wanted)
	if err != nil {
		return nil, "", err
	}
	return financeTransactionIds, found.ID, nil
}

func runFinanceProposeSpendingRules(ctx context.Context, command *cli.Command) error {
	financeTransactionIds, spendingCategoryId, err := transactionsAndSpendingCategory(ctx, command, false)
	if err != nil {
		return err
	}
	var proposals *client.SpendingRuleProposals
	variables := map[string]any{"financeTransactionIds": financeTransactionIds, "spendingCategoryId": spendingCategoryId}
	if err := financeCall(ctx, command, operationOf(command), variables, &proposals); err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(proposals)
	}
	if len(proposals.SpendingRuleProposals) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "no new spending rules: your spending rules already file these there, or they have no merchant or description to match")
	} else {
		names, err := spendingCategoryNames(ctx, command)
		if err != nil {
			return err
		}
		rows := make([][]string, 0, len(proposals.SpendingRuleProposals))
		for _, proposal := range proposals.SpendingRuleProposals {
			rows = append(rows, []string{
				proposal.MatchText, strconv.Itoa(proposal.FinanceTransactionCount), strconv.Itoa(proposal.ChangedTransactionCount),
				aheadOfSpendingRule(proposal, names),
			})
		}
		if err := printTable([]string{"matches", "transactions", "also changes", "goes ahead of"}, rows); err != nil {
			return err
		}
	}
	for _, reason := range proposals.LeftOutReasons() {
		_, _ = fmt.Fprintln(command.Writer, reason)
	}
	return nil
}

// aheadOfSpendingRule names the spending rule a proposed one goes ahead
// of, "zoomly eats → Dining", or nothing when it goes after every rule.
func aheadOfSpendingRule(proposal *client.SpendingRuleProposal, names map[string]string) string {
	if proposal.AheadOfSpendingRule == nil {
		return ""
	}
	return proposal.AheadOfSpendingRule.MatchText + " → " + names[proposal.AheadOfSpendingRule.SpendingCategoryID]
}

// runFinanceCategorizeTransaction categorizes one transaction with
// CategorizeTransaction, and several together with
// CategorizeTransactions, all or none.
func runFinanceCategorizeTransaction(ctx context.Context, command *cli.Command) error {
	financeTransactionIds, spendingCategoryId, err := transactionsAndSpendingCategory(ctx, command, true)
	if err != nil {
		return err
	}
	if len(financeTransactionIds) > 1 {
		return categorizeTransactions(ctx, command, financeTransactionIds, spendingCategoryId)
	}
	financeTransactionId := financeTransactionIds[0]
	variables := map[string]any{"financeTransactionId": financeTransactionId, "spendingCategoryId": spendingCategoryId}
	setBool(command, variables, "create-spending-rule", "shouldCreateSpendingRule")
	var categorized *client.CategorizedTransaction
	if err := financeCall(ctx, command, operationOf(command), variables, &categorized); err != nil {
		return err
	}
	line := financeTransactionId + ": categorized"
	if categorized.SpendingRule != nil {
		line += fmt.Sprintf("; spending rule %s matches %q from now on and in the past", categorized.SpendingRule.ID, categorized.SpendingRule.MatchText)
	}
	return printDone(command, categorized, line)
}

// categorizeTransactions is categorize-transaction given several ids. With
// --create-spending-rule it asks ProposeSpendingRules first and saves
// exactly what that proposes, as the dashboard does once the person
// confirms; propose-spending-rules shows the same list beforehand.
func categorizeTransactions(ctx context.Context, command *cli.Command, financeTransactionIds []string, spendingCategoryId string) error {
	variables := map[string]any{"financeTransactionIds": financeTransactionIds, "spendingCategoryId": spendingCategoryId}
	var proposals *client.SpendingRuleProposals
	if command.Bool("create-spending-rule") {
		if spendingCategoryId == "" {
			return usage("a spending rule needs the spending category it assigns, not none")
		}
		if err := financeCall(ctx, command, "ProposeSpendingRules", variables, &proposals); err != nil {
			return err
		}
		variables["spendingRules"] = proposals.Confirmed()
	}
	var categorized *client.CategorizedTransactions
	if err := financeCall(ctx, command, "CategorizeTransactions", variables, &categorized); err != nil {
		return err
	}
	line := fmt.Sprintf("%d transactions categorized", len(categorized.FinanceTransactions))
	for _, spendingRule := range categorized.SpendingRules {
		line += fmt.Sprintf("\nspending rule %s matches %q from now on and in the past", spendingRule.ID, spendingRule.MatchText)
	}
	if proposals != nil {
		leftOutReasons := proposals.LeftOutReasons()
		if len(categorized.SpendingRules) == 0 && len(leftOutReasons) == 0 {
			line += "\nno new spending rules: your spending rules already file these there"
		}
		for _, reason := range leftOutReasons {
			line += "\n" + reason
		}
	}
	return printDone(command, categorized, line)
}

// runFinanceCountTransaction is count-transaction and
// undo-count-transaction, which differ only in the operation they call.
func runFinanceCountTransaction(ctx context.Context, command *cli.Command) error {
	financeTransactionId, err := financeArgument(command, 0, "the transaction's id")
	if err != nil {
		return err
	}
	var counted *client.FinanceTransaction
	if err := financeCall(ctx, command, operationOf(command), map[string]any{"financeTransactionId": financeTransactionId}, &counted); err != nil {
		return err
	}
	if counted.DuplicateOfTransactionID != "" {
		return printDone(command, counted, counted.ID+": a duplicate of "+counted.DuplicateOfTransactionID+", left out of every total")
	}
	return printDone(command, counted, counted.ID+": counted")
}

// --- budgets, spending and savings targets --------------------------------

func runFinanceBudgets(ctx context.Context, command *cli.Command) error {
	var budgets []*client.Budget
	if err := financeCall(ctx, command, operationOf(command), nil, &budgets); err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(budgets)
	}
	if len(budgets) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "no budgets yet; teanode finance set-budget sets one")
		return nil
	}
	var spendingCategories []*client.SpendingCategory
	if err := financeCall(ctx, command, "SpendingCategories", nil, &spendingCategories); err != nil {
		return err
	}
	byId := map[string]*client.SpendingCategory{}
	for _, spendingCategory := range spendingCategories {
		byId[spendingCategory.ID] = spendingCategory
	}
	rows := make([][]string, 0, len(budgets))
	for _, budget := range budgets {
		name, budgetKind := "", "spending"
		if spendingCategory := byId[budget.SpendingCategoryID]; spendingCategory != nil {
			name = spendingCategory.SpendingCategoryName
			if spendingCategory.IsIncome {
				budgetKind = "income"
			}
		}
		rows = append(rows, []string{name, budgetKind, money(budget.MonthlyAmount, budget.CurrencyCode), strings.TrimSuffix(budget.EffectiveFrom, "-01"), budget.ID})
	}
	return printTable([]string{"spending category", "kind", "monthly", "from", "id"}, rows)
}

func runFinanceSetBudget(ctx context.Context, command *cli.Command) error {
	wanted, err := financeArgument(command, 0, "the spending category and the monthly amount: teanode finance set-budget dining 400")
	if err != nil {
		return err
	}
	monthlyAmount, err := financeArgument(command, 1, "the monthly amount")
	if err != nil {
		return err
	}
	found, err := spendingCategoryNamed(ctx, command, wanted)
	if err != nil {
		return err
	}
	variables := map[string]any{"spendingCategoryId": found.ID, "monthlyAmount": monthlyAmount}
	setString(command, variables, "currency", "currencyCode")
	setString(command, variables, "from", "effectiveFrom")
	var budget *client.Budget
	if err := financeCall(ctx, command, operationOf(command), variables, &budget); err != nil {
		return err
	}
	return printDone(command, budget, fmt.Sprintf("%s: %s a month from %s", found.SpendingCategoryName, money(budget.MonthlyAmount, budget.CurrencyCode), strings.TrimSuffix(budget.EffectiveFrom, "-01")))
}

func runFinanceBudgetStatus(ctx context.Context, command *cli.Command) error {
	variables := map[string]any{}
	setString(command, variables, "month", "month")
	var status *client.BudgetStatus
	if err := financeCall(ctx, command, operationOf(command), variables, &status); err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(status)
	}
	if len(status.SpendingCategories) == 0 && len(status.IncomeCategories) == 0 {
		_, _ = fmt.Fprintf(command.Writer, "no budgets in %s; teanode finance set-budget sets one\n", status.Month)
		return nil
	}
	_, _ = fmt.Fprintf(command.Writer, "%s, day %d of %d\n", status.Month, status.DayOfMonth, status.DaysInMonth)
	if len(status.SpendingCategories) > 0 {
		rows := make([][]string, 0, len(status.SpendingCategories))
		for _, row := range status.SpendingCategories {
			rows = append(rows, []string{
				row.SpendingCategoryName, money(row.BudgetAmount, row.CurrencyCode), money(row.SpendingAmount, row.CurrencyCode),
				money(row.SpendingBySameDayLastMonthAmount, row.CurrencyCode), money(row.ProjectedAmount, row.CurrencyCode), row.BudgetPace,
			})
		}
		if err := printTable([]string{"spending category", "budget", "spent", "same day last month", "projected", "pace"}, rows); err != nil {
			return err
		}
		// What each projection counts before it lands: merchants that
		// charged the spending category in each of the last three months
		// and have not yet this month.
		for _, row := range status.SpendingCategories {
			if len(row.ExpectedRepeatCharges) == 0 {
				continue
			}
			charges := make([]string, 0, len(row.ExpectedRepeatCharges))
			for _, charge := range row.ExpectedRepeatCharges {
				charges = append(charges, charge.MerchantName+" "+money(charge.ExpectedAmount, charge.CurrencyCode))
			}
			_, _ = fmt.Fprintf(command.Writer, "repeat charges still expected in %s: %s\n", row.SpendingCategoryName, strings.Join(charges, ", "))
		}
	}
	if len(status.IncomeCategories) > 0 {
		if len(status.SpendingCategories) > 0 {
			_, _ = fmt.Fprintln(command.Writer)
		}
		rows := make([][]string, 0, len(status.IncomeCategories))
		for _, row := range status.IncomeCategories {
			rows = append(rows, []string{
				row.SpendingCategoryName, money(row.BudgetAmount, row.CurrencyCode), money(row.IncomeAmount, row.CurrencyCode),
				money(row.ExpectedByTodayAmount, row.CurrencyCode), money(row.IncomeBySameDayLastMonthAmount, row.CurrencyCode),
				money(row.ProjectedAmount, row.CurrencyCode), row.IncomePace,
			})
		}
		return printTable([]string{"income category", "expected", "received", "expected by today", "same day last month", "projected", "pace"}, rows)
	}
	return nil
}

func runFinanceSavingSummary(ctx context.Context, command *cli.Command) error {
	variables := map[string]any{}
	setString(command, variables, "month", "month")
	setString(command, variables, "currency", "currencyCode")
	var summary *client.SavingSummary
	if err := financeCall(ctx, command, operationOf(command), variables, &summary); err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(summary)
	}
	if summary.ReportingCurrencyCode == "" {
		_, _ = fmt.Fprintln(command.Writer, "nothing to report in yet; link a finance source or set a reporting currency")
		return nil
	}
	if summary.IncomeBudgetCount == 0 && summary.SpendingBudgetCount == 0 {
		_, _ = fmt.Fprintf(command.Writer, "no budgets in %s; teanode finance set-budget sets one on an income or a spending category\n", summary.Month)
		return nil
	}
	currencyCode := summary.ReportingCurrencyCode
	_, _ = fmt.Fprintf(command.Writer, "%s, day %d of %d, %s\n", summary.Month, summary.DayOfMonth, summary.DaysInMonth, strings.ReplaceAll(summary.SavingPace, "_", " "))
	rows := [][]string{
		{"income", money(summary.ExpectedIncomeAmount, currencyCode), money(summary.IncomeAmount, currencyCode), money(summary.ProjectedIncomeAmount, currencyCode)},
		{"spending", money(summary.ExpectedSpendingAmount, currencyCode), money(summary.SpendingAmount, currencyCode), money(summary.ProjectedSpendingAmount, currencyCode)},
		{"saving", money(summary.ExpectedSavingAmount, currencyCode), money(summary.SavingAmount, currencyCode), money(summary.ProjectedSavingAmount, currencyCode)},
	}
	if err := printTable([]string{"", "budgeted", "so far", "projected"}, rows); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(command.Writer, "projected saving against budgeted: %s\n", money(summary.SavingDifferenceAmount, currencyCode))
	if summary.IncomeBudgetCount == 0 {
		_, _ = fmt.Fprintln(command.Writer, "no income budget yet, so the budgeted saving counts no income; teanode finance set-budget income 4000 sets one")
	}
	if len(summary.UnconvertedCurrencyCodes) > 0 {
		_, _ = fmt.Fprintf(command.Writer, "left out, with no exchange rate into %s: %s\n", currencyCode, strings.Join(summary.UnconvertedCurrencyCodes, ", "))
	}
	return nil
}

func runFinanceSpendingByDay(ctx context.Context, command *cli.Command) error {
	variables := map[string]any{}
	setString(command, variables, "month", "month")
	setString(command, variables, "compare-month", "compareMonth")
	setString(command, variables, "currency", "currencyCode")
	var byDay *client.SpendingByDay
	if err := financeCall(ctx, command, operationOf(command), variables, &byDay); err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(byDay)
	}
	compared := map[string]string{}
	for _, day := range byDay.CompareMonthDays {
		compared[day.SpentOn[len("2006-01-"):]] = day.CumulativeSpendingAmount
	}
	rows := make([][]string, 0, len(byDay.MonthDays))
	for _, day := range byDay.MonthDays {
		dayOfMonth := day.SpentOn[len("2006-01-"):]
		rows = append(rows, []string{day.SpentOn, money(day.SpendingAmount, byDay.ReportingCurrencyCode), money(day.CumulativeSpendingAmount, byDay.ReportingCurrencyCode), money(compared[dayOfMonth], byDay.ReportingCurrencyCode)})
	}
	return printTable([]string{"day", "spent", "month so far", byDay.CompareMonth + " so far"}, rows)
}

func runFinanceCashFlow(ctx context.Context, command *cli.Command) error {
	variables := map[string]any{}
	setString(command, variables, "from-month", "fromMonth")
	setString(command, variables, "to-month", "toMonth")
	setString(command, variables, "currency", "currencyCode")
	var cashFlow *client.CashFlow
	if err := financeCall(ctx, command, operationOf(command), variables, &cashFlow); err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(cashFlow)
	}
	rows := [][]string{}
	if cashFlow.ReportingCurrencyCode != "" {
		for _, month := range cashFlow.CashFlowMonths {
			rows = append(rows, []string{month.CashFlowMonth, money(month.IncomeAmount, cashFlow.ReportingCurrencyCode), money(month.SpendingAmount, cashFlow.ReportingCurrencyCode), money(month.NetAmount, cashFlow.ReportingCurrencyCode)})
		}
	} else {
		for _, month := range cashFlow.CurrencyCashFlowMonths {
			rows = append(rows, []string{month.CashFlowMonth, money(month.IncomeAmount, month.CurrencyCode), money(month.SpendingAmount, month.CurrencyCode), money(month.NetAmount, month.CurrencyCode)})
		}
	}
	if err := printTable([]string{"month", "income", "spending", "left"}, rows); err != nil {
		return err
	}
	if len(cashFlow.UnconvertedCurrencyCodes) > 0 {
		fmt.Fprintf(os.Stderr, "note: left out of the %s totals for want of an exchange rate: %s\n", cashFlow.ReportingCurrencyCode, strings.Join(cashFlow.UnconvertedCurrencyCodes, ", "))
	}
	return nil
}

func runFinanceSavingsTargets(ctx context.Context, command *cli.Command) error {
	var standings []*client.SavingsTargetStanding
	if err := financeCall(ctx, command, operationOf(command), nil, &standings); err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(standings)
	}
	if len(standings) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "no savings targets yet; teanode finance create-savings-target adds one")
		return nil
	}
	sort.SliceStable(standings, func(left, right int) bool {
		return standings[left].SavingsTarget.ClosedOn == "" && standings[right].SavingsTarget.ClosedOn != ""
	})
	rows := make([][]string, 0, len(standings))
	for _, standing := range standings {
		target, progress := standing.SavingsTarget, standing.SavingsTargetProgress
		state := "on track"
		switch {
		case target.ClosedOn != "":
			state = "closed " + target.ClosedOn
		case progress.IsBehind:
			state = "behind"
		}
		rows = append(rows, []string{
			target.ID, target.SavingsTargetName, target.TargetMeasure, money(target.TargetAmount, target.CurrencyCode), target.TargetOn,
			money(progress.SavedAmount, target.CurrencyCode), money(progress.RequiredMonthlyAmount, target.CurrencyCode), state,
		})
	}
	return printTable([]string{"id", "savings target", "measure", "target", "by", "saved", "needed a month", "state"}, rows)
}

// savingsTargetVariables reads the flags a savings target shares between
// create and update.
func savingsTargetVariables(command *cli.Command, variables map[string]any) {
	setString(command, variables, "target-on", "targetOn")
	setString(command, variables, "currency", "currencyCode")
	setString(command, variables, "measure", "targetMeasure")
	setString(command, variables, "starting-amount", "startingAmount")
	setString(command, variables, "started-on", "startedOn")
	if command.IsSet("asset") {
		variables["assetIds"] = command.StringSlice("asset")
	}
	if command.IsSet("finance-account") {
		variables["financeAccountIds"] = command.StringSlice("finance-account")
	}
}

func runFinanceCreateSavingsTarget(ctx context.Context, command *cli.Command) error {
	name, err := financeArgument(command, 0, "the name and the amount: teanode finance create-savings-target \"emergency fund\" 10000 --target-on 2027-06-30")
	if err != nil {
		return err
	}
	targetAmount, err := financeArgument(command, 1, "the amount to save")
	if err != nil {
		return err
	}
	if strings.TrimSpace(command.String("target-on")) == "" {
		return usage("give --target-on, the day to reach it by")
	}
	variables := map[string]any{"savingsTargetName": name, "targetAmount": targetAmount}
	savingsTargetVariables(command, variables)
	var standing *client.SavingsTargetStanding
	if err := financeCall(ctx, command, operationOf(command), variables, &standing); err != nil {
		return err
	}
	return printDone(command, standing, fmt.Sprintf("%s: added; it needs %s a month", standing.SavingsTarget.ID,
		money(standing.SavingsTargetProgress.RequiredMonthlyAmount, standing.SavingsTarget.CurrencyCode)))
}

func runFinanceUpdateSavingsTarget(ctx context.Context, command *cli.Command) error {
	savingsTargetId, err := financeArgument(command, 0, "the savings target's id; teanode finance savings-targets lists them")
	if err != nil {
		return err
	}
	variables := map[string]any{"savingsTargetId": savingsTargetId}
	setString(command, variables, "name", "savingsTargetName")
	setString(command, variables, "target-amount", "targetAmount")
	savingsTargetVariables(command, variables)
	var standing *client.SavingsTargetStanding
	if err := financeCall(ctx, command, operationOf(command), variables, &standing); err != nil {
		return err
	}
	return printDone(command, standing, standing.SavingsTarget.ID+": changed")
}

func runFinanceCloseSavingsTarget(ctx context.Context, command *cli.Command) error {
	savingsTargetId, err := financeArgument(command, 0, "the savings target's id; teanode finance savings-targets lists them")
	if err != nil {
		return err
	}
	variables := map[string]any{"savingsTargetId": savingsTargetId}
	setString(command, variables, "on", "closedOn")
	setBool(command, variables, "reopen", "shouldReopen")
	var standing *client.SavingsTargetStanding
	if err := financeCall(ctx, command, operationOf(command), variables, &standing); err != nil {
		return err
	}
	if standing.SavingsTarget.ClosedOn == "" {
		return printDone(command, standing, standing.SavingsTarget.ID+": open")
	}
	return printDone(command, standing, standing.SavingsTarget.ID+": closed on "+standing.SavingsTarget.ClosedOn)
}
