package client

import "context"

// The skills installed on a server, and what its registry offers.

// AgentSkill is an installed skill.
type AgentSkill struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Version     string            `json:"version"`
	Publisher   string            `json:"publisher"`
	Enabled     bool              `json:"enabled"`
	Readable    bool              `json:"readable"`
	Problem     string            `json:"problem,omitempty"`
	Secrets     []string          `json:"secrets"`
	Tools       []*AgentSkillTool `json:"tools"`
}

// AgentSkillTool is one tool a skill declares.
type AgentSkillTool struct {
	Name          string `json:"name"`
	Description   string `json:"description"`
	Kind          string `json:"kind"`
	NeedsComputer bool   `json:"needsComputer"`
}

// AgentSkillOffer is one skill the registry publishes.
type AgentSkillOffer struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Version     string   `json:"version"`
	Tags        []string `json:"tags"`
	Installed   string   `json:"installed,omitempty"`
	Newer       bool     `json:"newer"`
}

const (
	documentListAgentSkills = `query {
  ListAgentSkills { name description version publisher enabled readable problem secrets
    tools { name description kind needsComputer } }
}`

	documentSearchAgentSkills = `query ($query: String) {
  SearchAgentSkills(query: $query) { name description version tags installed newer }
}`

	documentInstallAgentSkill = `mutation ($name: String!) {
  InstallAgentSkill(name: $name) { name description version publisher enabled readable problem secrets
    tools { name description kind needsComputer } }
}`

	documentRemoveAgentSkill = `mutation ($name: String!) { RemoveAgentSkill(name: $name) }`

	documentSetAgentSkillEnabled = `mutation ($name: String!, $enabled: Boolean!) {
  SetAgentSkillEnabled(name: $name, enabled: $enabled) { name enabled }
}`
)

// ListAgentSkills is what is installed here.
func ListAgentSkills(ctx context.Context, connection *Client) ([]*AgentSkill, error) {
	var result struct {
		ListAgentSkills []*AgentSkill `json:"ListAgentSkills"`
	}
	if err := connection.Execute(ctx, documentListAgentSkills, nil, &result); err != nil {
		return nil, err
	}
	return result.ListAgentSkills, nil
}

// SearchAgentSkills is what the registry offers.
func SearchAgentSkills(ctx context.Context, connection *Client, query string) ([]*AgentSkillOffer, error) {
	var result struct {
		SearchAgentSkills []*AgentSkillOffer `json:"SearchAgentSkills"`
	}
	variables := map[string]any{}
	if query != "" {
		variables["query"] = query
	}
	if err := connection.Execute(ctx, documentSearchAgentSkills, variables, &result); err != nil {
		return nil, err
	}
	return result.SearchAgentSkills, nil
}

// InstallAgentSkill installs one, or replaces it with a newer version.
func InstallAgentSkill(ctx context.Context, connection *Client, name string) (*AgentSkill, error) {
	var result struct {
		InstallAgentSkill *AgentSkill `json:"InstallAgentSkill"`
	}
	if err := connection.Execute(ctx, documentInstallAgentSkill, map[string]any{"name": name}, &result); err != nil {
		return nil, err
	}
	return result.InstallAgentSkill, nil
}

// RemoveAgentSkill takes one away.
func RemoveAgentSkill(ctx context.Context, connection *Client, name string) error {
	var result struct {
		RemoveAgentSkill bool `json:"RemoveAgentSkill"`
	}
	return connection.Execute(ctx, documentRemoveAgentSkill, map[string]any{"name": name}, &result)
}

// SetAgentSkillEnabled offers a skill's tools or stops offering them.
func SetAgentSkillEnabled(ctx context.Context, connection *Client, name string, enabled bool) (*AgentSkill, error) {
	var result struct {
		SetAgentSkillEnabled *AgentSkill `json:"SetAgentSkillEnabled"`
	}
	if err := connection.Execute(ctx, documentSetAgentSkillEnabled, map[string]any{"name": name, "enabled": enabled}, &result); err != nil {
		return nil, err
	}
	return result.SetAgentSkillEnabled, nil
}
