package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/decide"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/finance/rates"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// fakeFinanceProvider answers each sync with what the test sets, and
// keeps what it was asked with.
type fakeFinanceProvider struct {
	mutex       sync.Mutex
	result      *finance.SyncResult
	err         error
	syncCount   int
	credentials []string
	cursors     []string
	removeCount int
}

func (self *fakeFinanceProvider) Kind() finance.ProviderKind { return finance.ProviderKindSimpleFIN }

func (self *fakeFinanceProvider) Sync(_ context.Context, credential string, cursor string) (*finance.SyncResult, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.syncCount++
	self.credentials = append(self.credentials, credential)
	self.cursors = append(self.cursors, cursor)
	return self.result, self.err
}

func (self *fakeFinanceProvider) Remove(context.Context, string) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.removeCount++
	return nil
}

// financeFixture is a person with an agent, a finance source through
// SimpleFIN with a sealed credential, and a worker whose provider is fake.
type financeFixture struct {
	database      db.Database
	configuration *config.Configuration
	worker        *Agent
	owner         *models.User
	agent         *models.Agent
	source        *models.AgentKnowledgeSource
	provider      *fakeFinanceProvider
}

const financeFixtureCredential = "https://person:invented-password@bridge.example.com/simplefin"

func newFinanceFixture(t *testing.T, providerURL string) *financeFixture {
	t.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(t)
	t.Cleanup(closeDatabase)
	configuration := config.Default()
	configuration.Agent.Enabled = true
	configuration.Agent.Features.Dreaming = new(bool)
	configuration.Server.Secret = "a secret long enough to seal things with"
	configuration.Agent.Finance.OfferedProviders = []string{config.AgentFinanceProviderSimpleFIN}
	settings := &Settings{Database: database, Configuration: func() *config.Configuration { return configuration }, Instance: "test", Tick: time.Hour}
	if providerURL != "" {
		configuration.Agent.Providers = []config.AgentProvider{{Name: "p", Kind: "openai", BaseURL: providerURL, APIKey: "k"}}
		configuration.Agent.Models.Default = "p:thinker"
		registry, err := llm.Open(&configuration.Agent)
		if err != nil {
			t.Fatalf("llm.Open: %s", err)
		}
		store, err := storage.Open(&storage.Settings{Directory: t.TempDir()})
		if err != nil {
			t.Fatalf("storage.Open: %s", err)
		}
		settings.Registry, settings.Storage = registry, store
	}
	worker := New(settings)
	worker.SetOperationsFactory(func(context.Context, *models.User) (Operations, error) {
		return &digestSplitOperations{}, nil
	})
	provider := &fakeFinanceProvider{}
	original := newFinanceProvider
	newFinanceProvider = func(*config.Configuration, string, *time.Location) (finance.Provider, error) { return provider, nil }
	t.Cleanup(func() { newFinanceProvider = original })

	fixture := &financeFixture{database: database, configuration: configuration, worker: worker, provider: provider}
	sealed, err := worker.SealSecret(financeFixtureCredential)
	if err != nil {
		t.Fatalf("SealSecret: %s", err)
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if fixture.owner, err = tx.CreateUser(&models.User{Username: "robin", Name: "Robin Example", Timezone: "UTC"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if fixture.agent, err = tx.CreateAgent(&models.Agent{UserID: fixture.owner.ID, Enabled: true, IsAlertsEnabled: true}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		settingsJSON, _ := json.Marshal(models.FinanceSourceSettings{InstitutionName: "Example Credit Union"})
		if fixture.source, err = tx.PutAgentSource(&models.AgentKnowledgeSource{
			AgentID: fixture.agent.ID, Kind: models.SourceFinance, Name: "example-credit-union", Enabled: true, Cron: models.FinanceSourceCron,
			Specification: models.AgentKnowledgeSpecification{Type: config.AgentFinanceProviderSimpleFIN, Settings: settingsJSON},
		}); err != nil {
			t.Fatalf("PutAgentSource: %s", err)
		}
		if err := tx.PutAgentSourceSecret(fixture.agent.ID, &models.AgentSourceSecret{SourceID: fixture.source.ID, Key: models.FinanceCredentialSecretKey, Value: sealed}); err != nil {
			t.Fatalf("PutAgentSourceSecret: %s", err)
		}
	})
	return fixture
}

func (self *financeFixture) run() *Run {
	return &Run{Agent: self.agent, Owner: self.owner, Now: time.Now(), settings: self.worker.settings}
}

// sync runs the finance source's ingest job once, on the source as it is
// stored now.
func (self *financeFixture) sync(t *testing.T) *models.AgentKnowledgeSource {
	t.Helper()
	if err := self.worker.runFinanceSync(t.Context(), self.run(), self.reload(t)); err != nil {
		t.Fatalf("runFinanceSync: %s", err)
	}
	return self.reload(t)
}

func (self *financeFixture) reload(t *testing.T) *models.AgentKnowledgeSource {
	t.Helper()
	var source *models.AgentKnowledgeSource
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		var err error
		if source, err = tx.GetAgentSource(self.agent.ID, self.source.ID); err != nil {
			t.Fatalf("GetAgentSource: %s", err)
		}
	})
	return source
}

