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
