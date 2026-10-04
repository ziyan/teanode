package finance

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"
	"unicode"

	normalization "golang.org/x/text/unicode/norm"

	"github.com/ziyan/teanode/internal/finance/ofx"
)

// Transaction rows: the transactions of one account as somebody read them
// off a list, typically the agent reading screenshots of a bank's or a card
// issuer's app, sent as rows rather than as a file. They are imported the
// way a statement is, into the statement source, with an account keyed as
// a statement's account is, so a later OFX file or set of rows for the same
// account number lands in the same finance account.
//
// Rows read from a picture can be misread or cut off, so they are checked
// before anything is written: the running balances a bank's list shows
// must chain from row to row, and the totals a card's list shows per month
// must be what that month's rows add up to. A set that does not add up is
// refused, naming the first row or month that does not, so whoever read it
// can read that part again.

// StatementAccountKind is what sort of account transaction rows are from.
type StatementAccountKind string

const (
	// StatementAccountKindBank is a bank account: checking, savings.
	StatementAccountKindBank StatementAccountKind = "bank"

	// StatementAccountKindCard is a credit card.
	StatementAccountKindCard StatementAccountKind = "card"

	// StatementAccountKindOther is anything else that holds money, such as
	// a prepaid or electronic money account. OFX has no third kind, so it
	// is keyed as a bank account is: an OFX file of it would be a bank
	// statement.
	StatementAccountKindOther StatementAccountKind = "other"
)

// StatementImportOriginTransactionRows is the cursor's word, and the
// account's and each transaction's metadata's, for an import of
// transaction rows.
const StatementImportOriginTransactionRows = "transaction_rows"

// statementImportOriginField is the provider metadata field that says an
// account was made from transaction rows, and that a transaction was
// written by them. On a transaction it is how a later file's import tells
// a row from a file's own transaction (LeaveOutStoredTransactionRows):
// both can be stored under an identifier made from what they say, and
// nothing else stored with them tells the two apart. It is kept in the
// metadata every transaction carries already, so it needs no column.
const statementImportOriginField = "statementImportOrigin"

// IsTransactionRowsMetadata says a stored transaction's provider metadata
// is that of a transaction an import of transaction rows wrote.
func IsTransactionRowsMetadata(providerMetadata json.RawMessage) bool {
	var metadata struct {
		StatementImportOrigin string `json:"statementImportOrigin"`
	}
	if len(providerMetadata) == 0 || json.Unmarshal(providerMetadata, &metadata) != nil {
		return false
	}
	return metadata.StatementImportOrigin == StatementImportOriginTransactionRows
}

// MaximumTransactionRows is the most rows one import takes: a year of a
// busy card is a few hundred, and a set this large was not read off
// screenshots one by one.
const MaximumTransactionRows = 2000

// transactionKindTypes is the OFX transaction type each transaction kind is
// imported as, which is what the provider category mapping reads: a
// payment on a card is a transfer, interest and fees settle their spending
// category, and a purchase or a refund is left to the categorize model.
var transactionKindTypes = map[string]string{
	"deposit": "DEP", "withdrawal": "DEBIT", "purchase": "DEBIT", "refund": "CREDIT", "payment": "PAYMENT",
	"transfer": "XFER", "interest": "INT", "dividend": "DIV", "fee": "FEE", "cash": "ATM", "other": "OTHER",
}

// TransactionKinds is every transaction kind a row may say, in order.
func TransactionKinds() []string {
	kinds := make([]string, 0, len(transactionKindTypes))
	for kind := range transactionKindTypes {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	return kinds
}

// currencyMinorUnits is how many places a currency's amounts have where
// that is not two (ISO 4217). An amount with more places than its currency
// has was misread, or is not an amount of that currency.
var currencyMinorUnits = map[string]int{
	"BIF": 0, "CLP": 0, "DJF": 0, "GNF": 0, "ISK": 0, "JPY": 0, "KMF": 0, "KRW": 0, "PYG": 0, "RWF": 0,
	"UGX": 0, "UYI": 0, "VND": 0, "VUV": 0, "XAF": 0, "XOF": 0, "XPF": 0,
	"BHD": 3, "IQD": 3, "JOD": 3, "KWD": 3, "LYD": 3, "OMR": 3, "TND": 3,
}

// CurrencyMinorUnits is how many places an amount in the currency has.
func CurrencyMinorUnits(currencyCode string) int {
	if places, isKnown := currencyMinorUnits[currencyCode]; isKnown {
		return places
	}
	return 2
}

// ErrTransactionRowsRefused begins every refusal of transaction rows.
var ErrTransactionRowsRefused = errors.New("the transactions were not imported")

func refuseRows(format string, arguments ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrTransactionRowsRefused}, arguments...)...)
}

// TransactionRowsInput is one account's transactions as read.
type TransactionRowsInput struct {
	InstitutionName string

	// AccountName is the account's own name or label as shown (Savings,
	// a card's product name); empty names it for the institution alone.
	AccountName string

	// AccountNumber is the account number as shown, or the digits of it
	// that are shown: masked digits (****1234) are allowed and marked
	// partial, letters are refused.
	AccountNumber          string
	IsAccountNumberPartial bool
	StatementAccountKind   StatementAccountKind
	CurrencyCode           string

	// BankCode is a bank's identifier where the list shows one, as an OFX
	// file's BANKID would carry it; empty otherwise.
	BankCode string

	TransactionRows []TransactionRow

	// LedgerBalanceAmount is the account's balance as of
	// LedgerBalanceOn, in the zone LedgerBalanceTimeZone names (the
	// person's when empty), as the newest list shows it.
	LedgerBalanceAmount   string
	LedgerBalanceOn       string
	LedgerBalanceTimeZone string

	// MonthlyTotals are the totals a card's list shows per month.
	MonthlyTotals []MonthlyTotal

	// FinanceAccountID names the existing account of imported statements
	// the rows go into, whatever number they show. With it the account
	// number may be left out: a list often shows none, or only a card's
	// last digits where the card's export knows it by another
	// identifier. IsNewAccount says the rows are of an account not
	// imported before, where an account at the same institution exists
	// that the rows' number does not match. At most one of the two.
	FinanceAccountID string
	IsNewAccount     bool
}

// TransactionRow is one transaction as read.
type TransactionRow struct {
	PostedOn    string
	Description string

	// Amount is signed, money out negative.
	Amount string

	// TransactionKind is one of TransactionKinds, or empty.
	TransactionKind string

	// RunningBalanceAmount is the balance the list shows after this
	// transaction, or empty.
	RunningBalanceAmount string

	// TotalMonth is the month heading the row was listed under, "2006-01",
	// when the list groups by a statement month rather than the day a
	// transaction posted; empty is the month it posted in.
	TotalMonth string
}

// MonthlyTotal is a month's total as a card's list shows it.
type MonthlyTotal struct {
	TotalMonth  string
	TotalAmount string
}

