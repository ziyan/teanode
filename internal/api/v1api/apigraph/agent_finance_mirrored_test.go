package apigraph

import (
	"context"
	"errors"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

// A fee one connection reports on two accounts lists twice, the copy
// naming the counted one, and counts once in the spending summary. The
// person counting the copy makes it count; taking that back makes it a
// duplicate again. Counting the counted copy, or another person's
// transaction, is refused.
func TestMirroredCopiesCountOnceAndThePersonCanCountOne(test *testing.T) {
	fixture := newFinanceFixture(test, true)
	resolver := fixture.resolver
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		source, err := resolver.LinkSimpleFIN(ctx, LinkSimpleFINArguments{SetupToken: "aW52ZW50ZWQtc2V0dXAtdG9rZW4="})
		if err != nil {
			test.Fatal(err)
		}
		account := func(providerAccountId string) finance.Account {
			return finance.Account{ProviderAccountID: providerAccountId, AccountName: "Brokerage " + providerAccountId, AccountKind: "investment", CurrencyCode: "USD"}
		}
		fee := func(providerTransactionId, providerAccountId string) finance.Transaction {
			return finance.Transaction{ProviderTransactionID: providerTransactionId, ProviderAccountID: providerAccountId, PostedOn: "2026-09-15",
				Amount: "-25", CurrencyCode: "USD", Description: "ACCOUNT FEE"}
		}
		if _, err := tx.ApplyFinanceSync(fixture.ownerAgent.ID, source.ID, &finance.SyncResult{
			Accounts: []finance.Account{account("one"), account("two")},
			Added:    []finance.Transaction{fee("fee-one", "one"), fee("fee-two", "two")},
		}, "2026-09-16"); err != nil {
			test.Fatal(err)
		}
	})

	var copyId, countedId string
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		page, err := resolver.FinanceTransactions(ctx, FinanceTransactionsArguments{})
		if err != nil || len(page.FinanceTransactions) != 2 {
			test.Fatalf("both copies are listed: %+v %v", page, err)
		}
		for _, financeTransaction := range page.FinanceTransactions {
			if financeTransaction.DuplicateOfTransactionID != "" {
				copyId, countedId = financeTransaction.ID, financeTransaction.DuplicateOfTransactionID
				if financeTransaction.DuplicateDecidedBy != models.DuplicateDecidedByMirrorDetection {
					test.Errorf("decided by %q", financeTransaction.DuplicateDecidedBy)
				}
			}
		}
		if copyId == "" {
			test.Fatalf("one copy is a duplicate of the other: %+v", page.FinanceTransactions)
		}
		duplicates, err := resolver.FinanceTransactions(ctx, FinanceTransactionsArguments{DuplicateOfTransactionID: countedId})
		if err != nil || len(duplicates.FinanceTransactions) != 1 || duplicates.FinanceTransactions[0].ID != copyId {
			test.Errorf("the counted copy's duplicates: %+v %v", duplicates, err)
		}
		if moneyOut := summaryMoneyOut(test, ctx, resolver); moneyOut != "25.0000" {
			test.Errorf("the fee counts once: %s", moneyOut)
		}
		if _, err := resolver.CountTransaction(ctx, CountTransactionArguments{FinanceTransactionID: countedId}); !errors.Is(err, api.ErrInvalidArguments) {
			test.Errorf("counting the counted copy answered %v", err)
		}
		counted, err := resolver.CountTransaction(ctx, CountTransactionArguments{FinanceTransactionID: copyId})
		if err != nil || counted.DuplicateOfTransactionID != "" || counted.DuplicateDecidedBy != models.DuplicateDecidedByPerson {
			test.Fatalf("the person counts the copy: %+v %v", counted, err)
		}
		if moneyOut := summaryMoneyOut(test, ctx, resolver); moneyOut != "50.0000" {
			test.Errorf("counted by the person, the copy counts too: %s", moneyOut)
		}
		undone, err := resolver.UndoCountTransaction(ctx, CountTransactionArguments{FinanceTransactionID: copyId})
		if err != nil || undone.DuplicateOfTransactionID != countedId || undone.DuplicateDecidedBy != models.DuplicateDecidedByMirrorDetection {
			test.Errorf("taken back, it is a duplicate again: %+v %v", undone, err)
		}
	})
	fixture.as(test, fixture.stranger, func(ctx context.Context, tx db.Transaction) {
		if _, err := resolver.CountTransaction(ctx, CountTransactionArguments{FinanceTransactionID: copyId}); !errors.Is(err, api.ErrNotFound) {
			test.Errorf("another person's transaction answered %v", err)
		}
	})
}

// summaryMoneyOut is the spending summary's money out in dollars.
func summaryMoneyOut(test *testing.T, ctx context.Context, resolver *graph) string {
	test.Helper()
	summary, err := resolver.FinanceSpendingSummary(ctx, FinanceSpendingSummaryArguments{})
	if err != nil {
		test.Fatal(err)
	}
	for _, total := range summary.CurrencyTotals {
		if total.CurrencyCode == "USD" {
			return total.MoneyOut
		}
	}
	return ""
}
