package agent

import (
	"fmt"
	netmail "net/mail"
	"regexp"
	"slices"
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
// what it is about, and what kind of thing it is. All of them come from
// the mail and the count, never from how a model worded anything, so a
// mute of them holds for the next message alike.
type alertFacts struct {
	senderAddress string
	burstKey      string
	mailCategory  string
	subjectKeys   []string
	kinds         []string

	// spendingCategoryId is the spending category a budget candidate is
	// about, empty for anything else.
	spendingCategoryId string
}

// candidateFacts are the facts of a candidate about a message from the
// address given, which the sorting filed under the category given.
func candidateFacts(candidate *models.AgentAlertCandidate, from, category string) *alertFacts {
	if candidate.CandidateKind == models.AlertCandidateBudget {
		return budgetCandidateFacts(candidate)
	}
	// A watched message is not stored: who sent it and what the sorting
	// called it travel on the candidate.
	if candidate.CandidateKind == models.AlertCandidateWatched {
		from, category = candidate.WatchedSender, candidate.WatchedMailCategory
	}
	facts := &alertFacts{senderAddress: alertSenderAddress(from), burstKey: candidate.BurstKey}
	if candidate.BurstKey != "" {
		facts.subjectKeys = append(facts.subjectKeys, candidate.BurstKey)
	}
	if candidate.CandidateKind == models.AlertCandidateBurst {
		facts.kinds = append(facts.kinds, models.AlertKindBurst)
	}
	if category = strings.ToLower(strings.TrimSpace(category)); category != "" {
		facts.mailCategory = category
		facts.kinds = append(facts.kinds, category)
	}
	return facts
}

// budgetCandidateFacts are the facts of a budget candidate: its key, the
// kind every budget alert shares, and the spending category it is about.
func budgetCandidateFacts(candidate *models.AgentAlertCandidate) *alertFacts {
	return &alertFacts{
		subjectKeys:        []string{candidate.BudgetKey},
		kinds:              []string{models.AlertKindBudget},
		spendingCategoryId: spendingCategoryOfBudgetKey(candidate.BudgetKey),
	}
}

// coveredTerms is what an alert was about in the terms a mute names.
type coveredTerms struct {
	burstKeys       []string
	senderAddresses []string
	senderDomains   []string
	mailCategories  []string
}

// coveredTermsOf gathers the facts of the candidates an alert covers,
// each term once, in the order the candidates came.
func coveredTermsOf(facts []*alertFacts) *coveredTerms {
	terms := &coveredTerms{}
	appendOnce := func(values []string, value string) []string {
		if value == "" || slices.Contains(values, value) {
			return values
		}
		return append(values, value)
	}
	for _, fact := range facts {
		if fact == nil {
			continue
		}
		terms.burstKeys = appendOnce(terms.burstKeys, fact.burstKey)
		terms.senderAddresses = appendOnce(terms.senderAddresses, fact.senderAddress)
		terms.senderDomains = appendOnce(terms.senderDomains, addressDomain(fact.senderAddress))
		terms.mailCategories = appendOnce(terms.mailCategories, fact.mailCategory)
	}
	return terms
}

// alertCoveredTerms is what an alert was about: as recorded with it, or,
// for one recorded before the terms were kept, read from its candidates
// and their messages.
func alertCoveredTerms(tx db.Transaction, alert *models.AgentAlert) (*coveredTerms, error) {
	if len(alert.CoveredBurstKeys) > 0 || len(alert.CoveredSenderAddresses) > 0 {
		return &coveredTerms{
			burstKeys: alert.CoveredBurstKeys, senderAddresses: alert.CoveredSenderAddresses,
			senderDomains: alert.CoveredSenderDomains, mailCategories: alert.CoveredMailCategories,
		}, nil
	}
	candidates, err := tx.ListAgentAlertCandidatesByID(alert.AgentID, alert.CandidateIDs)
	if err != nil {
		return nil, err
	}
	covered, err := coveredByAlert(tx, alert)
	if err != nil {
		return nil, err
	}
	byMail := map[string]*AlertCovered{}
	for _, entry := range covered {
		byMail[entry.MailID] = entry
	}
	facts := make([]*alertFacts, 0, len(candidates))
	for _, candidate := range candidates {
		from, category := "", ""
		if entry := byMail[candidate.MailID]; entry != nil {
			from, category = entry.FromAddress, entry.MailCategory
		}
		facts = append(facts, candidateFacts(candidate, from, category))
	}
	return coveredTermsOf(facts), nil
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
		case models.AlertMuteSpendingCategory:
			if facts.spendingCategoryId != "" && strings.EqualFold(facts.spendingCategoryId, mute.MuteTarget) {
				return mute
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
		return "", fmt.Errorf("%q is not sender, domain, subjectKey, kind or spendingCategory", muteScope)
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
			return "", fmt.Errorf("%q is not a kind: burst, budget, or a category such as notification", muteTarget)
		}
	case models.AlertMuteSpendingCategory:
		// A spending category's id, as the budget alerts name it, in lower
		// case like every other target; it is matched without case.
		muteTarget = strings.ToLower(muteTarget)
		if strings.ContainsAny(muteTarget, " @:") {
			return "", fmt.Errorf("%q is not a spending category's id", muteTarget)
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

	// MuteChoices is what a Mute of the alert offers, the default first.
	MuteChoices []*AlertMuteChoice `json:"muteChoices"`
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
		terms, err := alertCoveredTerms(tx, alert)
		if err != nil {
			return nil, err
		}
		views = append(views, &AlertView{
			ID: alert.ID, AlertText: alert.AlertText, SentAt: alert.SentAt, SubjectKey: alert.SubjectKey, IsUrgent: alert.IsUrgent, Covered: covered,
			MuteChoices: alertMuteChoices(alert, terms),
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
		if candidate.CandidateKind == models.AlertCandidateWatched {
			covered = append(covered, &AlertCovered{
				Subject: candidate.WatchedSubject, FromAddress: alertSenderAddress(candidate.WatchedSender),
				CandidateKind: string(candidate.CandidateKind), MailCategory: candidate.WatchedMailCategory,
			})
			continue
		}
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

// AlertMuteChoice is one way to mute an alert: the scope, and what it
// would mute, several joined by commas.
type AlertMuteChoice struct {
	MuteScope  models.AlertMuteScope `json:"muteScope"`
	MuteTarget string                `json:"muteTarget"`
}

// alertMuteChoices is what each scope would mute for an alert, the default
// first: the burst for an alert about a burst, the sender for one about a
// message.
func alertMuteChoices(alert *models.AgentAlert, terms *coveredTerms) []*AlertMuteChoice {
	choices := []*AlertMuteChoice{}
	add := func(muteScope models.AlertMuteScope, targets []string) {
		if len(targets) > 0 {
			choices = append(choices, &AlertMuteChoice{MuteScope: muteScope, MuteTarget: strings.Join(targets, ", ")})
		}
	}
	defaultScope := defaultAlertMuteScopeOf(alert, terms)
	add(defaultScope, muteTargetsOf(alert, terms, defaultScope))
	for _, muteScope := range []models.AlertMuteScope{models.AlertMuteSubjectKey, models.AlertMuteSender, models.AlertMuteDomain, models.AlertMuteKind, models.AlertMuteSpendingCategory} {
		if muteScope != defaultScope {
			add(muteScope, muteTargetsOf(alert, terms, muteScope))
		}
	}
	return choices
}

// defaultAlertMuteScopeOf is what "don't tell me about these" mutes of an
// alert when the person does not say: its bursts when it was about any,
// else its senders. Never the model's subject key alone, which the next
// alert about the same thing may word differently. A budget alert about a
// spending category mutes that spending category, and one about a savings
// target every budget alert, since nothing narrower names it.
func defaultAlertMuteScopeOf(alert *models.AgentAlert, terms *coveredTerms) models.AlertMuteScope {
	if isBudgetAlert(alert) {
		if spendingCategoryOfBudgetKey(alert.SubjectKey) != "" {
			return models.AlertMuteSpendingCategory
		}
		return models.AlertMuteKind
	}
	if len(terms.burstKeys) == 0 && len(terms.senderAddresses) > 0 {
		return models.AlertMuteSender
	}
	return models.AlertMuteSubjectKey
}

// muteTargetsOf is what a scope mutes of an alert. The subject of an alert
// about bursts is their burst keys, which the next candidate of the same
// burst carries; of one about messages, the model's subject key.
func muteTargetsOf(alert *models.AgentAlert, terms *coveredTerms, muteScope models.AlertMuteScope) []string {
	if isBudgetAlert(alert) {
		switch muteScope {
		case models.AlertMuteSubjectKey:
			return []string{alert.SubjectKey}
		case models.AlertMuteKind:
			return []string{models.AlertKindBudget}
		case models.AlertMuteSpendingCategory:
			if spendingCategoryId := spendingCategoryOfBudgetKey(alert.SubjectKey); spendingCategoryId != "" {
				return []string{spendingCategoryId}
			}
		}
		return nil
	}
	switch muteScope {
	case models.AlertMuteSubjectKey:
		if len(terms.burstKeys) > 0 {
			return terms.burstKeys
		}
		if alert.SubjectKey != "" {
			return []string{alert.SubjectKey}
		}
	case models.AlertMuteSender:
		return terms.senderAddresses
	case models.AlertMuteDomain:
		return terms.senderDomains
	case models.AlertMuteKind:
		if len(terms.burstKeys) > 0 {
			return []string{models.AlertKindBurst}
		}
		return terms.mailCategories
	}
	return nil
}

// isBudgetAlert says an alert told a budget or savings target crossing,
// whose subject key is the crossing's budget key.
func isBudgetAlert(alert *models.AgentAlert) bool {
	return alert != nil && (strings.HasPrefix(alert.SubjectKey, "spending-category:") || strings.HasPrefix(alert.SubjectKey, "savings-target:"))
}

// inferAlertMuteScope is the scope of a target named without one: an
// address when it has an @, a domain when it reads as one, and otherwise
// a subject key. A burst key has an address in it, and a space or a bar
// too, which no address or domain has.
func inferAlertMuteScope(muteTarget string) models.AlertMuteScope {
	muteTarget = strings.TrimSpace(muteTarget)
	switch {
	case strings.ContainsAny(muteTarget, " \t|"):
		return models.AlertMuteSubjectKey
	case strings.Contains(muteTarget, "@"):
		if strings.HasPrefix(muteTarget, "@") {
			return models.AlertMuteDomain
		}
		return models.AlertMuteSender
	case domainLike.MatchString(muteTarget):
		return models.AlertMuteDomain
	}
	return models.AlertMuteSubjectKey
}

// domainLike is a name with a dot and a top-level name of letters.
var domainLike = regexp.MustCompile(`(?i)^(?:\*\.)?(?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.)+[a-z]{2,}$`)

// MuteAlert keeps a mute for the agent. With an alert, the target may be
// left out and is taken from what the alert covered: by default its
// bursts, or its senders when it was about messages; or the scope asked
// for. Every target of that scope is muted, and the first mute returned.
// Without an alert, the target is the person's own, and its scope, when
// they leave it out, is read from it.
func MuteAlert(tx db.Transaction, agent *models.Agent, alertId string, muteScope models.AlertMuteScope, muteTarget string) (*models.AgentAlertMute, error) {
	alertId = strings.TrimSpace(alertId)
	muteTarget = strings.TrimSpace(muteTarget)
	if muteScope == "" && muteTarget != "" {
		muteScope = inferAlertMuteScope(muteTarget)
	}
	targets := []string{muteTarget}
	if alertId != "" && muteTarget == "" {
		alert, err := tx.GetAgentAlert(agent.ID, alertId)
		if err != nil {
			return nil, err
		}
		if alert == nil {
			return nil, fmt.Errorf("there is no alert %q", alertId)
		}
		terms, err := alertCoveredTerms(tx, alert)
		if err != nil {
			return nil, err
		}
		if muteScope == "" {
			muteScope = defaultAlertMuteScopeOf(alert, terms)
		}
		if !muteScope.IsValid() {
			return nil, fmt.Errorf("%q is not sender, domain, subjectKey, kind or spendingCategory", muteScope)
		}
		if targets = muteTargetsOf(alert, terms, muteScope); len(targets) == 0 {
			return nil, fmt.Errorf("the alert says nothing to mute by %s; the messages it was about may be gone, so mute it by another scope", muteScope)
		}
	}
	var first *models.AgentAlertMute
	for _, target := range targets {
		normalized, err := NormalizeAlertMute(muteScope, target)
		if err != nil {
			return nil, err
		}
		mute, err := tx.CreateAgentAlertMute(&models.AgentAlertMute{AgentID: agent.ID, MuteScope: muteScope, MuteTarget: normalized, AlertID: alertId})
		if err != nil {
			return nil, err
		}
		if first == nil {
			first = mute
		}
	}
	return first, nil
}
