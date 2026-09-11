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

// Triage: one message in, one insight out, and then the rules that were
// waiting for it.

// TriageAnswer is the JSON the model is asked for.
type TriageAnswer struct {
	Category    string   `json:"category"`
	Priority    string   `json:"priority"`
	NeedsReply  bool     `json:"needs_reply"`
	Research    bool     `json:"research"`
	Summary     string   `json:"summary"`
	ActionItems []string `json:"action_items"`
}

// TriageInput is everything the prompt is built from, so the prompt can be
// rendered and tested without a database.
type TriageInput struct {
	Configuration *config.Configuration
	Agent         *models.Agent
	Owner         *models.User
	Mailbox       *models.Mailbox
	Source        *models.AgentMailbox
	Message       *MessageContext
	Memories      []string
	Corrections   []string
	ResearchNotes []string
}

type triageCategory struct {
	Name        string
	Description string
}

// TriagePrompt renders the two messages a triage call sends.
func TriagePrompt(input *TriageInput) ([]llm.ChatMessage, error) {
	conduct, err := RenderConduct(input.Configuration, input.Agent, input.Owner, true)
	if err != nil {
		return nil, err
	}
	categories := make([]triageCategory, 0, len(models.AgentCategories)+len(input.Agent.Categories))
	for _, name := range models.AgentCategories {
		categories = append(categories, triageCategory{Name: name, Description: fixedCategoryDescriptions[name]})
	}
	for _, category := range input.Agent.Categories {
		categories = append(categories, triageCategory{Name: strings.ToLower(strings.TrimSpace(category.Name)), Description: category.Description})
	}
	directOnly := input.Source == nil || input.Source.Triage == nil || input.Source.Triage.ReplyExpectation != "any"
	user, err := render("triage.txt", map[string]any{
		"PersonName":    personName(input.Owner),
		"MailboxName":   input.Mailbox.Name,
		"Language":      languageName(Language(input.Agent, input.Owner)),
		"Categories":    categories,
		"DirectOnly":    directOnly,
		"ResearchNotes": input.ResearchNotes,
		"Memories":      input.Memories,
		"Corrections":   input.Corrections,
		"Message":       input.Message.Render(),
	})
	if err != nil {
		return nil, err
	}
	return []llm.ChatMessage{
		{Role: llm.RoleSystem, Content: conduct, CacheBreakpoint: true},
		{Role: llm.RoleUser, Content: user},
	}, nil
}

// fixedCategoryDescriptions say what the fixed vocabulary means, once, in
// the prompt rather than in the model's imagination.
var fixedCategoryDescriptions = map[string]string{
	"personal":     "a person writing to the person about their own life",
	"work":         "about the person's job or business",
	"newsletter":   "a periodical sent to many, usually with a way to unsubscribe",
	"notification": "a service telling the person something happened: a sign-in, a shipment, a comment",
	"receipt":      "an order, an invoice, a payment, a booking confirmation",
	"promotion":    "marketing: an offer, a sale, a product announcement",
	"social":       "a social network or a community talking",
	"invitation":   "an invitation to an event or a meeting",
	"other":        "none of the above",
}

