package apigraph

import (
	"context"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
)

// What a message carried, and what the person did about it.
//
// The agent reads a message it thinks carries an appointment or somebody's
// details and writes what it found onto the insight as a proposal. Nothing is
// in the calendar or the address book at that point: the reader draws a card
// from the proposal with the fields filled in, and the person adds it or
// waves it away. This is where their answer is recorded, so that a card they
// have dealt with does not come back.
//
// Adding it is the ordinary mutation for adding one -- SaveCalendarEvent or
// SaveContact -- called by the page with whatever is in the form, because the
// person may correct what was found before they keep it. This only says what
// became of the offer.

// SetMailProposalStatusArguments name one proposal on one message.
type SetMailProposalStatusArguments struct {
	ItemID string `json:"itemId"`

	// Index is the proposal's place in the list the reader was shown.
	Index int `json:"index"`

	// Status is "accepted" or "dismissed".
	Status string `json:"status"`
}

// SetMailProposalStatus records what the person did with one proposal.
func (self *graph) SetMailProposalStatus(ctx context.Context, arguments SetMailProposalStatusArguments) (*models.MailInsight, error) {
	items, mailbox, err := self.requireItems(ctx, models.PermissionMailWrite, []string{arguments.ItemID})
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, api.ErrNotFound
	}
	status := strings.ToLower(strings.TrimSpace(arguments.Status))
	switch status {
	case models.MailProposalAccepted, models.MailProposalDismissed:
	default:
		return nil, fmt.Errorf("%w: a proposal is accepted or dismissed", api.ErrInvalidArguments)
	}
	tx := self.transaction(ctx)
	insights, err := tx.GetMailInsights(mailbox.ID, []string{items[0].MailID})
	if err != nil {
		return nil, err
	}
	insight := insights[items[0].MailID]
	if insight == nil || arguments.Index < 0 || arguments.Index >= len(insight.Proposals) {
		return nil, api.ErrNotFound
	}
	insight.Proposals[arguments.Index].Status = status
	if err := tx.PutMailInsight(insight); err != nil {
		return nil, translateError(err)
	}
	return insight, nil
}
