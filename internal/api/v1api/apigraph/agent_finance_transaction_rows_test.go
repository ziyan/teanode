package apigraph

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// inventedBankRowArguments is an invented bank account's list as the agent
// read it off a screenshot, newest first, half-width katakana as the app
// writes it, a running balance on every row.
func inventedBankRowArguments() ImportTransactionsArguments {
	return ImportTransactionsArguments{
		InstitutionName: "Example Bank", AccountName: "Savings", AccountNumber: "1234567", StatementAccountKind: "bank",
		CurrencyCode: "JPY", BankCode: "0999",
		TransactionRows: []TransactionRow{
			{PostedOn: "2026-09-30", Description: "ﾃﾞﾝｷﾀﾞｲ", Amount: "-4500", RunningBalanceAmount: "344000"},
			{PostedOn: "2026-09-15", Description: "ｷﾕｳﾖ ｻﾝﾌﾟﾙ", Amount: "200000", TransactionKind: "deposit", RunningBalanceAmount: "348500"},
			{PostedOn: "2026-09-01", Description: "サンプル商店", Amount: "-1500", RunningBalanceAmount: "148500"},
		},
		LedgerBalanceAmount: "344000", LedgerBalanceOn: "2026-09-30", LedgerBalanceTimeZone: "Asia/Tokyo",
	}
}

// inventedBankStatement is the same invented account in an OFX file with
// its whole number, holding one of the same transactions without a FITID.
const inventedBankStatement = `<?xml version="1.0" encoding="UTF-8"?>
<?OFX OFXHEADER="200" VERSION="220" SECURITY="NONE" OLDFILEUID="NONE" NEWFILEUID="NONE"?>
<OFX><SIGNONMSGSRSV1><SONRS><STATUS><CODE>0</CODE><SEVERITY>INFO</SEVERITY></STATUS><DTSERVER>20261001</DTSERVER><LANGUAGE>JPN</LANGUAGE>
<FI><ORG>Example Bank</ORG></FI></SONRS></SIGNONMSGSRSV1>
<BANKMSGSRSV1><STMTTRNRS><TRNUID>0</TRNUID><STATUS><CODE>0</CODE><SEVERITY>INFO</SEVERITY></STATUS><STMTRS><CURDEF>JPY</CURDEF>
<BANKACCTFROM><BANKID>0999</BANKID><ACCTID>1234567</ACCTID><ACCTTYPE>SAVINGS</ACCTTYPE></BANKACCTFROM>
<BANKTRANLIST><DTSTART>20260930</DTSTART><DTEND>20260930</DTEND>
<STMTTRN><TRNTYPE>DEBIT</TRNTYPE><DTPOSTED>20260930</DTPOSTED><TRNAMT>-4500</TRNAMT><NAME>デンキダイ</NAME></STMTTRN>
</BANKTRANLIST></STMTRS></STMTTRNRS></BANKMSGSRSV1></OFX>
`

// importRows imports transaction rows as the owner.
func (self *financeFixture) importRows(test *testing.T, arguments ImportTransactionsArguments) (*models.FinanceStatementImport, error) {
	test.Helper()
	var imported *models.FinanceStatementImport
	var err error
	self.as(test, self.owner, func(ctx context.Context, tx db.Transaction) {
		imported, err = self.resolver.ImportTransactions(ctx, arguments)
	})
	return imported, err
}

// statementAccounts is the owner's accounts of imported statements.
func (self *financeFixture) statementAccounts(test *testing.T) []*FinanceAccountView {
	test.Helper()
	var found []*FinanceAccountView
	self.as(test, self.owner, func(ctx context.Context, tx db.Transaction) {
		accounts, err := self.resolver.FinanceAccounts(ctx, FinanceAccountsArguments{})
		if err != nil {
			test.Fatal(err)
		}
		for _, account := range accounts {
			if account.ProviderKind == "statement" {
				found = append(found, account)
			}
		}
	})
	return found
}

