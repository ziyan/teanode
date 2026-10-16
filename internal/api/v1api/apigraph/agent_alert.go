package apigraph

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
)

// Alerts: what the agent told the caller unasked, and what they asked not
// to be told about. The dashboard's Alerts card, the command line's agent
// alert and the agent's own agent_profile tool all call these, so the
// three cannot drift. The switches and the night are UpdateAgent's and
// GrantAgentMailbox's.
//
// The agent's follow-ups (a bill, a parcel, a deadline, a coming event, kept
// from mail until something closes them) are read and stopped here too, with
// the same permission: what the agent says about them is alerts, and
// "stop following that" and "don't tell me about parcels" go together.

// AgentAlertQuery reads the caller's alerts and mutes.
type AgentAlertQuery interface {
	// The caller's latest alerts, newest first: what was said, when, its
	// subject key, whether it could not wait, and the messages it was
	// about. Needs agent:use.
	ListAgentAlerts(ctx context.Context, arguments ListAgentAlertsArguments) ([]*agent.AlertView, error)

	// What the caller asked not to be told about, newest first. Needs
	// agent:use.
	ListAgentAlertMutes(ctx context.Context) ([]*models.AgentAlertMute, error)

	// What the caller's agent is following: bills, parcels, deadlines and
	// coming events. With isOpenOnly true or left out, only those still
	// being watched (open, overdue or stalled), by their day, the nearest
	// first; with false, the newest of any state, the closed and stopped
	// too. Needs agent:use.
	ListAgentFollowUps(ctx context.Context, arguments ListAgentFollowUpsArguments) ([]*models.AgentFollowUp, error)
}

// AgentAlertMutation changes the mutes.
type AgentAlertMutation interface {
	// Stop telling the caller about something: a sender, a domain, a
	// subject key or a kind of alert (burst, or a category of the sorting
	// such as notification). With an alert, the target may be left out
	// and is taken from the alert, and the scope left out is its subject
	// key. Muting what is already muted keeps the one mute. Needs
	// agent:use.
	MuteAgentAlert(ctx context.Context, arguments MuteAgentAlertArguments) (*models.AgentAlertMute, error)

	// Take a mute back: what it matched may be told again. Needs
	// agent:use.
	UnmuteAgentAlert(ctx context.Context, arguments UnmuteAgentAlertArguments) (bool, error)

	// Stop following one of the caller's agent's follow-ups: it is kept,
	// in the state stopped, and nothing more is said about it. One that is
	// already over is returned as it is. Not found when it is not the
	// caller's agent's. Needs agent:use.
	StopAgentFollowUp(ctx context.Context, arguments StopAgentFollowUpArguments) (*models.AgentFollowUp, error)

	// Follow something the caller names: a bill with an amount and a day,
	// a parcel, a deadline or an event with a day. The same one named
	// again, or one the agent already found, stays one follow-up, and one
	// that was over is followed again. Needs agent:use.
	AddAgentFollowUp(ctx context.Context, arguments AddAgentFollowUpArguments) (*models.AgentFollowUp, error)

	// Look back over the last 45 days of the caller's mail that the agent
	// already sorted, and queue the messages that look like a bill, a
	// parcel, a form or a dated notice (at most 150, the newest first) to
	// be read for follow-ups. It runs by itself once for an agent with
	// alerts on, after follow-ups ship; this runs it again on demand, for
	// someone who turned alerts on later. Reading the same message twice
	// changes nothing. Needs agent:use.
	BackfillAgentFollowUps(ctx context.Context) (*agent.FollowUpBackfillView, error)
}

// ListAgentFollowUpsArguments bound the listing.
type ListAgentFollowUpsArguments struct {
	// IsOpenOnly leaves out what is over; true when left out.
	IsOpenOnly *bool `json:"isOpenOnly" graphapi:"nullable"`
}

// StopAgentFollowUpArguments name the follow-up.
type StopAgentFollowUpArguments struct {
	FollowUpID string `json:"followUpId"`
}

// AddAgentFollowUpArguments describe a follow-up the caller names. The
// kind says which of the other fields matter: a bill needs an amount, a
// parcel a tracking number or a title, and a bill, deadline or event a day
// written YYYY-MM-DD.
type AddAgentFollowUpArguments struct {
	FollowUpKind   string `json:"followUpKind"`
	FollowUpTitle  string `json:"followUpTitle"`
	DueOn          string `json:"dueOn" graphapi:"nullable"`
	Payee          string `json:"payee" graphapi:"nullable"`
	Amount         string `json:"amount" graphapi:"nullable"`
	CurrencyCode   string `json:"currencyCode" graphapi:"nullable"`
	Carrier        string `json:"carrier" graphapi:"nullable"`
	TrackingNumber string `json:"trackingNumber" graphapi:"nullable"`
	Place          string `json:"place" graphapi:"nullable"`
}

