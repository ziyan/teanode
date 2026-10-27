package client

import (
	"context"
	"time"
)

// Search across everything: the same operation as the dashboard's search box
// and the agent's search tool.

// SearchResult is one thing found.
type SearchResult struct {
	SearchArea string     `json:"searchArea"`
	TargetID   string     `json:"targetId"`
	TitleText  string     `json:"titleText"`
	DetailText string     `json:"detailText"`
	HappenedAt *time.Time `json:"happenedAt"`
	IsMain     bool       `json:"isMain"`
}

// SearchArea is one area's results.
type SearchArea struct {
	SearchArea string          `json:"searchArea"`
	Results    []*SearchResult `json:"results"`
	HasMore    bool            `json:"hasMore"`
}

// SearchResults is what a search across everything found.
type SearchResults struct {
	Words       string        `json:"words"`
	Areas       []*SearchArea `json:"areas"`
	FailedAreas []string      `json:"failedAreas"`
}

const DocumentSearchEverything = `query ($words: String!, $perAreaLimit: Int, $areas: [String!]) {
  SearchEverything(words: $words, perAreaLimit: $perAreaLimit, areas: $areas) {
    words
    failedAreas
    areas { searchArea hasMore results { searchArea targetId titleText detailText happenedAt isMain } }
  }
}`

// SearchEverything looks for the words in the areas named, or in every area
// the person may use when none is named, and keeps perAreaLimit results from
// each; zero is the server's default.
func SearchEverything(ctx context.Context, connection *Client, words string, areas []string, perAreaLimit int) (*SearchResults, error) {
	variables := map[string]any{"words": words}
	if len(areas) > 0 {
		variables["areas"] = areas
	}
	if perAreaLimit > 0 {
		variables["perAreaLimit"] = perAreaLimit
	}
	var result struct {
		SearchEverything *SearchResults `json:"SearchEverything"`
	}
	if err := connection.Execute(ctx, DocumentSearchEverything, variables, &result); err != nil {
		return nil, err
	}
	return result.SearchEverything, nil
}
