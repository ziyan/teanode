package client

import (
	"context"
	"time"
)

// Alerts: what the agent told the person unasked, and what they asked not
// to be told about.

// AgentAlert is one alert, with the messages it was about.
type AgentAlert struct {
	ID         string               `json:"id"`
	AlertText  string               `json:"alertText"`
	SentAt     time.Time            `json:"sentAt"`
	SubjectKey string               `json:"subjectKey"`
	IsUrgent   bool                 `json:"isUrgent"`
	Covered    []*AgentAlertCovered `json:"covered"`

	// MuteChoices is what a mute of the alert offers, the default first.
	MuteChoices []*AgentAlertMuteChoice `json:"muteChoices"`
}

// AgentAlertMuteChoice is one way to mute an alert, and what it mutes.
type AgentAlertMuteChoice struct {
	MuteScope  string `json:"muteScope"`
	MuteTarget string `json:"muteTarget"`
}

// AgentAlertCovered is one message an alert was about.
type AgentAlertCovered struct {
	MailID        string `json:"mailId"`
	Subject       string `json:"subject"`
	FromAddress   string `json:"fromAddress"`
	CandidateKind string `json:"candidateKind"`
	MailCategory  string `json:"mailCategory"`
}

// AgentAlertMute is one thing the person asked not to be told about.
type AgentAlertMute struct {
	ID         string    `json:"id"`
	MuteScope  string    `json:"muteScope"`
	MuteTarget string    `json:"muteTarget"`
	AlertID    string    `json:"alertId"`
	CreatedAt  time.Time `json:"createdAt"`
}

const agentAlertMuteFields = `{ id muteScope muteTarget alertId createdAt }`

const (
	DocumentListAgentAlerts = `query ($first: Int) {
  ListAgentAlerts(first: $first) { id alertText sentAt subjectKey isUrgent
    covered { mailId subject fromAddress candidateKind mailCategory }
    muteChoices { muteScope muteTarget } }
}`

	DocumentListAgentAlertMutes = `query { ListAgentAlertMutes ` + agentAlertMuteFields + ` }`

	DocumentMuteAgentAlert = `mutation ($alertId: String, $muteScope: String, $muteTarget: String) {
  MuteAgentAlert(alertId: $alertId, muteScope: $muteScope, muteTarget: $muteTarget) ` + agentAlertMuteFields + `
}`

	DocumentUnmuteAgentAlert = `mutation ($muteId: String!) { UnmuteAgentAlert(muteId: $muteId) }`
)

// ListAgentAlerts is the latest alerts, newest first; first of zero is the
// server's default.
func ListAgentAlerts(ctx context.Context, connection *Client, first int) ([]*AgentAlert, error) {
	var result struct {
		ListAgentAlerts []*AgentAlert `json:"ListAgentAlerts"`
	}
	variables := map[string]any{}
	if first > 0 {
		variables["first"] = first
	}
	if err := connection.Execute(ctx, DocumentListAgentAlerts, variables, &result); err != nil {
		return nil, err
	}
	return result.ListAgentAlerts, nil
}

// ListAgentAlertMutes is what the person asked not to be told about.
func ListAgentAlertMutes(ctx context.Context, connection *Client) ([]*AgentAlertMute, error) {
	var result struct {
		ListAgentAlertMutes []*AgentAlertMute `json:"ListAgentAlertMutes"`
	}
	if err := connection.Execute(ctx, DocumentListAgentAlertMutes, nil, &result); err != nil {
		return nil, err
	}
	return result.ListAgentAlertMutes, nil
}

// MuteAgentAlert stops the agent telling the person about something: by
// an alert, a scope and a target, or both.
func MuteAgentAlert(ctx context.Context, connection *Client, alertId, muteScope, muteTarget string) (*AgentAlertMute, error) {
	var result struct {
		MuteAgentAlert *AgentAlertMute `json:"MuteAgentAlert"`
	}
	variables := map[string]any{}
	for name, given := range map[string]string{"alertId": alertId, "muteScope": muteScope, "muteTarget": muteTarget} {
		if given != "" {
			variables[name] = given
		}
	}
	if err := connection.Execute(ctx, DocumentMuteAgentAlert, variables, &result); err != nil {
		return nil, err
	}
	return result.MuteAgentAlert, nil
}

// UnmuteAgentAlert takes a mute back.
func UnmuteAgentAlert(ctx context.Context, connection *Client, muteId string) error {
	var result struct {
		UnmuteAgentAlert bool `json:"UnmuteAgentAlert"`
	}
	return connection.Execute(ctx, DocumentUnmuteAgentAlert, map[string]any{"muteId": muteId}, &result)
}
