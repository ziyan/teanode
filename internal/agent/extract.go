package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
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
// That it proposes rather than writes is the whole design for the diary and
// the address book. A follow-up is different. It goes on the agent's own list
// of what is due, on its way or coming up (models.AgentFollowUp), which
// changes nothing the person owns, so it is written at once: a card for every
// parcel would be the hassle the list exists to remove. A message is a
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
	Events    []ExtractedEvent    `json:"events"`
	Contacts  []ExtractedContact  `json:"contacts"`
	FollowUps []ExtractedFollowUp `json:"follow_ups"`
}

// ExtractedAmount is an amount as the model wrote it, a number or the text
// of one such as "1,234.00", which is read when the follow-up is.
type ExtractedAmount string

// UnmarshalJSON accepts a JSON number or string.
func (self *ExtractedAmount) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*self = ExtractedAmount(text)
		return nil
	}
	var number json.Number
	if err := json.Unmarshal(data, &number); err != nil {
		return err
	}
	*self = ExtractedAmount(number.String())
	return nil
}

// ExtractedFollowUp is something the message says is due, on its way or
// coming up, or says is done.
type ExtractedFollowUp struct {
	Kind           string          `json:"kind"`
	Title          string          `json:"title"`
	Payee          string          `json:"payee"`
	Carrier        string          `json:"carrier"`
	Amount         ExtractedAmount `json:"amount"`
	CurrencyCode   string          `json:"currency_code"`
	TrackingNumber string          `json:"tracking_number"`
	DueOn          string          `json:"due_on"`
	Place          string          `json:"place"`
	Key            string          `json:"key"`
	Resolves       bool            `json:"resolves"`
	Because        string          `json:"because"`
}