// ListAgentAlertsArguments bound the listing.
type ListAgentAlertsArguments struct {
	// First is how many; twenty when left out, a hundred at most.
	First int `json:"first" graphapi:"nullable"`
}

// MuteAgentAlertArguments name what not to be told about.
type MuteAgentAlertArguments struct {
	AlertID    string `json:"alertId" graphapi:"nullable"`
	MuteScope  string `json:"muteScope" graphapi:"nullable"`
	MuteTarget string `json:"muteTarget" graphapi:"nullable"`
}

// UnmuteAgentAlertArguments name the mute.
type UnmuteAgentAlertArguments struct {
	MuteID string `json:"muteId"`
}

func (self *graph) ListAgentAlerts(ctx context.Context, arguments ListAgentAlertsArguments) ([]*agent.AlertView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	first := arguments.First
	if first <= 0 {
		first = 20
	}
	return agent.ListAlertViews(self.transaction(ctx), found.ID, min(first, 100))
}

func (self *graph) ListAgentAlertMutes(ctx context.Context) ([]*models.AgentAlertMute, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	return self.transaction(ctx).ListAgentAlertMutes(found.ID)
}

func (self *graph) MuteAgentAlert(ctx context.Context, arguments MuteAgentAlertArguments) (*models.AgentAlertMute, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(arguments.AlertID) == "" && strings.TrimSpace(arguments.MuteTarget) == "" {
		return nil, fmt.Errorf("%w: name an alert, or what to mute", api.ErrInvalidArguments)
	}
	mute, err := agent.MuteAlert(self.writing(ctx), found, arguments.AlertID, models.AlertMuteScope(strings.TrimSpace(arguments.MuteScope)), arguments.MuteTarget)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
	}
	log.Noticef("%s muted alerts about %s %q", operatorName(ctx), mute.MuteScope, mute.MuteTarget)
	return mute, nil
}

func (self *graph) UnmuteAgentAlert(ctx context.Context, arguments UnmuteAgentAlertArguments) (bool, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return false, err
	}
	isDeleted, err := self.writing(ctx).DeleteAgentAlertMute(found.ID, strings.TrimSpace(arguments.MuteID))
	if err != nil {
		return false, err
	}
	if !isDeleted {
		return false, api.ErrNotFound
	}
	return true, nil
}

func (self *graph) ListAgentFollowUps(ctx context.Context, arguments ListAgentFollowUpsArguments) ([]*models.AgentFollowUp, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	return agent.ListFollowUps(self.transaction(ctx), found.ID, arguments.IsOpenOnly == nil || *arguments.IsOpenOnly)
}

func (self *graph) StopAgentFollowUp(ctx context.Context, arguments StopAgentFollowUpArguments) (*models.AgentFollowUp, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	followUp, err := agent.StopFollowUp(self.writing(ctx), found.ID, arguments.FollowUpID)
	if err != nil {
		return nil, err
	}
	if followUp == nil {
		return nil, api.ErrNotFound
	}
	log.Noticef("%s stopped following %s %q", operatorName(ctx), followUp.FollowUpKind, followUp.FollowUpTitle)
	return followUp, nil
}

func (self *graph) AddAgentFollowUp(ctx context.Context, arguments AddAgentFollowUpArguments) (*models.AgentFollowUp, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	followUp, err := agent.AddFollowUp(self.writing(ctx), found.ID, agent.NewFollowUp{
		FollowUpKind: models.AgentFollowUpKind(arguments.FollowUpKind), FollowUpTitle: arguments.FollowUpTitle, DueOn: arguments.DueOn,
		Payee: arguments.Payee, Amount: arguments.Amount, CurrencyCode: arguments.CurrencyCode,
		Carrier: arguments.Carrier, TrackingNumber: arguments.TrackingNumber, Place: arguments.Place,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
	}
	log.Noticef("%s follows %s %q", operatorName(ctx), followUp.FollowUpKind, followUp.FollowUpTitle)
	return followUp, nil
}

func (self *graph) BackfillAgentFollowUps(ctx context.Context) (*agent.FollowUpBackfillView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	queuedMessageCount, err := agent.BackfillFollowUps(self.writing(ctx), found, time.Now(), true)
	if err != nil {
		return nil, err
	}
	log.Noticef("%s had their agent look back over past mail for follow-ups, %d messages queued", operatorName(ctx), queuedMessageCount)
	return &agent.FollowUpBackfillView{QueuedMessageCount: queuedMessageCount}, nil
}
