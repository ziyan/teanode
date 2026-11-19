package apigraph

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/deferutil"
)

// The feed: what the agent says without being asked, as cards the person
// scrolls. The feed tool, the dashboard's Feed page, both phone apps and
// `teanode agent feed` all go through these, so they list, post, choose and
// dismiss the same. docs/subsystems/feed.md says how it works.

// AgentFeedQuery reads the feed.
type AgentFeedQuery interface {
	// The caller's feed, newest first: a page of cards, the cursor to hand
	// back for the next page (empty on the last), and how many cards are
	// unread in all. Dismissed cards only when asked for; cards past their
	// expiry never. Needs agent:use.
	ListAgentFeed(ctx context.Context, arguments ListAgentFeedArguments) (*AgentFeedPageView, error)

	// One stack's cards, newest first: the cards of a source that pile up
	// (every story a news goal found), which ListAgentFeed shows as one
	// entry. Needs agent:use.
	ListAgentFeedStack(ctx context.Context, arguments ListAgentFeedStackArguments) (*AgentFeedPageView, error)

	// One card of the caller's feed. Needs agent:use.
	GetAgentFeedItem(ctx context.Context, arguments AgentFeedItemArguments) (*AgentFeedItemView, error)
}

// AgentFeedMutation posts to the feed and answers what is in it.
type AgentFeedMutation interface {
	// Press a button of a card. A dismiss button closes the card with no
	// turn. An action button says the choice in the main conversation as the
	// person's own message with the card attached, and the turn that follows
	// carries it out; the card shows its first line as the outcome. A coding
	// session's option answers the session directly. A card is answered
	// once: the second answer is refused. Needs agent:use.
	ChooseAgentFeedItem(ctx context.Context, arguments ChooseAgentFeedItemArguments) (*AgentFeedItemView, error)

	// Like a card, or take the like back, and tell the agent's memory what
	// the person likes. Needs agent:use.
	LikeAgentFeedItem(ctx context.Context, arguments LikeAgentFeedItemArguments) (*AgentFeedItemView, error)

	// Tell the agent's memory the person wants fewer cards like this one,
	// and dismiss it. Needs agent:use.
	ShowLessLikeAgentFeedItem(ctx context.Context, arguments AgentFeedItemArguments) (*AgentFeedItemView, error)

	// Close a card: it leaves the list, and the question behind a decision
	// is stopped. Needs agent:use.
	DismissAgentFeedItem(ctx context.Context, arguments AgentFeedItemArguments) (*AgentFeedItemView, error)

	// Mark cards read, and say how many were newly read. Needs agent:use.
	MarkAgentFeedRead(ctx context.Context, arguments MarkAgentFeedReadArguments) (int, error)

	// Start talking about a card: marks it read and answers the id of the
	// main conversation. The screen attaches the card to the next message
	// as a reference (feedItemId), so the agent knows what is discussed.
	// Needs agent:use.
	DiscussAgentFeedItem(ctx context.Context, arguments AgentFeedItemArguments) (string, error)

	// Post a card to the feed, which is what the feed tool does: with the
	// coalesceKey of an open card it replaces that card, and with
	// feedItemId it changes that card where it stands. A decision with an
	// action choice raises its question in runConversationId, and nothing
	// waits on it. A page already in an open card of the last 30 days is
	// refused. Needs agent:use.
	PostAgentFeedItem(ctx context.Context, arguments PostAgentFeedItemArguments) (*AgentFeedItemView, error)
}

// AgentFeedSubscription follows the feed as it changes.
type AgentFeedSubscription interface {
	// Says which card was posted, replaced, settled or dismissed, and how
	// many cards are unread now, each time one of the caller's cards changes
	// on any instance; an open feed reads the card again. Needs agent:use.
	AgentFeedChanged(ctx context.Context) (<-chan *AgentFeedChangeView, error)
}

