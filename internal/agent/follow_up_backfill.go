package agent

import (
	"context"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// The look back: the first time an agent with alerts on is seen after
// follow-ups ship, the messages of the last 45 days that it already triaged
// and that look like they carry a bill, a parcel, a form to return or a
// dated notice are queued for an extract run, so the list holds what is
// already in flight the day it ships instead of only what arrives after.
//
// Nothing is triaged again and no model chooses: triage already flagged
// some of them (the extract flag), and for the rest a short list of words in
// the category and the subject is a net that is cheap to cast and is allowed
// to catch too much, since the extract run that follows reads the message
// and keeps only what is usable. The extract runs are ordinary extract jobs
// (runExtract), queued a little apart so that they do not crowd out the
// triage and replies of mail arriving now.
//
// That it ran is a column on the agent (follow_up_backfilled_at), claimed by
// a single conditional update, so a restart or a second worker cannot run it
// twice. The person who turns alerts on later is picked up by the same
// worker the next time it looks, and `teanode agent follow backfill` runs it
// again on demand.

const (
	// followUpBackfillDays is how far back the look reaches, by when the
	// message was received.
	followUpBackfillDays = 45

	// followUpBackfillMost is how many messages one look queues at most,
	// the newest first.
	followUpBackfillMost = 150

	// followUpBackfillScanMost is how many insights of one mailbox are read
	// to choose from.
	followUpBackfillScanMost = 2000

	// followUpBackfillSpacing is how long after the one before each extract
	// job is allowed to start. The queue runs jobs in the order they were
	// made, so without it the look would put 150 jobs in front of whatever
	// arrives in the next hour.
	followUpBackfillSpacing = 20 * time.Second

	// followUpBackfillEvery is how often the worker looks for an agent that
	// has not had the look yet.
	followUpBackfillEvery = 10 * time.Minute
)

// followUpBackfillLeftOutCategories are the sorting's categories never
// looked into: the two nobody wants, and promotions, whose "pay by Friday"
// is a sale and not a bill.
var followUpBackfillLeftOutCategories = []string{"phishing", "junk", "promotion"}

// followUpBackfillWords is a net for the look back only, not a classifier:
// words in a subject or category that suggest a bill or a payment due, a
// statement, a shipment or delivery, a form or permission slip, an RSVP, or
// a dated school, club or neighborhood notice. A message it catches by
// mistake costs one extract run that finds nothing; the live path does not
// use it, since triage flags those with the whole message in view.
var followUpBackfillWords = map[string]bool{
	"bill": true, "bills": true, "invoice": true, "invoices": true, "statement": true, "statements": true,
	"payment": true, "payments": true, "due": true, "overdue": true, "balance": true, "tax": true, "taxes": true,
	"renewal": true, "autopay": true,
	"parcel": true, "package": true, "shipment": true, "shipped": true, "shipping": true,
	"delivery": true, "delivered": true, "tracking": true,
	"form": true, "forms": true, "permission": true, "slip": true, "rsvp": true, "deadline": true,
	"school": true, "club": true, "neighborhood": true, "neighbourhood": true, "association": true,
	"concert": true, "meeting": true, "fair": true, "festival": true, "trip": true,
}

// followUpBackfillCategories are categories that suggest a dated notice on
// their own, whatever the subject says.
var followUpBackfillCategories = map[string]bool{"invitation": true}

// looksLikeFollowUp says whether the category or the subject of a sorted
// message suggests a follow-up, by the words above.
func looksLikeFollowUp(category, subject string) bool {
	if followUpBackfillCategories[strings.ToLower(strings.TrimSpace(category))] {
		return true
	}
	words := strings.FieldsFunc(strings.ToLower(category+" "+subject), func(character rune) bool {
		return !unicode.IsLetter(character)
	})
	for _, word := range words {
		if followUpBackfillWords[word] {
			return true
		}
	}
	return false
}

// chooseFollowUpBackfill picks, from the sorted messages of an agent's
// mailboxes, the ones to read for follow-ups: those sorting flagged for an
// extract, and those whose category or subject looks like one, never from a
// category left out, newest first, at most limit.
func chooseFollowUpBackfill(heads []*db.MailInsightHead, limit int) []*db.MailInsightHead {
	sorted := append([]*db.MailInsightHead{}, heads...)
	sort.SliceStable(sorted, func(first, second int) bool { return sorted[first].ReceivedAt.After(sorted[second].ReceivedAt) })
	chosen := []*db.MailInsightHead{}
	for _, head := range sorted {
		if len(chosen) >= limit {
			break
		}
		category := strings.ToLower(strings.TrimSpace(head.Category))
		if UnwantedCategory(category) || category == "promotion" {
			continue
		}
		if head.IsExtractAsked || looksLikeFollowUp(head.Category, head.Subject) {
			chosen = append(chosen, head)
		}
	}
	return chosen
}

// FollowUpBackfillView is what a look back answers with.
type FollowUpBackfillView struct {
	// QueuedMessageCount is how many past messages were queued to be read
	// for follow-ups, none when the agent has no mailbox to look into.
	QueuedMessageCount int `json:"queuedMessageCount"`
}

// BackfillFollowUps queues the extract runs of the look back for the agent
// and answers how many messages it queued. Unless isForced, it does so only
// the first time for the agent: the claim on the agent's follow_up_backfilled_at
// fails when it already ran, and nothing is queued. A forced run, for the
// person who turns alerts on later or wants it again, always queues and
// writes the time anew. An agent with no mailbox granted to it has nothing to
// look into and is not marked, so the look still happens once there is one.
//
// A message that is already waiting for an extract run keeps that job (the
// queue holds one open job per message), and a message read again finds the
// follow-ups it made before by their key, so a second look changes nothing
// but what is new.
func BackfillFollowUps(tx db.Transaction, agent *models.Agent, now time.Time, isForced bool) (int, error) {
	mailboxes, err := tx.ListMailboxes(agent.UserID)
	if err != nil {
		return 0, err
	}
	since := now.AddDate(0, 0, -followUpBackfillDays)
	heads := []*db.MailInsightHead{}
	hasMailbox := false
	for _, mailbox := range mailboxes {
		if mailbox.Agent == nil || !mailbox.Agent.Granted {
			continue
		}
		hasMailbox = true
		found, err := tx.ListMailInsightHeads(mailbox.ID, since, followUpBackfillLeftOutCategories, followUpBackfillScanMost)
		if err != nil {
			return 0, err
		}
		heads = append(heads, found...)
	}
	if !hasMailbox {
		return 0, nil
	}
	if isForced {
		if err := tx.MarkAgentFollowUpBackfilled(agent.ID, now); err != nil {
			return 0, err
		}
	} else if claimed, err := tx.ClaimAgentFollowUpBackfill(agent.ID, now); err != nil || !claimed {
		return 0, err
	}
	chosen := chooseFollowUpBackfill(heads, followUpBackfillMost)
	for index, head := range chosen {
		notBefore := now.Add(time.Duration(index) * followUpBackfillSpacing)
		if _, err := tx.EnqueueAgentJob(&models.AgentJob{
			AgentID: agent.ID, MailboxID: head.MailboxID, Kind: models.AgentJobExtract, SubjectID: head.MailID, NotBefore: &notBefore,
		}); err != nil {
			return 0, err
		}
	}
	return len(chosen), nil
}

// queueFollowUpBackfills looks, every few minutes, for an active agent with
// alerts on that has not had the look back and queues it. It waits while
// there is no model to read with, so the one look is not spent on extract
// runs that would do nothing.
func (self *Agent) queueFollowUpBackfills(ctx context.Context, now time.Time) {
	if now.Sub(self.lastFollowUpBackfill) < followUpBackfillEvery {
		return
	}
	self.lastFollowUpBackfill = now
	configuration := self.settings.Configuration()
	if !FeatureAllowed(configuration, "triage") || !self.canThink(configuration) {
		return
	}
	var agents []*models.Agent
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		agents, err = tx.ListAgents(nil)
		return err
	}); err != nil {
		log.Warningf("cannot list the agents to look back over mail for follow-ups: %s", err)
		return
	}
	for _, agent := range agents {
		if !agent.Active() || !agent.IsAlertsEnabled || agent.FollowUpBackfilledAt != nil {
			continue
		}
		if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
			queued, err := BackfillFollowUps(tx, agent, now, false)
			if err == nil && queued > 0 {
				log.Infof("queued %d messages of the last %d days for agent %q to be read for follow-ups", queued, followUpBackfillDays, agent.ID)
			}
			return err
		}); err != nil {
			log.Warningf("cannot look back over mail for follow-ups for agent %q: %s", agent.ID, err)
		}
	}
}
