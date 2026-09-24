// Package agentprofile is the agent's own profile, as the person tells it
// in conversation: what it is called, which language it talks in, and the
// lines the person wants added to its standing instructions. It is also
// where the person's word about the agent starting conversations on its
// own is kept: its introduction done, not now, no more tips, no more
// memory checks.
package agentprofile

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/agent/tools/operator"
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
				Description: "Your own profile, as the person tells it to you. `set` changes your name, the language you talk to them in, or adds a line to your standing instructions (never replaces them). `onboarding_done` ends your introduction. `not_now` keeps you from starting a conversation on your own for a day. `no_more_tips` and `no_more_memory_checks` switch those off. For the person's own name use account_update.",
				Parameters: tools.Object(map[string]any{
					"action":          tools.EnumProperty("what to do", "set", "onboarding_done", "not_now", "no_more_tips", "no_more_memory_checks"),
					"agent_name":      tools.StringProperty("for set: what they want to call you"),
					"language":        tools.StringProperty("for set: the language to talk to them in, as a tag: en, ja, zh"),
					"add_instruction": tools.StringProperty("for set: one line to add to your standing instructions, in their words: what they want help with, how they like to be written to"),
				}, "action"),
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
					case "no_more_tips":
						return "Switch the agent's tips off"
					case "no_more_memory_checks":
						return "Switch the agent's memory checks off"
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
	case "no_more_tips":
		if _, err := operator.Execute(ctx, `mutation { UpdateAgent(isTipsEnabled: false) { agent { id } } }`, nil); err != nil {
			return nil, err
		}
		return noted("tips are off; the person can switch them on again in the agent's settings"), nil
	case "no_more_memory_checks":
		if _, err := operator.Execute(ctx, `mutation { UpdateAgent(isMemoryCheckEnabled: false) { agent { id } } }`, nil); err != nil {
			return nil, err
		}
		return noted("memory checks are off; the person can switch them on again in the agent's settings"), nil
	default:
		return nil, fmt.Errorf("%q is not set, onboarding_done, not_now, no_more_tips or no_more_memory_checks", action)
	}
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
