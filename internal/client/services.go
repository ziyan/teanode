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
