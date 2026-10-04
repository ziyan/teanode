package finance

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/client"
	financecore "github.com/ziyan/teanode/internal/finance"
)

// import_transactions is how the agent imports what it read off pictures:
// screenshots of a bank's or a card issuer's app, a photographed
// statement. It reads the rows itself, with its own eyes, and sends them
// here as rows; the check that they add up (running balances that chain,
// monthly totals matched) is the finance package's, the same one the API
// runs before writing, so the confirmation card can say what was checked
// and a set that does not add up is refused before the person is asked.
// The card then asks the server what the import would do
// (PreviewImportTransactions, also the tool's preview_import_transactions):
// which account, and which rows that account holds already.

// transactionRowsArguments are import_transactions's arguments, and
// preview_import_transactions's.
var transactionRowsArguments = []string{
	"finance_account_id", "is_new_account",
	"institution_name", "account_name", "account_number", "is_account_number_partial", "statement_account_kind", "currency_code",
	"bank_code", "transaction_rows", "ledger_balance_amount", "ledger_balance_on", "ledger_balance_time_zone", "monthly_totals",
}

// previewTransactionRowCount is how many rows the confirmation card of an
// import names.
const previewTransactionRowCount = 3

// amountText is an amount argument as text: a model may send an amount as
// a JSON number, which arrives as a float64, and its shortest form is the
// decimal it wrote.
func amountText(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case json.Number:
		return typed.String()
	}
	return ""
}

// objects is a list argument's objects; anything else in it is left out.
func objects(call map[string]any, key string) []map[string]any {
	list, _ := call[key].([]any)
	found := make([]map[string]any, 0, len(list))
	for _, element := range list {
		if object, isObject := element.(map[string]any); isObject {
			found = append(found, object)
		}
	}
	return found
}

// transactionRowFields are what a row of transaction_rows says, and
// monthlyTotalFields what an entry of monthly_totals says.
var (
	transactionRowFields = []string{"posted_on", "description", "amount", "transaction_kind", "running_balance_amount", "total_month"}
	monthlyTotalFields   = []string{"total_month", "total_amount"}
)

// checkTransactionRowFields refuses a row or a total with a field it does
// not take, naming it: a balance sent as "balance" would be dropped, and
// the rows would then go unchecked where the person thinks they were.
func checkTransactionRowFields(call map[string]any) error {
	// A number holds about sixteen digits exactly, so a longer account
	// number sent as one arrives with its last digits changed, and the
	// account would be keyed on them for good.
	for _, key := range []string{"account_number", "bank_code"} {
		if _, isNumber := call[key].(float64); isNumber {
			return fmt.Errorf("give %s as a string, as the list shows it: a number can lose digits", key)
		}
	}
	for key, fields := range map[string][]string{"transaction_rows": transactionRowFields, "monthly_totals": monthlyTotalFields} {
		isField := map[string]bool{}
		for _, field := range fields {
			isField[field] = true
		}
		list, _ := call[key].([]any)
		for index, element := range list {
			object, isObject := element.(map[string]any)
			if !isObject {
				return fmt.Errorf("%s %d is not an object with %s", key, index+1, strings.Join(fields, ", "))
			}
			for field := range object {
				if !isField[field] {
					return fmt.Errorf("%s %d has %s, which it does not take; it takes %s", key, index+1, field, strings.Join(fields, ", "))
				}
			}
		}
	}
	return nil
}

