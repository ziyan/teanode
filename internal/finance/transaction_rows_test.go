package finance

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/finance/ofx"
)

// inventedBankInput is an invented bank account's list as read off two
// screenshots, oldest first: a running balance on each row, starting from
// 150000 before the first.
func inventedBankInput() *TransactionRowsInput {
	return &TransactionRowsInput{
		InstitutionName: "Example Bank", AccountName: "Savings", AccountNumber: "123-4567", StatementAccountKind: StatementAccountKindBank,
		CurrencyCode: "jpy", BankCode: "0999",
		TransactionRows: []TransactionRow{
			{PostedOn: "2026-08-28", Description: "サンプル商店", Amount: "-1500", RunningBalanceAmount: "148500"},
			{PostedOn: "2026-08-31", Description: "ﾘｿｸ", Amount: "12", TransactionKind: "interest", RunningBalanceAmount: "148512"},
			{PostedOn: "2026-09-15", Description: "ｷﾕｳﾖ ｻﾝﾌﾟﾙ", Amount: "200000", TransactionKind: "deposit", RunningBalanceAmount: "348512"},
			{PostedOn: "2026-09-30", Description: "ﾃﾞﾝｷﾀﾞｲ", Amount: "-4500", RunningBalanceAmount: "344012"},
		},
		LedgerBalanceAmount: "344012", LedgerBalanceOn: "2026-09-30", LedgerBalanceTimeZone: "Asia/Tokyo",
	}
}

// inventedCardInput is an invented card's list: each month's purchases
// under a heading with the month's total, shown positive.
func inventedCardInput() *TransactionRowsInput {
	return &TransactionRowsInput{
		InstitutionName: "Example Card Company", AccountNumber: "****9876", StatementAccountKind: StatementAccountKindCard, CurrencyCode: "JPY",
		TransactionRows: []TransactionRow{
			{PostedOn: "2026-09-20", Description: "ｻﾝﾌﾟﾙｽﾄｱ", Amount: "-3200"},
			{PostedOn: "2026-09-02", Description: "Example Books", Amount: "-1800"},
			{PostedOn: "2026-08-25", Description: "ｻﾝﾌﾟﾙｽﾄｱ", Amount: "-2400"},
			{PostedOn: "2026-08-10", Description: "Example Cafe", Amount: "-600"},
			{PostedOn: "2026-08-10", Description: "Example Cafe", Amount: "100", TransactionKind: "refund"},
		},
		MonthlyTotals: []MonthlyTotal{{TotalMonth: "2026-08", TotalAmount: "2900"}, {TotalMonth: "2026-09", TotalAmount: "5000"}},
	}
}

func isRefusal(err error, said ...string) bool {
	if !errors.Is(err, ErrTransactionRowsRefused) {
		return false
	}
	for _, words := range said {
		if !strings.Contains(err.Error(), words) {
			return false
		}
	}
	return true
}

// A bank list whose balances chain is taken, normalized, with the opening
// and closing balances, money in and out, and what was checked.
func TestCheckTransactionRowsChainsABank(test *testing.T) {
	test.Parallel()
	check, err := CheckTransactionRows(inventedBankInput(), "2026-10-01")
	if err != nil {
		test.Fatal(err)
	}
	if check.AccountNumber != "1234567" || check.IsAccountNumberPartial || check.CurrencyCode != "JPY" || check.BankCode != "0999" {
		test.Errorf("the account %+v", check)
	}
	if check.CheckedBalanceCount != 3 || check.OpeningBalanceAmount != "150000.0000" || check.ClosingBalanceAmount != "344012.0000" {
		test.Errorf("the balances %+v", check)
	}
	if check.MoneyInAmount != "200012.0000" || check.MoneyOutAmount != "-6000.0000" || check.FirstPostedOn != "2026-08-28" || check.LastPostedOn != "2026-09-30" {
		test.Errorf("the totals %+v", check)
	}
	if summary := check.VerificationSummary(); summary != "running balances chained on 4 rows, from 150000 to 344012" {
		test.Errorf("the summary %q", summary)
	}

	// Newest first, as an app lists them, is the same set.
	input := inventedBankInput()
	for left, right := 0, len(input.TransactionRows)-1; left < right; left, right = left+1, right-1 {
		input.TransactionRows[left], input.TransactionRows[right] = input.TransactionRows[right], input.TransactionRows[left]
	}
	reversed, err := CheckTransactionRows(input, "2026-10-01")
	if err != nil || reversed.TransactionRows[0].PostedOn != "2026-08-28" || reversed.TransactionRows[0].RowNumber != 4 || reversed.CheckedBalanceCount != 3 {
		test.Errorf("newest first: %+v %v", reversed, err)
	}
}

