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

// A fee one connection reports on two accounts lists once, the copy
// counted as left out, and twice when duplicates are asked for, the copy
// naming the counted one; it counts once in the spending summary. The
// person counting the copy makes it count; taking that back makes it a
// duplicate again. Counting the counted copy, or another person's
// transaction, is refused, and one is read by its id only by its owner.
func TestMirroredCopiesCountOnceAndThePersonCanCountOne(test *testing.T) {
	fixture := newFinanceFixture(test, true)
	resolver := fixture.resolver
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		// Plaid, the only provider whose copies are looked for.
		source, err := resolver.CompleteFinanceLink(ctx, CompleteFinanceLinkArguments{PublicToken: "public-invented", InstitutionID: "institution-invented"})
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
		listed, err := resolver.FinanceTransactions(ctx, FinanceTransactionsArguments{})
		if err != nil || len(listed.FinanceTransactions) != 1 || listed.TotalCount != 1 || listed.LeftOutDuplicateCount != 1 ||
			listed.FinanceTransactions[0].DuplicateOfTransactionID != "" {
			test.Fatalf("the counted copy is listed and the other left out: %+v %v", listed, err)
		}
		isDuplicateIncluded := true
		page, err := resolver.FinanceTransactions(ctx, FinanceTransactionsArguments{IsDuplicateIncluded: &isDuplicateIncluded})
		if err != nil || len(page.FinanceTransactions) != 2 || page.TotalCount != 2 || page.LeftOutDuplicateCount != 0 {
			test.Fatalf("both copies are listed when asked for: %+v %v", page, err)
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
		// The details of a copy ask for its counted copy by id, and a copy
		// is read by its id though the list leaves copies out.
		byId, err := resolver.FinanceTransactions(ctx, FinanceTransactionsArguments{FinanceTransactionIDs: []string{countedId}})
		if err != nil || len(byId.FinanceTransactions) != 1 || byId.FinanceTransactions[0].ID != countedId {
			test.Errorf("the counted copy by its id: %+v %v", byId, err)
		}
		copyById, err := resolver.FinanceTransactions(ctx, FinanceTransactionsArguments{FinanceTransactionIDs: []string{copyId}})
		if err != nil || len(copyById.FinanceTransactions) != 1 || copyById.FinanceTransactions[0].ID != copyId {
			test.Errorf("the copy by its id: %+v %v", copyById, err)
		}
		if _, err := resolver.FinanceTransactions(ctx, FinanceTransactionsArguments{FinanceTransactionIDs: []string{" "}}); !errors.Is(err, api.ErrInvalidArguments) {
			test.Errorf("a blank id answered %v", err)
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
		if page, err := resolver.FinanceTransactions(ctx, FinanceTransactionsArguments{FinanceTransactionIDs: []string{copyId}}); err != nil || len(page.FinanceTransactions) != 0 {
			test.Errorf("another person's transaction by its id: %+v %v", page, err)
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
