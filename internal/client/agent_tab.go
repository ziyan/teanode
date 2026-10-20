package client

import "context"

// AgentTabListing is one tab the agent may act in, as the person's browser
// last listed it: theirs, or one the agent opened.
type AgentTabListing struct {
	Tab       string `json:"tab"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	IsOwn     bool   `json:"isOwn"`
	IsCurrent bool   `json:"isCurrent"`
}

// AgentTab is the person's browser as the server knows it: whether it is
// connected through the extension, the tabs, and whether the person has
// taken the tab over from the dashboard.
type AgentTab struct {
	Connected   bool               `json:"connected"`
	Attached    bool               `json:"attached"`
	Title       string             `json:"title"`
	URL         string             `json:"url"`
	Allowed     bool               `json:"allowed"`
	Tabs        []*AgentTabListing `json:"tabs"`
	IsTakenOver bool               `json:"isTakenOver"`
}

const DocumentReadAgentTab = `query {
	ReadAgentTab { connected attached title url allowed tabs { tab title url isOwn isCurrent } isTakenOver }
}`

// ReadAgentTab is the person's browser and who holds its tab.
func ReadAgentTab(ctx context.Context, connection *Client) (*AgentTab, error) {
	var result struct {
		ReadAgentTab *AgentTab `json:"ReadAgentTab"`
	}
	if err := connection.Execute(ctx, DocumentReadAgentTab, nil, &result); err != nil {
		return nil, err
	}
	return result.ReadAgentTab, nil
}

const DocumentHandBackAgentTab = `mutation ($note: String) { HandBackAgentTab(note: $note) }`

// HandBackAgentTab gives the agent back the tab the person took over, with
// a note for it, and answers its requests to take the tab over. It says
// whether there was anything to hand back.
func HandBackAgentTab(ctx context.Context, connection *Client, note string) (bool, error) {
	var result struct {
		HandBackAgentTab bool `json:"HandBackAgentTab"`
	}
	if err := connection.Execute(ctx, DocumentHandBackAgentTab, map[string]any{"note": note}, &result); err != nil {
		return false, err
	}
	return result.HandBackAgentTab, nil
}
