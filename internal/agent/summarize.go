package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// A summary is written for a conversation, not a message: what it is about,
// where it stands, what the person has to do. It is rewritten from the
// previous summary and the messages since, so a conversation of a hundred
// messages costs the model a hundred messages once, not every time.

// SummarizeInput is everything the summarize prompt is built from.
type SummarizeInput struct {
	Configuration *config.Configuration
	Agent         *models.Agent
	Owner         *models.User
	Mailbox       *models.Mailbox
	Source        *models.AgentMailbox
	Subject       string

	// Previous is the summary so far, empty the first time; Messages are
	// the ones it does not cover, oldest first.
	Previous string
	Messages []*MessageContext
	Memories []string
}

type summarizeData struct {
	PersonName  string
	MailboxName string
	Language    string
	Subject     string
	Detailed    bool
	Previous    string
	Messages    []string
	Memories    []string
}

// summaryMessageCharacters bounds each message in the prompt, so that a
// conversation of long messages still fits; a summary reads the gist.
const summaryMessageCharacters = 3000

// summaryFirstMessages is how many of a conversation's newest messages the
// first summary reads when there is no previous one to build on.
const summaryFirstMessages = 30

// summaryThreadLimit bounds how much of a conversation is read from the
// database at all.
const summaryThreadLimit = 200

// defaultMinimumMessages is how long a conversation is before it is
// summarized without being opened.
const defaultMinimumMessages = 3

// SummarizePrompt builds the messages for one summarize call.
func SummarizePrompt(input *SummarizeInput) ([]llm.ChatMessage, error) {
	system, err := RenderConduct(input.Configuration, input.Agent, input.Owner, true)
	if err != nil {
		return nil, err
	}
	rendered := make([]string, 0, len(input.Messages))
	for _, message := range input.Messages {
		rendered = append(rendered, strings.TrimSpace(message.Render()))
	}
	user, err := render("summarize.txt", summarizeData{
		PersonName:  personName(input.Owner),
		MailboxName: input.Mailbox.Name,
		Language:    languageName(Language(input.Agent, input.Owner)),
		Subject:     input.Subject,
		Detailed:    input.Source != nil && input.Source.Summaries != nil && input.Source.Summaries.Style == "detailed",
		Previous:    strings.TrimSpace(input.Previous),
		Messages:    rendered,
		Memories:    input.Memories,
	})
	if err != nil {
		return nil, err
	}
	return []llm.ChatMessage{
		{Role: llm.RoleSystem, Content: system, CacheBreakpoint: true},
		{Role: llm.RoleUser, Content: user},
	}, nil
}

// MinimumMessages is how long a conversation in this source is before it is
// summarized unasked.
func MinimumMessages(source *models.AgentMailbox) int {
	if source == nil || source.Summaries == nil || source.Summaries.MinimumMessages <= 0 {
		return defaultMinimumMessages
	}
	return source.Summaries.MinimumMessages
}