func (self *financeFixture) transactions(t *testing.T) map[string]*models.FinanceTransaction {
	t.Helper()
	byDescription := map[string]*models.FinanceTransaction{}
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		page, err := tx.ListFinanceTransactions(self.agent.ID, &db.FinanceTransactionFilter{Limit: db.FinanceTransactionLimitMost})
		if err != nil {
			t.Fatalf("ListFinanceTransactions: %s", err)
		}
		for _, financeTransaction := range page.FinanceTransactions {
			byDescription[financeTransaction.Description] = financeTransaction
		}
	})
	return byDescription
}

func (self *financeFixture) spendingCategoryIdNamed(t *testing.T, agentId, name string) string {
	t.Helper()
	identifier := ""
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		if _, err := tx.EnsureDefaultSpendingCategories(agentId); err != nil {
			t.Fatalf("EnsureDefaultSpendingCategories: %s", err)
		}
		spendingCategories, err := tx.ListSpendingCategories(agentId)
		if err != nil {
			t.Fatalf("ListSpendingCategories: %s", err)
		}
		for _, spendingCategory := range spendingCategories {
			if spendingCategory.SpendingCategoryName == name {
				identifier = spendingCategory.ID
			}
		}
	})
	if identifier == "" {
		t.Fatalf("no spending category %q", name)
	}
	return identifier
}

func (self *financeFixture) jobsOfKind(t *testing.T, kind models.AgentJobKind) []*models.AgentJob {
	t.Helper()
	var jobs []*models.AgentJob
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		var err error
		if jobs, err = tx.ListAgentJobs(&db.AgentJobFilter{AgentID: self.agent.ID, Kinds: []models.AgentJobKind{kind}}, nil); err != nil {
			t.Fatalf("ListAgentJobs: %s", err)
		}
	})
	return jobs
}

// applySync writes a sync result straight to the database, for a test
// about what comes after.
func (self *financeFixture) applySync(t *testing.T, result *finance.SyncResult) {
	t.Helper()
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		if _, err := tx.ApplyFinanceSync(self.agent.ID, self.source.ID, result, time.Now().UTC().Format(time.DateOnly)); err != nil {
			t.Fatalf("ApplyFinanceSync: %s", err)
		}
	})
}

func inventedAccount() finance.Account {
	return finance.Account{
		ProviderAccountID: "account-1", AccountName: "Everyday Checking", AccountKind: finance.AccountKindDepository,
		CurrencyCode: "USD", CurrentBalance: "1200.0000", BalanceAt: time.Now(), ProviderMetadata: json.RawMessage(`{}`),
	}
}

func inventedTransaction(identifier, postedOn, amount, description, merchantName, providerCategoryDetailed string) finance.Transaction {
	return finance.Transaction{
		ProviderTransactionID: identifier, ProviderAccountID: "account-1", PostedOn: postedOn, Amount: amount, CurrencyCode: "USD",
		Description: description, MerchantName: merchantName, ProviderCategoryDetailed: providerCategoryDetailed,
		ProviderMetadata: json.RawMessage(`{}`),
	}
}

// A sync writes the finance accounts and finance transactions, maps the
// provider category of the one that has one, leaves the other for the
// categorize job it queues, saves the provider's cursor and is due again
// at its schedule; a second sync of the same answer writes nothing new
// and is asked from the saved cursor.
func TestFinanceSyncWritesRowsAndSavesCursor(t *testing.T) {
	fixture := newFinanceFixture(t, "")
	today := time.Now().UTC().Format(time.DateOnly)
	fixture.provider.result = &finance.SyncResult{
		Accounts: []finance.Account{inventedAccount()},
		Added: []finance.Transaction{
			inventedTransaction("transaction-1", today, "-42.17", "CORNER GROCER 0412", "Corner Grocer", "mcc:5411"),
			inventedTransaction("transaction-2", today, "-4.50", "INVENTED KIOSK", "", ""),
		},
		NextCursor:       "1758000000",
		ProviderWarnings: []string{"an invented warning"},
	}
	source := fixture.sync(t)
	if source.LastError != "" {
		t.Fatalf("a sync that worked has no error, its warnings only logged: %q", source.LastError)
	}
	if source.Cursor[models.FinanceCursorProviderCursor] != "1758000000" {
		t.Fatalf("the provider's cursor is saved: %+v", source.Cursor)
	}
	if source.NextRunAt == nil || time.Until(*source.NextRunAt) <= 0 || time.Until(*source.NextRunAt) > 6*time.Hour+time.Minute {
		t.Fatalf("the next sync is at the six-hour schedule: %v", source.NextRunAt)
	}
	if fixture.provider.credentials[0] != financeFixtureCredential || fixture.provider.cursors[0] != "" {
		t.Fatalf("the provider is asked with the opened credential and no cursor: %+v", fixture.provider)
	}
	written := fixture.transactions(t)
	groceriesId := fixture.spendingCategoryIdNamed(t, fixture.agent.ID, finance.SpendingCategoryGroceries)
	if grocer := written["CORNER GROCER 0412"]; grocer == nil || grocer.SpendingCategoryID != groceriesId || grocer.CategorizedBy != models.CategorizedByProviderCategoryMapping {
		t.Fatalf("the grocer's merchant category code maps to groceries: %+v", grocer)
	}
	if kiosk := written["INVENTED KIOSK"]; kiosk == nil || kiosk.SpendingCategoryID != "" {
		t.Fatalf("a finance transaction with no provider category is left uncategorized: %+v", kiosk)
	}
	if jobs := fixture.jobsOfKind(t, models.AgentJobCategorize); len(jobs) != 1 || jobs[0].SubjectID != fixture.agent.ID {
		t.Fatalf("one categorize job is queued for what is left: %+v", jobs)
	}

	fixture.sync(t)
	if len(fixture.transactions(t)) != 2 || fixture.provider.syncCount != 2 || fixture.provider.cursors[1] != "1758000000" {
		t.Fatalf("a second sync adds nothing and is asked from the saved cursor: %d rows, %+v", len(fixture.transactions(t)), fixture.provider.cursors)
	}
}

