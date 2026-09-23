package client

import (
	"context"
	"encoding/json"
)

// The source types installed on a server, and what its registry offers.

// AgentSourceType is an installed source type.
type AgentSourceType struct {
	Name        string                    `json:"name"`
	Description string                    `json:"description"`
	Version     string                    `json:"version"`
	Publisher   string                    `json:"publisher"`
	IsLocal     bool                      `json:"isLocal"`
	Reader      string                    `json:"reader"`
	Runs        []string                  `json:"runs"`
	Requires    []string                  `json:"requires"`
	Settings    []*AgentSourceTypeSetting `json:"settings"`
	Guide       string                    `json:"guide"`
	Readable    bool                      `json:"readable"`
	Problem     string                    `json:"problem,omitempty"`
}

// AgentSourceTypeSetting is one setting a type asks for.
type AgentSourceTypeSetting struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	SettingType string          `json:"settingType"`
	Pattern     string          `json:"pattern"`
	ItemPattern string          `json:"itemPattern"`
	Default     json.RawMessage `json:"default"`
	IsRequired  bool            `json:"isRequired"`
	Minimum     *int            `json:"minimum"`
	Maximum     *int            `json:"maximum"`
}

// AgentSourceTypeOffer is one type the registry publishes.
type AgentSourceTypeOffer struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Version     string   `json:"version"`
	Tags        []string `json:"tags"`
	Installed   string   `json:"installed,omitempty"`
	Newer       bool     `json:"newer"`
}

const (
	sourceTypeFields = `name description version publisher isLocal reader runs requires guide readable problem
    settings { name description settingType pattern itemPattern default isRequired minimum maximum }`

	DocumentListAgentSourceTypes = `query { ListAgentSourceTypes { ` + sourceTypeFields + ` } }`

	DocumentSearchAgentSourceTypes = `query ($query: String) {
  SearchAgentSourceTypes(query: $query) { name description version tags installed newer }
}`

	DocumentInstallAgentSourceType = `mutation ($name: String!) { InstallAgentSourceType(name: $name) { ` + sourceTypeFields + ` } }`

	DocumentAddLocalAgentSourceType = `mutation ($content: String!) { AddLocalAgentSourceType(content: $content) { ` + sourceTypeFields + ` } }`

	DocumentRemoveAgentSourceType = `mutation ($name: String!) { RemoveAgentSourceType(name: $name) }`
)

// ListAgentSourceTypes is what is installed here.
func ListAgentSourceTypes(ctx context.Context, connection *Client) ([]*AgentSourceType, error) {
	var result struct {
		ListAgentSourceTypes []*AgentSourceType `json:"ListAgentSourceTypes"`
	}
	if err := connection.Execute(ctx, DocumentListAgentSourceTypes, nil, &result); err != nil {
		return nil, err
	}
	return result.ListAgentSourceTypes, nil
}

// SearchAgentSourceTypes is what the registry offers.
func SearchAgentSourceTypes(ctx context.Context, connection *Client, query string) ([]*AgentSourceTypeOffer, error) {
	var result struct {
		SearchAgentSourceTypes []*AgentSourceTypeOffer `json:"SearchAgentSourceTypes"`
	}
	variables := map[string]any{}
	if query != "" {
		variables["query"] = query
	}
	if err := connection.Execute(ctx, DocumentSearchAgentSourceTypes, variables, &result); err != nil {
		return nil, err
	}
	return result.SearchAgentSourceTypes, nil
}

// InstallAgentSourceType installs one, or replaces it with a newer version.
func InstallAgentSourceType(ctx context.Context, connection *Client, name string) (*AgentSourceType, error) {
	var result struct {
		InstallAgentSourceType *AgentSourceType `json:"InstallAgentSourceType"`
	}
	if err := connection.Execute(ctx, DocumentInstallAgentSourceType, map[string]any{"name": name}, &result); err != nil {
		return nil, err
	}
	return result.InstallAgentSourceType, nil
}

// AddLocalAgentSourceType adds the operator's own file as a local type.
func AddLocalAgentSourceType(ctx context.Context, connection *Client, content string) (*AgentSourceType, error) {
	var result struct {
		AddLocalAgentSourceType *AgentSourceType `json:"AddLocalAgentSourceType"`
	}
	if err := connection.Execute(ctx, DocumentAddLocalAgentSourceType, map[string]any{"content": content}, &result); err != nil {
		return nil, err
	}
	return result.AddLocalAgentSourceType, nil
}

// RemoveAgentSourceType takes one away.
func RemoveAgentSourceType(ctx context.Context, connection *Client, name string) error {
	var result struct {
		RemoveAgentSourceType bool `json:"RemoveAgentSourceType"`
	}
	return connection.Execute(ctx, DocumentRemoveAgentSourceType, map[string]any{"name": name}, &result)
}
