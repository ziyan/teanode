package agent

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/security"
)

// The alert job: the waiting candidates (alert_candidate.go), gathered for
// a couple of minutes, decided on in one call with the person's memory and
// what they were already told, and the few worth it said in the main
// conversation in the agent's own words.
//
// The model decides what is worth saying and says it; the bounds are kept
// here, because a model talked into interrupting somebody by a stranger's
// message is exactly what they are for: a few a day at most, nothing at
// night unless it cannot wait, nothing said twice, and nothing the person
// muted.

// How many a day and when the night is are the person's, on the agent
// (alertBoundsOf in alert_mute.go); nothing is said in their night unless
// it cannot wait, and what waited is said when it ends.
const (
	// alertRepeatWindow is how long an alert's subject key keeps the same
	// thing from being said again, unless it changed.
	alertRepeatWindow = 7 * 24 * time.Hour

	// alertTextCharacters bounds what one alert says.
	alertTextCharacters = 400

	// alertCandidatesAtOnce bounds how many candidates one decision
	// reads; the rest wait for the next.
	alertCandidatesAtOnce = 30

	// alertMessageCharacters bounds how much of each message the decision
	// reads: enough to know what it is, not the whole of a newsletter.
	alertMessageCharacters = 1500

	// What the decision is shown of the person's memory: the pages the
	// senders and subjects find, this many, each cut to this length.
	alertPageCount      = 6
	alertPageCharacters = 500

	// alertSurface is what an alert is said on: the note of the events
	// the dashboard hears it by, and opens the drawer for.
	alertSurface = "alert"

	// alertStaleReason is what a candidate is dropped with when the job
	// comes to it, or to its message, past the freshness bound.
	alertStaleReason = "older than a day when the alert job came to it, and no longer news"

	// alertAfterTurn is how long the job waits when a turn is running in
	// the main conversation, before it looks again: the same minute a
	// goal's job waits.
	alertAfterTurn = time.Minute
)

// AlertDecision is the JSON the model is asked for.
type AlertDecision struct {
	Alerts  []AlertDecided `json:"alerts"`
	Dropped []AlertDropped `json:"dropped"`
}

// AlertDecided is one alert the model would send.
type AlertDecided struct {
	SubjectKey   string   `json:"subject_key"`
	IsUrgent     bool     `json:"is_urgent"`
	CandidateIDs []string `json:"candidate_ids"`
	AlertText    string   `json:"alert_text"`
	HasChanged   bool     `json:"has_changed"`
	ChangeReason string   `json:"change_reason"`
}

// AlertDropped is a candidate the model would not tell, and why.
type AlertDropped struct {
	CandidateID string `json:"candidate_id"`
	DropReason  string `json:"drop_reason"`
}

// AlertInput is everything the decision's prompt is built from, so it can
// be rendered and tested without a database.
type AlertInput struct {
	Owner        *models.User
	Language     string
	Now          time.Time
	Memories     []string
	Pages        []string
	RecentAlerts []*models.AgentAlert
	Candidates   []*labeledCandidate
}

// labeledCandidate is a candidate as the decision sees it: a short label
// the model can copy back, rather than an identifier it would get wrong,
// and what it is about.
type labeledCandidate struct {
	label     string
	candidate *models.AgentAlertCandidate
	message   *MessageContext

	// facts are who it is from and what it is about, as a mute reads them
	// and as the alert that tells it records them.
	facts *alertFacts
}

// AlertPrompt renders the decision's prompt. The mail, and everything the
// sorting wrote from it, goes inside a fence: it is a stranger's text, and
// what the decision writes is read by the person as the agent's word.
func AlertPrompt(input *AlertInput) (string, error) {
	location := Location(input.Owner)
	recent := make([]string, 0, len(input.RecentAlerts))
	for _, alert := range input.RecentAlerts {
		recent = append(recent, fmt.Sprintf("%s [%s] %s", alert.SentAt.In(location).Format("Monday 2 January, 15:04"), alert.SubjectKey, alert.AlertText))
	}
	candidates := make([]string, 0, len(input.Candidates))
	for _, labeled := range input.Candidates {
		candidates = append(candidates, renderAlertCandidate(labeled))
	}
	return render("alert.txt", map[string]any{
		"PersonName":   personName(input.Owner),
		"Language":     input.Language,
		"Now":          input.Now.In(location).Format("Monday 2 January, 15:04"),
		"Memories":     input.Memories,
		"Pages":        input.Pages,
		"RecentAlerts": recent,
		"Candidates":   candidates,
	})
}