// A misread amount is found at its row, with the balances on either side
// and how far off it is.
func TestCheckTransactionRowsFindsAWrongAmount(test *testing.T) {
	test.Parallel()
	input := inventedBankInput()
	input.TransactionRows[2].Amount = "20000"
	_, err := CheckTransactionRows(input, "")
	if !isRefusal(err, "row 3 (2026-09-15, キユウヨ サンプル, 20000)", "shows a balance of 348512", "row 2 (2026-08-31", "180000 off") {
		test.Errorf("a wrong amount answered %v", err)
	}
}

// Rows missing between two screenshots that do not overlap show as a gap
// between the rows whose balances do not chain, and when rows between
// them show no balance, the refusal names both ends.
func TestCheckTransactionRowsFindsAGap(test *testing.T) {
	test.Parallel()
	input := inventedBankInput()
	input.TransactionRows = append(input.TransactionRows[:1], input.TransactionRows[2:]...)
	if _, err := CheckTransactionRows(input, ""); !isRefusal(err, "row 2 (2026-09-15", "rows are missing between them", "12 off") {
		test.Errorf("a missing row answered %v", err)
	}

	input = inventedBankInput()
	input.TransactionRows[1].RunningBalanceAmount = ""
	input.TransactionRows[2].RunningBalanceAmount = ""
	input.TransactionRows[3].Amount = "-4000"
	if _, err := CheckTransactionRows(input, ""); !isRefusal(err, "row 4 (2026-09-30", "rows are missing between row 1 and row 4") {
		test.Errorf("a gap among rows without balances answered %v", err)
	}
}

// A card's months each come to their total, shown positive; a month that
// does not is refused by its month, and so is a month given without its
// total or a total without its rows.
func TestCheckTransactionRowsMatchesMonthlyTotals(test *testing.T) {
	test.Parallel()
	check, err := CheckTransactionRows(inventedCardInput(), "2026-10-01")
	if err != nil {
		test.Fatal(err)
	}
	if strings.Join(check.MatchedTotalMonths, ",") != "2026-08,2026-09" || check.AccountNumber != "9876" || !check.IsAccountNumberPartial {
		test.Errorf("%+v", check)
	}
	if summary := check.VerificationSummary(); summary != "monthly totals matched for 2026-08, 2026-09" {
		test.Errorf("the summary %q", summary)
	}

	wrong := inventedCardInput()
	wrong.TransactionRows[3].Amount = "-6000"
	if _, err := CheckTransactionRows(wrong, ""); !isRefusal(err, "the 3 rows of 2026-08 come to -8300", "shows 2900", "5400 apart") {
		test.Errorf("a wrong month answered %v", err)
	}
	cutOff := inventedCardInput()
	cutOff.MonthlyTotals = cutOff.MonthlyTotals[1:]
	if _, err := CheckTransactionRows(cutOff, ""); !isRefusal(err, "rows of 2026-08 were given without the month's total") {
		test.Errorf("a month without its total answered %v", err)
	}
	rowless := inventedCardInput()
	rowless.MonthlyTotals = append(rowless.MonthlyTotals, MonthlyTotal{TotalMonth: "2026-07", TotalAmount: "100"})
	if _, err := CheckTransactionRows(rowless, ""); !isRefusal(err, "2026-07 shows a total of 100 but no row of it") {
		test.Errorf("a total without rows answered %v", err)
	}
	// A list that groups by statement month says so per row.
	grouped := inventedCardInput()
	grouped.TransactionRows[1].TotalMonth = "2026-08"
	grouped.MonthlyTotals = []MonthlyTotal{{TotalMonth: "2026-08", TotalAmount: "4700"}, {TotalMonth: "2026-09", TotalAmount: "3200"}}
	if _, err := CheckTransactionRows(grouped, ""); err != nil {
		test.Errorf("rows under their statement month answered %v", err)
	}
}

