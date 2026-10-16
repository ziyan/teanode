package client

import "context"

const DocumentServiceCapabilities = `query { GetServiceCapabilities { isMailRoutingEnabled isMailboxEnabled isCalendarEnabled isRemindersEnabled isContactsEnabled isAssistantEnabled hasPendingServiceRestart hasCompletedSetup } }`
const DocumentUpdateServices = `mutation ($isMailRoutingEnabled: Boolean, $isMailboxEnabled: Boolean, $isCalendarEnabled: Boolean, $isRemindersEnabled: Boolean, $isContactsEnabled: Boolean) { UpdateServices(isMailRoutingEnabled: $isMailRoutingEnabled, isMailboxEnabled: $isMailboxEnabled, isCalendarEnabled: $isCalendarEnabled, isRemindersEnabled: $isRemindersEnabled, isContactsEnabled: $isContactsEnabled) { isMailRoutingEnabled isMailboxEnabled isCalendarEnabled isRemindersEnabled isContactsEnabled isAssistantEnabled hasPendingServiceRestart hasCompletedSetup } }`

// ServiceCapabilities describes hosted features running on the server.
type ServiceCapabilities struct {
	IsMailRoutingEnabled     bool `json:"isMailRoutingEnabled"`
	IsMailboxEnabled         bool `json:"isMailboxEnabled"`
	IsCalendarEnabled        bool `json:"isCalendarEnabled"`
	IsRemindersEnabled       bool `json:"isRemindersEnabled"`
	IsContactsEnabled        bool `json:"isContactsEnabled"`
	IsAssistantEnabled       bool `json:"isAssistantEnabled"`
	HasPendingServiceRestart bool `json:"hasPendingServiceRestart"`
	HasCompletedSetup        bool `json:"hasCompletedSetup"`
}

func (self *Client) ServiceCapabilities(ctx context.Context) (*ServiceCapabilities, error) {
	var response struct {
		GetServiceCapabilities ServiceCapabilities `json:"GetServiceCapabilities"`
	}
	if err := self.Execute(ctx, DocumentServiceCapabilities, nil, &response); err != nil {
		return nil, err
	}
	return &response.GetServiceCapabilities, nil
}
func (self *Client) UpdateServices(ctx context.Context, selectedServices map[string]any) (*ServiceCapabilities, error) {
	var response struct {
		UpdateServices ServiceCapabilities `json:"UpdateServices"`
	}
	if err := self.Execute(ctx, DocumentUpdateServices, selectedServices, &response); err != nil {
		return nil, err
	}
	return &response.UpdateServices, nil
}

const DocumentSetUpAssistant = `mutation ($kind: String!, $apiKey: String, $baseUrl: String, $model: String) { SetUpAssistant(kind: $kind, apiKey: $apiKey, baseUrl: $baseUrl, model: $model) { provider defaultModel embeddingModel fastModel isAgentOn needsRestart } }`

// AssistantSetup is what setting up the assistant in one step did.
type AssistantSetup struct {
	Provider       string `json:"provider"`
	DefaultModel   string `json:"defaultModel"`
	EmbeddingModel string `json:"embeddingModel"`
	FastModel      string `json:"fastModel"`
	IsAgentOn      bool   `json:"isAgentOn"`
	NeedsRestart   bool   `json:"needsRestart"`
}

func (self *Client) SetUpAssistant(ctx context.Context, variables map[string]any) (*AssistantSetup, error) {
	var response struct {
		SetUpAssistant AssistantSetup `json:"SetUpAssistant"`
	}
	if err := self.Execute(ctx, DocumentSetUpAssistant, variables, &response); err != nil {
		return nil, err
	}
	return &response.SetUpAssistant, nil
}