// renderAlertCandidate is one candidate in the prompt: its label and kind
// outside the fence, and everything that came from the mail inside it.
func renderAlertCandidate(labeled *labeledCandidate) string {
	candidate := labeled.candidate
	header := ""
	var inside []string
	switch candidate.CandidateKind {
	case models.AlertCandidateBudget:
		// Not mail: numbers the server computed from the person's own
		// finance transactions, and the spending category's name is the
		// person's own. Nothing a stranger wrote, so nothing to fence.
		return fmt.Sprintf("Candidate %s: a budget crossing the server computed after a sync of their finance accounts, not a message.\nWhat the numbers say: %s", labeled.label, candidate.CandidateReason)
	case models.AlertCandidateWatched:
		// Found by a skill's watch: the item as the judgement saw it is on
		// the candidate -- mail, a transaction, a mention -- and is text
		// from outside like any message.
		header = fmt.Sprintf("Candidate %s: something the %s skill's watch (%s) found, which the judgement marked %s.", labeled.label, candidate.WatchedSkillName, candidate.WatchedWatchName, candidate.AlertSignal)
		if candidate.CandidateReason != "" {
			inside = append(inside, "The judgement's note: "+candidate.CandidateReason)
		}
		inside = append(inside, candidate.WatchedItemText)
		return header + "\n" + fenced(strings.Join(inside, "\n\n"))
	case models.AlertCandidateBurst:
		header = fmt.Sprintf("Candidate %s: a burst of %d messages alike in %d hours, counted by the server. The latest of them:", labeled.label, candidate.BurstCount, int(burstWindow.Hours()))
		inside = append(inside, "What the count saw: "+candidate.CandidateReason)
	default:
		header = fmt.Sprintf("Candidate %s: a message the sorting marked %s.", labeled.label, candidate.AlertSignal)
		if candidate.CandidateReason != "" {
			inside = append(inside, "The sorting's note: "+candidate.CandidateReason)
		}
	}
	if labeled.message != nil {
		inside = append(inside, labeled.message.Render())
	} else {
		inside = append(inside, "(the message is no longer stored)")
	}
	return header + "\n" + fenced(strings.Join(inside, "\n\n"))
}

