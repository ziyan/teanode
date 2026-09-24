package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// A tip is one thing the agent can do that the person has not tried, told
// while they have the dashboard open and have gone quiet. None is told
// twice, and none about something they already use.

// tipIdle is how long the person has to have been still before a tip: a
// tip is for a pause, never for somebody in the middle of something.
const tipIdle = 5 * time.Minute

// Tip is one entry of the catalog.
type Tip struct {
	// TipKey names it in agent_tip, and never changes.
	TipKey string

	// Feature is what it is and what it is good for, for the agent to put
	// in its own words.
	Feature string

	// Where is how the person gets to it: a page of the dashboard, or what
	// to say to the agent.
	Where string

	// IsUsed says whether the person uses it already.
	IsUsed func(tx db.Transaction, agent *models.Agent, owner *models.User) (bool, error)
}

// tipCatalog is every tip, most useful first.
var tipCatalog = []*Tip{
	{
		TipKey:  "mailbox_source",
		Feature: "Letting the agent read a mailbox: it sorts what arrives, summarizes the long threads, drafts replies for them to approve, and answers questions about their mail.",
		Where:   "[Mail](/settings/agent/mail) in the agent's settings",
		IsUsed: func(tx db.Transaction, agent *models.Agent, owner *models.User) (bool, error) {
			mailboxes, err := tx.ListMailboxes(owner.ID)
			if err != nil {
				return false, err
			}
			for _, mailbox := range mailboxes {
				if mailbox.Agent != nil && mailbox.Agent.Granted {
					return true, nil
				}
			}
			return false, nil
		},
	},
	{
		TipKey:  "schedule",
		Feature: "Schedules: a standing request the agent carries out at a set time, such as a morning summary of what needs them, answered here or mailed.",
		Where:   "asking the agent (\"every weekday at eight, tell me what needs me\"), or [Schedules](/settings/agent/schedules)",
		IsUsed: func(tx db.Transaction, agent *models.Agent, owner *models.User) (bool, error) {
			schedules, err := tx.ListAgentSchedules(agent.ID)
			return len(schedules) > 0, err
		},
	},
	{
		TipKey:  "knowledge_source",
		Feature: "Sources beyond mail: a folder on their computer, a code repository, a chat archive or a wiki, which the agent reads at night and remembers.",
		Where:   "[Sources](/settings/agent/sources) in the agent's settings",
		IsUsed: func(tx db.Transaction, agent *models.Agent, owner *models.User) (bool, error) {
			sources, err := tx.ListAgentSources(agent.ID)
			return len(sources) > 0, err
		},
	},
	{
		TipKey:  "goal",
		Feature: "Goals: something the agent keeps working at on its own, turn after turn, until it is done, such as following up until an answer arrives.",
		Where:   "the target button above the box they type in, or asking the agent to keep at something",
		IsUsed: func(tx db.Transaction, agent *models.Agent, owner *models.User) (bool, error) {
			conversations, err := tx.ListAgentConversations(agent.ID, []models.AgentConversationKind{models.AgentConversationMain, models.AgentConversationNamed}, &db.Options{Limit: 200})
			if err != nil {
				return false, err
			}
			for _, conversation := range conversations {
				if conversation.GoalSetAt != nil {
					return true, nil
				}
			}
			return false, nil
		},
	},
	{
		TipKey:  "named_conversation",
		Feature: "Separate conversations: one per topic, each with its own history, so a long piece of work does not crowd out everything else.",
		Where:   "the plus button at the top of the chat",
		IsUsed: func(tx db.Transaction, agent *models.Agent, owner *models.User) (bool, error) {
			conversations, err := tx.ListAgentConversations(agent.ID, []models.AgentConversationKind{models.AgentConversationNamed}, &db.Options{Limit: 1})
			return len(conversations) > 0, err
		},
	},
	{
		TipKey:  "memory_edit",
		Feature: "Their memory pages: everything the agent remembers about them, which they can read, correct and prune, so the agent stops repeating a mistake.",
		Where:   "[Memory](/settings/agent/memory) in the agent's settings",
		IsUsed: func(tx db.Transaction, agent *models.Agent, owner *models.User) (bool, error) {
			revisions, err := tx.ListAgentRevisionsSince(agent.ID, time.Time{}, 500)
			if err != nil {
				return false, err
			}
			for _, revision := range revisions {
				if revision.Actor == models.ActorPerson {
					return true, nil
				}
			}
			return false, nil
		},
	},
	{
		TipKey:  "chat_channel",
		Feature: "Talking to the agent from a chat app on their phone, with the same memory and tools as here.",
		Where:   "[Connections](/settings/agent/connections) in the agent's settings",
		IsUsed: func(tx db.Transaction, agent *models.Agent, owner *models.User) (bool, error) {
			channels, err := tx.ListAgentChannels(agent.ID)
			return len(channels) > 0, err
		},
	},
	{
		TipKey:  "command_line",
		Feature: "The teanode command line: everything the dashboard does, from a terminal, including talking to the agent with teanode agent ask.",
		Where:   "installing the teanode program and signing in with teanode auth login",
		IsUsed: func(tx db.Transaction, agent *models.Agent, owner *models.User) (bool, error) {
			conversations, err := tx.ListAgentConversations(agent.ID, []models.AgentConversationKind{models.AgentConversationMain, models.AgentConversationNamed}, &db.Options{Limit: 200})
			if err != nil {
				return false, err
			}
			for _, conversation := range conversations {
				if conversation.Surface == "cli" {
					return true, nil
				}
			}
			return false, nil
		},
	},
}

