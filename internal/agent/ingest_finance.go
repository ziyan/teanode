package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

// A finance source's sync. It rides on the source machinery -- the row's
// schedule, cursor, last error, switch and sealed credential, and the
// ingest job that runs a due source -- and on nothing else of it: it files
// rows of finance accounts and finance transactions, not documents, so it
// has no passes, no sweep and nothing to embed, and it does not wait on
// the knowledge feature, which is about reading documents into memory.

const (
	// financeTransferWindowDays is how far back transfer detection looks
	// after a sync: long enough for both sides of a card payment or a
	// transfer between banks to have posted, which can take a few days
	// across a weekend.
	financeTransferWindowDays = 7

	// financeRemoveTimeout bounds the provider call that ends a finance
	// source when it is deleted. Best effort: a provider that does not
	// answer must not keep the person from deleting.
	financeRemoveTimeout = 20 * time.Second

	// What a finance source's last error says for the two refusals that
	// only the person can mend.
	financeSignInRequiredError    = "the institution needs you to sign in again"
	financeCredentialRefusedError = "the institution's access was revoked at the provider; delete this finance source and link the institution again"
)

// newFinanceProvider builds the provider a finance source syncs through,
// from the operator's current settings, reading days that are moments
// rather than dates in the person's time zone (location, nil for UTC). A
// variable so a test can hand in a provider that answers to order.
var newFinanceProvider = func(configuration *config.Configuration, providerKind string, location *time.Location) (finance.Provider, error) {
	settings := &configuration.Agent.Finance
	switch providerKind {
	case config.AgentFinanceProviderPlaid:
		plaid := &settings.Plaid
		return finance.NewPlaid(plaid.Environment, plaid.ClientID, plaid.Secret, plaid.ResolvedCountryCodes(), plaid.ResolvedProducts())
	case config.AgentFinanceProviderSimpleFIN:
		return finance.NewSimpleFIN().InLocation(location), nil
	}
	return nil, fmt.Errorf("there is no provider called %q", providerKind)
}

// runFinanceSync is the ingest job for a finance source: one sync, then
// what follows from the rows it wrote.
func (self *Agent) runFinanceSync(ctx context.Context, run *Run, source *models.AgentKnowledgeSource) error {
	configuration := run.Configuration()
	cursor := map[string]any{}
	for key, value := range source.Cursor {
		cursor[key] = value
	}
	nextRun := self.nextRunOf(source, run.Owner)
	mark := func(failure string) error {
		// With a context that outlives the deadline, as runIngest does:
		// this is the write that says the sync happened.
		markContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		err := self.markSource(markContext, source, cursor, db.SourceCounts{}, false, failure, nextRun)
		if errors.Is(err, errIngestSourceChanged) {
			return nil
		}
		return err
	}

	providerKind := source.Specification.Type
	if !configuration.Agent.Finance.Offers(providerKind) {
		return mark(fmt.Sprintf("this server no longer offers %s, so the institution cannot be synced; ask the operator", providerKind))
	}
	if source.IsFinanceSignInRequired() {
		// Asking again would fail the same way, and some providers count a
		// refused call against the finance source; the repair clears the
		// flag and makes the source due.
		failure := source.LastError
		if failure == "" {
			failure = financeSignInRequiredError
		}
		return mark(failure)
	}
	if source.IsFinanceCredentialRefused() {
		// The same credential is refused every time, and a provider may
		// count each refused call; only a new link mends it, and a new
		// link is a new finance source.
		return mark(financeCredentialRefusedError)
	}

	credential, err := self.financeCredential(ctx, source)
	if err != nil {
		return mark(err.Error())
	}
	provider, err := newFinanceProvider(configuration, providerKind, Location(run.Owner))
	if err != nil {
		return mark(err.Error())
	}
	if err := self.checkSourceRead(ctx, source); err != nil {
		if errors.Is(err, errIngestSourceChanged) {
			return nil
		}
		return err
	}
	providerCursor, _ := cursor[models.FinanceCursorProviderCursor].(string)
	syncResult, err := provider.Sync(ctx, credential, providerCursor)
	switch {
	case errors.Is(err, finance.ErrSignInRequired):
		cursor[models.FinanceCursorIsSignInRequired] = true
		return mark(financeSignInRequiredError)
	case errors.Is(err, finance.ErrCredentialRefused):
		cursor[models.FinanceCursorIsCredentialRefused] = true
		return mark(financeCredentialRefusedError)
	case err != nil:
		return mark(err.Error())
	}

	// A provider's warnings (SimpleFIN's "errors" list: a capped date
	// range, an institution that did not answer this time) are logged and
	// not shown as the source's error: the sync worked, and a row that
	// reads as failed after every sync teaches the person to ignore it.
	for _, warning := range syncResult.ProviderWarnings {
		log.Infof("finance source %q: the provider warned: %s", source.ID, warning)
	}

	syncedOn := time.Now().In(Location(run.Owner)).Format(time.DateOnly)
	var applied *db.FinanceSyncApplied
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		// Under the source's row lock, and only if it is still the source
		// the sync began with: a finance source deleted or switched off
		// while its provider answered writes nothing.
		if err := lockIngestSource(tx, source); err != nil {
			return err
		}
		applied, err = tx.ApplyFinanceSync(source.AgentID, source.ID, syncResult, syncedOn)
		return err
	}); err != nil {
		if errors.Is(err, errIngestSourceChanged) {
			return nil
		}
		return mark(err.Error())
	}
	cursor[models.FinanceCursorProviderCursor] = syncResult.NextCursor
	delete(cursor, models.FinanceCursorIsSignInRequired)
	log.Debugf("finance source %q synced: %d finance transaction(s) written, %d removed, %d pending replaced",
		source.ID, applied.WrittenTransactionCount, applied.RemovedTransactionCount, applied.ReplacedPendingTransactionCount)

	if err := self.afterFinanceSync(ctx, run, source, applied, syncedOn); err != nil {
		// The rows are written and the cursor moves on: what follows a
		// sync is repeated after the next one, so it is logged here rather
		// than making the sync look failed.
		log.Warningf("finance source %q synced, but what follows a sync failed: %s", source.ID, err)
	}
	return mark("")
}