// CheckedTransactionRow is one row as checked and normalized.
type CheckedTransactionRow struct {
	// RowNumber is the row's place among the rows as they were given,
	// from one, which every refusal names it by.
	RowNumber int

	PostedOn             string
	Description          string
	Amount               string
	TransactionKind      string
	RunningBalanceAmount string
	TotalMonth           string

	amountValue  *big.Rat
	balanceValue *big.Rat
}

// TransactionRowsCheck is a set of transaction rows that adds up, ready to
// import, with what was checked.
type TransactionRowsCheck struct {
	InstitutionName        string
	AccountName            string
	AccountNumber          string
	IsAccountNumberPartial bool
	StatementAccountKind   StatementAccountKind
	CurrencyCode           string
	BankCode               string
	FinanceAccountID       string
	IsNewAccount           bool

	// TransactionRows are oldest first.
	TransactionRows []*CheckedTransactionRow

	FirstPostedOn string
	LastPostedOn  string

	// MoneyInAmount is the sum of the positive amounts and MoneyOutAmount
	// of the negative ones, negative.
	MoneyInAmount  string
	MoneyOutAmount string

	// CheckedBalanceCount is how many rows showed a running balance that
	// was checked against the rows before it; OpeningBalanceAmount is the
	// balance before the first row and ClosingBalanceAmount after the
	// last, both empty when no row showed one.
	CheckedBalanceCount  int
	OpeningBalanceAmount string
	ClosingBalanceAmount string

	// MatchedTotalMonths are the months whose rows came to their total.
	MatchedTotalMonths []string

	LedgerBalanceAmount   string
	LedgerBalanceOn       string
	LedgerBalanceTimeZone string
}

// NormalizeTransactionDescription is a description as it is kept: NFKC,
// so half-width katakana becomes full-width (ﾃﾞﾝｷ is デンキ) and
// full-width letters and digits become plain ones, and trimmed. Two reads
// of the same list then agree, and so do the identifiers made from them.
func NormalizeTransactionDescription(description string) string {
	return strings.TrimSpace(normalization.NFKC.String(description))
}

// accountNumberMaskRunes are what a list prints in place of the digits it
// hides.
var accountNumberMaskRunes = map[rune]bool{'*': true, '•': true, '●': true, '・': true, '·': true, 'x': true, 'X': true, '…': true}

// normalizeAccountNumber is the digits of an account number as shown, and
// whether some were masked. Spaces, dashes and dots between groups are
// left out; a letter is refused, since a card's brand or a made-up word
// is not its number, and an account keyed on one would have a mask nobody
// can read and match no file of the account.
func normalizeAccountNumber(accountNumber string) (string, bool, error) {
	var digits strings.Builder
	isMasked := false
	for _, character := range normalization.NFKC.String(accountNumber) {
		switch {
		case character >= '0' && character <= '9':
			digits.WriteRune(character)
		case character == ' ' || character == '-' || character == '.':
		case accountNumberMaskRunes[character]:
			isMasked = true
		default:
			return "", false, refuseRows("the account number %q holds %q; give only the digits shown, masked ones as they are (****1234), and never a word in place of a number", accountNumber, string(character))
		}
	}
	if digits.Len() == 0 {
		return "", false, refuseRows("the account number %q shows no digits; give the digits the list shows, name the account already imported that the rows are of by its finance account id, or ask the person", accountNumber)
	}
	return digits.String(), isMasked, nil
}

// parseRowAmount reads an amount as read off a list: a sign, digits and a
// point, NFKC first so full-width digits and minus read. A thousands
// separator is refused rather than guessed at, and so are more places than
// the currency has.
func parseRowAmount(text, currencyCode string) (*big.Rat, error) {
	text = strings.ReplaceAll(strings.TrimSpace(normalization.NFKC.String(text)), "−", "-")
	if strings.Contains(text, ",") {
		return nil, errors.New("write it without thousands separators")
	}
	amountValue, err := parseDecimal(text)
	if err != nil {
		return nil, errors.New("it is not a signed decimal amount")
	}
	if _, fraction, hasPoint := strings.Cut(text, "."); hasPoint && len(strings.TrimRight(fraction, "0")) > CurrencyMinorUnits(currencyCode) {
		return nil, fmt.Errorf("it has more places than %s has", currencyCode)
	}
	return amountValue, nil
}

// formatRowAmount writes an amount with as many places as its currency
// has, for a refusal or a summary.
func formatRowAmount(amountValue *big.Rat, currencyCode string) string {
	return amountValue.FloatString(CurrencyMinorUnits(currencyCode))
}

// rowLabel names a row in a refusal: its number as given, its day, what it
// says and its amount.
func rowLabel(row *CheckedTransactionRow, currencyCode string) string {
	return fmt.Sprintf("row %d (%s, %s, %s)", row.RowNumber, row.PostedOn, row.Description, formatRowAmount(row.amountValue, currencyCode))
}

