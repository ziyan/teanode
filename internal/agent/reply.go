package agent

import (
	"context"
	"fmt"
	netmail "net/mail"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/mailer"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/mx"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

// Answering on the person's behalf is the one thing here that leaves the
// server without a person pressing send, so most of this is the ladder of
// reasons not to. A reply that passes every rung is written, held in Drafts
// for the hold the person chose, and sent by the send job — which climbs
// the ladder again before it does.

// ReplyAnswer is what the model answers: a reply, or a reason not to.
type ReplyAnswer struct {
	Reply  *string `json:"reply"`
	Reason *string `json:"reason"`
}

type replyData struct {
	PersonName string
	Language   string
	Guidance   string
	Memories   []string
	Summary    string
	Notes      string
	Earlier    []string
	Message    string
}

// ReplyPrompt builds the messages for one auto-reply call. The draft
// input carries everything but the guidance, which is the policy's.
func ReplyPrompt(input *DraftInput, guidance string) ([]llm.ChatMessage, error) {
	conduct, err := RenderConduct(input.Configuration, input.Agent, input.Owner, false)
	if err != nil {
		return nil, err
	}
	earlier := make([]string, 0, len(input.Earlier))
	for _, message := range input.Earlier {
		earlier = append(earlier, strings.TrimSpace(message.Render()))
	}
	user, err := render("reply.txt", replyData{
		PersonName: personName(input.Owner),
		Language:   languageName(Language(input.Agent, input.Owner)),
		Guidance:   strings.TrimSpace(guidance),
		Memories:   input.Memories,
		Summary:    strings.TrimSpace(input.Summary),
		Notes:      strings.TrimSpace(input.Notes),
		Earlier:    earlier,
		Message:    strings.TrimSpace(input.Message.Render()),
	})
	if err != nil {
		return nil, err
	}
	return []llm.ChatMessage{
		{Role: llm.RoleSystem, Content: conduct, CacheBreakpoint: true},
		{Role: llm.RoleUser, Content: user},
	}, nil
}

// replyRefusal is why the agent must not answer this message, or empty
// when it may: the out-of-office ladder first, then the source's policy.
// A refusal is a reason, never an error.
func (self *Agent) replyRefusal(tx db.Transaction, run *Run, policy *models.AgentAutoReply, mail *models.Mail, item *models.MailboxItem, recipient string, insight *models.MailInsight, now time.Time, atSend bool) (string, error) {
	mailbox := run.Mailbox
	if self.settings.Exchange == nil {
		return "", fmt.Errorf("no mail exchange to ask")
	}
	quiet := time.Duration(policy.EffectiveQuietDays()) * 24 * time.Hour
	reason, err := self.settings.Exchange.AutoReplyRefusal(tx, mailbox, recipient, item, mail, now, quiet)
	if err != nil || reason != "" {
		return reason, err
	}
	if insight == nil || !insight.NeedsReply {
		return "the message does not need a reply", nil
	}
	sender := strings.ToLower(strings.TrimSpace(mail.Sender))
	for _, never := range policy.Never {
		if addressMatches(sender, never) {
			return "the sender is on the never list", nil
		}
	}
	switch policy.Scope {
	case "", "known":
		contact, err := tx.GetContact(mailbox.ID, sender)
		if err != nil {
			return "", err
		}
		// Seen before this message: the one being answered counts once.
		if contact == nil || contact.Count <= 1 {
			return "the sender is not a contact", nil
		}
	case "list":
		allowed := false
		for _, allow := range policy.Allow {
			if addressMatches(sender, allow) {
				allowed = true
			}
		}
		if !allowed {
			return "the sender is not on the list", nil
		}
	}
	if len(policy.Categories) > 0 {
		wanted := false
		for _, category := range policy.Categories {
			if strings.EqualFold(strings.TrimSpace(category), insight.Category) {
				wanted = true
			}
		}
		if !wanted {
			return fmt.Sprintf("the message is %s, which the policy does not answer", insight.Category), nil
		}
	}
	local := now.In(Location(run.Owner))
	switch policy.When {
	case "outsideHours":
		if policy.Hours == nil || withinHours(policy.Hours, local) {
			return "it is within the hours the person answers themselves", nil
		}
	case "whenAway":
		if mailbox.AutoReply == nil || !mailbox.AutoReply.Enabled || (mailbox.AutoReply.From != nil && now.Before(*mailbox.AutoReply.From)) || (mailbox.AutoReply.Until != nil && now.After(*mailbox.AutoReply.Until)) {
			return "the person is not away", nil
		}
	}
	// The person answered already, or the agent is about to.
	threadId := mail.ThreadID
	if threadId == "" {
		threadId = mail.ID
	}
	mails, err := threadMails(tx, mailbox.ID, threadId)
	if err != nil {
		return "", err
	}
	mine := map[string]bool{}
	for _, address := range mailbox.Addresses {
		mine[strings.ToLower(address.Address)] = true
	}
	for _, candidate := range mails {
		if candidate.ID == mail.ID || candidate.Kind == models.MailKindDraft || !candidate.ReceivedAt.After(mail.ReceivedAt) {
			continue
		}
		if mine[strings.ToLower(strings.TrimSpace(candidate.From))] || mine[strings.ToLower(strings.TrimSpace(candidate.Sender))] {
			return "the person already replied", nil
		}
	}
	if atSend {
		// The held reply is this one, and the day's limit was counted when
		// it was held.
		return "", nil
	}
	held, err := tx.CountAgentReplies(&db.AgentReplyFilter{MailboxID: mailbox.ID, ThreadID: threadId, Statuses: []models.AgentReplyStatus{models.AgentReplyHeld}})
	if err != nil {
		return "", err
	}
	if held > 0 {
		return "a reply to this conversation is already held", nil
	}
	// The day's limit, counted in the person's own day.
	dayStart := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location())
	today, err := tx.CountAgentReplies(&db.AgentReplyFilter{MailboxID: mailbox.ID, Statuses: []models.AgentReplyStatus{models.AgentReplyHeld, models.AgentReplySent}, Since: dayStart})
	if err != nil {
		return "", err
	}
	if int(today) >= policy.EffectiveDailyLimit() {
		return "the day's limit of replies is reached", nil
	}
	return "", nil
}

