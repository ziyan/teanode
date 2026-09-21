package apigraph

import (
	"bytes"
	"context"
	"encoding/json"
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
// Calendar acceptance binds the corrected fields to the original offer in one
// retained request. Contact saves still use their ordinary mutation followed by
// a status update. Dismissal only records what became of the offer.

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
	insight, err := tx.LockMailInsight(mailbox.ID, items[0].MailID)
	if err != nil {
		return nil, err
	}
	if insight == nil || arguments.Index < 0 || arguments.Index >= len(insight.Proposals) {
		return nil, api.ErrNotFound
	}
	insight.Proposals[arguments.Index].Status = status
	if err := tx.SetMailProposalStatus(mailbox.ID, items[0].MailID, arguments.Index, status); err != nil {
		return nil, translateError(err)
	}
	return insight, nil
}

// calendarProposal checks the exact offer the person saw before changing it.
func (self *graph) calendarProposal(ctx context.Context, arguments SaveCalendarEventArguments) (*models.MailInsight, error) {
	if arguments.ProposalIndex == nil || arguments.ExpectedProposal == "" {
		return nil, api.ErrInvalidArguments
	}
	items, mailbox, err := self.requireItems(ctx, models.PermissionMailWrite, []string{arguments.ProposalItemID})
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, api.ErrNotFound
	}
	insight, err := self.transaction(ctx).LockMailInsight(mailbox.ID, items[0].MailID)
	if err != nil {
		return nil, err
	}
	index := *arguments.ProposalIndex
	if insight == nil || index < 0 || index >= len(insight.Proposals) {
		return nil, api.ErrNotFound
	}
	proposal := insight.Proposals[index]
	if proposal.Kind != "event" || proposal.Status != "" {
		return nil, fmt.Errorf("%w: this proposal is no longer available", api.ErrInvalidArguments)
	}
	var expected models.MailProposal
	if err := json.Unmarshal([]byte(arguments.ExpectedProposal), &expected); err != nil {
		return nil, api.ErrInvalidArguments
	}
	original, err := json.Marshal(proposal)
	if err != nil {
		return nil, err
	}
	supplied, err := json.Marshal(expected)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(original, supplied) {
		return nil, fmt.Errorf("%w: this proposal changed; refresh it before accepting", api.ErrInvalidArguments)
	}
	return insight, nil
}