// runAlert is the handler for an alert job.
func (self *Agent) runAlert(ctx context.Context, run *Run) error {
	configuration := run.Configuration()
	// Budget candidates do not come from the sorting, so the switch that
	// stops this job is the person's, not the triage feature's; a mail
	// candidate is still held to the feature by its mailbox's check below.
	if !run.Agent.Active() || !run.Agent.IsAlertsEnabled {
		return nil
	}
	if !self.canThink(configuration) {
		return fmt.Errorf("no way to act as the person")
	}
	now := time.Now()
	location := Location(run.Owner)

	// What waited past the freshness bound, a job that failed or was held
	// back by the budget meanwhile, is not news any more: dropped first,
	// whatever else this run comes to.
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		return tx.DropAgentAlertCandidatesMadeBefore(run.Agent.ID, now.Add(-alertFreshness), alertStaleReason, now)
	}); err != nil {
		return err
	}

	var waiting []*models.AgentAlertCandidate
	var recent []*models.AgentAlert
	var memories []string
	var mutes []*models.AgentAlertMute
	mailsById := map[string]*models.Mail{}
	categoriesByMail := map[string]string{}
	var gone []string
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if err := RequireBudget(tx, configuration, run.Agent, run.Owner, now); err != nil {
			return err
		}
		if waiting, err = tx.ListWaitingAgentAlertCandidates(run.Agent.ID, alertCandidatesAtOnce); err != nil {
			return err
		}
		if recent, err = tx.ListAgentAlertsSince(run.Agent.ID, now.Add(-alertRepeatWindow)); err != nil {
			return err
		}
		if memories, err = memoryLines(tx, run.Agent.ID, models.AudienceTriage, promptRunMemories, false); err != nil {
			return err
		}
		if mutes, err = tx.ListAgentAlertMutes(run.Agent.ID); err != nil {
			return err
		}
		// A candidate whose mailbox is no longer the person's, or no longer
		// alerts, or whose message is gone, is dropped rather than decided.
		isAllowedByMailbox := map[string]bool{}
		mailIds := make([]string, 0, len(waiting))
		mailIdsByMailbox := map[string][]string{}
		for _, candidate := range waiting {
			if candidate.CandidateKind == models.AlertCandidateBudget || candidate.CandidateKind == models.AlertCandidateWatched {
				continue
			}
			isAllowed, isKnown := isAllowedByMailbox[candidate.MailboxID]
			if !isKnown {
				mailbox, err := tx.GetMailbox(candidate.MailboxID)
				if err != nil {
					return err
				}
				isAllowed = mailbox != nil && mailbox.UserID == run.Owner.ID && isAlertingAllowed(configuration, run.Agent, mailbox.Agent)
				isAllowedByMailbox[candidate.MailboxID] = isAllowed
			}
			if !isAllowed {
				gone = append(gone, candidate.ID)
				continue
			}
			mailIds = append(mailIds, candidate.MailID)
			mailIdsByMailbox[candidate.MailboxID] = append(mailIdsByMailbox[candidate.MailboxID], candidate.MailID)
		}
		found, err := tx.GetMails(mailIds, nil)
		if err != nil {
			return err
		}
		for _, mail := range found {
			if mail != nil {
				mailsById[mail.ID] = mail
			}
		}
		// What the sorting called each message, which a mute of a kind
		// is matched against.
		for mailboxId, ids := range mailIdsByMailbox {
			insights, err := tx.GetMailInsights(mailboxId, ids)
			if err != nil {
				return err
			}
			for mailId, insight := range insights {
				if insight != nil {
					categoriesByMail[mailId] = insight.Category
				}
			}
		}
		return nil
	}); err != nil {
		return err
	}

	var labeled []*labeledCandidate
	var mutedIds []string
	var staleIds []string
	for _, candidate := range waiting {
		if candidate.CandidateKind == models.AlertCandidateBudget {
			facts := candidateFacts(candidate, "", "")
			if mutedBy(mutes, facts) != nil {
				mutedIds = append(mutedIds, candidate.ID)
				continue
			}
			labeled = append(labeled, &labeledCandidate{label: fmt.Sprintf("c%d", len(labeled)+1), candidate: candidate, facts: facts})
			continue
		}
		if candidate.CandidateKind == models.AlertCandidateWatched {
			// No mailbox to ask: the sorting is what made it, so it goes
			// when the person's switch or the sorting itself is off.
			if !isAlertingAllowed(configuration, run.Agent, nil) {
				gone = append(gone, candidate.ID)
				continue
			}
			facts := candidateFacts(candidate, "", "")
			if mutedBy(mutes, facts) != nil {
				mutedIds = append(mutedIds, candidate.ID)
				continue
			}
			labeled = append(labeled, &labeledCandidate{label: fmt.Sprintf("c%d", len(labeled)+1), candidate: candidate, facts: facts})
			continue
		}
		mail := mailsById[candidate.MailID]
		if mail == nil {
			if !slices.Contains(gone, candidate.ID) {
				gone = append(gone, candidate.ID)
			}
			continue
		}
		if !mail.ReceivedAt.IsZero() && now.Sub(mail.ReceivedAt) > alertFreshness {
			staleIds = append(staleIds, candidate.ID)
			continue
		}
		// A candidate made before the person muted what it is about.
		from := mail.From
		if from == "" {
			from = mail.Sender
		}
		facts := candidateFacts(candidate, from, categoriesByMail[mail.ID])
		if mutedBy(mutes, facts) != nil {
			mutedIds = append(mutedIds, candidate.ID)
			continue
		}
		message, err := BuildMessageContext(ctx, run.Storage(), mail, alertMessageCharacters, false)
		if err != nil {
			return err
		}
		labeled = append(labeled, &labeledCandidate{label: fmt.Sprintf("c%d", len(labeled)+1), candidate: candidate, message: message, facts: facts})
	}
	if len(gone) > 0 || len(mutedIds) > 0 || len(staleIds) > 0 {
		if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
			if err := tx.DropAgentAlertCandidates(gone, "its message or its mailbox is gone, or the mailbox no longer alerts", now); err != nil {
				return err
			}
			if err := tx.DropAgentAlertCandidates(staleIds, alertStaleReason, now); err != nil {
				return err
			}
			return tx.DropAgentAlertCandidates(mutedIds, alertMutedReason, now)
		}); err != nil {
			return err
		}
	}
	if len(labeled) == 0 {
		return nil
	}
	// Nothing is decided while a turn runs in the main conversation: the
	// alert would be written between the person's words and the answer
	// to them. Waiting before the decision costs nothing.
	main, err := self.alertConversation(ctx, run)
	if err != nil {
		return err
	}
	if self.isTurnRunning(main.ID) {
		return alertBehindTurn(now)
	}

	prompt, err := AlertPrompt(&AlertInput{
		Owner: run.Owner, Language: languageName(KnowledgeLanguage(run.Agent, run.Owner)), Now: now,
		Memories: memories, Pages: self.alertPages(ctx, run, labeled), RecentAlerts: recent, Candidates: labeled,
	})
	if err != nil {
		return err
	}
	thinking, err := self.oneShot(ctx, run, "Deciding what to tell", prompt, models.AgentJobAlert, config.AgentWorkSynthesize)
	if err != nil {
		return err
	}
	decision, err := llm.Extract[AlertDecision](thinking.Text)
	if err != nil {
		return fmt.Errorf("the alert decision did not answer with an object: %w", err)
	}
	bounds := alertBoundsOf(run.Agent, mutes)
	plan := planAlerts(&decision, labeled, recent, now, location, bounds)
	self.retitle(ctx, run, thinking.Conversation, fmt.Sprintf("Alerts: told %d, dropped %d, held %d for the morning", len(plan.sends), len(plan.drops), plan.heldCount))

	if len(plan.drops) > 0 {
		if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
			for _, dropped := range plan.drops {
				if err := tx.DropAgentAlertCandidates([]string{dropped.candidateId}, dropped.dropReason, now); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}
	for _, planned := range plan.sends {
		err := self.deliverAlert(ctx, run, main.ID, planned, now)
		// A turn began while the decision ran. What was not said stays
		// waiting, and is decided again once the turn has ended.
		if errors.Is(err, errTurnRunning) {
			return alertBehindTurn(time.Now())
		}
		if err != nil {
			return err
		}
	}
	return self.alertFollowUp(ctx, run, labeled, plan, now, location, bounds)
}

