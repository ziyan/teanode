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
	// and dismiss it; on a card already dismissed only the first. Needs
	// agent:use.
	ShowLessLikeAgentFeedItem(ctx context.Context, arguments AgentFeedItemArguments) (*AgentFeedItemView, error)

	// Close cards, and answer the ids this call closed (one already closed is
	// left out). Give exactly one of feedItemIds (at most 500 of the
	// caller's own cards), feedStackKey (every open card of that stack) or
	// isEveryReadCard (every read card, except a decision still waiting for
	// an answer). The cards leave the list, and the question behind a
	// decision is stopped. Each card is announced to open feeds as
	// dismissed. Needs agent:use.
	DismissAgentFeedItems(ctx context.Context, arguments DismissAgentFeedItemsArguments) ([]string, error)

	// Bring back cards the caller dismissed (at most 500) and answer the ids
	// brought back, still read. A card answered "not now" (a dismiss choice)
	// can be answered again; one answered with an action keeps its answer. A
	// card is left out when an open card holds its coalesce key, or when it
	// expired. Each card is announced to open feeds as restored. The question
	// a dismissal stopped stays stopped. Needs agent:use.
	RestoreAgentFeedItems(ctx context.Context, arguments RestoreAgentFeedItemsArguments) ([]string, error)

	// Take a card back for good: it is deleted, so it is not among the
	// dismissed cards and cannot be restored, and its question is stopped.
	// What the feed tool's withdraw does, when a card no longer holds. It is
	// announced to open feeds as dismissed, and the files of its pictures are
	// deleted with it. A card the person answered or resolved is refused (it
	// is theirs now: dismiss it), a goal's own turn (named by
	// runConversationId) may withdraw only a card it posted, and a shell
	// command of the agent is refused, as for resolving. Needs agent:use.
	WithdrawAgentFeedItem(ctx context.Context, arguments WithdrawAgentFeedItemArguments) (bool, error)

	// Mark a card resolved: its problem cleared, which says so on the card
	// instead of a new card that says all is clear. The card does not move,
	// become unread or notify anyone. The first resolution and its note
	// stand: a card already resolved is refused. A goal's own turn (named by
	// runConversationId) may resolve only a card it posted. Announced to
	// open feeds as resolved. Needs agent:use.
	ResolveAgentFeedItem(ctx context.Context, arguments ResolveAgentFeedItemArguments) (*AgentFeedItemView, error)

	// Take the resolved mark off a card, which also clears its note. A goal's
	// own turn may do it only to a card it posted, and never to one the
	// person resolved. Announced to open feeds as unresolved. Needs
	// agent:use.
	UnresolveAgentFeedItem(ctx context.Context, arguments UnresolveAgentFeedItemArguments) (*AgentFeedItemView, error)

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

	// Only the dismissed cards that have not expired, the most recently
	// dismissed first, each card an entry of its own (stacks are not
	// folded). Wins over includeDismissed.
	IsDismissedOnly *bool `json:"isDismissedOnly" graphapi:"nullable"`
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

	// RunConversationID is the conversation of the turn that answers for
	// the person, who asked it to in words; the feed tool sends it, and a
	// screen leaves it out. That turn carries an action out itself, so the
	// answer is recorded and no turn of its own starts.
	RunConversationID *string `json:"runConversationId" graphapi:"nullable"`
}

// LikeAgentFeedItemArguments name a card and say whether it is liked.
type LikeAgentFeedItemArguments struct {
	FeedItemID string `json:"feedItemId"`
	IsLiked    bool   `json:"isLiked"`
}

// DismissAgentFeedItemsArguments say which cards to close: exactly one field.
type DismissAgentFeedItemsArguments struct {
	// Those cards, at most 500
	FeedItemIDs []string `json:"feedItemIds" graphapi:"nullable"`

	// Every open card of that stack
	FeedStackKey *string `json:"feedStackKey" graphapi:"nullable"`

	// True for every read card that has no open choice
	IsEveryReadCard *bool `json:"isEveryReadCard" graphapi:"nullable"`
}

