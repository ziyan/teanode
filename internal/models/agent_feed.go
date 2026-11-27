package models

import (
	"strconv"
	"strings"
	"time"
)

// FeedItemKind is what a card is: an update, a decision the person is asked
// to make, an alert, a report a schedule or goal made, an idea, or news.
type FeedItemKind string

// The kinds of card.
const (
	FeedItemUpdate   FeedItemKind = "update"
	FeedItemDecision FeedItemKind = "decision"
	FeedItemAlert    FeedItemKind = "alert"
	FeedItemReport   FeedItemKind = "report"
	FeedItemIdea     FeedItemKind = "idea"
	FeedItemNews     FeedItemKind = "news"
)

// IsFeedItemKind says whether a word is one of the kinds of card.
func IsFeedItemKind(value FeedItemKind) bool {
	switch value {
	case FeedItemUpdate, FeedItemDecision, FeedItemAlert, FeedItemReport, FeedItemIdea, FeedItemNews:
		return true
	}
	return false
}

// FeedSourceKind is what made a card: a goal, an alert, the agent
// speaking first, a coding session, or a turn's own post.
type FeedSourceKind string

// The kinds of source.
const (
	FeedSourceGoal          FeedSourceKind = "goal"
	FeedSourceAlert         FeedSourceKind = "alert"
	FeedSourceSpeakFirst    FeedSourceKind = "speak_first"
	FeedSourceCodingSession FeedSourceKind = "coding_session"
	FeedSourceAgent         FeedSourceKind = "agent"
)

// IsFeedSourceKind says whether a word is one of the kinds of source.
func IsFeedSourceKind(value FeedSourceKind) bool {
	switch value {
	case FeedSourceGoal, FeedSourceAlert, FeedSourceSpeakFirst, FeedSourceCodingSession, FeedSourceAgent:
		return true
	}
	return false
}

// FeedUrgency is how soon the person should see a card. Only now sends a
// phone notification; the others raise the unread count.
type FeedUrgency string

// The urgencies.
const (
	FeedUrgencyNow      FeedUrgency = "now"
	FeedUrgencyToday    FeedUrgency = "today"
	FeedUrgencyWhenever FeedUrgency = "whenever"
)

// IsFeedUrgency says whether a word is one of the urgencies.
func IsFeedUrgency(value FeedUrgency) bool {
	return value == FeedUrgencyNow || value == FeedUrgencyToday || value == FeedUrgencyWhenever
}

// FeedChoiceRole is what choosing a button does: action carries it out,
// dismiss closes the card, discuss opens a conversation about it, open opens
// the thing it is about (a coding session).
type FeedChoiceRole string

// The roles of a choice.
const (
	FeedChoiceAction  FeedChoiceRole = "action"
	FeedChoiceDismiss FeedChoiceRole = "dismiss"
	FeedChoiceDiscuss FeedChoiceRole = "discuss"
	FeedChoiceOpen    FeedChoiceRole = "open"
)

// IsFeedChoiceRole says whether a word is one of the roles.
func IsFeedChoiceRole(value FeedChoiceRole) bool {
	switch value {
	case FeedChoiceAction, FeedChoiceDismiss, FeedChoiceDiscuss, FeedChoiceOpen:
		return true
	}
	return false
}

// FeedChange is what happened to a card, for a screen that has it open.
type FeedChange string

// The changes.
const (
	FeedChangePosted    FeedChange = "posted"
	FeedChangeReplaced  FeedChange = "replaced"
	FeedChangeSettled   FeedChange = "settled"
	FeedChangeDismissed FeedChange = "dismissed"
	FeedChangeRestored  FeedChange = "restored"

	// FeedChangeResolved and FeedChangeUnresolved say a card was marked
	// resolved, or the mark was taken off. Neither moves the card.
	FeedChangeResolved   FeedChange = "resolved"
	FeedChangeUnresolved FeedChange = "unresolved"
)

// FeedResolverKind is who marked a card resolved: a goal in its own turn, the
// person, or the agent outside a goal's turn.
type FeedResolverKind string

// The kinds of resolver.
const (
	FeedResolverGoal   FeedResolverKind = "goal"
	FeedResolverPerson FeedResolverKind = "person"
	FeedResolverAgent  FeedResolverKind = "agent"
)

