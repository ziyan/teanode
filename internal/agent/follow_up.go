package agent

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// What the person does with the agent's list of follow-ups: reads it, adds
// one they name, and stops one. The dashboard, the iPhone app, the command
// line and the agent_profile tool all reach these through the same GraphQL
// operations, so they hold to the same rules.

// FollowUpStoppedReason is the closed reason of a follow-up the person
// stopped.
const FollowUpStoppedReason = "stopped by the person"

// followUpsListedMost is how many follow-ups a listing of all of them
// reaches back: the newest, in any state.
const followUpsListedMost = 200

// ListFollowUps is the agent's follow-ups. Open only, the ones still being
// watched (open, overdue and stalled) by their day, the nearest first, and
// those with no day last; otherwise the newest of any state.
func ListFollowUps(tx db.Transaction, agentId string, isOpenOnly bool) ([]*models.AgentFollowUp, error) {
	if isOpenOnly {
		return tx.ListOpenAgentFollowUps(agentId)
	}
	return tx.ListAgentFollowUps(agentId, followUpsListedMost)
}

// NewFollowUp is a follow-up the person names themselves.
type NewFollowUp struct {
	FollowUpKind   models.AgentFollowUpKind
	FollowUpTitle  string
	DueOn          string
	Payee          string
	Amount         string
	CurrencyCode   string
	Carrier        string
	TrackingNumber string
	Place          string
}

// AddFollowUp keeps a follow-up the person names. It is held to what
// extract holds a follow-up read from mail to, since the sweep and the
// payment matching rely on it: a title; a day, for everything but a parcel;
// an amount that reads, for a bill. The key is made as extract makes it, so
// naming a bill the agent already found keeps the one row, and a message
// that comes later about it finds this row.
//
// A follow-up that is over (closed, expired or stopped) and is named again
// is opened again, and will be told about again, since the person asking is
// newer than whatever ended it. One still being watched is left in its
// state.
func AddFollowUp(tx db.Transaction, agentId string, input NewFollowUp) (*models.AgentFollowUp, error) {
	kind := models.AgentFollowUpKind(strings.ToLower(strings.TrimSpace(string(input.FollowUpKind))))
	if !kind.IsValid() {
		return nil, fmt.Errorf("%q is not bill, parcel, deadline or event", input.FollowUpKind)
	}
	title := cut(strings.TrimSpace(input.FollowUpTitle), 200)
	if title == "" {
		return nil, errors.New("say what to follow: a title")
	}
	dueOn := ""
	if text := strings.TrimSpace(input.DueOn); text != "" {
		day, err := time.Parse(time.DateOnly, text)
		if err != nil {
			return nil, fmt.Errorf("%q is not a day written YYYY-MM-DD", text)
		}
		dueOn = day.Format(time.DateOnly)
	}
	if dueOn == "" && kind != models.AgentFollowUpParcel {
		return nil, fmt.Errorf("a %s needs a day, written YYYY-MM-DD", kind)
	}
	amount := ""
	if kind == models.AgentFollowUpBill {
		if amount = parseFollowUpAmount(input.Amount); amount == "" {
			return nil, errors.New("a bill needs an amount")
		}
	}
	payee := cut(strings.TrimSpace(input.Payee), 200)
	carrier := cut(strings.TrimSpace(input.Carrier), 100)
	trackingNumber := cut(strings.TrimSpace(input.TrackingNumber), 100)
	place := cut(strings.TrimSpace(input.Place), 200)

	// What the same situation always gives: a parcel its tracking number,
	// a bill its payee and day, the others what is due and the day.
	var key string
	switch kind {
	case models.AgentFollowUpParcel:
		if key = trackingNumber; key == "" {
			key = strings.TrimSpace(carrier + " " + title)
		}
	case models.AgentFollowUpBill:
		name := payee
		if name == "" {
			name = title
		}
		key = name + " " + dueOn
	default:
		key = strings.TrimSpace(title + " " + dueOn)
	}
	followUpKey := FollowUpKeyOf(kind, key)
	if followUpKey == "" {
		return nil, errors.New("say what to follow: a title")
	}

	kept, _, err := tx.UpsertAgentFollowUp(&models.AgentFollowUp{
		AgentID: agentId, FollowUpKind: kind, FollowUpKey: followUpKey, FollowUpTitle: title,
		Payee: payee, Carrier: carrier, Amount: amount, CurrencyCode: cut(strings.ToUpper(strings.TrimSpace(input.CurrencyCode)), 3),
		TrackingNumber: trackingNumber, DueOn: dueOn, Place: place,
		FollowUpState: models.AgentFollowUpOpen, SourceItemIDs: []string{},
	})
	if err != nil {
		return nil, err
	}
	if kept.FollowUpState.IsOpen() {
		return kept, nil
	}
	return tx.UpdateAgentFollowUp(kept.ID, func(locked *models.AgentFollowUp) error {
		locked.FollowUpState, locked.ClosedReason = models.AgentFollowUpOpen, ""
		locked.ToldState, locked.ToldAt = "", nil
		return nil
	})
}

// StopFollowUp stops following one of the agent's follow-ups, or returns
// nil when the agent has none with that id. One that is already over is
// returned as it is: stopping a bill that was paid must not rewrite that it
// was paid.
func StopFollowUp(tx db.Transaction, agentId, followUpId string) (*models.AgentFollowUp, error) {
	found, err := tx.GetAgentFollowUps(agentId, []string{strings.TrimSpace(followUpId)})
	if err != nil || len(found) == 0 {
		return nil, err
	}
	if !found[0].FollowUpState.IsOpen() {
		return found[0], nil
	}
	return tx.UpdateAgentFollowUp(found[0].ID, func(locked *models.AgentFollowUp) error {
		if locked.FollowUpState.IsOpen() {
			locked.FollowUpState, locked.ClosedReason = models.AgentFollowUpStopped, FollowUpStoppedReason
		}
		return nil
	})
}