// RestoreAgentFeedItemsArguments name the cards to bring back.
type RestoreAgentFeedItemsArguments struct {
	FeedItemIDs []string `json:"feedItemIds"`
}

// WithdrawAgentFeedItemArguments name a card.
type WithdrawAgentFeedItemArguments struct {
	FeedItemID string `json:"feedItemId"`

	// RunConversationID is as for ResolveAgentFeedItem.
	RunConversationID *string `json:"runConversationId" graphapi:"nullable"`
}

// ResolveAgentFeedItemArguments name a card and say what cleared.
type ResolveAgentFeedItemArguments struct {
	FeedItemID string `json:"feedItemId"`

	// ResolutionNote is one line of at most 160 characters saying what
	// cleared; longer is cut and the answer says so.
	ResolutionNote *string `json:"resolutionNote" graphapi:"nullable"`

	// RunConversationID is the goal's conversation when the goal's own turn
	// resolves, which limits it to the cards it posted; the feed tool sends
	// it, and a screen leaves it out.
	RunConversationID *string `json:"runConversationId" graphapi:"nullable"`
}

// UnresolveAgentFeedItemArguments name a card.
type UnresolveAgentFeedItemArguments struct {
	FeedItemID string `json:"feedItemId"`

	// RunConversationID is as for ResolveAgentFeedItem.
	RunConversationID *string `json:"runConversationId" graphapi:"nullable"`
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

	// ChartBlocks are at most two charts, each with the line saying where and
	// when its numbers were read. Left out of a change, the card keeps the
	// ones it has.
	ChartBlocks []models.FeedChartBlock `json:"chartBlocks" graphapi:"nullable"`

	// Media are the card's pictures and videos, at most six of its own and
	// one for each row of a list. Left out of a post, a text card shows the
	// picture of its first source link and a list card's linked rows theirs;
	// left out of a change, the card keeps the ones it has.
	Media []*AgentFeedMedia `json:"media" graphapi:"nullable"`
}

// AgentFeedMedia is a picture or video to show on a card: one of the
// person's photos or videos, or the picture of a web page, which the server
// fetches and keeps. A page is fetched only when a tool answered with its
// address in the conversation posting the card, or the person posts it.
type AgentFeedMedia struct {
	PhotoID      string `json:"photoId" graphapi:"nullable"`
	PageURL      string `json:"pageUrl" graphapi:"nullable"`
	MediaCaption string `json:"mediaCaption" graphapi:"nullable"`

	// ListRowIndex makes it the thumbnail of that row of a list card,
	// counted from 0.
	ListRowIndex *int `json:"listRowIndex" graphapi:"nullable"`

	// CameraStill makes the entry a still from one of the person's cameras
	// instead of a photo or a page; it takes no photoId or pageUrl of its own.
	CameraStill *AgentFeedCameraStill `json:"cameraStill" graphapi:"nullable"`
}

// AgentFeedCameraStill is a still from one of the person's cameras. Its
// picture is one a camera tool fetched in the posting run, named by the
// attachmentId of the tool's answer, or one of the person's photos of that
// moment by photoId; give one of the two. A still whose picture cannot be
// read is left off, and the answer says so. An update keeps a still only when
// the camera and moment are the same: a new moment needs a new picture.
type AgentFeedCameraStill struct {
	AttachmentID string `json:"attachmentId" graphapi:"nullable"`
	PhotoID      string `json:"photoId" graphapi:"nullable"`
	CameraName   string `json:"cameraName"`

	// CapturedAt is when, in RFC 3339 on the camera's own clock with its
	// offset, never converted to UTC.
	CapturedAt string `json:"capturedAt"`

	// StillReason is why this camera, of at most 120 characters.
	StillReason string `json:"stillReason"`

	// PlaybackURL and LiveURL are https addresses or the app's own scheme.
	PlaybackURL string `json:"playbackUrl" graphapi:"nullable"`
	LiveURL     string `json:"liveUrl" graphapi:"nullable"`
}

