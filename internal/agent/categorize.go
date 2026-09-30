package agent

import (
	"context"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/decide"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// The categorize job: spending categories for the finance transactions
// that the person, their spending rules and the provider category mapping
// left uncategorized, from the categorize model. A decision model (Jev)
// scores the person's spending categories for one finance transaction at
// a time and is taken where it is sure; what it is unsure of, or all of
// it when the categorize model is a chat model, goes to a chat model in
// batches with a structured answer.
//
// What either is sent is the merchant, the description, the amount and
// currency, the account kind and the provider category: never an account
// number, a mask or provider metadata.

const (
	// categorizeFloor is how sure the decision model must be before its
	// answer is written. The same place worthOpeningFloor starts, to be
	// tuned from what the plan's Surprises & Discoveries measure.
	categorizeFloor = 0.6

	// categorizeTransactionsPerRun bounds one job; more waiting brings the
	// job back straight after.
	categorizeTransactionsPerRun = 200

	// categorizeBatchSize is how many finance transactions one chat call
	// is asked about: enough that the spending categories and the
	// instructions are paid for once per many, few enough that a small
	// model keeps track of the ids.
	categorizeBatchSize = 50

	// categorizeLongest bounds the job. Within the ten minutes a job that
	// is not a dream, an ingest or background work may hold its claim.
	categorizeLongest = 10 * time.Minute

	// categorizeAgain is how soon the job comes back when more finance
	// transactions wait than one run takes.
	categorizeAgain = time.Minute

	// categorizeQuestion is the one question the decision model is asked.
	categorizeQuestion = "spending_category"
)

// categorizeModels is what the categorize model resolves to: a decider
// when it names a decision model, and the chat model to ask about what is
// left, when there is one. A variable so a test can hand in a decider that
// answers to order.
var categorizeModels = func(self *Agent, configuration *config.Configuration) (llm.Decider, string) {
	registry := self.settings.Registry
	if registry == nil {
		return nil, ""
	}
	modelSettings := &configuration.Agent.Models
	name := modelSettings.CategorizeModel()
	if name == "" {
		return nil, ""
	}
	if decider, _, err := registry.DecidingFor(name); err == nil {
		// The decider takes the answer where it is sure; for the rest, the
		// chat model the bulk work runs on.
		chatName := modelSettings.ForWork(config.AgentWorkScan)
		if _, _, err := registry.ForModel(chatName); err != nil {
			chatName = ""
		}
		return decider, chatName
	}
	if _, _, err := registry.ForModel(name); err != nil {
		log.Warningf("the categorize model %q is neither a decision model nor a chat model: %s", name, err)
		return nil, ""
	}
	return nil, name
}

// categorizeItem is one finance transaction as the categorize model sees
// it.
type categorizeItem struct {
	financeTransactionId string
	state                string
}

// runCategorize is the handler for a categorize job; its subject is the
// agent.
func (self *Agent) runCategorize(ctx context.Context, run *Run) error {
	configuration := run.Configuration()
	agentId := run.Agent.ID
	var spendingCategories []*models.SpendingCategory
	var financeTransactions []*models.FinanceTransaction
	accountKindById := map[string]models.FinanceAccountKind{}
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if spendingCategories, err = tx.ListSpendingCategories(agentId); err != nil {
			return err
		}
		if financeTransactions, err = tx.ListUncategorizedFinanceTransactions(agentId, categorizeTransactionsPerRun+1); err != nil {
			return err
		}
		financeAccounts, err := tx.ListFinanceAccounts(agentId, "")
		if err != nil {
			return err
		}
		for _, financeAccount := range financeAccounts {
			accountKindById[financeAccount.ID] = financeAccount.AccountKind
		}
		return nil
	}); err != nil {
		return err
	}
	choices := spendingCategoryChoices(spendingCategories)
	if len(choices) == 0 || len(financeTransactions) == 0 {
		return nil
	}
	isMoreWaiting := len(financeTransactions) > categorizeTransactionsPerRun
	if isMoreWaiting {
		financeTransactions = financeTransactions[:categorizeTransactionsPerRun]
	}
	items := make([]categorizeItem, 0, len(financeTransactions))
	for _, financeTransaction := range financeTransactions {
		items = append(items, categorizeItem{
			financeTransactionId: financeTransaction.ID,
			state:                financeTransactionLine(financeTransaction, accountKindById[financeTransaction.FinanceAccountID]),
		})
	}

	decider, chatModelName := categorizeModels(self, configuration)
	if decider == nil && chatModelName == "" {
		// No model at all: they stay uncategorized, for the person, a
		// spending rule, or a model configured later.
		return nil
	}
	categorizedCount := 0
	unsure := items
	if decider != nil {
		var decided []categorizeAnswer
		decided, unsure = decideSpendingCategories(ctx, decider, items, choices)
		written, err := self.writeCategorizations(ctx, run, decided)
		if err != nil {
			return err
		}
		categorizedCount += written
	}
	if len(unsure) > 0 && chatModelName != "" && self.canThink(configuration) {
		for start := 0; start < len(unsure); start += categorizeBatchSize {
			if ctx.Err() != nil {
				break
			}
			batch := unsure[start:min(start+categorizeBatchSize, len(unsure))]
			answers, err := self.askSpendingCategories(ctx, run, chatModelName, batch, spendingCategories)
			if err != nil {
				log.Warningf("the categorize model could not categorize %d finance transaction(s) of agent %q: %s", len(batch), agentId, err)
				continue
			}
			written, err := self.writeCategorizations(ctx, run, answers)
			if err != nil {
				return err
			}
			categorizedCount += written
		}
	}
	// Only when this run got somewhere: what the models could not place
	// stays uncategorized and would be asked about again, forever, by a
	// job that brought itself back.
	if isMoreWaiting && categorizedCount > 0 {
		return &Deferral{Until: time.Now().Add(categorizeAgain), Reason: "more finance transactions wait to be categorized"}
	}
	return nil
}

