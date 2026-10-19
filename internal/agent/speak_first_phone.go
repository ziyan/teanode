package agent

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// phoneTourWithin is how long after a phone connects the agent may still
// show the person around it: a phone connected a week ago and never toured
// is one they have found their way around already.
const phoneTourWithin = 7 * 24 * time.Hour

// phoneTourReason is the agent showing the person around a phone they have
// just connected, in the app, once per phone: what this server lets it do,
// one thing at a time, offering only what is turned on.
func (self *Agent) phoneTourReason() speakFirstReason {
	return speakFirstReason{
		name:   SpeakFirstPhoneTour,
		canAsk: true,
		isDue: func(ctx context.Context, tx db.Transaction, agent *models.Agent, owner *models.User, idle time.Duration, now time.Time) (bool, error) {
			// The introduction comes first, for somebody new to the agent
			// altogether.
			if agent.OnboardedAt == nil {
				return false, nil
			}
			phone, err := untouredPhone(tx, agent.ID, now, false)
			return phone != nil, err
		},
		checkIn: func(ctx context.Context, tx db.Transaction, agent *models.Agent, owner *models.User, now time.Time, prepared map[string]string) (string, error) {
			isAsked := prepared[preparedIsAsked] == "true"
			phone, err := untouredPhone(tx, agent.ID, now, isAsked)
			if err != nil || phone == nil {
				return "", err
			}
			// Once per phone, also when two instances start it together.
			if isMarked, err := tx.MarkAgentPhoneToured(phone.ID, now); err != nil || (!isMarked && !isAsked) {
				return "", err
			}
			mailboxes, err := tx.ListMailboxes(owner.ID)
			if err != nil {
				return "", err
			}
			return speakFirstMessage(owner, now, "The person has just connected their phone, "+phone.PhoneName+", to you with the TeaNode app. Show them around it.", prepared) +
				phoneTourInstructions(self.phoneTourFacts(ctx, agent, owner, phone, len(mailboxes) > 0)), nil
		},
	}
}

// untouredPhone is the newest phone connected within phoneTourWithin and
// not shown around yet, or, when the person asked for the tour, their newest
// phone whatever its state; nil when there is none.
//
// Signing in again connects the same phone anew, with a new record: a phone
// of the same name and model as one already shown around is that phone, and
// is not shown around again unless they ask.
func untouredPhone(tx db.Transaction, agentId string, now time.Time, isAsked bool) (*models.AgentPhone, error) {
	phones, err := tx.ListAgentPhones(agentId)
	if err != nil {
		return nil, err
	}
	sort.Slice(phones, func(first, second int) bool { return phones[first].CreatedAt.After(phones[second].CreatedAt) })
	isToured := map[string]bool{}
	for _, phone := range phones {
		if phone.TouredAt != nil {
			isToured[phone.PhoneName+"\x00"+phone.PhoneModel] = true
		}
	}
	for _, phone := range phones {
		if isAsked {
			return phone, nil
		}
		if phone.TouredAt == nil && now.Sub(phone.CreatedAt) < phoneTourWithin && !isToured[phone.PhoneName+"\x00"+phone.PhoneModel] {
			return phone, nil
		}
	}
	return nil, nil
}

// phoneTourFacts is what this server lets the phone do, as the turn is told
// it: each thing on, so nothing switched off is offered.
type phoneTourFacts struct {
	canBeNotified   bool
	isPushEnabled   bool
	isVoiceOn       bool
	hasCodingAgents bool
	hasMailbox      bool
	hasLocation     bool
	hasHealth       bool
}