// AgentFeedMediaView is a picture or video on a card, ready to draw.
type AgentFeedMediaView struct {
	MediaKey string `json:"mediaKey"`

	// FeedMediaKind is photo, video, picture (a web page's picture) or
	// camera_still.
	FeedMediaKind string `json:"feedMediaKind"`

	// ImageURL is the picture, or a video's poster, and VideoURL the video
	// to play; server paths that work for the signed-in person only.
	ImageURL string `json:"imageUrl"`
	VideoURL string `json:"videoUrl" graphapi:"nullable"`

	// LinkURL is the page a picture is from, which a tap opens.
	LinkURL      string `json:"linkUrl" graphapi:"nullable"`
	SiteName     string `json:"siteName" graphapi:"nullable"`
	MediaCaption string `json:"mediaCaption" graphapi:"nullable"`
	WidthPixels  *int   `json:"widthPixels" graphapi:"nullable"`
	HeightPixels *int   `json:"heightPixels" graphapi:"nullable"`

	// ListRowIndex is the row of a list card this is the thumbnail of, null
	// for a picture of the whole card.
	ListRowIndex *int `json:"listRowIndex" graphapi:"nullable"`

	// For a camera still: the camera, when the still was taken (RFC 3339 on
	// the camera's clock, with its offset), why this camera, and the
	// addresses a tap on the still and Watch live open.
	CameraName  string `json:"cameraName" graphapi:"nullable"`
	CapturedAt  string `json:"capturedAt" graphapi:"nullable"`
	StillReason string `json:"stillReason" graphapi:"nullable"`
	PlaybackURL string `json:"playbackUrl" graphapi:"nullable"`
	LiveURL     string `json:"liveUrl" graphapi:"nullable"`
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

	// FeedChange is posted, replaced, settled, dismissed, restored, resolved
	// or unresolved.
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

	// FeedMedia are the card's pictures and videos and its list rows'
	// thumbnails; a page's picture appears once the server has fetched it.
	FeedMedia []*AgentFeedMediaView `json:"feedMedia"`

	// ChartBlocks are the card's charts.
	ChartBlocks []models.FeedChartBlock `json:"chartBlocks"`

	Choices []models.FeedChoice `json:"choices"`

	// IsResolved says the card's problem cleared. ResolvedAt is when, null
	// while it is not, ResolutionNote the one line saying what cleared, and
	// ResolvedByKind who marked it: goal, person or agent, empty while not
	// resolved.
	IsResolved     bool       `json:"isResolved"`
	ResolvedAt     *time.Time `json:"resolvedAt" graphapi:"nullable"`
	ResolutionNote string     `json:"resolutionNote"`
	ResolvedByKind string     `json:"resolvedByKind"`

	// PostNotes are what the post or change did that its poster did not ask
	// for, such as a title that was cut or a camera still left off; only an
	// answer to a post, a change or a resolution has any.
	PostNotes []string `json:"postNotes"`

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

	// DismissedAt is when the card was dismissed, null while it is not.
	DismissedAt *time.Time `json:"dismissedAt" graphapi:"nullable"`

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

	// feedChangeMost is how many cards one dismissal or restore names.
	feedChangeMost = 500
)