// transactionRowsInputOf is import_transactions's arguments as the finance
// package checks them. The risk, the card and the run all read them
// through this one function, so what is judged is what is sent.
func transactionRowsInputOf(call map[string]any) *financecore.TransactionRowsInput {
	input := &financecore.TransactionRowsInput{
		InstitutionName: text(call, "institution_name"), AccountName: text(call, "account_name"),
		AccountNumber: amountText(call["account_number"]), IsAccountNumberPartial: isTrue(call, "is_account_number_partial"),
		StatementAccountKind: financecore.StatementAccountKind(strings.ToLower(text(call, "statement_account_kind"))),
		CurrencyCode:         text(call, "currency_code"), BankCode: amountText(call["bank_code"]),
		LedgerBalanceAmount: amountText(call["ledger_balance_amount"]), LedgerBalanceOn: text(call, "ledger_balance_on"),
		LedgerBalanceTimeZone: text(call, "ledger_balance_time_zone"),
		FinanceAccountID:      text(call, "finance_account_id"), IsNewAccount: isTrue(call, "is_new_account"),
	}
	for _, row := range objects(call, "transaction_rows") {
		input.TransactionRows = append(input.TransactionRows, financecore.TransactionRow{
			PostedOn: text(row, "posted_on"), Description: text(row, "description"), Amount: amountText(row["amount"]),
			TransactionKind: text(row, "transaction_kind"), RunningBalanceAmount: amountText(row["running_balance_amount"]),
			TotalMonth: text(row, "total_month"),
		})
	}
	for _, total := range objects(call, "monthly_totals") {
		input.MonthlyTotals = append(input.MonthlyTotals, financecore.MonthlyTotal{TotalMonth: text(total, "total_month"), TotalAmount: amountText(total["total_amount"])})
	}
	return input
}

// isRefusedTransactionRows says an import_transactions call would be
// refused because its rows do not add up. Such a call is not put to the
// person: nothing would be written, and a card that asks to import what
// will be refused asks them to decide nothing. It runs instead, and its
// answer names the row or month to read again. The day is not checked
// here, since the person's today is not known; the API refuses a day
// after it after the card.
func isRefusedTransactionRows(arguments json.RawMessage) bool {
	call := map[string]any{}
	if json.Unmarshal(arguments, &call) != nil {
		return false
	}
	if checkTransactionRowFields(call) != nil {
		return true
	}
	_, err := financecore.CheckTransactionRows(transactionRowsInputOf(call), "")
	return err != nil
}

// refusalText is a refusal of transaction rows without the words every
// refusal begins with.
func refusalText(err error) string {
	said := err.Error()
	if index := strings.Index(said, financecore.ErrTransactionRowsRefused.Error()+": "); index >= 0 {
		said = said[index+len(financecore.ErrTransactionRowsRefused.Error()+": "):]
	}
	return said
}

// importTransactionsPreview is the confirmation card of an import, from
// the server's preview of it: the account (existing, and how it was
// found, or new), how many rows are new and how many it holds already, a
// few of the new rows, their days, money in and out of them, and what was
// checked. Rows that do not add up, or an account the server refuses,
// say so. With no run to ask the server through, it says what the rows
// alone say.
func importTransactionsPreview(lookup *previewLookup, call map[string]any) string {
	check, err := financecore.CheckTransactionRows(transactionRowsInputOf(call), "")
	if err != nil {
		return "Import transactions read from pictures; they do not add up and will be refused: " + refusalText(err)
	}
	if lookup == nil || lookup.executor == nil {
		return rowsAlonePreview(check)
	}
	variables, err := transactionRowsVariables(call)
	if err != nil {
		return "Import transactions read from pictures; they will be refused: " + err.Error()
	}
	var preview *client.TransactionRowsPreview
	if err := client.RunFinance(lookup.ctx, lookup.executor, "PreviewImportTransactions", variables, &preview); err != nil || preview == nil {
		reason := "the server did not say what the import would do"
		if err != nil {
			reason = refusalText(err)
		}
		return "Import transactions read from pictures; they will be refused: " + reason
	}
	return transactionRowsPreviewLine(preview)
}

// accountMatchWords say how the server found the account.
var accountMatchWords = map[string]string{
	string(financecore.TransactionRowsAccountMatchFinanceAccountID): "named by its id",
	string(financecore.TransactionRowsAccountMatchAccountNumber):    "found by its number",
	string(financecore.TransactionRowsAccountMatchAccountMask):      "found by the last digits shown",
}