// IsFeedResolverKind says whether a word is one of the kinds of resolver.
func IsFeedResolverKind(value FeedResolverKind) bool {
	return value == FeedResolverGoal || value == FeedResolverPerson || value == FeedResolverAgent
}

// FeedResolution is what marks a card resolved: who, and the one line saying
// what cleared.
type FeedResolution struct {
	ResolutionNote string
	ResolvedByKind FeedResolverKind
	ResolvedByID   string
}

// FeedCardLayout is how a card is drawn: text, the default, or a layout with
// structured content that every app draws natively.
type FeedCardLayout string

// The layouts.
const (
	FeedLayoutText    FeedCardLayout = "text"
	FeedLayoutList    FeedCardLayout = "list"
	FeedLayoutWeather FeedCardLayout = "weather"
	FeedLayoutAgenda  FeedCardLayout = "agenda"
)

// IsFeedCardLayout says whether a word is one of the layouts.
func IsFeedCardLayout(value FeedCardLayout) bool {
	switch value {
	case FeedLayoutText, FeedLayoutList, FeedLayoutWeather, FeedLayoutAgenda:
		return true
	}
	return false
}

// The destinations of what the agent says unasked.
const (
	FeedDestinationFeed = "feed"
	FeedDestinationMain = "main"
)

// IsFeedDestination is one of the two, and an empty one is the feed.
func IsFeedDestination(value string) bool {
	return value == "" || value == FeedDestinationFeed || value == FeedDestinationMain
}

// IsFeedFor says whether what the agent says unasked goes to its feed, which
// is the default.
func (self *Agent) IsFeedFor() bool {
	return self.FeedDestination != FeedDestinationMain
}

// The limits on a card.
const (
	// FeedFactRowsMost is how many rows the short table of a card may have.
	FeedFactRowsMost = 8

	// FeedChoicesMost is how many buttons a card may have.
	FeedChoicesMost = 4

	// FeedExpiryDays is how long a card that needs no answer is kept in the
	// list, and how long a settled one is.
	FeedExpiryDays = 30

	// FeedAnsweredLifetime is how long a card answered with a choice stays
	// in the feed before it moves to the dismissed cards: long enough to see
	// what came of the answer, short enough that the answered do not crowd
	// out what still needs the person.
	FeedAnsweredLifetime = 24 * time.Hour

	// FeedSourceURLDays is how far back a post is refused for linking a page
	// an open card already links.
	FeedSourceURLDays = 30
)

// FeedFactRow is one line of a card's short table.
type FeedFactRow struct {
	FactLabel string `json:"factLabel"`
	FactValue string `json:"factValue"`
}

// FeedMediaKind is what a picture or video on a card is.
type FeedMediaKind string

const (
	// FeedMediaPhoto is one of the person's own photos.
	FeedMediaPhoto FeedMediaKind = "photo"

	// FeedMediaVideo is one of the person's own videos.
	FeedMediaVideo FeedMediaKind = "video"

	// FeedMediaPicture is the picture of a web page, which the server
	// fetches and keeps so that the person's device never asks the page's
	// site for it.
	FeedMediaPicture FeedMediaKind = "picture"

	// FeedMediaCameraStill is a still from one of the person's cameras, kept
	// by the server as a page's picture is, or one of the person's photos of
	// that moment, with why this camera was chosen.
	FeedMediaCameraStill FeedMediaKind = "camera_still"
)

const (
	// FeedMediaMost is how many pictures and videos a card may show as its
	// own, and FeedRowMediaMost how many thumbnails the rows of a list card
	// may have.
	FeedMediaMost    = 6
	FeedRowMediaMost = 10
)

