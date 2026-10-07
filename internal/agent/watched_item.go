package agent

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/skills"
)

// Watches: what a skill says is worth watching, looked at with the skill's
// own tools so that the agent can tell the person about what arrives
// without being asked (skills/watch.go has the contract). A look lists
// what arrived since shortly before the newest item already looked at,
// reads each new item, judges it -- mail with the prompt the person's own
// mail is sorted with, anything else with the watch's own guidance -- and
// makes an alert candidate of what the judgement says they should hear
// about. Nothing else follows: the rules, replies and lookups that follow
// sorting act on mail this server holds.
//
// The look is code, not a turn: it runs only the list and read tools the
// skill names for the watch, and finds the computer itself, since there is
// no turn to ask. docs/planning/skill-watches-execplan.md is the design.

const (
	// watchQueueEvery is how often the tick asks whether a watch is due.
	watchQueueEvery = time.Minute

	// watchItemsAtOnce bounds the items one look judges. The oldest go
	// first, so what is left is found by the next look.
	watchItemsAtOnce = 25

	// watchedItemCharacters is how much of an item a candidate keeps for
	// the alert job to read.
	watchedItemCharacters = 4000

	// watchedItemsKept is how long the record of an item looked at is kept.
	watchedItemsKept = 30 * 24 * time.Hour
)

// watchSubject is a watch job's subject: the skill and the watch.
func watchSubject(skillName, watchName string) string {
	return skillName + "/" + watchName
}

// watchSince is where a look starts: the overlap before the newest item
// already looked at, or before now on a first look.
func watchSince(latest *time.Time, now time.Time, overlap time.Duration) time.Time {
	if latest == nil || latest.After(now) {
		return now.Add(-overlap)
	}
	return latest.Add(-overlap)
}

// watchedItemTimeLayouts are the forms an item's moment is read in.
var watchedItemTimeLayouts = []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02T15:04:05", time.DateOnly}

// watchedItemAt is when an item happened, in any of the usual forms, as
// seconds or milliseconds since 1970 too; the zero time when it does not
// say.
func watchedItemAt(written string) time.Time {
	written = strings.TrimSpace(written)
	if written == "" {
		return time.Time{}
	}
	if number, err := strconv.ParseInt(written, 10, 64); err == nil {
		if number > 1e12 {
			return time.UnixMilli(number)
		}
		return time.Unix(number, 0)
	}
	for _, layout := range watchedItemTimeLayouts {
		if at, err := time.Parse(layout, written); err == nil {
			return at
		}
	}
	return time.Time{}
}

// installedWatches are the watches of every installed, enabled skill that
// parses, by skill.
func installedWatches(tx db.Transaction) ([]*skills.Skill, error) {
	installed, err := tx.ListAgentSkills()
	if err != nil {
		return nil, err
	}
	var watching []*skills.Skill
	for _, row := range installed {
		if !row.Enabled {
			continue
		}
		skill, err := skills.Parse([]byte(row.Content))
		if err != nil || len(skill.Watches) == 0 {
			continue
		}
		watching = append(watching, skill)
	}
	return watching, nil
}

// watchRunsCommands says the watch runs a command on the person's
// computer, which it can do only while one is attached.
func watchRunsCommands(skill *skills.Skill, watch *skills.Watch) bool {
	if SkillRunsCommands(skill.Tool(watch.List.Tool)) {
		return true
	}
	return watch.Read != nil && SkillRunsCommands(skill.Tool(watch.Read.Tool))
}

// watchNeedsSecrets says a tool the watch runs needs a secret, which a
// look has nobody to fill in for: such a watch is not run.
func watchNeedsSecrets(skill *skills.Skill, watch *skills.Watch) bool {
	if len(skill.SecretsFor(watch.List.Tool)) > 0 {
		return true
	}
	return watch.Read != nil && len(skill.SecretsFor(watch.Read.Tool)) > 0
}

