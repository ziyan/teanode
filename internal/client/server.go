package client

import (
	"context"
	"strconv"
	"time"
)

// ServerStatus describes the running instance rather than the configuration.
type ServerStatus struct {
	Instance       string    `json:"instance"`
	Version        string    `json:"version"`
	Commit         string    `json:"commit"`
	StartedAt      time.Time `json:"startedAt"`
	UptimeSeconds  int64     `json:"uptimeSeconds"`
	PendingRestart []string  `json:"pendingRestart"`
	Supervision    string    `json:"supervision"`
	Restarting     bool      `json:"restarting"`
}

// GetServerStatus returns what the instance is and how it is running.
func GetServerStatus(ctx context.Context, connection *Client) (*ServerStatus, error) {
	var result struct {
		GetServerStatus *ServerStatus `json:"GetServerStatus"`
	}
	query := `query { GetServerStatus { instance version commit startedAt uptimeSeconds pendingRestart supervision restarting } }`
	if err := connection.Execute(ctx, query, nil, &result); err != nil {
		return nil, err
	}
	return result.GetServerStatus, nil
}

// RestartResult is what the server says when asked to restart.
type RestartResult struct {
	Started     bool   `json:"started"`
	Instance    string `json:"instance"`
	Supervision string `json:"supervision"`
}

// RestartServer asks the instance to exit so its supervisor starts a new one.
func RestartServer(ctx context.Context, connection *Client) (*RestartResult, error) {
	var result struct {
		RestartServer *RestartResult `json:"RestartServer"`
	}
	if err := connection.Execute(ctx, `mutation { RestartServer { started instance supervision } }`, nil, &result); err != nil {
		return nil, err
	}
	return result.RestartServer, nil
}

// itoa is strconv.Itoa under a shorter name for the table helpers here.
func itoa(value int) string {
	return strconv.Itoa(value)
}
