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

// A conversation is named and summarized by the fast model, in a line each,
// once it has gone quiet: a title after the first exchange so the picker
// has something to show, and then again whenever something was said since
// the last description and a few minutes have passed without more. Not
// after every turn — a person mid-thought would pay for a description of
// every half-finished step — and not on a fixed count of turns, which
// leaves a conversation abandoned after three turns described as it was
// after none.

// The bounds of a description.
const (
	// describeQuiet is how long a conversation rests before it is described.
	describeQuiet = 3 * time.Minute

	// describeEvery is how often the worker looks for quiet conversations.
	describeEvery = time.Minute

	// describeBatch bounds one look.
	describeBatch = 20

	describeMessages   = 12
	describeCharacters = 6000
)

// describeAnswer is what the model says a conversation is.
type describeAnswer struct {
	Title   string `json:"title"`
	Summary string `json:"summary"`
}

// describeDue describes the conversations that have gone quiet, at most
// once a minute.
func (self *Agent) describeDue(ctx context.Context, now time.Time) {
	if now.Sub(self.lastDescribe) < describeEvery || self.settings.Registry == nil {
		return
	}
	self.lastDescribe = now
	var due []*models.AgentConversation
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		due, err = tx.ListAgentConversationsToDescribe(now.Add(-describeQuiet), describeBatch)
		return err
	}); err != nil {
		log.Warningf("cannot list the conversations to describe: %s", err)
		return
	}
	for _, conversation := range due {
		if ctx.Err() != nil {
			return
		}
		if _, err := self.describeConversation(ctx, conversation); err != nil {
			log.Warningf("cannot describe conversation %q: %s", conversation.ID, err)
		}
	}
}

// describeConversation writes a conversation's title and summary from the
// tail of what was said, and returns the title when it changed. The
// conversation is marked described whatever happened, so that one the
// model cannot make sense of is not asked about every minute; the next
// message makes it due again.
func (self *Agent) describeConversation(ctx context.Context, conversation *models.AgentConversation) (string, error) {
	if conversation.Kind == models.AgentConversationRun {
		return "", nil
	}
	now := time.Now()
	mark := func(title, summary string) (string, error) {
		titled := ""
		err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
			_, err := tx.UpdateAgentConversation(conversation.ID, func(conversation *models.AgentConversation) error {
				conversation.DescribedAt = &now
				if summary != "" {
					conversation.Summary = summary
				}
				if title != "" && conversation.Kind == models.AgentConversationNamed && conversation.TitledBy != "person" && conversation.Title != title {
					conversation.Title = title
					titled = title
				}
				return nil
			})
			return err
		})
		return titled, err
	}

	var messages []*models.AgentMessage
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		messages, err = tx.ListAgentMessages(conversation.ID, nil)
		return err
	}); err != nil {
		return "", err
	}
	history := make([]llm.ChatMessage, 0, len(messages))
	for _, message := range messages {
		switch message.Role {
		case string(llm.RoleUser):
			history = append(history, llm.ChatMessage{Role: llm.RoleUser, Content: message.Content})
		case string(llm.RoleAssistant):
			if strings.TrimSpace(message.Content) != "" {
				history = append(history, llm.ChatMessage{Role: llm.RoleAssistant, Content: message.Content})
			}
		}
	}
	if len(history) == 0 {
		return mark("", "")
	}
	if len(history) > describeMessages {
		history = history[len(history)-describeMessages:]
	}
	transcript := renderHistory(history)
	if len(transcript) > describeCharacters {
		transcript = transcript[len(transcript)-describeCharacters:]
	}

	registry := self.settings.Registry
	provider, model, err := registry.ForWork(config.AgentWorkCompact)
	if err != nil {
		if provider, model, err = registry.ForWork(config.AgentWorkAsk); err != nil {
			return "", err
		}
	}
	callContext, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	response, err := provider.Chat(callContext, &llm.ChatRequest{Model: model, MaxTokens: 120, JSONObject: true, Messages: []llm.ChatMessage{
		{Role: llm.RoleUser, Content: "This is the recent part of a conversation between a person and their assistant. Answer with JSON only: {\"title\": a name for the conversation of at most five words, no quotes and no full stop; \"summary\": one sentence on what it is about now}. Write both in the language the person writes in — English when they write English.\n\n" + transcript},
	}})
	if response != nil {
		RecordUsage(self.settings.Database, conversation.AgentID, "", registry.Configuration().Models.ForWork(config.AgentWorkCompact), "compact", response.Usage)
	}
	if err != nil {
		// The provider was not there: nothing is marked, and the next
		// look tries again.
		return "", fmt.Errorf("asking the model: %w", err)
	}
	answer, err := llm.Extract[describeAnswer](response.Message.Content)
	if err != nil {
		_, markErr := mark("", "")
		if markErr != nil {
			return "", markErr
		}
		return "", fmt.Errorf("reading the answer: %w", err)
	}
	title := strings.Trim(strings.TrimSpace(strings.Split(answer.Title, "\n")[0]), `"'. `)
	if len(title) > 80 {
		title = title[:80]
	}
	summary := strings.TrimSpace(answer.Summary)
	if len(summary) > 300 {
		summary = summary[:300]
	}
	return mark(title, summary)
}