// transactionRowsPreviewLine is the card for a preview the server gave.
func transactionRowsPreviewLine(preview *client.TransactionRowsPreview) string {
	amount := func(value string) string {
		return financecore.FormatTransactionRowsAmount(value, preview.CurrencyCode)
	}
	account := "a new account " + tools.Named(preview.AccountName, "")
	if !preview.IsNewAccount {
		account = "the existing account " + tools.Named(preview.AccountName, "")
		if words := accountMatchWords[preview.AccountMatch]; words != "" {
			account += " (" + words + ")"
		}
	}
	line := fmt.Sprintf("Import transactions read from pictures into %s: %d new, %d already there", account,
		len(preview.NewTransactionRows), len(preview.PresentTransactionRows))
	if len(preview.NewTransactionRows) == 0 {
		line += "; nothing to add"
	} else {
		var examples []string
		nearbyCount := 0
		for _, row := range preview.NewTransactionRows {
			if row.HasNearbyStoredTransaction {
				nearbyCount++
			}
		}
		shown := preview.NewTransactionRows
		if len(shown) > previewTransactionRowCount {
			shown = shown[:previewTransactionRowCount]
		}
		for _, row := range shown {
			examples = append(examples, fmt.Sprintf("%s %s %s", row.PostedOn, tools.Named(row.Description, ""), amount(row.Amount)))
		}
		line += ". New: " + strings.Join(examples, "; ")
		if remainingCount := len(preview.NewTransactionRows) - len(shown); remainingCount > 0 {
			line += fmt.Sprintf("; and %d more", remainingCount)
		}
		line += fmt.Sprintf(". From %s to %s, money in %s, money out %s", preview.FirstPostedOn, preview.LastPostedOn,
			amount(preview.MoneyInAmount), amount(preview.MoneyOutAmount))
		// An app and an export can date one transaction differently (the
		// day it was made and the day it posted), which the match by day
		// does not take as the same; the person is shown it instead.
		switch {
		case nearbyCount == 1:
			line += fmt.Sprintf(". 1 new row has a stored transaction of the same amount within %d days, perhaps the same one dated differently",
				financecore.NearbyStoredTransactionDays)
		case nearbyCount > 1:
			line += fmt.Sprintf(". %d new rows have a stored transaction of the same amount within %d days, perhaps the same ones dated differently",
				nearbyCount, financecore.NearbyStoredTransactionDays)
		}
	}
	line += "; " + preview.VerificationSummary
	if preview.LedgerBalanceAmount != "" {
		line += fmt.Sprintf("; balance %s on %s", amount(preview.LedgerBalanceAmount), preview.LedgerBalanceOn)
		if preview.LedgerBalanceTimeZone != "" {
			line += " (" + preview.LedgerBalanceTimeZone + ")"
		}
	}
	return line
}

// rowsAlonePreview is the card from the rows alone, when the server
// cannot be asked: the account as the rows name it, how many over which
// days, money in and out, what was checked, and a few of the rows.
func rowsAlonePreview(check *financecore.TransactionRowsCheck) string {
	amount := func(value string) string {
		return financecore.FormatTransactionRowsAmount(value, check.CurrencyCode)
	}
	accountName := check.InstitutionName
	if check.AccountName != "" {
		accountName += " " + check.AccountName
	}
	number := "account ending " + financecore.StatementAccountMask(check.AccountNumber)
	if check.IsAccountNumberPartial {
		number = "only part of its number shown, ending " + financecore.StatementAccountMask(check.AccountNumber)
	}
	line := fmt.Sprintf("Import %s into %s (%s, %s), %s from %s to %s: money in %s, money out %s; %s",
		countOf(len(check.TransactionRows), "transaction"), tools.Named(accountName, "an account"), check.StatementAccountKind, number,
		check.CurrencyCode, check.FirstPostedOn, check.LastPostedOn, amount(check.MoneyInAmount), amount(check.MoneyOutAmount),
		check.VerificationSummary())
	if check.LedgerBalanceAmount != "" {
		line += fmt.Sprintf("; balance %s on %s", amount(check.LedgerBalanceAmount), check.LedgerBalanceOn)
		if check.LedgerBalanceTimeZone != "" {
			line += " (" + check.LedgerBalanceTimeZone + ")"
		}
	}
	var examples []string
	for _, row := range examplesOf(check.TransactionRows) {
		examples = append(examples, fmt.Sprintf("%s %s %s", row.PostedOn, tools.Named(row.Description, ""), amount(row.Amount)))
	}
	line += ". For example: " + strings.Join(examples, "; ")
	if remainingCount := len(check.TransactionRows) - len(examples); remainingCount > 0 {
		line += fmt.Sprintf("; and %d more", remainingCount)
	}
	return line + ". Rows the account holds already are not added again"
}

