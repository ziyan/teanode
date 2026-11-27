package agent

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/push"
)

// The agent's feed: what it says without being asked, as cards the person
// scrolls, instead of turns in their main conversation. See
// docs/subsystems/feed.md.

// ErrFeedRefused is wrapped by every refusal of a post or a choice that the
// caller can fix: a card with no headline, a link already in an open card, a
// choice already made.
var ErrFeedRefused = errors.New("the feed refuses this")

// feedRefused is a refusal that says only what is wrong, and is
// ErrFeedRefused to errors.Is.
type feedRefused struct{ message string }

func (self *feedRefused) Error() string        { return self.message }
func (self *feedRefused) Is(target error) bool { return target == ErrFeedRefused }

func feedRefusal(format string, arguments ...any) error {
	return &feedRefused{message: fmt.Sprintf(format, arguments...)}
}

// The lengths a card is kept to, so that one post cannot fill a screen.
const (
	feedTitleCharacters = 200
	feedBodyCharacters  = 4000
	feedLabelCharacters = 80
	feedFactCharacters  = 200
	feedLinksMost       = 12
)

// FeedPost is a card to post and where its question is raised.
type FeedPost struct {
	// AgentID is the agent posting; its person's feed is the one posted to.
	AgentID string

	Item *models.AgentFeedItem

	// InteractionConversationID is the conversation of the turn that posted,
	// where a decision's durable question is raised. Empty raises none: a card
	// an alert or a coding session posts has no turn to hang it on.
	InteractionConversationID string
}

// NormalizeFeedItem trims and bounds a card as posted, fills in what a post
// may leave out, and refuses what cannot be shown.
func NormalizeFeedItem(item *models.AgentFeedItem) error {
	item.PostNotes = nil
	item.Title = feedLine(item, "title", item.Title, feedTitleCharacters)
	item.Body = feedText(item, "body", item.Body, feedBodyCharacters)
	item.PictureSymbol = strings.TrimSpace(item.PictureSymbol)
	item.CoalesceKey = strings.TrimSpace(item.CoalesceKey)
	if item.Title == "" {
		return feedRefusal("a card needs a title, the headline")
	}
	if len(item.FactRows) > models.FeedFactRowsMost {
		return feedRefusal("a card has at most %d fact rows, and this one has %d", models.FeedFactRowsMost, len(item.FactRows))
	}
	rows := make([]models.FeedFactRow, 0, len(item.FactRows))
	for index, row := range item.FactRows {
		row.FactLabel = feedLine(item, fmt.Sprintf("factRows[%d].factLabel", index), row.FactLabel, feedFactCharacters)
		row.FactValue = feedLine(item, fmt.Sprintf("factRows[%d].factValue", index), row.FactValue, feedFactCharacters)
		if row.FactLabel == "" && row.FactValue == "" {
			continue
		}
		rows = append(rows, row)
	}
	item.FactRows = rows
	if err := refuseFeedFactSeries(item.FactRows); err != nil {
		return err
	}
	if len(item.SourceLinks) > feedLinksMost {
		return feedRefusal("a card has at most %d source links", feedLinksMost)
	}
	links := make([]models.FeedSourceLink, 0, len(item.SourceLinks))
	for index, link := range item.SourceLinks {
		link.SourceURL, link.SourceTitle = strings.TrimSpace(link.SourceURL), feedLine(item, fmt.Sprintf("sourceLinks[%d].sourceTitle", index), link.SourceTitle, feedFactCharacters)
		parsed, err := url.Parse(link.SourceURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return feedRefusal("%q is not a link to a page; a source link is an http or https address", link.SourceURL)
		}
		if link.SourceTitle == "" {
			link.SourceTitle = parsed.Host
		}
		links = append(links, link)
	}
	item.SourceLinks = links
	if len(item.Choices) > models.FeedChoicesMost {
		return feedRefusal("a card has at most %d choices", models.FeedChoicesMost)
	}
	seenKeys := map[string]bool{}
	for index := range item.Choices {
		choice := &item.Choices[index]
		choice.ChoiceKey, choice.ChoiceLabel = strings.TrimSpace(choice.ChoiceKey), feedLine(item, fmt.Sprintf("choices[%d].choiceLabel", index), choice.ChoiceLabel, feedLabelCharacters)
		if choice.ChoiceRole == "" {
			choice.ChoiceRole = models.FeedChoiceAction
		}
		if choice.ChoiceKey == "" {
			choice.ChoiceKey = fmt.Sprintf("choice-%d", index+1)
		}
		if choice.ChoiceLabel == "" || !models.IsFeedChoiceRole(choice.ChoiceRole) || seenKeys[choice.ChoiceKey] {
			return feedRefusal("every choice needs a distinct choice_key, a choice_label and a choice_role of action, dismiss, discuss or open")
		}
		seenKeys[choice.ChoiceKey] = true
	}
	if item.FeedItemKind == "" {
		item.FeedItemKind = models.FeedItemUpdate
		if len(item.Choices) > 0 {
			item.FeedItemKind = models.FeedItemDecision
		}
	}
	if !models.IsFeedItemKind(item.FeedItemKind) {
		return feedRefusal("%q is not a kind of card; the kinds are update, decision, alert, report, idea and news", item.FeedItemKind)
	}
	if item.FeedSourceKind == "" {
		item.FeedSourceKind = models.FeedSourceAgent
	}
	if !models.IsFeedSourceKind(item.FeedSourceKind) {
		return feedRefusal("%q is not a source of a card", item.FeedSourceKind)
	}
	if item.FeedUrgency == "" {
		item.FeedUrgency = models.FeedUrgencyToday
	}
	if !models.IsFeedUrgency(item.FeedUrgency) {
		return feedRefusal("%q is not an urgency; use now, today or whenever", item.FeedUrgency)
	}
	if err := normalizeFeedLayout(item); err != nil {
		return err
	}
	if err := normalizeFeedMedia(item); err != nil {
		return err
	}
	if err := normalizeFeedCharts(item); err != nil {
		return err
	}
	item.FeedStackTitle = feedLine(item, "stackTitle", item.FeedStackTitle, feedTitleCharacters)
	item.FeedStackKey = strings.TrimSpace(item.FeedStackKey)
	switch {
	case !item.CanStack():
		item.FeedStackKey = ""
	case item.FeedStackKey == "":
		item.FeedStackKey = item.DefaultFeedStackKey()
	}
	item.FeedStackKey = cutMarked(item.FeedStackKey, feedLabelCharacters*2)
	if item.FactRows == nil {
		item.FactRows = []models.FeedFactRow{}
	}
	if item.SourceLinks == nil {
		item.SourceLinks = []models.FeedSourceLink{}
	}
	if item.RecordReferences == nil {
		item.RecordReferences = []models.AgentReference{}
	}
	if item.Choices == nil {
		item.Choices = []models.FeedChoice{}
	}
	return nil
}

