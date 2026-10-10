package client

import (
	"context"
	"time"
)

// OAuthDevicePairing is a device waiting for its pairing code to be approved:
// the TeaNode phone app, showing a code such as KQ7M-4TXD.
type OAuthDevicePairing struct {
	UserCode     string    `json:"userCode"`
	ClientName   string    `json:"clientName"`
	DeviceName   string    `json:"deviceName"`
	Username     string    `json:"username"`
	IsFullAccess bool      `json:"isFullAccess"`
	IsApproved   bool      `json:"isApproved"`
	IsDenied     bool      `json:"isDenied"`
	ExpiresAt    time.Time `json:"expiresAt"`
}

const oauthDevicePairingFields = `userCode clientName deviceName username isFullAccess isApproved isDenied expiresAt`

const (
	DocumentReadOAuthDevicePairing = `query ($userCode: String!) {
  ReadOAuthDevicePairing(userCode: $userCode) { ` + oauthDevicePairingFields + ` }
}`
	DocumentDecideOAuthDevicePairing = `mutation ($userCode: String!, $isApproved: Boolean!) {
  DecideOAuthDevicePairing(userCode: $userCode, isApproved: $isApproved) { ` + oauthDevicePairingFields + ` }
}`
)

// ReadOAuthDevicePairing describes the device showing a pairing code.
func ReadOAuthDevicePairing(ctx context.Context, connection *Client, userCode string) (*OAuthDevicePairing, error) {
	var result struct {
		ReadOAuthDevicePairing *OAuthDevicePairing `json:"ReadOAuthDevicePairing"`
	}
	if err := connection.Execute(ctx, DocumentReadOAuthDevicePairing, map[string]any{"userCode": userCode}, &result); err != nil {
		return nil, err
	}
	return result.ReadOAuthDevicePairing, nil
}

// DecideOAuthDevicePairing approves the device showing a pairing code,
// signing it in as you, or refuses it.
func DecideOAuthDevicePairing(ctx context.Context, connection *Client, userCode string, isApproved bool) (*OAuthDevicePairing, error) {
	var result struct {
		DecideOAuthDevicePairing *OAuthDevicePairing `json:"DecideOAuthDevicePairing"`
	}
	if err := connection.Execute(ctx, DocumentDecideOAuthDevicePairing, map[string]any{"userCode": userCode, "isApproved": isApproved}, &result); err != nil {
		return nil, err
	}
	return result.DecideOAuthDevicePairing, nil
}
