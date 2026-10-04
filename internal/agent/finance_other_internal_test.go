package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/decide"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// The categorize model is offered the other category, marked as what fits
// none of the others whatever the person named it, so a transaction it
// can read but not place has somewhere to go. Hidden, it is not offered,
// like any hidden spending category.
func TestCategorizeModelIsOfferedTheOtherCategory(t *testing.T) {
	choices := spendingCategoryChoices([]*models.SpendingCategory{
		{ID: "groceries", SpendingCategoryName: finance.SpendingCategoryGroceries},
		{ID: "other", SpendingCategoryName: "odds and ends", IsOther: true},
		{ID: "own-other", SpendingCategoryName: finance.SpendingCategoryOther},
	})
	if choices["other"] != "odds and ends"+otherChoiceSuffix || choices["own-other"] != finance.SpendingCategoryOther || len(choices) != 3 {
		t.Errorf("the choices: %v", choices)
	}
	hidden := spendingCategoryChoices([]*models.SpendingCategory{
		{ID: "groceries", SpendingCategoryName: finance.SpendingCategoryGroceries},
		{ID: "other", SpendingCategoryName: finance.SpendingCategoryOther, IsOther: true, IsHidden: true},
	})
	if _, isOffered := hidden["other"]; isOffered {
		t.Errorf("a hidden other category is not offered: %v", hidden)
	}
}

// instructionsCategorizer is a decision model that keeps what it was asked
// and answers nothing, so every finance transaction goes on to the chat
// model.
type instructionsCategorizer struct {
	instructions []string
}

func (self *instructionsCategorizer) Decide(_ context.Context, _ string, questions map[string]decide.Question) (decide.Answers, error) {
	self.instructions = append(self.instructions, questions[categorizeQuestion].Instructions)
	return nil, errors.New("the service is down")
}

// Both models are told the other category is the answer for what fits
// nothing, rather than leaving a transaction uncategorized, and the chat
// model's answer of it is written like any other.
func TestCategorizeModelPlacesWhatFitsNothingInOther(t *testing.T) {
	model := &alertModel{}
	server := model.serve(t)
	fixture := newFinanceFixture(t, server.URL)
	otherId := fixture.spendingCategoryIdNamed(t, fixture.agent.ID, finance.SpendingCategoryOther)
	fixture.applySync(t, &finance.SyncResult{
		Accounts: []finance.Account{inventedAccount()},
		Added:    []finance.Transaction{inventedTransaction("transaction-1", "2026-09-19", "-12.00", "STALL WITH NO SIGN", "Stall With No Sign", "")},
	})
	decider := &instructionsCategorizer{}
	original := categorizeModels
	categorizeModels = func(*Agent, *config.Configuration) (llm.Decider, string) { return decider, "p:thinker" }
	t.Cleanup(func() { categorizeModels = original })
	model.answers = []string{fmt.Sprintf(`{"categorizations": [{"transactionId": "t1", "spendingCategoryId": %q}]}`, otherId)}

	if err := fixture.worker.runCategorize(t.Context(), fixture.run()); err != nil {
		t.Fatalf("runCategorize: %s", err)
	}
	if len(decider.instructions) != 1 || !strings.Contains(decider.instructions[0], "fits none of the others") {
		t.Errorf("the decision model is told what other is for: %q", decider.instructions)
	}
	if model.callCount() != 1 {
		t.Fatalf("one batch goes to the chat model: %d calls", model.callCount())
	}
	prompt := model.prompts[0]
	if !strings.Contains(prompt, otherId+": other"+otherChoiceSuffix) || !strings.Contains(prompt, "that is the answer for a transaction that fits nothing") {
		t.Errorf("the chat model is offered other and told what it is for: %s", prompt)
	}
	if stall := fixture.transactions(t)["STALL WITH NO SIGN"]; stall.SpendingCategoryID != otherId || stall.CategorizedBy != models.CategorizedByCategorizeModel {
		t.Errorf("the model's other is written: %+v", stall)
	}
}