// PostFeedItem stores a card in the agent's person's feed, replacing the open
// card that has the same coalesce key. A decision with an action raises its
// durable question in the posting turn's conversation, which nothing waits
// on, and an urgent card notifies the person's phones.
func (self *Agent) PostFeedItem(ctx context.Context, post *FeedPost) (*models.AgentFeedItem, models.FeedChange, error) {
	item := post.Item
	// The pages a card may show the pictures of are those a tool answered
	// with in the turn that posts, before the card is credited elsewhere.
	postingConversationId := item.RunConversationID
	// Credited before normalizing, which picks the default stack from the source.
	if (item.FeedSourceKind == "" || item.FeedSourceKind == models.FeedSourceAgent) && item.RunConversationID != "" {
		if err := self.creditFeedSource(ctx, item); err != nil {
			return nil, "", err
		}
	}
	if err := NormalizeFeedItem(item); err != nil {
		return nil, "", err
	}
	var stored *models.AgentFeedItem
	var change models.FeedChange
	var found *models.Agent
	var readyGoal *models.AgentConversation
	var removedPictures, keptStills []string
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		var err error
		if found, err = tx.GetAgent(post.AgentID); err != nil {
			return err
		}
		if found == nil {
			return db.ErrNotFound
		}
		item.AgentID, item.UserID = found.ID, found.UserID
		now := time.Now()
		// A coding session's question goes in the stack of the day's
		// questions, the day and its title in the person's own zone and
		// language.
		if item.FeedSourceKind == models.FeedSourceCodingSession && item.FeedStackKey == "" {
			owner, err := tx.GetUser(found.UserID)
			if err != nil {
				return err
			}
			day := now.In(Location(owner))
			item.FeedStackKey = models.FeedStackKeyCodingDay(day)
			item.FeedStackTitle = codingStackTitle(day, push.PhoneLanguage(Language(found, owner)))
		}
		previous := (*models.AgentFeedItem)(nil)
		if item.CoalesceKey != "" {
			if previous, err = tx.GetOpenAgentFeedItemByCoalesceKey(found.ID, item.CoalesceKey); err != nil {
				return err
			}
		}
		// A page already in an open card of the last month is not posted
		// again, except by the card that has it, which is being replaced.
		since := now.AddDate(0, 0, -models.FeedSourceURLDays)
		for _, link := range item.SourceLinks {
			earlier, err := tx.FindOpenAgentFeedItemBySourceURL(found.ID, link.SourceURL, since)
			if err != nil {
				return err
			}
			if earlier != nil && (previous == nil || earlier.ID != previous.ID) {
				return feedRefusal("%s is already in an open card, %q; do not post the same page twice, and update that card if it has changed", link.SourceURL, earlier.Title)
			}
		}
		// The cards of one turn of a goal on set times are a stack of their
		// own, so each morning's brief is one; and the first card of a turn
		// of one that asks for it says the cards are ready, once.
		if item.FeedSourceKind == models.FeedSourceGoal && item.FeedSourceID != "" {
			if readyGoal, err = goalTurnStack(tx, found, item, now); err != nil {
				return err
			}
		}
		item.InteractionID = ""
		if post.InteractionConversationID != "" && item.HasChoiceRole(models.FeedChoiceAction) {
			labels := make([]string, 0, len(item.Choices))
			for _, choice := range item.Choices {
				labels = append(labels, choice.ChoiceLabel)
			}
			// The call is named by the card's own new id: a run raises one
			// question per call, and this card is not a call of any run.
			interaction, err := tx.CreateAgentInteraction(&models.AgentInteraction{
				AgentID: found.ID, ConversationID: post.InteractionConversationID, RunID: "feed", CallID: newFeedCallID(),
				InteractionKind: models.InteractionQuestion, ToolName: "feed", InteractionText: item.Title, InteractionChoices: labels,
			})
			if err != nil {
				return err
			}
			item.InteractionID = interaction.ID
		}
		if previous != nil {
			carryFeedStills(previous.FeedMedia, item)
		}
		if err := resolveFeedPhotos(tx, found.UserID, item); err != nil {
			return err
		}
		if keptStills, err = self.keepFeedStills(ctx, tx, found.ID, item); err != nil {
			return err
		}
		if previous != nil {
			removedPictures = carryFeedPictures(previous.FeedMedia, item)
		}
		if stored, change, err = tx.UpsertAgentFeedItem(item, now); err != nil {
			return err
		}
		// What a replaced card was waiting on is not waited on any more.
		if previous != nil && previous.InteractionID != "" {
			if _, err := tx.ClaimAgentInteraction(previous.InteractionID, models.InteractionStopped); err != nil {
				return err
			}
		}
		return self.announceFeedChange(tx, stored, change)
	}); err != nil {
		self.deleteFeedPictures(keptStills)
		return nil, "", err
	}
	stored.PostNotes = item.PostNotes
	self.deleteFeedPictures(removedPictures)
	self.startFeedPictures(stored, postingConversationId)
	self.notifyFeedPhones(found, stored)
	if readyGoal != nil {
		self.notifyGoalReady(found, readyGoal)
	}
	return stored, change, nil
}