// Rows are imported into the statement source, normalized, with the
// account's balance; rows that overlap add only what is new; an OFX file
// of the same account number lands in the same account and adds nothing
// the rows had; and rows that do not add up, or reach past today, are
// refused with nothing written.
func TestImportTransactionsFromTheAPI(test *testing.T) {
	fixture, store, _ := newStatementFixture(test)
	first, err := fixture.importRows(test, inventedBankRowArguments())
	if err != nil {
		test.Fatalf("ImportTransactions: %s", err)
	}
	if first.AddedTransactionCount != 3 || first.StatementImportOrigin != models.StatementImportOriginTransactionRows ||
		len(first.FinanceAccountNames) != 1 || first.FinanceAccountNames[0] != "Example Bank Savings ··4567" {
		test.Errorf("first %+v", first)
	}
	accounts := fixture.statementAccounts(test)
	if len(accounts) != 1 || accounts[0].CurrentBalance != "344000.0000" || accounts[0].InstitutionName != "Example Bank" {
		test.Fatalf("accounts %+v", accounts)
	}
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		page, err := fixture.resolver.FinanceTransactions(ctx, FinanceTransactionsArguments{FinanceAccountID: accounts[0].ID})
		if err != nil || len(page.FinanceTransactions) != 3 || page.FinanceTransactions[0].Description != "デンキダイ" {
			test.Errorf("transactions %+v %v", page, err)
		}
	})

	// The next screenshots overlap the last two rows and add one.
	later := inventedBankRowArguments()
	later.TransactionRows = append([]TransactionRow{{PostedOn: "2026-10-01", Description: "ﾘｿｸ", Amount: "3", TransactionKind: "interest", RunningBalanceAmount: "344003"}},
		later.TransactionRows[:2]...)
	later.LedgerBalanceAmount, later.LedgerBalanceOn = "344003", "2026-10-01"
	again, err := fixture.importRows(test, later)
	if err != nil || again.AddedTransactionCount != 1 || again.UnchangedTransactionCount != 2 {
		test.Errorf("again %+v %v", again, err)
	}

	// The OFX file of the same account, with the whole number.
	var attachment *models.AgentAttachment
	dbtest.RunTransactionOn(test, fixture.database, func(tx db.Transaction) {
		if attachment, err = tx.CreateAgentAttachment(&models.AgentAttachment{
			AgentID: fixture.ownerAgent.ID, Name: "savings.ofx", ContentType: "application/x-ofx", Size: int64(len(inventedBankStatement)),
		}); err != nil {
			test.Fatal(err)
		}
	})
	if err := store.PutFile(context.Background(), attachment.ID, []byte(inventedBankStatement)); err != nil {
		test.Fatal(err)
	}
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		imported, err := fixture.resolver.ImportStatement(ctx, ImportStatementArguments{AgentAttachmentID: attachment.ID})
		// The same transaction, updated in place: the file says its type.
		if err != nil || imported.AddedTransactionCount != 0 || imported.UpdatedTransactionCount != 1 {
			test.Errorf("the file %+v %v", imported, err)
		}
	})
	if accounts := fixture.statementAccounts(test); len(accounts) != 1 {
		test.Errorf("the file made another account: %+v", accounts)
	}

	misread := inventedBankRowArguments()
	misread.TransactionRows[1].Amount = "20000"
	misread.TransactionRows[0].Description = "ｼﾝｷ"
	if _, err := fixture.importRows(test, misread); !errors.Is(err, api.ErrInvalidArguments) || !strings.Contains(err.Error(), "row 2 (2026-09-15") {
		test.Errorf("rows that do not add up answered %v", err)
	}
	future := inventedBankRowArguments()
	future.TransactionRows[0].PostedOn = "2099-09-30"
	if _, err := fixture.importRows(test, future); !errors.Is(err, api.ErrInvalidArguments) || !strings.Contains(err.Error(), "after today") {
		test.Errorf("a day after today answered %v", err)
	}
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		page, err := fixture.resolver.FinanceTransactions(ctx, FinanceTransactionsArguments{Text: "シンキ"})
		if err != nil || len(page.FinanceTransactions) != 0 {
			test.Errorf("a refused set wrote %+v %v", page, err)
		}
	})
}