// ListAgentFeedArguments page the feed.
type ListAgentFeedArguments struct {
	// How many cards; 30 when left out, at most 100
	First *int `json:"first" graphapi:"nullable"`

	// The nextCursor of the page before
	After *string `json:"after" graphapi:"nullable"`

	// Dismissed cards too, which a page that shows the feed does not want
	IncludeDismissed *bool `json:"includeDismissed" graphapi:"nullable"`
}

// ListAgentFeedStackArguments name a stack and page it.
type ListAgentFeedStackArguments struct {
	FeedStackKey string `json:"feedStackKey"`

	// How many cards; 30 when left out, at most 100
	First *int `json:"first" graphapi:"nullable"`

	// The nextCursor of the page before
	After *string `json:"after" graphapi:"nullable"`
}

// AgentFeedItemArguments name one card.
type AgentFeedItemArguments struct {
	FeedItemID string `json:"feedItemId"`
}

// ChooseAgentFeedItemArguments name a card and the button pressed.
type ChooseAgentFeedItemArguments struct {
	FeedItemID string `json:"feedItemId"`
	ChoiceKey  string `json:"choiceKey"`
}

// LikeAgentFeedItemArguments name a card and say whether it is liked.
type LikeAgentFeedItemArguments struct {
	FeedItemID string `json:"feedItemId"`
	IsLiked    bool   `json:"isLiked"`
}

// MarkAgentFeedReadArguments name the cards read.
type MarkAgentFeedReadArguments struct {
	FeedItemIDs []string `json:"feedItemIds"`
}

// PostAgentFeedItemArguments are what a post says. Everything but the title
// may be left out of a new card, and everything may be left out of a change.
type PostAgentFeedItemArguments struct {
	// FeedItemID changes that card where it stands instead of posting one.
	FeedItemID string `json:"feedItemId" graphapi:"nullable"`

	// FeedItemKind is update, decision, alert, report, idea or news; a
	// decision when there are choices and an update otherwise.
	FeedItemKind string `json:"feedItemKind" graphapi:"nullable"`

	// FeedSourceKind and FeedSourceID say what made the card: goal,
	// schedule, alert, speak_first, coding_session or agent, the default.
	FeedSourceKind string `json:"feedSourceKind" graphapi:"nullable"`
	FeedSourceID   string `json:"feedSourceId" graphapi:"nullable"`

	// RunConversationID is the conversation of the turn posting, to read the
	// agent's work and where a decision's question is raised.
	RunConversationID string `json:"runConversationId" graphapi:"nullable"`

	Title string `json:"title" graphapi:"nullable"`

	// Body is Markdown: a few lines, links allowed.
	Body string `json:"body" graphapi:"nullable"`

	// PictureSymbol is an SF Symbol name for the card's picture.
	PictureSymbol string `json:"pictureSymbol" graphapi:"nullable"`

	// FactRows are at most eight lines of a short table.
	FactRows         []models.FeedFactRow    `json:"factRows" graphapi:"nullable"`
	SourceLinks      []models.FeedSourceLink `json:"sourceLinks" graphapi:"nullable"`
	RecordReferences []models.AgentReference `json:"recordReferences" graphapi:"nullable"`

	// CoalesceKey makes a later post with the same key replace this card.
	CoalesceKey string `json:"coalesceKey" graphapi:"nullable"`

	// FeedUrgency is now, which notifies the phones, today, the default, or
	// whenever.
	FeedUrgency string `json:"feedUrgency" graphapi:"nullable"`

	// StackKey puts the card in a stack of its own naming; left out, a goal's
	// or schedule's cards stack by their source. A card with buttons that
	// act, dismiss or open, and an alert, never stack.
	StackKey string `json:"stackKey" graphapi:"nullable"`

	// StackPosition places the card inside its stack, the lowest first; a
	// summary is 0. StackTitle names the stack.
	StackPosition *int   `json:"stackPosition" graphapi:"nullable"`
	StackTitle    string `json:"stackTitle" graphapi:"nullable"`

	// CardLayout is text, the default, list, weather or agenda, and
	// LayoutContent the layout's content as a JSON object in a string. The
	// body stays plain Markdown for every layout.
	CardLayout    string `json:"cardLayout" graphapi:"nullable"`
	LayoutContent string `json:"layoutContent" graphapi:"nullable"`

	// Choices are at most four buttons.
	Choices []models.FeedChoice `json:"choices" graphapi:"nullable"`
}

