package agent

import (
	"fmt"
	netmail "net/mail"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// The person's say over alerts: the switches and the night are on the
// agent and the mailbox (models.Agent, models.AgentMailbox), and "don't
// tell me about these" is a mute, a row of its own that the candidate step
// and the alert job read. A mute is not a memory the decision is shown:
// a model can be talked out of what it was told, and the person who said
// "stop" should not have to hope; and a row can be listed and taken back.

// alertMutedReason is what a candidate a mute matched is dropped with.
const alertMutedReason = "muted"

// alertBounds are what the alert job keeps to for one person: their
// night, as minutes after midnight in their zone, the day's most, and
// what they asked not to be told.
type alertBounds struct {
	quietStartMinute int
	quietEndMinute   int
	dailyMost        int
	mutes            []*models.AgentAlertMute
}

// alertBoundsOf reads the person's settings. A time that does not read
// falls back to the default rather than to no night at all.
func alertBoundsOf(agent *models.Agent, mutes []*models.AgentAlertMute) *alertBounds {
	start, end := agent.AlertQuietHours()
	return &alertBounds{
		quietStartMinute: clockMinute(start, models.AlertQuietStartDefault),
		quietEndMinute:   clockMinute(end, models.AlertQuietEndDefault),
		dailyMost:        agent.EffectiveAlertDailyMost(),
		mutes:            mutes,
	}
}

// clockMinute is "22:00" as minutes after midnight, or the fallback's.
func clockMinute(clock, fallback string) int {
	parsed, err := time.Parse("15:04", clock)
	if err != nil {
		parsed, _ = time.Parse("15:04", fallback)
	}
	return parsed.Hour()*60 + parsed.Minute()
}

// alertFacts are what a mute is matched against: who a candidate is from,
// what it is about, and what kind of thing it is.
type alertFacts struct {
	senderAddress string
	subjectKeys   []string
	kinds         []string
}

// candidateFacts are the facts of a candidate about a message from the
// address given, which the sorting filed under the category given.
func candidateFacts(candidate *models.AgentAlertCandidate, from, category string) *alertFacts {
	facts := &alertFacts{senderAddress: alertSenderAddress(from)}
	if candidate.BurstKey != "" {
		facts.subjectKeys = append(facts.subjectKeys, candidate.BurstKey)
	}
	if candidate.CandidateKind == models.AlertCandidateBurst {
		facts.kinds = append(facts.kinds, models.AlertKindBurst)
	}
	if category = strings.ToLower(strings.TrimSpace(category)); category != "" {
		facts.kinds = append(facts.kinds, category)
	}
	return facts
}

// mutedBy is the first mute that matches the facts, or nil. A domain
// matches its own subdomains: muting example.com mutes mail.example.com.
func mutedBy(mutes []*models.AgentAlertMute, facts *alertFacts) *models.AgentAlertMute {
	senderDomain := addressDomain(facts.senderAddress)
	for _, mute := range mutes {
		switch mute.MuteScope {
		case models.AlertMuteSender:
			if facts.senderAddress != "" && facts.senderAddress == mute.MuteTarget {
				return mute
			}
		case models.AlertMuteDomain:
			if senderDomain != "" && (senderDomain == mute.MuteTarget || strings.HasSuffix(senderDomain, "."+mute.MuteTarget)) {
				return mute
			}
		case models.AlertMuteSubjectKey:
			for _, subjectKey := range facts.subjectKeys {
				if normalizedSubjectKey(subjectKey) == mute.MuteTarget {
					return mute
				}
			}
		case models.AlertMuteKind:
			for _, kind := range facts.kinds {
				if kind == mute.MuteTarget {
					return mute
				}
			}
		}
	}
	return nil
}

// alertSenderAddress is the bare address of a From line, in lower case.
func alertSenderAddress(from string) string {
	from = strings.TrimSpace(from)
	if from == "" {
		return ""
	}
	if parsed, err := netmail.ParseAddress(from); err == nil {
		return strings.ToLower(parsed.Address)
	}
	if opened := strings.LastIndex(from, "<"); opened >= 0 {
		from = strings.TrimSuffix(from[opened+1:], ">")
	}
	return strings.ToLower(strings.TrimSpace(from))
}

// normalizedSubjectKey is a subject key in the one form it is stored and
// compared in: lower case, the spaces collapsed.
func normalizedSubjectKey(subjectKey string) string {
	return strings.Join(strings.Fields(strings.ToLower(subjectKey)), " ")
}

// NormalizeAlertMute checks a scope and puts its target in the form it is
// matched in: an address or a domain in lower case, a subject key as the
// alerts keep them, a kind as a word.
func NormalizeAlertMute(muteScope models.AlertMuteScope, muteTarget string) (string, error) {
	if !muteScope.IsValid() {
		return "", fmt.Errorf("%q is not sender, domain, subjectKey or kind", muteScope)
	}
	muteTarget = strings.TrimSpace(muteTarget)
	switch muteScope {
	case models.AlertMuteSender:
		muteTarget = alertSenderAddress(muteTarget)
		if !strings.Contains(muteTarget, "@") {
			return "", fmt.Errorf("%q is not an address", muteTarget)
		}
	case models.AlertMuteDomain:
		muteTarget = strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(muteTarget, "@"), "*."))
		if at := strings.LastIndex(muteTarget, "@"); at >= 0 {
			muteTarget = muteTarget[at+1:]
		}
		if !strings.Contains(muteTarget, ".") || strings.ContainsAny(muteTarget, " /") {
			return "", fmt.Errorf("%q is not a domain", muteTarget)
		}
	case models.AlertMuteSubjectKey:
		muteTarget = normalizedSubjectKey(muteTarget)
	case models.AlertMuteKind:
		muteTarget = strings.ToLower(muteTarget)
		if strings.ContainsAny(muteTarget, " @") {
			return "", fmt.Errorf("%q is not a kind: burst, or a category such as notification", muteTarget)
		}
	}
	if muteTarget == "" {
		return "", fmt.Errorf("say what to mute")
	}
	return muteTarget, nil
}

