package client

import (
	"context"
	"time"
)

// Follow-ups: what the agent keeps track of from mail until something
// closes it (a bill, a parcel, a deadline, a coming event), and what the
// person asks it to follow or to stop following.

// AgentFollowUp is one follow-up. DueOn is a day written YYYY-MM-DD, and the
// fields a kind does not use are empty.
type AgentFollowUp struct {
	ID             string    `json:"id"`
	FollowUpKind   string    `json:"followUpKind"`
	FollowUpKey    string    `json:"followUpKey"`
	FollowUpTitle  string    `json:"followUpTitle"`
	Payee          string    `json:"payee"`
	Carrier        string    `json:"carrier"`
	Amount         string    `json:"amount"`
	CurrencyCode   string    `json:"currencyCode"`
	TrackingNumber string    `json:"trackingNumber"`
	DueOn          string    `json:"dueOn"`
	Place          string    `json:"place"`
	SourceURL      string    `json:"sourceUrl"`
	FollowUpState  string    `json:"followUpState"`
	ClosedReason   string    `json:"closedReason"`
	ToldState      string    `json:"toldState"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// NewAgentFollowUp is a follow-up the person names.
type NewAgentFollowUp struct {
	FollowUpKind   string
	FollowUpTitle  string
	DueOn          string
	Payee          string
	Amount         string
	CurrencyCode   string
	Carrier        string
	TrackingNumber string
	Place          string
}

const agentFollowUpFields = `{ id followUpKind followUpKey followUpTitle payee carrier amount currencyCode trackingNumber dueOn place sourceUrl followUpState closedReason toldState createdAt updatedAt }`

const (
	DocumentListAgentFollowUps = `query ($isOpenOnly: Boolean) { ListAgentFollowUps(isOpenOnly: $isOpenOnly) ` + agentFollowUpFields + ` }`

	DocumentStopAgentFollowUp = `mutation ($followUpId: String!) { StopAgentFollowUp(followUpId: $followUpId) ` + agentFollowUpFields + ` }`

	DocumentBackfillAgentFollowUps = `mutation { BackfillAgentFollowUps { queuedMessageCount } }`

	DocumentAddAgentFollowUp = `mutation ($followUpKind: String!, $followUpTitle: String!, $dueOn: String, $payee: String, $amount: String, $currencyCode: String, $carrier: String, $trackingNumber: String, $place: String) {
  AddAgentFollowUp(followUpKind: $followUpKind, followUpTitle: $followUpTitle, dueOn: $dueOn, payee: $payee, amount: $amount, currencyCode: $currencyCode, carrier: $carrier, trackingNumber: $trackingNumber, place: $place) ` + agentFollowUpFields + `
}`
)

// ListAgentFollowUps is what the agent is following: only what is still
// being watched, or, with isOpenOnly false, the newest of any state.
func ListAgentFollowUps(ctx context.Context, connection *Client, isOpenOnly bool) ([]*AgentFollowUp, error) {
	var result struct {
		ListAgentFollowUps []*AgentFollowUp `json:"ListAgentFollowUps"`
	}
	if err := connection.Execute(ctx, DocumentListAgentFollowUps, map[string]any{"isOpenOnly": isOpenOnly}, &result); err != nil {
		return nil, err
	}
	return result.ListAgentFollowUps, nil
}

// StopAgentFollowUp stops following one.
func StopAgentFollowUp(ctx context.Context, connection *Client, followUpId string) (*AgentFollowUp, error) {
	var result struct {
		StopAgentFollowUp *AgentFollowUp `json:"StopAgentFollowUp"`
	}
	if err := connection.Execute(ctx, DocumentStopAgentFollowUp, map[string]any{"followUpId": followUpId}, &result); err != nil {
		return nil, err
	}
	return result.StopAgentFollowUp, nil
}

// AddAgentFollowUp follows something the person names; what is left empty
// is not sent.
func AddAgentFollowUp(ctx context.Context, connection *Client, followUp NewAgentFollowUp) (*AgentFollowUp, error) {
	var result struct {
		AddAgentFollowUp *AgentFollowUp `json:"AddAgentFollowUp"`
	}
	variables := map[string]any{"followUpKind": followUp.FollowUpKind, "followUpTitle": followUp.FollowUpTitle}
	for name, given := range map[string]string{
		"dueOn": followUp.DueOn, "payee": followUp.Payee, "amount": followUp.Amount, "currencyCode": followUp.CurrencyCode,
		"carrier": followUp.Carrier, "trackingNumber": followUp.TrackingNumber, "place": followUp.Place,
	} {
		if given != "" {
			variables[name] = given
		}
	}
	if err := connection.Execute(ctx, DocumentAddAgentFollowUp, variables, &result); err != nil {
		return nil, err
	}
	return result.AddAgentFollowUp, nil
}

// BackfillAgentFollowUps has the agent look back over the last 45 days of
// mail for bills, parcels and notices, and answers how many messages it
// queued to be read.
func BackfillAgentFollowUps(ctx context.Context, connection *Client) (int, error) {
	var result struct {
		BackfillAgentFollowUps struct {
			QueuedMessageCount int `json:"queuedMessageCount"`
		} `json:"BackfillAgentFollowUps"`
	}
	if err := connection.Execute(ctx, DocumentBackfillAgentFollowUps, nil, &result); err != nil {
		return 0, err
	}
	return result.BackfillAgentFollowUps.QueuedMessageCount, nil
}