// AgentFeedPageView is a page of the feed.
type AgentFeedPageView struct {
	FeedItems []*AgentFeedItemView `json:"feedItems"`

	// NextCursor is handed back as after for the next page; empty on the last.
	NextCursor string `json:"nextCursor" graphapi:"nullable"`

	// UnreadCount is how many cards are unread in all, not on this page.
	UnreadCount int `json:"unreadCount"`
}

// AgentFeedChangeView is what a screen with the feed open is told.
type AgentFeedChangeView struct {
	FeedItemID string `json:"feedItemId"`

	// FeedChange is posted, replaced, settled or dismissed.
	FeedChange  string `json:"feedChange"`
	UnreadCount int    `json:"unreadCount"`
}

// AgentFeedItemView is one card.
type AgentFeedItemView struct {
	FeedItemID     string `json:"feedItemId"`
	FeedItemKind   string `json:"feedItemKind"`
	FeedSourceKind string `json:"feedSourceKind"`
	FeedSourceID   string `json:"feedSourceId"`

	// FeedSourceTitle is the goal's or schedule's name, empty otherwise.
	FeedSourceTitle   string `json:"feedSourceTitle"`
	RunConversationID string `json:"runConversationId"`

	Title string `json:"title"`
	Body  string `json:"body"`

	// PictureSymbol is an SF Symbol name; a client without that symbol shows
	// a newspaper.
	PictureSymbol string `json:"pictureSymbol"`

	FactRows    []models.FeedFactRow    `json:"factRows"`
	SourceLinks []models.FeedSourceLink `json:"sourceLinks"`

	// RecordReferences are the references a message carries.
	RecordReferences []models.AgentReference `json:"recordReferences"`

	Choices []models.FeedChoice `json:"choices"`

	ChosenChoiceKey string     `json:"chosenChoiceKey" graphapi:"nullable"`
	ChosenAt        *time.Time `json:"chosenAt" graphapi:"nullable"`

	// OutcomeText is the agent's one line after a choice, and
	// IsOutcomePending says the turn that will write it is still running.
	OutcomeText      string `json:"outcomeText"`
	IsOutcomePending bool   `json:"isOutcomePending"`

	FeedUrgency string `json:"feedUrgency"`
	IsLiked     bool   `json:"isLiked"`
	IsRead      bool   `json:"isRead"`
	IsDismissed bool   `json:"isDismissed"`

	// FeedStackKey names the stack the card is in, empty for a card that
	// stands alone. StackItemCount is how many cards the stack holds,
	// including this one, and StackUnreadCount how many of them are unread:
	// ListAgentFeed fills them for the newest card of a stack, and they are
	// 1 and 0 or 1 for a card alone.
	FeedStackKey     string `json:"feedStackKey"`
	StackItemCount   int    `json:"stackItemCount"`
	StackUnreadCount int    `json:"stackUnreadCount"`

	// FeedStackTitle names the stack, such as "Morning brief, Wed Nov 19",
	// in the first card that gave one, and StackPosition orders a card inside
	// a stack that has an order, the lowest first; empty and null otherwise.
	// A stack's top card is its lowest position, else its newest card.
	FeedStackTitle string `json:"feedStackTitle"`
	StackPosition  *int   `json:"stackPosition" graphapi:"nullable"`

	// FeedCardLayout is text, list, weather or agenda, and LayoutContent the
	// layout's content as a JSON object in a string, empty for text. The body
	// is plain Markdown for every layout.
	FeedCardLayout string `json:"feedCardLayout"`
	LayoutContent  string `json:"layoutContent"`

	PostedAt  time.Time `json:"postedAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

const (
	feedPageDefault = 30
	feedPageMost    = 100
)

// feedItemView is a card as the API answers, with the name of the goal or
// schedule that made it.
func feedItemView(tx db.Transaction, item *models.AgentFeedItem) (*AgentFeedItemView, error) {
	sourceTitle := ""
	switch item.FeedSourceKind {
	case models.FeedSourceGoal:
		goal, err := tx.GetAgentConversation(item.FeedSourceID)
		if err != nil {
			return nil, err
		}
		if goal != nil && goal.AgentID == item.AgentID && goal.IsGoal() {
			sourceTitle = goalView(goal).GoalTitle
		}
	case models.FeedSourceSchedule:
		schedule, err := tx.GetAgentSchedule(item.FeedSourceID)
		if err != nil {
			return nil, err
		}
		if schedule != nil && schedule.AgentID == item.AgentID {
			sourceTitle = schedule.Name
		}
	}
	chosenChoiceKey := item.ChosenChoiceKey
	return &AgentFeedItemView{
		FeedItemID: item.ID, FeedItemKind: string(item.FeedItemKind), FeedSourceKind: string(item.FeedSourceKind), FeedSourceID: item.FeedSourceID,
		FeedSourceTitle: sourceTitle, RunConversationID: item.RunConversationID,
		Title: item.Title, Body: item.Body, PictureSymbol: item.PictureSymbol,
		FactRows: item.FactRows, SourceLinks: item.SourceLinks, RecordReferences: item.RecordReferences, Choices: item.Choices,
		ChosenChoiceKey: chosenChoiceKey, ChosenAt: item.ChosenAt,
		OutcomeText: item.OutcomeText, IsOutcomePending: agent.IsFeedOutcomePending(item, time.Now()),
		FeedUrgency: string(item.FeedUrgency), IsLiked: item.IsLiked, IsRead: item.ReadAt != nil, IsDismissed: item.DismissedAt != nil,
		FeedStackKey: item.FeedStackKey, StackItemCount: 1, StackUnreadCount: unreadAlone(item),
		FeedStackTitle: item.FeedStackTitle, StackPosition: item.StackPosition, FeedCardLayout: layoutName(item), LayoutContent: item.LayoutContent,
		PostedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}, nil
}

// layoutName is the card's layout, text for a card from before layouts.
func layoutName(item *models.AgentFeedItem) string {
	if item.FeedCardLayout == "" {
		return string(models.FeedLayoutText)
	}
	return string(item.FeedCardLayout)
}

// unreadAlone is the unread count of a card on its own: 1 when it is unread.
func unreadAlone(item *models.AgentFeedItem) int {
	if item.ReadAt == nil {
		return 1
	}
	return 0
}

// ownFeedItem is a card of the caller's agent, or not found.
func (self *graph) ownFeedItem(tx db.Transaction, found *models.Agent, feedItemId string) (*models.AgentFeedItem, error) {
	item, err := tx.GetAgentFeedItem(strings.TrimSpace(feedItemId))
	if err != nil {
		return nil, err
	}
	if item == nil || item.AgentID != found.ID {
		return nil, api.ErrNotFound
	}
	return item, nil
}

func (self *graph) ListAgentFeed(ctx context.Context, arguments ListAgentFeedArguments) (*AgentFeedPageView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	limit := feedPageDefault
	if arguments.First != nil && *arguments.First > 0 {
		limit = min(*arguments.First, feedPageMost)
	}
	after := ""
	if arguments.After != nil {
		after = *arguments.After
	}
	cursor, err := db.DecodeFeedCursor(after)
	if err != nil {
		return nil, fmt.Errorf("%w: that is not the cursor of a page of the feed", api.ErrInvalidArguments)
	}
	now := time.Now()
	tx := self.transaction(ctx)
	items, next, err := tx.ListAgentFeedItems(found.UserID, cursor, limit, arguments.IncludeDismissed != nil && *arguments.IncludeDismissed, now)
	if err != nil {
		return nil, err
	}
	unreadCount, err := tx.CountUnreadAgentFeedItems(found.UserID, now)
	if err != nil {
		return nil, err
	}
	stackKeys := []string{}
	for _, item := range items {
		if item.FeedStackKey != "" {
			stackKeys = append(stackKeys, item.FeedStackKey)
		}
	}
	stackCounts, err := tx.CountAgentFeedStacks(found.UserID, stackKeys, now)
	if err != nil {
		return nil, err
	}
	page := &AgentFeedPageView{FeedItems: make([]*AgentFeedItemView, 0, len(items)), NextCursor: db.EncodeFeedCursor(next), UnreadCount: unreadCount}
	for _, item := range items {
		view, err := feedItemView(tx, item)
		if err != nil {
			return nil, err
		}
		if counts := stackCounts[item.FeedStackKey]; item.FeedStackKey != "" && counts != nil {
			view.StackItemCount, view.StackUnreadCount = counts.ItemCount, counts.UnreadCount
			if counts.StackTitle != "" {
				view.FeedStackTitle = counts.StackTitle
			}
		}
		page.FeedItems = append(page.FeedItems, view)
	}
	return page, nil
}

func (self *graph) ListAgentFeedStack(ctx context.Context, arguments ListAgentFeedStackArguments) (*AgentFeedPageView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	stackKey := strings.TrimSpace(arguments.FeedStackKey)
	if stackKey == "" {
		return nil, fmt.Errorf("%w: say which stack, by feedStackKey", api.ErrInvalidArguments)
	}
	limit := feedPageDefault
	if arguments.First != nil && *arguments.First > 0 {
		limit = min(*arguments.First, feedPageMost)
	}
	after := ""
	if arguments.After != nil {
		after = *arguments.After
	}
	// A stack is ordered by position as well as by time, so its pages are
	// counted from the start, not cut at a time.
	offset := 0
	if after != "" {
		if offset, err = strconv.Atoi(after); err != nil || offset < 0 {
			return nil, fmt.Errorf("%w: that is not the cursor of a page of the feed", api.ErrInvalidArguments)
		}
	}
	now := time.Now()
	tx := self.transaction(ctx)
	items, next, err := tx.ListAgentFeedStackItems(found.UserID, stackKey, offset, limit, false, now)
	if err != nil {
		return nil, err
	}
	nextCursor := ""
	if next >= 0 {
		nextCursor = strconv.Itoa(next)
	}
	unreadCount, err := tx.CountUnreadAgentFeedItems(found.UserID, now)
	if err != nil {
		return nil, err
	}
	page := &AgentFeedPageView{FeedItems: make([]*AgentFeedItemView, 0, len(items)), NextCursor: nextCursor, UnreadCount: unreadCount}
	for _, item := range items {
		view, err := feedItemView(tx, item)
		if err != nil {
			return nil, err
		}
		page.FeedItems = append(page.FeedItems, view)
	}
	return page, nil
}

func (self *graph) GetAgentFeedItem(ctx context.Context, arguments AgentFeedItemArguments) (*AgentFeedItemView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	item, err := self.ownFeedItem(tx, found, arguments.FeedItemID)
	if err != nil {
		return nil, err
	}
	return feedItemView(tx, item)
}

func (self *graph) ChooseAgentFeedItem(ctx context.Context, arguments ChooseAgentFeedItemArguments) (*AgentFeedItemView, error) {
	principal, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	tx := self.transaction(ctx)
	item, err := self.ownFeedItem(tx, found, arguments.FeedItemID)
	if err != nil {
		return nil, err
	}
	chosen, err := worker.ChooseFeedItem(ctx, &agent.FeedChoiceSettings{
		Agent: found, Owner: principal.User,
		Operations:   &agentOperations{graph: self, user: principal.User, permissions: principal.Permissions, impersonator: principal.Impersonator},
		IsPersonsTap: principal.IsPersonsTap(), IsOperatorActingAsPerson: principal.IsImpersonation(),
		Item: item, ChoiceKey: strings.TrimSpace(arguments.ChoiceKey),
	})
	if err != nil {
		return nil, translateError(err)
	}
	return feedItemView(tx, chosen)
}

func (self *graph) LikeAgentFeedItem(ctx context.Context, arguments LikeAgentFeedItemArguments) (*AgentFeedItemView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	tx := self.transaction(ctx)
	item, err := self.ownFeedItem(tx, found, arguments.FeedItemID)
	if err != nil {
		return nil, err
	}
	liked, err := worker.LikeFeedItem(ctx, found.ID, item, arguments.IsLiked)
	if err != nil {
		return nil, translateError(err)
	}
	return feedItemView(tx, liked)
}

func (self *graph) ShowLessLikeAgentFeedItem(ctx context.Context, arguments AgentFeedItemArguments) (*AgentFeedItemView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	tx := self.transaction(ctx)
	item, err := self.ownFeedItem(tx, found, arguments.FeedItemID)
	if err != nil {
		return nil, err
	}
	dismissed, err := worker.ShowLessLikeFeedItem(ctx, found.ID, item)
	if err != nil {
		return nil, translateError(err)
	}
	return feedItemView(tx, dismissed)
}

func (self *graph) DismissAgentFeedItem(ctx context.Context, arguments AgentFeedItemArguments) (*AgentFeedItemView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	item, err := self.ownFeedItem(tx, found, arguments.FeedItemID)
	if err != nil {
		return nil, err
	}
	isDismissed, err := tx.DismissAgentFeedItem(item.ID, time.Now())
	if err != nil {
		return nil, err
	}
	if isDismissed && item.InteractionID != "" {
		if _, err := tx.ClaimAgentInteraction(item.InteractionID, models.InteractionStopped); err != nil {
			return nil, err
		}
	}
	dismissed, err := tx.GetAgentFeedItem(item.ID)
	if err != nil {
		return nil, err
	}
	if isDismissed {
		if err := self.announceFeedChange(tx, dismissed, models.FeedChangeDismissed); err != nil {
			return nil, err
		}
	}
	return feedItemView(tx, dismissed)
}

// announceFeedChange tells the open feeds once the request commits.
func (self *graph) announceFeedChange(tx db.Transaction, item *models.AgentFeedItem, change models.FeedChange) error {
	unreadCount, err := tx.CountUnreadAgentFeedItems(item.UserID, time.Now())
	if err != nil {
		return err
	}
	event := &models.FeedEvent{UserID: item.UserID, FeedItemID: item.ID, FeedChange: change, UnreadCount: unreadCount}
	tx.AfterCommit(func() {
		if err := self.database.NotifyFeedEvent(event); err != nil {
			log.Warningf("cannot tell the open feeds of a change: %s", err)
		}
	})
	return nil
}

func (self *graph) MarkAgentFeedRead(ctx context.Context, arguments MarkAgentFeedReadArguments) (int, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return 0, err
	}
	return self.transaction(ctx).MarkAgentFeedItemsRead(found.UserID, arguments.FeedItemIDs, time.Now())
}

func (self *graph) DiscussAgentFeedItem(ctx context.Context, arguments AgentFeedItemArguments) (string, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return "", err
	}
	tx := self.transaction(ctx)
	item, err := self.ownFeedItem(tx, found, arguments.FeedItemID)
	if err != nil {
		return "", err
	}
	if _, err := tx.MarkAgentFeedItemsRead(found.UserID, []string{item.ID}, time.Now()); err != nil {
		return "", err
	}
	conversation, err := self.mainConversation(tx, found)
	if err != nil {
		return "", err
	}
	return conversation.ID, nil
}

func (self *graph) PostAgentFeedItem(ctx context.Context, arguments PostAgentFeedItemArguments) (*AgentFeedItemView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	tx := self.transaction(ctx)
	// The conversation of the turn that posts must be the caller's own.
	if arguments.RunConversationID != "" {
		if _, err := self.ownConversation(tx, found, arguments.RunConversationID, true); err != nil {
			return nil, err
		}
	}
	var stored *models.AgentFeedItem
	if arguments.FeedItemID != "" {
		stored, err = worker.UpdateFeedItem(ctx, found.ID, arguments.FeedItemID, func(item *models.AgentFeedItem) error {
			applyFeedPost(item, &arguments)
			return nil
		})
	} else {
		item := &models.AgentFeedItem{}
		applyFeedPost(item, &arguments)
		stored, _, err = worker.PostFeedItem(ctx, &agent.FeedPost{AgentID: found.ID, Item: item, InteractionConversationID: arguments.RunConversationID})
	}
	if err != nil {
		return nil, translateError(err)
	}
	return feedItemView(tx, stored)
}

// applyFeedPost writes the fields a post gave onto a card, leaving the rest.
func applyFeedPost(item *models.AgentFeedItem, arguments *PostAgentFeedItemArguments) {
	if arguments.FeedItemKind != "" {
		item.FeedItemKind = models.FeedItemKind(strings.ToLower(strings.TrimSpace(arguments.FeedItemKind)))
	}
	if arguments.FeedSourceKind != "" {
		item.FeedSourceKind = models.FeedSourceKind(strings.ToLower(strings.TrimSpace(arguments.FeedSourceKind)))
	}
	if arguments.FeedSourceID != "" {
		item.FeedSourceID = arguments.FeedSourceID
	}
	if arguments.RunConversationID != "" {
		item.RunConversationID = arguments.RunConversationID
	}
	if arguments.Title != "" {
		item.Title = arguments.Title
	}
	if arguments.Body != "" {
		item.Body = arguments.Body
	}
	if arguments.PictureSymbol != "" {
		item.PictureSymbol = arguments.PictureSymbol
	}
	if arguments.FactRows != nil {
		item.FactRows = arguments.FactRows
	}
	if arguments.SourceLinks != nil {
		item.SourceLinks = arguments.SourceLinks
	}
	if arguments.RecordReferences != nil {
		item.RecordReferences = arguments.RecordReferences
	}
	if arguments.CoalesceKey != "" {
		item.CoalesceKey = arguments.CoalesceKey
	}
	if arguments.FeedUrgency != "" {
		item.FeedUrgency = models.FeedUrgency(strings.ToLower(strings.TrimSpace(arguments.FeedUrgency)))
	}
	if arguments.Choices != nil {
		item.Choices = arguments.Choices
	}
	if arguments.StackKey != "" {
		item.FeedStackKey = arguments.StackKey
	}
	if arguments.StackPosition != nil {
		item.StackPosition = arguments.StackPosition
	}
	if arguments.StackTitle != "" {
		item.FeedStackTitle = arguments.StackTitle
	}
	if arguments.CardLayout != "" {
		item.FeedCardLayout = models.FeedCardLayout(strings.ToLower(strings.TrimSpace(arguments.CardLayout)))
	}
	if arguments.LayoutContent != "" {
		item.LayoutContent = arguments.LayoutContent
	}
}

// AgentFeedChanged follows the caller's feed.
func (self *graph) AgentFeedChanged(ctx context.Context) (<-chan *AgentFeedChangeView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	received, release, err := self.agentFeedEvents.subscribe(self.database, found.UserID)
	if err != nil {
		return nil, err
	}
	views := make(chan *AgentFeedChangeView)
	go func() {
		defer deferutil.Recover()
		defer close(views)
		defer release()
		for {
			select {
			case <-ctx.Done():
				return
			case event := <-received:
				view := &AgentFeedChangeView{FeedItemID: event.FeedItemID, FeedChange: string(event.FeedChange), UnreadCount: event.UnreadCount}
				select {
				case views <- view:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return views, nil
}