// CheckTransactionRows checks one account's transaction rows and
// normalizes them, refusing the set with the first thing that is wrong:
// a malformed day or amount, a day after today (empty skips that check,
// for a caller that does not know the person's day), rows out of order,
// running balances that do not chain, or a month whose rows do not come to
// its total. Rows may be given oldest first or newest first, as a list
// shows them.
func CheckTransactionRows(input *TransactionRowsInput, today string) (*TransactionRowsCheck, error) {
	if input == nil {
		return nil, refuseRows("there is nothing to import")
	}
	check := &TransactionRowsCheck{
		InstitutionName: NormalizeTransactionDescription(input.InstitutionName),
		AccountName:     NormalizeTransactionDescription(input.AccountName),
		CurrencyCode:    strings.ToUpper(strings.TrimSpace(input.CurrencyCode)),
		BankCode:        strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(normalization.NFKC.String(input.BankCode), " ", ""), "-", "")),
	}
	if check.InstitutionName == "" {
		return nil, refuseRows("name the institution, as the list or the app shows it")
	}
	if len(check.CurrencyCode) != 3 || strings.IndexFunc(check.CurrencyCode, func(letter rune) bool { return letter < 'A' || letter > 'Z' }) >= 0 {
		return nil, refuseRows("the currency %q is not a currency code like JPY or USD", input.CurrencyCode)
	}
	switch input.StatementAccountKind {
	case StatementAccountKindBank, StatementAccountKindCard, StatementAccountKindOther:
		check.StatementAccountKind = input.StatementAccountKind
	default:
		return nil, refuseRows("the account kind %q is not bank, card or other", input.StatementAccountKind)
	}
	for _, character := range check.BankCode {
		if !unicode.IsLetter(character) && !unicode.IsDigit(character) {
			return nil, refuseRows("the bank code %q holds %q; give it as the list shows it, letters and digits", input.BankCode, string(character))
		}
	}
	check.FinanceAccountID = strings.TrimSpace(input.FinanceAccountID)
	check.IsNewAccount = input.IsNewAccount
	if check.FinanceAccountID != "" && check.IsNewAccount {
		return nil, refuseRows("name the existing account the rows are of, or say they are of a new account, not both")
	}
	// An account named by its id needs no number; one the list shows is
	// still read, for the card to name.
	if check.FinanceAccountID == "" || strings.TrimSpace(input.AccountNumber) != "" {
		accountNumber, isMasked, err := normalizeAccountNumber(input.AccountNumber)
		if err != nil {
			return nil, err
		}
		check.AccountNumber = accountNumber
		check.IsAccountNumberPartial = input.IsAccountNumberPartial || isMasked
	}

	if len(input.TransactionRows) == 0 {
		return nil, refuseRows("there are no rows")
	}
	if len(input.TransactionRows) > MaximumTransactionRows {
		return nil, refuseRows("%d rows are more than one import takes (%d); send them in parts", len(input.TransactionRows), MaximumTransactionRows)
	}
	rows := make([]*CheckedTransactionRow, 0, len(input.TransactionRows))
	for index, given := range input.TransactionRows {
		row := &CheckedTransactionRow{RowNumber: index + 1, Description: NormalizeTransactionDescription(given.Description)}
		postedOn, err := time.Parse(time.DateOnly, strings.TrimSpace(given.PostedOn))
		if err != nil {
			return nil, refuseRows("row %d: the day %q is not a day like 2026-09-30", row.RowNumber, given.PostedOn)
		}
		row.PostedOn = postedOn.Format(time.DateOnly)
		if today != "" && row.PostedOn > today {
			return nil, refuseRows("row %d: %s is after today (%s); a year read wrong, perhaps", row.RowNumber, row.PostedOn, today)
		}
		if row.Description == "" {
			return nil, refuseRows("row %d (%s) has no description; give what the list shows", row.RowNumber, row.PostedOn)
		}
		if row.amountValue, err = parseRowAmount(given.Amount, check.CurrencyCode); err != nil {
			return nil, refuseRows("row %d (%s, %s): the amount %q cannot be read: %s", row.RowNumber, row.PostedOn, row.Description, given.Amount, err)
		}
		row.Amount = FormatAmount(row.amountValue)
		if kind := strings.ToLower(strings.TrimSpace(given.TransactionKind)); kind != "" {
			if _, isKnown := transactionKindTypes[kind]; !isKnown {
				return nil, refuseRows("row %d: the transaction kind %q is not one of %s", row.RowNumber, given.TransactionKind, strings.Join(TransactionKinds(), ", "))
			}
			row.TransactionKind = kind
		}
		if balance := strings.TrimSpace(given.RunningBalanceAmount); balance != "" {
			if row.balanceValue, err = parseRowAmount(balance, check.CurrencyCode); err != nil {
				return nil, refuseRows("%s: the running balance %q cannot be read: %s", rowLabel(row, check.CurrencyCode), given.RunningBalanceAmount, err)
			}
			row.RunningBalanceAmount = FormatAmount(row.balanceValue)
		}
		if month := strings.TrimSpace(given.TotalMonth); month != "" {
			parsed, err := time.Parse("2006-01", month)
			if err != nil {
				return nil, refuseRows("row %d: the total month %q is not a month like 2026-09", row.RowNumber, given.TotalMonth)
			}
			row.TotalMonth = parsed.Format("2006-01")
		}
		rows = append(rows, row)
	}

	// Oldest first, or newest first as most lists show them; anything
	// else is rows read out of order, which no balance check can follow.
	isAscending, isDescending := true, true
	for index := 1; index < len(rows); index++ {
		if rows[index].PostedOn < rows[index-1].PostedOn {
			isAscending = false
		}
		if rows[index].PostedOn > rows[index-1].PostedOn {
			isDescending = false
		}
		if !isAscending && !isDescending {
			return nil, refuseRows("row %d (%s) and the rows before it are not in order of their days; give the rows oldest first, or newest first as the list shows them", rows[index].RowNumber, rows[index].PostedOn)
		}
	}
	reversed := make([]*CheckedTransactionRow, len(rows))
	for index, row := range rows {
		reversed[len(rows)-1-index] = row
	}
	if isDescending && !isAscending {
		rows = reversed
	}
	opening, closing, checkedCount, err := chainRunningBalances(rows, check.CurrencyCode)
	if err != nil && isAscending && isDescending {
		// Every row on one day says nothing about the order; the balances
		// are tried the other way too before refusing.
		if reversedOpening, reversedClosing, reversedCount, reversedErr := chainRunningBalances(reversed, check.CurrencyCode); reversedErr == nil {
			rows, opening, closing, checkedCount, err = reversed, reversedOpening, reversedClosing, reversedCount, nil
		}
	}
	if err != nil {
		return nil, err
	}
	check.TransactionRows = rows
	check.FirstPostedOn, check.LastPostedOn = rows[0].PostedOn, rows[len(rows)-1].PostedOn
	check.CheckedBalanceCount = checkedCount
	if opening != nil {
		check.OpeningBalanceAmount, check.ClosingBalanceAmount = FormatAmount(opening), FormatAmount(closing)
	}
	moneyIn, moneyOut := new(big.Rat), new(big.Rat)
	for _, row := range rows {
		if row.amountValue.Sign() > 0 {
			moneyIn.Add(moneyIn, row.amountValue)
		} else {
			moneyOut.Add(moneyOut, row.amountValue)
		}
	}
	check.MoneyInAmount, check.MoneyOutAmount = FormatAmount(moneyIn), FormatAmount(moneyOut)

	if check.MatchedTotalMonths, err = matchMonthlyTotals(rows, input.MonthlyTotals, check.CurrencyCode); err != nil {
		return nil, err
	}

	if amount := strings.TrimSpace(input.LedgerBalanceAmount); amount != "" {
		balanceValue, err := parseRowAmount(amount, check.CurrencyCode)
		if err != nil {
			return nil, refuseRows("the ledger balance %q cannot be read: %s", input.LedgerBalanceAmount, err)
		}
		balanceOn, err := time.Parse(time.DateOnly, strings.TrimSpace(input.LedgerBalanceOn))
		if err != nil {
			return nil, refuseRows("the ledger balance needs the day it is as of, like 2026-09-30, not %q", input.LedgerBalanceOn)
		}
		check.LedgerBalanceOn = balanceOn.Format(time.DateOnly)
		if today != "" && check.LedgerBalanceOn > today {
			return nil, refuseRows("the ledger balance is as of %s, after today (%s)", check.LedgerBalanceOn, today)
		}
		if zone := strings.TrimSpace(input.LedgerBalanceTimeZone); zone != "" {
			if _, err := time.LoadLocation(zone); err != nil {
				return nil, refuseRows("the time zone %q is not one like Asia/Tokyo", input.LedgerBalanceTimeZone)
			}
			check.LedgerBalanceTimeZone = zone
		}
		check.LedgerBalanceAmount = FormatAmount(balanceValue)
	} else if strings.TrimSpace(input.LedgerBalanceOn) != "" {
		return nil, refuseRows("a ledger balance day was given without its amount")
	}
	return check, nil
}