// A finance source that is due is queued with the knowledge feature off:
// it syncs rows and reads nothing into memory.
func TestDueFinanceSourceIsQueuedWithKnowledgeOff(t *testing.T) {
	fixture := newFinanceFixture(t, "")
	fixture.configuration.Agent.Features.Knowledge = new(bool)
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		source, err := tx.LockAgentSource(fixture.agent.ID, fixture.source.ID)
		if err != nil {
			t.Fatalf("LockAgentSource: %s", err)
		}
		due := time.Now().Add(-time.Minute)
		source.NextRunAt = &due
		if fixture.source, err = tx.PutAgentSource(source); err != nil {
			t.Fatalf("PutAgentSource: %s", err)
		}
	})
	fixture.worker.queueIngestion(t.Context(), time.Now())
	if jobs := fixture.jobsOfKind(t, models.AgentJobIngest); len(jobs) != 1 || jobs[0].SubjectID != fixture.source.ID {
		t.Fatalf("the due finance source is queued: %+v", jobs)
	}
}

// A sign-in the institution wants sets the flag and says so; while it is
// set the provider is not called again.
func TestFinanceSyncStopsWhileSignInIsRequired(t *testing.T) {
	fixture := newFinanceFixture(t, "")
	fixture.provider.err = fmt.Errorf("finance: %w", finance.ErrSignInRequired)
	source := fixture.sync(t)
	if !source.IsFinanceSignInRequired() || source.LastError != financeSignInRequiredError {
		t.Fatalf("a sign-in required is flagged and said: %+v %q", source.Cursor, source.LastError)
	}
	source = fixture.sync(t)
	if fixture.provider.syncCount != 1 || source.LastError != financeSignInRequiredError {
		t.Fatalf("while the flag is set the provider is not called: %d calls, %q", fixture.provider.syncCount, source.LastError)
	}
	if source.NextRunAt == nil || time.Until(*source.NextRunAt) <= 0 {
		t.Fatalf("a flagged source waits for its schedule rather than being due at once: %v", source.NextRunAt)
	}
}

// A credential the provider refuses is not a sign-in: it cannot be
// repaired, so the error says to link again, it is flagged as refused
// rather than as a sign-in, and the provider is not called again.
func TestFinanceSyncSaysARefusedCredentialNeedsLinkingAgain(t *testing.T) {
	fixture := newFinanceFixture(t, "")
	fixture.provider.err = fmt.Errorf("finance: %w", finance.ErrCredentialRefused)
	source := fixture.sync(t)
	if source.IsFinanceSignInRequired() || !source.IsFinanceCredentialRefused() || source.LastError != financeCredentialRefusedError {
		t.Fatalf("a refused credential says to link again and is flagged as refused: %+v %q", source.Cursor, source.LastError)
	}
	fixture.provider.err = nil
	fixture.provider.result = &finance.SyncResult{Accounts: []finance.Account{inventedAccount()}}
	source = fixture.sync(t)
	if fixture.provider.syncCount != 1 || source.LastError != financeCredentialRefusedError || !source.IsFinanceCredentialRefused() {
		t.Fatalf("a refused credential is not tried again: %d calls, %q", fixture.provider.syncCount, source.LastError)
	}
}