// queueWatching queues a look for each watch of each installed skill that
// is due, for each person it can run for: they want alerts, and for a
// watch that runs commands, a computer of theirs is attached.
func (self *Agent) queueWatching(ctx context.Context, now time.Time) {
	if now.Sub(self.lastWatch) < watchQueueEvery {
		return
	}
	configuration := self.settings.Configuration()
	if !FeatureAllowed(configuration, "skills") || !FeatureAllowed(configuration, "triage") || !self.canThink(configuration) {
		return
	}
	self.lastWatch = now
	var watching []*skills.Skill
	var agents []*models.Agent
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if watching, err = installedWatches(tx); err != nil || len(watching) == 0 {
			return err
		}
		agents, err = tx.ListAgents(nil)
		return err
	}); err != nil {
		log.Warningf("cannot say what to watch: %s", err)
		return
	}
	for _, agent := range agents {
		if !isAlertingAllowed(configuration, agent, nil) {
			continue
		}
		for _, skill := range watching {
			for _, watch := range skill.Watches {
				if watchNeedsSecrets(skill, watch) {
					continue
				}
				if watchRunsCommands(skill, watch) && self.watchComputer(ctx, agent.ID, skill.Name) == nil {
					continue
				}
				key := agent.ID + "|" + watchSubject(skill.Name, watch.Name)
				if looked, ok := self.watchLookedAt(key); ok && now.Sub(looked) < watch.EveryDuration() {
					continue
				}
				self.markWatchLookedAt(key, now)
				if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
					_, err := self.Enqueue(tx, models.AgentJobWatch, agent.ID, "", watchSubject(skill.Name, watch.Name))
					return err
				}); err != nil {
					log.Warningf("cannot queue a look by %s for agent %q: %s", watchSubject(skill.Name, watch.Name), agent.ID, err)
				}
			}
		}
	}
}

// watchLookedAt is when a look was last queued for a person's watch, and
// markWatchLookedAt says it was now.
func (self *Agent) watchLookedAt(key string) (time.Time, bool) {
	self.watchMutex.Lock()
	defer self.watchMutex.Unlock()
	looked, ok := self.watchesQueuedAt[key]
	return looked, ok
}

func (self *Agent) markWatchLookedAt(key string, now time.Time) {
	self.watchMutex.Lock()
	defer self.watchMutex.Unlock()
	if self.watchesQueuedAt == nil {
		self.watchesQueuedAt = map[string]time.Time{}
	}
	self.watchesQueuedAt[key] = now
}

// watchComputer is the computer a look runs the skill on: the one the
// person chose for the skill, or else the only one attached. Nil when
// neither is attached.
func (self *Agent) watchComputer(ctx context.Context, agentId, skillName string) *attachedComputer {
	computers := self.computersFor(agentId)
	chosen := self.reachOf(ctx, agentId, models.AgentReachSkill, skillName)
	for _, computer := range computers {
		if chosen != "" && strings.EqualFold(computer.name, chosen) {
			return computer
		}
	}
	if chosen == "" && len(computers) == 1 {
		return computers[0]
	}
	return nil
}

