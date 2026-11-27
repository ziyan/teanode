package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// feedOutcomeLines is how long a card's outcome line may be.
const feedOutcomeCharacters = 300

// feedOutcomePendingFor is how long a card is shown as waiting on the turn it
// started. A restart in the middle of that turn leaves the flag set, and a
// card must not wait forever on a turn that is gone.
const feedOutcomePendingFor = 30 * time.Minute

// IsFeedOutcomePending says whether a card is still waiting for the line its
// turn will write.
func IsFeedOutcomePending(item *models.AgentFeedItem, now time.Time) bool {
	return item.IsOutcomePending && now.Sub(item.UpdatedAt) < feedOutcomePendingFor
}

// FeedChoiceSettings is a person's choice on a card, and what carrying it out needs.
type FeedChoiceSettings struct {
	Agent      *models.Agent
	Owner      *models.User
	Operations Operations

	// IsPersonsTap and IsOperatorActingAsPerson are the request's, as for
	// AskSettings: the message a chosen action sends is the person's own only
	// when they tapped.
	IsPersonsTap             bool
	IsOperatorActingAsPerson bool

	Item      *models.AgentFeedItem
	ChoiceKey string
}

// ChooseFeedItem carries out what a button of a card says, once. A dismiss
// settles the card and answers its durable question with no turn. An action
// says the choice in the person's main conversation, as their own message
// with the card attached, so that an ordinary turn carries it out; the card
// then shows that turn's first line as its outcome. A coding session's
// option answers the session directly with no message. A discuss or open
// button changes nothing here: the screen opens the conversation or the
// session.
func (self *Agent) ChooseFeedItem(ctx context.Context, settings *FeedChoiceSettings) (*models.AgentFeedItem, error) {
	item := settings.Item
	if item.AgentID != settings.Agent.ID {
		return nil, db.ErrNotFound
	}
	choice := item.FindChoice(settings.ChoiceKey)
	if choice == nil {
		return nil, feedRefusal("this card has no choice %q", settings.ChoiceKey)
	}
	if item.IsChosen() {
		return nil, feedRefusal("this card is already answered")
	}
	// A dismissed card can still be answered from the dismissed cards, and
	// stays dismissed: putting it back is the person's restore.
	switch choice.ChoiceRole {
	case models.FeedChoiceDiscuss, models.FeedChoiceOpen:
		return item, nil
	case models.FeedChoiceDismiss:
		return self.settleFeedItem(ctx, item, choice, &models.FeedSettlement{ChoiceKey: choice.ChoiceKey})
	}
	if item.FeedSourceKind == models.FeedSourceCodingSession {
		return self.chooseCodingOption(ctx, settings, choice)
	}
	return self.chooseAction(ctx, settings, choice)
}

// settleFeedItem records the choice, answers the card's durable question with
// the choice's words, and tells the open feeds. It refuses a card another
// request settled first.
func (self *Agent) settleFeedItem(ctx context.Context, item *models.AgentFeedItem, choice *models.FeedChoice, settlement *models.FeedSettlement) (*models.AgentFeedItem, error) {
	var settled *models.AgentFeedItem
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		isSettled, err := tx.SettleAgentFeedItem(item.ID, settlement, time.Now())
		if err != nil {
			return err
		}
		if !isSettled {
			return feedRefusal("this card is already answered")
		}
		if item.InteractionID != "" {
			if _, err := tx.ClaimAgentInteraction(item.InteractionID, choice.ChoiceLabel); err != nil {
				return err
			}
		}
		if settled, err = tx.GetAgentFeedItem(item.ID); err != nil {
			return err
		}
		return self.announceFeedChange(tx, settled, models.FeedChangeSettled)
	}); err != nil {
		return nil, err
	}
	return settled, nil
}

// chooseAction says an action in the person's main conversation.
func (self *Agent) chooseAction(ctx context.Context, settings *FeedChoiceSettings, choice *models.FeedChoice) (*models.AgentFeedItem, error) {
	item := settings.Item
	var conversation *models.AgentConversation
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		conversation, err = tx.MainAgentConversation(settings.Agent.ID, "drawer")
		return err
	}); err != nil {
		return nil, err
	}
	// Settled first, so that two taps start one turn.
	settled, err := self.settleFeedItem(ctx, item, choice, &models.FeedSettlement{ChoiceKey: choice.ChoiceKey, ConversationID: conversation.ID, IsOutcomePending: true})
	if err != nil {
		return nil, err
	}
	run, err := self.Ask(&AskSettings{
		Agent: settings.Agent, Owner: settings.Owner, Operations: settings.Operations, Conversation: conversation,
		Message: feedChoiceMessage(choice, item), Surface: "drawer",
		References:   []models.AgentReference{{FeedItemID: item.ID}},
		IsPersonsTap: settings.IsPersonsTap, IsOperatorActingAsPerson: settings.IsOperatorActingAsPerson,
	})
	if err != nil {
		if outcomeErr := self.writeFeedOutcome(item.ID, "That could not be carried out: "+err.Error()); outcomeErr != nil {
			log.Warningf("cannot write the outcome of card %s: %s", item.ID, outcomeErr)
		}
		return nil, err
	}
	self.waitGroup.Add(1)
	go func() {
		defer self.waitGroup.Done()
		self.followFeedOutcome(run, item.ID)
	}()
	return settled, nil
}

