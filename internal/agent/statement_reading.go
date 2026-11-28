package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

// Reading a statement the person handed in: a screenshot of an account's
// transactions in its app, a photo of a paper statement, or a PDF, shared
// or uploaded to be imported. The statement job reads its rows with a
// model and imports them the way rows the agent reads are imported
// (ImportTransactionRows), and posts what came of it to the person's
// feed. An OFX file needs no reading and is imported at once
// (ImportStatementFiles).

// QueueStatementReading queues the job that reads the transactions of an
// uploaded picture or PDF and imports them.
func (self *Agent) QueueStatementReading(tx db.Transaction, agentId, attachmentId string) (*models.AgentJob, error) {
	return self.Enqueue(tx, models.AgentJobStatementPhoto, agentId, "", attachmentId)
}

// rowAccounts are the agent's accounts rows are matched against when
// nobody says which they are of: those of imported statements, and those a
// provider syncs.
func (self *Agent) rowAccounts(ctx context.Context, agentId string) (statementAccounts, providerAccounts []finance.ExistingStatementAccount, err error) {
	err = self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		sources, err := tx.ListAgentSources(agentId)
		if err != nil {
			return err
		}
		sourceOf := map[string]*models.AgentKnowledgeSource{}
		for _, source := range sources {
			sourceOf[source.ID] = source
			if IsStatementSource(source) {
				if statementAccounts, err = existingStatementAccountsOf(tx, agentId, source); err != nil {
					return err
				}
			}
		}
		accounts, err := tx.ListFinanceAccounts(agentId, "")
		if err != nil {
			return err
		}
		for _, account := range accounts {
			source := sourceOf[account.SourceID]
			if source == nil || source.Kind != models.SourceFinance || IsStatementSource(source) {
				continue
			}
			providerAccounts = append(providerAccounts, finance.ExistingStatementAccount{
				FinanceAccountID: account.ID, AccountName: account.AccountName, AccountMask: account.AccountMask, AccountKind: string(account.AccountKind),
				CurrencyCode: account.CurrencyCode, InstitutionName: account.InstitutionName(source), ProviderMetadata: account.ProviderMetadata,
			})
		}
		return nil
	})
	return statementAccounts, providerAccounts, err
}

// syncedAccountWhy says why rows were not imported beside the synced
// accounts they look like.
func syncedAccountWhy(like []*finance.ExistingStatementAccount) string {
	return fmt.Sprintf("it looks like %s, which is already synced from the bank, so importing it would count its transactions twice", strings.Join(finance.AccountNames(like), " or "))
}

// chooseHandedInAccount picks the account for rows handed in that show no
// account number, which an account of imported statements cannot be
// started from: the one account of imported statements at the
// institution, of the kind and currency. Otherwise it says, for the
// person, why nothing is imported.
func (self *Agent) chooseHandedInAccount(ctx context.Context, agentId string, input *finance.TransactionRowsInput) (string, error) {
	statementAccounts, providerAccounts, err := self.rowAccounts(ctx, agentId)
	if err != nil {
		return "", err
	}
	kind := finance.StatementAccountKind(input.StatementAccountKind)
	if like := finance.AccountsLikeRows(input.InstitutionName, kind, input.CurrencyCode, "", providerAccounts); len(like) > 0 {
		return syncedAccountWhy(like), nil
	}
	like := finance.AccountsLikeRows(input.InstitutionName, kind, input.CurrencyCode, "", statementAccounts)
	switch len(like) {
	case 1:
		input.FinanceAccountID = like[0].FinanceAccountID
		return "", nil
	case 0:
		return fmt.Sprintf("it shows no account number, and no account at %s was imported before, so it is not clear which account it is. "+
			"Share a screenshot that shows the account's number or its last digits", strings.TrimSpace(input.InstitutionName)), nil
	}
	return fmt.Sprintf("it shows no account number, and it could be %s", strings.Join(finance.AccountNames(like), " or ")), nil
}

// refuseRowsOfASyncedAccount refuses rows that would start an account of
// imported statements beside one a provider already syncs, which they are
// likely of: the same transactions would be counted in both.
func (self *Agent) refuseRowsOfASyncedAccount(ctx context.Context, agentId string, check *finance.TransactionRowsCheck) error {
	_, providerAccounts, err := self.rowAccounts(ctx, agentId)
	if err != nil {
		return err
	}
	if like := finance.AccountsLikeRows(check.InstitutionName, check.StatementAccountKind, check.CurrencyCode, check.AccountNumber, providerAccounts); len(like) > 0 {
		return fmt.Errorf("%w: %s", finance.ErrTransactionRowsRefused, syncedAccountWhy(like))
	}
	return nil
}

// postStatementReading posts to the person's feed what came of reading a
// statement they handed in: the import, or why nothing was imported.
func (self *Agent) postStatementReading(ctx context.Context, run *Run, attachment *models.AgentAttachment, imported *models.FinanceStatementImport, whyNothing string) error {
	card := &models.AgentFeedItem{
		FeedItemKind: models.FeedItemUpdate, FeedSourceKind: models.FeedSourceAgent,
		PictureSymbol: "list.bullet.rectangle", CoalesceKey: "statement:" + attachment.ID,
	}
	if imported == nil {
		card.Title = "Nothing was imported from " + attachment.Name
		card.Body = upperFirst(strings.TrimSuffix(strings.TrimSpace(whyNothing), ".")) + ". Or send it to your assistant in a chat, which can ask you about it."
		card.FeedUrgency = models.FeedUrgencyNow
	} else {
		card.Title = fmt.Sprintf("Imported %s into %s", countOf(imported.AddedTransactionCount, "transaction", "transactions"), strings.Join(imported.FinanceAccountNames, ", "))
		card.Body = statementReadingDetails(imported)
		card.FeedUrgency = models.FeedUrgencyToday
	}
	if _, _, err := self.PostFeedItem(ctx, &FeedPost{AgentID: run.Agent.ID, Item: card}); err != nil {
		return err
	}
	log.Noticef("agent %q read the statement %q it was handed, and said what came of it in the feed", run.Agent.ID, attachment.ID)
	return nil
}

// statementReadingDetails is what else an import of read rows did, beside
// the rows it added: the rows it held already, and what it could not
// import.
func statementReadingDetails(imported *models.FinanceStatementImport) string {
	var details []string
	if imported.UnchangedTransactionCount > 0 {
		details = append(details, countOf(imported.UnchangedTransactionCount, "transaction was", "transactions were")+" already there.")
	}
	if imported.UpdatedTransactionCount > 0 {
		details = append(details, countOf(imported.UpdatedTransactionCount, "transaction was", "transactions were")+" updated.")
	}
	if imported.SkippedTransactionCount > 0 {
		details = append(details, countOf(imported.SkippedTransactionCount, "transaction was", "transactions were")+" skipped.")
	}
	if imported.ImportErrorMessage != "" {
		details = append(details, "Not imported: "+strings.TrimSuffix(imported.ImportErrorMessage, ".")+".")
	}
	return strings.Join(details, " ")
}

// upperFirst is text with its first letter in upper case.
func upperFirst(text string) string {
	for index, letter := range text {
		return strings.ToUpper(string(letter)) + text[index+len(string(letter)):]
	}
	return text
}