// chainRunningBalances checks the running balances of rows oldest first:
// each shown balance is the one before it plus the amounts since. It
// answers the balance before the first row and after the last (nil when
// no row shows one) and how many balances it checked. The first row that
// shows a balance is not checked against anything, but fixes the opening
// balance as its balance less its amount.
func chainRunningBalances(rows []*CheckedTransactionRow, currencyCode string) (*big.Rat, *big.Rat, int, error) {
	var opening, balance *big.Rat
	var lastShown *CheckedTransactionRow
	checkedCount := 0
	for index, row := range rows {
		if balance != nil {
			balance = new(big.Rat).Add(balance, row.amountValue)
		}
		if row.balanceValue == nil {
			continue
		}
		if balance == nil {
			// The rows before it, which show no balance, are carried back
			// from it.
			opening = new(big.Rat).Set(row.balanceValue)
			for _, earlier := range rows[:index+1] {
				opening.Sub(opening, earlier.amountValue)
			}
			balance = new(big.Rat).Set(row.balanceValue)
			lastShown = row
			continue
		}
		if balance.Cmp(row.balanceValue) != 0 {
			difference := new(big.Rat).Sub(row.balanceValue, balance)
			if lastShown == rows[index-1] {
				return nil, nil, 0, refuseRows("%s shows a balance of %s, but %s showed %s, and %s plus this amount is %s, %s off: "+
					"this amount or one of the two balances is misread, or rows are missing between them (two screenshots that do not overlap)",
					rowLabel(row, currencyCode), formatRowAmount(row.balanceValue, currencyCode), rowLabel(lastShown, currencyCode),
					formatRowAmount(lastShown.balanceValue, currencyCode), formatRowAmount(lastShown.balanceValue, currencyCode),
					formatRowAmount(balance, currencyCode), formatRowAmount(difference, currencyCode))
			}
			return nil, nil, 0, refuseRows("%s shows a balance of %s, but %s showed %s, and the amounts since then come to %s, %s off: "+
				"an amount or a balance is misread, or rows are missing between row %d and row %d",
				rowLabel(row, currencyCode), formatRowAmount(row.balanceValue, currencyCode), rowLabel(lastShown, currencyCode),
				formatRowAmount(lastShown.balanceValue, currencyCode), formatRowAmount(balance, currencyCode), formatRowAmount(difference, currencyCode),
				lastShown.RowNumber, row.RowNumber)
		}
		checkedCount++
		lastShown = row
	}
	return opening, balance, checkedCount, nil
}

// matchMonthlyTotals checks each month's rows come to the total its list
// shows, in either sign, since a card's list shows what was spent as a
// positive total while the rows are money out. A month with a total and
// no rows, or with rows and no total once totals are given, is refused:
// the first is rows missing, and the second a month that was not checked,
// often one cut off at the bottom of a screenshot. It answers the months
// that matched.
func matchMonthlyTotals(rows []*CheckedTransactionRow, totals []MonthlyTotal, currencyCode string) ([]string, error) {
	if len(totals) == 0 {
		return nil, nil
	}
	sums := map[string]*big.Rat{}
	rowCounts := map[string]int{}
	for _, row := range rows {
		month := row.TotalMonth
		if month == "" {
			month = row.PostedOn[:len("2006-01")]
		}
		if sums[month] == nil {
			sums[month] = new(big.Rat)
		}
		sums[month].Add(sums[month], row.amountValue)
		rowCounts[month]++
	}
	isTotaled := map[string]bool{}
	var matched []string
	for _, total := range totals {
		parsed, err := time.Parse("2006-01", strings.TrimSpace(total.TotalMonth))
		if err != nil {
			return nil, refuseRows("the monthly total's month %q is not a month like 2026-09", total.TotalMonth)
		}
		month := parsed.Format("2006-01")
		if isTotaled[month] {
			return nil, refuseRows("%s has two monthly totals", month)
		}
		isTotaled[month] = true
		totalValue, err := parseRowAmount(total.TotalAmount, currencyCode)
		if err != nil {
			return nil, refuseRows("the total of %s, %q, cannot be read: %s", month, total.TotalAmount, err)
		}
		sum := sums[month]
		if sum == nil {
			return nil, refuseRows("%s shows a total of %s but no row of it was given", month, formatRowAmount(totalValue, currencyCode))
		}
		if sum.Cmp(totalValue) != 0 && sum.Cmp(new(big.Rat).Neg(totalValue)) != 0 {
			difference := new(big.Rat).Sub(new(big.Rat).Abs(totalValue), new(big.Rat).Abs(sum))
			return nil, refuseRows("the %d rows of %s come to %s, but the month's total shows %s, %s apart: a row of %s is misread or missing",
				rowCounts[month], month, formatRowAmount(sum, currencyCode), formatRowAmount(totalValue, currencyCode),
				formatRowAmount(difference, currencyCode), month)
		}
		matched = append(matched, month)
	}
	var unchecked []string
	for month := range sums {
		if !isTotaled[month] {
			unchecked = append(unchecked, month)
		}
	}
	if len(unchecked) > 0 {
		sort.Strings(unchecked)
		return nil, refuseRows("rows of %s were given without the month's total; give its total as shown, or leave a month that is cut off out and ask for the rest",
			strings.Join(unchecked, ", "))
	}
	sort.Strings(matched)
	return matched, nil
}

// VerificationSummary says what was checked, in a sentence.
func (self *TransactionRowsCheck) VerificationSummary() string {
	var said []string
	if self.CheckedBalanceCount > 0 || self.OpeningBalanceAmount != "" {
		opening, _ := parseDecimal(self.OpeningBalanceAmount)
		closing, _ := parseDecimal(self.ClosingBalanceAmount)
		if self.CheckedBalanceCount == 0 {
			said = append(said, fmt.Sprintf("one running balance was given, so the balances could not be chained (%s before the first row)",
				formatRowAmount(opening, self.CurrencyCode)))
		} else {
			said = append(said, fmt.Sprintf("running balances chained on %d rows, from %s to %s", self.CheckedBalanceCount+1,
				formatRowAmount(opening, self.CurrencyCode), formatRowAmount(closing, self.CurrencyCode)))
		}
	}
	if len(self.MatchedTotalMonths) > 0 {
		said = append(said, "monthly totals matched for "+strings.Join(self.MatchedTotalMonths, ", "))
	}
	if len(said) == 0 {
		return "no running balances or monthly totals were given, so nothing could be checked against the list"
	}
	return strings.Join(said, "; ")
}

// FormatTransactionRowsAmount writes an amount as an import of transaction
// rows says it: with as many places as its currency has.
func FormatTransactionRowsAmount(amount, currencyCode string) string {
	amountValue, err := parseDecimal(amount)
	if err != nil {
		return amount
	}
	return formatRowAmount(amountValue, currencyCode)
}