// addressMatches says whether an address is the entry, or in the entry's
// domain when the entry is a domain or an @domain.
func addressMatches(address, entry string) bool {
	entry = strings.ToLower(strings.TrimSpace(entry))
	address = strings.ToLower(strings.TrimSpace(address))
	if entry == "" || address == "" {
		return false
	}
	if strings.Contains(entry, "@") && !strings.HasPrefix(entry, "@") {
		return address == entry
	}
	domain := strings.TrimPrefix(entry, "@")
	_, addressDomain := mailparse.SplitAddress(address)
	return addressDomain == domain || strings.HasSuffix(addressDomain, "."+domain)
}

// withinHours says whether a local time falls in the hours.
func withinHours(hours *models.AgentHours, local time.Time) bool {
	if len(hours.Days) > 0 {
		today := false
		for _, day := range hours.Days {
			if day == int(local.Weekday()) {
				today = true
			}
		}
		if !today {
			return false
		}
	}
	from, errFrom := time.Parse("15:04", strings.TrimSpace(hours.From))
	until, errUntil := time.Parse("15:04", strings.TrimSpace(hours.Until))
	if errFrom != nil || errUntil != nil {
		return true // unreadable hours: assume the person is around
	}
	minutes := local.Hour()*60 + local.Minute()
	fromMinutes := from.Hour()*60 + from.Minute()
	untilMinutes := until.Hour()*60 + until.Minute()
	if fromMinutes <= untilMinutes {
		return minutes >= fromMinutes && minutes < untilMinutes
	}
	return minutes >= fromMinutes || minutes < untilMinutes // over midnight
}