// InterpretTriage turns the model's answer into an insight, refusing values
// outside the vocabulary the way a rule would refuse them.
func InterpretTriage(answer *TriageAnswer, agent *models.Agent) (*models.MailInsight, error) {
	category := strings.ToLower(strings.TrimSpace(answer.Category))
	known := false
	for _, name := range agent.CategoryNames() {
		if name == category {
			known = true
		}
	}
	if !known {
		category = "other"
	}
	priority := strings.ToLower(strings.TrimSpace(answer.Priority))
	switch priority {
	case "high", "normal", "low":
	default:
		priority = "normal"
	}
	items := make([]string, 0, len(answer.ActionItems))
	for _, item := range answer.ActionItems {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	summary := strings.TrimSpace(answer.Summary)
	if len(summary) > 300 {
		summary = summary[:300]
	}
	// A notification, a newsletter, a receipt, a promotion or a social
	// network's digest never needs a reply, whatever the model said: on a
	// real mailbox it said so for a plan-expiry notice and a community
	// site's question of the day, and the prompt's own rule is the one that
	// holds.
	needsReply := answer.NeedsReply
	switch category {
	case "newsletter", "notification", "receipt", "promotion", "social":
		needsReply = false
	}
	return &models.MailInsight{
		Category:      category,
		Priority:      priority,
		NeedsReply:    needsReply,
		ResearchAsked: answer.Research,
		Summary:       summary,
		ActionItems:   items,
	}, nil
}

// runTriage is the handler for a triage job.
func (self *Agent) runTriage(ctx context.Context, run *Run) error {
	if run.Mailbox == nil || run.Source == nil || !run.Source.Granted || run.Source.Triage == nil || !run.Source.Triage.Enabled {
		return nil // revoked or switched off since it was queued: nothing to do
	}
	configuration := run.Configuration()
	if !FeatureAllowed(configuration, "triage") {
		return nil
	}
	registry := run.Registry()
	if registry == nil {
		return fmt.Errorf("no model registry")
	}
	provider, model, err := registry.ForWork(config.AgentWorkTriage)
	if err != nil {
		return err
	}

	var mail *models.Mail
	var memories, corrections, researchNotes []string
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		if err := RequireBudget(tx, configuration, run.Agent, run.Owner, time.Now()); err != nil {
			return err
		}
		mails, err := tx.GetMails([]string{run.Job.SubjectID}, nil)
		if err != nil {
			return err
		}
		if len(mails) == 0 || mails[0] == nil {
			return nil
		}
		mail = mails[0]
		if memories, err = memoryLines(tx, run.Agent.ID, models.AudienceTriage, promptRunMemories, false); err != nil {
			return err
		}
		if corrections, err = correctionLines(tx, run.Agent.ID, []models.AgentFeedbackKind{models.FeedbackFiled}); err != nil {
			return err
		}
		if run.Source.Research && FeatureAllowed(configuration, "research") {
			if researchNotes, err = memoryLines(tx, run.Agent.ID, models.AudienceResearch, promptRunMemories, false); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if mail == nil {
		return nil // the message is gone
	}

	message, err := BuildMessageContext(ctx, run.Storage(), mail, configuration.Agent.Limits.MaxBodyCharacters, false)
	if err != nil {
		return err
	}
	if run.Source.Research && FeatureAllowed(configuration, "research") {
		// Until research memories exist the notes list is empty, and the
		// prompt says to answer false unless the message plainly asks.
		researchNotes = nil
	}
	messages, err := TriagePrompt(&TriageInput{
		Configuration: configuration,
		Agent:         run.Agent,
		Owner:         run.Owner,
		Mailbox:       run.Mailbox,
		Source:        run.Source,
		Message:       message,
		Memories:      memories,
		Corrections:   corrections,
		ResearchNotes: researchNotes,
	})
	if err != nil {
		return err
	}

	callContext, cancel := context.WithTimeout(ctx, configuration.Agent.Limits.RequestTimeout.Duration())
	defer cancel()
	response, err := provider.Chat(callContext, &llm.ChatRequest{
		Model:      model,
		Messages:   messages,
		MaxTokens:  600,
		JSONObject: true,
	})
	modelName := registry.Configuration().Models.ForWork(config.AgentWorkTriage)
	if response != nil {
		RecordUsage(run.Database(), run.Agent.ID, run.Mailbox.ID, modelName, string(models.AgentJobTriage), response.Usage)
	}
	if err != nil {
		return fmt.Errorf("asking the model: %w", err)
	}
	answer, err := llm.Extract[TriageAnswer](response.Message.Content)
	if err != nil {
		return err
	}
	insight, err := InterpretTriage(&answer, run.Agent)
	if err != nil {
		return err
	}
	insight.MailID = mail.ID
	insight.MailboxID = run.Mailbox.ID
	insight.AgentID = run.Agent.ID
	insight.Model = modelName
	insight.RunID = run.Job.ID

	return run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		if err := tx.PutMailInsight(insight); err != nil {
			return err
		}
		transcript, err := self.recordRun(tx, run, fmt.Sprintf("Sorted %q: %s, %s priority%s. %s", mail.Subject, insight.Category, insight.Priority, map[bool]string{true: ", needs a reply", false: ""}[insight.NeedsReply], insight.Summary), messages[1].Content, response, modelName)
		if err != nil {
			return err
		}
		insight.RunID = transcript.ID
		if err := tx.PutMailInsight(insight); err != nil {
			return err
		}
		// The rules that were waiting for this insight.
		items, err := tx.ListItemsByMail(mail.ID)
		if err != nil {
			return err
		}
		for _, item := range items {
			folder, err := tx.GetFolder(item.FolderID)
			if err != nil {
				return err
			}
			if folder == nil || folder.MailboxID != run.Mailbox.ID || folder.Kind != models.MailboxFolderKindInbox {
				continue
			}
			if self.settings.Exchange != nil {
				if err := self.settings.Exchange.RunInsightRules(tx, run.Mailbox, item, mail, insight); err != nil {
					log.Warningf("the insight rules of mailbox %q failed on message %q: %s", run.Mailbox.ID, mail.ID, err)
				}
			}
		}
		if insight.NeedsReply && run.Source.AutoReply != nil && run.Source.AutoReply.Enabled && FeatureAllowed(configuration, "autoReply") {
			if _, err := self.Enqueue(tx, models.AgentJobReply, run.Agent.ID, run.Mailbox.ID, mail.ID); err != nil {
				return err
			}
		}
		if insight.ResearchAsked && run.Source.Research && FeatureAllowed(configuration, "research") {
			if _, err := self.Enqueue(tx, models.AgentJobResearch, run.Agent.ID, run.Mailbox.ID, mail.ID); err != nil {
				return err
			}
		}
		return nil
	})
}

