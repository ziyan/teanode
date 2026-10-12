package agent

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/deferutil"
	"github.com/ziyan/teanode/internal/util/emoji"
)

// A friend who reads your message taps a reaction on it before they have
// finished answering. The agent was asked to do the same with the
// conversation tool's react action, and the model often did not: a turn
// that went straight to a tool never reacted, and once the reaction came
// from inside a background run that nobody reads.
//
// So the server does it, beside the turn and independent of the model the
// turn runs on: one small call with no tools reads the person's message
// and picks the emoji. The model's own tool stays, and replaces this
// reaction when it has a reason to.

const (
	// acknowledgeMessageCharacters bounds the person's message in the
	// prompt; the start of a long message says how it feels.
	acknowledgeMessageCharacters = 1000

	// acknowledgePreviousCharacters bounds the agent's last answer, which
	// is there only so that "thanks" can be told from "do that".
	acknowledgePreviousCharacters = 300

	// acknowledgeAnswerCharacters is the longest answer read as an
	// emoji: a model that explains itself has not answered the question,
	// and an emoji in its prose was not the choice.
	acknowledgeAnswerCharacters = 16

	// acknowledgeTimeout bounds the call. A reaction that lands after the
	// turn's answer is not a reaction to the message any more.
	acknowledgeTimeout = 20 * time.Second
)

// acknowledgePrompt asks for the emoji. Any emoji will do, as a friend
// picks from the whole keyboard; the common ones are named so the usual
// cases come out the usual way, and the default is 👀 because most of
// what a person writes to their agent is a request.
const acknowledgePrompt = "A person sent their assistant a chat message. Choose the one emoji a friend would tap on it to show they read it. " +
	"Answer with exactly one emoji, or the word none, and nothing else. Any emoji may be the right one: pick the one that fits this message best, such as " +
	"👀 for a request the assistant is about to work on (the default), " +
	"👍 for an instruction or a plain acknowledgment, " +
	"❤️ for thanks or kindness, " +
	"😂 for a joke, " +
	"🎉 for good news, " +
	"🙏 for a favor done for the assistant or an apology, " +
	"😮 for surprising news, " +
	"😢 for sad news, " +
	"or one that suits the subject better, such as ☕ for a coffee break, 🐶 for a story about a dog, or 💪 for a hard day's work done. " +
	"Answer none only when no reaction fits, which is rare."

// parseAcknowledgement is the emoji a model answered with, or empty for
// none. The first word of a short answer that is one emoji, as the
// reaction store takes it, wins; a heart a model wrote without its
// variation selector gets it back. Anything that is not a short answer
// is none: a model that explains itself has not answered the question.
func parseAcknowledgement(answer string) string {
	answer = strings.TrimSpace(answer)
	if utf8.RuneCountInString(answer) > acknowledgeAnswerCharacters {
		return ""
	}
	for _, word := range strings.Fields(answer) {
		word = strings.Trim(word, ".,:;!?\"'()[]")
		if word == "❤" {
			word = "❤️"
		}
		found, err := emoji.Split(word)
		if err != nil || len(found) != 1 || models.ValidateReactionEmoji(word) != nil {
			continue
		}
		return word
	}
	return ""
}

// shouldAcknowledge says whether a turn begins with the person's own
// message in a conversation they read, on a surface that shows
// reactions. A run's transcript has nobody reading it, a schedule, a
// goal's check-in, a late answer to a card and a woken background command
// are not something the person just said, and a terminal, a mail, a call
// or Siri has nowhere to draw the emoji.
func shouldAcknowledge(settings *AskSettings) bool {
	if settings.Headless || settings.Conversation == nil || !surfaceOf(settings.Surface).hasReactions {
		return false
	}
	switch settings.Conversation.Kind {
	case models.AgentConversationMain, models.AgentConversationNamed, models.AgentConversationGoal:
	default:
		return false
	}
	if models.IsOwnTurn(settings.Message) {
		return false
	}
	for _, marker := range []string{AnsweringMarker, ApprovedMarker, DeclinedMarker} {
		if strings.HasPrefix(settings.Message, marker) {
			return false
		}
	}
	return true
}

