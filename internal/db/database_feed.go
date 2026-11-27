package db

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

// FeedOperation keeps the cards of an agent's feed: what it said without
// being asked, newest first. See docs/planning/agent-feed-execplan.md.
type FeedOperation interface {
	// UpsertAgentFeedItem stores a card. One whose coalesce key matches an
	// open card of the same agent replaces that card's content, moves it to
	// the top and makes it unread; the change says which it was.
	UpsertAgentFeedItem(item *models.AgentFeedItem, now time.Time) (*models.AgentFeedItem, models.FeedChange, error)

	// GetAgentFeedItem is one card, or nil.
	GetAgentFeedItem(feedItemId string) (*models.AgentFeedItem, error)

	// GetOpenAgentFeedItemByCoalesceKey is the agent's card that is not
	// dismissed and has the key, or nil.
	GetOpenAgentFeedItemByCoalesceKey(agentId, coalesceKey string) (*models.AgentFeedItem, error)

	// FindOpenAgentFeedItemBySourceURL is the agent's card, not dismissed and
	// posted since the time, that lists the page among its source links, or
	// nil: a card about it is not posted twice.
	FindOpenAgentFeedItemBySourceURL(agentId, sourceUrl string, since time.Time) (*models.AgentFeedItem, error)

	// ListAgentFeedItems is a person's feed, newest first, at most limit
	// entries, after the cursor when there is one, and the cursor of the page
	// after this one, nil when this is the last. An entry is a card that
	// stands alone or the newest card of a stack. Dismissed cards only when
	// asked for, and cards past their expiry never.
	ListAgentFeedItems(userId string, cursor *models.FeedCursor, limit int, includeDismissed bool, now time.Time) ([]*models.AgentFeedItem, *models.FeedCursor, error)

	// ListDismissedAgentFeedItems is a person's dismissed cards that have not
	// expired, the most recently dismissed first, at most limit, after the
	// cursor when there is one, and the cursor of the next page, nil on the
	// last. A stack is not folded: each dismissed card is an entry.
	ListDismissedAgentFeedItems(userId string, cursor *models.FeedCursor, limit int, now time.Time) ([]*models.AgentFeedItem, *models.FeedCursor, error)

	// ListUnsettledAgentFeedItemsBySource is the agent's cards of a source
	// that are neither chosen nor dismissed.
	ListUnsettledAgentFeedItemsBySource(agentId string, feedSourceKind models.FeedSourceKind, feedSourceId string) ([]*models.AgentFeedItem, error)

	// ListAgentFeedStackItems is the cards of one stack from the offset: those
	// with a position first, the lowest first, then the rest newest first. It
	// says where the next page starts, or -1 on the last.
	ListAgentFeedStackItems(userId, feedStackKey string, offset, limit int, includeDismissed bool, now time.Time) ([]*models.AgentFeedItem, int, error)

	// CountAgentFeedStacks is what each of the stacks holds, by key: the
	// cards in it that are not dismissed or expired, and how many of them are
	// unread.
	CountAgentFeedStacks(userId string, feedStackKeys []string, now time.Time) (map[string]*models.FeedStackCounts, error)

	// CountUnreadAgentFeedItems is how many of a person's cards are neither
	// read, dismissed nor expired.
	CountUnreadAgentFeedItems(userId string, now time.Time) (int, error)

	// CountAgentFeedItemsInRun is how many cards a run conversation posted.
	CountAgentFeedItemsInRun(runConversationId string) (int, error)

	// UpdateAgentFeedItem changes what a card says; the choice made on it,
	// its readings and its likes are not part of that.
	UpdateAgentFeedItem(feedItemId string, modify func(*models.AgentFeedItem) error, now time.Time) (*models.AgentFeedItem, error)

	// SettleAgentFeedItem records the choice made on a card and what came of
	// it, and says whether this call was the one that did: a card is settled
	// once.
	SettleAgentFeedItem(feedItemId string, settlement *models.FeedSettlement, now time.Time) (bool, error)

	// ResolveAgentFeedItem marks a card resolved and says whether this call
	// was the one that did: a card is resolved once, so the first resolution
	// and its note stand. The card does not move, become unread or expire
	// later.
	ResolveAgentFeedItem(feedItemId string, resolution *models.FeedResolution, now time.Time) (bool, error)

	// UnresolveAgentFeedItem takes the mark off and says whether the card was
	// resolved.
	UnresolveAgentFeedItem(feedItemId string, now time.Time) (bool, error)

	// SetAgentFeedItemOutcome writes the line a chosen card ends with and
	// says the turn behind it is over.
	SetAgentFeedItemOutcome(feedItemId, outcomeText string, now time.Time) error

	// SetAgentFeedItemLiked records a like or an unlike.
	SetAgentFeedItemLiked(feedItemId string, isLiked bool, now time.Time) error

	// MarkAgentFeedItemsRead marks the person's cards read and says how many
	// were newly read.
	MarkAgentFeedItemsRead(userId string, feedItemIds []string, now time.Time) (int, error)

	// DismissAgentFeedItems closes the person's cards the selection names
	// and answers the ones this call closed; a card already closed is left
	// out. A selection by every read card leaves out a decision still waiting
	// for an answer.
	DismissAgentFeedItems(userId string, selection *FeedDismissSelection, now time.Time) ([]*models.AgentFeedItem, error)

	// RestoreAgentFeedItems brings back the person's dismissed cards and
	// answers the ones this call brought back, still read. A card a dismiss
	// choice settled has the answer cleared, so it can be answered again; one
	// answered with an action keeps its answer, since the action was carried
	// out. A card whose coalesce key a card that is not dismissed holds is
	// left out, as is one that expired.
	RestoreAgentFeedItems(userId string, feedItemIds []string, now time.Time) ([]*models.AgentFeedItem, error)

	// DismissAgentFeedItemsAnsweredBefore dismisses every card answered with a
	// choice before the time that is not dismissed yet, and answers them.
	DismissAgentFeedItemsAnsweredBefore(before, now time.Time) ([]*models.AgentFeedItem, error)

	// DeleteAgentFeedItem removes a card for good: one the agent took back
	// is not among the dismissed cards to be restored.
	DeleteAgentFeedItem(feedItemId string) error

	// ExpireAgentFeedItems deletes cards that expired before the time, and
	// says how many.
	ExpireAgentFeedItems(before time.Time) (int64, error)
}