// FeedMedia is a picture or video on a card.
type FeedMedia struct {
	// MediaKey names the entry on its card, for the address its picture is
	// served from.
	MediaKey      string        `json:"mediaKey"`
	FeedMediaKind FeedMediaKind `json:"feedMediaKind"`

	// PhotoID is the person's photo or video, for those kinds.
	PhotoID string `json:"photoId,omitempty"`

	// PageURL is the page whose picture this is, for a picture. ImageFileID
	// is the picture as the server keeps it, empty until it is fetched, and
	// IsPictureMissing says the page had none or could not be read, so it
	// is not shown or tried again.
	PageURL          string `json:"pageUrl,omitempty"`
	ImageFileID      string `json:"imageFileId,omitempty"`
	IsPictureMissing bool   `json:"isPictureMissing,omitempty"`

	// SiteName is what the page calls its site.
	SiteName     string `json:"siteName,omitempty"`
	MediaCaption string `json:"mediaCaption,omitempty"`
	WidthPixels  int    `json:"widthPixels,omitempty"`
	HeightPixels int    `json:"heightPixels,omitempty"`

	// ListRowIndex makes the entry the thumbnail of that row of a list card;
	// nil for a picture of the whole card.
	ListRowIndex *int `json:"listRowIndex,omitempty"`

	// CameraName is the camera a still is from, and CapturedAt when, in RFC
	// 3339 on the camera's own clock with its offset, never converted to
	// UTC. StillReason says why this camera. PlaybackURL opens the
	// recording and LiveURL the live view, each https or the app's own
	// scheme. All for a camera still.
	CameraName  string `json:"cameraName,omitempty"`
	CapturedAt  string `json:"capturedAt,omitempty"`
	StillReason string `json:"stillReason,omitempty"`
	PlaybackURL string `json:"playbackUrl,omitempty"`
	LiveURL     string `json:"liveUrl,omitempty"`

	// AttachmentID is the file of the posting run that a camera still is
	// made from. It is read as the card is posted and never stored.
	AttachmentID string `json:"-"`
}

// IsShown says the entry has something to show: a photo or video, a page's
// picture that was fetched, or a camera still that was kept or is one of the
// person's photos.
func (self *FeedMedia) IsShown() bool {
	switch self.FeedMediaKind {
	case FeedMediaPicture:
		return self.ImageFileID != ""
	case FeedMediaCameraStill:
		return self.ImageFileID != "" || self.PhotoID != ""
	}
	return self.PhotoID != ""
}

// IsPictureWaiting says the entry is a page's picture not fetched yet.
func (self *FeedMedia) IsPictureWaiting() bool {
	return self.FeedMediaKind == FeedMediaPicture && self.ImageFileID == "" && !self.IsPictureMissing
}

// FeedChartKind is how a chart is drawn: a series over time, or bars.
type FeedChartKind string

// The kinds of chart.
const (
	FeedChartKindSeries FeedChartKind = "series"
	FeedChartKindBars   FeedChartKind = "bars"
)

// FeedSeriesRole is what a line of a series chart is: measured, the default,
// a reference to compare with (last month, the average), or a projection of
// what is expected ahead.
type FeedSeriesRole string

// The roles of a line.
const (
	FeedSeriesMeasured   FeedSeriesRole = "measured"
	FeedSeriesReference  FeedSeriesRole = "reference"
	FeedSeriesProjection FeedSeriesRole = "projection"
)

// The limits on a chart.
const (
	// FeedChartsMost is how many charts a card carries.
	FeedChartsMost = 2

	FeedChartLabelsLeast   = 2
	FeedChartLabelsMost    = 60
	FeedChartSeriesMost    = 3
	FeedChartMarkersMost   = 3
	FeedChartBarsMost      = 8
	FeedProvenanceMost     = 120
	FeedStillReasonMost    = 120
	FeedResolutionNoteMost = 160
)

// FeedChartSeries is one line of a series chart: a number for each label of
// the axis, or nil where there is none.
type FeedChartSeries struct {
	SeriesName string         `json:"seriesName"`
	SeriesRole FeedSeriesRole `json:"seriesRole"`
	Values     []*float64     `json:"values"`
}

// FeedChartMarker is a labeled vertical rule at one of the axis labels.
type FeedChartMarker struct {
	MarkerAt    string `json:"markerAt"`
	MarkerLabel string `json:"markerLabel"`
}

// FeedChartBar is one bar of a bar chart, never negative.
type FeedChartBar struct {
	BarLabel string  `json:"barLabel"`
	BarValue float64 `json:"barValue"`
}

// FeedChartBlock is a chart on a card. Provenance says where and when the
// numbers were read.
type FeedChartBlock struct {
	ChartKind  FeedChartKind `json:"chartKind"`
	ChartTitle string        `json:"chartTitle"`

	// Unit is what the numbers count, such as "USD" or "steps".
	Unit       string `json:"unit"`
	Provenance string `json:"provenance"`

	// XLabels, Series and Markers are for a series chart.
	XLabels []string          `json:"xLabels"`
	Series  []FeedChartSeries `json:"series"`
	Markers []FeedChartMarker `json:"markers"`

	// Bars are for a bar chart, largest first.
	Bars []FeedChartBar `json:"bars"`
}

