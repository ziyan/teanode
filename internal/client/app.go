package client

import (
	"context"
	"time"
)

// App is a program somebody authorized, holding a token of theirs that it
// renews by itself.
type App struct {
	ClientID       string     `json:"clientId"`
	Name           string     `json:"name"`
	RegisteredName string     `json:"registeredName"`
	RedirectHosts  []string   `json:"redirectHosts"`
	Registered     *time.Time `json:"registered"`
	LastUsed       *time.Time `json:"lastUsed"`
	LastUsedIP     string     `json:"lastUsedIp"`
	Expires        *time.Time `json:"expires"`
	RenewableUntil *time.Time `json:"renewableUntil"`
	TokenCount     int        `json:"tokenCount"`
}

// ListApps returns the apps holding a token of the account this client is
// authenticated as, or of username on the console.
func ListApps(ctx context.Context, connection *Client, username string) ([]*App, error) {
	var result struct {
		ListApps []*App `json:"ListApps"`
	}
	query := `query ($username: String) {
		ListApps(username: $username) {
			clientId name registeredName redirectHosts registered lastUsed lastUsedIp expires renewableUntil tokenCount
		}
	}`
	variables := map[string]any{}
	if username != "" {
		variables["username"] = username
	}
	if err := connection.Execute(ctx, query, variables, &result); err != nil {
		return nil, err
	}
	return result.ListApps, nil
}

// RenameApp names an app on every token it holds, and on each it renews into.
func RenameApp(ctx context.Context, connection *Client, clientId, username, name string) error {
	query := `mutation ($clientId: String!, $name: String!, $username: String) { RenameApp(clientId: $clientId, name: $name, username: $username) }`
	variables := map[string]any{"clientId": clientId, "name": name}
	if username != "" {
		variables["username"] = username
	}
	return connection.Execute(ctx, query, variables, nil)
}

// DisconnectApp revokes every token an app holds.
func DisconnectApp(ctx context.Context, connection *Client, clientId, username string) error {
	query := `mutation ($clientId: String!, $username: String) { DisconnectApp(clientId: $clientId, username: $username) }`
	variables := map[string]any{"clientId": clientId}
	if username != "" {
		variables["username"] = username
	}
	return connection.Execute(ctx, query, variables, nil)
}