// The provider category mapping's other is the other category found by its
// flag: cash out at a machine, from a statement or from Plaid, goes there
// after the person renamed it, and never to a spending category of their
// own that they named other.
func TestProviderCategoryMappingSendsOtherToTheOtherCategory(t *testing.T) {
	fixture := newFinanceFixture(t, "")
	otherId := fixture.spendingCategoryIdNamed(t, fixture.agent.ID, finance.SpendingCategoryOther)
	ownOtherId := ""
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		if _, err := tx.UpdateSpendingCategory(fixture.agent.ID, otherId, func(spendingCategory *models.SpendingCategory) error {
			spendingCategory.SpendingCategoryName = "cash and odds"
			return nil
		}); err != nil {
			t.Fatalf("UpdateSpendingCategory: %s", err)
		}
		ownOther, err := tx.CreateSpendingCategory(&models.SpendingCategory{AgentID: fixture.agent.ID, SpendingCategoryName: finance.SpendingCategoryOther})
		if err != nil {
			t.Fatalf("CreateSpendingCategory: %s", err)
		}
		ownOtherId = ownOther.ID
	})
	today := time.Now().UTC().Format(time.DateOnly)
	machine := inventedTransaction("statement-cash", today, "-60.00", "CASH MACHINE", "", finance.StatementCategoryPrefix+"ATM")
	machine.ProviderCategoryPrimary = finance.StatementCategoryPrimaryBankAccount
	fixture.provider.result = &finance.SyncResult{
		Accounts: []finance.Account{inventedAccount()},
		Added: []finance.Transaction{
			machine,
			inventedTransaction("plaid-cash", today, "-40.00", "WITHDRAWAL", "", "TRANSFER_OUT_WITHDRAWAL"),
		},
	}
	fixture.sync(t)

	written := fixture.transactions(t)
	for _, description := range []string{"CASH MACHINE", "WITHDRAWAL"} {
		financeTransaction := written[description]
		if financeTransaction.SpendingCategoryID != otherId || financeTransaction.CategorizedBy != models.CategorizedByProviderCategoryMapping {
			t.Errorf("%s goes to the other category, not their own other %s: %+v", description, ownOtherId, financeTransaction)
		}
	}
}

// The other category is counted the way money with no spending category
// always was: money out in it is spending, and money in it is income
// rather than a refund that lowers that spending. No income budget counts
// that income, since it belongs to no income category, but the saving
// summary's income does.
func TestOtherCategoryCountsMoneyInAsIncome(t *testing.T) {
	fixture := newFinanceFixture(t, "")
	otherId := fixture.spendingCategoryIdNamed(t, fixture.agent.ID, finance.SpendingCategoryOther)
	incomeId := fixture.spendingCategoryIdNamed(t, fixture.agent.ID, finance.SpendingCategoryIncome)
	fixture.applySync(t, &finance.SyncResult{Accounts: []finance.Account{inventedAccount()}, Added: []finance.Transaction{
		inventedTransaction("odd-purchase", "2026-06-03", "-50.00", "EXAMPLE ODD PURCHASE", "", ""),
		inventedTransaction("odd-credit", "2026-06-04", "20.00", "EXAMPLE ODD CREDIT", "", ""),
	}})
	var status *models.BudgetStatus
	var summary *models.SavingSummary
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		page, err := tx.ListFinanceTransactions(fixture.agent.ID, &db.FinanceTransactionFilter{})
		if err != nil {
			t.Fatalf("ListFinanceTransactions: %s", err)
		}
		for _, financeTransaction := range page.FinanceTransactions {
			if _, err := tx.SetTransactionCategorization(fixture.agent.ID, financeTransaction.ID, otherId, models.CategorizedByPerson, nil); err != nil {
				t.Fatalf("SetTransactionCategorization: %s", err)
			}
		}
		for _, budget := range []*models.Budget{
			{AgentID: fixture.agent.ID, SpendingCategoryID: otherId, MonthlyAmount: "100", CurrencyCode: "USD", EffectiveFrom: "2026-06"},
			{AgentID: fixture.agent.ID, SpendingCategoryID: incomeId, MonthlyAmount: "1000", CurrencyCode: "USD", EffectiveFrom: "2026-06"},
		} {
			if _, err := tx.SetBudget(budget); err != nil {
				t.Fatalf("SetBudget: %s", err)
			}
		}
		if status, err = BudgetStatus(t.Context(), tx, nil, fixture.agent.ID, "2026-06", "2026-07-01"); err != nil {
			t.Fatalf("BudgetStatus: %s", err)
		}
		if summary, err = SavingSummary(t.Context(), tx, nil, fixture.agent.ID, "2026-06", "2026-07-01", "USD"); err != nil {
			t.Fatalf("SavingSummary: %s", err)
		}
	})
	if len(status.SpendingCategories) != 1 || status.SpendingCategories[0].SpendingCategoryID != otherId ||
		status.SpendingCategories[0].SpendingAmount != "50.0000" {
		t.Errorf("other's spending is its money out, not lowered by its money in: %+v", status.SpendingCategories)
	}
	if len(status.IncomeCategories) != 1 || status.IncomeCategories[0].IncomeAmount != "0.0000" {
		t.Errorf("no income budget counts other's money in: %+v", status.IncomeCategories)
	}
	if summary.IncomeAmount != "20.0000" || summary.SpendingAmount != "50.0000" || summary.SavingAmount != "-30.0000" {
		t.Errorf("the month's income is other's money in and its spending other's money out: income %s, spending %s, saving %s",
			summary.IncomeAmount, summary.SpendingAmount, summary.SavingAmount)
	}
}