// feedChoiceMessage is a choice as the person's own words: what they pressed
// and what it was about, "Approve: Sort newsletters into a Newsletters folder".
func feedChoiceMessage(choice *models.FeedChoice, item *models.AgentFeedItem) string {
	return choice.ChoiceLabel + ": " + item.Title
}

// followFeedOutcome waits for the turn a chosen action started, and writes the
// first line of its answer on the card.
func (self *Agent) followFeedOutcome(run *AskRun, feedItemId string) {
	events, unsubscribe := run.Subscribe()
	defer unsubscribe()
	answer, failure := "", ""
	for event := range events {
		switch event.Kind {
		case EventMessage:
			answer = event.Text
		case EventError:
			failure = event.Error
		}
	}
	outcome := firstFeedLine(answer)
	if outcome == "" {
		outcome = "Done."
		if failure != "" {
			outcome = "That did not finish: " + failure
		}
	}
	if err := self.writeFeedOutcome(feedItemId, outcome); err != nil {
		log.Warningf("cannot write the outcome of card %s: %s", feedItemId, err)
	}
}

// firstFeedLine is the first line of an answer as plain words, for the line a
// card ends with.
func firstFeedLine(answer string) string {
	for _, line := range strings.Split(models.StripHiddenMarkers(models.StripSuggestedReplies(answer)), "\n") {
		if line = strings.TrimSpace(models.PlainText(line)); line != "" {
			return cutMarked(line, feedOutcomeCharacters)
		}
	}
	return ""
}

// writeFeedOutcome stores a card's outcome line and tells the open feeds.
func (self *Agent) writeFeedOutcome(feedItemId, outcomeText string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(self.ctx), 30*time.Second)
	defer cancel()
	return self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		if err := tx.SetAgentFeedItemOutcome(feedItemId, outcomeText, time.Now()); err != nil {
			return err
		}
		item, err := tx.GetAgentFeedItem(feedItemId)
		if err != nil || item == nil {
			return err
		}
		return self.announceFeedChange(tx, item, models.FeedChangeSettled)
	})
}

// chooseCodingOption answers a coding session's question with the option the
// button names, with no turn and no message. The session is asked before the
// card is settled: the computer refuses an answer to a question that has gone
// on, and then the card must stay open.
func (self *Agent) chooseCodingOption(ctx context.Context, settings *FeedChoiceSettings, choice *models.FeedChoice) (*models.AgentFeedItem, error) {
	item := settings.Item
	optionNumber, isOption := models.FeedOptionNumber(choice.ChoiceKey)
	if !isOption {
		return nil, feedRefusal("%q is not an option of a coding session's question", choice.ChoiceKey)
	}
	computerName, paneId, fingerprint, isCodingCard := codingCardParts(item)
	if !isCodingCard {
		return nil, feedRefusal("this card does not say which session it is about")
	}
	answered, err := self.AnswerHerdrQuestion(ctx, settings.Agent.ID, computerName, paneId, fingerprint, []int{optionNumber}, nil, "")
	if err != nil {
		return nil, err
	}
	if !answered.IsAnswerAccepted {
		return nil, feedRefusal("the session no longer waits on this question")
	}
	outcome := strings.TrimSpace(answered.AnsweredWith)
	if outcome == "" {
		outcome = choice.ChoiceLabel
	}
	return self.settleFeedItem(ctx, item, choice, &models.FeedSettlement{ChoiceKey: choice.ChoiceKey, OutcomeText: cutMarked(outcome, feedOutcomeCharacters)})
}

// codingCardParts reads a coding session's card: the computer and the pane
// from its source id, and the question's fingerprint from the end of its
// coalesce key, where it was put (see herdrFeedItem).
func codingCardParts(item *models.AgentFeedItem) (computerName, paneId, fingerprint string, isCodingCard bool) {
	separator := strings.LastIndex(item.FeedSourceID, "/")
	keySeparator := strings.LastIndex(item.CoalesceKey, "/")
	if separator <= 0 || separator == len(item.FeedSourceID)-1 || keySeparator < 0 || keySeparator == len(item.CoalesceKey)-1 {
		return "", "", "", false
	}
	return item.FeedSourceID[:separator], item.FeedSourceID[separator+1:], item.CoalesceKey[keySeparator+1:], true
}