// A provider the operator stopped offering is an error on the source, and
// the provider is not called.
func TestFinanceSyncRefusesAProviderNoLongerOffered(t *testing.T) {
	fixture := newFinanceFixture(t, "")
	fixture.configuration.Agent.Finance.OfferedProviders = nil
	source := fixture.sync(t)
	if fixture.provider.syncCount != 0 || !strings.Contains(source.LastError, "no longer offers simplefin") {
		t.Fatalf("a provider no longer offered is not called and is said: %d calls, %q", fixture.provider.syncCount, source.LastError)
	}
}

// Deleting a finance source ends it at its provider first; deleting any
// other source asks nothing of a provider.
func TestBeforeDeletingSourceEndsAFinanceSourceAtItsProvider(t *testing.T) {
	fixture := newFinanceFixture(t, "")
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		if err := fixture.worker.BeforeDeletingSource(t.Context(), tx, fixture.source); err != nil {
			t.Fatalf("BeforeDeletingSource: %s", err)
		}
		if err := fixture.worker.BeforeDeletingSource(t.Context(), tx, &models.AgentKnowledgeSource{Kind: models.SourceComputer}); err != nil {
			t.Fatalf("BeforeDeletingSource: %s", err)
		}
		if err := fixture.worker.BeforeDeletingAgent(t.Context(), tx, fixture.agent.ID); err != nil {
			t.Fatalf("BeforeDeletingAgent: %s", err)
		}
	})
	if fixture.provider.removeCount != 2 {
		t.Fatalf("the finance source is ended at its provider, once for it and once for the agent: %d", fixture.provider.removeCount)
	}
}

// scriptedCategorizer is a decision model that answers by the merchant in
// the state: a choice and a confidence, or an error.
type scriptedCategorizer struct {
	answerByMerchant map[string]decide.Answer
}

func (self *scriptedCategorizer) Decide(_ context.Context, state string, questions map[string]decide.Question) (decide.Answers, error) {
	if _, isAsked := questions[categorizeQuestion]; !isAsked {
		return nil, errors.New("asked something else")
	}
	for merchant, answer := range self.answerByMerchant {
		if strings.Contains(state, "merchant: "+merchant) {
			return decide.Answers{categorizeQuestion: answer}, nil
		}
	}
	return nil, errors.New("the service is down")
}

// With a decision model, an answer above the floor is written with its
// confidence; one below the floor and one the service could not answer go
// to the chat model, whose answer is written only where it names a
// finance transaction it was sent and one of the person's own spending
// categories: another agent's spending category and an invented label
// are dropped.
func TestCategorizeTakesTheDecisionWhereSureAndAsksAChatModelElsewhere(t *testing.T) {
	model := &alertModel{}
	server := model.serve(t)
	fixture := newFinanceFixture(t, server.URL)
	stranger := ""
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		other, err := tx.CreateUser(&models.User{Username: "sam", Name: "Sam Example", Timezone: "UTC"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		otherAgent, err := tx.CreateAgent(&models.Agent{UserID: other.ID, Enabled: true})
		if err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		stranger = otherAgent.ID
	})
	groceriesId := fixture.spendingCategoryIdNamed(t, fixture.agent.ID, finance.SpendingCategoryGroceries)
	diningId := fixture.spendingCategoryIdNamed(t, fixture.agent.ID, finance.SpendingCategoryDining)
	strangersDiningId := fixture.spendingCategoryIdNamed(t, stranger, finance.SpendingCategoryDining)
	fixture.applySync(t, &finance.SyncResult{
		Accounts: []finance.Account{inventedAccount()},
		Added: []finance.Transaction{
			inventedTransaction("transaction-1", "2026-09-21", "-42.17", "CORNER GROCER 0412", "Corner Grocer", ""),
			inventedTransaction("transaction-2", "2026-09-20", "-38.00", "NIGHT OWL DINER", "Night Owl Diner", ""),
			inventedTransaction("transaction-3", "2026-09-19", "-12.00", "MYSTERY CHARGE", "Mystery Charge", ""),
		},
	})
	original := categorizeModels
	categorizeModels = func(*Agent, *config.Configuration) (llm.Decider, string) {
		return &scriptedCategorizer{answerByMerchant: map[string]decide.Answer{
			"Corner Grocer":   {Choice: groceriesId, Confidence: 0.9},
			"Night Owl Diner": {Choice: diningId, Confidence: 0.4},
		}}, "p:thinker"
	}
	t.Cleanup(func() { categorizeModels = original })
	// The unsure ones go newest first: t1 is the diner, t2 the mystery.
	model.answers = []string{fmt.Sprintf(`{"categorizations": [{"transactionId": "t1", "spendingCategoryId": %q}, {"transactionId": "t2", "spendingCategoryId": %q}, {"transactionId": "t9", "spendingCategoryId": %q}]}`,
		diningId, strangersDiningId, diningId)}

	if err := fixture.worker.runCategorize(t.Context(), fixture.run()); err != nil {
		t.Fatalf("runCategorize: %s", err)
	}
	written := fixture.transactions(t)
	grocer := written["CORNER GROCER 0412"]
	if grocer.SpendingCategoryID != groceriesId || grocer.CategorizedBy != models.CategorizedByCategorizeModel || grocer.CategorizationConfidence == "" || !strings.HasPrefix(grocer.CategorizationConfidence, "0.9") {
		t.Fatalf("an answer above the floor is written with its confidence: %+v", grocer)
	}
	diner := written["NIGHT OWL DINER"]
	if diner.SpendingCategoryID != diningId || diner.CategorizedBy != models.CategorizedByCategorizeModel || diner.CategorizationConfidence != "" {
		t.Fatalf("an answer below the floor goes to the chat model, which placed it: %+v", diner)
	}
	if mystery := written["MYSTERY CHARGE"]; mystery.SpendingCategoryID != "" {
		t.Fatalf("another agent's spending category is dropped: %+v", mystery)
	}
	if model.callCount() != 1 {
		t.Fatalf("one batch goes to the chat model: %d calls", model.callCount())
	}
	prompt := model.prompts[0]
	if !strings.Contains(prompt, "Night Owl Diner") || !strings.Contains(prompt, "Mystery Charge") || strings.Contains(prompt, "Corner Grocer") {
		t.Fatalf("only the unsure are sent to the chat model")
	}
	if strings.Contains(prompt, "account-1") || strings.Contains(prompt, "providerMetadata") {
		t.Fatalf("no account number or provider metadata is sent")
	}
}