// NewTransactionRowsImport turns checked transaction rows into what a sync
// writes, as NewStatementImport turns a statement: the account keyed by
// its kind, its bank code and the digits of its number, so an OFX file for
// the same account number is the same finance account, and each
// transaction known by its day, amount and description, numbered when the
// same one appears twice on a day, as a statement's transaction without a
// FITID is. It builds every row, and the transaction for the row at an
// index of check.TransactionRows is at the same index of what it answers;
// PlanTransactionRowsImport is what leaves out the rows an account holds
// already. location is the person's zone, for a ledger balance that names
// none.
func NewTransactionRowsImport(accountKey []byte, check *TransactionRowsCheck, location *time.Location, existingAccounts []ExistingStatementAccount) (*StatementImport, error) {
	if check == nil || len(check.TransactionRows) == 0 {
		return nil, errors.New("finance: there are no checked rows to import")
	}
	statement := transactionRowsStatement(check)
	statement.StartedOn, statement.EndedOn = check.FirstPostedOn, check.LastPostedOn
	options := statementImportOptions{
		accountMetadata: map[string]any{
			statementImportOriginField: StatementImportOriginTransactionRows, "statementAccountKind": string(check.StatementAccountKind),
			"accountLabel": check.AccountName, "isAccountNumberPartial": check.IsAccountNumberPartial,
		},
		transactionMetadata: map[string]any{statementImportOriginField: StatementImportOriginTransactionRows},
	}
	if check.StatementAccountKind == StatementAccountKindOther {
		options.accountKind = AccountKindOther
	}
	switch {
	case check.AccountName == "":
	case strings.Contains(strings.ToLower(check.AccountName), strings.ToLower(check.InstitutionName)):
		options.accountName = check.AccountName
	default:
		options.accountName = check.InstitutionName + " " + check.AccountName
	}
	if check.LedgerBalanceAmount != "" {
		if check.LedgerBalanceTimeZone != "" {
			if named, err := time.LoadLocation(check.LedgerBalanceTimeZone); err == nil {
				location = named
			}
		}
		if location == nil {
			location = time.UTC
		}
		asOf, err := time.ParseInLocation(time.DateOnly, check.LedgerBalanceOn, location)
		if err != nil {
			return nil, err
		}
		statement.LedgerBalance = &ofx.Balance{Amount: check.LedgerBalanceAmount, AsOf: asOf, AsOfDay: check.LedgerBalanceOn}
	}
	for _, row := range check.TransactionRows {
		statement.Transactions = append(statement.Transactions, &ofx.Transaction{
			TransactionType: transactionKindTypes[row.TransactionKind], PostedOn: row.PostedOn, Amount: row.Amount, Name: row.Description,
		})
	}
	document := &ofx.Document{InstitutionOrganization: check.InstitutionName}
	return newStatementImport(accountKey, document, statement, existingAccounts, options)
}

// transactionRowsStatement is the statement rows are keyed as: a card's
// as a card statement's, a bank's and anything else's as a bank
// statement's, the bank code as its BANKID and the digits of the number
// as its ACCTID.
func transactionRowsStatement(check *TransactionRowsCheck) *ofx.Statement {
	statement := &ofx.Statement{StatementKind: ofx.StatementKindBank, CurrencyCode: check.CurrencyCode, AccountID: check.AccountNumber, BankID: check.BankCode}
	if check.StatementAccountKind == StatementAccountKindCard {
		statement.StatementKind = ofx.StatementKindCreditCard
	}
	return statement
}

// TransactionRowsAccountMatch is how the account transaction rows go into
// was found.
type TransactionRowsAccountMatch string

const (
	// TransactionRowsAccountMatchFinanceAccountID is the account the
	// caller named by its id.
	TransactionRowsAccountMatchFinanceAccountID TransactionRowsAccountMatch = "finance_account_id"

	// TransactionRowsAccountMatchAccountNumber is the account the rows'
	// number keys, as a file's or earlier rows' with the same number.
	TransactionRowsAccountMatchAccountNumber TransactionRowsAccountMatch = "account_number"

	// TransactionRowsAccountMatchAccountMask is the one account at the
	// institution, of the kind and currency, whose mask the rows' number
	// ends with.
	TransactionRowsAccountMatchAccountMask TransactionRowsAccountMatch = "account_mask"

	// TransactionRowsAccountMatchNewAccount is an account made for the
	// rows.
	TransactionRowsAccountMatchNewAccount TransactionRowsAccountMatch = "new_account"
)

// TransactionRowsAccount is the account transaction rows go into: an
// existing account of imported statements, or a new one when
// ExistingAccount is nil.
type TransactionRowsAccount struct {
	ExistingAccount *ExistingStatementAccount
	AccountMatch    TransactionRowsAccountMatch
}

// isCardAccount says an existing statement account is a card's, as rows
// of kind card are: a credit account that is no bank's line of credit.
func (self *ExistingStatementAccount) isCardAccount() bool {
	return self.AccountKind == AccountKindCredit && !IsStatementCreditLine(self.ProviderMetadata)
}

// displayName is the account as a refusal or a card names it: its name
// and the end of its identifier, as the import's answer names it.
func (self *ExistingStatementAccount) displayName() string {
	name := self.AccountName
	if self.AccountMask != "" {
		name += " ··" + self.AccountMask
	}
	return name
}

// normalizeInstitutionName is an institution's name as two are compared:
// NFKC, lower case, letters and digits only, so "Example Bank, N.A." and
// "EXAMPLE BANK NA" are one name.
func normalizeInstitutionName(institutionName string) string {
	var normalized strings.Builder
	for _, character := range strings.ToLower(normalization.NFKC.String(institutionName)) {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			normalized.WriteRune(character)
		}
	}
	return normalized.String()
}

// isSameInstitution says two institution names may name one institution:
// one normalized name holds the other, since an app and its export name
// it differently ("Example Bank" and "Example Bank NA"). A name that is
// not known is the same only as another that is not: every name holds the
// empty one, and an account whose institution nobody wrote down is no
// evidence the rows are of it.
func isSameInstitution(existingInstitutionName, institutionName string) bool {
	existing, given := normalizeInstitutionName(existingInstitutionName), normalizeInstitutionName(institutionName)
	if existing == "" || given == "" {
		return existing == given
	}
	return strings.Contains(existing, given) || strings.Contains(given, existing)
}

// isMaskMatch says an account's mask and the end of the number shown
// agree: the shorter ends the longer, since a list may show fewer digits
// than a mask keeps.
func isMaskMatch(accountMask, shownMask string) bool {
	if accountMask == "" || shownMask == "" {
		return false
	}
	return strings.HasSuffix(accountMask, shownMask) || strings.HasSuffix(shownMask, accountMask)
}