func (self *Agent) phoneTourFacts(ctx context.Context, agent *models.Agent, owner *models.User, phone *models.AgentPhone, hasMailbox bool) phoneTourFacts {
	configuration := self.settings.Configuration()
	return phoneTourFacts{
		hasLocation:     self.hasPermission(ctx, owner, models.PermissionLocationUse),
		hasHealth:       self.hasPermission(ctx, owner, models.PermissionHealthUse),
		canBeNotified:   phone.CanBeNotified(),
		isPushEnabled:   configuration.Agent.Push.IsEnabled(),
		isVoiceOn:       configuration.Agent.Voice.Enabled && configuration.Agent.VoiceProvider() != nil,
		hasCodingAgents: len(self.herdrComputers(agent.ID)) > 0,
		hasMailbox:      hasMailbox && configuration.Services.IsEnabled(models.ServiceMailboxes),
	}
}

// hasPermission says the person holds a permission, which is what the offers
// of Location (location:use) and Health (health:use) in the tour depend on.
func (self *Agent) hasPermission(ctx context.Context, owner *models.User, permission models.Permission) bool {
	if self.operations == nil {
		return false
	}
	operations, err := self.operations(ctx, owner)
	if err != nil || operations == nil {
		return false
	}
	return operations.Permissions().Has(permission)
}

// phoneTourInstructions is what the tour offers, in order, from what is on.
func phoneTourInstructions(facts phoneTourFacts) string {
	var offers []string
	switch {
	case !facts.isPushEnabled:
		offers = append(offers, "Notifications are switched off on this server, so the phone hears from you only while the app is open; say so once, plainly, and do not offer them.")
	case facts.canBeNotified:
		offers = append(offers, "Notifications reach this phone. Ask which they want: the account tool's phones action lists the kinds, and phone_notifications sets them.")
	default:
		offers = append(offers, "Notifications do not reach this phone yet: tell them to allow them when the app asks, or in the app's Settings, then send a test with the account tool's test_phone.")
	}
	offers = append(offers,
		"Sending you things from any app: the Share button, then TeaNode, with a note; a photo of a receipt, a link, a file.",
		"Siri: \"Ask TeaNode\" asks you something and reads the answer; it works from the lock screen and in Shortcuts.",
		"Widgets: Your agent (ask or call), Waiting for you (what you need from them) and Up next (events and unread mail), on the Home Screen and the Lock Screen.")
	if facts.isVoiceOn {
		offers = append(offers, "Calling you: the waveform button beside the message box, \"Call TeaNode\" to Siri, or a Control Center button; it works with AirPods and the screen locked.")
	}
	if facts.hasCodingAgents {
		offers = append(offers, "Their coding sessions: a Claude Code or Codex question on their computer reaches the phone, and they answer it from the notification.")
	}
	if facts.hasLocation {
		offers = append(offers, "Location: the app can record the places they go, so they can look back on where they were and share where they are with their family. It starts with three short rows they tap through: allow location, mark home, and share with family. If they want it, open it for them with the location tool's set_up action, or say it is Location in the app's menu.")
	}
	if facts.hasHealth {
		offers = append(offers, "Health: the app can copy their Apple Health data (steps, sleep, heart rate, workouts and the rest, whatever they allow on Apple's Health permissions screen) to this server, so they see it as charts on the dashboard and in the app, and can share some of it with their family. You read it only if they turn on Let the agent read Health data. If they want it, open it for them with the health tool's set_up action, or say it is Health in the app's Settings.")
	}
	if facts.hasMailbox {
		offers = append(offers, "Their mail, calendars and contacts in the iPhone's own apps: the account tool's set_up_phone_mail action takes them to where one tap sets it up.")
	}
	lines := []string{
		"Greet them on the new phone in a sentence, then offer these one at a time, waiting for them to say yes, no or enough before the next. Each offer is the question of one ask_user call, with nothing written beside it, and is offered once: after they answer, go on to the next item, never to the one just asked.",
		"",
	}
	for index, offer := range offers {
		lines = append(lines, strconv.Itoa(index+1)+". "+offer)
	}
	lines = append(lines, "",
		"Do what they say yes to with your tools, or say exactly where to tap. When they say enough, or the list is done, stop: say they can ask about any of it later. Never offer something not on this list.")
	return strings.Join(lines, "\n")
}
