package client

import (
	"context"
	"time"
)

// The feed: what the agent says without being asked, as cards, newest first.

// AgentFeedFactRow is one line of a card's short table.
type AgentFeedFactRow struct {
	FactLabel string `json:"factLabel"`
	FactValue string `json:"factValue"`
}

// AgentFeedSourceLink is a page a card is built from.
type AgentFeedSourceLink struct {
	SourceTitle string `json:"sourceTitle"`
	SourceURL   string `json:"sourceUrl"`
}

// AgentFeedChoice is a button of a card; ChoiceRole is action, dismiss,
// discuss or open.
type AgentFeedChoice struct {
	ChoiceKey   string `json:"choiceKey"`
	ChoiceLabel string `json:"choiceLabel"`
	ChoiceRole  string `json:"choiceRole"`
}

// AgentFeedReference is a record a card points at.
type AgentFeedReference struct {
	ItemID               string `json:"itemId,omitempty"`
	ThreadID             string `json:"threadId,omitempty"`
	Subject              string `json:"subject,omitempty"`
	From                 string `json:"from,omitempty"`
	Path                 string `json:"path,omitempty"`
	Name                 string `json:"name,omitempty"`
	FinanceTransactionID string `json:"financeTransactionId,omitempty"`
	RecordingID          string `json:"recordingId,omitempty"`
	RecordingTitle       string `json:"recordingTitle,omitempty"`
	PhotoID              string `json:"photoId,omitempty"`
	PhotoTitle           string `json:"photoTitle,omitempty"`
}

// AgentFeedItem is one card.
type AgentFeedItem struct {
	FeedItemID        string                `json:"feedItemId"`
	FeedItemKind      string                `json:"feedItemKind"`
	FeedSourceKind    string                `json:"feedSourceKind"`
	FeedSourceID      string                `json:"feedSourceId"`
	FeedSourceTitle   string                `json:"feedSourceTitle"`
	RunConversationID string                `json:"runConversationId"`
	Title             string                `json:"title"`
	Body              string                `json:"body"`
	PictureSymbol     string                `json:"pictureSymbol"`
	FactRows          []AgentFeedFactRow    `json:"factRows"`
	SourceLinks       []AgentFeedSourceLink `json:"sourceLinks"`
	RecordReferences  []AgentFeedReference  `json:"recordReferences"`
	Choices           []AgentFeedChoice     `json:"choices"`
	ChosenChoiceKey   string                `json:"chosenChoiceKey"`
	ChosenAt          *time.Time            `json:"chosenAt"`
	OutcomeText       string                `json:"outcomeText"`
	IsOutcomePending  bool                  `json:"isOutcomePending"`
	FeedUrgency       string                `json:"feedUrgency"`
	IsLiked           bool                  `json:"isLiked"`
	IsRead            bool                  `json:"isRead"`
	IsDismissed       bool                  `json:"isDismissed"`
	FeedStackKey      string                `json:"feedStackKey"`
	StackItemCount    int                   `json:"stackItemCount"`
	StackUnreadCount  int                   `json:"stackUnreadCount"`
	FeedStackTitle    string                `json:"feedStackTitle"`
	StackPosition     *int                  `json:"stackPosition"`
	FeedCardLayout    string                `json:"feedCardLayout"`
	LayoutContent     string                `json:"layoutContent"`
	FeedMedia         []AgentFeedMedia      `json:"feedMedia"`
	PostedAt          time.Time             `json:"postedAt"`
	UpdatedAt         time.Time             `json:"updatedAt"`
}

// AgentFeedMedia is a picture or video on a card.
type AgentFeedMedia struct {
	MediaKey      string `json:"mediaKey"`
	FeedMediaKind string `json:"feedMediaKind"`
	ImageURL      string `json:"imageUrl"`
	VideoURL      string `json:"videoUrl"`
	LinkURL       string `json:"linkUrl"`
	SiteName      string `json:"siteName"`
	MediaCaption  string `json:"mediaCaption"`
	WidthPixels   *int   `json:"widthPixels"`
	HeightPixels  *int   `json:"heightPixels"`
	ListRowIndex  *int   `json:"listRowIndex"`
}

// AgentFeedPage is a page of the feed.
type AgentFeedPage struct {
	FeedItems   []*AgentFeedItem `json:"feedItems"`
	NextCursor  string           `json:"nextCursor"`
	UnreadCount int              `json:"unreadCount"`
}

