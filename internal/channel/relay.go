package channel

import (
	"context"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/security"
)

// The agent's own turns, sent on. A chat message's turn is followed as it
// runs and answered in the chat (turn and follow in channel.go). Everything
// else the agent says in the main conversation -- an alert, speaking first,
// a schedule answering there, a goal's check-in, a turn a background
// command woke -- was written into the conversation and stopped there, so a
// person who talks to their agent from their phone never heard it. Those
// turns open with one of models.OwnTurnMarkers, which is how they are told
// from a turn somebody asked for.
//
// They are read back from the conversation rather than followed as they
// run, because the feed drops what a listener misses and an alert must not
// be. How far the bot has looked is kept on the channel row and moved
// forward before anything is sent, only by the instance holding the bot and
// only from where it was read: a turn is sent once, and one whose sending
// failed is left in the drawer rather than sent twice.

// relay sends the agent's own turns to the linked chat until the bot
// stops: at every relayEvery, and shortly after a turn in the main
// conversation ends.
func (self *chatState) relay(ctx context.Context) {
	every := self.manager.settings.RelayEvery
	if every <= 0 {
		every = relayEvery
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	followedId := ""
	var events <-chan agent.Event
	unsubscribe := func() {}
	defer func() { unsubscribe() }()
	for {
		conversationId, err := self.relayOnce(ctx, time.Now())
		if err != nil && ctx.Err() == nil {
			log.Warningf("the %s bot of agent %s cannot send on the agent's own turns: %s", self.kind, self.agentId, err)
		}
		// The main conversation's feed says when a turn ends, which is
		// sooner than the next tick. A fresh main conversation is followed
		// in place of the old one.
		if conversationId != followedId {
			unsubscribe()
			events, unsubscribe = nil, func() {}
			if conversationId != "" && self.manager.settings.Worker != nil {
				events, unsubscribe = self.manager.settings.Worker.SubscribeConversation(conversationId)
			}
			followedId = conversationId
		}
		var settled <-chan time.Time
	waiting:
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				break waiting
			case <-settled:
				break waiting
			case event, isOpen := <-events:
				if !isOpen {
					events = nil
					continue
				}
				if event.Kind == agent.EventDone && settled == nil {
					settled = time.After(relaySettle + time.Second)
				}
			}
		}
	}
}

// relayOnce sends what the agent said on its own in the main conversation
// since the bot last looked, and says which conversation that is.
func (self *chatState) relayOnce(ctx context.Context, now time.Time) (string, error) {
	var channel *models.AgentChannel
	var conversationId string
	var answers []*models.AgentMessage
	instance := self.manager.settings.Instance
	if err := self.manager.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		found, err := tx.GetAgentChannel(self.agentId, self.kind)
		if err != nil || found == nil || found.ID != self.channelId || !found.Linked() || found.LinkedSenderID == "" {
			return err
		}
		person, err := tx.GetAgent(found.AgentID)
		if err != nil || !person.Active() {
			return err
		}
		mains, err := tx.ListAgentConversations(found.AgentID, []models.AgentConversationKind{models.AgentConversationMain}, &db.Options{Limit: 1})
		if err != nil {
			return err
		}
		// Linked just now, or before the bot looked at all: what was said
		// before is not sent, only what is said from here on.
		if found.RelayedThrough == "" {
			_, err := tx.AdvanceAgentChannelRelay(found.ID, instance, "", security.NewULIDFromTime(now))
			if len(mains) > 0 {
				conversationId = mains[0].ID
			}
			return err
		}
		if len(mains) == 0 {
			return nil
		}
		conversationId = mains[0].ID
		before := now.Add(-relaySettle)
		if answers, err = tx.ListAgentOwnTurnAnswers(conversationId, found.RelayedThrough, before, relayAtOnce); err != nil {
			return err
		}
		through := ""
		if len(answers) == relayAtOnce {
			through = answers[len(answers)-1].ID
		} else if through, err = tx.LastAgentMessageID(conversationId, before); err != nil {
			return err
		}
		if through <= found.RelayedThrough {
			answers = nil
			return nil
		}
		isAdvanced, err := tx.AdvanceAgentChannelRelay(found.ID, instance, found.RelayedThrough, through)
		if err != nil || !isAdvanced {
			// Another instance holds the bot now, or looked first.
			answers = nil
			return err
		}
		channel = found
		return nil
	}); err != nil {
		return conversationId, err
	}
	if len(answers) == 0 {
		return conversationId, nil
	}
	chat, err := self.bot.ChatFor(channel.LinkedID)
	if err != nil {
		return conversationId, err
	}
	dashboard := self.manager.settings.Configuration().DashboardBase()
	for _, answer := range answers {
		text := models.StripSuggestedReplies(answer.Content)
		if text == "" {
			continue
		}
		(&preview{chat: chat, dashboard: dashboard}).finish(ctx, text)
	}
	return conversationId, nil
}