// FollowUpFinding is a follow-up the answer carried that is usable: the
// follow-up to keep, or, when Resolves, the one to close and the words that
// say why.
type FollowUpFinding struct {
	FollowUp models.AgentFollowUp
	Resolves bool
	Because  string
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
	findings := InterpretFollowUps(&answer, run.Agent.ID, mail.ID)
	if len(proposals) == 0 && len(findings) == 0 {
		self.retitle(ctx, run, thinking.Conversation, fmt.Sprintf("Read %q: nothing to add", mail.Subject))
		return nil
	}
	found := describeProposals(proposals)
	if len(findings) > 0 {
		if found != "" {
			found += " and "
		}
		found += plural(len(findings), "something to follow up", "%d things to follow up")
	}
	self.retitle(ctx, run, thinking.Conversation, fmt.Sprintf("Found %s in %q", found, mail.Subject))
	return run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		if len(proposals) > 0 {
			if err := tx.ReplaceMailProposals(run.Mailbox.ID, mail.ID, proposals); err != nil {
				return err
			}
		}
		return StoreFollowUps(tx, findings)
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

// InterpretFollowUps turns the model's answer into follow-ups for the agent,
// refusing what is not usable: an unknown kind, one with no key, a bill with
// no amount that can be read, a bill, deadline or event with no day. Those
// are guesses, and a guessed bill is worse than a missing one. A follow-up
// that resolves is held to less, since a receipt or a delivery notice often
// leaves out the amount and the day, and only has to name what it closes.
//
// The key is made here and not left to the model's spelling: the kind in
// front, the rest in lower case with its spaces gathered, so a later message
// about the same parcel or bill finds the same row.
func InterpretFollowUps(answer *ExtractAnswer, agentId, mailId string) []FollowUpFinding {
	findings := []FollowUpFinding{}
	for _, followUp := range answer.FollowUps {
		kind := models.AgentFollowUpKind(strings.ToLower(strings.TrimSpace(followUp.Kind)))
		if !kind.IsValid() {
			continue
		}
		trackingNumber := cut(strings.TrimSpace(followUp.TrackingNumber), 100)
		key := followUp.Key
		if strings.TrimSpace(key) == "" && kind == models.AgentFollowUpParcel {
			key = trackingNumber
		}
		followUpKey := FollowUpKeyOf(kind, key)
		if followUpKey == "" {
			continue
		}
		finding := FollowUpFinding{
			Resolves: followUp.Resolves,
			Because:  cut(strings.TrimSpace(followUp.Because), 300),
			FollowUp: models.AgentFollowUp{
				AgentID: agentId, FollowUpKind: kind, FollowUpKey: followUpKey,
				FollowUpState: models.AgentFollowUpOpen, SourceItemIDs: []string{mailId},
			},
		}
		if followUp.Resolves {
			findings = append(findings, finding)
			continue
		}
		title := cut(strings.TrimSpace(followUp.Title), 200)
		if title == "" {
			continue
		}
		dueOn := ""
		if day, err := time.Parse(time.DateOnly, strings.TrimSpace(followUp.DueOn)); err == nil {
			dueOn = day.Format(time.DateOnly)
		}
		if dueOn == "" && kind != models.AgentFollowUpParcel {
			continue
		}
		amount := ""
		if kind == models.AgentFollowUpBill {
			if amount = parseFollowUpAmount(string(followUp.Amount)); amount == "" {
				continue
			}
		}
		finding.FollowUp.FollowUpTitle = title
		finding.FollowUp.Payee = cut(strings.TrimSpace(followUp.Payee), 200)
		finding.FollowUp.Carrier = cut(strings.TrimSpace(followUp.Carrier), 100)
		finding.FollowUp.Amount = amount
		finding.FollowUp.CurrencyCode = cut(strings.ToUpper(strings.TrimSpace(followUp.CurrencyCode)), 3)
		finding.FollowUp.TrackingNumber = trackingNumber
		finding.FollowUp.DueOn = dueOn
		finding.FollowUp.Place = cut(strings.TrimSpace(followUp.Place), 200)
		findings = append(findings, finding)
	}
	return findings
}

// FollowUpKeyOf is the stored key of a follow-up: the kind in front, then
// the key in lower case with its spaces gathered, or "" when the key says
// nothing. Extract and the people who add a follow-up by hand make it the
// same way, so a later message about a bill someone added finds that row.
func FollowUpKeyOf(kind models.AgentFollowUpKind, key string) string {
	key = strings.Join(strings.Fields(strings.ToLower(key)), " ")
	if key == "" {
		return ""
	}
	return cut(string(kind)+":"+key, 300)
}

// parseFollowUpAmount reads an amount the way a bill writes it, with a
// currency sign or a thousands separator, as a decimal with two places, or
// "" when it is not a positive number.
func parseFollowUpAmount(text string) string {
	cleaned := strings.Map(func(character rune) rune {
		if (character >= '0' && character <= '9') || character == '.' {
			return character
		}
		return -1
	}, text)
	amount, err := strconv.ParseFloat(cleaned, 64)
	if err != nil || amount <= 0 {
		return ""
	}
	return strconv.FormatFloat(amount, 'f', 2, 64)
}

// StoreFollowUps writes what InterpretFollowUps found. A finding that
// resolves closes the open follow-up with its key, and when there is none
// writes nothing: a delivery notice for a parcel nobody was told was coming
// is not something to follow. The others are kept by key, so the same
// message read twice, or a third notice about one parcel, changes one row.
//
// A parcel that closes this way is also a candidate for an alert saying it
// was delivered, since that is the news the person was waiting for; a bill
// or a deadline that a message closes is not, they only wanted to hear of
// it going wrong.
func StoreFollowUps(tx db.Transaction, findings []FollowUpFinding) error {
	now := time.Now()
	for _, finding := range findings {
		followUp := finding.FollowUp
		if !finding.Resolves {
			if _, _, err := tx.UpsertAgentFollowUp(&followUp); err != nil {
				return err
			}
			continue
		}
		open, err := tx.ListOpenAgentFollowUps(followUp.AgentID)
		if err != nil {
			return err
		}
		for _, candidate := range open {
			if candidate.FollowUpKey != followUp.FollowUpKey {
				continue
			}
			closedReason := finding.Because
			if closedReason == "" {
				closedReason = "a later message says it is done"
			}
			isTold := candidate.FollowUpKind == models.AgentFollowUpParcel
			closed, err := tx.UpdateAgentFollowUp(candidate.ID, func(locked *models.AgentFollowUp) error {
				locked.FollowUpState = models.AgentFollowUpClosed
				locked.ClosedReason = closedReason
				locked.SourceItemIDs = append(locked.SourceItemIDs, followUp.SourceItemIDs...)
				if isTold {
					locked.ToldState, locked.ToldAt = models.AgentFollowUpToldDelivered, &now
				}
				return nil
			})
			if err != nil {
				return err
			}
			if isTold {
				if err := noteParcelDelivered(tx, closed, now); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// noteParcelDelivered writes the alert candidate that a parcel arrived and
// queues the alert job, for an agent that has alerts on.
func noteParcelDelivered(tx db.Transaction, parcel *models.AgentFollowUp, now time.Time) error {
	agent, err := tx.GetAgent(parcel.AgentID)
	if err != nil {
		return err
	}
	if agent == nil || !agent.Active() || !agent.IsAlertsEnabled {
		return nil
	}
	mutes, err := tx.ListAgentAlertMutes(agent.ID)
	if err != nil {
		return err
	}
	candidate, err := writeFollowUpCandidate(tx, mutes, &models.AgentAlertCandidate{
		AgentID: agent.ID, CandidateKind: models.AlertCandidateFollowUp, AlertSignal: models.AlertSignalSoon,
		CandidateReason: "a message says it was delivered", FollowUpIDs: []string{parcel.ID},
		FollowUpToldState: models.AgentFollowUpToldDelivered, FollowUpSubjectKey: parcel.FollowUpKey + ":" + string(models.AgentFollowUpToldDelivered),
	}, now)
	if err != nil || candidate == nil {
		return err
	}
	return queueAlert(tx, agent.ID, false, now)
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
	if events+contacts == 0 {
		return ""
	}
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