const (
	feedItemFields = `feedItemId feedItemKind feedSourceKind feedSourceId feedSourceTitle runConversationId title body pictureSymbol
		factRows { factLabel factValue } sourceLinks { sourceTitle sourceUrl }
		recordReferences { itemId threadId subject from path name financeTransactionId recordingId recordingTitle photoId photoTitle }
		choices { choiceKey choiceLabel choiceRole } chosenChoiceKey chosenAt outcomeText isOutcomePending
		feedUrgency isLiked isRead isDismissed feedStackKey stackItemCount stackUnreadCount feedStackTitle stackPosition feedCardLayout layoutContent postedAt updatedAt
		feedMedia { mediaKey feedMediaKind imageUrl videoUrl linkUrl siteName mediaCaption widthPixels heightPixels listRowIndex }`

	// DocumentPostAgentFeedItem is also what the feed tool sends.
	postFields = `$feedItemId: String, $feedItemKind: String, $feedSourceKind: String, $feedSourceId: String, $runConversationId: String,
		$title: String, $body: String, $pictureSymbol: String, $factRows: [FeedFactRowInput!], $sourceLinks: [FeedSourceLinkInput!],
		$coalesceKey: String, $feedUrgency: String, $stackKey: String, $stackPosition: Int, $stackTitle: String, $cardLayout: String, $layoutContent: String, $choices: [FeedChoiceInput!], $media: [AgentFeedMediaInput!]`
	postArguments = `feedItemId: $feedItemId, feedItemKind: $feedItemKind, feedSourceKind: $feedSourceKind, feedSourceId: $feedSourceId,
			runConversationId: $runConversationId, title: $title, body: $body, pictureSymbol: $pictureSymbol, factRows: $factRows,
			sourceLinks: $sourceLinks, coalesceKey: $coalesceKey, feedUrgency: $feedUrgency, stackKey: $stackKey, stackPosition: $stackPosition, stackTitle: $stackTitle, cardLayout: $cardLayout, layoutContent: $layoutContent, choices: $choices, media: $media`
)

// The documents the command line and the feed tool send, exported so a test
// can check them against the schema.
const (
	DocumentListAgentFeed = `query ($first: Int, $after: String, $includeDismissed: Boolean) {
		ListAgentFeed(first: $first, after: $after, includeDismissed: $includeDismissed) {
			feedItems { ` + feedItemFields + ` } nextCursor unreadCount
		}
	}`
	DocumentListAgentFeedStack = `query ($feedStackKey: String!, $first: Int, $after: String) {
		ListAgentFeedStack(feedStackKey: $feedStackKey, first: $first, after: $after) {
			feedItems { ` + feedItemFields + ` } nextCursor unreadCount
		}
	}`
	DocumentGetAgentFeedItem = `query ($feedItemId: String!) {
		GetAgentFeedItem(feedItemId: $feedItemId) { ` + feedItemFields + ` }
	}`
	DocumentChooseAgentFeedItem = `mutation ($feedItemId: String!, $choiceKey: String!) {
		ChooseAgentFeedItem(feedItemId: $feedItemId, choiceKey: $choiceKey) { ` + feedItemFields + ` }
	}`
	DocumentLikeAgentFeedItem = `mutation ($feedItemId: String!, $isLiked: Boolean!) {
		LikeAgentFeedItem(feedItemId: $feedItemId, isLiked: $isLiked) { ` + feedItemFields + ` }
	}`
	DocumentShowLessLikeAgentFeedItem = `mutation ($feedItemId: String!) {
		ShowLessLikeAgentFeedItem(feedItemId: $feedItemId) { ` + feedItemFields + ` }
	}`
	DocumentDismissAgentFeedItem = `mutation ($feedItemId: String!) {
		DismissAgentFeedItem(feedItemId: $feedItemId) { ` + feedItemFields + ` }
	}`
	DocumentMarkAgentFeedRead = `mutation ($feedItemIds: [String!]!) {
		MarkAgentFeedRead(feedItemIds: $feedItemIds)
	}`
	DocumentDiscussAgentFeedItem = `mutation ($feedItemId: String!) {
		DiscussAgentFeedItem(feedItemId: $feedItemId)
	}`
	DocumentPostAgentFeedItem = `mutation (` + postFields + `) {
		PostAgentFeedItem(` + postArguments + `) { ` + feedItemFields + ` }
	}`
)

// ListAgentFeed is a page of the feed, newest first.
func ListAgentFeed(ctx context.Context, connection *Client, first int, after string, includeDismissed bool) (*AgentFeedPage, error) {
	var result struct {
		ListAgentFeed *AgentFeedPage `json:"ListAgentFeed"`
	}
	variables := map[string]any{"includeDismissed": includeDismissed}
	if first > 0 {
		variables["first"] = first
	}
	if after != "" {
		variables["after"] = after
	}
	if err := connection.Execute(ctx, DocumentListAgentFeed, variables, &result); err != nil {
		return nil, err
	}
	return result.ListAgentFeed, nil
}