// tipsToGive is the catalog's tips the person does not use and has not
// been given, in the catalog's order.
func tipsToGive(tx db.Transaction, agent *models.Agent, owner *models.User) ([]*Tip, error) {
	given, err := tx.ListAgentTips(agent.ID)
	if err != nil {
		return nil, err
	}
	isGiven := map[string]bool{}
	for _, tip := range given {
		isGiven[tip.TipKey] = true
	}
	candidates := []*Tip{}
	for _, tip := range tipCatalog {
		if isGiven[tip.TipKey] {
			continue
		}
		isUsed, err := tip.IsUsed(tx, agent, owner)
		if err != nil {
			return nil, err
		}
		if !isUsed {
			candidates = append(candidates, tip)
		}
	}
	return candidates, nil
}

// tipDecisionApart is how long after one tip decision the next may be
// made, whether the last one gave a tip or chose not to: a model asked
// every minute would find a reason in the end.
const tipDecisionApart = 24 * time.Hour

// tipLatelyConversationCount is how many of the person's latest
// conversations the decision is shown.
const tipLatelyConversationCount = 10

// tipReason is the agent telling an idle person about one thing they have
// not tried.
//
// The catalog's own checks only say what the person certainly uses
// already. Whether a tip is worth giving now, and which, is decided by a
// model shown what is left and what the person has been doing lately:
// "has no schedule" says nothing about whether a schedule would help this
// person. The tip it chose is recorded as given when the turn starts, so
// it never comes round again whatever the turn then says.
func (self *Agent) tipReason() speakFirstReason {
	return speakFirstReason{
		name:           SpeakFirstTip,
		isDailyLimited: true,
		isDue: func(ctx context.Context, tx db.Transaction, agent *models.Agent, owner *models.User, idle time.Duration, now time.Time) (bool, error) {
			if !agent.IsTipsEnabled || agent.OnboardedAt == nil || idle < tipIdle {
				return false, nil
			}
			decided, err := tx.CountAgentJobs(&db.AgentJobFilter{
				AgentID: agent.ID, Kinds: []models.AgentJobKind{models.AgentJobSpeakFirst},
				SubjectID: SpeakFirstTip, Since: now.Add(-tipDecisionApart),
			})
			if err != nil || decided > 0 {
				return false, err
			}
			candidates, err := tipsToGive(tx, agent, owner)
			return len(candidates) > 0, err
		},
		prepare: self.chooseTip,
		checkIn: func(ctx context.Context, tx db.Transaction, agent *models.Agent, owner *models.User, now time.Time, prepared map[string]string) (string, error) {
			var tip *Tip
			for _, candidate := range tipCatalog {
				if candidate.TipKey == prepared["tipKey"] {
					tip = candidate
				}
			}
			if tip == nil {
				return "", nil
			}
			main, err := scheduleConversation(tx, agent.ID, "")
			if err != nil {
				return "", err
			}
			if err := tx.AddAgentTip(&models.AgentTip{AgentID: agent.ID, TipKey: tip.TipKey, GivenAt: now, ConversationID: main.ID}); err != nil {
				return "", err
			}
			lines := []string{
				"They have the dashboard open and have been quiet for a while. Tell them about one thing you can do that they have not tried:",
				"",
				"- What: " + tip.Feature,
				"- Where: " + tip.Where,
			}
			if tieIn := strings.TrimSpace(prepared["tieIn"]); tieIn != "" {
				lines = append(lines, "- Why them: "+tieIn)
			}
			lines = append(lines,
				"",
				"Two sentences at most: what it is, tied to what they did where you can, and where to find it. No greeting, no list, nothing else. Do not set anything up yourself.",
				"",
				"If they answer that they do not want tips, call agent_profile with no_more_tips. If they say not now, call agent_profile with not_now.",
			)
			return speakFirstMessage(owner, now, "A tip.") + strings.Join(lines, "\n"), nil
		},
	}
}