// alertConversation is the main conversation alerts are said in, made
// when there is none yet.
func (self *Agent) alertConversation(ctx context.Context, run *Run) (*models.AgentConversation, error) {
	var main *models.AgentConversation
	err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		main, err = scheduleConversation(tx, run.Agent.ID, "")
		return err
	})
	return main, err
}

// alertBehindTurn puts the alert job back until a turn running in the
// main conversation has had time to end.
func alertBehindTurn(now time.Time) error {
	return &Deferral{Until: now.Add(alertAfterTurn), Reason: "a turn is running in the main conversation"}
}

// alertFollowUp says when the job runs next: in the morning when an alert
// was held for it, and shortly when candidates wait that this run did not
// read, which a job already running could not be queued again for. Held
// for the morning, only something pressing that arrived after readAt
// brings it forward: what this run read and held, or left unread behind
// a backlog, waits for the morning with it, rather than being read again
// every two minutes all night.
func (self *Agent) alertFollowUp(ctx context.Context, run *Run, labeled []*labeledCandidate, plan *alertPlan, readAt time.Time, location *time.Location, bounds *alertBounds) error {
	now := time.Now()
	var until time.Time
	reason := ""
	if plan.heldCount > 0 {
		until, reason = nextAlertMorning(now, location, bounds), "alerts held for the morning"
	}
	decided := map[string]bool{}
	for _, entry := range labeled {
		decided[entry.candidate.ID] = true
	}
	isLeft, isNewAndPressing := false, false
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		waiting, err := tx.ListWaitingAgentAlertCandidates(run.Agent.ID, alertCandidatesAtOnce*2)
		if err != nil {
			return err
		}
		for _, candidate := range waiting {
			if decided[candidate.ID] {
				continue
			}
			isLeft = true
			isPressing := candidate.AlertSignal == models.AlertSignalNow || candidate.CandidateKind == models.AlertCandidateBurst
			isNewAndPressing = isNewAndPressing || (isPressing && candidate.CreatedAt.After(readAt))
		}
		return nil
	}); err != nil {
		return err
	}
	switch {
	case plan.heldCount > 0 && isNewAndPressing:
		until, reason = now.Add(alertGather), "something pressing arrived while the last decision ran"
	case plan.heldCount == 0 && isLeft:
		until, reason = now.Add(alertGather), "candidates wait that the last decision did not read"
	}
	if until.IsZero() {
		return nil
	}
	return &Deferral{Until: until, Reason: reason}
}