// acknowledge reacts to the person's message in the background, once it
// is stored; settings are the message's own, which are not the run's when
// the message was handed to a turn already running. It never delays or
// fails the turn: the call runs on its own goroutine, ends with the turn,
// and an error is logged and dropped. previousAnswer is what the agent
// last said in the conversation, or empty.
func (self *AskRun) acknowledge(settings *AskSettings, messageId, previousAnswer string) {
	if messageId == "" || !shouldAcknowledge(settings) || !self.agent.canThink(self.agent.settings.Configuration()) || self.agent.settings.Registry == nil {
		return
	}
	message := cutRunes(strings.TrimSpace(settings.Message), acknowledgeMessageCharacters)
	previousAnswer = cutRunes(strings.TrimSpace(previousAnswer), acknowledgePreviousCharacters)
	self.agent.waitGroup.Add(1)
	go func() {
		defer deferutil.Recover()
		defer self.agent.waitGroup.Done()
		// The turn's own context: when the turn has answered and ended,
		// a reaction to the message that began it adds nothing.
		ctx, cancel := context.WithTimeout(self.ctx, acknowledgeTimeout)
		defer cancel()
		if err := self.agent.acknowledge(ctx, settings, messageId, message, previousAnswer); err != nil {
			if ctx.Err() == nil {
				log.Warningf("cannot react to message %q of conversation %q: %s", messageId, settings.Conversation.ID, err)
			}
		}
	}()
}

// acknowledge picks an emoji for the person's message and puts it there,
// unless the agent has reacted to it already.
func (self *Agent) acknowledge(ctx context.Context, settings *AskSettings, messageId, message, previousAnswer string) error {
	configuration := self.settings.Configuration()
	// The same stop every optional call makes when the day's budget is
	// spent: the reaction is the first thing to give up.
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		return RequireBudget(tx, configuration, settings.Agent, settings.Owner, time.Now())
	}); err != nil {
		var deferral *Deferral
		if errors.As(err, &deferral) {
			return nil
		}
		return err
	}
	prompt := acknowledgePrompt + "\n\n"
	if previousAnswer != "" {
		prompt += "<previous_answer>\n" + previousAnswer + "\n</previous_answer>\n\n"
	}
	prompt += "<message>\n" + message + "\n</message>"
	// Cheap work, as compaction is: the compact model when one is set,
	// else the fast one, else the default (AgentModels). The turn's own
	// model would answer seconds later, after the reaction stopped
	// meaning "I read it".
	thinking, err := self.oneShot(ctx, self.runFor(settings.Agent, settings.Owner, nil, settings.Conversation.ID), "Picked a reaction", prompt, models.AgentJobReact, config.AgentWorkCompact)
	if err != nil {
		return err
	}
	reactionEmoji := parseAcknowledgement(thinking.Text)
	if reactionEmoji == "" {
		return nil
	}
	return self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		stored, err := tx.GetAgentMessage(messageId)
		if err != nil || stored == nil {
			return err
		}
		// The model may have reacted first, or the person may have
		// changed the conversation under us: its reaction stands, and a
		// message that cannot be reacted to is left alone.
		if !stored.CanBeReactedToBy(models.ReactorAgent) {
			return nil
		}
		for _, reaction := range stored.Reactions {
			if reaction.ReactorKind == models.ReactorAgent {
				return nil
			}
		}
		isChanged, err := tx.SetAgentMessageReaction(settings.Agent.ID, settings.Conversation.ID, messageId, models.ReactorAgent, reactionEmoji)
		if err != nil {
			return err
		}
		if isChanged {
			conversationId := settings.Conversation.ID
			tx.AfterCommit(func() { self.PublishReaction(conversationId, messageId, reactionEmoji, models.ReactorAgent) })
		}
		return nil
	})
}