// newFeedCallID is a call id for a card's question.
func newFeedCallID() string {
	return "feed-" + fmt.Sprint(time.Now().UnixNano())
}

// announceFeedChange tells the screens that have the feed open, once the
// change is stored, with the person's unread count as it is then.
func (self *Agent) announceFeedChange(tx db.Transaction, item *models.AgentFeedItem, change models.FeedChange) error {
	unreadCount, err := tx.CountUnreadAgentFeedItems(item.UserID, time.Now())
	if err != nil {
		return err
	}
	event := &models.FeedEvent{UserID: item.UserID, FeedItemID: item.ID, FeedChange: change, UnreadCount: unreadCount}
	tx.AfterCommit(func() {
		if err := self.settings.Database.NotifyFeedEvent(event); err != nil {
			log.Warningf("cannot tell the open feeds of a change: %s", err)
		}
	})
	return nil
}

// notifyFeedPhones sends the phones a card that cannot wait: only urgency
// now. A decision carries its choices as the notification's buttons, so that
// a lock screen can answer it; the rest of the cards raise the unread count
// and nothing more.
func (self *Agent) notifyFeedPhones(found *models.Agent, item *models.AgentFeedItem) {
	// A coding session's question already notifies as one (tellHerdrQuestion),
	// and it is not said twice.
	if item.FeedUrgency != models.FeedUrgencyNow || found == nil || item.FeedSourceKind == models.FeedSourceCodingSession {
		return
	}
	content := &push.Content{
		NotificationKind: "agent_feed",
		FeedItemID:       item.ID,
		Title:            item.Title,
		Body:             PhoneBody(item.Body),
		ThreadID:         "feed:" + item.ID,
	}
	if content.Body == "" {
		content.Body = PhoneBody(item.Title)
	}
	if item.FeedItemKind == models.FeedItemDecision && !item.IsChosen() {
		for _, choice := range item.Choices {
			if choice.ChoiceRole == models.FeedChoiceDiscuss {
				continue
			}
			content.Buttons = append(content.Buttons, push.Button{Label: models.PlainText(choice.ChoiceLabel), ButtonAction: "feed:" + choice.ChoiceKey})
		}
		if len(content.Buttons) > 4 {
			content.Buttons = content.Buttons[:4]
		}
	}
	self.notifyPhones(found.ID, models.NotificationMessage, content)
}