// A description is NFKC and trimmed: half-width katakana is full-width,
// its sound marks joined, full-width letters and digits plain; kanji kept.
func TestNormalizeTransactionDescription(test *testing.T) {
	test.Parallel()
	for given, wanted := range map[string]string{
		" ｻﾝﾌﾟﾙｶﾞｽ ":  "サンプルガス",
		"ｺｰﾋｰ":        "コーヒー",
		"ＥＸＡＭＰＬＥ　１２３": "EXAMPLE 123",
		"見本電力":        "見本電力",
	} {
		if have := NormalizeTransactionDescription(given); have != wanted {
			test.Errorf("%q is %q, not %q", given, have, wanted)
		}
	}
	check, err := CheckTransactionRows(inventedBankInput(), "")
	if err != nil || check.TransactionRows[3].Description != "デンキダイ" {
		test.Errorf("%+v %v", check, err)
	}
}

// Malformed rows are refused by their row: a day after today, a day that
// is no day, an amount with a separator or more places than the currency
// has, an unknown kind, rows out of order, and an account number that is
// a word.
func TestCheckTransactionRowsRefusesWhatCannotBeRead(test *testing.T) {
	test.Parallel()
	for name, change := range map[string]struct {
		edit func(input *TransactionRowsInput)
		said string
	}{
		"future":      {func(input *TransactionRowsInput) { input.TransactionRows[3].PostedOn = "2026-10-02" }, "row 4: 2026-10-02 is after today"},
		"day":         {func(input *TransactionRowsInput) { input.TransactionRows[0].PostedOn = "26.08.28" }, "row 1: the day \"26.08.28\""},
		"separator":   {func(input *TransactionRowsInput) { input.TransactionRows[2].Amount = "200,000" }, "without thousands separators"},
		"places":      {func(input *TransactionRowsInput) { input.TransactionRows[0].Amount = "-1500.5" }, "more places than JPY has"},
		"kind":        {func(input *TransactionRowsInput) { input.TransactionRows[0].TransactionKind = "groceries" }, "row 1: the transaction kind \"groceries\""},
		"order":       {func(input *TransactionRowsInput) { input.TransactionRows[1].PostedOn = "2026-09-20" }, "row 3 (2026-09-15) and the rows before it are not in order"},
		"brand":       {func(input *TransactionRowsInput) { input.AccountNumber = "VISA" }, "never a word in place of a number"},
		"no number":   {func(input *TransactionRowsInput) { input.AccountNumber = "****" }, "shows no digits"},
		"currency":    {func(input *TransactionRowsInput) { input.CurrencyCode = "円" }, "not a currency code"},
		"kind of acc": {func(input *TransactionRowsInput) { input.StatementAccountKind = "savings" }, "not bank, card or other"},
		"no rows":     {func(input *TransactionRowsInput) { input.TransactionRows = nil }, "there are no rows"},
		"ledger day":  {func(input *TransactionRowsInput) { input.LedgerBalanceOn = "" }, "the ledger balance needs the day"},
		"zone":        {func(input *TransactionRowsInput) { input.LedgerBalanceTimeZone = "Somewhere/Else" }, "the time zone"},
	} {
		input := inventedBankInput()
		change.edit(input)
		if _, err := CheckTransactionRows(input, "2026-10-01"); !isRefusal(err, change.said) {
			test.Errorf("%s answered %v, not %q", name, err, change.said)
		}
	}
	// Full-width digits and a full-width minus read; masked digits are
	// kept as the digits shown and marked partial.
	input := inventedBankInput()
	input.TransactionRows[0].Amount = "－１５００"
	input.AccountNumber = "•••4567"
	if check, err := CheckTransactionRows(input, ""); err != nil || check.AccountNumber != "4567" || !check.IsAccountNumberPartial {
		test.Errorf("%+v %v", check, err)
	}
}

