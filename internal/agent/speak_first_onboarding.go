package agent

import (
	"context"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// onboardingLasts is how long an introduction stays open when the person
// never finishes it: after that it is over, answered or not, and what
// was not asked stays on the settings page.
const onboardingLasts = 3 * 24 * time.Hour

// onboardingReason is the agent introducing itself to somebody who has
// just switched it on and never written to it: once, and before anything
// else it might say.
func (self *Agent) onboardingReason() speakFirstReason {
	return speakFirstReason{
		name: SpeakFirstOnboarding,
		isDue: func(ctx context.Context, tx db.Transaction, agent *models.Agent, owner *models.User, idle time.Duration, now time.Time) (bool, error) {
			if agent.OnboardedAt != nil {
				return false, nil
			}
			// Once: the greeting stays open in the conversation, carried
			// by the profile tool's overlay, until the person answers or
			// the three days are up.
			if agent.SpokeFirstAt != nil {
				if now.Sub(*agent.SpokeFirstAt) > onboardingLasts {
					return false, tx.MarkAgentSpokeFirst(agent.ID, nil, &now, nil)
				}
				return false, nil
			}
			// Somebody who has written to it already knows it; they are
			// not greeted as if they had just arrived.
			spoke, err := tx.LastAgentPersonWordAt(agent.ID)
			if err != nil {
				return false, err
			}
			if spoke != nil {
				return false, tx.MarkAgentSpokeFirst(agent.ID, nil, &now, nil)
			}
			return true, nil
		},
		checkIn: func(ctx context.Context, tx db.Transaction, agent *models.Agent, owner *models.User, now time.Time) (string, error) {
			return speakFirstMessage(owner, now, "The person has just switched you on and has never written to you. Introduce yourself.") + strings.Join([]string{
				"Greet them in a sentence, say in one or two what you can do for them (read and sort their mail, answer from what you remember, keep schedules, work in their browser and on their computer when they connect them), and then ask the first of these, one question per message, waiting for each answer:",
				"",
				"1. What they would like to call you.",
				"2. What you should call them.",
				"3. Which language to talk to them in.",
				"4. What they would like help with first.",
				"",
				"Ask only the first now. As each answer comes, save it at once: agent_profile for your name, your language, and what they want help with (as a line added to your instructions); account_update for their name. Skip any they would rather not answer. When they want to skip the lot, or all four are answered, call agent_profile with onboarding_done. At the end, offer to connect a mailbox ([Mail](/settings/agent/mail)) or another source ([Sources](/settings/agent/sources)) so you have something to work from.",
				"",
				"If they say not now, call agent_profile with not_now, say you will be here, and stop.",
			}, "\n"), nil
		},
	}
}

// memoryCheckReason is the agent asking to check a few things it
// remembers. Not due until the check exists (Milestone 4 of
// docs/planning/agent-speaks-first-execplan.md).
func (self *Agent) memoryCheckReason() speakFirstReason {
	return speakFirstReason{
		name:           SpeakFirstMemoryCheck,
		isDailyLimited: true,
		isDue: func(ctx context.Context, tx db.Transaction, agent *models.Agent, owner *models.User, idle time.Duration, now time.Time) (bool, error) {
			return false, nil
		},
		checkIn: func(ctx context.Context, tx db.Transaction, agent *models.Agent, owner *models.User, now time.Time) (string, error) {
			return "", nil
		},
	}
}

// tipReason is the agent telling an idle person about one thing they have
// not tried. Not due until tips exist (Milestone 7).
func (self *Agent) tipReason() speakFirstReason {
	return speakFirstReason{
		name:           SpeakFirstTip,
		isDailyLimited: true,
		isDue: func(ctx context.Context, tx db.Transaction, agent *models.Agent, owner *models.User, idle time.Duration, now time.Time) (bool, error) {
			return false, nil
		},
		checkIn: func(ctx context.Context, tx db.Transaction, agent *models.Agent, owner *models.User, now time.Time) (string, error) {
			return "", nil
		},
	}
}