// feedItemView is a card as the API answers, with the name of the goal that
// made it.
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
	}
	chosenChoiceKey := item.ChosenChoiceKey
	return &AgentFeedItemView{
		FeedItemID: item.ID, FeedItemKind: string(item.FeedItemKind), FeedSourceKind: string(item.FeedSourceKind), FeedSourceID: item.FeedSourceID,
		FeedSourceTitle: sourceTitle, RunConversationID: item.RunConversationID,
		Title: item.Title, Body: item.Body, PictureSymbol: item.PictureSymbol,
		FactRows: item.FactRows, SourceLinks: item.SourceLinks, RecordReferences: item.RecordReferences, FeedMedia: feedMediaViews(item), ChartBlocks: feedChartBlocks(item), Choices: item.Choices,
		IsResolved: item.IsResolved(), ResolvedAt: item.ResolvedAt, ResolutionNote: item.ResolutionNote, ResolvedByKind: string(item.ResolvedByKind), PostNotes: feedPostNotes(item),
		ChosenChoiceKey: chosenChoiceKey, ChosenAt: item.ChosenAt,
		OutcomeText: item.OutcomeText, IsOutcomePending: agent.IsFeedOutcomePending(item, time.Now()),
		FeedUrgency: string(item.FeedUrgency), IsLiked: item.IsLiked, IsRead: item.ReadAt != nil, IsDismissed: item.DismissedAt != nil, DismissedAt: item.DismissedAt,
		FeedStackKey: item.FeedStackKey, StackItemCount: 1, StackUnreadCount: unreadAlone(item),
		FeedStackTitle: item.FeedStackTitle, StackPosition: item.StackPosition, FeedCardLayout: layoutName(item), LayoutContent: item.LayoutContent,
		PostedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}, nil
}

// feedChartBlocks are the card's charts, an empty list for none.
func feedChartBlocks(item *models.AgentFeedItem) []models.FeedChartBlock {
	if item.ChartBlocks == nil {
		return []models.FeedChartBlock{}
	}
	return item.ChartBlocks
}

// feedPostNotes are what the post did that was not asked for, an empty list
// for none.
func feedPostNotes(item *models.AgentFeedItem) []string {
	if item.PostNotes == nil {
		return []string{}
	}
	return item.PostNotes
}