// accountFitsRows refuses an account the rows cannot be of: another
// currency, or a card for a bank's rows and the other way round.
func accountFitsRows(existing *ExistingStatementAccount, check *TransactionRowsCheck) error {
	if existing.CurrencyCode != "" && !strings.EqualFold(existing.CurrencyCode, check.CurrencyCode) {
		return refuseRows("%s is in %s, but the rows are in %s; name the account they are of", existing.displayName(), existing.CurrencyCode, check.CurrencyCode)
	}
	if existing.isCardAccount() != (check.StatementAccountKind == StatementAccountKindCard) {
		return refuseRows("%s is not of the kind %s; name the account the rows are of", existing.displayName(), check.StatementAccountKind)
	}
	return nil
}

// candidateList names accounts for a refusal: each by its name, the end
// of its identifier and its id, which the caller passes back.
func candidateList(candidates []*ExistingStatementAccount) string {
	named := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		named = append(named, fmt.Sprintf("%s (finance account id %s)", candidate.displayName(), candidate.FinanceAccountID))
	}
	return strings.Join(named, ", ")
}

// accountsAlreadyThere names, for a refusal, the accounts at the rows'
// institution and those whose institution is not known.
func accountsAlreadyThere(institutionName string, institutionAccounts, unknownInstitutionAccounts []*ExistingStatementAccount) string {
	var said []string
	if len(institutionAccounts) > 0 {
		said = append(said, institutionName+" already has "+candidateList(institutionAccounts))
	}
	if len(unknownInstitutionAccounts) > 0 {
		said = append(said, "there is already "+candidateList(unknownInstitutionAccounts)+" at an institution that is not known")
	}
	return strings.Join(said, ", and ")
}

// ChooseTransactionRowsAccount is the account transaction rows go into,
// among the existing accounts of imported statements (only those: the
// caller refuses an id of any other account before this):
//
//   - the account check.FinanceAccountID names, when it names one;
//   - else the account the rows' number keys, as a file's or earlier
//     rows' with the same number and bank code;
//   - else the one account at the same institution, of the same kind and
//     currency, whose mask the shown digits end with, so a screenshot
//     showing an account's last digits lands in the account a file made.
//     Two such accounts are refused, naming them, for the person to
//     choose, and so is one when the caller says the rows are of a new
//     account (check.IsNewAccount), since the person said so about the
//     accounts a refusal named and this one matches;
//   - else, when no account of the kind and currency at the same
//     institution or at one not known exists, or the caller says the
//     rows are of a new account, a new account. An account at the
//     institution whose mask does not match is refused rather than passed
//     over, since a card's export can know it by an identifier that is
//     not its number, and the rows would then be counted twice in two
//     accounts. An account whose institution is not known is refused the
//     same way, and never chosen by its mask.
func ChooseTransactionRowsAccount(accountKey []byte, check *TransactionRowsCheck, existingAccounts []ExistingStatementAccount) (*TransactionRowsAccount, error) {
	if check.FinanceAccountID != "" {
		for index := range existingAccounts {
			existing := &existingAccounts[index]
			if existing.FinanceAccountID != check.FinanceAccountID {
				continue
			}
			if err := accountFitsRows(existing, check); err != nil {
				return nil, err
			}
			return &TransactionRowsAccount{ExistingAccount: existing, AccountMatch: TransactionRowsAccountMatchFinanceAccountID}, nil
		}
		return nil, refuseRows("%s is not an account of imported statements; the accounts listing gives their ids", check.FinanceAccountID)
	}
	if check.AccountNumber == "" {
		return nil, refuseRows("give the account number shown, or the finance account id of the account the rows are of")
	}
	keyedProviderAccountId, _ := resolveStatementAccountID(accountKey, transactionRowsStatement(check), existingAccounts)
	for index := range existingAccounts {
		existing := &existingAccounts[index]
		if existing.ProviderAccountID != keyedProviderAccountId {
			continue
		}
		if err := accountFitsRows(existing, check); err != nil {
			return nil, err
		}
		return &TransactionRowsAccount{ExistingAccount: existing, AccountMatch: TransactionRowsAccountMatchAccountNumber}, nil
	}
	shownMask := StatementAccountMask(check.AccountNumber)
	// An account whose institution is not known (a file without its
	// optional FI block) is never chosen by its mask, since nothing says
	// it is at this institution; but it is named rather than passed over,
	// since nothing says it is not, and passing it over would make a
	// second account beside it.
	var maskMatches, institutionMatches, unknownInstitutionMaskMatches, unknownInstitutionAccounts []*ExistingStatementAccount
	for index := range existingAccounts {
		existing := &existingAccounts[index]
		if !strings.EqualFold(existing.CurrencyCode, check.CurrencyCode) || existing.isCardAccount() != (check.StatementAccountKind == StatementAccountKindCard) {
			continue
		}
		isMasked := isMaskMatch(existing.AccountMask, shownMask)
		switch {
		case isSameInstitution(existing.InstitutionName, check.InstitutionName):
			institutionMatches = append(institutionMatches, existing)
			if isMasked {
				maskMatches = append(maskMatches, existing)
			}
		case normalizeInstitutionName(existing.InstitutionName) == "":
			unknownInstitutionAccounts = append(unknownInstitutionAccounts, existing)
			if isMasked {
				unknownInstitutionMaskMatches = append(unknownInstitutionMaskMatches, existing)
			}
		}
	}
	switch {
	case len(maskMatches) == 1 && !check.IsNewAccount:
		return &TransactionRowsAccount{ExistingAccount: maskMatches[0], AccountMatch: TransactionRowsAccountMatchAccountMask}, nil
	case len(maskMatches) == 1:
		// The person's word that the account is new was given about
		// accounts the server named; one whose mask the digits end with is
		// more likely the account they are of than a new one beside it.
		return nil, refuseRows("the rows were said to be of an account not imported before, but the number ending %s matches %s; "+
			"ask the person whether the rows are of it, and if they are, give its finance account id", shownMask, candidateList(maskMatches))
	case len(maskMatches) > 1:
		return nil, refuseRows("the number ending %s could be any of %s; ask the person which one the rows are of and give its finance account id",
			shownMask, candidateList(maskMatches))
	case len(unknownInstitutionMaskMatches) > 0:
		return nil, refuseRows("the number ending %s matches %s, whose institution is not known; "+
			"ask the person whether the rows are of it, and if they are, give its finance account id", shownMask, candidateList(unknownInstitutionMaskMatches))
	case len(institutionMatches)+len(unknownInstitutionAccounts) > 0 && !check.IsNewAccount:
		return nil, refuseRows("no account ending %s was imported before, but %s, and an export may know the account by another number; "+
			"if the rows are of one of these, give its finance account id, and if they are of an account not imported before, say it is a new account; ask the person when unsure",
			shownMask, accountsAlreadyThere(check.InstitutionName, institutionMatches, unknownInstitutionAccounts))
	}
	return &TransactionRowsAccount{AccountMatch: TransactionRowsAccountMatchNewAccount}, nil
}

// NearbyStoredTransactionDays is how many days either side of a new row a
// stored transaction of the same amount is pointed out as possibly the
// same one, posted on another day. It is only pointed out, never matched:
// a genuine second charge of the same amount days apart is common.
const NearbyStoredTransactionDays = 3

