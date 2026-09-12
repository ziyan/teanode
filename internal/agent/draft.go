package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// A draft is a reply the person asked for and will read before it goes:
// written in the request, from the conversation, the notes the agent
// already has on it, and a line of instructions. It goes into the composer;
// sending is the person's.

// DraftInput is everything the draft prompt is built from.
type DraftInput struct {
	Configuration *config.Configuration
	Agent         *models.Agent
	Owner         *models.User
	Mailbox       *models.Mailbox
	Source        *models.AgentMailbox
	Subject       string
	Instructions  string

	// Message is the one being answered; Earlier are the ones before it,
	// oldest first, as many as fit.
	Message *MessageContext
	Earlier []*MessageContext

	// Summary and Notes are what the agent already worked out about the
	// conversation and the message, when it has.
	Summary  string
	Notes    string
	Memories []string
}

type draftData struct {
	PersonName   string
	Language     string
	Subject      string
	Instructions string
	Memories     []string
	Summary      string
	Notes        string
	Earlier      []string
	Message      string
}

// draftEarlierMessages is how many earlier messages a draft reads when the
// conversation has no summary to stand in for them; draftEarlierCharacters
// bounds each.
const draftEarlierMessages = 6
const draftEarlierCharacters = 1500

// DraftPrompt builds the messages for one draft call.
func DraftPrompt(input *DraftInput) ([]llm.ChatMessage, error) {
	conduct, err := RenderConduct(input.Configuration, input.Agent, input.Owner, false)
	if err != nil {
		return nil, err
	}
	earlier := make([]string, 0, len(input.Earlier))
	for _, message := range input.Earlier {
		earlier = append(earlier, strings.TrimSpace(message.Render()))
	}
	user, err := render("draft.txt", draftData{
		PersonName:   personName(input.Owner),
		Language:     languageName(Language(input.Agent, input.Owner)),
		Subject:      input.Subject,
		Instructions: strings.TrimSpace(input.Instructions),
		Memories:     input.Memories,
		Summary:      strings.TrimSpace(input.Summary),
		Notes:        strings.TrimSpace(input.Notes),
		Earlier:      earlier,
		Message:      strings.TrimSpace(input.Message.Render()),
	})
	if err != nil {
		return nil, err
	}
	return []llm.ChatMessage{
		{Role: llm.RoleSystem, Content: conduct, CacheBreakpoint: true},
		{Role: llm.RoleUser, Content: user},
	}, nil
}

// DraftReply writes a reply to a message for the person to read. It is the
// one thing the agent does in the request rather than from the queue,
// because the person is sitting in the composer waiting for it.
func (self *Agent) DraftReply(ctx context.Context, request *models.AgentDraftRequest) (*models.AgentDraft, error) {
	if self == nil {
		return nil, ErrUnavailable
	}
	mailbox, mail := request.Mailbox, request.Mail
	source := mailbox.Agent
	if source == nil || !source.Granted || !source.DraftReplies {
		return nil, ErrNotGranted
	}
	configuration := self.settings.Configuration()
	if !FeatureAllowed(configuration, "draftReplies") {
		return nil, ErrUnavailable
	}
	if self.settings.Registry == nil {
		return nil, ErrUnavailable
	}
	provider, model, err := self.settings.Registry.ForWork(config.AgentWorkReply)
	if err != nil {
		return nil, err
	}

	var mails []*models.Mail
	var memories []string
	summary, notes := "", ""
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		if err := RequireBudget(tx, configuration, request.Agent, request.Owner, time.Now()); err != nil {
			return err
		}
		threadId := mail.ThreadID
		if threadId == "" {
			threadId = mail.ID
		}
		if mails, err = threadMails(tx, mailbox.ID, threadId); err != nil {
			return err
		}
		if found, err := tx.GetThreadSummary(mailbox.ID, threadId); err != nil {
			return err
		} else if found != nil && found.ThroughMailID == mail.ID {
			// A summary that reads through the message being answered
			// stands in for the earlier messages; one that stops short
			// would leave the newest out, so the messages are read instead.
			summary = found.Summary
		}
		insights, err := tx.GetMailInsights(mailbox.ID, []string{mail.ID})
		if err != nil {
			return err
		}
		if insight := insights[mail.ID]; insight != nil {
			notes = insight.Notes
		}
		memories, err = memoryLines(tx, request.Agent.ID, models.AudienceReply, promptRunMemories, false)
		return err
	}); err != nil {
		return nil, err
	}

	if err := LoadHeaders(ctx, self.settings.Storage, mail); err != nil {
		return nil, err
	}
	message, err := BuildMessageContext(ctx, self.settings.Storage, mail, configuration.Agent.Limits.MaxBodyCharacters, false)
	if err != nil {
		return nil, err
	}
	var earlier []*MessageContext
	if summary == "" {
		before := make([]*models.Mail, 0, len(mails))
		for _, candidate := range mails {
			if candidate.ID != mail.ID && !candidate.ReceivedAt.After(mail.ReceivedAt) {
				before = append(before, candidate)
			}
		}
		if len(before) > draftEarlierMessages {
			before = before[len(before)-draftEarlierMessages:]
		}
		for _, candidate := range before {
			context, err := BuildMessageContext(ctx, self.settings.Storage, candidate, draftEarlierCharacters, false)
			if err != nil {
				return nil, err
			}
			earlier = append(earlier, context)
		}
	}
	messages, err := DraftPrompt(&DraftInput{
		Configuration: configuration,
		Agent:         request.Agent,
		Owner:         request.Owner,
		Mailbox:       mailbox,
		Source:        source,
		Subject:       threadSubject(mail.Subject),
		Instructions:  request.Instructions,
		Message:       message,
		Earlier:       earlier,
		Summary:       summary,
		Notes:         notes,
		Memories:      memories,
	})
	if err != nil {
		return nil, err
	}
	callContext, cancel := context.WithTimeout(ctx, configuration.Agent.Limits.RequestTimeout.Duration())
	defer cancel()
	response, err := provider.Chat(callContext, &llm.ChatRequest{
		Model:     model,
		Messages:  messages,
		MaxTokens: 1200,
	})
	modelName := self.settings.Registry.Configuration().Models.ForWork(config.AgentWorkReply)
	if response != nil {
		RecordUsage(self.settings.Database, request.Agent.ID, mailbox.ID, modelName, "draft", response.Usage)
	}
	if err != nil {
		return nil, fmt.Errorf("asking the model: %w", err)
	}
	text := strings.TrimSpace(response.Message.Content)
	if text == "" {
		return nil, fmt.Errorf("the model answered with nothing")
	}
	draft := &models.AgentDraft{Text: text, Model: modelName}
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		transcript, err := recordCall(tx, &callRecord{
			AgentID:   request.Agent.ID,
			MailboxID: mailbox.ID,
			Kind:      "draft",
			SubjectID: mail.ID,
			Note:      fmt.Sprintf("Drafted a reply to %q", mail.Subject),
			Prompt:    messages[1].Content,
			Response:  response,
			Model:     modelName,
		})
		if err != nil {
			return err
		}
		draft.RunID = transcript.ID
		return nil
	}); err != nil {
		return nil, err
	}
	return draft, nil
}