// recipientOf is the mailbox address the message was written to, for the
// reply to go from: one in To or Cc, else the mailbox's first.
func recipientOf(mailbox *models.Mailbox, mail *models.Mail) string {
	mine := map[string]bool{}
	for _, address := range mailbox.Addresses {
		mine[strings.ToLower(address.Address)] = true
	}
	for _, header := range []string{"To", "Cc"} {
		list, err := netmail.ParseAddressList(mailparse.DecodeHeaderValue(mailparse.FindHeaderValue(mail.Headers, header)))
		if err != nil {
			continue
		}
		for _, address := range list {
			if mine[strings.ToLower(address.Address)] {
				return strings.ToLower(address.Address)
			}
		}
	}
	if len(mailbox.Addresses) > 0 {
		return strings.ToLower(mailbox.Addresses[0].Address)
	}
	return ""
}

// inboxItemOf is the message's item in the mailbox's Inbox, or nil.
func inboxItemOf(tx db.Transaction, mailbox *models.Mailbox, mail *models.Mail) (*models.MailboxItem, error) {
	inbox, err := tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindInbox)
	if err != nil || inbox == nil {
		return nil, err
	}
	items, err := tx.ListItemsByMail(mail.ID)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if item.FolderID == inbox.ID {
			return item, nil
		}
	}
	return nil, nil
}

// threadingHeaders are what make a reply part of the conversation.
func threadingHeaders(original *models.Mail) []string {
	if original.MessageID == "" {
		return nil
	}
	references := original.MessageID
	if existing := strings.TrimSpace(mailparse.FindHeaderValue(original.Headers, "References")); existing != "" {
		references = existing + " " + original.MessageID
	}
	return []string{
		mailparse.UnsplitHeader("In-Reply-To", original.MessageID),
		mailparse.UnsplitHeader("References", references),
	}
}

