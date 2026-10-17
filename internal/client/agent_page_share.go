package client

import (
	"context"
	"time"
)

// Pages of the agent's memory shared with other people, and the pages other
// people shared. The same operations the dashboard and the agent's memory
// tool use.

// AgentPageShare is one share the signed-in account made of one of its own
// pages.
type AgentPageShare struct {
	ID       string `json:"id"`
	Path     string `json:"path"`
	PageName string `json:"pageName"`

	GranteeUserID     *string `json:"granteeUserId"`
	GranteeUsername   string  `json:"granteeUsername"`
	GranteeName       string  `json:"granteeName"`
	GranteeCircleID   *string `json:"granteeCircleId"`
	GranteeCircleName string  `json:"granteeCircleName"`

	IsQuoteShared bool      `json:"isQuoteShared"`
	CreatedAt     time.Time `json:"createdAt"`
}

// SharedAgentPageRoot is a root of a subtree another person shared with the
// signed-in account.
type SharedAgentPageRoot struct {
	OwnerUserID           string  `json:"ownerUserId"`
	OwnerUsername         string  `json:"ownerUsername"`
	OwnerName             string  `json:"ownerName"`
	OwnerAvatarURL        *string `json:"ownerAvatarUrl"`
	OwnerAvatarColorIndex int     `json:"ownerAvatarColorIndex"`

	Path          string `json:"path"`
	Name          string `json:"name"`
	NodeKind      string `json:"nodeKind"`
	Opening       string `json:"opening"`
	IsQuoteShared bool   `json:"isQuoteShared"`
}

// SharedAgentPageHit is a page or a fact of a shared page that a search
// found, with whose it is. FactNumber is zero for a page.
type SharedAgentPageHit struct {
	OwnerUserID   string `json:"ownerUserId"`
	OwnerUsername string `json:"ownerUsername"`
	OwnerName     string `json:"ownerName"`

	Path       string `json:"path"`
	Name       string `json:"name"`
	Opening    string `json:"opening"`
	FactNumber int    `json:"factNumber"`
	Text       string `json:"text"`
}

// SharedAgentFact is one fact of a shared page.
type SharedAgentFact struct {
	Number            int        `json:"number"`
	FactKind          string     `json:"factKind"`
	Text              string     `json:"text"`
	HappenedAt        *time.Time `json:"happenedAt"`
	HappenedPrecision string     `json:"happenedPrecision"`
	SaidAt            *time.Time `json:"saidAt"`
	IsInferred        bool       `json:"isInferred"`
	EvidenceSource    string     `json:"evidenceSource"`
	EvidenceQuote     string     `json:"evidenceQuote"`
}

// SharedAgentEdge is a link between two shared pages.
type SharedAgentEdge struct {
	FromPath   string `json:"fromPath"`
	ToPath     string `json:"toPath"`
	FromName   string `json:"fromName"`
	ToName     string `json:"toName"`
	Relation   string `json:"relation"`
	IsProposed bool   `json:"isProposed"`
}

// SharedAgentChild is a page under a shared page.
type SharedAgentChild struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	NodeKind string `json:"nodeKind"`
	Opening  string `json:"opening"`
}

// SharedAgentPage is a page another person shared, as the signed-in account
// is shown it.
type SharedAgentPage struct {
	OwnerUserID           string  `json:"ownerUserId"`
	OwnerUsername         string  `json:"ownerUsername"`
	OwnerName             string  `json:"ownerName"`
	OwnerAvatarURL        *string `json:"ownerAvatarUrl"`
	OwnerAvatarColorIndex int     `json:"ownerAvatarColorIndex"`

	Path          string              `json:"path"`
	Name          string              `json:"name"`
	NodeKind      string              `json:"nodeKind"`
	Aliases       []string            `json:"aliases"`
	Opening       string              `json:"opening"`
	IsQuoteShared bool                `json:"isQuoteShared"`
	Facts         []*SharedAgentFact  `json:"facts"`
	Edges         []*SharedAgentEdge  `json:"edges"`
	Children      []*SharedAgentChild `json:"children"`
}