// FeedDismissSelection says which cards to dismiss: exactly one of its three
// fields is set.
type FeedDismissSelection struct {
	// FeedItemIDs are those cards.
	FeedItemIDs []string

	// FeedStackKey is every open card of that stack.
	FeedStackKey string

	// IsEveryReadCard is every card that is read and has no open choice.
	IsEveryReadCard bool
}

type agentFeedItemModel struct {
	ID      string `gorm:"column:id;primaryKey"`
	AgentID string `gorm:"column:agent_id"`
	UserID  string `gorm:"column:user_id"`

	FeedItemKind      string `gorm:"column:feed_item_kind"`
	FeedSourceKind    string `gorm:"column:feed_source_kind"`
	FeedSourceID      string `gorm:"column:feed_source_id"`
	RunConversationID string `gorm:"column:run_conversation_id"`

	Title         string `gorm:"column:title"`
	Body          string `gorm:"column:body"`
	PictureSymbol string `gorm:"column:picture_symbol"`

	FactRows         []byte `gorm:"column:fact_rows;type:jsonb"`
	SourceLinks      []byte `gorm:"column:source_links;type:jsonb"`
	RecordReferences []byte `gorm:"column:record_references;type:jsonb"`
	FeedMedia        []byte `gorm:"column:feed_media;type:jsonb"`
	ChartBlocks      []byte `gorm:"column:chart_blocks;type:jsonb"`

	CoalesceKey *string `gorm:"column:coalesce_key"`
	Choices     []byte  `gorm:"column:choices;type:jsonb"`

	InteractionID        *string    `gorm:"column:interaction_id"`
	ChosenChoiceKey      *string    `gorm:"column:chosen_choice_key"`
	ChosenAt             *time.Time `gorm:"column:chosen_at"`
	ChosenConversationID string     `gorm:"column:chosen_conversation_id"`
	OutcomeText          string     `gorm:"column:outcome_text"`
	IsOutcomePending     bool       `gorm:"column:is_outcome_pending"`

	FeedUrgency    string `gorm:"column:feed_urgency"`
	FeedStackKey   string `gorm:"column:feed_stack_key"`
	FeedCardLayout string `gorm:"column:feed_card_layout"`
	LayoutContent  []byte `gorm:"column:layout_content;type:jsonb"`
	StackPosition  *int   `gorm:"column:stack_position"`
	FeedStackTitle string `gorm:"column:feed_stack_title"`
	IsLiked        bool   `gorm:"column:is_liked"`

	ResolvedAt     *time.Time `gorm:"column:resolved_at"`
	ResolutionNote string     `gorm:"column:resolution_note"`
	ResolvedByKind string     `gorm:"column:resolved_by_kind"`
	ResolvedByID   string     `gorm:"column:resolved_by_id"`

	ReadAt      *time.Time `gorm:"column:read_at"`
	DismissedAt *time.Time `gorm:"column:dismissed_at"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	UpdatedAt   time.Time  `gorm:"column:updated_at"`
	ExpiresAt   *time.Time `gorm:"column:expires_at"`
}

func (agentFeedItemModel) TableName() string { return "agent_feed_item" }

// jsonArray is a list as a jsonb column holds it: an empty list rather than
// NULL, which the column does not take.
func jsonArray(list any) ([]byte, error) {
	encoded, err := json.Marshal(list)
	if err != nil {
		return nil, err
	}
	if string(encoded) == "null" {
		return []byte("[]"), nil
	}
	return encoded, nil
}

func feedItemToModel(item *models.AgentFeedItem) (*agentFeedItemModel, error) {
	model := &agentFeedItemModel{
		ID: item.ID, AgentID: item.AgentID, UserID: item.UserID,
		FeedItemKind: string(item.FeedItemKind), FeedSourceKind: string(item.FeedSourceKind), FeedSourceID: item.FeedSourceID,
		RunConversationID: item.RunConversationID,
		Title:             item.Title, Body: item.Body, PictureSymbol: item.PictureSymbol,
		CoalesceKey: optionalID(item.CoalesceKey), InteractionID: optionalID(item.InteractionID),
		ChosenChoiceKey: optionalID(item.ChosenChoiceKey), ChosenAt: item.ChosenAt, ChosenConversationID: item.ChosenConversationID,
		OutcomeText: item.OutcomeText, IsOutcomePending: item.IsOutcomePending,
		FeedUrgency: string(item.FeedUrgency), FeedStackKey: item.FeedStackKey, FeedCardLayout: string(item.FeedCardLayout), StackPosition: item.StackPosition, FeedStackTitle: item.FeedStackTitle, IsLiked: item.IsLiked,
		ResolvedAt: item.ResolvedAt, ResolutionNote: item.ResolutionNote, ResolvedByKind: string(item.ResolvedByKind), ResolvedByID: item.ResolvedByID,
		ReadAt: item.ReadAt, DismissedAt: item.DismissedAt, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt, ExpiresAt: item.ExpiresAt,
	}
	if item.LayoutContent != "" {
		model.LayoutContent = []byte(item.LayoutContent)
	}
	if model.FeedCardLayout == "" {
		model.FeedCardLayout = string(models.FeedLayoutText)
	}
	var err error
	if model.FactRows, err = jsonArray(item.FactRows); err != nil {
		return nil, err
	}
	if model.SourceLinks, err = jsonArray(item.SourceLinks); err != nil {
		return nil, err
	}
	if model.RecordReferences, err = jsonArray(item.RecordReferences); err != nil {
		return nil, err
	}
	if model.Choices, err = jsonArray(item.Choices); err != nil {
		return nil, err
	}
	if model.FeedMedia, err = jsonArray(item.FeedMedia); err != nil {
		return nil, err
	}
	if model.ChartBlocks, err = jsonArray(item.ChartBlocks); err != nil {
		return nil, err
	}
	return model, nil
}

func (self *agentFeedItemModel) toModel() (*models.AgentFeedItem, error) {
	item := &models.AgentFeedItem{
		ID: self.ID, AgentID: self.AgentID, UserID: self.UserID,
		FeedItemKind: models.FeedItemKind(self.FeedItemKind), FeedSourceKind: models.FeedSourceKind(self.FeedSourceKind), FeedSourceID: self.FeedSourceID,
		RunConversationID: self.RunConversationID,
		Title:             self.Title, Body: self.Body, PictureSymbol: self.PictureSymbol,
		FactRows: []models.FeedFactRow{}, SourceLinks: []models.FeedSourceLink{}, RecordReferences: []models.AgentReference{}, Choices: []models.FeedChoice{}, FeedMedia: []models.FeedMedia{}, ChartBlocks: []models.FeedChartBlock{},
		ChosenAt: localTime(self.ChosenAt), ChosenConversationID: self.ChosenConversationID,
		OutcomeText: self.OutcomeText, IsOutcomePending: self.IsOutcomePending,
		FeedUrgency: models.FeedUrgency(self.FeedUrgency), FeedStackKey: self.FeedStackKey, FeedCardLayout: models.FeedCardLayout(self.FeedCardLayout), LayoutContent: string(self.LayoutContent), StackPosition: self.StackPosition, FeedStackTitle: self.FeedStackTitle, IsLiked: self.IsLiked,
		ResolvedAt: localTime(self.ResolvedAt), ResolutionNote: self.ResolutionNote, ResolvedByKind: models.FeedResolverKind(self.ResolvedByKind), ResolvedByID: self.ResolvedByID,
		ReadAt: localTime(self.ReadAt), DismissedAt: localTime(self.DismissedAt),
		CreatedAt: self.CreatedAt.In(time.Local), UpdatedAt: self.UpdatedAt.In(time.Local), ExpiresAt: localTime(self.ExpiresAt),
	}
	item.CoalesceKey, item.InteractionID, item.ChosenChoiceKey = optionalString(self.CoalesceKey), optionalString(self.InteractionID), optionalString(self.ChosenChoiceKey)
	for _, column := range []struct {
		data   []byte
		target any
	}{{self.FactRows, &item.FactRows}, {self.SourceLinks, &item.SourceLinks}, {self.RecordReferences, &item.RecordReferences}, {self.Choices, &item.Choices}, {self.FeedMedia, &item.FeedMedia}, {self.ChartBlocks, &item.ChartBlocks}} {
		if err := decodeJSON(column.data, column.target); err != nil {
			return nil, err
		}
	}
	return item, nil
}

// feedExpiry is when a card leaves the list: never while a decision waits
// for an answer, thirty days after it was posted otherwise.
func feedExpiry(item *models.AgentFeedItem, now time.Time) *time.Time {
	if item.FeedItemKind == models.FeedItemDecision && len(item.Choices) > 0 {
		return nil
	}
	expiresAt := now.AddDate(0, 0, models.FeedExpiryDays)
	return &expiresAt
}

func (self *transaction) UpsertAgentFeedItem(item *models.AgentFeedItem, now time.Time) (*models.AgentFeedItem, models.FeedChange, error) {
	if item.AgentID == "" || item.UserID == "" {
		return nil, "", fmt.Errorf("db: a card of the feed needs the agent and the person")
	}
	if strings.TrimSpace(item.Title) == "" {
		return nil, "", fmt.Errorf("db: a card of the feed needs a headline")
	}
	posted := *item
	posted.CreatedAt, posted.UpdatedAt = now, now
	posted.ExpiresAt = feedExpiry(&posted, now)
	posted.ReadAt, posted.DismissedAt = nil, nil
	posted.ResolvedAt, posted.ResolutionNote, posted.ResolvedByKind, posted.ResolvedByID = nil, "", "", ""
	posted.ChosenChoiceKey, posted.ChosenAt, posted.ChosenConversationID, posted.OutcomeText, posted.IsOutcomePending = "", nil, "", "", false

	if posted.CoalesceKey != "" {
		var matches []agentFeedItemModel
		if err := self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where(`"agent_id" = ? AND "coalesce_key" = ? AND "dismissed_at" IS NULL`, posted.AgentID, posted.CoalesceKey).
			Limit(1).Find(&matches).Error; err != nil {
			return nil, "", err
		}
		if len(matches) == 1 {
			existing := matches[0]
			posted.ID, posted.IsLiked = existing.ID, existing.IsLiked
			model, err := feedItemToModel(&posted)
			if err != nil {
				return nil, "", err
			}
			updates := map[string]any{
				"created_at": now, "updated_at": now, "expires_at": model.ExpiresAt, "read_at": nil,
				"chosen_choice_key": nil, "chosen_at": nil, "chosen_conversation_id": "", "outcome_text": "", "is_outcome_pending": false,
				"feed_item_kind": model.FeedItemKind, "feed_source_kind": model.FeedSourceKind, "feed_source_id": model.FeedSourceID,
				"run_conversation_id": model.RunConversationID, "title": model.Title, "body": model.Body, "picture_symbol": model.PictureSymbol,
				"fact_rows": model.FactRows, "source_links": model.SourceLinks, "record_references": model.RecordReferences, "feed_media": model.FeedMedia, "chart_blocks": model.ChartBlocks,
				// A new post on a subject is a new state of it: the earlier resolution does not carry over.
				"resolved_at": nil, "resolution_note": "", "resolved_by_kind": "", "resolved_by_id": "",
				"choices": model.Choices, "interaction_id": model.InteractionID, "feed_urgency": model.FeedUrgency, "feed_stack_key": model.FeedStackKey, "feed_card_layout": model.FeedCardLayout, "layout_content": model.LayoutContent, "stack_position": model.StackPosition, "feed_stack_title": model.FeedStackTitle,
			}
			if err := self.tx.Model(&agentFeedItemModel{}).Where(`"id" = ?`, existing.ID).Updates(updates).Error; err != nil {
				return nil, "", err
			}
			stored, err := self.GetAgentFeedItem(existing.ID)
			return stored, models.FeedChangeReplaced, err
		}
	}
	posted.ID = newID()
	model, err := feedItemToModel(&posted)
	if err != nil {
		return nil, "", err
	}
	if err := self.tx.Create(model).Error; err != nil {
		return nil, "", err
	}
	stored, err := model.toModel()
	return stored, models.FeedChangePosted, err
}

func (self *transaction) GetAgentFeedItem(feedItemId string) (*models.AgentFeedItem, error) {
	var found []agentFeedItemModel
	if err := self.tx.Where(`"id" = ?`, feedItemId).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return found[0].toModel()
}

func (self *transaction) GetOpenAgentFeedItemByCoalesceKey(agentId, coalesceKey string) (*models.AgentFeedItem, error) {
	var found []agentFeedItemModel
	if err := self.tx.Where(`"agent_id" = ? AND "coalesce_key" = ? AND "dismissed_at" IS NULL`, agentId, coalesceKey).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return found[0].toModel()
}

func (self *transaction) FindOpenAgentFeedItemBySourceURL(agentId, sourceUrl string, since time.Time) (*models.AgentFeedItem, error) {
	contained, err := json.Marshal([]map[string]string{{"sourceUrl": sourceUrl}})
	if err != nil {
		return nil, err
	}
	var found []agentFeedItemModel
	if err := self.tx.Where(`"agent_id" = ? AND "dismissed_at" IS NULL AND "created_at" >= ? AND "source_links" @> ?::jsonb`, agentId, since, string(contained)).
		Order(`"created_at" DESC`).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return found[0].toModel()
}

// feedCursorSeparator joins the parts of a cursor in the text clients carry.
const feedCursorSeparator = "|"

// EncodeFeedCursor is a cursor as the text a client hands back to read the
// next page.
func EncodeFeedCursor(cursor *models.FeedCursor) string {
	if cursor == nil {
		return ""
	}
	return strconv.FormatInt(cursor.OrderedAt.UnixNano(), 10) + feedCursorSeparator + cursor.ID
}

// DecodeFeedCursor reads what EncodeFeedCursor wrote; empty is no cursor.
func DecodeFeedCursor(text string) (*models.FeedCursor, error) {
	if text == "" {
		return nil, nil
	}
	nanoseconds, identifier, isCursor := strings.Cut(text, feedCursorSeparator)
	if !isCursor || identifier == "" {
		return nil, fmt.Errorf("db: %q is not a cursor of the feed", text)
	}
	parsed, err := strconv.ParseInt(nanoseconds, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("db: %q is not a cursor of the feed", text)
	}
	return &models.FeedCursor{OrderedAt: time.Unix(0, parsed), ID: identifier}, nil
}

func (self *transaction) ListAgentFeedItems(userId string, cursor *models.FeedCursor, limit int, includeDismissed bool, now time.Time) ([]*models.AgentFeedItem, *models.FeedCursor, error) {
	query := self.tx.Where(`"user_id" = ? AND ("expires_at" IS NULL OR "expires_at" > ?)`, userId, now)
	// Only the newest card of a stack is an entry, the newest of those the
	// list would show.
	stackNewest := `SELECT "inner_item"."id" FROM "agent_feed_item" "inner_item"
		WHERE "inner_item"."user_id" = "agent_feed_item"."user_id" AND "inner_item"."feed_stack_key" = "agent_feed_item"."feed_stack_key"
		AND ("inner_item"."expires_at" IS NULL OR "inner_item"."expires_at" > ?)`
	stackArguments := []any{now}
	if !includeDismissed {
		query = query.Where(`"dismissed_at" IS NULL`)
		stackNewest += ` AND "inner_item"."dismissed_at" IS NULL`
	}
	// The top of a stack is its lowest position, else its newest card.
	stackNewest += ` ORDER BY ("inner_item"."stack_position" IS NULL), "inner_item"."stack_position", "inner_item"."created_at" DESC, "inner_item"."id" DESC LIMIT 1`
	query = query.Where(`("feed_stack_key" = '' OR "id" = (`+stackNewest+`))`, stackArguments...)
	return self.pageFeedItems(query, cursor, limit)
}

func (self *transaction) ListAgentFeedStackItems(userId, feedStackKey string, offset, limit int, includeDismissed bool, now time.Time) ([]*models.AgentFeedItem, int, error) {
	if limit <= 0 {
		limit = 30
	}
	query := self.tx.Where(`"user_id" = ? AND "feed_stack_key" = ? AND ("expires_at" IS NULL OR "expires_at" > ?)`, userId, feedStackKey, now)
	if !includeDismissed {
		query = query.Where(`"dismissed_at" IS NULL`)
	}
	var found []agentFeedItemModel
	if err := query.Order(`("stack_position" IS NULL), "stack_position", "created_at" DESC, "id" DESC`).Offset(max(offset, 0)).Limit(limit + 1).Find(&found).Error; err != nil {
		return nil, 0, err
	}
	next := -1
	if len(found) > limit {
		found, next = found[:limit], max(offset, 0)+limit
	}
	items := make([]*models.AgentFeedItem, 0, len(found))
	for index := range found {
		item, err := found[index].toModel()
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, next, nil
}

func (self *transaction) ListDismissedAgentFeedItems(userId string, cursor *models.FeedCursor, limit int, now time.Time) ([]*models.AgentFeedItem, *models.FeedCursor, error) {
	if limit <= 0 {
		limit = 30
	}
	query := self.tx.Where(`"user_id" = ? AND "dismissed_at" IS NOT NULL AND ("expires_at" IS NULL OR "expires_at" > ?)`, userId, now)
	if cursor != nil {
		query = query.Where(`("dismissed_at", "id") < (?, ?)`, cursor.OrderedAt, cursor.ID)
	}
	var found []agentFeedItemModel
	if err := query.Order(`"dismissed_at" DESC, "id" DESC`).Limit(limit + 1).Find(&found).Error; err != nil {
		return nil, nil, err
	}
	var next *models.FeedCursor
	if len(found) > limit {
		found = found[:limit]
		next = &models.FeedCursor{OrderedAt: *found[limit-1].DismissedAt, ID: found[limit-1].ID}
	}
	items := make([]*models.AgentFeedItem, 0, len(found))
	for index := range found {
		item, err := found[index].toModel()
		if err != nil {
			return nil, nil, err
		}
		items = append(items, item)
	}
	return items, next, nil
}

// pageFeedItems reads a page of the cards a query selects, newest first.
func (self *transaction) pageFeedItems(query *gorm.DB, cursor *models.FeedCursor, limit int) ([]*models.AgentFeedItem, *models.FeedCursor, error) {
	if limit <= 0 {
		limit = 30
	}
	if cursor != nil {
		query = query.Where(`("created_at", "id") < (?, ?)`, cursor.OrderedAt, cursor.ID)
	}
	var found []agentFeedItemModel
	if err := query.Order(`"created_at" DESC, "id" DESC`).Limit(limit + 1).Find(&found).Error; err != nil {
		return nil, nil, err
	}
	var next *models.FeedCursor
	if len(found) > limit {
		found = found[:limit]
		next = &models.FeedCursor{OrderedAt: found[limit-1].CreatedAt, ID: found[limit-1].ID}
	}
	items := make([]*models.AgentFeedItem, 0, len(found))
	for index := range found {
		item, err := found[index].toModel()
		if err != nil {
			return nil, nil, err
		}
		items = append(items, item)
	}
	return items, next, nil
}

func (self *transaction) CountUnreadAgentFeedItems(userId string, now time.Time) (int, error) {
	var total int64
	if err := self.tx.Model(&agentFeedItemModel{}).
		Where(`"user_id" = ? AND "read_at" IS NULL AND "dismissed_at" IS NULL AND ("expires_at" IS NULL OR "expires_at" > ?)`, userId, now).
		Count(&total).Error; err != nil {
		return 0, err
	}
	return int(total), nil
}

func (self *transaction) CountAgentFeedItemsInRun(runConversationId string) (int, error) {
	var total int64
	if err := self.tx.Model(&agentFeedItemModel{}).Where(`"run_conversation_id" = ?`, runConversationId).Count(&total).Error; err != nil {
		return 0, err
	}
	return int(total), nil
}

func (self *transaction) UpdateAgentFeedItem(feedItemId string, modify func(*models.AgentFeedItem) error, now time.Time) (*models.AgentFeedItem, error) {
	var existing agentFeedItemModel
	if err := self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(`"id" = ?`, feedItemId).First(&existing).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	item, err := existing.toModel()
	if err != nil {
		return nil, err
	}
	if err := modify(item); err != nil {
		return nil, err
	}
	model, err := feedItemToModel(item)
	if err != nil {
		return nil, err
	}
	// A key taken by another open card is refused by the unique index.
	updates := map[string]any{
		"updated_at": now, "coalesce_key": model.CoalesceKey, "feed_item_kind": model.FeedItemKind, "title": model.Title, "body": model.Body,
		"picture_symbol": model.PictureSymbol, "fact_rows": model.FactRows, "source_links": model.SourceLinks,
		"record_references": model.RecordReferences, "feed_media": model.FeedMedia, "chart_blocks": model.ChartBlocks, "choices": model.Choices, "interaction_id": model.InteractionID, "feed_urgency": model.FeedUrgency,
		"feed_stack_key": model.FeedStackKey, "stack_position": model.StackPosition, "feed_stack_title": model.FeedStackTitle,
		"feed_card_layout": model.FeedCardLayout, "layout_content": model.LayoutContent,
	}
	if err := self.tx.Model(&agentFeedItemModel{}).Where(`"id" = ?`, feedItemId).Updates(updates).Error; err != nil {
		return nil, err
	}
	return self.GetAgentFeedItem(feedItemId)
}

func (self *transaction) SettleAgentFeedItem(feedItemId string, settlement *models.FeedSettlement, now time.Time) (bool, error) {
	expiresAt := now.AddDate(0, 0, models.FeedExpiryDays)
	result := self.tx.Model(&agentFeedItemModel{}).Where(`"id" = ? AND "chosen_choice_key" IS NULL`, feedItemId).
		Updates(map[string]any{
			"chosen_choice_key": settlement.ChoiceKey, "chosen_at": now, "chosen_conversation_id": settlement.ConversationID,
			"outcome_text": settlement.OutcomeText, "is_outcome_pending": settlement.IsOutcomePending,
			"read_at": gorm.Expr(`COALESCE("read_at", ?::timestamptz)`, now), "updated_at": now, "expires_at": expiresAt,
		})
	return result.RowsAffected == 1, result.Error
}

func (self *transaction) ResolveAgentFeedItem(feedItemId string, resolution *models.FeedResolution, now time.Time) (bool, error) {
	result := self.tx.Model(&agentFeedItemModel{}).Where(`"id" = ? AND "resolved_at" IS NULL`, feedItemId).
		Updates(map[string]any{
			"resolved_at": now, "resolution_note": resolution.ResolutionNote,
			"resolved_by_kind": string(resolution.ResolvedByKind), "resolved_by_id": resolution.ResolvedByID, "updated_at": now,
		})
	return result.RowsAffected == 1, result.Error
}

func (self *transaction) UnresolveAgentFeedItem(feedItemId string, now time.Time) (bool, error) {
	result := self.tx.Model(&agentFeedItemModel{}).Where(`"id" = ? AND "resolved_at" IS NOT NULL`, feedItemId).
		Updates(map[string]any{"resolved_at": nil, "resolution_note": "", "resolved_by_kind": "", "resolved_by_id": "", "updated_at": now})
	return result.RowsAffected == 1, result.Error
}

func (self *transaction) SetAgentFeedItemOutcome(feedItemId, outcomeText string, now time.Time) error {
	return self.tx.Model(&agentFeedItemModel{}).Where(`"id" = ?`, feedItemId).
		Updates(map[string]any{"outcome_text": outcomeText, "is_outcome_pending": false, "updated_at": now}).Error
}

func (self *transaction) SetAgentFeedItemLiked(feedItemId string, isLiked bool, now time.Time) error {
	return self.tx.Model(&agentFeedItemModel{}).Where(`"id" = ?`, feedItemId).
		Updates(map[string]any{"is_liked": isLiked, "updated_at": now}).Error
}

func (self *transaction) MarkAgentFeedItemsRead(userId string, feedItemIds []string, now time.Time) (int, error) {
	if len(feedItemIds) == 0 {
		return 0, nil
	}
	result := self.tx.Model(&agentFeedItemModel{}).Where(`"user_id" = ? AND "id" IN ? AND "read_at" IS NULL`, userId, feedItemIds).
		Update("read_at", now)
	return int(result.RowsAffected), result.Error
}

func (self *transaction) DismissAgentFeedItems(userId string, selection *FeedDismissSelection, now time.Time) ([]*models.AgentFeedItem, error) {
	// A card past its expiry is out of the feed already, though not yet
	// deleted: it is neither dismissed nor announced.
	query := self.tx.Where(`"user_id" = ? AND "dismissed_at" IS NULL AND ("expires_at" IS NULL OR "expires_at" > ?)`, userId, now)
	switch {
	case len(selection.FeedItemIDs) > 0:
		query = query.Where(`"id" IN ?`, selection.FeedItemIDs)
	case selection.FeedStackKey != "":
		query = query.Where(`"feed_stack_key" = ?`, selection.FeedStackKey)
	case selection.IsEveryReadCard:
		// A card still waiting for an answer stays, whatever its kind: the
		// agent may put choices on any card.
		query = query.Where(`"read_at" IS NOT NULL AND NOT (jsonb_array_length("choices") > 0 AND "chosen_choice_key" IS NULL)`)
	default:
		return []*models.AgentFeedItem{}, nil
	}
	return updateFeedItems(query, map[string]any{"dismissed_at": now, "read_at": gorm.Expr(`COALESCE("read_at", ?::timestamptz)`, now), "updated_at": now})
}

func (self *transaction) RestoreAgentFeedItems(userId string, feedItemIds []string, now time.Time) ([]*models.AgentFeedItem, error) {
	if len(feedItemIds) == 0 {
		return []*models.AgentFeedItem{}, nil
	}
	// A key held by an open card would break the unique index, and the card
	// holding it is the newer word on that subject.
	query := self.tx.Where(`"user_id" = ? AND "id" IN ? AND "dismissed_at" IS NOT NULL AND ("expires_at" IS NULL OR "expires_at" > ?)`, userId, feedItemIds, now).
		Where(`("coalesce_key" IS NULL OR NOT EXISTS (SELECT 1 FROM "agent_feed_item" "open_item" WHERE "open_item"."agent_id" = "agent_feed_item"."agent_id"
			AND "open_item"."coalesce_key" = "agent_feed_item"."coalesce_key" AND "open_item"."dismissed_at" IS NULL))`)
	var candidates []agentFeedItemModel
	if err := query.Clauses(clause.Locking{Strength: "UPDATE"}).Select(`"id"`, `"coalesce_key"`, `"choices"`, `"chosen_choice_key"`).Order(`"dismissed_at" DESC, "id" DESC`).Find(&candidates).Error; err != nil {
		return nil, err
	}
	restoredIds, answerClearedIds := []string{}, map[string]bool{}
	restoredKeys := map[string]bool{}
	for index := range candidates {
		candidate := &candidates[index]
		// Of two cards on one subject only the one dismissed last comes back.
		if candidate.CoalesceKey != nil {
			if restoredKeys[*candidate.CoalesceKey] {
				continue
			}
			restoredKeys[*candidate.CoalesceKey] = true
		}
		restoredIds = append(restoredIds, candidate.ID)
		if candidate.ChosenChoiceKey == nil {
			continue
		}
		var choices []models.FeedChoice
		if err := decodeJSON(candidate.Choices, &choices); err != nil {
			return nil, err
		}
		for _, choice := range choices {
			if choice.ChoiceKey == *candidate.ChosenChoiceKey && choice.ChoiceRole == models.FeedChoiceDismiss {
				answerClearedIds[candidate.ID] = true
			}
		}
	}
	// One card at a time, each in a savepoint: a post may have opened a card
	// with the same coalesce key since the candidates were read, which the
	// unique index refuses. That card is left out, as one whose key is held
	// is, and the others come back.
	restored := make([]*models.AgentFeedItem, 0, len(restoredIds))
	for _, restoredId := range restoredIds {
		items, isRestored, err := self.restoreAgentFeedItem(restoredId, answerClearedIds[restoredId], now)
		if err != nil {
			return nil, err
		}
		if isRestored {
			restored = append(restored, items...)
		}
	}
	sort.SliceStable(restored, func(first, second int) bool { return restored[first].CreatedAt.Before(restored[second].CreatedAt) })
	return restored, nil
}

// restoreAgentFeedItem brings back one card, and says whether it did: a
// coalesce key taken by an open card meanwhile (SQLSTATE 23505) leaves it out.
func (self *transaction) restoreAgentFeedItem(feedItemId string, isAnswerCleared bool, now time.Time) ([]*models.AgentFeedItem, bool, error) {
	savepointName := "restore_" + newID()
	if err := self.tx.Exec("SAVEPOINT " + savepointName).Error; err != nil {
		return nil, false, err
	}
	items, err := func() ([]*models.AgentFeedItem, error) {
		// "Not now" was an answer that did nothing, so a card put back after
		// it is waiting again.
		if isAnswerCleared {
			if err := self.tx.Model(&agentFeedItemModel{}).Where(`"id" = ?`, feedItemId).Updates(map[string]any{
				"chosen_choice_key": nil, "chosen_at": nil, "chosen_conversation_id": "", "outcome_text": "", "is_outcome_pending": false,
			}).Error; err != nil {
				return nil, err
			}
		}
		return updateFeedItems(self.tx.Where(`"id" = ?`, feedItemId), map[string]any{"dismissed_at": nil, "updated_at": now})
	}()
	if err != nil {
		var failure *pgconn.PgError
		if !errors.As(err, &failure) || failure.Code != "23505" {
			return nil, false, err
		}
		if rollbackErr := self.tx.Exec("ROLLBACK TO SAVEPOINT " + savepointName).Error; rollbackErr != nil {
			return nil, false, errors.Join(err, rollbackErr)
		}
		if releaseErr := self.tx.Exec("RELEASE SAVEPOINT " + savepointName).Error; releaseErr != nil {
			return nil, false, releaseErr
		}
		return nil, false, nil
	}
	if err := self.tx.Exec("RELEASE SAVEPOINT " + savepointName).Error; err != nil {
		return nil, false, err
	}
	return items, true, nil
}

// DismissAgentFeedItemsAnsweredBefore dismisses the cards answered before the
// given time. A card changed since then (a restored one is stamped with the
// restore time) is left alone, so a restored card stays a full day.
func (self *transaction) DismissAgentFeedItemsAnsweredBefore(before, now time.Time) ([]*models.AgentFeedItem, error) {
	query := self.tx.Where(`"chosen_at" IS NOT NULL AND "chosen_at" < ? AND "updated_at" < ? AND "dismissed_at" IS NULL AND ("expires_at" IS NULL OR "expires_at" > ?)`, before, before, now)
	return updateFeedItems(query, map[string]any{"dismissed_at": now, "read_at": gorm.Expr(`COALESCE("read_at", ?::timestamptz)`, now), "updated_at": now})
}

func (self *transaction) DeleteAgentFeedItem(feedItemId string) error {
	return self.tx.Where(`"id" = ?`, feedItemId).Delete(&agentFeedItemModel{}).Error
}

// updateFeedItems changes the cards the query selects and answers them as
// they are after the change, oldest first.
func updateFeedItems(query *gorm.DB, changes map[string]any) ([]*models.AgentFeedItem, error) {
	var changed []agentFeedItemModel
	if err := query.Model(&changed).Clauses(clause.Returning{}).Updates(changes).Error; err != nil {
		return nil, err
	}
	sort.SliceStable(changed, func(first, second int) bool { return changed[first].CreatedAt.Before(changed[second].CreatedAt) })
	items := make([]*models.AgentFeedItem, 0, len(changed))
	for index := range changed {
		item, err := changed[index].toModel()
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (self *transaction) ExpireAgentFeedItems(before time.Time) (int64, error) {
	result := self.tx.Where(`"expires_at" IS NOT NULL AND "expires_at" < ?`, before).Delete(&agentFeedItemModel{})
	return result.RowsAffected, result.Error
}

func (self *transaction) ListUnsettledAgentFeedItemsBySource(agentId string, feedSourceKind models.FeedSourceKind, feedSourceId string) ([]*models.AgentFeedItem, error) {
	var found []agentFeedItemModel
	if err := self.tx.Where(`"agent_id" = ? AND "feed_source_kind" = ? AND "feed_source_id" = ? AND "chosen_choice_key" IS NULL AND "dismissed_at" IS NULL`,
		agentId, string(feedSourceKind), feedSourceId).Order(`"created_at" ASC`).Find(&found).Error; err != nil {
		return nil, err
	}
	items := make([]*models.AgentFeedItem, 0, len(found))
	for index := range found {
		item, err := found[index].toModel()
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (self *transaction) CountAgentFeedStacks(userId string, feedStackKeys []string, now time.Time) (map[string]*models.FeedStackCounts, error) {
	counts := map[string]*models.FeedStackCounts{}
	if len(feedStackKeys) == 0 {
		return counts, nil
	}
	var rows []struct {
		FeedStackKey string
		ItemCount    int
		UnreadCount  int
		StackTitle   string
	}
	if err := self.tx.Model(&agentFeedItemModel{}).
		Select(`"feed_stack_key", count(*) AS item_count, count(*) FILTER (WHERE "read_at" IS NULL) AS unread_count, COALESCE((array_agg("feed_stack_title" ORDER BY "created_at") FILTER (WHERE "feed_stack_title" <> ''))[1], '') AS stack_title`).
		Where(`"user_id" = ? AND "feed_stack_key" IN ? AND "dismissed_at" IS NULL AND ("expires_at" IS NULL OR "expires_at" > ?)`, userId, feedStackKeys, now).
		Group(`"feed_stack_key"`).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		counts[row.FeedStackKey] = &models.FeedStackCounts{ItemCount: row.ItemCount, UnreadCount: row.UnreadCount, StackTitle: row.StackTitle}
	}
	return counts, nil
}