// runWatch is the handler for a watch job: one look.
func (self *Agent) runWatch(ctx context.Context, run *Run) error {
	configuration := run.Configuration()
	skillName, watchName, _ := strings.Cut(run.Job.SubjectID, "/")
	if !FeatureAllowed(configuration, "skills") || !isAlertingAllowed(configuration, run.Agent, nil) {
		return nil
	}
	if !self.canThink(configuration) {
		return fmt.Errorf("no way to act as the person")
	}
	now := time.Now()
	var skill *skills.Skill
	var latest *time.Time
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		if err := RequireBudget(tx, configuration, run.Agent, run.Owner, now); err != nil {
			return err
		}
		watching, err := installedWatches(tx)
		if err != nil {
			return err
		}
		for _, candidate := range watching {
			if candidate.Name == skillName {
				skill = candidate
			}
		}
		if skill == nil || skill.Watch(watchName) == nil {
			skill = nil
			return nil
		}
		if err := tx.DeleteAgentWatchedItemsBefore(run.Agent.ID, skillName, watchName, now.Add(-watchedItemsKept)); err != nil {
			return err
		}
		latest, err = tx.LatestAgentWatchedItemAt(run.Agent.ID, skillName, watchName)
		return err
	}); err != nil {
		return err
	}
	if skill == nil {
		return nil // uninstalled, switched off, or no longer watching this
	}
	watch := skill.Watch(watchName)
	if watchNeedsSecrets(skill, watch) {
		return nil
	}
	running := &skills.Running{}
	where := "this server"
	if watchRunsCommands(skill, watch) {
		computer := self.watchComputer(ctx, run.Agent.ID, skill.Name)
		if computer == nil {
			return nil // the next look, once one is attached
		}
		running.Shell = &computerShell{attached: computer}
		where = computer.name
	}
	isFirstLook := latest == nil
	since := watchSince(latest, now, watch.OverlapDuration())

	listed, err := skill.Run(ctx, watch.List.Tool, skill.ListArguments(watch, since), running)
	if err != nil {
		return fmt.Errorf("%s on %s: %w", watchSubject(skill.Name, watch.Name), where, err)
	}
	items, err := skills.ParseWatchedItems(listed)
	if err != nil {
		return fmt.Errorf("%s on %s: %w", watchSubject(skill.Name, watch.Name), where, err)
	}
	if len(items) == 0 {
		return nil
	}
	var looked map[string]bool
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		ids := make([]string, 0, len(items))
		for _, item := range items {
			ids = append(ids, item.ID)
		}
		looked, err = tx.ListAgentWatchedItemsLooked(run.Agent.ID, skill.Name, watch.Name, ids)
		return err
	}); err != nil {
		return err
	}
	var fresh []*skills.WatchedItem
	for _, item := range items {
		key := db.WatchedItemKey(item.ID, item.Version)
		if !looked[key] {
			looked[key] = true
			fresh = append(fresh, item)
		}
	}
	// The oldest first, so that a look cut short leaves the newest for the
	// next one; items that do not say when are taken as they came.
	slices.SortStableFunc(fresh, func(left, right *skills.WatchedItem) int {
		return watchedItemAt(left.At).Compare(watchedItemAt(right.At))
	})
	if isFirstLook {
		// What is already there when a watch starts is not news: noted,
		// so that the next look starts from it, and not judged.
		return run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
			for _, item := range fresh {
				if err := tx.AddAgentWatchedItem(watchedItemRecord(run.Agent.ID, skill, watch, item, now)); err != nil {
					return err
				}
			}
			return nil
		})
	}
	if len(fresh) > watchItemsAtOnce {
		fresh = fresh[:watchItemsAtOnce]
	}
	for _, item := range fresh {
		if err := self.watchOne(ctx, run, skill, watch, running, item, now); err != nil {
			return err
		}
	}
	return nil
}

// watchedItemRecord is the record that an item was looked at.
func watchedItemRecord(agentId string, skill *skills.Skill, watch *skills.Watch, item *skills.WatchedItem, now time.Time) *models.AgentWatchedItem {
	at := watchedItemAt(item.At)
	if at.IsZero() || at.After(now) {
		at = now
	}
	return &models.AgentWatchedItem{
		AgentID: agentId, SkillName: skill.Name, WatchName: watch.Name,
		WatchedItemID: item.ID, WatchedItemVersion: item.Version, WatchedItemAt: at, LookedAt: now,
	}
}

// watchedItemAnswer is the JSON an item's judgement is asked for.
type watchedItemAnswer struct {
	AlertSignal string `json:"alert_signal"`
	AlertReason string `json:"alert_reason"`
	Summary     string `json:"summary"`
}

// watchedJudgement is what judging an item came to.
type watchedJudgement struct {
	alertSignal     string
	alertReason     string
	summary         string
	watchedCategory string
}

