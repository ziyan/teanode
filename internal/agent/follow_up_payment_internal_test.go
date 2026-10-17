package agent

import (
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

// A bill is paid by money leaving for its amount to the cent, in its
// currency, within ten days of its due day, from somebody whose name has one
// of the payee's own words in it. Anything short of that leaves it open: a
// bill wrongly called paid is never reminded, and that is worse than a
// reminder that was not needed.
func TestFollowUpPaymentMatch(t *testing.T) {
	bill := func(change func(*models.AgentFollowUp)) *models.AgentFollowUp {
		followUp := &models.AgentFollowUp{
			FollowUpKind: models.AgentFollowUpBill, FollowUpState: models.AgentFollowUpOpen,
			Payee: "Riverbend County Tax Office", Amount: "1234.5000", CurrencyCode: "USD", DueOn: "2026-11-01",
		}
		if change != nil {
			change(followUp)
		}
		return followUp
	}
	transaction := func(change func(*models.FinanceTransaction)) *models.FinanceTransaction {
		financeTransaction := &models.FinanceTransaction{
			Amount: "-1234.50", CurrencyCode: "USD", PostedOn: "2026-10-30", Description: "RIVERBEND CNTY TAX PMT 0041",
		}
		if change != nil {
			change(financeTransaction)
		}
		return financeTransaction
	}
	cases := []struct {
		name               string
		followUp           *models.AgentFollowUp
		financeTransaction *models.FinanceTransaction
		isPaid             bool
	}{
		{"the payment", bill(nil), transaction(nil), true},
		{"the same amount written with more places", bill(nil), transaction(func(financeTransaction *models.FinanceTransaction) { financeTransaction.Amount = "-1234.5000" }), true},
		{"on the due day", bill(nil), transaction(func(financeTransaction *models.FinanceTransaction) { financeTransaction.PostedOn = "2026-11-01" }), true},
		{"ten days early", bill(nil), transaction(func(financeTransaction *models.FinanceTransaction) { financeTransaction.PostedOn = "2026-10-22" }), true},
		{"ten days late", bill(nil), transaction(func(financeTransaction *models.FinanceTransaction) { financeTransaction.PostedOn = "2026-11-11" }), true},
		{"named by the merchant and not the description", bill(nil), transaction(func(financeTransaction *models.FinanceTransaction) {
			financeTransaction.Description, financeTransaction.MerchantName = "ONLINE PAYMENT 7731", "Riverbend Taxes"
		}), true},
		{"an overdue bill is still paid", bill(func(followUp *models.AgentFollowUp) { followUp.FollowUpState = models.AgentFollowUpOverdue }), transaction(nil), true},
		{"a bill with no currency", bill(func(followUp *models.AgentFollowUp) { followUp.CurrencyCode = "" }), transaction(func(financeTransaction *models.FinanceTransaction) { financeTransaction.CurrencyCode = "EUR" }), true},
		{"a transaction with no currency", bill(nil), transaction(func(financeTransaction *models.FinanceTransaction) { financeTransaction.CurrencyCode = "" }), true},

		{"a cent too much", bill(nil), transaction(func(financeTransaction *models.FinanceTransaction) { financeTransaction.Amount = "-1234.51" }), false},
		{"a cent too little", bill(nil), transaction(func(financeTransaction *models.FinanceTransaction) { financeTransaction.Amount = "-1234.49" }), false},
		{"another payee's name", bill(nil), transaction(func(financeTransaction *models.FinanceTransaction) {
			financeTransaction.Description = "HILLSIDE COUNTY TAX OFFICE"
		}), false},
		{"only the generic words of the payee", bill(nil), transaction(func(financeTransaction *models.FinanceTransaction) {
			financeTransaction.Description = "COUNTY OFFICE ONLINE PAYMENT"
		}), false},
		{"a short word of the payee", bill(func(followUp *models.AgentFollowUp) { followUp.Payee = "The Tax Co" }), transaction(func(financeTransaction *models.FinanceTransaction) { financeTransaction.Description = "THE TAX CO" }), false},
		{"eleven days early", bill(nil), transaction(func(financeTransaction *models.FinanceTransaction) { financeTransaction.PostedOn = "2026-10-21" }), false},
		{"eleven days late", bill(nil), transaction(func(financeTransaction *models.FinanceTransaction) { financeTransaction.PostedOn = "2026-11-12" }), false},
		{"money coming in", bill(nil), transaction(func(financeTransaction *models.FinanceTransaction) { financeTransaction.Amount = "1234.50" }), false},
		{"another currency", bill(nil), transaction(func(financeTransaction *models.FinanceTransaction) { financeTransaction.CurrencyCode = "EUR" }), false},
		{"a bill already closed", bill(func(followUp *models.AgentFollowUp) { followUp.FollowUpState = models.AgentFollowUpClosed }), transaction(nil), false},
		{"a parcel", bill(func(followUp *models.AgentFollowUp) { followUp.FollowUpKind = models.AgentFollowUpParcel }), transaction(nil), false},
		{"a bill with no amount", bill(func(followUp *models.AgentFollowUp) { followUp.Amount = "" }), transaction(nil), false},
		{"a bill with no payee", bill(func(followUp *models.AgentFollowUp) { followUp.Payee = "" }), transaction(nil), false},
	}
	for _, each := range cases {
		if got := isFollowUpPaidBy(each.followUp, each.financeTransaction); got != each.isPaid {
			t.Errorf("%s: paid is %v, want %v", each.name, got, each.isPaid)
		}
	}
}

// The matcher reads the stored transactions of the last forty-five days and
// not only what a sync brought, closes the bill with the day and the
// description of the payment, says nothing about it, and leaves the bills no
// transaction paid alone.
func TestMatchFollowUpPaymentsClosesPaidBills(t *testing.T) {
	fixture := newAlertFixtureWith(t, "", "UTC")
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		source, err := tx.PutAgentSource(&models.AgentKnowledgeSource{
			AgentID: fixture.agent.ID, Kind: models.SourceWeb, Name: "institution",
			Specification: models.AgentKnowledgeSpecification{Start: "https://example.com/"},
		})
		if err != nil {
			t.Fatalf("PutAgentSource: %s", err)
		}
		if _, err := tx.ApplyFinanceSync(fixture.agent.ID, source.ID, &finance.SyncResult{
			Accounts: []finance.Account{{ProviderAccountID: "account-checking", AccountName: "Everyday Checking", AccountKind: finance.AccountKindDepository, CurrencyCode: "USD", CurrentBalance: "5000.00"}},
			Added: []finance.Transaction{
				{ProviderTransactionID: "water", ProviderAccountID: "account-checking", PostedOn: "2026-10-19", Amount: "-480.00", CurrencyCode: "USD", Description: "BRIGHTLEAF WATER 8841"},
				{ProviderTransactionID: "gas", ProviderAccountID: "account-checking", PostedOn: "2026-10-21", Amount: "-90.01", CurrencyCode: "USD", Description: "HILLSIDE GAS 5521"},
				{ProviderTransactionID: "internet", ProviderAccountID: "account-checking", PostedOn: "2026-10-20", Amount: "-60.00", CurrencyCode: "USD", Description: "SKYLINE FIBER"},
				{ProviderTransactionID: "refund", ProviderAccountID: "account-checking", PostedOn: "2026-10-20", Amount: "75.00", CurrencyCode: "USD", Description: "FERNWOOD ELECTRIC REFUND"},
				{ProviderTransactionID: "old", ProviderAccountID: "account-checking", PostedOn: "2026-08-20", Amount: "-310.00", CurrencyCode: "USD", Description: "OAKRIDGE INSURANCE"},
			},
		}, "2026-10-20"); err != nil {
			t.Fatalf("ApplyFinanceSync: %s", err)
		}
		for _, bill := range []*models.AgentFollowUp{
			{FollowUpKey: "bill:brightleaf water utility 2026-10-21", FollowUpTitle: "Water", Payee: "Brightleaf Water Utility", Amount: "480.00", DueOn: "2026-10-21"},
			{FollowUpKey: "bill:hillside gas 2026-10-22", FollowUpTitle: "Gas", Payee: "Hillside Gas", Amount: "90.00", DueOn: "2026-10-22"},
			{FollowUpKey: "bill:skyline fiber 2026-10-20", FollowUpTitle: "Internet", Payee: "Skyline Fiber", Amount: "60.00", DueOn: "2026-10-20"},
			{FollowUpKey: "bill:fernwood electric 2026-10-20", FollowUpTitle: "Electric", Payee: "Fernwood Electric", Amount: "75.00", DueOn: "2026-10-20"},
			{FollowUpKey: "bill:oakridge insurance 2026-08-21", FollowUpTitle: "Insurance", Payee: "Oakridge Insurance", Amount: "310.00", DueOn: "2026-08-21"},
		} {
			bill.AgentID, bill.FollowUpKind = fixture.agent.ID, models.AgentFollowUpBill
			if _, _, err := tx.UpsertAgentFollowUp(bill); err != nil {
				t.Fatalf("UpsertAgentFollowUp: %s", err)
			}
		}
	})

	var closedCount int
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		var err error
		if closedCount, err = matchFollowUpPayments(tx, fixture.agent.ID, mustDay(t, "2026-10-20")); err != nil {
			t.Fatalf("matchFollowUpPayments: %s", err)
		}
	})
	if closedCount != 2 {
		t.Fatalf("the water and the internet bill are paid, nothing else: %d closed", closedCount)
	}
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		all, err := tx.ListAgentFollowUps(fixture.agent.ID, 20)
		if err != nil {
			t.Fatalf("ListAgentFollowUps: %s", err)
		}
		want := map[string]struct {
			followUpState models.AgentFollowUpState
			closedReason  string
		}{
			"bill:brightleaf water utility 2026-10-21": {models.AgentFollowUpClosed, "paid on 2026-10-19: BRIGHTLEAF WATER 8841"},
			"bill:skyline fiber 2026-10-20":            {models.AgentFollowUpClosed, "paid on 2026-10-20: SKYLINE FIBER"},
			// A cent off, a refund coming in, and a payment older than
			// forty-five days are all still open.
			"bill:hillside gas 2026-10-22":       {models.AgentFollowUpOpen, ""},
			"bill:fernwood electric 2026-10-20":  {models.AgentFollowUpOpen, ""},
			"bill:oakridge insurance 2026-08-21": {models.AgentFollowUpOpen, ""},
		}
		for _, followUp := range all {
			expected := want[followUp.FollowUpKey]
			if followUp.FollowUpState != expected.followUpState || followUp.ClosedReason != expected.closedReason {
				t.Errorf("%s: %s %q, want %s %q", followUp.FollowUpKey, followUp.FollowUpState, followUp.ClosedReason, expected.followUpState, expected.closedReason)
			}
			if followUp.ToldState != "" {
				t.Errorf("%s: a payment is not said: %+v", followUp.FollowUpKey, followUp)
			}
		}
		waiting, err := tx.ListWaitingAgentAlertCandidates(fixture.agent.ID, 10)
		if err != nil || len(waiting) != 0 {
			t.Fatalf("a payment writes no candidate: %v %+v", err, waiting)
		}
	})

	// Run again, nothing changes.
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		if closedCount, err := matchFollowUpPayments(tx, fixture.agent.ID, mustDay(t, "2026-10-20")); err != nil || closedCount != 0 {
			t.Fatalf("matched again: %d %v", closedCount, err)
		}
	})
}