// StoredTransaction is a transaction the account holds already, as rows,
// or a file's transactions, are matched against it.
type StoredTransaction struct {
	ProviderTransactionID string
	PostedOn              string
	Amount                string
	Description           string

	// IsFromTransactionRows says an import of transaction rows wrote it
	// (IsTransactionRowsMetadata).
	IsFromTransactionRows bool
}

// TransactionRowsPlan is what importing transaction rows would do: the
// account, the rows it does not hold yet and the rows it does, and what
// a sync writes for the new ones.
type TransactionRowsPlan struct {
	// FinanceAccountID is the existing account's, empty for a new one;
	// AccountName is the account's name with the end of its identifier.
	FinanceAccountID string
	AccountName      string
	AccountMatch     TransactionRowsAccountMatch

	// NewTransactionRows are those the account does not hold, oldest
	// first, and PresentTransactionRows those it does.
	NewTransactionRows     []*CheckedTransactionRow
	PresentTransactionRows []*CheckedTransactionRow

	// HasNearbyStoredTransaction marks, by row number, the new rows with
	// a stored transaction of the same amount within
	// NearbyStoredTransactionDays that no row matched.
	HasNearbyStoredTransaction map[int]bool

	// The days and the money of the new rows alone; the days are empty
	// when there are none.
	FirstPostedOn  string
	LastPostedOn   string
	MoneyInAmount  string
	MoneyOutAmount string

	// StatementImport is what a sync writes: the account, its balance and
	// the new rows.
	StatementImport *StatementImport
}

// transactionMatchKey is what a row and a stored transaction are matched
// by: the day and the exact amount.
func transactionMatchKey(postedOn string, amountValue *big.Rat) string {
	return postedOn + "\x00" + amountValue.RatString()
}

// descriptionMatchKey is a description as a row and a stored transaction
// are paired by within one day and amount: NFKC, lower case, without
// spaces.
func descriptionMatchKey(description string) string {
	return strings.Join(strings.Fields(strings.ToLower(normalization.NFKC.String(description))), "")
}

// storedMatchEntry is a stored transaction while rows are matched against
// it, and whether a row has claimed it.
type storedMatchEntry struct {
	storedTransaction StoredTransaction
	amountValue       *big.Rat
	postedOn          time.Time
	isClaimed         bool
}

// matchStoredTransactions says which rows the account holds already, and
// which new rows have a stored transaction of the same amount nearby. The
// rows and the stored transactions are matched as multisets of (day,
// amount): of k rows with a day and amount that s stored transactions
// have, min(k, s) are present and the rest new, whatever the descriptions
// say, since a file and a screenshot write them differently. Which rows of
// the k are taken as present changes no count, only which ones are
// written: first a row whose identifier a stored transaction has (rows
// sent before), then one whose description is a stored one's, then in
// order.
func matchStoredTransactions(rows []*CheckedTransactionRow, providerTransactionIds []string, storedTransactions []StoredTransaction) ([]bool, map[int]bool, error) {
	entriesByKey := map[string][]*storedMatchEntry{}
	var entries []*storedMatchEntry
	for _, storedTransaction := range storedTransactions {
		amountValue, err := parseDecimal(storedTransaction.Amount)
		if err != nil {
			return nil, nil, fmt.Errorf("finance: a stored transaction's amount %q: %w", storedTransaction.Amount, err)
		}
		postedOn, err := time.Parse(time.DateOnly, storedTransaction.PostedOn)
		if err != nil {
			return nil, nil, fmt.Errorf("finance: a stored transaction's day %q: %w", storedTransaction.PostedOn, err)
		}
		entry := &storedMatchEntry{storedTransaction: storedTransaction, amountValue: amountValue, postedOn: postedOn}
		key := transactionMatchKey(storedTransaction.PostedOn, amountValue)
		entriesByKey[key] = append(entriesByKey[key], entry)
		entries = append(entries, entry)
	}
	isPresent := make([]bool, len(rows))
	claim := func(index int, isWanted func(entry *storedMatchEntry) bool) {
		for _, entry := range entriesByKey[transactionMatchKey(rows[index].PostedOn, rows[index].amountValue)] {
			if !entry.isClaimed && isWanted(entry) {
				entry.isClaimed, isPresent[index] = true, true
				return
			}
		}
	}
	for index := range rows {
		claim(index, func(entry *storedMatchEntry) bool {
			return entry.storedTransaction.ProviderTransactionID == providerTransactionIds[index]
		})
	}
	for index, row := range rows {
		if !isPresent[index] {
			claim(index, func(entry *storedMatchEntry) bool {
				return descriptionMatchKey(entry.storedTransaction.Description) == descriptionMatchKey(row.Description)
			})
		}
	}
	for index := range rows {
		if !isPresent[index] {
			claim(index, func(*storedMatchEntry) bool { return true })
		}
	}
	hasNearby := map[int]bool{}
	for index, row := range rows {
		if isPresent[index] {
			continue
		}
		postedOn, _ := time.Parse(time.DateOnly, row.PostedOn)
		for _, entry := range entries {
			dayCount := int(entry.postedOn.Sub(postedOn).Hours() / 24)
			if entry.isClaimed || entry.amountValue.Cmp(row.amountValue) != 0 || dayCount < -NearbyStoredTransactionDays || dayCount > NearbyStoredTransactionDays {
				continue
			}
			entry.isClaimed = true
			hasNearby[row.RowNumber] = true
			break
		}
	}
	return isPresent, hasNearby, nil
}

// keepExistingAccount points what a sync writes at an existing account and
// leaves the account as it is but for its balance: its name, mask, kind
// and metadata are a file's, or the person's, and rows read off a picture
// know less about the account than either. A balance given is recorded
// in the metadata as a statement's is.
func keepExistingAccount(statementImport *StatementImport, existing *ExistingStatementAccount) error {
	account := &statementImport.SyncResult.Accounts[0]
	accountMetadata := map[string]any{}
	if len(existing.ProviderMetadata) > 0 {
		if err := json.Unmarshal(existing.ProviderMetadata, &accountMetadata); err != nil {
			accountMetadata = map[string]any{}
		}
	}
	if account.CurrentBalance != "" {
		accountMetadata["ledgerBalance"] = account.CurrentBalance
		accountMetadata["ledgerBalanceOn"] = statementImport.BalanceOn
		// The rows say no available balance, and the newer balance
		// written empties the stored one.
		delete(accountMetadata, "availableBalance")
		delete(accountMetadata, "availableBalanceOn")
	}
	encoded, err := json.Marshal(accountMetadata)
	if err != nil {
		return err
	}
	account.ProviderAccountID, account.AccountName, account.AccountMask = existing.ProviderAccountID, existing.AccountName, existing.AccountMask
	if existing.AccountKind != "" {
		account.AccountKind = existing.AccountKind
	}
	account.ProviderMetadata = encoded
	for index := range statementImport.SyncResult.Added {
		statementImport.SyncResult.Added[index].ProviderAccountID = existing.ProviderAccountID
	}
	return nil
}