// An imported account is renamed, its asset with it, and keeps the name
// through the next import; it is deleted with its transactions and its
// asset, and the transfer one of its transactions was paired in is let go
// of. A provider's account is neither renamed nor deleted here, and
// another person's is not found.
func TestRenameAndDeleteStatementAccount(test *testing.T) {
	fixture, _, _ := newStatementFixture(test)
	_, providerAccount, providerTransactions := fixture.seedFinanceSource(test)

	// The card's payment matches the checking account's 42.17 out, so it
	// is paired as a transfer when it is imported.
	card := ImportTransactionsArguments{
		InstitutionName: "Example Card Company", AccountNumber: "****9876", StatementAccountKind: "card", CurrencyCode: "USD",
		TransactionRows: []TransactionRow{
			{PostedOn: "2026-09-12", Description: "THANK YOU PAYMENT", Amount: "42.17", TransactionKind: "payment"},
			{PostedOn: "2026-09-10", Description: "EXAMPLE BOOKS", Amount: "-18.00", TransactionKind: "purchase"},
		},
		LedgerBalanceAmount: "-18.00", LedgerBalanceOn: "2026-09-20",
	}
	if _, err := fixture.importRows(test, card); err != nil {
		test.Fatalf("ImportTransactions: %s", err)
	}
	cardAccount := fixture.statementAccounts(test)[0]
	isTransfer := func(financeTransactionId string) bool {
		var transfer bool
		dbtest.RunTransactionOn(test, fixture.database, func(tx db.Transaction) {
			found, err := tx.GetFinanceTransaction(fixture.ownerAgent.ID, financeTransactionId)
			if err != nil {
				test.Fatal(err)
			}
			category, err := tx.EnsureTransferSpendingCategory(fixture.ownerAgent.ID)
			if err != nil {
				test.Fatal(err)
			}
			transfer = found.SpendingCategoryID == category.ID
		})
		return transfer
	}
	var groceryId string
	for _, transaction := range providerTransactions {
		if transaction.Amount == "-42.1700" {
			groceryId = transaction.ID
		}
	}
	if !isTransfer(groceryId) {
		test.Fatal("the checking side of the card payment was not paired")
	}

	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		renamed, err := fixture.resolver.RenameStatementAccount(ctx, RenameStatementAccountArguments{FinanceAccountID: cardAccount.ID, AccountName: "Everyday card"})
		if err != nil || renamed.AccountName != "Everyday card" || renamed.ProviderKind != "statement" {
			test.Errorf("renamed %+v %v", renamed, err)
		}
		assets, err := fixture.resolver.Assets(ctx, AssetsArguments{FinanceAccountID: cardAccount.ID})
		if err != nil || len(assets) != 1 || assets[0].AssetName != "Everyday card" {
			test.Errorf("the asset %+v %v", assets, err)
		}
		if _, err := fixture.resolver.RenameStatementAccount(ctx, RenameStatementAccountArguments{FinanceAccountID: providerAccount.ID, AccountName: "Mine"}); !errors.Is(err, api.ErrInvalidArguments) {
			test.Errorf("renaming a provider's account answered %v", err)
		}
		if _, err := fixture.resolver.DeleteStatementAccount(ctx, StatementAccountArguments{FinanceAccountID: providerAccount.ID}); !errors.Is(err, api.ErrInvalidArguments) {
			test.Errorf("deleting a provider's account answered %v", err)
		}
		if _, err := fixture.resolver.RenameStatementAccount(ctx, RenameStatementAccountArguments{FinanceAccountID: cardAccount.ID, AccountName: "  "}); !errors.Is(err, api.ErrInvalidArguments) {
			test.Errorf("an empty name answered %v", err)
		}
	})
	if _, err := fixture.importRows(test, card); err != nil {
		test.Fatal(err)
	}
	if accounts := fixture.statementAccounts(test); len(accounts) != 1 || accounts[0].AccountName != "Everyday card" {
		test.Errorf("the next import lost the name: %+v", accounts)
	}

	fixture.as(test, fixture.stranger, func(ctx context.Context, tx db.Transaction) {
		if _, err := fixture.resolver.RenameStatementAccount(ctx, RenameStatementAccountArguments{FinanceAccountID: cardAccount.ID, AccountName: "Theirs"}); !errors.Is(err, api.ErrNotFound) {
			test.Errorf("another person's rename answered %v", err)
		}
		if _, err := fixture.resolver.DeleteStatementAccount(ctx, StatementAccountArguments{FinanceAccountID: cardAccount.ID}); !errors.Is(err, api.ErrNotFound) {
			test.Errorf("another person's delete answered %v", err)
		}
	})

	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		deleted, err := fixture.resolver.DeleteStatementAccount(ctx, StatementAccountArguments{FinanceAccountID: cardAccount.ID})
		if err != nil || deleted.DeletedTransactionCount != 2 || deleted.DeletedAssetCount != 1 {
			test.Fatalf("deleted %+v %v", deleted, err)
		}
	})
	if accounts := fixture.statementAccounts(test); len(accounts) != 0 {
		test.Errorf("the account is still there: %+v", accounts)
	}
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		page, err := fixture.resolver.FinanceTransactions(ctx, FinanceTransactionsArguments{FinanceAccountID: cardAccount.ID})
		if err != nil || len(page.FinanceTransactions) != 0 {
			test.Errorf("its transactions are still there: %+v %v", page, err)
		}
		assets, err := fixture.resolver.Assets(ctx, AssetsArguments{Text: "Everyday card"})
		if err != nil || len(assets) != 0 {
			test.Errorf("its asset is still there: %+v %v", assets, err)
		}
	})
	if isTransfer(groceryId) {
		test.Error("the checking side is still a transfer with nothing on the other side")
	}
}