// feedMediaViews are the card's pictures and videos that have something to
// show, with the addresses they are served from.
func feedMediaViews(item *models.AgentFeedItem) []*AgentFeedMediaView {
	views := []*AgentFeedMediaView{}
	for _, entry := range item.FeedMedia {
		if !entry.IsShown() {
			continue
		}
		view := &AgentFeedMediaView{
			MediaKey: entry.MediaKey, FeedMediaKind: string(entry.FeedMediaKind), SiteName: entry.SiteName, MediaCaption: entry.MediaCaption,
			ListRowIndex: entry.ListRowIndex,
		}
		if entry.WidthPixels > 0 && entry.HeightPixels > 0 {
			widthPixels, heightPixels := entry.WidthPixels, entry.HeightPixels
			view.WidthPixels, view.HeightPixels = &widthPixels, &heightPixels
		}
		switch entry.FeedMediaKind {
		case models.FeedMediaCameraStill:
			view.CameraName, view.CapturedAt, view.StillReason, view.PlaybackURL, view.LiveURL = entry.CameraName, entry.CapturedAt, entry.StillReason, entry.PlaybackURL, entry.LiveURL
			view.ImageURL = api.FeedMediaPath(item.ID, entry.MediaKey)
			if entry.PhotoID != "" {
				view.ImageURL = api.PhotoPreviewPath(entry.PhotoID, string(models.PhotoPreviewLarge))
			}
		case models.FeedMediaPicture:
			view.ImageURL, view.LinkURL = api.FeedMediaPath(item.ID, entry.MediaKey), entry.PageURL
		case models.FeedMediaVideo:
			view.ImageURL = api.PhotoPreviewPath(entry.PhotoID, string(models.PhotoPreviewLarge))
			view.VideoURL = api.PhotoPreviewPath(entry.PhotoID, string(models.PhotoPreviewVideo))
		default:
			view.ImageURL = api.PhotoPreviewPath(entry.PhotoID, string(models.PhotoPreviewLarge))
		}
		views = append(views, view)
	}
	return views
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
	isDismissedOnly := arguments.IsDismissedOnly != nil && *arguments.IsDismissedOnly
	var items []*models.AgentFeedItem
	var next *models.FeedCursor
	if isDismissedOnly {
		items, next, err = tx.ListDismissedAgentFeedItems(found.UserID, cursor, limit, now)
	} else {
		items, next, err = tx.ListAgentFeedItems(found.UserID, cursor, limit, arguments.IncludeDismissed != nil && *arguments.IncludeDismissed, now)
	}
	if err != nil {
		return nil, err
	}
	unreadCount, err := tx.CountUnreadAgentFeedItems(found.UserID, now)
	if err != nil {
		return nil, err
	}
	stackKeys := []string{}
	for _, item := range items {
		// A dismissed card is an entry alone, whatever its stack holds.
		if item.FeedStackKey != "" && !isDismissedOnly {
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
	if err := requireNotFeedAgentCommand(ctx, "answering a card"); err != nil {
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
	answeringConversationId := ""
	if arguments.RunConversationID != nil && strings.TrimSpace(*arguments.RunConversationID) != "" {
		conversation, err := self.ownConversation(tx, found, *arguments.RunConversationID, true)
		if err != nil {
			return nil, err
		}
		answeringConversationId = conversation.ID
	}
	chosen, err := worker.ChooseFeedItem(ctx, &agent.FeedChoiceSettings{
		Agent: found, Owner: principal.User,
		Operations:   &agentOperations{graph: self, user: principal.User, permissions: principal.Permissions, impersonator: principal.Impersonator},
		IsPersonsTap: principal.IsPersonsTap(), IsOperatorActingAsPerson: principal.IsImpersonation(),
		Item: item, ChoiceKey: strings.TrimSpace(arguments.ChoiceKey),
		AnsweringConversationID: answeringConversationId,
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
	if err := requireNotFeedAgentCommand(ctx, "liking a card"); err != nil {
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
	if err := requireNotFeedAgentCommand(ctx, "showing less like a card"); err != nil {
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

func (self *graph) DismissAgentFeedItems(ctx context.Context, arguments DismissAgentFeedItemsArguments) ([]string, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	selection := &db.FeedDismissSelection{}
	givenCount := 0
	if len(arguments.FeedItemIDs) > 0 {
		givenCount++
		if len(arguments.FeedItemIDs) > feedChangeMost {
			return nil, fmt.Errorf("%w: at most %d cards at once", api.ErrInvalidArguments, feedChangeMost)
		}
		// Every card must be the caller's own, or none is dismissed.
		for _, feedItemId := range arguments.FeedItemIDs {
			item, err := self.ownFeedItem(tx, found, feedItemId)
			if err != nil {
				return nil, err
			}
			selection.FeedItemIDs = append(selection.FeedItemIDs, item.ID)
		}
	}
	if arguments.FeedStackKey != nil && strings.TrimSpace(*arguments.FeedStackKey) != "" {
		givenCount++
		selection.FeedStackKey = strings.TrimSpace(*arguments.FeedStackKey)
	}
	if arguments.IsEveryReadCard != nil && *arguments.IsEveryReadCard {
		givenCount++
		selection.IsEveryReadCard = true
	}
	if givenCount != 1 {
		return nil, fmt.Errorf("%w: give exactly one of feedItemIds, feedStackKey or isEveryReadCard", api.ErrInvalidArguments)
	}
	dismissed, err := tx.DismissAgentFeedItems(found.UserID, selection, time.Now())
	if err != nil {
		return nil, err
	}
	dismissedIds := make([]string, 0, len(dismissed))
	for _, item := range dismissed {
		if item.InteractionID != "" {
			if _, err := tx.ClaimAgentInteraction(item.InteractionID, models.InteractionStopped); err != nil {
				return nil, err
			}
		}
		if err := self.announceFeedChange(tx, item, models.FeedChangeDismissed); err != nil {
			return nil, err
		}
		dismissedIds = append(dismissedIds, item.ID)
	}
	return dismissedIds, nil
}

func (self *graph) RestoreAgentFeedItems(ctx context.Context, arguments RestoreAgentFeedItemsArguments) ([]string, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	if err := requireNotFeedAgentCommand(ctx, "restoring a card"); err != nil {
		return nil, err
	}
	if len(arguments.FeedItemIDs) > feedChangeMost {
		return nil, fmt.Errorf("%w: at most %d cards at once", api.ErrInvalidArguments, feedChangeMost)
	}
	tx := self.transaction(ctx)
	feedItemIds := make([]string, 0, len(arguments.FeedItemIDs))
	for _, feedItemId := range arguments.FeedItemIDs {
		item, err := self.ownFeedItem(tx, found, feedItemId)
		if err != nil {
			return nil, err
		}
		feedItemIds = append(feedItemIds, item.ID)
	}
	restored, err := tx.RestoreAgentFeedItems(found.UserID, feedItemIds, time.Now())
	if err != nil {
		return nil, err
	}
	restoredIds := make([]string, 0, len(restored))
	for _, item := range restored {
		if err := self.announceFeedChange(tx, item, models.FeedChangeRestored); err != nil {
			return nil, err
		}
		restoredIds = append(restoredIds, item.ID)
	}
	return restoredIds, nil
}

func (self *graph) WithdrawAgentFeedItem(ctx context.Context, arguments WithdrawAgentFeedItemArguments) (bool, error) {
	principal, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return false, err
	}
	if err := requireNotFeedAgentCommand(ctx, "withdrawing a card"); err != nil {
		return false, err
	}
	tx := self.transaction(ctx)
	item, err := self.ownFeedItem(tx, found, arguments.FeedItemID)
	if err != nil {
		return false, err
	}
	resolver, err := self.feedResolver(ctx, tx, principal, found, arguments.RunConversationID)
	if err != nil {
		return false, err
	}
	if err := agent.CheckFeedWithdrawal(resolver, item); err != nil {
		return false, translateError(err)
	}
	if item.InteractionID != "" && !item.IsChosen() {
		if _, err := tx.ClaimAgentInteraction(item.InteractionID, models.InteractionStopped); err != nil {
			return false, err
		}
	}
	if err := tx.DeleteAgentFeedItem(item.ID); err != nil {
		return false, err
	}
	// The pictures go with the card, once the deletion is committed.
	if pictureFileIds := agent.FeedPictureFileIDs(item); len(pictureFileIds) > 0 && self.storage != nil {
		tx.AfterCommit(func() {
			for _, fileId := range pictureFileIds {
				if err := self.storage.DeleteFile(context.Background(), fileId); err != nil {
					log.Warningf("cannot remove the picture %s of a withdrawn card: %s", fileId, err)
				}
			}
		})
	}
	if err := self.announceFeedChange(tx, item, models.FeedChangeDismissed); err != nil {
		return false, err
	}
	return true, nil
}

// requireNotFeedAgentCommand refuses what the agent's own shell command asks
// of the feed on its own account. Such a command reaches the API with the
// person's token and says so in api.AgentCommandHeader. These operations are
// the person's, and the agent's feed tool's, which carries no request.
func requireNotFeedAgentCommand(ctx context.Context, doing string) error {
	if request := api.ContextRequest(ctx); request != nil && request.Header.Get(api.AgentCommandHeader) != "" {
		return fmt.Errorf("%w: %s is for the person, and for the feed tool inside a turn; a command the agent runs does not do it", api.ErrPermissionDenied, doing)
	}
	return nil
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

// feedResolver is who the call is: the goal whose own turn it is when the call
// names that goal's conversation, the agent when its tool made the call, and
// the person otherwise.
func (self *graph) feedResolver(ctx context.Context, tx db.Transaction, principal *api.Principal, found *models.Agent, runConversationId *string) (*agent.FeedResolver, error) {
	if runConversationId != nil && strings.TrimSpace(*runConversationId) != "" {
		conversation, err := self.ownConversation(tx, found, *runConversationId, true)
		if err != nil {
			return nil, err
		}
		if conversation.IsGoal() {
			return &agent.FeedResolver{Kind: models.FeedResolverGoal, ID: conversation.ID}, nil
		}
	}
	if db.PrincipalFromContext(ctx).ActorKind == models.AuditActorAgent {
		return &agent.FeedResolver{Kind: models.FeedResolverAgent}, nil
	}
	return &agent.FeedResolver{Kind: models.FeedResolverPerson, ID: principal.User.ID}, nil
}

func (self *graph) ResolveAgentFeedItem(ctx context.Context, arguments ResolveAgentFeedItemArguments) (*AgentFeedItemView, error) {
	principal, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	if err := requireNotFeedAgentCommand(ctx, "resolving a card"); err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	item, err := self.ownFeedItem(tx, found, arguments.FeedItemID)
	if err != nil {
		return nil, err
	}
	resolver, err := self.feedResolver(ctx, tx, principal, found, arguments.RunConversationID)
	if err != nil {
		return nil, err
	}
	resolutionNote := ""
	if arguments.ResolutionNote != nil {
		resolutionNote = *arguments.ResolutionNote
	}
	resolved, err := agent.ResolveFeedItem(tx, resolver, item, resolutionNote, time.Now())
	if err != nil {
		return nil, translateError(err)
	}
	if err := self.announceFeedChange(tx, resolved, models.FeedChangeResolved); err != nil {
		return nil, err
	}
	return feedItemView(tx, resolved)
}

func (self *graph) UnresolveAgentFeedItem(ctx context.Context, arguments UnresolveAgentFeedItemArguments) (*AgentFeedItemView, error) {
	principal, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	if err := requireNotFeedAgentCommand(ctx, "taking the resolved mark off a card"); err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	item, err := self.ownFeedItem(tx, found, arguments.FeedItemID)
	if err != nil {
		return nil, err
	}
	resolver, err := self.feedResolver(ctx, tx, principal, found, arguments.RunConversationID)
	if err != nil {
		return nil, err
	}
	unresolved, err := agent.UnresolveFeedItem(tx, resolver, item, time.Now())
	if err != nil {
		return nil, translateError(err)
	}
	if err := self.announceFeedChange(tx, unresolved, models.FeedChangeUnresolved); err != nil {
		return nil, err
	}
	return feedItemView(tx, unresolved)
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
		stored, err = worker.UpdateFeedItem(ctx, found.ID, arguments.FeedItemID, arguments.RunConversationID, func(item *models.AgentFeedItem) error {
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
	if arguments.ChartBlocks != nil {
		item.ChartBlocks = arguments.ChartBlocks
	}
	if arguments.Media != nil {
		item.FeedMedia = make([]models.FeedMedia, 0, len(arguments.Media))
		for _, entry := range arguments.Media {
			if entry == nil {
				continue
			}
			media := models.FeedMedia{PhotoID: entry.PhotoID, PageURL: entry.PageURL, MediaCaption: entry.MediaCaption, ListRowIndex: entry.ListRowIndex}
			if still := entry.CameraStill; still != nil {
				media.FeedMediaKind, media.PhotoID, media.AttachmentID = models.FeedMediaCameraStill, still.PhotoID, still.AttachmentID
				media.CameraName, media.CapturedAt, media.StillReason, media.PlaybackURL, media.LiveURL = still.CameraName, still.CapturedAt, still.StillReason, still.PlaybackURL, still.LiveURL
			}
			item.FeedMedia = append(item.FeedMedia, media)
		}
	}
}

// AgentFeedChanged follows the caller's feed.
func (self *graph) AgentFeedChanged(ctx context.Context) (<-chan *AgentFeedChangeView, error) {
	// A subscription has no transaction of its own, so the lookup opens one,
	// as the other subscriptions do.
	var found *models.Agent
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		_, found, err = self.requireAgentPerson(api.ContextWithTransaction(ctx, tx))
		return err
	}); err != nil {
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