// wantsFeed says whether what the agent says unasked goes to its feed: it
// does unless the person sent it back to the main conversation.
func wantsFeed(found *models.Agent) bool {
	return found != nil && found.IsFeedFor()
}

// UpdateFeedItem changes what a card says in place, as the posting agent's
// own change: it keeps the choice made on it, and tells the open feeds.
func (self *Agent) UpdateFeedItem(ctx context.Context, agentId, feedItemId, postingConversationId string, modify func(*models.AgentFeedItem) error) (*models.AgentFeedItem, error) {
	var updated *models.AgentFeedItem
	var removedPictures, keptStills []string
	var postNotes []string
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		existing, err := tx.GetAgentFeedItem(feedItemId)
		if err != nil {
			return err
		}
		if existing == nil || existing.AgentID != agentId || existing.DismissedAt != nil {
			return db.ErrNotFound
		}
		if updated, err = tx.UpdateAgentFeedItem(feedItemId, func(item *models.AgentFeedItem) error {
			if err := modify(item); err != nil {
				return err
			}
			if err := NormalizeFeedItem(item); err != nil {
				return err
			}
			carryFeedStills(existing.FeedMedia, item)
			if err := resolveFeedPhotos(tx, existing.UserID, item); err != nil {
				return err
			}
			if keptStills, err = self.keepFeedStills(ctx, tx, existing.AgentID, item); err != nil {
				return err
			}
			removedPictures = carryFeedPictures(existing.FeedMedia, item)
			postNotes = item.PostNotes
			return nil
		}, time.Now()); err != nil {
			return err
		}
		return self.announceFeedChange(tx, updated, models.FeedChangeReplaced)
	}); err != nil {
		self.deleteFeedPictures(keptStills)
		return nil, err
	}
	updated.PostNotes = postNotes
	self.deleteFeedPictures(removedPictures)
	self.startFeedPictures(updated, postingConversationId)
	return updated, nil
}

// phraseGoalReady is the quiet notification of a goal on set times whose
// cards are posted.
var phraseGoalReady = push.Phrase{English: "%s is ready", Chinese: "%s已准备好", Japanese: "%sの準備ができました"}

// notifyGoalReady tells the phones, quietly, that the first card of a turn
// of a goal on set times is in the feed.
func (self *Agent) notifyGoalReady(found *models.Agent, goal *models.AgentConversation) {
	name := goalTitleOf(goal)
	self.notifyPhones(found.ID, models.NotificationMessage, &push.Content{
		NotificationKind: "agent_feed",
		Title:            phraseGoalReady.In(push.LanguageEnglish, name),
		ThreadID:         "feed:goal:" + goal.ID,
		IsQuiet:          true,
		Wording:          func(content *push.Content, language string) { content.Title = phraseGoalReady.In(language, name) },
	})
}

