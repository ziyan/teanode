// Package agentprofile is the agent's own profile, as the person tells it
// in conversation: what it is called, which language it talks in, and the
// lines the person wants added to its standing instructions. It is also
// where the person's word about the agent starting conversations on its
// own is kept: its introduction done, not now, no more ideas, no more
// memory checks, no alerts, and "don't tell me about these".
package agentprofile

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/agent/tools/operator"
	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// NotNowLasts is how long "not now" keeps the agent from starting a
// conversation on its own, for any reason.
const NotNowLasts = 24 * time.Hour

// instructionMostRuneCount is the longest line one call adds to the
// standing instructions: a sentence the person said, not an essay.
const instructionMostRuneCount = 500

func init() {
	tools.Register(func() []*tools.Tool {
		return []*tools.Tool{
			{
				Name: "agent_profile", Family: tools.FamilyGeneral, Core: true, Risk: tools.RiskWrite,
				Description: "Your own profile, as the person tells it to you. `set` changes your name, the language you talk to them in, or adds a line to your standing instructions (never replaces them). `onboarding_done` ends your introduction. `not_now` keeps you from starting a conversation on your own for a day. `no_more_ideas` and `no_more_memory_checks` switch those off, only when they say they never want them; stopping one check or dismissing one idea is not that. `ideas_on` and `memory_checks_on` switch them back on. `no_alerts` stops you telling them unasked what their mail says they should know, only when they want none at all; `alerts_on` starts it again. When they say not to be told about something like an alert again, `mute_alert`: by default what the alert was about (the burst, or the sender of a single message), or by mute_scope its subject, its sender, the sender's domain or its kind; the alert whose id they answered (each alert's line names it), else the latest; or give mute_target to name the address, domain, subject or kind (burst, budget, or a category such as notification) yourself, whose scope is read from it when left out. A budget alert mutes by default its spending category (mute_scope spendingCategory, mute_target the spending category's id); kind budget mutes every budget and savings target alert. `unmute_alert` takes one back, by mute_scope and mute_target. `unattended` says what you may do without their word when they are not there (outward: speak for them, such as sending mail; destructive: what cannot be undone; granting: giving somebody access; listed: tools on their ask-me-first list); `set_unattended` replaces that list with unattended_allowed_risks, only when they ask, and they confirm it. For the person's own name use account_update.",
				Parameters: tools.Object(map[string]any{
					"action":          tools.EnumProperty("what to do", "set", "onboarding_done", "not_now", "no_more_ideas", "no_more_memory_checks", "ideas_on", "memory_checks_on", "no_alerts", "alerts_on", "mute_alert", "unmute_alert", "unattended", "set_unattended"),
					"agent_name":      tools.StringProperty("for set: what they want to call you"),
					"language":        tools.StringProperty("for set: the language to talk to them in, as a tag: en, ja, zh"),
					"add_instruction": tools.StringProperty("for set: one line to add to your standing instructions, in their words: what they want help with, how they like to be written to"),
					"alert_id":        tools.StringProperty("for mute_alert: the alert to mute from; the latest when left out"),
					"mute_scope":      tools.EnumProperty("for mute_alert and unmute_alert: what to match; for mute_alert, left out, what the alert was about, or what mute_target reads as", string(models.AlertMuteSubjectKey), string(models.AlertMuteSender), string(models.AlertMuteDomain), string(models.AlertMuteKind), string(models.AlertMuteSpendingCategory)),
					"mute_target":     tools.StringProperty("for mute_alert: the address, domain, subject key or kind, when not taken from the alert; for unmute_alert: the one to take back"),
					"unattended_allowed_risks": map[string]any{
						"type":        "array",
						"description": "for set_unattended: every kind of action you may take without their word when they are not there; an empty list allows none",
						"items":       map[string]any{"type": "string", "enum": []string{string(models.UnattendedRiskOutward), string(models.UnattendedRiskDestructive), string(models.UnattendedRiskGranting), string(models.UnattendedRiskListed)}},
					},
				}, "action"),
				// Reading what is allowed is a read; changing it gives the
				// agent more or less room to act alone, which the person
				// confirms.
				RiskOf: func(raw json.RawMessage) tools.Risk {
					var asked arguments
					if json.Unmarshal(raw, &asked) != nil {
						return ""
					}
					switch strings.ToLower(strings.TrimSpace(asked.Action)) {
					case "unattended":
						return tools.RiskRead
					case "set_unattended":
						return tools.RiskGranting
					}
					return ""
				},
				Preview: tools.PreviewOf(func(call arguments) string {
					switch strings.TrimSpace(call.Action) {
					case "set":
						changing := []string{}
						if strings.TrimSpace(call.AgentName) != "" {
							changing = append(changing, fmt.Sprintf("call the agent %q", strings.TrimSpace(call.AgentName)))
						}
						if strings.TrimSpace(call.Language) != "" {
							changing = append(changing, "talk in "+strings.TrimSpace(call.Language))
						}
						if strings.TrimSpace(call.AddInstruction) != "" {
							changing = append(changing, "remember an instruction")
						}
						if len(changing) == 0 {
							return "Change the agent's profile"
						}
						return "Agent profile: " + strings.Join(changing, ", ")
					case "onboarding_done":
						return "End the agent's introduction"
					case "not_now":
						return "Keep the agent from starting conversations for a day"
					case "no_more_ideas":
						return "Switch the agent's ideas off"
					case "no_more_memory_checks":
						return "Switch the agent's memory checks off"
					case "ideas_on":
						return "Switch the agent's ideas on"
					case "memory_checks_on":
						return "Switch the agent's memory checks on"
					case "no_alerts":
						return "Switch the agent's alerts off"
					case "alerts_on":
						return "Switch the agent's alerts on"
					case "mute_alert":
						return "Stop the agent telling you about these"
					case "unmute_alert":
						return "Let the agent tell you about these again"
					case "unattended":
						return "Say what the agent may do when you are not there"
					case "set_unattended":
						if len(call.UnattendedAllowedRisks) == 0 {
							return "When you are not there, the agent does nothing that needs your word"
						}
						return "When you are not there, the agent may do without your word: " + strings.Join(call.UnattendedAllowedRisks, ", ")
					}
					return "Change the agent's profile"
				}),
				Run:     run,
				Overlay: overlay,
			},
		}
	})
}