// watchOne reads one new item, judges it, and records that it was looked
// at, with the candidate the judgement makes of it, in one transaction.
func (self *Agent) watchOne(ctx context.Context, run *Run, skill *skills.Skill, watch *skills.Watch, running *skills.Running, item *skills.WatchedItem, now time.Time) error {
	configuration := run.Configuration()
	watched := watchedItemRecord(run.Agent.ID, skill, watch, item, now)
	content := strings.TrimSpace(item.Text)
	if watch.Read != nil {
		read, err := skill.Run(ctx, watch.Read.Tool, skill.ReadArguments(watch, item), running)
		var text string
		if err == nil {
			text, _ = read["text"].(string)
			text = strings.TrimSpace(text)
			if strings.HasPrefix(text, "[ended ") {
				err = fmt.Errorf("%s", cutRunes(text, 300))
			}
		}
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// An item that cannot be read, gone since it was listed or too
			// large to print, is recorded as looked at and left: failing the
			// look would fail every look after it on the same item.
			log.Warningf("%s could not read an item of agent %q: %s", watchSubject(skill.Name, watch.Name), run.Agent.ID, err)
			return run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
				return tx.AddAgentWatchedItem(watched)
			})
		}
		if content != "" {
			// What the list said of it, before what the read says.
			text = "What the list said: " + content + "\n\n" + text
		}
		content = text
	}
	maximumCharacters := configuration.Agent.Limits.MaxBodyCharacters
	if maximumCharacters > 0 && len([]rune(content)) > maximumCharacters {
		content = cutRunes(content, maximumCharacters) + "\n[cut here: it goes on]"
	}

	var memories, corrections []string
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if err := RequireBudget(tx, configuration, run.Agent, run.Owner, time.Now()); err != nil {
			return err
		}
		if memories, err = memoryLines(tx, run.Agent.ID, models.AudienceTriage, promptRunMemories, false); err != nil {
			return err
		}
		if watch.Kind == skills.WatchKindMail {
			corrections, err = correctionLines(tx, run.Agent.ID, []models.AgentFeedbackKind{models.FeedbackFiled, models.FeedbackSorted})
		}
		return err
	}); err != nil {
		return err
	}
	rendered := renderWatchedItem(skill, item, content)
	judgement, err := self.judgeWatchedItem(ctx, run, skill, watch, item, content, rendered, memories, corrections)
	if err != nil {
		return err
	}
	watched.AlertSignal = judgement.alertSignal
	return run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		if err := tx.AddAgentWatchedItem(watched); err != nil {
			return err
		}
		return noteWatchedCandidate(tx, run.Agent.ID, skill, watch, item, rendered, judgement, now)
	})
}

// renderWatchedItem is an item as a judgement and the alert job read it.
func renderWatchedItem(skill *skills.Skill, item *skills.WatchedItem, content string) string {
	var builder strings.Builder
	for _, line := range [][2]string{{"From", item.From}, {"Title", item.Title}, {"When", item.At}, {"Where", item.URL}} {
		if strings.TrimSpace(line[1]) != "" {
			builder.WriteString(line[0] + ": " + strings.TrimSpace(line[1]) + "\n")
		}
	}
	builder.WriteString("Found by: the " + skill.Name + " skill\n\n")
	if content == "" {
		builder.WriteString("(nothing more)")
	} else {
		builder.WriteString(content)
	}
	return builder.String()
}

