package agent

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

// What follows a sync gives the transfer category to a card payment
// paired with its checking withdrawal (transfer detection), to what the
// provider calls a transfer (the provider category mapping) and to what a
// spending rule sends there, saying which did it; none of them is left
// for the categorize model.
func TestFinanceSyncGivesTransfersTheTransferCategory(t *testing.T) {
	fixture := newFinanceFixture(t, "")
	today := time.Now().UTC().Format(time.DateOnly)
	card := inventedAccount()
	card.ProviderAccountID, card.AccountName, card.AccountKind, card.CurrentBalance = "account-2", "Invented Card", finance.AccountKindCredit, "-80.0000"
	cardPayment := inventedTransaction("transaction-3", today, "500", "INVENTED CARD PAYMENT RECEIVED", "", "")
	cardPayment.ProviderAccountID = "account-2"
	fixture.provider.result = &finance.SyncResult{
		Accounts: []finance.Account{inventedAccount(), card},
		Added: []finance.Transaction{
			inventedTransaction("transaction-1", today, "-500", "INVENTED CARD AUTOPAY", "", ""),
			inventedTransaction("transaction-2", today, "-250", "INVENTED BROKERAGE", "", "TRANSFER_OUT_ACCOUNT_TRANSFER"),
			cardPayment,
			inventedTransaction("transaction-4", today, "-35", "ROUND UP TO SAVINGS", "", ""),
			inventedTransaction("transaction-5", today, "-4.50", "INVENTED KIOSK", "", ""),
		},
	}
	transferCategoryId := fixture.transferCategoryId(t)
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		if _, err := tx.CreateSpendingRule(&models.SpendingRule{AgentID: fixture.agent.ID, MatchText: "round up", SpendingCategoryID: transferCategoryId}); err != nil {
			t.Fatalf("CreateSpendingRule: %s", err)
		}
	})
	fixture.sync(t)

	written := fixture.transactions(t)
	for description, expectedCategorizedBy := range map[string]models.CategorizedBy{
		"INVENTED CARD AUTOPAY":          models.CategorizedByTransferDetection,
		"INVENTED CARD PAYMENT RECEIVED": models.CategorizedByTransferDetection,
		"INVENTED BROKERAGE":             models.CategorizedByProviderCategoryMapping,
		"ROUND UP TO SAVINGS":            models.CategorizedBySpendingRule,
	} {
		if financeTransaction := written[description]; financeTransaction.SpendingCategoryID != transferCategoryId || financeTransaction.CategorizedBy != expectedCategorizedBy {
			t.Errorf("%s: %+v, want the transfer category by %s", description, financeTransaction, expectedCategorizedBy)
		}
	}
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		uncategorized, err := tx.ListUncategorizedFinanceTransactions(fixture.agent.ID, 10)
		if err != nil || len(uncategorized) != 1 || uncategorized[0].Description != "INVENTED KIOSK" {
			t.Errorf("only the kiosk is left for the categorize model: %v %+v", err, uncategorized)
		}
	})
}

// The categorize model is never offered the transfer category: what it
// cannot place is spending until something surer says otherwise.
func TestCategorizeModelIsNotOfferedTheTransferCategory(t *testing.T) {
	choices := spendingCategoryChoices([]*models.SpendingCategory{
		{ID: "groceries", SpendingCategoryName: finance.SpendingCategoryGroceries},
		{ID: "income", SpendingCategoryName: finance.SpendingCategoryIncome, IsIncome: true},
		{ID: "transfer", SpendingCategoryName: finance.SpendingCategoryTransfer, IsTransfer: true},
	})
	if _, isOffered := choices["transfer"]; isOffered || len(choices) != 2 || choices["income"] != "income (income)" {
		t.Errorf("the choices: %v", choices)
	}
}
