package agent

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// Alert candidates: what might be worth telling the person about without
// being asked. Two things make one. The sorting says so of a message it
// has just read, and a count notices a burst of messages alike, which no
// single message's sorting can see: one sign-in code is routine, fourteen
// overnight are somebody trying the account. Neither interrupts anybody
// here; a candidate waits for the alert job, which gathers what arrives
// together and decides with the person's memory in hand (alert.go).
// docs/planning/mail-alerts-execplan.md is the design.

const (
	// alertGather is how long candidates gather before the job decides:
	// three messages about one incident arrive within a minute or two of
	// each other, and they are one alert, not three.
	alertGather = 2 * time.Minute

	// burstWindow is how far back a burst is counted, and burstThreshold
	// how many messages alike in it make one.
	burstWindow    = 6 * time.Hour
	burstThreshold = 5

	// burstSubjectLimit bounds what one count reads.
	burstSubjectLimit = 500

	// alertFreshness is how old a message may be and still make a
	// candidate. A mailbox granted today has its older mail sorted too,
	// and last month's notice is not news.
	alertFreshness = 24 * time.Hour
)

// isAlertingAllowed says whether the agent may tell the person about what
// arrives, unasked: for the whole agent when source is nil, or for one
// mailbox. Wherever the agent sorts the mail, unless the person switched
// alerts off for the agent or for that mailbox.
func isAlertingAllowed(configuration *config.Configuration, agent *models.Agent, source *models.AgentMailbox) bool {
	if !agent.Active() || !agent.IsAlertsEnabled || !FeatureAllowed(configuration, "triage") {
		return false
	}
	if source == nil {
		return true
	}
	return source.Granted && source.Triage != nil && source.Triage.Enabled && source.IsAlertsEnabled()
}

// AlertSubjectPattern is a subject with what changes from one message of
// a kind to the next taken out: the digits of a code, a count, a date. Lower
// case, with the spaces collapsed, so "Your code is 481 022" and "Your
// code is 99 310" are the same message sent twice.
func AlertSubjectPattern(subject string) string {
	var builder strings.Builder
	isSpace := false
	for _, character := range strings.ToLower(subject) {
		switch {
		case character >= '0' && character <= '9':
			continue
		case unicode.IsSpace(character):
			isSpace = true
			continue
		}
		if isSpace && builder.Len() > 0 {
			builder.WriteByte(' ')
		}
		isSpace = false
		builder.WriteRune(character)
	}
	return builder.String()
}

// addressDomain is the domain of an address, in lower case, or empty.
func addressDomain(address string) string {
	at := strings.LastIndex(address, "@")
	if at < 0 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(strings.TrimSuffix(address[at+1:], ">")))
}

// isReplySubject says the subject answers or passes on another message: a
// conversation going back and forth is not a burst, however quick.
func isReplySubject(pattern string) bool {
	for _, prefix := range []string{"re:", "fw:", "fwd:", "aw:", "wg:", "sv:", "tr:"} {
		if strings.HasPrefix(pattern, prefix) {
			return true
		}
	}
	return false
}

// burstKeyOf is what a burst is known by within a mailbox: the sender's
// domain and the subject's pattern.
func burstKeyOf(fromDomain, subjectPattern string) string {
	return fromDomain + "|" + subjectPattern
}

// isBurstGrown says a burst counted again has grown enough since the last
// candidate to be worth another: by half, so fourteen codes after five is
// news and six after five is not.
func isBurstGrown(previousCount, count int) bool {
	return count*2 >= previousCount*3
}