// examplesOf is the rows a card names: the first, one from the middle and
// the last, oldest first, so the person can hold the ends against the
// first and the last screenshot.
func examplesOf(rows []*financecore.CheckedTransactionRow) []*financecore.CheckedTransactionRow {
	if len(rows) <= previewTransactionRowCount {
		return rows
	}
	return []*financecore.CheckedTransactionRow{rows[0], rows[len(rows)/2], rows[len(rows)-1]}
}

// countOf is a count with its noun, made plural past one.
func countOf(count int, noun string) string {
	if count == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", count, noun)
}

// transactionRowsVariables are import_transactions's arguments as the API
// names them, the rows' and totals' fields too, amounts as text.
func transactionRowsVariables(asked map[string]any) (map[string]any, error) {
	if err := checkTransactionRowFields(asked); err != nil {
		return nil, err
	}
	variables := map[string]any{}
	for _, key := range transactionRowsArguments {
		value, isGiven := asked[key]
		if !isGiven || value == nil {
			continue
		}
		switch key {
		case "transaction_rows", "monthly_totals":
			var converted []map[string]any
			for _, object := range objects(asked, key) {
				entry := map[string]any{}
				for field, fieldValue := range object {
					if strings.HasSuffix(field, "amount") {
						fieldValue = amountText(fieldValue)
					}
					entry[camelCase(field)] = fieldValue
				}
				converted = append(converted, entry)
			}
			value = converted
		case "account_number", "bank_code", "ledger_balance_amount":
			value = amountText(value)
		}
		variables[camelCase(key)] = value
	}
	return variables, nil
}

// importTransactions checks the rows and sends them to ImportTransactions,
// or with preview_import_transactions to PreviewImportTransactions, with
// the rows and totals as the API names them, answering what was imported,
// or would be, and what was checked.
func importTransactions(ctx context.Context, executor tools.Operations, name string, asked map[string]any) (*tools.Result, error) {
	variables, err := transactionRowsVariables(asked)
	if err != nil {
		return nil, err
	}
	check, err := financecore.CheckTransactionRows(transactionRowsInputOf(asked), "")
	if err != nil {
		return nil, err
	}
	operation := "ImportTransactions"
	if name == "preview_import_transactions" {
		operation = "PreviewImportTransactions"
	}
	var answered any
	if err := client.RunFinance(ctx, executor, operation, variables, &answered); err != nil {
		return nil, err
	}
	payload := map[string]any{name: answered, "verification": check.VerificationSummary()}
	if check.IsAccountNumberPartial && check.FinanceAccountID == "" {
		payload["hint"] = "the account is found by the digits shown, ending " + financecore.StatementAccountMask(check.AccountNumber) +
			", or by the id of the account they are of; a new account made from them is known by those digits, so an OFX file carrying the whole number would be a separate account"
	}
	result, err := tools.JSONResult(payload)
	if err != nil {
		return nil, err
	}
	result.Untrusted = true
	result.Note = "import transactions"
	if operation == "PreviewImportTransactions" {
		result.Note = "preview an import of transactions"
	}
	return result, nil
}