// recordRun writes a run's transcript: a note saying what it did, the
// prompt it sent, and the answer it got, with the tokens on the answer.
func (self *Agent) recordRun(tx db.Transaction, run *Run, note, prompt string, response *llm.ChatResponse, modelName string) (*models.AgentConversation, error) {
	return recordCall(tx, &callRecord{
		AgentID:   run.Agent.ID,
		MailboxID: run.Job.MailboxID,
		JobID:     run.Job.ID,
		Kind:      string(run.Job.Kind),
		SubjectID: run.Job.SubjectID,
		Note:      note,
		Prompt:    prompt,
		Response:  response,
		Model:     modelName,
	})
}

// callRecord is one model call as the transcript records it: the run it
// belonged to, what was asked, what came back.
type callRecord struct {
	AgentID   string
	MailboxID string
	JobID     string
	Kind      string
	SubjectID string
	Note      string
	Prompt    string
	Response  *llm.ChatResponse
	Model     string
}

// recordCall writes a run transcript: a note saying what was done, the
// prompt, and the answer with what it cost.
func recordCall(tx db.Transaction, record *callRecord) (*models.AgentConversation, error) {
	conversation, err := tx.CreateAgentConversation(&models.AgentConversation{
		AgentID:   record.AgentID,
		MailboxID: record.MailboxID,
		Kind:      models.AgentConversationRun,
		Title:     record.Note,
		JobID:     record.JobID,
		JobKind:   record.Kind,
		SubjectID: record.SubjectID,
	})
	if err != nil {
		return nil, err
	}
	if _, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: conversation.ID, Role: models.AgentMessageNote, Content: record.Note}); err != nil {
		return nil, err
	}
	if _, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: conversation.ID, Role: string(llm.RoleUser), Content: record.Prompt}); err != nil {
		return nil, err
	}
	if record.Response != nil {
		if _, err := tx.AppendAgentMessage(&models.AgentMessage{
			ConversationID: conversation.ID,
			Role:           string(llm.RoleAssistant),
			Content:        record.Response.Message.Content,
			Usage: &models.AgentUsageNote{
				Model:            record.Model,
				Kind:             record.Kind,
				PromptTokens:     record.Response.Usage.PromptTokens,
				CompletionTokens: record.Response.Usage.CompletionTokens,
				CacheReadTokens:  record.Response.Usage.CacheReadTokens,
				CacheWriteTokens: record.Response.Usage.CacheWriteTokens,
			},
		}); err != nil {
			return nil, err
		}
	}
	return conversation, nil
}

// backfillBatch is how many messages one backfill job queues; the job
// requeues itself while there are more, so a large mailbox is sorted over
// ticks rather than in one burst.
const backfillBatch = 200

// runBackfill queues triage for the messages already in the Inbox when a
// mailbox is granted, per the source's choice.
func (self *Agent) runBackfill(ctx context.Context, run *Run) error {
	if run.Mailbox == nil || run.Source == nil || !run.Source.Granted {
		return nil
	}
	if err := self.backfillEmbeddings(ctx, run); err != nil {
		return err
	}
	if run.Source.Triage == nil || !run.Source.Triage.Enabled {
		return nil
	}
	mode := run.Source.Triage.Backfill
	if mode == "none" {
		return nil
	}
	limit := backfillBatch
	return run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		ids, err := tx.ListMailWithoutInsight(run.Mailbox.ID, limit)
		if err != nil {
			return err
		}
		for _, id := range ids {
			if _, err := self.Enqueue(tx, models.AgentJobTriage, run.Agent.ID, run.Mailbox.ID, id); err != nil {
				return err
			}
		}
		if mode == "all" && len(ids) == limit {
			// More to do: come back after this batch has been sorted.
			later := time.Now().Add(10 * time.Minute)
			_, err := tx.EnqueueAgentJob(&models.AgentJob{AgentID: run.Agent.ID, MailboxID: run.Mailbox.ID, Kind: models.AgentJobBackfill, SubjectID: "more", NotBefore: &later})
			return err
		}
		return nil
	})
}
