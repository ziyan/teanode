package client

import (
	"context"
	"time"
)

// Session is one signed-in browser.
type Session struct {
	ID        string     `json:"id"`
	Current   bool       `json:"current"`
	Created   time.Time  `json:"created"`
	Expires   *time.Time `json:"expires"`
	LastUsed  *time.Time `json:"lastUsed"`
	IP        string     `json:"ip"`
	UserAgent string     `json:"userAgent"`
	Revoked   *time.Time `json:"revoked"`
}

const sessionFields = `{ id current created expires lastUsed ip userAgent revoked }`

// ListSessions returns an account's signed-in browsers, newest first. The
// console, which is not an account, names whose with username.
func ListSessions(ctx context.Context, connection *Client, username string, includeRevoked bool) ([]*Session, error) {
	var result struct {
		ListSessions []*Session `json:"ListSessions"`
	}
	query := `query ($username: String, $includeRevoked: Boolean) {
		ListSessions(username: $username, includeRevoked: $includeRevoked) ` + sessionFields + `
	}`
	variables := map[string]any{"includeRevoked": includeRevoked}
	if username != "" {
		variables["username"] = username
	}
	if err := connection.Execute(ctx, query, variables, &result); err != nil {
		return nil, err
	}
	return result.ListSessions, nil
}

// RevokeSession ends one signed-in browser.
func RevokeSession(ctx context.Context, connection *Client, sessionId string) error {
	query := `mutation ($sessionId: String!) { RevokeSession(sessionId: $sessionId) }`
	return connection.Execute(ctx, query, map[string]any{"sessionId": sessionId}, nil)
}

// RevokeAllSessions ends every browser the account is signed in on.
func RevokeAllSessions(ctx context.Context, connection *Client) error {
	return connection.Execute(ctx, `mutation { RevokeAllSessions { authenticated } }`, nil, nil)
}
