package client

import (
	"context"
	"time"
)

// Token is an issued API token, without its secret.
type Token struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Username   string     `json:"username"`
	Created    time.Time  `json:"created"`
	Expires    *time.Time `json:"expires"`
	LastUsed   *time.Time `json:"lastUsed"`
	LastUsedIP string     `json:"lastUsedIp"`
	Revoked    *time.Time `json:"revoked"`

	IsHeldByProgram bool `json:"isHeldByProgram"`
}

const tokenFields = `id name username created expires lastUsed lastUsedIp revoked isHeldByProgram`

// ListTokens returns the tokens belonging to the account this client is
// authenticated as.
//
// There is no listing somebody else's: a token acts as the person it belongs
// to, so a list of theirs is a list of ways to become them. The console,
// which is not an account, names whose with username.
func ListTokens(ctx context.Context, connection *Client, username string, includeRevoked bool) ([]*Token, error) {
	var result struct {
		ListTokens []*Token `json:"ListTokens"`
	}
	query := `query ($username: String, $includeRevoked: Boolean) {
		ListTokens(username: $username, includeRevoked: $includeRevoked) { ` + tokenFields + ` }
	}`
	variables := map[string]any{"includeRevoked": includeRevoked}
	if username != "" {
		variables["username"] = username
	}
	if err := connection.Execute(ctx, query, variables, &result); err != nil {
		return nil, err
	}
	return result.ListTokens, nil
}

// CreateToken issues a token and returns it. The secret is not recoverable
// afterwards.
func CreateToken(ctx context.Context, connection *Client, name, username, lifetime string) (*Token, string, error) {
	var result struct {
		CreateToken struct {
			Token  *Token `json:"token"`
			Secret string `json:"secret"`
		} `json:"CreateToken"`
	}
	query := `mutation ($name: String!, $username: String, $lifetime: String) {
		CreateToken(name: $name, username: $username, lifetime: $lifetime) {
			token { ` + tokenFields + ` }
			secret
		}
	}`
	variables := map[string]any{"name": name}
	if username != "" {
		variables["username"] = username
	}
	if lifetime != "" {
		variables["lifetime"] = lifetime
	}
	if err := connection.Execute(ctx, query, variables, &result); err != nil {
		return nil, "", err
	}
	return result.CreateToken.Token, result.CreateToken.Secret, nil
}

// UpdateToken renames a token or gives it a new lifetime counted from now,
// "never" for one that does not expire. An empty name or lifetime leaves
// that as it is.
func UpdateToken(ctx context.Context, connection *Client, id, username, name, lifetime string) (*Token, error) {
	var result struct {
		UpdateToken *Token `json:"UpdateToken"`
	}
	query := `mutation ($tokenId: String!, $username: String, $name: String, $lifetime: String) {
		UpdateToken(tokenId: $tokenId, username: $username, name: $name, lifetime: $lifetime) { ` + tokenFields + ` }
	}`
	variables := map[string]any{"tokenId": id}
	if username != "" {
		variables["username"] = username
	}
	if name != "" {
		variables["name"] = name
	}
	if lifetime != "" {
		variables["lifetime"] = lifetime
	}
	if err := connection.Execute(ctx, query, variables, &result); err != nil {
		return nil, err
	}
	return result.UpdateToken, nil
}

// DeleteToken revokes a token.
func DeleteToken(ctx context.Context, connection *Client, id, username string) error {
	query := `mutation ($tokenId: String!, $username: String) { DeleteToken(tokenId: $tokenId, username: $username) }`
	variables := map[string]any{"tokenId": id, "username": nil}
	if username != "" {
		variables["username"] = username
	}
	return connection.Execute(ctx, query, variables, nil)
}