// SettleFeedItemsOfSession settles the open question cards of a coding
// session as answered on the computer, once the session says its question
// went. A card the person answered from the feed is settled already.
func (self *Agent) SettleFeedItemsOfSession(ctx context.Context, agentId, computerName, paneId, answerText string) error {
	outcome := "Answered on the computer"
	if answerText = strings.TrimSpace(answerText); answerText != "" {
		outcome += ": " + cutMarked(answerText, feedOutcomeCharacters)
	}
	return self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		items, err := tx.ListUnsettledAgentFeedItemsBySource(agentId, models.FeedSourceCodingSession, computerName+"/"+paneId)
		if err != nil {
			return err
		}
		for _, item := range items {
			isSettled, err := tx.SettleAgentFeedItem(item.ID, &models.FeedSettlement{ChoiceKey: "answered", OutcomeText: outcome}, time.Now())
			if err != nil {
				return err
			}
			if !isSettled {
				continue
			}
			if item.InteractionID != "" {
				if _, err := tx.ClaimAgentInteraction(item.InteractionID, outcome); err != nil {
					return err
				}
			}
			settled, err := tx.GetAgentFeedItem(item.ID)
			if err != nil {
				return err
			}
			if err := self.announceFeedChange(tx, settled, models.FeedChangeSettled); err != nil {
				return err
			}
		}
		return nil
	})
}

// LikeFeedItem records a like or an unlike, and writes it to the agent's
// memory as a preference about the card's subject, which the goals that post
// read before choosing what to post.
func (self *Agent) LikeFeedItem(ctx context.Context, agentId string, item *models.AgentFeedItem, isLiked bool) (*models.AgentFeedItem, error) {
	text := fmt.Sprintf("The person liked a card in their feed, %q: show more like it.", item.Title)
	if !isLiked {
		text = fmt.Sprintf("The person took back their like of a card in their feed, %q: it is not something they asked for more of.", item.Title)
	}
	var liked *models.AgentFeedItem
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		if err := tx.SetAgentFeedItemLiked(item.ID, isLiked, time.Now()); err != nil {
			return err
		}
		if err := self.rememberFeedPreference(tx, agentId, item, text); err != nil {
			return err
		}
		var err error
		liked, err = tx.GetAgentFeedItem(item.ID)
		return err
	}); err != nil {
		return nil, err
	}
	return liked, nil
}

// ShowLessLikeFeedItem records that the person wants fewer cards like this
// one, and dismisses it. A card already dismissed is only remembered.
func (self *Agent) ShowLessLikeFeedItem(ctx context.Context, agentId string, item *models.AgentFeedItem) (*models.AgentFeedItem, error) {
	text := fmt.Sprintf("The person asked to see less like a card in their feed, %q: post fewer cards about this.", item.Title)
	var dismissed *models.AgentFeedItem
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		if err := self.rememberFeedPreference(tx, agentId, item, text); err != nil {
			return err
		}
		if item.DismissedAt != nil {
			var err error
			dismissed, err = tx.GetAgentFeedItem(item.ID)
			return err
		}
		if _, err := tx.DismissAgentFeedItems(item.UserID, &db.FeedDismissSelection{FeedItemIDs: []string{item.ID}}, time.Now()); err != nil {
			return err
		}
		var err error
		if dismissed, err = tx.GetAgentFeedItem(item.ID); err != nil {
			return err
		}
		return self.announceFeedChange(tx, dismissed, models.FeedChangeDismissed)
	}); err != nil {
		return nil, err
	}
	return dismissed, nil
}

// feedPreferencePath is the page of the agent's memory that keeps what the
// person likes and does not like in their feed.
var feedPreferencePath = models.JoinPath(models.PathSelf, "feed")

// rememberFeedPreference files a preference as a fact on its page, the way the
// memory tool files one.
func (self *Agent) rememberFeedPreference(tx db.Transaction, agentId string, item *models.AgentFeedItem, text string) error {
	if err := tx.EnsureAgentRoots(agentId); err != nil {
		return err
	}
	node, err := tx.GetAgentNode(agentId, feedPreferencePath)
	if err != nil {
		return err
	}
	if node == nil {
		if node, err = tx.PutAgentNode(&models.AgentNode{AgentID: agentId, Path: feedPreferencePath, Kind: models.NodeTopic, Name: "Feed preferences"}); err != nil {
			return err
		}
	}
	_, err = tx.AddAgentFact(&models.AgentFact{
		AgentID: agentId, NodeID: node.ID, Kind: models.FactPreference, Text: text, Confidence: 1,
		Evidence:  []models.Evidence{{Kind: models.EvidencePerson, Quote: cutMarked(item.Title, 100)}},
		Audiences: []models.AgentAudience{models.AudienceAsk, models.AudienceResearch},
	})
	return err
}

// SweepAnsweredFeedItems moves the cards answered with a choice more than
// models.FeedAnsweredLifetime ago to the dismissed cards, where they can be
// found and put back, and tells the open feeds. An answered card has said
// what it had to; left in place it crowds what still needs the person.
func (self *Agent) SweepAnsweredFeedItems(ctx context.Context) error {
	return self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		now := time.Now()
		dismissed, err := tx.DismissAgentFeedItemsAnsweredBefore(now.Add(-models.FeedAnsweredLifetime), now)
		if err != nil {
			return err
		}
		for _, item := range dismissed {
			if err := self.announceFeedChange(tx, item, models.FeedChangeDismissed); err != nil {
				return err
			}
		}
		return nil
	})
}