// alertPages is what the person's memory holds about the candidates'
// senders and subjects, found the way a turn's recall finds it and read
// only: whose school it is, which bank, which service they use.
func (self *Agent) alertPages(ctx context.Context, run *Run, labeled []*labeledCandidate) []string {
	var words []string
	for _, entry := range labeled {
		if entry.candidate.CandidateKind == models.AlertCandidateWatched {
			words = append(words, entry.candidate.WatchedSender, entry.candidate.WatchedTitle)
			continue
		}
		if entry.message == nil {
			continue
		}
		words = append(words, entry.message.From, entry.message.Subject)
	}
	question := cutRunes(strings.Join(words, "; "), 600)
	if strings.TrimSpace(question) == "" {
		return nil
	}
	pages, err := self.RecallForQuestion(ctx, run.Agent, run.Owner, question)
	if err != nil {
		log.Debugf("the alert decision of agent %q has no memory to hand: %s", run.Agent.ID, err)
		return nil
	}
	lines := []string{}
	for _, page := range pages {
		if len(lines) == alertPageCount {
			break
		}
		parts := []string{}
		if page.Summary != "" {
			parts = append(parts, page.Summary)
		} else if page.Overview != "" {
			parts = append(parts, page.Overview)
		}
		for _, fact := range page.Facts {
			parts = append(parts, fact.Text)
		}
		if len(parts) == 0 {
			continue
		}
		lines = append(lines, cutRunes(page.Path+": "+strings.Join(parts, " "), alertPageCharacters))
	}
	return lines
}

// plannedAlert is an alert that passed every bound and is to be said.
type plannedAlert struct {
	subjectKey string
	alertText  string
	isUrgent   bool
	candidates []*models.AgentAlertCandidate

	// covered is what the candidates are about, recorded with the alert
	// for a mute taken from it.
	covered *coveredTerms
}

// droppedCandidate is a candidate not to be told, and why.
type droppedCandidate struct {
	candidateId string
	dropReason  string
}

// alertPlan is what a decision comes to once the bounds are kept.
type alertPlan struct {
	sends []*plannedAlert
	drops []droppedCandidate

	// heldCount is how many alerts wait for the morning, their
	// candidates left waiting with them.
	heldCount int
}

