package apigraph

import (
	"context"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
)

// Alerts: what the agent told the caller unasked, and what they asked not
// to be told about. The dashboard's Alerts card, the command line's agent
// alert and the agent's own agent_profile tool all call these, so the
// three cannot drift. The switches and the night are UpdateAgent's and
// GrantAgentMailbox's.

// AgentAlertQuery reads the caller's alerts and mutes.
type AgentAlertQuery interface {
	// The caller's latest alerts, newest first: what was said, when, its
	// subject key, whether it could not wait, and the messages it was
	// about. Needs agent:use.
	ListAgentAlerts(ctx context.Context, arguments ListAgentAlertsArguments) ([]*agent.AlertView, error)

	// What the caller asked not to be told about, newest first. Needs
	// agent:use.
	ListAgentAlertMutes(ctx context.Context) ([]*models.AgentAlertMute, error)
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