// inventedCardStatementWithFITIDs is an invented card's OFX export, which
// knows the card by an opaque identifier rather than its number and gives
// every transaction a FITID.
const inventedCardStatementWithFITIDs = `<?xml version="1.0" encoding="UTF-8"?>
<?OFX OFXHEADER="200" VERSION="220" SECURITY="NONE" OLDFILEUID="NONE" NEWFILEUID="NONE"?>
<OFX><SIGNONMSGSRSV1><SONRS><STATUS><CODE>0</CODE><SEVERITY>INFO</SEVERITY></STATUS><DTSERVER>20260901</DTSERVER><LANGUAGE>JPN</LANGUAGE>
<FI><ORG>Example Card Company</ORG></FI></SONRS></SIGNONMSGSRSV1>
<CREDITCARDMSGSRSV1><CCSTMTTRNRS><TRNUID>0</TRNUID><STATUS><CODE>0</CODE><SEVERITY>INFO</SEVERITY></STATUS><CCSTMTRS><CURDEF>JPY</CURDEF>
<CCACCTFROM><ACCTID>a1b2c3d4-0000-4e5f-9a8b-77cc</ACCTID></CCACCTFROM>
<BANKTRANLIST><DTSTART>20260801</DTSTART><DTEND>20260831</DTEND>
<STMTTRN><TRNTYPE>DEBIT</TRNTYPE><DTPOSTED>20260803</DTPOSTED><TRNAMT>-1200</TRNAMT><FITID>fit-invented-card-1</FITID><NAME>EXAMPLE BOOKS</NAME></STMTTRN>
<STMTTRN><TRNTYPE>DEBIT</TRNTYPE><DTPOSTED>20260810</DTPOSTED><TRNAMT>-600</TRNAMT><FITID>fit-invented-card-2</FITID><NAME>EXAMPLE CAFE TOKYO</NAME></STMTTRN>
<STMTTRN><TRNTYPE>DEBIT</TRNTYPE><DTPOSTED>20260820</DTPOSTED><TRNAMT>-3000</TRNAMT><FITID>fit-invented-card-3</FITID><NAME>EXAMPLE MARKET</NAME></STMTTRN>
</BANKTRANLIST></CCSTMTRS></CCSTMTTRNRS></CREDITCARDMSGSRSV1></OFX>
`