// chooseTip asks a model whether a tip is worth giving now, and which.
func (self *Agent) chooseTip(ctx context.Context, run *Run, now time.Time) (map[string]string, bool, error) {
	var candidates []*Tip
	var lately, given []string
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if candidates, err = tipsToGive(tx, run.Agent, run.Owner); err != nil {
			return err
		}
		if lately, err = recentConversationLines(tx, run.Agent, tipLatelyConversationCount); err != nil {
			return err
		}
		tips, err := tx.ListAgentTips(run.Agent.ID)
		for _, tip := range tips {
			given = append(given, fmt.Sprintf("- %s, %s", tip.TipKey, tip.GivenAt.In(Location(run.Owner)).Format("2 January 2006")))
		}
		return err
	}); err != nil || len(candidates) == 0 {
		return nil, false, err
	}
	prompt, err := render("tip_choose.txt", map[string]any{
		"PersonName": personName(run.Owner),
		"Candidates": candidates,
		"Lately":     lately,
		"Given":      given,
	})
	if err != nil {
		return nil, false, err
	}
	chose, err := self.oneShot(ctx, self.runFor(run.Agent, run.Owner, nil, ""), "Chose a tip", prompt, models.AgentJobSpeakFirst, config.AgentWorkAsk)
	if err != nil {
		return nil, false, err
	}
	decision := readModelAnswer[struct {
		ShouldTip      bool   `json:"shouldTip"`
		TipKey         string `json:"tipKey"`
		TieIn          string `json:"tieIn"`
		DecisionReason string `json:"decisionReason"`
	}](chose.Text, "shouldTip")
	if !decision.IsValid {
		log.Warningf("the tip decision for agent %q could not be read: %s", run.Agent.ID, decision.Problem)
		return nil, false, nil
	}
	if !decision.Value.ShouldTip {
		log.Debugf("no tip for agent %q: %s", run.Agent.ID, decision.Value.DecisionReason)
		return nil, false, nil
	}
	for _, candidate := range candidates {
		if candidate.TipKey == decision.Value.TipKey {
			return map[string]string{"tipKey": candidate.TipKey, "tieIn": decision.Value.TieIn}, true, nil
		}
	}
	log.Warningf("the tip decision for agent %q named %q, which is not one it was offered", run.Agent.ID, decision.Value.TipKey)
	return nil, false, nil
}

// recentConversationLines names the person's latest conversations, by
// title and summary, as lines of a list.
func recentConversationLines(tx db.Transaction, agent *models.Agent, conversationCount uint64) ([]string, error) {
	conversations, err := tx.ListAgentConversations(agent.ID, []models.AgentConversationKind{models.AgentConversationMain, models.AgentConversationNamed}, &db.Options{Limit: conversationCount})
	if err != nil {
		return nil, err
	}
	lines := []string{}
	for _, conversation := range conversations {
		said := strings.TrimSpace(conversation.Title)
		if summary := strings.TrimSpace(conversation.Summary); summary != "" {
			said = strings.TrimSpace(said + ": " + cutRunes(summary, 200))
		}
		if said != "" {
			lines = append(lines, fmt.Sprintf("- %s", said))
		}
	}
	return lines, nil
}