type arguments struct {
	Action         string `json:"action"`
	AgentName      string `json:"agent_name"`
	Language       string `json:"language"`
	AddInstruction string `json:"add_instruction"`
	AlertID        string `json:"alert_id"`
	MuteScope      string `json:"mute_scope"`
	MuteTarget     string `json:"mute_target"`

	// UnattendedAllowedRisks is the new list for set_unattended.
	UnattendedAllowedRisks []string `json:"unattended_allowed_risks"`
}

func run(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	asked, err := tools.DecodeArguments[arguments](call)
	if err != nil {
		return nil, err
	}
	current, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	agent := current.Agent()
	if agent == nil {
		return nil, fmt.Errorf("there is no agent")
	}
	now := time.Now()
	switch action := strings.ToLower(strings.TrimSpace(asked.Action)); action {
	case "set":
		return set(ctx, current, agent, asked)
	case "onboarding_done":
		if err := mark(ctx, current, agent.ID, nil, &now, nil); err != nil {
			return nil, err
		}
		return noted("your introduction is over"), nil
	case "not_now":
		until := now.Add(NotNowLasts)
		if err := mark(ctx, current, agent.ID, nil, nil, &until); err != nil {
			return nil, err
		}
		return noted("you will not start a conversation on your own until " + until.In(tools.Location(current.Owner())).Format("Monday 15:04")), nil
	case "unattended":
		allowed := []string{}
		for _, unattendedRisk := range agent.UnattendedAllowedRisks {
			allowed = append(allowed, string(unattendedRisk))
		}
		if len(allowed) == 0 {
			return noted("when the person is not there you do nothing that needs their word; such a call is refused and you say what you would have done"), nil
		}
		return noted("when the person is not there you may do without their word what is " + strings.Join(allowed, ", ") + "; anything else that needs it is refused"), nil
	case "set_unattended":
		// Never from a run nobody is watching, whatever it is allowed:
		// one kind allowed must not become the means to allow the rest.
		if current.Headless() {
			return nil, fmt.Errorf("only the person changes what you may do when they are not there; ask them, or they set it under When you are not there on their agent's settings")
		}
		variables := map[string]any{"unattendedAllowedRisks": asked.UnattendedAllowedRisks}
		if asked.UnattendedAllowedRisks == nil {
			variables["unattendedAllowedRisks"] = []string{}
		}
		if _, err := operator.Execute(ctx, `mutation ($unattendedAllowedRisks: [String!]) { UpdateAgent(unattendedAllowedRisks: $unattendedAllowedRisks) { agent { id } } }`, variables); err != nil {
			return nil, err
		}
		return noted("saved: what you may do when they are not there is now " + strings.Join(asked.UnattendedAllowedRisks, ", ") + "; nothing else that needs their word"), nil
	case "no_more_ideas":
		if _, err := operator.Execute(ctx, `mutation { UpdateAgent(isIdeasEnabled: false) { agent { id } } }`, nil); err != nil {
			return nil, err
		}
		return noted("ideas are off: you will not offer one on your own; the person can switch them on again in the agent's settings"), nil
	case "no_more_memory_checks":
		if _, err := operator.Execute(ctx, `mutation { UpdateAgent(isMemoryCheckEnabled: false) { agent { id } } }`, nil); err != nil {
			return nil, err
		}
		return noted("memory checks are off; the person can switch them on again in the agent's settings"), nil
	case "ideas_on":
		if _, err := operator.Execute(ctx, `mutation { UpdateAgent(isIdeasEnabled: true) { agent { id } } }`, nil); err != nil {
			return nil, err
		}
		return noted("ideas are on"), nil
	case "memory_checks_on":
		if _, err := operator.Execute(ctx, `mutation { UpdateAgent(isMemoryCheckEnabled: true) { agent { id } } }`, nil); err != nil {
			return nil, err
		}
		return noted("memory checks are on"), nil
	case "no_alerts":
		if _, err := operator.Execute(ctx, `mutation { UpdateAgent(isAlertsEnabled: false) { agent { id } } }`, nil); err != nil {
			return nil, err
		}
		return noted("alerts are off: you will not tell them about their mail unasked; the person can switch them on again in the agent's settings"), nil
	case "alerts_on":
		if _, err := operator.Execute(ctx, `mutation { UpdateAgent(isAlertsEnabled: true) { agent { id } } }`, nil); err != nil {
			return nil, err
		}
		return noted("alerts are on"), nil
	case "mute_alert":
		return muteAlert(ctx, asked)
	case "unmute_alert":
		return unmuteAlert(ctx, asked)
	default:
		return nil, fmt.Errorf("%q is not set, onboarding_done, not_now, no_more_ideas, no_more_memory_checks, ideas_on, memory_checks_on, no_alerts, alerts_on, mute_alert or unmute_alert", action)
	}
}