// The categorize model resolves to the decider path when it names a
// decision model and to the chat path when it names a chat model.
func TestCategorizeModelsResolvesAChatModel(t *testing.T) {
	configuration := config.Default()
	configuration.Agent.Enabled = true
	configuration.Agent.Providers = []config.AgentProvider{{Name: "p", Kind: "openai", BaseURL: "http://model.example.com", APIKey: "k"}}
	configuration.Agent.Models.Default = "p:thinker"
	configuration.Agent.Models.Categorize = "p:cheap"
	registry, err := llm.Open(&configuration.Agent)
	if err != nil {
		t.Fatalf("llm.Open: %s", err)
	}
	worker := &Agent{settings: &Settings{Registry: registry}}
	decider, chatModelName := categorizeModels(worker, configuration)
	if decider != nil || chatModelName != "p:cheap" {
		t.Fatalf("a chat categorize model is the chat path: %v %q", decider, chatModelName)
	}
	configuration.Agent.Models.Categorize = ""
	if decider, chatModelName = categorizeModels(worker, configuration); decider != nil || chatModelName != "p:thinker" {
		t.Fatalf("with nothing set it falls back to what the scan runs on: %v %q", decider, chatModelName)
	}
}

// A spending category past eighty percent of its budget writes one budget
// candidate, and a second look the same day writes none; once the person
// mutes that spending category, going over writes none either.
func TestBudgetCandidateWrittenOncePerKeyAndMutedSpendingCategorySkipped(t *testing.T) {
	fixture := newFinanceFixture(t, "")
	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	lastDay := monthStart.AddDate(0, 1, -1)
	at := time.Date(lastDay.Year(), lastDay.Month(), lastDay.Day(), 12, 0, 0, 0, time.UTC)
	month := monthStart.Format("2006-01")
	groceriesId := fixture.spendingCategoryIdNamed(t, fixture.agent.ID, finance.SpendingCategoryGroceries)
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		if _, err := tx.SetBudget(&models.Budget{AgentID: fixture.agent.ID, SpendingCategoryID: groceriesId, MonthlyAmount: "400", CurrencyCode: "USD", EffectiveFrom: monthStart.Format(time.DateOnly)}); err != nil {
			t.Fatalf("SetBudget: %s", err)
		}
	})
	fixture.applySync(t, &finance.SyncResult{
		Accounts: []finance.Account{inventedAccount()},
		Added:    []finance.Transaction{inventedTransaction("transaction-1", monthStart.Format(time.DateOnly), "-340.00", "CORNER GROCER 0412", "Corner Grocer", "mcc:5411")},
	})
	categorize := func() {
		dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
			page, err := tx.ListFinanceTransactions(fixture.agent.ID, &db.FinanceTransactionFilter{})
			if err != nil {
				t.Fatalf("ListFinanceTransactions: %s", err)
			}
			for _, financeTransaction := range page.FinanceTransactions {
				if _, err := tx.SetTransactionCategorization(fixture.agent.ID, financeTransaction.ID, groceriesId, models.CategorizedByPerson, nil); err != nil {
					t.Fatalf("SetTransactionCategorization: %s", err)
				}
			}
		})
	}
	categorize()
	waiting := func() []*models.AgentAlertCandidate {
		var candidates []*models.AgentAlertCandidate
		dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
			var err error
			if candidates, err = tx.ListWaitingAgentAlertCandidates(fixture.agent.ID, 0); err != nil {
				t.Fatalf("ListWaitingAgentAlertCandidates: %s", err)
			}
		})
		return candidates
	}

	if err := fixture.worker.noteBudgetCandidates(t.Context(), fixture.run(), at); err != nil {
		t.Fatalf("noteBudgetCandidates: %s", err)
	}
	candidates := waiting()
	expectedKey := "spending-category:" + groceriesId + ":" + month + ":80_percent"
	if len(candidates) != 1 || candidates[0].CandidateKind != models.AlertCandidateBudget || candidates[0].BudgetKey != expectedKey ||
		candidates[0].AlertSignal != models.AlertSignalSoon || !strings.Contains(candidates[0].CandidateReason, "340.00 of 400.00 USD") {
		t.Fatalf("eighty-five percent writes one budget candidate with the numbers: %+v", candidates)
	}
	if jobs := fixture.jobsOfKind(t, models.AgentJobAlert); len(jobs) != 1 {
		t.Fatalf("the alert job is queued: %+v", jobs)
	}
	if err := fixture.worker.noteBudgetCandidates(t.Context(), fixture.run(), at); err != nil {
		t.Fatalf("noteBudgetCandidates: %s", err)
	}
	if candidates := waiting(); len(candidates) != 1 {
		t.Fatalf("the same crossing is written once: %+v", candidates)
	}

	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		if _, err := MuteAlert(tx, fixture.agent, "", models.AlertMuteSpendingCategory, groceriesId); err != nil {
			t.Fatalf("MuteAlert: %s", err)
		}
	})
	fixture.applySync(t, &finance.SyncResult{
		Accounts: []finance.Account{inventedAccount()},
		Added:    []finance.Transaction{inventedTransaction("transaction-2", monthStart.Format(time.DateOnly), "-100.00", "CORNER GROCER 0413", "Corner Grocer", "mcc:5411")},
	})
	categorize()
	if err := fixture.worker.noteBudgetCandidates(t.Context(), fixture.run(), at); err != nil {
		t.Fatalf("noteBudgetCandidates: %s", err)
	}
	if candidates := waiting(); len(candidates) != 1 {
		t.Fatalf("a muted spending category writes nothing when it goes over: %+v", candidates)
	}
}