// noteAlertCandidates makes the candidates a message just sorted gives
// rise to, in the sorting's own transaction, and queues the job that
// decides on them. Nothing here calls a model.
func (self *Agent) noteAlertCandidates(tx db.Transaction, run *Run, mail *models.Mail, insight *models.MailInsight, now time.Time) error {
	if !isAlertingAllowed(run.Configuration(), run.Agent, run.Source) {
		return nil
	}
	if !mail.ReceivedAt.IsZero() && now.Sub(mail.ReceivedAt) > alertFreshness {
		return nil
	}
	// Nothing in Junk or Trash is worth an interruption: the filter or
	// the person has already said what it is.
	filed, err := self.filedAway(tx, run.Mailbox.ID, mail.ID)
	if err != nil || filed {
		return err
	}
	var candidates []*models.AgentAlertCandidate
	if insight.AlertSignal == models.AlertSignalSoon || insight.AlertSignal == models.AlertSignalNow {
		candidates = append(candidates, &models.AgentAlertCandidate{
			AgentID: run.Agent.ID, MailboxID: run.Mailbox.ID, MailID: mail.ID, CandidateKind: models.AlertCandidateMessage,
			AlertSignal: insight.AlertSignal, CandidateReason: insight.AlertReason,
		})
	}
	burst, err := self.burstCandidate(tx, run, mail, now)
	if err != nil {
		return err
	}
	if burst != nil {
		candidates = append(candidates, burst)
	}
	if len(candidates) == 0 {
		return nil
	}
	mutes, err := tx.ListAgentAlertMutes(run.Agent.ID)
	if err != nil {
		return err
	}
	from := mail.From
	if from == "" {
		from = mail.Sender
	}
	isWaiting, isPressing := false, false
	for _, candidate := range candidates {
		created, err := tx.CreateAgentAlertCandidate(candidate)
		if err != nil {
			return err
		}
		// Kept, and dropped at once: what the person said not to be told
		// about is still written down, so the list of what was dropped
		// shows the mute working.
		if mutedBy(mutes, candidateFacts(candidate, from, insight.Category)) != nil {
			if err := tx.DropAgentAlertCandidates([]string{created.ID}, alertMutedReason, now); err != nil {
				return err
			}
			continue
		}
		isWaiting = true
		isPressing = isPressing || candidate.AlertSignal == models.AlertSignalNow || candidate.CandidateKind == models.AlertCandidateBurst
	}
	if !isWaiting {
		return nil
	}
	return queueAlert(tx, run.Agent.ID, isPressing, now)
}

// burstCandidate counts the messages like this one that arrived in the
// mailbox in the window, and makes a candidate when there are enough of
// them and none has been made for the same burst since, unless it has
// grown by half.
func (self *Agent) burstCandidate(tx db.Transaction, run *Run, mail *models.Mail, now time.Time) (*models.AgentAlertCandidate, error) {
	// A mailing list sends many alike on purpose, numbered: a digest's
	// volume and issue are the digits a pattern takes out.
	if mail.ListKey != "" {
		return nil, nil
	}
	fromDomain := addressDomain(mail.From)
	if fromDomain == "" {
		fromDomain = addressDomain(mail.Sender)
	}
	subjectPattern := AlertSubjectPattern(mail.Subject)
	if fromDomain == "" || isReplySubject(subjectPattern) {
		return nil, nil
	}
	subjects, err := tx.ListMailSubjectsFromDomain(run.Mailbox.ID, fromDomain, now.Add(-burstWindow), burstSubjectLimit)
	if err != nil {
		return nil, err
	}
	burstCount := 0
	for _, subject := range subjects {
		if AlertSubjectPattern(subject) == subjectPattern {
			burstCount++
		}
	}
	if burstCount < burstThreshold {
		return nil, nil
	}
	burstKey := burstKeyOf(fromDomain, subjectPattern)
	previous, err := tx.LatestAgentBurstCandidate(run.Agent.ID, run.Mailbox.ID, burstKey, now.Add(-burstWindow))
	if err != nil {
		return nil, err
	}
	if previous != nil && !isBurstGrown(previous.BurstCount, burstCount) {
		return nil, nil
	}
	return &models.AgentAlertCandidate{
		AgentID: run.Agent.ID, MailboxID: run.Mailbox.ID, MailID: mail.ID, CandidateKind: models.AlertCandidateBurst,
		CandidateReason: fmt.Sprintf("%d messages from %s alike in the last %d hours", burstCount, fromDomain, int(burstWindow.Hours())),
		BurstKey:        burstKey, BurstCount: burstCount,
	}, nil
}

// queueAlert queues the job that decides on the waiting candidates, after
// they have had time to gather. One already queued takes the new ones
// with it; one waiting for the morning is brought forward when isPressing,
// because what may not wait is what the night's hold lets through.
func queueAlert(tx db.Transaction, agentId string, isPressing bool, now time.Time) error {
	notBefore := now.Add(alertGather)
	if _, err := tx.EnqueueAgentJob(&models.AgentJob{AgentID: agentId, Kind: models.AgentJobAlert, SubjectID: agentId, NotBefore: &notBefore}); err != nil {
		return err
	}
	if !isPressing {
		return nil
	}
	return tx.AdvanceAgentJob(agentId, models.AgentJobAlert, agentId, notBefore)
}