// muteAlert is "don't tell me about these", through the same operation as
// the Mute button on the agent page. Said after an alert, "these" is the
// latest one.
func muteAlert(ctx context.Context, asked arguments) (*tools.Result, error) {
	// A scope left out is the server's to choose: what the alert was
	// about, or what the target reads as.
	variables := map[string]any{}
	if muteScope := strings.TrimSpace(asked.MuteScope); muteScope != "" {
		variables["muteScope"] = muteScope
	}
	if target := strings.TrimSpace(asked.MuteTarget); target != "" {
		variables["muteTarget"] = target
	} else {
		alertId := strings.TrimSpace(asked.AlertID)
		if alertId == "" {
			var listed struct {
				ListAgentAlerts []struct {
					ID string `json:"id"`
				} `json:"ListAgentAlerts"`
			}
			if err := tools.MustRun(ctx).Operations().Execute(ctx, client.DocumentListAgentAlerts, map[string]any{"first": 1}, &listed); err != nil {
				return nil, err
			}
			if len(listed.ListAgentAlerts) == 0 {
				return nil, fmt.Errorf("you have told them nothing unasked yet; give mute_scope and mute_target to name what to mute")
			}
			alertId = listed.ListAgentAlerts[0].ID
		}
		variables["alertId"] = alertId
	}
	var muted struct {
		MuteAgentAlert struct {
			MuteScope  string `json:"muteScope"`
			MuteTarget string `json:"muteTarget"`
		} `json:"MuteAgentAlert"`
	}
	if err := tools.MustRun(ctx).Operations().Execute(ctx, client.DocumentMuteAgentAlert, variables, &muted); err != nil {
		return nil, err
	}
	tools.RecordGoalArtifact(ctx, models.GoalArtifactAlertMute, muted.MuteAgentAlert.MuteTarget,
		fmt.Sprintf("alerts muted for %s %s", muted.MuteAgentAlert.MuteScope, muted.MuteAgentAlert.MuteTarget))
	return noted(fmt.Sprintf("muted %s %q: you will not tell them about it unasked; the person can unmute it on the agent page", muted.MuteAgentAlert.MuteScope, muted.MuteAgentAlert.MuteTarget)), nil
}