// AgentPageSharePreview is what a share would show, and how many pages it
// would reach.
type AgentPageSharePreview struct {
	Page                  *SharedAgentPage `json:"page"`
	ReachedPageCount      int              `json:"reachedPageCount"`
	IsReachedPageCountCut bool             `json:"isReachedPageCountCut"`
}

const (
	agentPageShareFields = `id path pageName granteeUserId granteeUsername granteeName granteeCircleId granteeCircleName isQuoteShared createdAt`

	sharedAgentPageFields = `ownerUserId ownerUsername ownerName ownerAvatarUrl ownerAvatarColorIndex
    path name nodeKind aliases opening isQuoteShared
    facts { number factKind text happenedAt happenedPrecision saidAt isInferred evidenceSource evidenceQuote }
    edges { fromPath toPath fromName toName relation isProposed }
    children { path name nodeKind opening }`

	// DocumentListAgentPageShares lists the shares the caller made.
	DocumentListAgentPageShares = `query { ListAgentPageShares { ` + agentPageShareFields + ` } }`

	// DocumentShareAgentPage shares a page with a person or a circle.
	DocumentShareAgentPage = `mutation ($path: String!, $granteeUserId: String, $granteeCircleId: String, $isQuoteShared: Boolean) {
  ShareAgentPage(path: $path, granteeUserId: $granteeUserId, granteeCircleId: $granteeCircleId, isQuoteShared: $isQuoteShared) {
    ` + agentPageShareFields + `
  }
}`

	// DocumentUnshareAgentPage takes a share back.
	DocumentUnshareAgentPage = `mutation ($shareId: String!) { UnshareAgentPage(shareId: $shareId) }`

	// DocumentPreviewAgentPageShare is what a share would show.
	DocumentPreviewAgentPageShare = `query ($path: String!, $granteeUserId: String, $granteeCircleId: String, $isQuoteShared: Boolean) {
  PreviewAgentPageShare(path: $path, granteeUserId: $granteeUserId, granteeCircleId: $granteeCircleId, isQuoteShared: $isQuoteShared) {
    page { ` + sharedAgentPageFields + ` }
    reachedPageCount isReachedPageCountCut
  }
}`

	// DocumentListSharedAgentPages lists what other people shared with the
	// caller.
	DocumentListSharedAgentPages = `query { ListSharedAgentPages {
  ownerUserId ownerUsername ownerName ownerAvatarUrl ownerAvatarColorIndex path name nodeKind opening isQuoteShared
} }`

	// DocumentSearchSharedAgentPages finds, by words, pages and facts in what
	// other people shared with the caller.
	DocumentSearchSharedAgentPages = `query ($query: String!, $first: Int) {
  SearchSharedAgentPages(query: $query, first: $first) { ownerUserId ownerUsername ownerName path name opening factNumber text }
}`

	// DocumentReadSharedAgentPage reads one page another person shared.
	DocumentReadSharedAgentPage = `query ($ownerUserId: String!, $path: String!) {
  ReadSharedAgentPage(ownerUserId: $ownerUserId, path: $path) { ` + sharedAgentPageFields + ` }
}`
)

// ListAgentPageShares are the shares the signed-in account made.
func ListAgentPageShares(ctx context.Context, connection *Client) ([]*AgentPageShare, error) {
	var result struct {
		ListAgentPageShares []*AgentPageShare `json:"ListAgentPageShares"`
	}
	if err := connection.Execute(ctx, DocumentListAgentPageShares, nil, &result); err != nil {
		return nil, err
	}
	return result.ListAgentPageShares, nil
}

