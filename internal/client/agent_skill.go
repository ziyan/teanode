package client

import "context"

// The skills installed on a server, and what its registry offers.

// AgentSkill is an installed skill.
type AgentSkill struct {
	Name            string            `json:"name"`
	Description     string            `json:"description"`
	Version         string            `json:"version"`
	Publisher       string            `json:"publisher"`
	Enabled         bool              `json:"enabled"`
	Readable        bool              `json:"readable"`
	Problem         string            `json:"problem,omitempty"`
	Secrets         []string          `json:"secrets"`
	PersonalSecrets []string          `json:"personalSecrets"`
	Scope           string            `json:"scope"`
	Tools           []*AgentSkillTool `json:"tools"`
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
  ListAgentSkills { name description version publisher enabled readable problem scope secrets personalSecrets
    tools { name description kind needsComputer } }
}`

	documentSearchAgentSkills = `query ($query: String) {
  SearchAgentSkills(query: $query) { name description version tags installed newer }
}`

	documentInstallAgentSkill = `mutation ($name: String!) {
  InstallAgentSkill(name: $name) { name description version publisher enabled readable problem scope secrets personalSecrets
    tools { name description kind needsComputer } }
}`

	documentRemoveAgentSkill = `mutation ($name: String!) { RemoveAgentSkill(name: $name) }`

	documentSetAgentSkillEnabled = `mutation ($name: String!, $enabled: Boolean!) {
  SetAgentSkillEnabled(name: $name, enabled: $enabled) { name enabled }
}`

	documentSetAgentSkillScope = `mutation ($name: String!, $scope: String!) {
  SetAgentSkillScope(name: $name, scope: $scope) { name scope secrets personalSecrets }
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

// AgentSkillSecret is one value an installed skill asks a person for.
type AgentSkillSecret struct {
	Skill       string `json:"skill"`
	Key         string `json:"key"`
	Description string `json:"description"`
	Set         bool   `json:"set"`
}

const (
	documentAgentSkillSecrets = `query { ListAgentSkillSecrets { skill key description set } }`

	documentSetAgentSkillSecret = `mutation ($skill: String!, $key: String!, $value: String!) {
  SetAgentSkillSecret(skill: $skill, key: $key, value: $value) { skill key set }
}`

	documentClearAgentSkillSecret = `mutation ($skill: String!, $key: String!) { ClearAgentSkillSecret(skill: $skill, key: $key) }`
)

// ListAgentSkillSecrets is what the installed skills ask this person for.
func ListAgentSkillSecrets(ctx context.Context, connection *Client) ([]*AgentSkillSecret, error) {
	var result struct {
		ListAgentSkillSecrets []*AgentSkillSecret `json:"ListAgentSkillSecrets"`
	}
	if err := connection.Execute(ctx, documentAgentSkillSecrets, nil, &result); err != nil {
		return nil, err
	}
	return result.ListAgentSkillSecrets, nil
}

// SetAgentSkillSecret keeps one of this person's values.
func SetAgentSkillSecret(ctx context.Context, connection *Client, skill, key, value string) (*AgentSkillSecret, error) {
	var result struct {
		SetAgentSkillSecret *AgentSkillSecret `json:"SetAgentSkillSecret"`
	}
	if err := connection.Execute(ctx, documentSetAgentSkillSecret, map[string]any{"skill": skill, "key": key, "value": value}, &result); err != nil {
		return nil, err
	}
	return result.SetAgentSkillSecret, nil
}

// ClearAgentSkillSecret forgets one, and says whether there was one to
// forget.
func ClearAgentSkillSecret(ctx context.Context, connection *Client, skill, key string) (bool, error) {
	var result struct {
		ClearAgentSkillSecret bool `json:"ClearAgentSkillSecret"`
	}
	if err := connection.Execute(ctx, documentClearAgentSkillSecret, map[string]any{"skill": skill, "key": key}, &result); err != nil {
		return false, err
	}
	return result.ClearAgentSkillSecret, nil
}

// SetAgentSkillScope settles who fills a skill's secrets in: "operator",
// "person", or empty to leave it to what the skill declares.
func SetAgentSkillScope(ctx context.Context, connection *Client, name, scope string) (*AgentSkill, error) {
	var result struct {
		SetAgentSkillScope *AgentSkill `json:"SetAgentSkillScope"`
	}
	if err := connection.Execute(ctx, documentSetAgentSkillScope, map[string]any{"name": name, "scope": scope}, &result); err != nil {
		return nil, err
	}
	return result.SetAgentSkillScope, nil
}