// judgeWatchedItem asks the model whether the person should hear about
// the item: as mail, with the sorting's prompt, or as an item, with the
// watch's guidance.
func (self *Agent) judgeWatchedItem(ctx context.Context, run *Run, skill *skills.Skill, watch *skills.Watch, item *skills.WatchedItem, content, rendered string, memories, corrections []string) (*watchedJudgement, error) {
	title := fmt.Sprintf("Watching %s: %q", skill.Name, cutRunes(firstNonEmpty(item.Title, item.From, item.ID), 80))
	if watch.Kind == skills.WatchKindMail {
		messages, err := TriagePrompt(&TriageInput{
			Configuration: run.Configuration(), Agent: run.Agent, Owner: run.Owner, Mailbox: &models.Mailbox{Name: skill.Name},
			Message: &MessageContext{
				From: item.From, Subject: item.Title, Date: item.At, Text: content,
				Facts: []string{"read from the person's mailbox through the " + skill.Name + " skill; this server did not receive it, so it has no authentication results of its own"},
			},
			Memories: memories, Corrections: corrections,
		})
		if err != nil {
			return nil, err
		}
		thinking, err := self.oneShot(ctx, run, title, messages[1].Content, models.AgentJobWatch, config.AgentWorkTriage)
		if err != nil {
			return nil, err
		}
		answer, err := llm.Extract[TriageAnswer](thinking.Text)
		if err != nil {
			return nil, fmt.Errorf("the judgement of %s did not answer with an object: %w", watchSubject(skill.Name, watch.Name), err)
		}
		insight, err := InterpretTriage(&answer, run.Agent)
		if err != nil {
			return nil, err
		}
		self.retitle(ctx, run, thinking.Conversation, fmt.Sprintf("%s: %s, alert %s. %s", title, insight.Category, insight.AlertSignal, insight.Summary))
		return &watchedJudgement{alertSignal: insight.AlertSignal, alertReason: insight.AlertReason, summary: insight.Summary, watchedCategory: insight.Category}, nil
	}
	prompt, err := render("watched_item.txt", map[string]any{
		"PersonName": personName(run.Owner), "SkillName": skill.Name, "WatchDescription": watch.Description,
		"Guidance": strings.TrimSpace(watch.Guidance), "Language": languageName(KnowledgeLanguage(run.Agent, run.Owner)),
		"Memories": memories, "Item": rendered,
	})
	if err != nil {
		return nil, err
	}
	thinking, err := self.oneShot(ctx, run, title, prompt, models.AgentJobWatch, config.AgentWorkTriage)
	if err != nil {
		return nil, err
	}
	answer, err := llm.Extract[watchedItemAnswer](thinking.Text)
	if err != nil {
		return nil, fmt.Errorf("the judgement of %s did not answer with an object: %w", watchSubject(skill.Name, watch.Name), err)
	}
	judgement := &watchedJudgement{alertSignal: models.AlertSignalNone, summary: cutRunes(strings.TrimSpace(answer.Summary), 200), watchedCategory: watch.Name}
	switch alertSignal := strings.ToLower(strings.TrimSpace(answer.AlertSignal)); alertSignal {
	case models.AlertSignalSoon, models.AlertSignalNow:
		judgement.alertSignal = alertSignal
		judgement.alertReason = cutRunes(strings.TrimSpace(answer.AlertReason), 200)
	}
	self.retitle(ctx, run, thinking.Conversation, fmt.Sprintf("%s: alert %s. %s", title, judgement.alertSignal, judgement.summary))
	return judgement, nil
}

// firstNonEmpty is the first of the values with something in it.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// noteWatchedCandidate makes the candidate an item's judgement gives rise
// to, if any, and queues the job that decides on it. An item the person
// muted the sender, the domain or the kind of is written down and dropped
// at once, as hosted mail's is.
func noteWatchedCandidate(tx db.Transaction, agentId string, skill *skills.Skill, watch *skills.Watch, item *skills.WatchedItem, rendered string, judgement *watchedJudgement, now time.Time) error {
	if judgement.alertSignal != models.AlertSignalSoon && judgement.alertSignal != models.AlertSignalNow {
		return nil
	}
	// Older than the look reaches back to is not news, though it is new
	// here: an item that turned up late is news for as long as the
	// overlap that found it.
	if at := watchedItemAt(item.At); !at.IsZero() && now.Sub(at) > alertFreshness+watch.OverlapDuration() {
		return nil
	}
	happenedAt := now
	if at := watchedItemAt(item.At); !at.IsZero() && at.Before(now) {
		happenedAt = at
	}
	created, err := tx.CreateAgentAlertCandidate(&models.AgentAlertCandidate{
		AgentID: agentId, CandidateKind: models.AlertCandidateWatched,
		AlertSignal: judgement.alertSignal, CandidateReason: judgement.alertReason,
		WatchedSkillName: skill.Name, WatchedWatchName: watch.Name, WatchedItemID: item.ID,
		WatchedSender: item.From, WatchedTitle: item.Title, WatchedCategory: judgement.watchedCategory,
		WatchedItemAt: &happenedAt, WatchedItemText: cutRunes(rendered, watchedItemCharacters), WatchedItemURL: item.URL,
	})
	if err != nil {
		return err
	}
	mutes, err := tx.ListAgentAlertMutes(agentId)
	if err != nil {
		return err
	}
	if mutedBy(mutes, candidateFacts(created, "", "")) != nil {
		return tx.DropAgentAlertCandidates([]string{created.ID}, alertMutedReason, now)
	}
	return queueAlert(tx, agentId, judgement.alertSignal == models.AlertSignalNow, now)
}
