package client

import (
	"context"
	"time"
)

// Passkey is an authenticator registered to an account.
type Passkey struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	CreatedAt      time.Time `json:"createdAt"`
	UsedAt         time.Time `json:"usedAt"`
	IP             string    `json:"ip"`
	Transports     []string  `json:"transports"`
	BackupEligible bool      `json:"backupEligible"`
	BackupState    bool      `json:"backupState"`
}

// PasskeyPolicy is whether this server offers passkeys, and how many an
// account may have.
type PasskeyPolicy struct {
	Enabled        bool `json:"enabled"`
	MaximumPerUser int  `json:"maximumPerUser"`
}

const passkeyFields = `{ id name createdAt usedAt ip transports backupEligible backupState }`

// ListPasskeys returns the passkeys of the account this client acts as.
func ListPasskeys(ctx context.Context, connection *Client) ([]*Passkey, error) {
	var result struct {
		ListPasskeys []*Passkey `json:"ListPasskeys"`
	}
	if err := connection.Execute(ctx, `query { ListPasskeys `+passkeyFields+` }`, nil, &result); err != nil {
		return nil, err
	}
	return result.ListPasskeys, nil
}

// GetPasskeyPolicy returns whether passkeys are offered at all.
func GetPasskeyPolicy(ctx context.Context, connection *Client) (*PasskeyPolicy, error) {
	var result struct {
		GetPasskeyPolicy *PasskeyPolicy `json:"GetPasskeyPolicy"`
	}
	if err := connection.Execute(ctx, `query { GetPasskeyPolicy { enabled maximumPerUser } }`, nil, &result); err != nil {
		return nil, err
	}
	return result.GetPasskeyPolicy, nil
}

// RenamePasskey changes what an authenticator is called.
func RenamePasskey(ctx context.Context, connection *Client, passkeyId, name string) (*Passkey, error) {
	var result struct {
		RenamePasskey *Passkey `json:"RenamePasskey"`
	}
	query := `mutation ($passkeyId: String!, $name: String!) {
		RenamePasskey(passkeyId: $passkeyId, name: $name) ` + passkeyFields + `
	}`
	if err := connection.Execute(ctx, query, map[string]any{"passkeyId": passkeyId, "name": name}, &result); err != nil {
		return nil, err
	}
	return result.RenamePasskey, nil
}

// DeletePasskey removes a passkey, so that authenticator can no longer sign in.
func DeletePasskey(ctx context.Context, connection *Client, passkeyId string) error {
	query := `mutation ($passkeyId: String!) { DeletePasskey(passkeyId: $passkeyId) }`
	return connection.Execute(ctx, query, map[string]any{"passkeyId": passkeyId}, nil)
}