// planAlerts keeps the bounds over what the model decided: every candidate
// told at most once, nothing the person muted, the day's most, the night,
// and nothing said twice in a week unless the model says what changed.
// Urgent alerts first, so that the day's last places go to what could not
// wait.
func planAlerts(decision *AlertDecision, labeled []*labeledCandidate, recent []*models.AgentAlert, now time.Time, location *time.Location, bounds *alertBounds) *alertPlan {
	plan := &alertPlan{}
	byLabel := map[string]*labeledCandidate{}
	for _, entry := range labeled {
		byLabel[entry.label] = entry
	}
	decided := map[string]bool{}
	drop := func(candidates []*models.AgentAlertCandidate, dropReason string) {
		for _, candidate := range candidates {
			plan.drops = append(plan.drops, droppedCandidate{candidateId: candidate.ID, dropReason: dropReason})
		}
	}

	local := now.In(location)
	dayStart := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
	sentTodayCount := 0
	lastSaid := map[string]time.Time{}
	for _, alert := range recent {
		if !alert.SentAt.Before(dayStart) {
			sentTodayCount++
		}
		if said, ok := lastSaid[alert.SubjectKey]; !ok || alert.SentAt.After(said) {
			lastSaid[alert.SubjectKey] = alert.SentAt
		}
	}
	isQuiet := isAlertQuietHour(local, bounds)

	alerts := append([]AlertDecided(nil), decision.Alerts...)
	sort.SliceStable(alerts, func(left, right int) bool { return alerts[left].IsUrgent && !alerts[right].IsUrgent })
	for _, decidedAlert := range alerts {
		var covered []*models.AgentAlertCandidate
		var coveredFacts []*alertFacts
		for _, label := range decidedAlert.CandidateIDs {
			entry := byLabel[strings.TrimSpace(label)]
			if entry == nil || decided[entry.candidate.ID] {
				continue
			}
			decided[entry.candidate.ID] = true
			covered = append(covered, entry.candidate)
			coveredFacts = append(coveredFacts, entry.facts)
		}
		if len(covered) == 0 {
			continue
		}
		alertText := cleanAlertText(decidedAlert.AlertText)
		if alertText == "" {
			drop(covered, "the decision had nothing to say about it")
			continue
		}
		// The candidates were matched against the mutes before the model
		// was asked; the model's own key is matched as well, for a mute
		// the person named by a subject in their words.
		subjectKey := alertSubjectKey(decidedAlert.SubjectKey, covered[0])
		if mutedBy(bounds.mutes, &alertFacts{subjectKeys: []string{subjectKey}}) != nil {
			drop(covered, alertMutedReason)
			continue
		}
		if said, ok := lastSaid[subjectKey]; ok && now.Sub(said) < alertRepeatWindow {
			if !decidedAlert.HasChanged || strings.TrimSpace(decidedAlert.ChangeReason) == "" {
				drop(covered, fmt.Sprintf("already told on %s (%s)", said.In(location).Format("2 January"), subjectKey))
				continue
			}
		}
		if isQuiet && !decidedAlert.IsUrgent {
			// Left waiting, with its candidates: the morning's decision
			// sees them again with whatever else arrived overnight.
			plan.heldCount++
			continue
		}
		if sentTodayCount >= bounds.dailyMost {
			drop(covered, fmt.Sprintf("the day's %d alerts had been said", bounds.dailyMost))
			continue
		}
		sentTodayCount++
		lastSaid[subjectKey] = now
		plan.sends = append(plan.sends, &plannedAlert{
			subjectKey: subjectKey, alertText: alertText, isUrgent: decidedAlert.IsUrgent, candidates: covered, covered: coveredTermsOf(coveredFacts),
		})
	}
	for _, dropped := range decision.Dropped {
		entry := byLabel[strings.TrimSpace(dropped.CandidateID)]
		if entry == nil || decided[entry.candidate.ID] {
			continue
		}
		candidate := entry.candidate
		decided[candidate.ID] = true
		dropReason := strings.TrimSpace(dropped.DropReason)
		if dropReason == "" {
			dropReason = "not worth telling"
		}
		drop([]*models.AgentAlertCandidate{candidate}, cutRunes(dropReason, 300))
	}
	for _, entry := range labeled {
		if !decided[entry.candidate.ID] {
			drop([]*models.AgentAlertCandidate{entry.candidate}, "the decision did not mention it")
		}
	}
	return plan
}

// isAlertQuietHour says whether a moment, in the person's zone, is their
// night. A night that starts when it ends is no night at all.
func isAlertQuietHour(local time.Time, bounds *alertBounds) bool {
	minute := local.Hour()*60 + local.Minute()
	start, end := bounds.quietStartMinute, bounds.quietEndMinute
	switch {
	case start == end:
		return false
	case start < end:
		return minute >= start && minute < end
	default:
		return minute >= start || minute < end
	}
}

// nextAlertMorning is when the night after now ends, in the person's zone.
func nextAlertMorning(now time.Time, location *time.Location, bounds *alertBounds) time.Time {
	local := now.In(location)
	morning := time.Date(local.Year(), local.Month(), local.Day(), bounds.quietEndMinute/60, bounds.quietEndMinute%60, 0, 0, location)
	if !local.Before(morning) {
		morning = morning.AddDate(0, 0, 1)
	}
	return morning
}

