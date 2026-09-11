package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// Research is a run nobody is present for that looks something up about a
// message triage flagged: it reads — the web, the mailbox, a connected
// server that is headless and read-only — and never writes, and its
// notes land on the insight for the reader to show and the reply to draw
// on.

// researchTools is the fixed set a research run may use, beside the
// read-only headless tools of connected servers.
var researchTools = map[string]bool{"web_fetch": true, "web_search": true, "datetime": true, "mail_search": true, "mail_read": true, "contact_search": true, "tool_search": true, "memory": true}

// runResearch is the handler for a research job; its subject is the
// message.
func (self *Agent) runResearch(ctx context.Context, run *Run) error {
	if run.Mailbox == nil || run.Source == nil || !run.Source.Granted || !run.Source.Research {
		return nil
	}
	configuration := run.Configuration()
	if !FeatureAllowed(configuration, "research") || !FeatureAllowed(configuration, "ask") {
		return nil
	}
	if self.operations == nil {
		return fmt.Errorf("no way to act as the person")
	}
	var mail *models.Mail
	var insight *models.MailInsight
	var memories []string
	var conversation *models.AgentConversation
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		if err := RequireBudget(tx, configuration, run.Agent, run.Owner, time.Now()); err != nil {
			return err
		}
		found, err := tx.GetMails([]string{run.Job.SubjectID}, nil)
		if err != nil {
			return err
		}
		if len(found) == 0 || found[0] == nil {
			return nil
		}
		mail = found[0]
		insights, err := tx.GetMailInsights(run.Mailbox.ID, []string{mail.ID})
		if err != nil {
			return err
		}
		insight = insights[mail.ID]
		if memories, err = memoryLines(tx, run.Agent.ID, models.AudienceResearch, promptRunMemories, false); err != nil {
			return err
		}
		conversation, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: run.Agent.ID, MailboxID: run.Mailbox.ID, Kind: models.AgentConversationRun, Title: fmt.Sprintf("Researched %q", mail.Subject), JobID: run.Job.ID, JobKind: string(models.AgentJobResearch), SubjectID: mail.ID, Surface: "research", LastAt: time.Now()})
		return err
	}); err != nil {
		return err
	}
	if mail == nil {
		return nil
	}
	item, err := self.itemOf(ctx, run, mail)
	if err != nil {
		return err
	}
	message, err := BuildMessageContext(ctx, run.Storage(), mail, configuration.Agent.Limits.MaxBodyCharacters, false)
	if err != nil {
		return err
	}
	data := map[string]any{
		"PersonName":  personName(run.Owner),
		"MailboxName": run.Mailbox.Name,
		"Language":    languageName(Language(run.Agent, run.Owner)),
		"Memories":    memories,
		"ItemID":      item,
		"Message":     strings.TrimSpace(message.Render()),
	}
	if insight != nil {
		data["Summary"] = insight.Summary
		data["ActionItems"] = insight.ActionItems
	}
	prompt, err := render("research.txt", data)
	if err != nil {
		return err
	}
	operations, err := self.operations(ctx, run.Owner)
	if err != nil {
		return err
	}
	maximumRounds := configuration.Agent.Limits.MaxRoundsPerResearch
	if maximumRounds <= 0 {
		maximumRounds = 8
	}
	turn, err := self.Ask(&AskSettings{Agent: run.Agent, Owner: run.Owner, Operations: operations, Conversation: conversation, Message: prompt, Surface: "research", ReadOnly: true, Allow: researchTools, Headless: true, MaxRounds: maximumRounds, UsageKind: string(models.AgentJobResearch)})
	if err != nil {
		return err
	}
	events, unsubscribe := turn.Subscribe()
	defer unsubscribe()
	notes, failure := "", ""
	for event := range events {
		switch event.Kind {
		case EventMessage:
			notes = event.Text
		case EventError:
			failure = event.Error
		}
	}
	if failure != "" {
		return fmt.Errorf("the research turn failed: %s", failure)
	}
	notes = strings.TrimSpace(notes)
	if notes == "" {
		return nil
	}
	return run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		return tx.SetMailInsightNotes(mail.ID, run.Mailbox.ID, notes, conversation.ID)
	})
}

// itemOf is the message's item id in the mailbox, for the prompt's
// citation; empty when it has none.
func (self *Agent) itemOf(ctx context.Context, run *Run, mail *models.Mail) (string, error) {
	itemId := ""
	err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		items, err := tx.ListItemsByMail(mail.ID)
		if err != nil {
			return err
		}
		for _, item := range items {
			folder, err := tx.GetFolder(item.FolderID)
			if err != nil {
				return err
			}
			if folder != nil && folder.MailboxID == run.Mailbox.ID {
				itemId = item.ID
				return nil
			}
		}
		return nil
	})
	return itemId, err
}

var _ = config.AgentWorkResearch