// categorizeAnswer is a spending category for a finance transaction, and
// the decision model's confidence when it gave it.
type categorizeAnswer struct {
	financeTransactionId string
	spendingCategoryId   string
	confidence           *float64
}

// spendingCategoryChoices is the answers the categorize model chooses
// among: each spending category the person has not hidden, by id, named
// with its parent when it has one.
func spendingCategoryChoices(spendingCategories []*models.SpendingCategory) map[string]string {
	nameById := map[string]string{}
	for _, spendingCategory := range spendingCategories {
		nameById[spendingCategory.ID] = spendingCategory.SpendingCategoryName
	}
	choices := map[string]string{}
	for _, spendingCategory := range spendingCategories {
		if spendingCategory.IsHidden {
			continue
		}
		name := spendingCategory.SpendingCategoryName
		if parentName := nameById[spendingCategory.ParentSpendingCategoryID]; parentName != "" {
			name = parentName + " / " + name
		}
		if spendingCategory.IsIncome {
			name += " (income)"
		}
		choices[spendingCategory.ID] = name
	}
	return choices
}

// financeTransactionLine is a finance transaction in one line, as the
// categorize model is shown it.
func financeTransactionLine(financeTransaction *models.FinanceTransaction, accountKind models.FinanceAccountKind) string {
	parts := []string{}
	if merchant := strings.TrimSpace(financeTransaction.MerchantName); merchant != "" {
		parts = append(parts, "merchant: "+merchant)
	}
	if description := strings.TrimSpace(financeTransaction.Description); description != "" {
		parts = append(parts, "description: "+description)
	}
	parts = append(parts, "amount: "+readableAmount(financeTransaction.Amount)+" "+financeTransaction.CurrencyCode)
	if accountKind != "" {
		parts = append(parts, "account kind: "+string(accountKind))
	}
	providerCategory := strings.TrimSpace(financeTransaction.ProviderCategoryDetailed)
	if providerCategory == "" {
		providerCategory = strings.TrimSpace(financeTransaction.ProviderCategoryPrimary)
	}
	if providerCategory != "" {
		parts = append(parts, "provider category: "+providerCategory)
	}
	return strings.Join(parts, "; ")
}

// readableAmount is a stored amount with two places, "-42.17" for
// "-42.1700": the places the columns keep are not a fact about the charge.
func readableAmount(amount string) string {
	value, err := finance.ParseAmount(amount)
	if err != nil {
		return amount
	}
	return displayAmount(value)
}

// decideSpendingCategories asks the decision model about each finance
// transaction, a few at a time, and splits them into what it answered
// sure enough and what goes to the chat model: an answer below the floor,
// one off the list, and a call that failed.
func decideSpendingCategories(ctx context.Context, decider llm.Decider, items []categorizeItem, choices map[string]string) ([]categorizeAnswer, []categorizeItem) {
	type verdict struct {
		answer   *categorizeAnswer
		isUnsure bool
	}
	verdicts := make([]verdict, len(items))
	gate := make(chan struct{}, decidersAtOnce)
	var waiting sync.WaitGroup
	for index, item := range items {
		waiting.Add(1)
		go func(index int, item categorizeItem) {
			defer waiting.Done()
			gate <- struct{}{}
			defer func() { <-gate }()
			verdicts[index] = verdict{isUnsure: true}
			if ctx.Err() != nil {
				return
			}
			answers, err := decider.Decide(ctx, item.state, map[string]decide.Question{
				categorizeQuestion: {
					Instructions: "Which of the person's spending categories does this finance transaction belong to?",
					Choices:      choices,
				},
			})
			if err != nil {
				log.Debugf("the decision model could not categorize finance transaction %q: %s", item.financeTransactionId, err)
				return
			}
			answer := answers[categorizeQuestion]
			if _, isChoice := choices[answer.Choice]; !isChoice || answer.Confidence < categorizeFloor {
				return
			}
			confidence := answer.Confidence
			verdicts[index] = verdict{answer: &categorizeAnswer{financeTransactionId: item.financeTransactionId, spendingCategoryId: answer.Choice, confidence: &confidence}}
		}(index, item)
	}
	waiting.Wait()
	var decided []categorizeAnswer
	var unsure []categorizeItem
	for index, each := range verdicts {
		if each.isUnsure {
			unsure = append(unsure, items[index])
			continue
		}
		decided = append(decided, *each.answer)
	}
	return decided, unsure
}