var (
	// markdownLink is a link written in Markdown, whose words are kept.
	markdownLink = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	// webAddress is a web address written out with its scheme or www,
	// without the punctuation that ends the sentence after it.
	webAddress = regexp.MustCompile(`(?i)\b(?:https?://|www\.)\S*[^\s.,;:!?)\]'"]`)
	// emailAddress is an address somebody could be written to.
	emailAddress = regexp.MustCompile(`(?i)\b[a-z0-9._%+-]+@(?:[a-z0-9-]+\.)+[a-z]{2,}\b`)
	// hostName is a name a browser would go to, with or without a path:
	// photos.example.com/reset, or a shortener's bit.ly/abc. A top-level
	// name is letters, so a decimal or a version is not one.
	hostName = regexp.MustCompile(`(?i)\b(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,24}\b(?:/\S*[^\s.,;:!?)\]'"])?/?`)
	// phoneLike is what might be a telephone number; phoneNumber decides.
	phoneLike = regexp.MustCompile(`\+?\(?\d[\d ().-]{5,}\d`)
	// calendarDate is a date written with digits, which is not a number to
	// call.
	calendarDate = regexp.MustCompile(`^(?:\d{4}-\d{1,2}-\d{1,2}|\d{1,2}[./-]\d{1,2}[./-]\d{2,4})$`)
	// extraSpace is what taking an address out leaves behind.
	extraSpace = regexp.MustCompile(`[ \t]{2,}`)
)

// What an address, a host, an email address or a telephone number in an
// alert is replaced with: what it was, not where it pointed.
const (
	alertLinkRemoved    = "(link removed)"
	alertAddressRemoved = "(address removed)"
	alertNumberRemoved  = "(number removed)"
)

// cleanAlertText is what the model wrote with anything that leads
// somewhere taken out, and cut to length: web addresses, host names with
// or without a path (a shortener's included), email addresses and
// telephone numbers. A service named without its domain is left alone.
// The prompt asks for none of them; this is the same rule where a model
// cannot be talked out of it, because an alert is read as the agent's
// word, and a link, an address or a number to call in it that came from a
// phishing message is the phishing message delivered by the agent.
func cleanAlertText(alertText string) string {
	alertText = markdownLink.ReplaceAllString(alertText, "$1")
	alertText = webAddress.ReplaceAllString(alertText, alertLinkRemoved)
	alertText = emailAddress.ReplaceAllString(alertText, alertAddressRemoved)
	alertText = hostName.ReplaceAllString(alertText, alertLinkRemoved)
	alertText = phoneLike.ReplaceAllStringFunc(alertText, func(found string) string {
		if isPhoneNumber(found) {
			return alertNumberRemoved
		}
		return found
	})
	alertText = extraSpace.ReplaceAllString(alertText, " ")
	return cutRunes(strings.TrimSpace(alertText), alertTextCharacters)
}

// isPhoneNumber says whether digits written with spaces, dots, dashes or
// brackets read as a telephone number: seven to fifteen digits, not a
// date, and either written with a plus or a separator or ten digits long,
// so that an order number or a year in a sentence is left alone.
func isPhoneNumber(found string) bool {
	digitCount := 0
	for _, character := range found {
		if character >= '0' && character <= '9' {
			digitCount++
		}
	}
	if digitCount < 7 || digitCount > 15 || calendarDate.MatchString(found) {
		return false
	}
	return strings.HasPrefix(found, "+") || strings.ContainsAny(found, " ().-") || digitCount >= 10
}

// alertSubjectKey is the model's key in one form, or, when it gave none,
// one made from what the first candidate is about.
func alertSubjectKey(subjectKey string, candidate *models.AgentAlertCandidate) string {
	// A budget crossing's key is its subject whatever the model called
	// it: it is what keeps the same crossing from being written, and
	// told, a second time.
	if candidate.CandidateKind == models.AlertCandidateBudget && candidate.BudgetKey != "" {
		return candidate.BudgetKey
	}
	subjectKey = normalizedSubjectKey(subjectKey)
	if subjectKey == "" {
		if candidate.BurstKey != "" {
			return candidate.BurstKey
		}
		if candidate.CandidateKind == models.AlertCandidateWatched {
			return candidate.WatchedSkillName + " " + candidate.WatchedWatchName + " " + candidate.WatchedItemID
		}
		return "mail " + candidate.MailID
	}
	return cutRunes(subjectKey, 120)
}