// FeedSourceLink is a page a card is built from, listed on its info sheet.
type FeedSourceLink struct {
	SourceTitle string `json:"sourceTitle"`
	SourceURL   string `json:"sourceUrl"`
}

// FeedChoice is a button on a card.
type FeedChoice struct {
	ChoiceKey   string         `json:"choiceKey"`
	ChoiceLabel string         `json:"choiceLabel"`
	ChoiceRole  FeedChoiceRole `json:"choiceRole"`
}

// AgentFeedItem is one card of the agent's feed. See
// docs/planning/agent-feed-execplan.md.
type AgentFeedItem struct {
	ID      string `json:"id"`
	AgentID string `json:"agentId"`
	UserID  string `json:"userId"`

	FeedItemKind   FeedItemKind   `json:"feedItemKind"`
	FeedSourceKind FeedSourceKind `json:"feedSourceKind"`

	// FeedSourceID is the goal, schedule or alert the card came from, the
	// computer and pane joined by a slash for a coding session, and empty for
	// a turn's own post.
	FeedSourceID string `json:"feedSourceId"`

	// RunConversationID is the run that posted it, to read the agent's work.
	RunConversationID string `json:"runConversationId"`

	Title string `json:"title"`

	// Body is Markdown: a few lines, links allowed.
	Body          string `json:"body"`
	PictureSymbol string `json:"pictureSymbol"`

	FactRows         []FeedFactRow    `json:"factRows"`
	SourceLinks      []FeedSourceLink `json:"sourceLinks"`
	RecordReferences []AgentReference `json:"recordReferences"`

	// FeedMedia are the card's pictures and videos, and the thumbnails of a
	// list card's rows.
	FeedMedia []FeedMedia `json:"feedMedia"`

	// ChartBlocks are the card's charts, at most FeedChartsMost.
	ChartBlocks []FeedChartBlock `json:"chartBlocks"`

	// CoalesceKey makes a later post with the same key from the same agent
	// replace this card. Empty for a card that is never replaced.
	CoalesceKey string `json:"coalesceKey"`

	Choices []FeedChoice `json:"choices"`

	// InteractionID is the durable question behind a decision.
	InteractionID string `json:"interactionId"`

	ChosenChoiceKey string     `json:"chosenChoiceKey"`
	ChosenAt        *time.Time `json:"chosenAt"`

	// ChosenConversationID is the main conversation a chosen action carried
	// on in.
	ChosenConversationID string `json:"chosenConversationId"`

	// OutcomeText is the agent's one line after a choice, and
	// IsOutcomePending says the turn that will write it has not ended.
	OutcomeText      string `json:"outcomeText"`
	IsOutcomePending bool   `json:"isOutcomePending"`

	FeedUrgency FeedUrgency `json:"feedUrgency"`

	// FeedStackKey names the stack the card is in: cards of one source pile
	// up and are shown as one entry. Empty for a card that stands alone.
	FeedStackKey string `json:"feedStackKey"`

	// FeedCardLayout is how the card is drawn, and LayoutContent the layout's
	// content as a JSON object, empty for text. The body is filled in plain
	// Markdown whatever the layout.
	FeedCardLayout FeedCardLayout `json:"feedCardLayout"`
	LayoutContent  string         `json:"layoutContent"`

	// StackPosition orders the cards of a stack that has an order, the
	// lowest first and a summary at 0; nil for a card that is placed by
	// when it was posted. FeedStackTitle names the stack, in the first card
	// that gives one.
	StackPosition  *int   `json:"stackPosition"`
	FeedStackTitle string `json:"feedStackTitle"`
	IsLiked        bool   `json:"isLiked"`

	// ResolvedAt is when the card was marked resolved, nil while it is not:
	// its problem cleared. ResolutionNote is the one line saying what
	// cleared, ResolvedByKind who marked it, and ResolvedByID the goal's
	// conversation or the person's user.
	ResolvedAt     *time.Time       `json:"resolvedAt"`
	ResolutionNote string           `json:"resolutionNote"`
	ResolvedByKind FeedResolverKind `json:"resolvedByKind"`
	ResolvedByID   string           `json:"resolvedById"`

	// PostNotes say what a post changed for the poster, such as a title that
	// was cut. They are answered with the post and never stored.
	PostNotes []string `json:"-"`

	ReadAt      *time.Time `json:"readAt"`
	DismissedAt *time.Time `json:"dismissedAt"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	ExpiresAt   *time.Time `json:"expiresAt"`
}

// IsResolved says whether the card is marked resolved.
func (self *AgentFeedItem) IsResolved() bool { return self.ResolvedAt != nil }

// IsChosen says whether a choice was made on the card.
func (self *AgentFeedItem) IsChosen() bool { return self.ChosenChoiceKey != "" }

// FindChoice is the card's button with the key, or nil.
func (self *AgentFeedItem) FindChoice(choiceKey string) *FeedChoice {
	for index := range self.Choices {
		if self.Choices[index].ChoiceKey == choiceKey {
			return &self.Choices[index]
		}
	}
	return nil
}

// HasChoiceRole says whether any button of the card has the role.
func (self *AgentFeedItem) HasChoiceRole(choiceRole FeedChoiceRole) bool {
	for _, choice := range self.Choices {
		if choice.ChoiceRole == choiceRole {
			return true
		}
	}
	return false
}

// FeedSettlement is what settles a card: the choice, what came of it, and
// where it carried on.
type FeedSettlement struct {
	ChoiceKey      string
	OutcomeText    string
	ConversationID string

	// IsOutcomePending says a turn is running whose last words will be the
	// outcome.
	IsOutcomePending bool
}

// FeedStackCounts is what a stack holds: how many cards, and how many of
// them are unread.
type FeedStackCounts struct {
	ItemCount   int
	UnreadCount int

	// StackTitle is the first title any card of the stack gave.
	StackTitle string
}

// CanStack says whether the card may be in a stack. A card that needs the
// person on its own never stacks: one with a button that acts, dismisses or
// opens, and an alert. A coding session's questions do stack, a day of them
// in one stack, since a busy day of sessions asks many, and each card still
// answers its own session once the stack is open.
func (self *AgentFeedItem) CanStack() bool {
	if self.FeedItemKind == FeedItemAlert {
		return false
	}
	if self.FeedSourceKind == FeedSourceCodingSession {
		return true
	}
	for _, choice := range self.Choices {
		if choice.ChoiceRole != FeedChoiceDiscuss {
			return false
		}
	}
	return true
}

// DefaultFeedStackKey is the stack a card goes in when its poster named
// none: its source for a goal, the reason for the agent speaking first, and
// for a schedule the run, so that each morning's brief is a stack of its own.
func (self *AgentFeedItem) DefaultFeedStackKey() string {
	switch self.FeedSourceKind {
	case FeedSourceGoal, FeedSourceSpeakFirst:
		if self.FeedSourceID != "" {
			return string(self.FeedSourceKind) + ":" + self.FeedSourceID
		}
	}
	return ""
}

// FeedStackKeyCodingDay is the stack of the coding sessions' questions of one
// day, the day as the person's clock tells it.
func FeedStackKeyCodingDay(day time.Time) string {
	return "coding:" + day.Format(time.DateOnly)
}

// FeedCursor is how far a page of the feed got: the time the page is
// ordered by (when the card was posted, or for the dismissed cards when it
// was dismissed) and the id of its last card.
type FeedCursor struct {
	OrderedAt time.Time
	ID        string
}

// FeedEvent is what a screen with the feed open is told.
type FeedEvent struct {
	UserID      string     `json:"userId"`
	FeedItemID  string     `json:"feedItemId"`
	FeedChange  FeedChange `json:"feedChange"`
	UnreadCount int        `json:"unreadCount"`
}

// FeedChoiceKeyOption is the key of a coding session's option, "option:3".
func FeedChoiceKeyOption(optionNumber int) string {
	return "option:" + strconv.Itoa(optionNumber)
}

// FeedOptionNumber is the number in an "option:N" key, and whether the key
// is one.
func FeedOptionNumber(choiceKey string) (int, bool) {
	rest, isOption := strings.CutPrefix(choiceKey, "option:")
	if !isOption || rest == "" {
		return 0, false
	}
	number := 0
	for _, digit := range rest {
		if digit < '0' || digit > '9' {
			return 0, false
		}
		number = number*10 + int(digit-'0')
	}
	return number, true
}
