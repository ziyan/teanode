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
