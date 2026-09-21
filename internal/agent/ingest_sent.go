package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/computer"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// readSentMail files the person's own sent messages, which is what lets
// the agent write the way they do.
//
// Their own words to real people, with the quoting stripped: not a style
// guide somebody wrote about them, but two thousand examples of how they
// actually open, how long they make it and how they sign off.
func (self *Agent) readSentMail(ctx context.Context, run *Run, source *models.AgentKnowledgeSource, cursor map[string]any) (string, db.SourceCounts, error) {
	counts := db.SourceCounts{}
	mailboxId := source.Specification.MailboxID
	if mailboxId == "" {
		return "", counts, fmt.Errorf("which mailbox?")
	}
	position, err := readSentCursor(cursor, time.Now())
	if err != nil {
		return "", counts, err
	}

	var folder *models.MailboxFolder
	var items []*models.MailboxItem
	var known map[string]string
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if folder, err = tx.GetFolderByKind(mailboxId, models.MailboxFolderKindSent); err != nil || folder == nil {
			return err
		}
		if known, err = tx.ListAgentDocumentHashes(source.ID); err != nil {
			return err
		}
		items, err = tx.ListItems(folder.ID, &db.ItemOptions{
			Before: position.ReceivedAt, BeforeItemID: position.ItemID, Limit: ingestEntries, ByReceived: true,
		})
		return err
	}); err != nil {
		return "", counts, err
	}
	if folder == nil {
		return "", counts, fmt.Errorf("that mailbox has no sent folder")
	}
	if len(items) == 0 {
		return "", counts, nil
	}

	var last *sentIngestCursor
	for _, item := range items {
		if err := self.checkSourceRead(ctx, source); err != nil {
			return "", counts, err
		}
		if ctx.Err() != nil {
			return "", counts, ctx.Err()
		}
		var mail *models.Mail
		if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
			found, err := tx.GetMails([]string{item.MailID}, nil)
			if err != nil || len(found) == 0 {
				return err
			}
			mail = found[0]
			return nil
		}); err != nil {
			return "", counts, err
		}
		if mail == nil {
			continue
		}
		last = &sentIngestCursor{ReceivedAt: mail.ReceivedAt, ItemID: item.ID}
		if known[mail.ID] != "" {
			continue
		}
		// Without the quoted reply underneath: what they wrote is the
		// example, and the message they were answering is not.
		message, err := buildMessageContext(ctx, run.Storage(), mail, sentCharacters, false, false)
		if err != nil {
			return "", counts, err
		}
		if strings.TrimSpace(message.Text) == "" {
			continue
		}
		happened := mail.ReceivedAt
		entry := computer.ScanEntry{
			ExternalID: mail.ID,
			Kind:       "message",
			Title:      "To " + message.To + ": " + message.Subject,
			HappenedAt: &happened,
			Hash:       mail.ID,
			Text:       message.Text,
			// As with a commit: a message is not a file on anybody's
			// disk, and an entry with text and no size is filed as a
			// document that appears to hold nothing.
			Size: int64(len(message.Text)),
			Metadata: map[string]any{
				"to": message.To, "subject": message.Subject, "author": "them",
			},
		}
		written, err := self.fileDocument(ctx, run, source, entry, "")
		if err != nil {
			return "", counts, err
		}
		counts.Documents++
		counts.Chunks += written
	}
	// Backwards through time: the newest are the best examples, and a
	// pass that stops halfway has the useful half.
	if last != nil {
		next, err := last.encode()
		return next, counts, err
	}
	return "", counts, nil
}

// sentCharacters is how much of one sent message is kept as an example.
// Long enough to show how they structure something; short enough that two
// thousand of them are a reasonable corpus.
const sentCharacters = 4000