// runReply is the handler for a reply job; its subject is the message.
func (self *Agent) runReply(ctx context.Context, run *Run) error {
	if run.Mailbox == nil || run.Source == nil || !run.Source.Granted || run.Source.AutoReply == nil || !run.Source.AutoReply.Enabled {
		return nil
	}
	configuration := run.Configuration()
	if !FeatureAllowed(configuration, "autoReply") {
		return nil
	}
	registry := run.Registry()
	if registry == nil || self.settings.Mailer == nil {
		return fmt.Errorf("no model registry or mailer")
	}
	provider, model, err := registry.ForWork(config.AgentWorkReply)
	if err != nil {
		return err
	}
	policy := run.Source.AutoReply
	mailbox := run.Mailbox
	now := run.Now
	if now.IsZero() {
		now = time.Now()
	}

	var mail *models.Mail
	var item *models.MailboxItem
	var insight *models.MailInsight
	var mails []*models.Mail
	var memories []string
	summary, notes, recipient := "", "", ""
	refused := ""
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		found, err := tx.GetMails([]string{run.Job.SubjectID}, nil)
		if err != nil {
			return err
		}
		if len(found) == 0 || found[0] == nil {
			return nil
		}
		mail = found[0]
		if err := LoadHeaders(ctx, run.Storage(), mail); err != nil {
			return err
		}
		if item, err = inboxItemOf(tx, mailbox, mail); err != nil {
			return err
		}
		if item == nil {
			refused = "the message is not in the Inbox"
			return nil
		}
		insights, err := tx.GetMailInsights(mailbox.ID, []string{mail.ID})
		if err != nil {
			return err
		}
		insight = insights[mail.ID]
		recipient = recipientOf(mailbox, mail)
		if refused, err = self.replyRefusal(tx, run, policy, mail, item, recipient, insight, now, false); err != nil || refused != "" {
			return err
		}
		if err := RequireBudget(tx, configuration, run.Agent, run.Owner, now); err != nil {
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
			summary = found.Summary
		}
		if insight != nil {
			notes = insight.Notes
		}
		memories, err = memoryLines(tx, run.Agent.ID, models.AudienceReply, promptRunMemories, false)
		return err
	}); err != nil {
		return err
	}
	if mail == nil {
		return nil // the message is gone
	}
	if refused != "" {
		return self.recordRefusal(ctx, run, mail, refused)
	}

	message, err := BuildMessageContext(ctx, run.Storage(), mail, configuration.Agent.Limits.MaxBodyCharacters, false)
	if err != nil {
		return err
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
			context, err := BuildMessageContext(ctx, run.Storage(), candidate, draftEarlierCharacters, false)
			if err != nil {
				return err
			}
			earlier = append(earlier, context)
		}
	}
	messages, err := ReplyPrompt(&DraftInput{
		Configuration: configuration,
		Agent:         run.Agent,
		Owner:         run.Owner,
		Mailbox:       mailbox,
		Source:        run.Source,
		Subject:       threadSubject(mail.Subject),
		Message:       message,
		Earlier:       earlier,
		Summary:       summary,
		Notes:         notes,
		Memories:      memories,
	}, policy.Guidance)
	if err != nil {
		return err
	}
	callContext, cancel := context.WithTimeout(ctx, configuration.Agent.Limits.RequestTimeout.Duration())
	defer cancel()
	response, err := provider.Chat(callContext, &llm.ChatRequest{
		Model:      model,
		Messages:   messages,
		MaxTokens:  1200,
		JSONObject: true,
	})
	modelName := registry.Configuration().Models.ForWork(config.AgentWorkReply)
	if response != nil {
		RecordUsage(run.Database(), run.Agent.ID, mailbox.ID, modelName, string(models.AgentJobReply), response.Usage)
	}
	if err != nil {
		return fmt.Errorf("asking the model: %w", err)
	}
	answer, err := llm.Extract[ReplyAnswer](response.Message.Content)
	if err != nil {
		return err
	}
	text := ""
	if answer.Reply != nil {
		text = strings.TrimSpace(*answer.Reply)
	}
	if text == "" {
		reason := "the agent declined"
		if answer.Reason != nil && strings.TrimSpace(*answer.Reason) != "" {
			reason = "the agent declined: " + strings.TrimSpace(*answer.Reason)
		}
		return run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
			transcript, err := self.recordRun(tx, run, fmt.Sprintf("Declined to answer %q: %s", mail.Subject, reason), messages[1].Content, response, modelName)
			if err != nil {
				return err
			}
			_, err = tx.CreateAgentReply(&models.AgentReply{
				AgentID: run.Agent.ID, MailboxID: mailbox.ID, MailID: mail.ID, ThreadID: threadIdOf(mail),
				RunID: transcript.ID, Status: models.AgentReplyRefused, Reason: reason,
				Subject: replySubject(mail.Subject), From: recipient, To: strings.TrimSpace(mail.Sender),
			})
			return err
		})
	}

	// Held: a draft in the conversation, and a send job for when the hold
	// ends.
	sendAfter := now.Add(time.Duration(policy.EffectiveHoldMinutes()) * time.Minute)
	composed, err := self.settings.Mailer.Compose(ctx, &mailer.Message{
		From:     recipient,
		FromName: mailbox.Name,
		To:       []string{strings.TrimSpace(mail.Sender)},
		Subject:  replySubject(mail.Subject),
		Text:     text,
		Headers:  append(threadingHeaders(mail), mailparse.UnsplitHeader(mx.DraftHeaderReplyTo, item.ID)),
	})
	if err != nil {
		return err
	}
	var reply *models.AgentReply
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		transcript, err := self.recordRun(tx, run, fmt.Sprintf("Answered %q; the reply is held for %d minutes", mail.Subject, policy.EffectiveHoldMinutes()), messages[1].Content, response, modelName)
		if err != nil {
			return err
		}
		draftItem, err := self.storeDraft(ctx, tx, mailbox, recipient, mail, composed)
		if err != nil {
			return err
		}
		reply, err = tx.CreateAgentReply(&models.AgentReply{
			AgentID: run.Agent.ID, MailboxID: mailbox.ID, MailID: mail.ID, ThreadID: threadIdOf(mail),
			DraftItemID: draftItem.ID, RunID: transcript.ID, Status: models.AgentReplyHeld,
			Subject: replySubject(mail.Subject), From: recipient, To: strings.TrimSpace(mail.Sender), Text: text,
			SendAfter: &sendAfter,
		})
		if err != nil {
			return err
		}
		_, err = tx.EnqueueAgentJob(&models.AgentJob{AgentID: run.Agent.ID, MailboxID: mailbox.ID, Kind: models.AgentJobSend, SubjectID: reply.ID, NotBefore: &sendAfter})
		return err
	}); err != nil {
		return err
	}
	return nil
}