// pageShareArguments is the arguments naming a page and who it is for: a
// person or a circle by identifier, one of the two.
func pageShareArguments(path, granteeUserId, granteeCircleId string, isQuoteShared bool) map[string]any {
	arguments := map[string]any{"path": path, "isQuoteShared": isQuoteShared, "granteeUserId": nil, "granteeCircleId": nil}
	if granteeUserId != "" {
		arguments["granteeUserId"] = granteeUserId
	}
	if granteeCircleId != "" {
		arguments["granteeCircleId"] = granteeCircleId
	}
	return arguments
}

// ShareAgentPage shares one of the signed-in account's pages, and everything
// under it, with a person or a circle, or changes whether the quotes are
// shared when it is shared with them already.
func ShareAgentPage(ctx context.Context, connection *Client, path, granteeUserId, granteeCircleId string, isQuoteShared bool) (*AgentPageShare, error) {
	var result struct {
		ShareAgentPage *AgentPageShare `json:"ShareAgentPage"`
	}
	if err := connection.Execute(ctx, DocumentShareAgentPage, pageShareArguments(path, granteeUserId, granteeCircleId, isQuoteShared), &result); err != nil {
		return nil, err
	}
	return result.ShareAgentPage, nil
}

// UnshareAgentPage takes a share back.
func UnshareAgentPage(ctx context.Context, connection *Client, shareId string) error {
	var result struct {
		UnshareAgentPage bool `json:"UnshareAgentPage"`
	}
	return connection.Execute(ctx, DocumentUnshareAgentPage, map[string]any{"shareId": shareId}, &result)
}

// PreviewAgentPageShare is what the person or circle would be shown if the
// page were shared with them.
func PreviewAgentPageShare(ctx context.Context, connection *Client, path, granteeUserId, granteeCircleId string, isQuoteShared bool) (*AgentPageSharePreview, error) {
	var result struct {
		PreviewAgentPageShare *AgentPageSharePreview `json:"PreviewAgentPageShare"`
	}
	if err := connection.Execute(ctx, DocumentPreviewAgentPageShare, pageShareArguments(path, granteeUserId, granteeCircleId, isQuoteShared), &result); err != nil {
		return nil, err
	}
	return result.PreviewAgentPageShare, nil
}

// ListSharedAgentPages are the roots other people shared with the signed-in
// account.
func ListSharedAgentPages(ctx context.Context, connection *Client) ([]*SharedAgentPageRoot, error) {
	var result struct {
		ListSharedAgentPages []*SharedAgentPageRoot `json:"ListSharedAgentPages"`
	}
	if err := connection.Execute(ctx, DocumentListSharedAgentPages, nil, &result); err != nil {
		return nil, err
	}
	return result.ListSharedAgentPages, nil
}

// SearchSharedAgentPages finds, by words, pages and facts in the pages other
// people shared with the signed-in account; first bounds the hits, 20 when it
// is zero.
func SearchSharedAgentPages(ctx context.Context, connection *Client, query string, first int) ([]*SharedAgentPageHit, error) {
	variables := map[string]any{"query": query, "first": nil}
	if first > 0 {
		variables["first"] = first
	}
	var result struct {
		SearchSharedAgentPages []*SharedAgentPageHit `json:"SearchSharedAgentPages"`
	}
	if err := connection.Execute(ctx, DocumentSearchSharedAgentPages, variables, &result); err != nil {
		return nil, err
	}
	return result.SearchSharedAgentPages, nil
}

// ReadSharedAgentPage is one page another person shared, or a page under one.
func ReadSharedAgentPage(ctx context.Context, connection *Client, ownerUserId, path string) (*SharedAgentPage, error) {
	var result struct {
		ReadSharedAgentPage *SharedAgentPage `json:"ReadSharedAgentPage"`
	}
	if err := connection.Execute(ctx, DocumentReadSharedAgentPage, map[string]any{"ownerUserId": ownerUserId, "path": path}, &result); err != nil {
		return nil, err
	}
	return result.ReadSharedAgentPage, nil
}