// goalTurnStack puts a card of a goal on set times in the stack of the turn
// posting it, the goal's running job naming the turn, unless the card named
// a stack of its own; and answers with the goal when the card is the turn's
// first and the goal asks to be told.
func goalTurnStack(tx db.Transaction, found *models.Agent, item *models.AgentFeedItem, now time.Time) (*models.AgentConversation, error) {
	goal, err := tx.GetAgentConversation(item.FeedSourceID)
	if err != nil || goal == nil || goal.AgentID != found.ID || !goal.IsGoalOnSetTimes() {
		return nil, err
	}
	running, err := tx.ListAgentJobs(&db.AgentJobFilter{
		AgentID: found.ID, Kinds: []models.AgentJobKind{models.AgentJobGoal},
		Statuses: []models.AgentJobStatus{models.AgentJobRunning}, SubjectID: goal.ID,
	}, &db.Options{Limit: 1})
	if err != nil || len(running) == 0 {
		return nil, err
	}
	turnStackKey := string(models.FeedSourceGoal) + ":" + goal.ID + ":" + running[0].ID
	if item.CanStack() && (item.FeedStackKey == "" || item.FeedStackKey == item.DefaultFeedStackKey()) {
		item.FeedStackKey = turnStackKey
		if item.FeedStackTitle == "" {
			item.FeedStackTitle = goalTitleOf(goal)
		}
	}
	if !goal.IsGoalNotifiedWhenPosted {
		return nil, nil
	}
	_, postedCount, err := tx.ListAgentFeedStackItems(found.UserID, turnStackKey, 0, 1, true, now)
	if err != nil || postedCount > 0 {
		return nil, err
	}
	return goal, nil
}

// feedSourceHopsMost bounds the walk from a subagent's run up to the turn that
// started it: a subagent cannot start another, so one hop is the most there is,
// and the bound only guards against a loop.
const feedSourceHopsMost = 3

// creditFeedSource credits a card posted from a subagent's run to what started
// that subagent: a goal or a run of speaking first. The tool
// credits a card posted in such a run directly; one posted by a subagent the
// run handed its work to would otherwise be the agent's own, with no link to
// the schedule, no stack of its run and no "is ready" notification.
func (self *Agent) creditFeedSource(ctx context.Context, item *models.AgentFeedItem) error {
	return self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		conversation, err := tx.GetAgentConversation(item.RunConversationID)
		if err != nil || conversation == nil {
			return err
		}
		for hop := 0; hop < feedSourceHopsMost && conversation.Kind == models.AgentConversationRun &&
			conversation.JobKind == string(models.AgentJobSubagent) && conversation.SubjectID != ""; hop++ {
			parent, err := tx.GetAgentConversation(conversation.SubjectID)
			if err != nil {
				return err
			}
			if parent == nil || parent.AgentID != conversation.AgentID {
				return nil
			}
			conversation = parent
		}
		switch {
		case conversation.IsGoal():
			item.FeedSourceKind, item.FeedSourceID = models.FeedSourceGoal, conversation.ID
		case conversation.Kind == models.AgentConversationRun && conversation.JobKind == string(models.AgentJobSpeakFirst) && conversation.SubjectID != "":
			item.FeedSourceKind, item.FeedSourceID = models.FeedSourceSpeakFirst, conversation.SubjectID
		}
		return nil
	})
}

// The title of a day's stack of coding sessions' questions, with the day, in
// the languages the apps are in.
var (
	phraseCodingStack = push.Phrase{English: "Coding sessions, %s", Chinese: "编程会话，%s", Japanese: "コーディングセッション、%s"}
	chineseWeekdays   = [...]string{"日", "一", "二", "三", "四", "五", "六"}
	japaneseWeekdays  = [...]string{"日", "月", "火", "水", "木", "金", "土"}
)

func codingStackTitle(day time.Time, language string) string {
	switch language {
	case push.LanguageChinese:
		return phraseCodingStack.In(language, fmt.Sprintf("%d月%d日 周%s", day.Month(), day.Day(), chineseWeekdays[day.Weekday()]))
	case push.LanguageJapanese:
		return phraseCodingStack.In(language, fmt.Sprintf("%d月%d日(%s)", day.Month(), day.Day(), japaneseWeekdays[day.Weekday()]))
	}
	return phraseCodingStack.In(language, day.Format("Mon Jan 2"))
}