// A budget candidate's facts are what a mute of it names: its spending
// category, the budget kind, and its key.
func TestBudgetCandidateFactsMatchTheBudgetMutes(t *testing.T) {
	candidate := &models.AgentAlertCandidate{CandidateKind: models.AlertCandidateBudget, BudgetKey: "spending-category:category-1:2026-09:at_risk"}
	facts := candidateFacts(candidate, "", "")
	for _, mute := range []*models.AgentAlertMute{
		{MuteScope: models.AlertMuteSpendingCategory, MuteTarget: "category-1"},
		{MuteScope: models.AlertMuteKind, MuteTarget: models.AlertKindBudget},
		{MuteScope: models.AlertMuteSubjectKey, MuteTarget: normalizedSubjectKey(candidate.BudgetKey)},
	} {
		if mutedBy([]*models.AgentAlertMute{mute}, facts) == nil {
			t.Fatalf("a mute of %s %q matches the candidate", mute.MuteScope, mute.MuteTarget)
		}
	}
	if mutedBy([]*models.AgentAlertMute{{MuteScope: models.AlertMuteSpendingCategory, MuteTarget: "category-2"}}, facts) != nil {
		t.Fatal("another spending category's mute does not match")
	}
	if key := alertSubjectKey("whatever the model called it", candidate); key != candidate.BudgetKey {
		t.Fatalf("a budget alert's subject is its key: %q", key)
	}
}

// queuedJobsOfKind is the jobs of a kind still waiting to run.
func (self *financeFixture) queuedJobsOfKind(t *testing.T, kind models.AgentJobKind) []*models.AgentJob {
	t.Helper()
	var queued []*models.AgentJob
	for _, job := range self.jobsOfKind(t, kind) {
		if job.Status == models.AgentJobQueued {
			queued = append(queued, job)
		}
	}
	return queued
}