// financeCredential opens a finance source's credential.
func (self *Agent) financeCredential(ctx context.Context, source *models.AgentKnowledgeSource) (string, error) {
	var sealed string
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		secrets, err := tx.ListAgentSourceSecrets(source.ID)
		if err != nil {
			return err
		}
		for _, secret := range secrets {
			if secret.Key == models.FinanceCredentialSecretKey {
				sealed = secret.Value
			}
		}
		return nil
	}); err != nil {
		return "", err
	}
	if sealed == "" {
		return "", errors.New("the finance source has no credential; delete it and link the institution again")
	}
	credential, err := self.OpenSecret(sealed)
	if err != nil {
		return "", fmt.Errorf("the finance source's credential cannot be opened: %w", err)
	}
	return credential, nil
}

// afterFinanceSync is what follows a sync that wrote rows: the default
// spending categories for an agent that has none, transfer detection over
// the last week, the person's spending rules, the provider category
// mapping for what is still uncategorized, a categorize job for what is
// left, and budget alert candidates.
func (self *Agent) afterFinanceSync(ctx context.Context, run *Run, source *models.AgentKnowledgeSource, applied *db.FinanceSyncApplied, syncedOn string) error {
	agentId := source.AgentID
	today, err := time.Parse(time.DateOnly, syncedOn)
	if err != nil {
		return err
	}
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		if _, err := tx.EnsureDefaultSpendingCategories(agentId); err != nil {
			return err
		}
		if _, err := tx.DetectFinanceTransfers(agentId, source.ID, today.AddDate(0, 0, -financeTransferWindowDays).Format(time.DateOnly)); err != nil {
			return err
		}
		if _, err := tx.ApplySpendingRules(agentId); err != nil {
			return err
		}
		if err := applyProviderCategoryMapping(tx, agentId, applied.FinanceTransactionIDsToCategorize); err != nil {
			return err
		}
		uncategorized, err := tx.ListUncategorizedFinanceTransactions(agentId, 1)
		if err != nil {
			return err
		}
		if len(uncategorized) > 0 {
			if _, err := self.Enqueue(tx, models.AgentJobCategorize, agentId, "", agentId); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	return self.noteBudgetCandidates(ctx, run, time.Now())
}

// applyProviderCategoryMapping gives the finance transactions a sync
// wrote the default spending category their provider category maps to,
// where the agent still has a spending category of that name and nothing
// before it (the person, a spending rule) has categorized them. A mapping
// that says transfer marks the finance transaction as one.
func applyProviderCategoryMapping(tx db.Transaction, agentId string, financeTransactionIds []string) error {
	if len(financeTransactionIds) == 0 {
		return nil
	}
	spendingCategories, err := tx.ListSpendingCategories(agentId)
	if err != nil {
		return err
	}
	spendingCategoryIdByName := map[string]string{}
	for _, spendingCategory := range spendingCategories {
		spendingCategoryIdByName[strings.ToLower(strings.TrimSpace(spendingCategory.SpendingCategoryName))] = spendingCategory.ID
	}
	for _, financeTransactionId := range financeTransactionIds {
		financeTransaction, err := tx.GetFinanceTransaction(agentId, financeTransactionId)
		if err != nil {
			return err
		}
		if financeTransaction == nil || financeTransaction.SpendingCategoryID != "" || financeTransaction.IsTransfer ||
			financeTransaction.TransferMarkedBy == models.TransferMarkedByPerson || financeTransaction.CategorizedBy == models.CategorizedByPerson {
			continue
		}
		spendingCategoryName, isTransfer := finance.MapProviderCategory(financeTransaction.ProviderCategoryPrimary, financeTransaction.ProviderCategoryDetailed)
		if isTransfer {
			if _, err := tx.MarkFinanceTransactionTransfer(agentId, financeTransaction.ID, true, models.TransferMarkedByProviderCategoryMapping); err != nil {
				return err
			}
			continue
		}
		spendingCategoryId := spendingCategoryIdByName[spendingCategoryName]
		if spendingCategoryName == "" || spendingCategoryId == "" {
			continue
		}
		if _, err := tx.SetTransactionCategorization(agentId, financeTransaction.ID, spendingCategoryId, models.CategorizedByProviderCategoryMapping, nil); err != nil {
			return err
		}
	}
	return nil
}

// BeforeDeletingSource is what deleting a finance source does first, in
// the deleting transaction: it ends the finance source at its provider,
// best effort, so the operator stops paying for a link nobody can reach,
// and turns the assets its finance accounts valued into manual ones so
// their history outlives the source. Any other kind of source needs
// nothing. The API calls it before DeleteAgentSource.
func (self *Agent) BeforeDeletingSource(ctx context.Context, tx db.Transaction, source *models.AgentKnowledgeSource) error {
	if source == nil || source.Kind != models.SourceFinance {
		return nil
	}
	self.removeFinanceSourceAtProvider(ctx, tx, source)
	_, err := tx.DetachAssetsOfSource(source.AgentID, source.ID)
	return err
}

// BeforeDeletingAgent ends each of an agent's finance sources at its
// provider, best effort, before the agent and everything it has are
// deleted. The API calls it before DeleteAgent.
func (self *Agent) BeforeDeletingAgent(ctx context.Context, tx db.Transaction, agentId string) error {
	sources, err := tx.ListAgentSources(agentId)
	if err != nil {
		return err
	}
	for _, source := range sources {
		if source.Kind == models.SourceFinance {
			self.removeFinanceSourceAtProvider(ctx, tx, source)
		}
	}
	return nil
}

// removeFinanceSourceAtProvider calls the provider's Remove with the
// finance source's credential, logging rather than failing: a deletion the
// person asked for goes ahead whatever the provider says.
func (self *Agent) removeFinanceSourceAtProvider(ctx context.Context, tx db.Transaction, source *models.AgentKnowledgeSource) {
	secrets, err := tx.ListAgentSourceSecrets(source.ID)
	if err != nil {
		log.Warningf("cannot read the credential of finance source %q to end it at its provider: %s", source.ID, err)
		return
	}
	sealed := ""
	for _, secret := range secrets {
		if secret.Key == models.FinanceCredentialSecretKey {
			sealed = secret.Value
		}
	}
	if sealed == "" {
		return
	}
	credential, err := self.OpenSecret(sealed)
	if err != nil {
		log.Warningf("cannot open the credential of finance source %q to end it at its provider: %s", source.ID, err)
		return
	}
	provider, err := newFinanceProvider(self.settings.Configuration(), source.Specification.Type, nil)
	if err != nil {
		log.Warningf("cannot end finance source %q at its provider: %s", source.ID, err)
		return
	}
	removeContext, cancel := context.WithTimeout(ctx, financeRemoveTimeout)
	defer cancel()
	if err := provider.Remove(removeContext, credential); err != nil {
		log.Warningf("the provider did not end finance source %q: %s", source.ID, err)
		return
	}
	log.Noticef("ended finance source %q at %s", source.ID, source.Specification.Type)
}
