package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/db"
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

// postStatementReading posts to the person's feed what came of reading a
// statement they handed in: the import, or why nothing was imported.
func (self *Agent) postStatementReading(ctx context.Context, run *Run, attachment *models.AgentAttachment, imported *models.FinanceStatementImport, whyNothing string) error {
	card := &models.AgentFeedItem{
		FeedItemKind: models.FeedItemUpdate, FeedSourceKind: models.FeedSourceAgent,
		PictureSymbol: "list.bullet.rectangle", CoalesceKey: "statement:" + attachment.ID,
	}
	if imported == nil {
		card.Title = "Nothing was imported from " + attachment.Name
		card.Body = upperFirst(strings.TrimSuffix(strings.TrimSpace(whyNothing), ".")) + ". Send it to your assistant in a chat to import it with its help."
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