// A categorize run that places nothing marks what it was asked about, so
// neither it nor the sync after it brings the job back for the same; a
// finance transaction whose merchant then changes is asked about again. A
// call the decision model failed is not an answer, and is asked again.
func TestCategorizeRunThatPlacesNothingIsNotRepeated(t *testing.T) {
	fixture := newFinanceFixture(t, "")
	fixture.spendingCategoryIdNamed(t, fixture.agent.ID, finance.SpendingCategoryGroceries)
	fixture.applySync(t, &finance.SyncResult{
		Accounts: []finance.Account{inventedAccount()},
		Added: []finance.Transaction{
			inventedTransaction("transaction-1", "2026-09-19", "-12.00", "MYSTERY CHARGE", "Mystery Charge", ""),
			inventedTransaction("transaction-2", "2026-09-18", "-7.00", "UNREACHABLE CHARGE", "Unreachable Charge", ""),
		},
	})
	original := categorizeModels
	categorizeModels = func(*Agent, *config.Configuration) (llm.Decider, string) {
		// Unsure about the mystery; the service is down for the other.
		return &scriptedCategorizer{answerByMerchant: map[string]decide.Answer{
			"Mystery Charge": {Choice: "not-a-spending-category", Confidence: 0.2},
		}}, ""
	}
	t.Cleanup(func() { categorizeModels = original })

	if err := fixture.worker.runCategorize(t.Context(), fixture.run()); err != nil {
		t.Fatalf("a run that placed nothing does not bring itself back: %v", err)
	}
	uncategorized := func() []*models.FinanceTransaction {
		var found []*models.FinanceTransaction
		dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
			var err error
			if found, err = tx.ListUncategorizedFinanceTransactions(fixture.agent.ID, 10); err != nil {
				t.Fatalf("ListUncategorizedFinanceTransactions: %s", err)
			}
		})
		return found
	}
	if waiting := uncategorized(); len(waiting) != 1 || waiting[0].Description != "UNREACHABLE CHARGE" {
		t.Fatalf("the one the model answered is not asked again; the one it could not be asked about is: %+v", waiting)
	}

	// With nothing left but what the model could not be reached for
	// resolved, a sync after it queues nothing.
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		if _, err := tx.MarkCategorizeAttempted(fixture.agent.ID, []string{uncategorized()[0].ID}); err != nil {
			t.Fatalf("MarkCategorizeAttempted: %s", err)
		}
	})
	afterSync := func() {
		if err := fixture.worker.afterFinanceSync(t.Context(), fixture.run(), fixture.source, &db.FinanceSyncApplied{}, time.Now().UTC().Format(time.DateOnly), false); err != nil {
			t.Fatalf("afterFinanceSync: %s", err)
		}
	}
	afterSync()
	if queued := fixture.queuedJobsOfKind(t, models.AgentJobCategorize); len(queued) != 0 {
		t.Fatalf("nothing the model has not already been asked about waits, so no job: %+v", queued)
	}

	changed := inventedTransaction("transaction-1", "2026-09-19", "-12.00", "MYSTERY CHARGE", "Mystery Charge Online", "")
	fixture.applySync(t, &finance.SyncResult{Accounts: []finance.Account{inventedAccount()}, Added: []finance.Transaction{changed}})
	if waiting := uncategorized(); len(waiting) != 1 || waiting[0].MerchantName != "Mystery Charge Online" {
		t.Fatalf("a changed merchant is asked about again: %+v", waiting)
	}
	afterSync()
	if queued := fixture.queuedJobsOfKind(t, models.AgentJobCategorize); len(queued) != 1 {
		t.Fatalf("and a job is queued for it: %+v", queued)
	}
}

// What a merchant charged two counted spending categories this month is
// added together, not the one read last.
func TestFixedChargesAddWhatAMerchantChargedThisMonth(t *testing.T) {
	counted := map[string]bool{"category-parent": true, "category-child": true}
	history := []*models.MerchantMonthSpending{}
	for _, spendingMonth := range []string{"2026-06", "2026-07", "2026-08"} {
		history = append(history, &models.MerchantMonthSpending{SpendingCategoryID: "category-parent", MerchantName: "Example Gym",
			SpendingMonth: spendingMonth, CurrencyCode: "USD", SpendingAmount: "50.0000"})
	}
	recent := []*models.MerchantMonthSpending{
		{SpendingCategoryID: "category-parent", MerchantName: "Example Gym", SpendingMonth: "2026-09", CurrencyCode: "USD", SpendingAmount: "30.0000"},
		{SpendingCategoryID: "category-child", MerchantName: "Example Gym", SpendingMonth: "2026-09", CurrencyCode: "USD", SpendingAmount: "20.0000"},
	}
	charges, err := fixedCharges(rates.NewConverter(t.Context(), nil, nil), counted, history, recent, "2026-09", "USD", "2026-09-10")
	if err != nil {
		t.Fatalf("fixedCharges: %s", err)
	}
	if finance.FormatAmount(charges.seenAmount) != "50.0000" || charges.dueAmount.Sign() != 0 {
		t.Fatalf("seen is both charges and nothing is still due: seen %s, due %s", finance.FormatAmount(charges.seenAmount), finance.FormatAmount(charges.dueAmount))
	}
}