// AlertCovered is one message an alert was about, as the person is shown
// it beside the alert.
type AlertCovered struct {
	MailID        string `json:"mailId"`
	Subject       string `json:"subject"`
	FromAddress   string `json:"fromAddress"`
	CandidateKind string `json:"candidateKind"`
	// MailCategory is what the sorting called the message, when it did.
	MailCategory string `json:"mailCategory"`
}

// AlertView is an alert as the person reads it back: what was said, when,
// and what it was about.
type AlertView struct {
	ID         string          `json:"id"`
	AlertText  string          `json:"alertText"`
	SentAt     time.Time       `json:"sentAt"`
	SubjectKey string          `json:"subjectKey"`
	IsUrgent   bool            `json:"isUrgent"`
	Covered    []*AlertCovered `json:"covered"`
}

// ListAlertViews is the agent's latest alerts, newest first, each with the
// messages it covered.
func ListAlertViews(tx db.Transaction, agentId string, limit int) ([]*AlertView, error) {
	alerts, err := tx.ListRecentAgentAlerts(agentId, limit)
	if err != nil {
		return nil, err
	}
	views := make([]*AlertView, 0, len(alerts))
	for _, alert := range alerts {
		covered, err := coveredByAlert(tx, alert)
		if err != nil {
			return nil, err
		}
		views = append(views, &AlertView{
			ID: alert.ID, AlertText: alert.AlertText, SentAt: alert.SentAt, SubjectKey: alert.SubjectKey, IsUrgent: alert.IsUrgent, Covered: covered,
		})
	}
	return views, nil
}

