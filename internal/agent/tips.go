package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// A tip is one thing the agent can do that the person has not tried, told
// while they have the dashboard open and have gone quiet. None is told
// twice, and none about something they already use.

// tipIdle is how long the person has to have been still before a tip: a
// tip is for a pause, never for somebody in the middle of something.
const tipIdle = 5 * time.Minute

// tipRecentConversationCount is how many of the person's latest
// conversations the check-in names, so the tip can be tied to what they
// actually do.
const tipRecentConversationCount = 5

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

// tipReason is the agent telling an idle person about one thing they have
// not tried.
//
// The tip is chosen here, not by the model: the first of the catalog they
// do not use and were not told, recorded as given when the turn starts. A
// model asked to pick one and say which would sometimes not say, and the
// same tip would come round again.
func (self *Agent) tipReason() speakFirstReason {
	return speakFirstReason{
		name:           SpeakFirstTip,
		isDailyLimited: true,
		isDue: func(ctx context.Context, tx db.Transaction, agent *models.Agent, owner *models.User, idle time.Duration, now time.Time) (bool, error) {
			if !agent.IsTipsEnabled || agent.OnboardedAt == nil || idle < tipIdle {
				return false, nil
			}
			candidates, err := tipsToGive(tx, agent, owner)
			return len(candidates) > 0, err
		},
		checkIn: func(ctx context.Context, tx db.Transaction, agent *models.Agent, owner *models.User, now time.Time) (string, error) {
			candidates, err := tipsToGive(tx, agent, owner)
			if err != nil || len(candidates) == 0 {
				return "", err
			}
			tip := candidates[0]
			main, err := scheduleConversation(tx, agent.ID, "")
			if err != nil {
				return "", err
			}
			if err := tx.AddAgentTip(&models.AgentTip{AgentID: agent.ID, TipKey: tip.TipKey, GivenAt: now, ConversationID: main.ID}); err != nil {
				return "", err
			}
			lately, err := recentConversationLines(tx, agent)
			if err != nil {
				return "", err
			}
			lines := []string{
				"They have the dashboard open and have been quiet for a while. Tell them about one thing you can do that they have not tried:",
				"",
				"- What: " + tip.Feature,
				"- Where: " + tip.Where,
				"",
			}
			if len(lately) > 0 {
				lines = append(lines, "What they have been doing with you lately, so you can tie the tip to it where it fits:")
				lines = append(lines, lately...)
				lines = append(lines, "")
			}
			lines = append(lines,
				"Two sentences at most: what it is, tied to something they actually did if you can, and where to find it. No greeting, no list, nothing else. Do not set anything up yourself.",
				"",
				"If they answer that they do not want tips, call agent_profile with no_more_tips. If they say not now, call agent_profile with not_now.",
			)
			return speakFirstMessage(owner, now, "A tip.") + strings.Join(lines, "\n"), nil
		},
	}
}

// recentConversationLines names the person's latest conversations, by
// title and summary, as lines of a list.
func recentConversationLines(tx db.Transaction, agent *models.Agent) ([]string, error) {
	conversations, err := tx.ListAgentConversations(agent.ID, []models.AgentConversationKind{models.AgentConversationMain, models.AgentConversationNamed}, &db.Options{Limit: tipRecentConversationCount})
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