// unmuteAlert takes a mute back, found by what it matches.
func unmuteAlert(ctx context.Context, asked arguments) (*tools.Result, error) {
	muteScope, muteTarget := strings.TrimSpace(asked.MuteScope), strings.ToLower(strings.TrimSpace(asked.MuteTarget))
	var listed struct {
		ListAgentAlertMutes []struct {
			ID         string `json:"id"`
			MuteScope  string `json:"muteScope"`
			MuteTarget string `json:"muteTarget"`
		} `json:"ListAgentAlertMutes"`
	}
	if err := tools.MustRun(ctx).Operations().Execute(ctx, client.DocumentListAgentAlertMutes, nil, &listed); err != nil {
		return nil, err
	}
	names := []string{}
	for _, mute := range listed.ListAgentAlertMutes {
		if strings.ToLower(mute.MuteTarget) == muteTarget && (muteScope == "" || mute.MuteScope == muteScope) {
			if _, err := operator.Execute(ctx, client.DocumentUnmuteAgentAlert, map[string]any{"muteId": mute.ID}); err != nil {
				return nil, err
			}
			return noted(fmt.Sprintf("unmuted %s %q", mute.MuteScope, mute.MuteTarget)), nil
		}
		names = append(names, fmt.Sprintf("%s %q", mute.MuteScope, mute.MuteTarget))
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("nothing is muted")
	}
	return nil, fmt.Errorf("no mute matches %q; muted are: %s", muteTarget, strings.Join(names, ", "))
}

// set changes the profile through the same mutation the settings page
// uses, so it is held to the same permission and written to the same
// audit log.
func set(ctx context.Context, current tools.Run, agent *models.Agent, asked arguments) (*tools.Result, error) {
	variables := map[string]any{}
	said := []string{}
	if name := strings.TrimSpace(asked.AgentName); name != "" {
		variables["name"] = name
		said = append(said, fmt.Sprintf("you are called %q", name))
	}
	if language := strings.TrimSpace(asked.Language); language != "" {
		variables["language"] = language
		said = append(said, "you talk in "+language)
	}
	if line := strings.TrimSpace(asked.AddInstruction); line != "" {
		if runes := []rune(line); len(runes) > instructionMostRuneCount {
			line = string(runes[:instructionMostRuneCount])
		}
		// Added under what is there, never in its place: the instructions
		// are the person's own words, and a line from one conversation is
		// not a reason to lose the rest.
		instructions := strings.TrimSpace(agent.Instructions)
		if !strings.Contains(instructions, line) {
			if instructions != "" {
				instructions += "\n"
			}
			instructions += line
		}
		variables["instructions"] = instructions
		said = append(said, "the line is in your instructions")
	}
	if len(variables) == 0 {
		return nil, fmt.Errorf("nothing to set: give agent_name, language or add_instruction")
	}
	if _, err := operator.Execute(ctx, `mutation ($name: String, $language: String, $instructions: String) { UpdateAgent(name: $name, language: $language, instructions: $instructions) { agent { id } } }`, variables); err != nil {
		return nil, err
	}
	return noted(strings.Join(said, "; ")), nil
}

func mark(ctx context.Context, current tools.Run, agentId string, spokeFirstAt, onboardedAt, snoozedUntil *time.Time) error {
	return current.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		return tx.MarkAgentSpokeFirst(agentId, spokeFirstAt, onboardedAt, snoozedUntil)
	})
}

func noted(line string) *tools.Result {
	return &tools.Result{Content: line, Note: line}
}

// overlay, while the introduction is open, is what is still to ask. Read
// from the database rather than the run's copy of the agent, which is as
// it was when the turn began.
func overlay(ctx context.Context) string {
	current, err := tools.RunFrom(ctx)
	if err != nil || current.Agent() == nil {
		return ""
	}
	conversation := current.Conversation()
	if conversation == nil || conversation.Kind != models.AgentConversationMain {
		return ""
	}
	var agent *models.Agent
	var owner *models.User
	if err := current.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if agent, err = tx.GetAgent(current.Agent().ID); err != nil {
			return err
		}
		owner, err = tx.GetUser(current.Owner().ID)
		return err
	}); err != nil || agent == nil || agent.OnboardedAt != nil || agent.SpokeFirstAt == nil {
		return ""
	}
	open := []string{}
	if agent.Name == "" || agent.Name == models.AgentDefaultName {
		open = append(open, "what to call you")
	}
	if owner != nil && strings.TrimSpace(owner.Name) == "" {
		open = append(open, "what to call them")
	}
	if strings.TrimSpace(agent.Language) == "" {
		open = append(open, "which language to talk in")
	}
	if strings.TrimSpace(agent.Instructions) == "" {
		open = append(open, "what they want help with first")
	}
	if len(open) == 0 {
		return "<introduction>\nYour introduction is open and everything is answered: offer to connect a mailbox or a source, then call agent_profile with onboarding_done.\n</introduction>"
	}
	return "<introduction>\nYou are introducing yourself; still to ask, one at a time and only if they are willing: " + strings.Join(open, "; ") + ". Save each answer as it comes. If they want to skip it, call agent_profile with onboarding_done.\n</introduction>"
}