// inventedBankStatementWithoutFITIDs is an invented bank account's OFX
// export with its whole number and no FITIDs.
const inventedBankStatementWithoutFITIDs = `<?xml version="1.0" encoding="UTF-8"?>
<?OFX OFXHEADER="200" VERSION="220" SECURITY="NONE" OLDFILEUID="NONE" NEWFILEUID="NONE"?>
<OFX><SIGNONMSGSRSV1><SONRS><STATUS><CODE>0</CODE><SEVERITY>INFO</SEVERITY></STATUS><DTSERVER>20260925</DTSERVER><LANGUAGE>JPN</LANGUAGE>
<FI><ORG>Example Bank</ORG></FI></SONRS></SIGNONMSGSRSV1>
<BANKMSGSRSV1><STMTTRNRS><TRNUID>0</TRNUID><STATUS><CODE>0</CODE><SEVERITY>INFO</SEVERITY></STATUS><STMTRS><CURDEF>JPY</CURDEF>
<BANKACCTFROM><BANKID>0999</BANKID><ACCTID>7654321</ACCTID><ACCTTYPE>SAVINGS</ACCTTYPE></BANKACCTFROM>
<BANKTRANLIST><DTSTART>20260901</DTSTART><DTEND>20260920</DTEND>
<STMTTRN><TRNTYPE>DEBIT</TRNTYPE><DTPOSTED>20260901</DTPOSTED><TRNAMT>-1500</TRNAMT><NAME>サンプル商店</NAME></STMTTRN>
<STMTTRN><TRNTYPE>DEP</TRNTYPE><DTPOSTED>20260915</DTPOSTED><TRNAMT>200000</TRNAMT><NAME>キユウヨ サンプル</NAME></STMTTRN>
<STMTTRN><TRNTYPE>DEBIT</TRNTYPE><DTPOSTED>20260918</DTPOSTED><TRNAMT>-4500</TRNAMT><NAME>デンキダイ</NAME></STMTTRN>
<STMTTRN><TRNTYPE>DEBIT</TRNTYPE><DTPOSTED>20260920</DTPOSTED><TRNAMT>-500</TRNAMT><NAME>EXAMPLE VENDING</NAME></STMTTRN>
</BANKTRANLIST></STMTRS></STMTTRNRS></BANKMSGSRSV1></OFX>
`