// threadMails is a conversation's messages in a mailbox, oldest first, each
// once however many folders it is filed in.
func threadMails(tx db.Transaction, mailboxId, threadId string) ([]*models.Mail, error) {
	items, err := tx.ListItems("", &db.ItemOptions{MailboxID: mailboxId, ThreadID: threadId, Limit: summaryThreadLimit})
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		if !seen[item.MailID] {
			seen[item.MailID] = true
			ids = append(ids, item.MailID)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	found, err := tx.GetMails(ids, nil)
	if err != nil {
		return nil, err
	}
	mails := make([]*models.Mail, 0, len(found))
	for _, mail := range found {
		if mail != nil {
			mails = append(mails, mail)
		}
	}
	sort.SliceStable(mails, func(left, right int) bool { return mails[left].ReceivedAt.Before(mails[right].ReceivedAt) })
	return mails, nil
}

// runSummarize is the handler for a summarize job; its subject is the
// conversation.
func (self *Agent) runSummarize(ctx context.Context, run *Run) error {
	if run.Mailbox == nil || run.Source == nil || !run.Source.Granted || run.Source.Summaries == nil || !run.Source.Summaries.Enabled {
		return nil
	}
	configuration := run.Configuration()
	if !FeatureAllowed(configuration, "summaries") {
		return nil
	}
	registry := run.Registry()
	if registry == nil {
		return fmt.Errorf("no model registry")
	}
	provider, model, err := registry.ForWork(config.AgentWorkSummarize)
	if err != nil {
		return err
	}
	threadId := run.Job.SubjectID

	var mails []*models.Mail
	var previous *models.ThreadSummary
	var memories []string
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		if err := RequireBudget(tx, configuration, run.Agent, run.Owner, time.Now()); err != nil {
			return err
		}
		if mails, err = threadMails(tx, run.Mailbox.ID, threadId); err != nil {
			return err
		}
		if memories, err = memoryLines(tx, run.Agent.ID, models.AudienceSummaries, promptRunMemories, false); err != nil {
			return err
		}
		previous, err = tx.GetThreadSummary(run.Mailbox.ID, threadId)
		return err
	}); err != nil {
		return err
	}
	if len(mails) == 0 {
		return nil // the conversation is gone
	}
	newest := mails[len(mails)-1]
	if previous != nil && previous.ThroughMailID == newest.ID {
		return nil // still fresh: somebody opened it twice
	}

	// What the previous summary does not cover; all of it, capped, when
	// there is no previous summary or its last message has since gone.
	since := mails
	previousText := ""
	if previous != nil {
		for index, mail := range mails {
			if mail.ID == previous.ThroughMailID {
				since = mails[index+1:]
				previousText = previous.Summary
				break
			}
		}
	}
	if previousText == "" && len(since) > summaryFirstMessages {
		since = since[len(since)-summaryFirstMessages:]
	}
	contexts := make([]*MessageContext, 0, len(since))
	for _, mail := range since {
		message, err := BuildMessageContext(ctx, run.Storage(), mail, summaryMessageCharacters, false)
		if err != nil {
			return err
		}
		contexts = append(contexts, message)
	}
	subject := threadSubject(mails[0].Subject)
	messages, err := SummarizePrompt(&SummarizeInput{
		Configuration: configuration,
		Agent:         run.Agent,
		Owner:         run.Owner,
		Mailbox:       run.Mailbox,
		Source:        run.Source,
		Subject:       subject,
		Previous:      previousText,
		Messages:      contexts,
		Memories:      memories,
	})
	if err != nil {
		return err
	}
	maximumTokens := 400
	if run.Source.Summaries.Style == "detailed" {
		maximumTokens = 900
	}
	callContext, cancel := context.WithTimeout(ctx, configuration.Agent.Limits.RequestTimeout.Duration())
	defer cancel()
	response, err := provider.Chat(callContext, &llm.ChatRequest{
		Model:     model,
		Messages:  messages,
		MaxTokens: maximumTokens,
	})
	modelName := registry.Configuration().Models.ForWork(config.AgentWorkSummarize)
	if response != nil {
		RecordUsage(run.Database(), run.Agent.ID, run.Mailbox.ID, modelName, string(models.AgentJobSummarize), response.Usage)
	}
	if err != nil {
		return fmt.Errorf("asking the model: %w", err)
	}
	text := strings.TrimSpace(response.Message.Content)
	if text == "" {
		return fmt.Errorf("the model answered with nothing")
	}

	return run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		summary := &models.ThreadSummary{
			ThreadID:      threadId,
			MailboxID:     run.Mailbox.ID,
			AgentID:       run.Agent.ID,
			Summary:       text,
			ThroughMailID: newest.ID,
			MessageCount:  len(mails),
			Model:         modelName,
		}
		transcript, err := self.recordRun(tx, run, fmt.Sprintf("Summarized %q, %d messages", subject, len(mails)), messages[1].Content, response, modelName)
		if err != nil {
			return err
		}
		summary.RunID = transcript.ID
		return tx.PutThreadSummary(summary)
	})
}