// alertCheckIn is the line an alert is written under in the conversation:
// the marker, and what the agent is told when it reads the conversation
// back, so that an answer to it is understood as one. The alert's id is in
// it, so that "not these" said about an earlier alert mutes that one.
func alertCheckIn(owner *models.User, alertId string) string {
	return models.AlertMarker + " Nobody asked for this: " + personName(owner) + "'s mail showed something they should know, and you told them, unasked (alert " + alertId + "). What you said follows. If they answer, it is about this; if they say they do not want to hear about things like it, mute it with agent_profile's mute_alert and alert_id " + alertId + ", and if they want no alerts at all, no_alerts."
}

// deliverAlert says one alert in the main conversation and records it, in
// one transaction: the candidates are taken first, and an alert whose
// candidates another run already told is not said again. Then the drawer
// and every other listener hear it as a turn's events would be heard.
//
// Written only while no turn runs in the conversation, and under the lock
// Ask starts turns with, so no turn can start between the look and the
// commit; errTurnRunning when one is running.
func (self *Agent) deliverAlert(ctx context.Context, run *Run, conversationId string, planned *plannedAlert, now time.Time) error {
	var alert *models.AgentAlert
	alertId := security.NewULID()
	checkIn := alertCheckIn(run.Owner, alertId)
	isWritten, err := self.whileNoTurnRuns(conversationId, func() error {
		return run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
			candidateIds := make([]string, 0, len(planned.candidates))
			for _, candidate := range planned.candidates {
				candidateIds = append(candidateIds, candidate.ID)
			}
			taken, err := tx.LockWaitingAgentAlertCandidates(run.Agent.ID, candidateIds)
			if err != nil || len(taken) == 0 {
				return err
			}
			candidateIds = candidateIds[:0]
			for _, candidate := range taken {
				candidateIds = append(candidateIds, candidate.ID)
			}
			conversation, err := scheduleConversation(tx, run.Agent.ID, conversationId)
			if err != nil {
				return err
			}
			if _, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: conversation.ID, Role: "user", Content: checkIn}); err != nil {
				return err
			}
			said, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: conversation.ID, Role: "assistant", Content: planned.alertText})
			if err != nil {
				return err
			}
			covered := planned.covered
			if covered == nil {
				covered = &coveredTerms{}
			}
			if alert, err = tx.CreateAgentAlert(&models.AgentAlert{
				ID: alertId, AgentID: run.Agent.ID, SubjectKey: planned.subjectKey, AlertText: planned.alertText, IsUrgent: planned.isUrgent,
				CandidateIDs: candidateIds, ConversationID: conversation.ID, MessageID: said.ID, SentAt: now,
				CoveredBurstKeys: covered.burstKeys, CoveredSenderAddresses: covered.senderAddresses,
				CoveredSenderDomains: covered.senderDomains, CoveredMailCategories: covered.mailCategories,
			}); err != nil {
				return err
			}
			return tx.MarkAgentAlertCandidatesAlerted(candidateIds, alert.ID)
		})
	})
	if err != nil {
		return err
	}
	if !isWritten {
		return errTurnRunning
	}
	if alert == nil {
		return nil
	}
	log.Noticef("agent %q told its person about %q unasked", run.Agent.ID, alert.SubjectKey)
	// The alert is the run the events belong to: a turn in the drawer's
	// eyes, begun, answered and done, on the surface it opens for.
	at := time.Now()
	for sequence, event := range []Event{
		{Kind: EventAsked, Text: checkIn, Note: alertSurface},
		{Kind: EventMessage, Text: alert.AlertText},
		{Kind: EventDone},
	} {
		event.RunID, event.ConversationID, event.Sequence, event.At = alert.ID, alert.ConversationID, sequence, at
		self.publish(event, true)
	}
	return nil
}
