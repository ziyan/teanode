package finance

import (
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
// account metadata's, for an import of transaction rows.
const StatementImportOriginTransactionRows = "transaction_rows"

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
		return "", false, refuseRows("the account number %q shows no digits; give the digits the list shows, or ask the person for them", accountNumber)
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
	accountNumber, isMasked, err := normalizeAccountNumber(input.AccountNumber)
	if err != nil {
		return nil, err
	}
	check.AccountNumber = accountNumber
	check.IsAccountNumberPartial = input.IsAccountNumberPartial || isMasked

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
// FITID is. Sending rows again, or rows that overlap ones sent before,
// adds nothing twice. location is the person's zone, for a ledger balance
// that names none.
func NewTransactionRowsImport(accountKey []byte, check *TransactionRowsCheck, location *time.Location, existingAccounts []ExistingStatementAccount) (*StatementImport, error) {
	if check == nil || len(check.TransactionRows) == 0 {
		return nil, errors.New("finance: there are no checked rows to import")
	}
	statement := &ofx.Statement{
		StatementKind: ofx.StatementKindBank, CurrencyCode: check.CurrencyCode, AccountID: check.AccountNumber, BankID: check.BankCode,
		StartedOn: check.FirstPostedOn, EndedOn: check.LastPostedOn,
	}
	options := statementImportOptions{accountMetadata: map[string]any{
		"statementImportOrigin": StatementImportOriginTransactionRows, "statementAccountKind": string(check.StatementAccountKind),
		"accountLabel": check.AccountName, "isAccountNumberPartial": check.IsAccountNumberPartial,
	}}
	switch check.StatementAccountKind {
	case StatementAccountKindCard:
		statement.StatementKind = ofx.StatementKindCreditCard
	case StatementAccountKindOther:
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