// Rows become what a sync writes: an account keyed as a statement's
// account is, so an OFX file of the same account number is the same
// account, named for the institution and its label, with its balance; and
// each transaction known by its day, amount and description, as an OFX
// transaction without a FITID is, so sending rows that overlap adds
// nothing twice.
func TestTransactionRowsImportIsAStatementImport(test *testing.T) {
	test.Parallel()
	check, err := CheckTransactionRows(inventedBankInput(), "")
	if err != nil {
		test.Fatal(err)
	}
	rowsImport, err := NewTransactionRowsImport(inventedAccountKey, check, time.UTC, nil)
	if err != nil {
		test.Fatal(err)
	}
	account := rowsImport.SyncResult.Accounts[0]
	if account.AccountName != "Example Bank Savings" || account.AccountMask != "4567" || account.AccountKind != AccountKindDepository ||
		account.CurrencyCode != "JPY" || account.CurrentBalance != "344012.0000" || rowsImport.BalanceOn != "2026-09-30" {
		test.Errorf("the account %+v, balance on %s", account, rowsImport.BalanceOn)
	}
	tokyo, _ := time.LoadLocation("Asia/Tokyo")
	if !account.BalanceAt.Equal(time.Date(2026, 9, 30, 0, 0, 0, 0, tokyo)) {
		test.Errorf("the balance is as of %s", account.BalanceAt)
	}
	var metadata map[string]any
	_ = json.Unmarshal(account.ProviderMetadata, &metadata)
	if metadata["statementImportOrigin"] != StatementImportOriginTransactionRows || metadata["institutionOrganization"] != "Example Bank" {
		test.Errorf("the metadata %v", metadata)
	}
	if strings.Contains(string(account.ProviderMetadata), "1234567") {
		test.Errorf("the account number was kept: %s", account.ProviderMetadata)
	}

	// The same account in an OFX file.
	statement := &ofx.Statement{StatementKind: ofx.StatementKindBank, CurrencyCode: "JPY", AccountID: "1234567", BankID: "0999",
		Transactions: []*ofx.Transaction{{TransactionType: "DEBIT", PostedOn: "2026-09-30", Amount: "-4500", Name: "デンキダイ"}}}
	fileImport, err := NewStatementImport(inventedAccountKey, &ofx.Document{InstitutionOrganization: "Example Bank"}, statement, nil)
	if err != nil {
		test.Fatal(err)
	}
	if fileImport.SyncResult.Accounts[0].ProviderAccountID != account.ProviderAccountID {
		test.Error("the OFX file of the same account number is another account")
	}
	if fileImport.SyncResult.Added[0].ProviderTransactionID != rowsImport.SyncResult.Added[3].ProviderTransactionID {
		test.Error("the same transaction in the file has another identifier")
	}

	// A later set that overlaps the first finds the same identifiers for
	// the rows both hold.
	later := inventedBankInput()
	later.TransactionRows = append(later.TransactionRows[2:], TransactionRow{PostedOn: "2026-10-01", Description: "ﾘｿｸ", Amount: "3", RunningBalanceAmount: "344015"})
	laterCheck, err := CheckTransactionRows(later, "")
	if err != nil {
		test.Fatal(err)
	}
	laterImport, err := NewTransactionRowsImport(inventedAccountKey, laterCheck, time.UTC, nil)
	if err != nil {
		test.Fatal(err)
	}
	if laterImport.SyncResult.Added[0].ProviderTransactionID != rowsImport.SyncResult.Added[2].ProviderTransactionID ||
		laterImport.SyncResult.Added[1].ProviderTransactionID != rowsImport.SyncResult.Added[3].ProviderTransactionID ||
		laterImport.SyncResult.Added[2].ProviderTransactionID == rowsImport.SyncResult.Added[1].ProviderTransactionID {
		test.Error("overlapping rows have other identifiers")
	}
	// The interest row is the provider category mapping's to place.
	if rowsImport.SyncResult.Added[1].ProviderCategoryDetailed != "ofx:INT" || rowsImport.SyncResult.Added[1].ProviderCategoryPrimary != StatementCategoryPrimaryBankAccount {
		test.Errorf("the interest row %+v", rowsImport.SyncResult.Added[1])
	}
}

