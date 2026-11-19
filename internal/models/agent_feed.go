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

// FeedSourceKind is what made a card: a goal, a schedule, an alert, the
// agent speaking first, a coding session, or a turn's own post.
type FeedSourceKind string

// The kinds of source.
const (
	FeedSourceGoal          FeedSourceKind = "goal"
	FeedSourceSchedule      FeedSourceKind = "schedule"
	FeedSourceAlert         FeedSourceKind = "alert"
	FeedSourceSpeakFirst    FeedSourceKind = "speak_first"
	FeedSourceCodingSession FeedSourceKind = "coding_session"
	FeedSourceAgent         FeedSourceKind = "agent"
)

// IsFeedSourceKind says whether a word is one of the kinds of source.
func IsFeedSourceKind(value FeedSourceKind) bool {
	switch value {
	case FeedSourceGoal, FeedSourceSchedule, FeedSourceAlert, FeedSourceSpeakFirst, FeedSourceCodingSession, FeedSourceAgent:
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
)

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

	// FeedSourceURLDays is how far back a post is refused for linking a page
	// an open card already links.
	FeedSourceURLDays = 30
)

// FeedFactRow is one line of a card's short table.
type FeedFactRow struct {
	FactLabel string `json:"factLabel"`
	FactValue string `json:"factValue"`
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

	ReadAt      *time.Time `json:"readAt"`
	DismissedAt *time.Time `json:"dismissedAt"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	ExpiresAt   *time.Time `json:"expiresAt"`
}

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
// opens, an alert, and a coding session's question.
func (self *AgentFeedItem) CanStack() bool {
	if self.FeedItemKind == FeedItemAlert || self.FeedSourceKind == FeedSourceCodingSession {
		return false
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
	case FeedSourceSchedule:
		if self.FeedSourceID != "" && self.RunConversationID != "" {
			return "schedule:" + self.FeedSourceID + ":" + self.RunConversationID
		}
		if self.FeedSourceID != "" {
			return "schedule:" + self.FeedSourceID
		}
	case FeedSourceGoal, FeedSourceSpeakFirst:
		if self.FeedSourceID != "" {
			return string(self.FeedSourceKind) + ":" + self.FeedSourceID
		}
	}
	return ""
}

// FeedCursor is how far a page of the feed got.
type FeedCursor struct {
	CreatedAt time.Time
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
