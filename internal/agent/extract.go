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

// What a message carries that belongs somewhere else.
//
// An invitation with a calendar part in it is read at delivery and becomes an
// event (see internal/scheduling). Most appointments never arrive that way.
// They arrive as "shall we say Thursday at four", and most new telephone
// numbers arrive at the bottom of a signature, and until now nothing read
// either of them: the message was sorted into Work and left there.
//
// An extract run reads a message triage flagged and says what it found. It
// writes nothing anywhere -- the finding is a proposal on the insight, and
// the reader draws a card from it with the fields filled in and the line they
// came from quoted underneath. Adding it is a press.
//
// That it proposes rather than writes is the whole design. A message is a
// stranger's words; putting an appointment in somebody's diary because those
// words mentioned a day is how a calendar stops being trusted. It is the same
// reasoning as the hold window on an automatic reply.

// extractTools are what the run may reach: the message and the thread it
// belongs to, the diary and the address book to see what is already there,
// and the date so that "Thursday" is a date.
var extractTools = map[string]bool{
	"mail_read": true, "calendar": true, "contact_book": true,
	"datetime": true,
}

// ExtractAnswer is the object the run answers with.
type ExtractAnswer struct {
	Events   []ExtractedEvent   `json:"events"`
	Contacts []ExtractedContact `json:"contacts"`
}

// ExtractedEvent is an appointment the words describe.
type ExtractedEvent struct {
	Summary  string `json:"summary"`
	Starts   string `json:"starts"`
	Ends     string `json:"ends"`
	Location string `json:"location"`
	AllDay   bool   `json:"all_day"`
	Because  string `json:"because"`
}

// ExtractedContact is somebody the words describe, usually a signature.
type ExtractedContact struct {
	Name         string   `json:"name"`
	Organization string   `json:"organization"`
	Title        string   `json:"title"`
	Emails       []string `json:"emails"`
	Phones       []string `json:"phones"`
	Note         string   `json:"note"`
	ContactID    string   `json:"contact_id"`
	Because      string   `json:"because"`
}

// runExtract is the handler for an extract job; its subject is the message.
func (self *Agent) runExtract(ctx context.Context, run *Run) error {
	if run.Mailbox == nil || run.Source == nil || !run.Source.Granted {
		return nil
	}
	configuration := run.Configuration()
	if !FeatureAllowed(configuration, "triage") || !self.canThink(configuration) {
		return nil
	}
	var mail *models.Mail
	var insight *models.MailInsight
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
		return nil
	}); err != nil {
		return err
	}
	if mail == nil || insight == nil {
		return nil
	}

	message, err := BuildMessageContext(ctx, run.Storage(), mail, configuration.Agent.Limits.MaxBodyCharacters, false)
	if err != nil {
		return err
	}
	prompt, err := render("extract.txt", map[string]any{
		"PersonName": personName(run.Owner),
		"Language":   languageName(KnowledgeLanguage(run.Agent, run.Owner)),
		"Today":      time.Now().In(Location(run.Owner)).Format("Monday 2 January 2006"),
		"Zone":       Location(run.Owner).String(),
		"Sender":     strings.TrimSpace(mail.From),
		"Message":    strings.TrimSpace(message.Render()),
	})
	if err != nil {
		return err
	}
	thinking, err := self.think(ctx, run, fmt.Sprintf("Read %q for what it carries", mail.Subject), prompt,
		extractTools, roundsFor(configuration, models.AgentJobExtract), models.AgentJobExtract, config.AgentWorkTriage)
	if err != nil {
		return err
	}
	answer, err := llm.Extract[ExtractAnswer](thinking.Text)
	if err != nil {
		log.Warningf("the extract run for %q did not answer with an object: %s", mail.ID, err)
		return nil
	}
	proposals := InterpretExtraction(&answer)
	if len(proposals) == 0 {
		self.retitle(ctx, run, thinking.Conversation, fmt.Sprintf("Read %q: nothing to add", mail.Subject))
		return nil
	}
	self.retitle(ctx, run, thinking.Conversation, fmt.Sprintf("Found %s in %q", describeProposals(proposals), mail.Subject))
	return run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		return tx.ReplaceMailProposals(run.Mailbox.ID, mail.ID, proposals)
	})
}

// InterpretExtraction turns the model's answer into proposals, refusing what
// is not usable: an event with no summary or no start is not an appointment,
// and a contact with neither an address nor a number is not a person.
func InterpretExtraction(answer *ExtractAnswer) []models.MailProposal {
	proposals := []models.MailProposal{}
	for _, event := range answer.Events {
		summary := strings.TrimSpace(event.Summary)
		starts := strings.TrimSpace(event.Starts)
		if summary == "" || starts == "" {
			continue
		}
		proposals = append(proposals, models.MailProposal{
			Kind: models.MailProposalEvent, Summary: cut(summary, 200), Starts: starts,
			Ends: strings.TrimSpace(event.Ends), Location: cut(strings.TrimSpace(event.Location), 200),
			AllDay: event.AllDay, Because: cut(strings.TrimSpace(event.Because), 300),
		})
	}
	for _, contact := range answer.Contacts {
		emails := trimmed(contact.Emails)
		phones := trimmed(contact.Phones)
		if len(emails) == 0 && len(phones) == 0 {
			continue
		}
		name := strings.TrimSpace(contact.Name)
		if name == "" && len(emails) > 0 {
			name = emails[0]
		}
		proposals = append(proposals, models.MailProposal{
			Kind: models.MailProposalContact, Name: cut(name, 200),
			Organization: cut(strings.TrimSpace(contact.Organization), 200),
			Title:        cut(strings.TrimSpace(contact.Title), 200),
			Emails:       emails, Phones: phones,
			Note:      cut(strings.TrimSpace(contact.Note), 500),
			ContactID: strings.TrimSpace(contact.ContactID),
			Because:   cut(strings.TrimSpace(contact.Because), 300),
		})
	}
	return proposals
}

func trimmed(values []string) []string {
	kept := []string{}
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			kept = append(kept, value)
		}
	}
	return kept
}

func cut(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

// describeProposals is what the run is called in the activity view.
func describeProposals(proposals []models.MailProposal) string {
	events, contacts := 0, 0
	for _, proposal := range proposals {
		if proposal.Kind == models.MailProposalEvent {
			events++
		} else {
			contacts++
		}
	}
	parts := []string{}
	if events > 0 {
		parts = append(parts, plural(events, "an appointment", "%d appointments"))
	}
	if contacts > 0 {
		parts = append(parts, plural(contacts, "somebody's details", "%d people's details"))
	}
	return strings.Join(parts, " and ")
}

func plural(count int, one, many string) string {
	if count == 1 {
		return one
	}
	return fmt.Sprintf(many, count)
}