// A card is a credit account, "other" an account of kind other keyed as a
// bank's, and two identical charges on one day are two transactions.
func TestTransactionRowsImportOfACardAndOther(test *testing.T) {
	test.Parallel()
	check, err := CheckTransactionRows(inventedCardInput(), "")
	if err != nil {
		test.Fatal(err)
	}
	cardImport, err := NewTransactionRowsImport(inventedAccountKey, check, time.UTC, nil)
	if err != nil {
		test.Fatal(err)
	}
	if account := cardImport.SyncResult.Accounts[0]; account.AccountKind != AccountKindCredit || account.AccountName != "Example Card Company" || account.AccountMask != "9876" {
		test.Errorf("the card %+v", account)
	}

	twice := inventedCardInput()
	twice.TransactionRows = append(twice.TransactionRows, TransactionRow{PostedOn: "2026-08-10", Description: "Example Cafe", Amount: "-600"})
	twice.MonthlyTotals[0].TotalAmount = "3500"
	twiceCheck, err := CheckTransactionRows(twice, "")
	if err != nil {
		test.Fatal(err)
	}
	twiceImport, err := NewTransactionRowsImport(inventedAccountKey, twiceCheck, time.UTC, nil)
	if err != nil {
		test.Fatal(err)
	}
	identifiers := map[string]bool{}
	for _, transaction := range twiceImport.SyncResult.Added {
		identifiers[transaction.ProviderTransactionID] = true
	}
	if len(identifiers) != len(twiceImport.SyncResult.Added) {
		test.Error("two identical charges on one day share an identifier")
	}

	other := inventedBankInput()
	other.StatementAccountKind = StatementAccountKindOther
	otherCheck, _ := CheckTransactionRows(other, "")
	otherImport, err := NewTransactionRowsImport(inventedAccountKey, otherCheck, time.UTC, nil)
	if err != nil || otherImport.SyncResult.Accounts[0].AccountKind != AccountKindOther {
		test.Errorf("other %+v %v", otherImport, err)
	}
}

// The name the person gave a statement account outlasts the next import,
// a file's or a set of rows'.
func TestStatementImportKeepsThePersonsName(test *testing.T) {
	test.Parallel()
	check, _ := CheckTransactionRows(inventedBankInput(), "")
	first, err := NewTransactionRowsImport(inventedAccountKey, check, time.UTC, nil)
	if err != nil {
		test.Fatal(err)
	}
	existing := []ExistingStatementAccount{{
		ProviderAccountID: first.SyncResult.Accounts[0].ProviderAccountID,
		ProviderMetadata:  json.RawMessage(`{"personAccountName":"Rainy day fund"}`),
	}}
	again, err := NewTransactionRowsImport(inventedAccountKey, check, time.UTC, existing)
	if err != nil {
		test.Fatal(err)
	}
	if account := again.SyncResult.Accounts[0]; account.AccountName != "Rainy day fund" || StatementPersonAccountName(account.ProviderMetadata) != "Rainy day fund" {
		test.Errorf("the account %+v", account)
	}
}