// Spending last month and a regular charge still due in a currency with
// no exchange rate are reported, not dropped.
func TestBudgetStatusReportsWhatItCouldNotConvert(t *testing.T) {
	fixture := newFinanceFixture(t, "")
	groceriesId := fixture.spendingCategoryIdNamed(t, fixture.agent.ID, finance.SpendingCategoryGroceries)
	charge := func(identifier, postedOn string) finance.Transaction {
		transaction := inventedTransaction(identifier, postedOn, "-15.00", "EXAMPLE BOX", "Example Box", "")
		transaction.CurrencyCode = "XTS"
		return transaction
	}
	fixture.applySync(t, &finance.SyncResult{Accounts: []finance.Account{inventedAccount()}, Added: []finance.Transaction{
		charge("box-march", "2026-03-05"), charge("box-april", "2026-04-05"), charge("box-may", "2026-05-05"),
	}})
	var status *models.BudgetStatus
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		page, err := tx.ListFinanceTransactions(fixture.agent.ID, &db.FinanceTransactionFilter{})
		if err != nil {
			t.Fatalf("ListFinanceTransactions: %s", err)
		}
		for _, financeTransaction := range page.FinanceTransactions {
			if _, err := tx.SetTransactionCategorization(fixture.agent.ID, financeTransaction.ID, groceriesId, models.CategorizedByPerson, nil); err != nil {
				t.Fatalf("SetTransactionCategorization: %s", err)
			}
		}
		if _, err := tx.SetBudget(&models.Budget{AgentID: fixture.agent.ID, SpendingCategoryID: groceriesId, MonthlyAmount: "400", CurrencyCode: "USD", EffectiveFrom: "2026-03"}); err != nil {
			t.Fatalf("SetBudget: %s", err)
		}
		if status, err = BudgetStatus(t.Context(), tx, nil, fixture.agent.ID, "2026-06", "2026-06-10"); err != nil {
			t.Fatalf("BudgetStatus: %s", err)
		}
	})
	if len(status.SpendingCategories) != 1 {
		t.Fatalf("one spending category with a budget: %+v", status)
	}
	row := status.SpendingCategories[0]
	if len(row.UnconvertedSpendingBySameDayLastMonth) != 1 || row.UnconvertedSpendingBySameDayLastMonth[0].CurrencyCode != "XTS" ||
		row.UnconvertedSpendingBySameDayLastMonth[0].Amount != "15.0000" || row.SpendingBySameDayLastMonthAmount != "0.0000" {
		t.Errorf("last month's spending with no rate is reported apart: %+v", row.UnconvertedSpendingBySameDayLastMonth)
	}
	if len(row.UnconvertedFixedChargesDue) != 1 || row.UnconvertedFixedChargesDue[0].Amount != "15.0000" || row.FixedChargesDueAmount != "0.0000" {
		t.Errorf("a regular charge due with no rate is reported apart: %+v", row.UnconvertedFixedChargesDue)
	}
	if len(row.UnconvertedSpending) != 0 {
		t.Errorf("nothing was spent this month: %+v", row.UnconvertedSpending)
	}
}

// An asset value savings target counts what its assets are worth today:
// not an asset sold before today, and not a valuation recorded for a day
// still to come.
func TestSavingsTargetProgressCountsWhatTheAssetsAreWorthToday(t *testing.T) {
	fixture := newFinanceFixture(t, "")
	agentId := fixture.agent.ID
	var progress *SavingsTargetProgress
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		fund, err := tx.CreateAsset(&models.Asset{AgentID: agentId, AssetName: "index fund", AssetKind: models.AssetKindInvestment, CurrencyCode: "USD"})
		if err != nil {
			t.Fatalf("CreateAsset: %s", err)
		}
		boat, err := tx.CreateAsset(&models.Asset{AgentID: agentId, AssetName: "boat", AssetKind: models.AssetKindVehicle, CurrencyCode: "USD"})
		if err != nil {
			t.Fatalf("CreateAsset: %s", err)
		}
		for _, valuation := range []*models.AssetValuation{
			{AgentID: agentId, AssetID: fund.ID, ValuedOn: "2026-06-01", Value: "1000", ValuationSource: models.ValuationSourceManual},
			{AgentID: agentId, AssetID: fund.ID, ValuedOn: "2026-07-01", Value: "5000", ValuationSource: models.ValuationSourceManual},
			{AgentID: agentId, AssetID: boat.ID, ValuedOn: "2026-06-01", Value: "2000", ValuationSource: models.ValuationSourceManual},
		} {
			if _, err := tx.RecordValuation(valuation); err != nil {
				t.Fatalf("RecordValuation: %s", err)
			}
		}
		if _, err := tx.CloseAsset(agentId, boat.ID, "2026-06-05"); err != nil {
			t.Fatalf("CloseAsset: %s", err)
		}
		savingsTarget, err := tx.CreateSavingsTarget(&models.SavingsTarget{AgentID: agentId, SavingsTargetName: "house deposit", TargetAmount: "10000",
			CurrencyCode: "USD", TargetOn: "2027-06-01", TargetMeasure: models.TargetMeasureAssetValue, StartedOn: "2026-06-01",
			AssetIDs: []string{fund.ID, boat.ID}})
		if err != nil {
			t.Fatalf("CreateSavingsTarget: %s", err)
		}
		if progress, err = SavingsTargetProgressOf(t.Context(), tx, nil, agentId, savingsTarget, "2026-06-10"); err != nil {
			t.Fatalf("SavingsTargetProgressOf: %s", err)
		}
	})
	if progress.SavedAmount != "1000.0000" || progress.RemainingAmount != "9000.0000" {
		t.Fatalf("only the fund, at today's value: %+v", progress)
	}
}
