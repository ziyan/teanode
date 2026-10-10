package client

import (
	"context"
	"time"
)

// OAuthConnectCode is a one-time code that connects the TeaNode phone app to
// the server as you: a link, and the same link as a QR code.
type OAuthConnectCode struct {
	ConnectLink string    `json:"connectLink"`
	QRCodeSVG   string    `json:"qrCodeSvg"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

const DocumentCreateOAuthConnectCode = `mutation {
  CreateOAuthConnectCode { connectLink qrCodeSvg expiresAt }
}`

// CreateOAuthConnectCode makes a code for a phone to scan.
func CreateOAuthConnectCode(ctx context.Context, connection *Client) (*OAuthConnectCode, error) {
	var result struct {
		CreateOAuthConnectCode *OAuthConnectCode `json:"CreateOAuthConnectCode"`
	}
	if err := connection.Execute(ctx, DocumentCreateOAuthConnectCode, nil, &result); err != nil {
		return nil, err
	}
	return result.CreateOAuthConnectCode, nil
}

// AgentPhone is one phone a person talks to their agent from.
type AgentPhone struct {
	ID                string     `json:"id"`
	PhoneName         string     `json:"phoneName"`
	PhoneModel        string     `json:"phoneModel"`
	NotificationKinds []string   `json:"notificationKinds"`
	CanBeNotified     bool       `json:"canBeNotified"`
	CreatedAt         time.Time  `json:"createdAt"`
	LastSeenAt        *time.Time `json:"lastSeenAt"`
	LastDeliveredAt   *time.Time `json:"lastDeliveredAt"`
	LastDeliveryError string     `json:"lastDeliveryError"`
}

// AgentPhoneList is a person's phones and whether notifications go out.
type AgentPhoneList struct {
	Phones            []*AgentPhone `json:"phones"`
	IsPushEnabled     bool          `json:"isPushEnabled"`
	NotificationKinds []string      `json:"notificationKinds"`
}

const agentPhoneFields = `id phoneName phoneModel notificationKinds canBeNotified createdAt lastSeenAt lastDeliveredAt lastDeliveryError`

const (
	DocumentListAgentPhones = `query {
  ListAgentPhones { phones { ` + agentPhoneFields + ` } isPushEnabled notificationKinds }
}`
	DocumentUpdateAgentPhone = `mutation ($phoneId: String!, $phoneName: String, $notificationKinds: [String!]) {
  UpdateAgentPhone(phoneId: $phoneId, phoneName: $phoneName, notificationKinds: $notificationKinds) { ` + agentPhoneFields + ` }
}`
	DocumentRemoveAgentPhone = `mutation ($phoneId: String!) {
  RemoveAgentPhone(phoneId: $phoneId)
}`
	DocumentTestAgentPhone = `mutation ($phoneId: String!) {
  TestAgentPhone(phoneId: $phoneId) { ` + agentPhoneFields + ` }
}`
)

// ListAgentPhones lists the caller's phones.
func ListAgentPhones(ctx context.Context, connection *Client) (*AgentPhoneList, error) {
	var result struct {
		ListAgentPhones *AgentPhoneList `json:"ListAgentPhones"`
	}
	if err := connection.Execute(ctx, DocumentListAgentPhones, nil, &result); err != nil {
		return nil, err
	}
	return result.ListAgentPhones, nil
}

// UpdateAgentPhone renames a phone or chooses its notifications; nil leaves
// either as it is.
func UpdateAgentPhone(ctx context.Context, connection *Client, phoneId string, phoneName *string, notificationKinds *[]string) (*AgentPhone, error) {
	variables := map[string]any{"phoneId": phoneId}
	if phoneName != nil {
		variables["phoneName"] = *phoneName
	}
	if notificationKinds != nil {
		variables["notificationKinds"] = *notificationKinds
	}
	var result struct {
		UpdateAgentPhone *AgentPhone `json:"UpdateAgentPhone"`
	}
	if err := connection.Execute(ctx, DocumentUpdateAgentPhone, variables, &result); err != nil {
		return nil, err
	}
	return result.UpdateAgentPhone, nil
}

// RemoveAgentPhone removes a phone and signs it out.
func RemoveAgentPhone(ctx context.Context, connection *Client, phoneId string) error {
	var result struct {
		RemoveAgentPhone bool `json:"RemoveAgentPhone"`
	}
	return connection.Execute(ctx, DocumentRemoveAgentPhone, map[string]any{"phoneId": phoneId}, &result)
}

// TestAgentPhone sends a phone a notification that says it can be reached.
func TestAgentPhone(ctx context.Context, connection *Client, phoneId string) (*AgentPhone, error) {
	var result struct {
		TestAgentPhone *AgentPhone `json:"TestAgentPhone"`
	}
	if err := connection.Execute(ctx, DocumentTestAgentPhone, map[string]any{"phoneId": phoneId}, &result); err != nil {
		return nil, err
	}
	return result.TestAgentPhone, nil
}