// inventedExistingCard is an invented card a file made, known by an opaque
// identifier ending 77cc.
func inventedExistingCard() ExistingStatementAccount {
	return ExistingStatementAccount{
		ProviderAccountID: "ofx-invented-card", FinanceAccountID: "account-card", AccountName: "Example Card Company", AccountMask: "77cc",
		AccountKind: AccountKindCredit, CurrencyCode: "JPY", InstitutionName: "EXAMPLE CARD COMPANY, LTD.",
		ProviderMetadata: json.RawMessage(`{"statementKind":"creditcard","accountMask":"77cc","institutionOrganization":"EXAMPLE CARD COMPANY, LTD."}`),
	}
}

// The account rows go into: the one named; the one whose last digits the
// number ends with, at an institution named a little differently; two
// such refused naming both; one at the institution that matches neither
// refused unless the rows are said to be of a new account; and another
// currency or kind is no candidate.
func TestChooseTransactionRowsAccount(test *testing.T) {
	test.Parallel()
	check, err := CheckTransactionRows(inventedCardInput(), "")
	if err != nil {
		test.Fatal(err)
	}
	card := inventedExistingCard()
	if _, err := ChooseTransactionRowsAccount(inventedAccountKey, check, []ExistingStatementAccount{card}); !isRefusal(err, "Example Card Company ··77cc (finance account id account-card)", "new account") {
		test.Errorf("a card at the institution whose mask does not match answered %v", err)
	}
	named := *check
	named.FinanceAccountID = "account-card"
	chosen, err := ChooseTransactionRowsAccount(inventedAccountKey, &named, []ExistingStatementAccount{card})
	if err != nil || chosen.ExistingAccount.FinanceAccountID != "account-card" || chosen.AccountMatch != TransactionRowsAccountMatchFinanceAccountID {
		test.Errorf("named %+v %v", chosen, err)
	}
	isNew := *check
	isNew.IsNewAccount = true
	if chosen, err := ChooseTransactionRowsAccount(inventedAccountKey, &isNew, []ExistingStatementAccount{card}); err != nil || chosen.ExistingAccount != nil {
		test.Errorf("a new account %+v %v", chosen, err)
	}

	byMask := card
	byMask.AccountMask = "9876"
	chosen, err = ChooseTransactionRowsAccount(inventedAccountKey, &isNew, []ExistingStatementAccount{byMask})
	if err != nil || chosen.ExistingAccount == nil || chosen.AccountMatch != TransactionRowsAccountMatchAccountMask {
		test.Errorf("a partial number whose digits match a mask, even said to be new, %+v %v", chosen, err)
	}
	other := byMask
	other.FinanceAccountID, other.ProviderAccountID = "account-other", "ofx-invented-other"
	if _, err := ChooseTransactionRowsAccount(inventedAccountKey, check, []ExistingStatementAccount{byMask, other}); !isRefusal(err, "account-card", "account-other") {
		test.Errorf("two cards ending 9876 answered %v", err)
	}
	inDollars, bank := byMask, byMask
	inDollars.CurrencyCode, inDollars.FinanceAccountID = "USD", "account-dollars"
	bank.AccountKind = AccountKindDepository
	if chosen, err := ChooseTransactionRowsAccount(inventedAccountKey, check, []ExistingStatementAccount{inDollars, bank}); err != nil || chosen.ExistingAccount != nil {
		test.Errorf("another currency or kind was a candidate: %+v %v", chosen, err)
	}
	if _, err := ChooseTransactionRowsAccount(inventedAccountKey, &named, []ExistingStatementAccount{inDollars}); !isRefusal(err, "not an account of imported statements") {
		test.Errorf("an id not among the accounts answered %v", err)
	}
	inDollars.FinanceAccountID = "account-card"
	if _, err := ChooseTransactionRowsAccount(inventedAccountKey, &named, []ExistingStatementAccount{inDollars}); !isRefusal(err, "USD") {
		test.Errorf("a named account in another currency answered %v", err)
	}
}