// coveredByAlert is the messages an alert's candidates were about, in the
// order they were made; a message no longer stored is left out.
func coveredByAlert(tx db.Transaction, alert *models.AgentAlert) ([]*AlertCovered, error) {
	candidates, err := tx.ListAgentAlertCandidatesByID(alert.AgentID, alert.CandidateIDs)
	if err != nil {
		return nil, err
	}
	mailIds := make([]string, 0, len(candidates))
	mailIdsByMailbox := map[string][]string{}
	for _, candidate := range candidates {
		mailIds = append(mailIds, candidate.MailID)
		mailIdsByMailbox[candidate.MailboxID] = append(mailIdsByMailbox[candidate.MailboxID], candidate.MailID)
	}
	mails, err := tx.GetMails(mailIds, nil)
	if err != nil {
		return nil, err
	}
	mailsById := map[string]*models.Mail{}
	for _, mail := range mails {
		if mail != nil {
			mailsById[mail.ID] = mail
		}
	}
	categories := map[string]string{}
	for mailboxId, ids := range mailIdsByMailbox {
		insights, err := tx.GetMailInsights(mailboxId, ids)
		if err != nil {
			return nil, err
		}
		for mailId, insight := range insights {
			if insight != nil {
				categories[mailId] = insight.Category
			}
		}
	}
	covered := []*AlertCovered{}
	for _, candidate := range candidates {
		mail := mailsById[candidate.MailID]
		if mail == nil {
			continue
		}
		from := mail.From
		if from == "" {
			from = mail.Sender
		}
		covered = append(covered, &AlertCovered{
			MailID: mail.ID, Subject: mail.Subject, FromAddress: alertSenderAddress(from),
			CandidateKind: string(candidate.CandidateKind), MailCategory: categories[mail.ID],
		})
	}
	return covered, nil
}

// MuteAlert keeps a mute for the agent. With an alert, the target may be
// left out and is taken from it: its subject key, or the sender, the
// sender's domain or the kind of the first message it covered; the scope
// left out is the subject key. Without one, the scope and the target are
// the person's own.
func MuteAlert(tx db.Transaction, agent *models.Agent, alertId string, muteScope models.AlertMuteScope, muteTarget string) (*models.AgentAlertMute, error) {
	alertId = strings.TrimSpace(alertId)
	if muteScope == "" && alertId != "" {
		muteScope = models.AlertMuteSubjectKey
	}
	if alertId != "" && strings.TrimSpace(muteTarget) == "" {
		alert, err := tx.GetAgentAlert(agent.ID, alertId)
		if err != nil {
			return nil, err
		}
		if alert == nil {
			return nil, fmt.Errorf("there is no alert %q", alertId)
		}
		if muteTarget, err = muteTargetOf(tx, alert, muteScope); err != nil {
			return nil, err
		}
	}
	normalized, err := NormalizeAlertMute(muteScope, muteTarget)
	if err != nil {
		return nil, err
	}
	return tx.CreateAgentAlertMute(&models.AgentAlertMute{AgentID: agent.ID, MuteScope: muteScope, MuteTarget: normalized, AlertID: alertId})
}

// muteTargetOf is what an alert says for a scope.
func muteTargetOf(tx db.Transaction, alert *models.AgentAlert, muteScope models.AlertMuteScope) (string, error) {
	if muteScope == models.AlertMuteSubjectKey {
		return alert.SubjectKey, nil
	}
	covered, err := coveredByAlert(tx, alert)
	if err != nil {
		return "", err
	}
	if len(covered) == 0 {
		return "", fmt.Errorf("the messages the alert was about are gone; mute it by its subject key instead")
	}
	first := covered[0]
	switch muteScope {
	case models.AlertMuteSender:
		return first.FromAddress, nil
	case models.AlertMuteDomain:
		return addressDomain(first.FromAddress), nil
	case models.AlertMuteKind:
		if first.CandidateKind == string(models.AlertCandidateBurst) {
			return models.AlertKindBurst, nil
		}
		if first.MailCategory == "" {
			return "", fmt.Errorf("the message the alert was about has no category; mute its sender instead")
		}
		return first.MailCategory, nil
	}
	return "", fmt.Errorf("%q is not sender, domain, subjectKey or kind", muteScope)
}