// ListAgentFeedStack is the cards of one stack, newest first.
func ListAgentFeedStack(ctx context.Context, connection *Client, feedStackKey string, first int, after string) (*AgentFeedPage, error) {
	var result struct {
		ListAgentFeedStack *AgentFeedPage `json:"ListAgentFeedStack"`
	}
	variables := map[string]any{"feedStackKey": feedStackKey}
	if first > 0 {
		variables["first"] = first
	}
	if after != "" {
		variables["after"] = after
	}
	if err := connection.Execute(ctx, DocumentListAgentFeedStack, variables, &result); err != nil {
		return nil, err
	}
	return result.ListAgentFeedStack, nil
}

// GetAgentFeedItem is one card.
func GetAgentFeedItem(ctx context.Context, connection *Client, feedItemId string) (*AgentFeedItem, error) {
	var result struct {
		GetAgentFeedItem *AgentFeedItem `json:"GetAgentFeedItem"`
	}
	if err := connection.Execute(ctx, DocumentGetAgentFeedItem, map[string]any{"feedItemId": feedItemId}, &result); err != nil {
		return nil, err
	}
	return result.GetAgentFeedItem, nil
}

// ChooseAgentFeedItem presses a button of a card.
func ChooseAgentFeedItem(ctx context.Context, connection *Client, feedItemId, choiceKey string) (*AgentFeedItem, error) {
	var result struct {
		ChooseAgentFeedItem *AgentFeedItem `json:"ChooseAgentFeedItem"`
	}
	if err := connection.Execute(ctx, DocumentChooseAgentFeedItem, map[string]any{"feedItemId": feedItemId, "choiceKey": choiceKey}, &result); err != nil {
		return nil, err
	}
	return result.ChooseAgentFeedItem, nil
}

// LikeAgentFeedItem likes a card or takes the like back.
func LikeAgentFeedItem(ctx context.Context, connection *Client, feedItemId string, isLiked bool) (*AgentFeedItem, error) {
	var result struct {
		LikeAgentFeedItem *AgentFeedItem `json:"LikeAgentFeedItem"`
	}
	if err := connection.Execute(ctx, DocumentLikeAgentFeedItem, map[string]any{"feedItemId": feedItemId, "isLiked": isLiked}, &result); err != nil {
		return nil, err
	}
	return result.LikeAgentFeedItem, nil
}

// ShowLessLikeAgentFeedItem asks for fewer cards like this one and dismisses it.
func ShowLessLikeAgentFeedItem(ctx context.Context, connection *Client, feedItemId string) (*AgentFeedItem, error) {
	var result struct {
		ShowLessLikeAgentFeedItem *AgentFeedItem `json:"ShowLessLikeAgentFeedItem"`
	}
	if err := connection.Execute(ctx, DocumentShowLessLikeAgentFeedItem, map[string]any{"feedItemId": feedItemId}, &result); err != nil {
		return nil, err
	}
	return result.ShowLessLikeAgentFeedItem, nil
}

// DismissAgentFeedItem closes a card.
func DismissAgentFeedItem(ctx context.Context, connection *Client, feedItemId string) (*AgentFeedItem, error) {
	var result struct {
		DismissAgentFeedItem *AgentFeedItem `json:"DismissAgentFeedItem"`
	}
	if err := connection.Execute(ctx, DocumentDismissAgentFeedItem, map[string]any{"feedItemId": feedItemId}, &result); err != nil {
		return nil, err
	}
	return result.DismissAgentFeedItem, nil
}

// MarkAgentFeedRead marks cards read and says how many were newly read.
func MarkAgentFeedRead(ctx context.Context, connection *Client, feedItemIds []string) (int, error) {
	var result struct {
		MarkAgentFeedRead int `json:"MarkAgentFeedRead"`
	}
	if err := connection.Execute(ctx, DocumentMarkAgentFeedRead, map[string]any{"feedItemIds": feedItemIds}, &result); err != nil {
		return 0, err
	}
	return result.MarkAgentFeedRead, nil
}

// DiscussAgentFeedItem is the main conversation's id, to talk about a card in.
func DiscussAgentFeedItem(ctx context.Context, connection *Client, feedItemId string) (string, error) {
	var result struct {
		DiscussAgentFeedItem string `json:"DiscussAgentFeedItem"`
	}
	if err := connection.Execute(ctx, DocumentDiscussAgentFeedItem, map[string]any{"feedItemId": feedItemId}, &result); err != nil {
		return "", err
	}
	return result.DiscussAgentFeedItem, nil
}

// PostAgentFeedItem posts a card; the variables are the mutation's.
func PostAgentFeedItem(ctx context.Context, connection *Client, variables map[string]any) (*AgentFeedItem, error) {
	var result struct {
		PostAgentFeedItem *AgentFeedItem `json:"PostAgentFeedItem"`
	}
	if err := connection.Execute(ctx, DocumentPostAgentFeedItem, variables, &result); err != nil {
		return nil, err
	}
	return result.PostAgentFeedItem, nil
}