// categorizeChatAnswer is the structured answer the chat model is asked
// for.
type categorizeChatAnswer struct {
	Categorizations []struct {
		TransactionID      string `json:"transactionId"`
		SpendingCategoryID string `json:"spendingCategoryId"`
	} `json:"categorizations"`
}

// askSpendingCategories asks a chat model about one batch and returns the
// pairs it may write: those whose finance transaction was in the batch and
// whose spending category is one of the person's. Anything else in the
// answer -- an id invented, another agent's -- is dropped here, before it
// can reach the database.
func (self *Agent) askSpendingCategories(ctx context.Context, run *Run, modelName string, batch []categorizeItem, spendingCategories []*models.SpendingCategory) ([]categorizeAnswer, error) {
	choices := spendingCategoryChoices(spendingCategories)
	// Short labels rather than the stored ids, which a model copies back
	// wrong more often than "t12".
	labelToTransactionId := map[string]string{}
	var lines []string
	for index, item := range batch {
		label := "t" + strconv.Itoa(index+1)
		labelToTransactionId[label] = item.financeTransactionId
		lines = append(lines, label+": "+item.state)
	}
	var choiceLines []string
	for _, spendingCategory := range spendingCategories {
		if name, isChoice := choices[spendingCategory.ID]; isChoice {
			choiceLines = append(choiceLines, spendingCategory.ID+": "+name)
		}
	}
	prompt, err := render("categorize.txt", map[string]any{
		"PersonName":         personName(run.Owner),
		"SpendingCategories": choiceLines,
		"Transactions":       fenced(strings.Join(lines, "\n")),
	})
	if err != nil {
		return nil, err
	}
	thinking, err := self.oneShotOn(ctx, run, fmt.Sprintf("Categorizing %d finance transaction(s)", len(batch)), prompt, models.AgentJobCategorize, modelName)
	if err != nil {
		return nil, err
	}
	answer := readModelAnswer[categorizeChatAnswer](thinking.Text, "categorizations")
	if !answer.IsValid {
		return nil, fmt.Errorf("the answer could not be read: %s", answer.Problem)
	}
	var answers []categorizeAnswer
	isAnswered := map[string]bool{}
	for _, pair := range answer.Value.Categorizations {
		financeTransactionId := labelToTransactionId[strings.TrimSpace(pair.TransactionID)]
		spendingCategoryId := strings.TrimSpace(pair.SpendingCategoryID)
		if _, isChoice := choices[spendingCategoryId]; financeTransactionId == "" || !isChoice || isAnswered[financeTransactionId] {
			continue
		}
		isAnswered[financeTransactionId] = true
		answers = append(answers, categorizeAnswer{financeTransactionId: financeTransactionId, spendingCategoryId: spendingCategoryId})
	}
	self.retitle(ctx, run, thinking.Conversation, fmt.Sprintf("Categorized %d of %d finance transaction(s)", len(answers), len(batch)))
	return answers, nil
}

// writeCategorizations writes the answers as the categorize model's, and
// says how many were written. One the person or a spending rule got to
// first is left as it is.
func (self *Agent) writeCategorizations(ctx context.Context, run *Run, answers []categorizeAnswer) (int, error) {
	if len(answers) == 0 {
		return 0, nil
	}
	writtenCount := 0
	err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		for _, answer := range answers {
			current, err := tx.GetFinanceTransaction(run.Agent.ID, answer.financeTransactionId)
			if err != nil {
				return err
			}
			if current == nil || current.SpendingCategoryID != "" || current.IsTransfer {
				continue
			}
			var confidence *string
			if answer.confidence != nil {
				text := new(big.Rat).SetFloat64(*answer.confidence).FloatString(4)
				confidence = &text
			}
			isApplied, err := tx.SetTransactionCategorization(run.Agent.ID, answer.financeTransactionId, answer.spendingCategoryId, models.CategorizedByCategorizeModel, confidence)
			if err != nil {
				return err
			}
			if isApplied {
				writtenCount++
			}
		}
		return nil
	})
	return writtenCount, err
}