// Rows are matched against what the account holds by day and amount, as
// multisets: of two rows of one day and amount where one is stored, one
// is new; descriptions do not matter, and a row whose identifier is
// stored is the one taken as present. A new row with a stored transaction
// of its amount two days off is marked, and still new.
func TestPlanTransactionRowsImportLeavesOutWhatIsStored(test *testing.T) {
	test.Parallel()
	input := inventedCardInput()
	input.TransactionRows = append(input.TransactionRows, TransactionRow{PostedOn: "2026-08-10", Description: "Example Cafe", Amount: "-600"})
	input.MonthlyTotals[0].TotalAmount = "3500"
	check, err := CheckTransactionRows(input, "")
	if err != nil {
		test.Fatal(err)
	}
	card := inventedExistingCard()
	built, err := NewTransactionRowsImport(inventedAccountKey, check, time.UTC, nil)
	if err != nil {
		test.Fatal(err)
	}
	// Oldest first: 08-10 -600, 08-10 +100, 08-10 -600, 08-25 -2400,
	// 09-02 -1800, 09-20 -3200.
	secondCafeId := built.SyncResult.Added[2].ProviderTransactionID
	stored := []StoredTransaction{
		{ProviderTransactionID: secondCafeId, PostedOn: "2026-08-10", Amount: "-600.0000", Description: "EXAMPLE CAFE TOKYO"},
		{ProviderTransactionID: "fit-invented-2", PostedOn: "2026-08-25", Amount: "-2400.0000", Description: "SAMPLE STORE"},
		{ProviderTransactionID: "fit-invented-3", PostedOn: "2026-09-18", Amount: "-3200.0000", Description: "SAMPLE STORE"},
	}
	plan, err := PlanTransactionRowsImport(inventedAccountKey, check, time.UTC, []ExistingStatementAccount{card},
		&TransactionRowsAccount{ExistingAccount: &card, AccountMatch: TransactionRowsAccountMatchFinanceAccountID}, stored)
	if err != nil {
		test.Fatal(err)
	}
	if len(plan.NewTransactionRows) != 4 || len(plan.PresentTransactionRows) != 2 || plan.FinanceAccountID != "account-card" || plan.AccountName != "Example Card Company ··77cc" {
		test.Fatalf("plan %+v", plan)
	}
	added := plan.StatementImport.SyncResult.Added
	if len(added) != 4 || plan.StatementImport.PresentTransactionCount != 2 {
		test.Fatalf("written %+v", plan.StatementImport)
	}
	for _, transaction := range added {
		if transaction.ProviderTransactionID == secondCafeId {
			test.Error("the row whose identifier is stored was written again")
		}
		if transaction.ProviderAccountID != "ofx-invented-card" {
			test.Errorf("a row was written to %s", transaction.ProviderAccountID)
		}
	}
	if account := plan.StatementImport.SyncResult.Accounts[0]; account.ProviderAccountID != "ofx-invented-card" || account.AccountMask != "77cc" || account.AccountName != "Example Card Company" {
		test.Errorf("the account %+v", account)
	}
	var nearbyRowNumbers []int
	for _, row := range plan.NewTransactionRows {
		if plan.HasNearbyStoredTransaction[row.RowNumber] {
			nearbyRowNumbers = append(nearbyRowNumbers, row.RowNumber)
		}
	}
	if len(nearbyRowNumbers) != 1 || nearbyRowNumbers[0] != 1 {
		test.Errorf("rows marked near a stored transaction: %v", nearbyRowNumbers)
	}
	if plan.FirstPostedOn != "2026-08-10" || plan.LastPostedOn != "2026-09-20" || plan.MoneyInAmount != "100.0000" || plan.MoneyOutAmount != "-5600.0000" {
		test.Errorf("the new rows' days and money %+v", plan)
	}

	// A new account holds nothing, and every row is new.
	fresh, err := PlanTransactionRowsImport(inventedAccountKey, check, time.UTC, nil, &TransactionRowsAccount{AccountMatch: TransactionRowsAccountMatchNewAccount}, nil)
	if err != nil || len(fresh.NewTransactionRows) != 6 || fresh.FinanceAccountID != "" || fresh.AccountName != "Example Card Company ··9876" {
		test.Errorf("a new account %+v %v", fresh, err)
	}
}