// PlanTransactionRowsImport is what importing checked transaction rows
// into the chosen account would do (ChooseTransactionRowsAccount). Every
// row is built, so each keeps the identifier it has when the whole set is
// sent, and then the rows the account already holds are left out:
// storedTransactions are the account's transactions around the rows'
// days, of any origin (a file's with FITIDs, a file's without, earlier
// rows), matched as matchStoredTransactions says. A new account holds
// nothing, and every row is new. The running balances and monthly totals
// were checked over every row sent, before this.
func PlanTransactionRowsImport(accountKey []byte, check *TransactionRowsCheck, location *time.Location, existingAccounts []ExistingStatementAccount,
	account *TransactionRowsAccount, storedTransactions []StoredTransaction) (*TransactionRowsPlan, error) {
	statementImport, err := NewTransactionRowsImport(accountKey, check, location, existingAccounts)
	if err != nil {
		return nil, err
	}
	added := statementImport.SyncResult.Added
	if len(added) != len(check.TransactionRows) {
		return nil, errors.New("finance: the rows built are not the rows checked")
	}
	plan := &TransactionRowsPlan{AccountMatch: account.AccountMatch, HasNearbyStoredTransaction: map[int]bool{}}
	isPresent := make([]bool, len(check.TransactionRows))
	if account.ExistingAccount != nil {
		if err := keepExistingAccount(statementImport, account.ExistingAccount); err != nil {
			return nil, err
		}
		plan.FinanceAccountID = account.ExistingAccount.FinanceAccountID
		providerTransactionIds := make([]string, len(added))
		for index, transaction := range added {
			providerTransactionIds[index] = transaction.ProviderTransactionID
		}
		if isPresent, plan.HasNearbyStoredTransaction, err = matchStoredTransactions(check.TransactionRows, providerTransactionIds, storedTransactions); err != nil {
			return nil, err
		}
	}
	built := statementImport.SyncResult.Accounts[0]
	plan.AccountName = built.AccountName
	if built.AccountMask != "" {
		plan.AccountName += " ··" + built.AccountMask
	}
	kept := make([]Transaction, 0, len(added))
	moneyIn, moneyOut := new(big.Rat), new(big.Rat)
	for index, row := range check.TransactionRows {
		if isPresent[index] {
			plan.PresentTransactionRows = append(plan.PresentTransactionRows, row)
			continue
		}
		plan.NewTransactionRows = append(plan.NewTransactionRows, row)
		kept = append(kept, added[index])
		if plan.FirstPostedOn == "" {
			plan.FirstPostedOn = row.PostedOn
		}
		plan.LastPostedOn = row.PostedOn
		if row.amountValue.Sign() > 0 {
			moneyIn.Add(moneyIn, row.amountValue)
		} else {
			moneyOut.Add(moneyOut, row.amountValue)
		}
	}
	plan.MoneyInAmount, plan.MoneyOutAmount = FormatAmount(moneyIn), FormatAmount(moneyOut)
	statementImport.SyncResult.Added = kept
	statementImport.FirstPostedOn = plan.FirstPostedOn
	statementImport.GeneratedIDCount = len(kept)
	statementImport.PresentTransactionCount = len(plan.PresentTransactionRows)
	plan.StatementImport = statementImport
	return plan, nil
}

// LeaveOutStoredTransactionRows leaves out of a file's import the
// transactions its account holds already as transaction rows, the way
// PlanTransactionRowsImport leaves out rows a file wrote. A row is stored
// under an identifier made from its day, amount and description as the
// picture showed it, and a file knows the same transaction by its FITID,
// or by a hash of its description as the file writes it, so matching by
// identifier alone would store it a second time.
//
// storedTransactions are the account's transactions over the file's days.
// A file transaction whose identifier is stored is that transaction, as
// always, and a row it names that way is taken. The rest are matched to
// the stored rows left, of transaction rows only, by posted day and exact
// amount as multisets: of k file transactions with a day and amount that
// s such rows have, min(k, s) are already there and are left out, and the
// others are written. Which of the k are left out changes no count: first
// one whose description is a row's, then in order. One left out is not
// stored, so importing the same file again finds it present the same way,
// and the import counts it as already here.
func LeaveOutStoredTransactionRows(statementImport *StatementImport, storedTransactions []StoredTransaction) error {
	added := statementImport.SyncResult.Added
	isAddedId := make(map[string]bool, len(added))
	for _, transaction := range added {
		isAddedId[transaction.ProviderTransactionID] = true
	}
	isStoredId := make(map[string]bool, len(storedTransactions))
	entriesByKey := map[string][]*storedMatchEntry{}
	for _, storedTransaction := range storedTransactions {
		isStoredId[storedTransaction.ProviderTransactionID] = true
		if !storedTransaction.IsFromTransactionRows || isAddedId[storedTransaction.ProviderTransactionID] {
			continue
		}
		amountValue, err := parseDecimal(storedTransaction.Amount)
		if err != nil {
			return fmt.Errorf("finance: a stored transaction's amount %q: %w", storedTransaction.Amount, err)
		}
		key := transactionMatchKey(storedTransaction.PostedOn, amountValue)
		entriesByKey[key] = append(entriesByKey[key], &storedMatchEntry{storedTransaction: storedTransaction, amountValue: amountValue})
	}
	if len(entriesByKey) == 0 {
		return nil
	}
	matchKeys := make([]string, len(added))
	for index, transaction := range added {
		if isStoredId[transaction.ProviderTransactionID] {
			continue
		}
		amountValue, err := parseDecimal(transaction.Amount)
		if err != nil {
			return fmt.Errorf("finance: a transaction's amount %q: %w", transaction.Amount, err)
		}
		matchKeys[index] = transactionMatchKey(transaction.PostedOn, amountValue)
	}
	isPresent := make([]bool, len(added))
	claim := func(index int, isWanted func(entry *storedMatchEntry) bool) {
		if matchKeys[index] == "" || isPresent[index] {
			return
		}
		for _, entry := range entriesByKey[matchKeys[index]] {
			if !entry.isClaimed && isWanted(entry) {
				entry.isClaimed, isPresent[index] = true, true
				return
			}
		}
	}
	for index, transaction := range added {
		claim(index, func(entry *storedMatchEntry) bool {
			return descriptionMatchKey(entry.storedTransaction.Description) == descriptionMatchKey(transaction.Description)
		})
	}
	for index := range added {
		claim(index, func(*storedMatchEntry) bool { return true })
	}
	kept := make([]Transaction, 0, len(added))
	for index, transaction := range added {
		if isPresent[index] {
			statementImport.PresentTransactionCount++
			continue
		}
		kept = append(kept, transaction)
	}
	statementImport.SyncResult.Added = kept
	return nil
}