func threadIdOf(mail *models.Mail) string {
	if mail.ThreadID != "" {
		return mail.ThreadID
	}
	return mail.ID
}

// recordRefusal writes why a message was not answered, so the person can
// see it in the agent's activity rather than wonder.
func (self *Agent) recordRefusal(ctx context.Context, run *Run, mail *models.Mail, reason string) error {
	return run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		transcript, err := self.recordRun(tx, run, fmt.Sprintf("Did not answer %q: %s", mail.Subject, reason), "", nil, "")
		if err != nil {
			return err
		}
		_, err = tx.CreateAgentReply(&models.AgentReply{
			AgentID: run.Agent.ID, MailboxID: run.Mailbox.ID, MailID: mail.ID, ThreadID: threadIdOf(mail),
			RunID: transcript.ID, Status: models.AgentReplyRefused, Reason: reason,
			Subject: replySubject(mail.Subject), From: recipientOf(run.Mailbox, mail), To: strings.TrimSpace(mail.Sender),
		})
		return err
	})
}

// storeDraft files a composed message in Drafts, in its conversation, the
// way the composer's own save does.
func (self *Agent) storeDraft(ctx context.Context, tx db.Transaction, mailbox *models.Mailbox, from string, original *models.Mail, composed *mailer.Composed) (*models.MailboxItem, error) {
	drafts, err := tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindDrafts)
	if err != nil {
		return nil, err
	}
	if drafts == nil {
		return nil, fmt.Errorf("the mailbox has no Drafts folder")
	}
	_, domainName := mailparse.SplitAddress(from)
	domain, err := tx.GetDomainByName(domainName)
	if err != nil {
		return nil, err
	}
	if domain == nil {
		return nil, fmt.Errorf("the domain of %q is gone", from)
	}
	threadId, err := mx.ThreadIDFor(tx, composed.Headers)
	if err != nil {
		return nil, err
	}
	if threadId == "" {
		threadId = threadIdOf(original)
	}
	created, err := tx.CreateMail(&models.Mail{
		ThreadID:   threadId,
		DomainID:   domain.ID,
		EnvelopeID: composed.ID,
		Sender:     from,
		Recipients: []string{strings.TrimSpace(original.Sender)},
		MessageID:  mailparse.DecodeHeaderValue(mailparse.FindHeaderValue(composed.Headers, "Message-ID")),
		From:       from,
		Subject:    replySubject(original.Subject),
		Headers:    composed.Headers,
		Body:       composed.Body,
		Size:       uint64(len(composed.Body)),
		Status:     models.MailStatusAccepted,
		ReceivedAt: time.Now(),
		Kind:       models.MailKindDraft,
	}, nil)
	if err != nil {
		return nil, err
	}
	if err := self.settings.Storage.Put(ctx, created.ID, composed.Headers, composed.Body); err != nil {
		return nil, err
	}
	yes := true
	item, err := tx.AddItem(drafts.ID, created.ID, "", models.MailboxItemFlags{Draft: &yes, Seen: &yes})
	if err != nil {
		return nil, err
	}
	if err := tx.SetMailSearch(created.ID, mx.SearchDocument(created), mx.AttachmentCount(created)); err != nil {
		return nil, err
	}
	return item, nil
}

// discardDraft removes a draft the agent holds: the item, and the row and
// bytes when nothing else holds them.
func (self *Agent) discardDraft(ctx context.Context, tx db.Transaction, itemId string) error {
	if itemId == "" {
		return nil
	}
	item, err := tx.GetItem(itemId)
	if err != nil || item == nil {
		return err
	}
	if _, err := tx.DeleteItems([]string{item.ID}); err != nil {
		return err
	}
	others, err := tx.ListItemsByMail(item.MailID)
	if err != nil || len(others) > 0 {
		return err
	}
	if err := tx.DeleteMail(item.MailID, nil); err != nil {
		return err
	}
	if err := self.settings.Storage.Delete(ctx, item.MailID); err != nil {
		log.Warningf("failed to remove the bytes of draft %q: %s", item.MailID, err)
	}
	return nil
}