// inventedBankStatementOf is an invented Example Bank savings account's
// OFX export under an account number, with one transaction.
func inventedBankStatementOf(accountNumber string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<?OFX OFXHEADER="200" VERSION="220" SECURITY="NONE" OLDFILEUID="NONE" NEWFILEUID="NONE"?>
<OFX><SIGNONMSGSRSV1><SONRS><STATUS><CODE>0</CODE><SEVERITY>INFO</SEVERITY></STATUS><DTSERVER>20260925</DTSERVER><LANGUAGE>JPN</LANGUAGE>
<FI><ORG>Example Bank</ORG></FI></SONRS></SIGNONMSGSRSV1>
<BANKMSGSRSV1><STMTTRNRS><TRNUID>0</TRNUID><STATUS><CODE>0</CODE><SEVERITY>INFO</SEVERITY></STATUS><STMTRS><CURDEF>JPY</CURDEF>
<BANKACCTFROM><BANKID>0999</BANKID><ACCTID>` + accountNumber + `</ACCTID><ACCTTYPE>SAVINGS</ACCTTYPE></BANKACCTFROM>
<BANKTRANLIST><DTSTART>20260901</DTSTART><DTEND>20260901</DTEND>
<STMTTRN><TRNTYPE>DEBIT</TRNTYPE><DTPOSTED>20260901</DTPOSTED><TRNAMT>-1500</TRNAMT><NAME>サンプル商店</NAME></STMTTRN>
</BANKTRANLIST></STMTRS></STMTTRNRS></BANKMSGSRSV1></OFX>
`
}

// importStatementFile imports an OFX file as the owner, uploaded.
func (self *financeFixture) importStatementFile(test *testing.T, store storage.Storage, statementFileName, content string) *models.FinanceStatementImport {
	test.Helper()
	var attachment *models.AgentAttachment
	dbtest.RunTransactionOn(test, self.database, func(tx db.Transaction) {
		var err error
		if attachment, err = tx.CreateAgentAttachment(&models.AgentAttachment{
			AgentID: self.ownerAgent.ID, Name: statementFileName, ContentType: "application/x-ofx", Size: int64(len(content)),
		}); err != nil {
			test.Fatal(err)
		}
	})
	if err := store.PutFile(context.Background(), attachment.ID, []byte(content)); err != nil {
		test.Fatal(err)
	}
	var imported *models.FinanceStatementImport
	self.as(test, self.owner, func(ctx context.Context, tx db.Transaction) {
		var err error
		if imported, err = self.resolver.ImportStatement(ctx, ImportStatementArguments{AgentAttachmentID: attachment.ID}); err != nil {
			test.Fatalf("ImportStatement %s: %s", statementFileName, err)
		}
	})
	return imported
}

// previewRows previews an import of transaction rows as the owner.
func (self *financeFixture) previewRows(test *testing.T, arguments ImportTransactionsArguments) (*TransactionRowsPreviewView, error) {
	test.Helper()
	var preview *TransactionRowsPreviewView
	var err error
	self.as(test, self.owner, func(ctx context.Context, tx db.Transaction) {
		preview, err = self.resolver.PreviewImportTransactions(ctx, arguments)
	})
	return preview, err
}

// accountTransactionCount is how many transactions a finance account of
// the owner's holds.
func (self *financeFixture) accountTransactionCount(test *testing.T, financeAccountId string) int {
	test.Helper()
	var transactionCount int
	dbtest.RunTransactionOn(test, self.database, func(tx db.Transaction) {
		page, err := tx.ListFinanceTransactions(self.ownerAgent.ID, &db.FinanceTransactionFilter{FinanceAccountID: financeAccountId, Limit: db.FinanceTransactionLimitMost})
		if err != nil {
			test.Fatal(err)
		}
		transactionCount = len(page.FinanceTransactions)
	})
	return transactionCount
}

// inventedCardRowArguments is the invented card's list as read off a
// screenshot of the issuer's app, which shows the card's last digits, not
// the identifier its export uses, and writes descriptions its own way. Two
// of its rows are in the export already; the third is not.
func inventedCardRowArguments() ImportTransactionsArguments {
	return ImportTransactionsArguments{
		InstitutionName: "Example Card Company", AccountNumber: "****0042", StatementAccountKind: "card", CurrencyCode: "JPY",
		TransactionRows: []TransactionRow{
			{PostedOn: "2026-08-25", Description: "ｴｸｻﾝﾌﾟﾙ ｼｮｯﾌﾟ", Amount: "-800"},
			{PostedOn: "2026-08-20", Description: "ｴｸｻﾝﾌﾟﾙ ﾏｰｹｯﾄ", Amount: "-3000"},
			{PostedOn: "2026-08-10", Description: "EXAMPLE CAFE", Amount: "-600"},
		},
		MonthlyTotals: []MonthlyTotal{{TotalMonth: "2026-08", TotalAmount: "4400"}},
	}
}

// A card imported from an OFX file with FITIDs, under an identifier that
// is not its number: a screenshot showing only its last digits is not
// taken as a new account, but refused naming the card; named by its id,
// only the row the file did not have is added, whatever the descriptions
// say. A preview before anything exists writes nothing, not even the
// statement source; a preview against the card writes nothing and says
// what the import then does.
func TestImportTransactionsIntoACardFromAFileWithFITIDs(test *testing.T) {
	fixture, store, _ := newStatementFixture(test)
	before, err := fixture.previewRows(test, inventedCardRowArguments())
	if err != nil || !before.IsNewAccount || before.AccountMatch != "new_account" || len(before.NewTransactionRows) != 3 || len(before.PresentTransactionRows) != 0 {
		test.Fatalf("the preview before anything was imported %+v %v", before, err)
	}
	dbtest.RunTransactionOn(test, fixture.database, func(tx db.Transaction) {
		sources, err := tx.ListAgentSources(fixture.ownerAgent.ID)
		if err != nil || len(sources) != 0 {
			test.Errorf("the preview wrote a source: %+v %v", sources, err)
		}
	})

	fixture.importStatementFile(test, store, "card.ofx", inventedCardStatementWithFITIDs)
	accounts := fixture.statementAccounts(test)
	if len(accounts) != 1 {
		test.Fatalf("accounts %+v", accounts)
	}
	card := accounts[0]

	if _, err := fixture.importRows(test, inventedCardRowArguments()); !errors.Is(err, api.ErrInvalidArguments) ||
		!strings.Contains(err.Error(), "Example Card Company ··77cc (finance account id "+card.ID+")") {
		test.Errorf("rows whose last digits match no account answered %v", err)
	}

	named := inventedCardRowArguments()
	named.FinanceAccountID, named.AccountNumber = card.ID, ""
	preview, err := fixture.previewRows(test, named)
	if err != nil {
		test.Fatalf("PreviewImportTransactions: %s", err)
	}
	if preview.IsNewAccount || preview.FinanceAccountID != card.ID || preview.AccountMatch != "finance_account_id" ||
		len(preview.NewTransactionRows) != 1 || preview.NewTransactionRows[0].PostedOn != "2026-08-25" || len(preview.PresentTransactionRows) != 2 ||
		preview.MoneyOutAmount != "-800.0000" || !strings.Contains(preview.VerificationSummary, "monthly totals matched for 2026-08") {
		test.Errorf("preview %+v", preview)
	}
	if transactionCount := fixture.accountTransactionCount(test, card.ID); transactionCount != 3 {
		test.Errorf("the preview wrote: the card holds %d transactions", transactionCount)
	}
	imported, err := fixture.importRows(test, named)
	if err != nil {
		test.Fatalf("ImportTransactions: %s", err)
	}
	if imported.AddedTransactionCount != len(preview.NewTransactionRows) || imported.UnchangedTransactionCount != len(preview.PresentTransactionRows) ||
		len(imported.FinanceAccountIDs) != 1 || imported.FinanceAccountIDs[0] != card.ID {
		test.Errorf("imported %+v, previewed %+v", imported, preview)
	}
	if transactionCount := fixture.accountTransactionCount(test, card.ID); transactionCount != 4 {
		test.Errorf("the card holds %d transactions", transactionCount)
	}
	// The card keeps the file's name and identifier.
	if accounts := fixture.statementAccounts(test); len(accounts) != 1 || accounts[0].AccountMask != "77cc" || accounts[0].AccountName != card.AccountName {
		test.Errorf("accounts %+v", accounts)
	}
	again, err := fixture.importRows(test, named)
	if err != nil || again.AddedTransactionCount != 0 || again.UnchangedTransactionCount != 3 {
		test.Errorf("the same rows again %+v %v", again, err)
	}
}

// A bank account imported from an OFX file without FITIDs: a screenshot
// showing the number's last digits and no bank code lands in it by its
// mask, the rows the file had add nothing although the app writes their
// descriptions otherwise, and of two rows of one day and amount where the
// file has one, exactly one is added.
func TestImportTransactionsIntoABankAccountFromAFileWithoutFITIDs(test *testing.T) {
	fixture, store, _ := newStatementFixture(test)
	fixture.importStatementFile(test, store, "savings.ofx", inventedBankStatementWithoutFITIDs)
	accounts := fixture.statementAccounts(test)
	if len(accounts) != 1 {
		test.Fatalf("accounts %+v", accounts)
	}
	savings := accounts[0]
	rows := ImportTransactionsArguments{
		InstitutionName: "Example Bank", AccountNumber: "***4321", StatementAccountKind: "bank", CurrencyCode: "JPY",
		TransactionRows: []TransactionRow{
			{PostedOn: "2026-09-20", Description: "ｴｸｻﾝﾌﾟﾙ ｼﾞﾊﾝｷ", Amount: "-500", RunningBalanceAmount: "343000"},
			{PostedOn: "2026-09-20", Description: "ｴｸｻﾝﾌﾟﾙ ｼﾞﾊﾝｷ", Amount: "-500", RunningBalanceAmount: "343500"},
			{PostedOn: "2026-09-18", Description: "ﾃﾞﾝｷ ﾀﾞｲ", Amount: "-4500", RunningBalanceAmount: "344000"},
			{PostedOn: "2026-09-15", Description: "ｷﾕｳﾖ", Amount: "200000", RunningBalanceAmount: "348500"},
		},
	}
	preview, err := fixture.previewRows(test, rows)
	if err != nil {
		test.Fatalf("PreviewImportTransactions: %s", err)
	}
	if preview.FinanceAccountID != savings.ID || preview.AccountMatch != "account_mask" || len(preview.NewTransactionRows) != 1 ||
		preview.NewTransactionRows[0].Amount != "-500.0000" || len(preview.PresentTransactionRows) != 3 {
		test.Errorf("preview %+v", preview)
	}
	imported, err := fixture.importRows(test, rows)
	if err != nil || imported.AddedTransactionCount != 1 || imported.UnchangedTransactionCount != 3 {
		test.Errorf("imported %+v %v", imported, err)
	}
	if transactionCount := fixture.accountTransactionCount(test, savings.ID); transactionCount != 5 {
		test.Errorf("the account holds %d transactions", transactionCount)
	}
	if accounts := fixture.statementAccounts(test); len(accounts) != 1 {
		test.Errorf("the screenshot made another account: %+v", accounts)
	}
}

// Rows whose last digits two accounts end with are refused naming both; an
// account id of a provider's account, or of another person's, is refused;
// and with the person's word that the rows are of an account not imported
// before, a new one is made beside the one at the same institution.
func TestImportTransactionsRefusesAnAccountItCannotBeSureOf(test *testing.T) {
	fixture, store, _ := newStatementFixture(test)
	_, providerAccount, _ := fixture.seedFinanceSource(test)
	fixture.importStatementFile(test, store, "first.ofx", inventedBankStatementOf("1110042"))
	fixture.importStatementFile(test, store, "second.ofx", inventedBankStatementOf("2220042"))
	accounts := fixture.statementAccounts(test)
	if len(accounts) != 2 {
		test.Fatalf("accounts %+v", accounts)
	}
	rows := ImportTransactionsArguments{
		InstitutionName: "EXAMPLE BANK", AccountNumber: "****0042", StatementAccountKind: "bank", CurrencyCode: "JPY",
		TransactionRows: []TransactionRow{{PostedOn: "2026-09-21", Description: "ﾘｿｸ", Amount: "3", TransactionKind: "interest"}},
	}
	_, err := fixture.importRows(test, rows)
	if !errors.Is(err, api.ErrInvalidArguments) || !strings.Contains(err.Error(), accounts[0].ID) || !strings.Contains(err.Error(), accounts[1].ID) {
		test.Errorf("rows two accounts could be answered %v", err)
	}
	if _, err := fixture.previewRows(test, rows); !errors.Is(err, api.ErrInvalidArguments) {
		test.Errorf("the preview of rows two accounts could be answered %v", err)
	}

	ofProvider := rows
	ofProvider.FinanceAccountID = providerAccount.ID
	if _, err := fixture.importRows(test, ofProvider); !errors.Is(err, api.ErrInvalidArguments) || !strings.Contains(err.Error(), "provider") {
		test.Errorf("a provider's account answered %v", err)
	}
	ofStatement := rows
	ofStatement.FinanceAccountID = accounts[0].ID
	fixture.as(test, fixture.stranger, func(ctx context.Context, tx db.Transaction) {
		if _, err := fixture.resolver.ImportTransactions(ctx, ofStatement); !errors.Is(err, api.ErrInvalidArguments) || !strings.Contains(err.Error(), "no finance account") {
			test.Errorf("another person's account answered %v", err)
		}
	})
	for _, account := range accounts {
		if transactionCount := fixture.accountTransactionCount(test, account.ID); transactionCount != 1 {
			test.Errorf("a refused import wrote: %s holds %d transactions", account.ID, transactionCount)
		}
	}

	isNewAccount := true
	elsewhere := rows
	elsewhere.AccountNumber, elsewhere.IsNewAccount = "****9999", &isNewAccount
	if _, err := fixture.importRows(test, elsewhere); err != nil {
		test.Errorf("a new account the person vouched for answered %v", err)
	}
	if accounts := fixture.statementAccounts(test); len(accounts) != 3 {
		test.Errorf("accounts %+v", accounts)
	}
}
