package agent

import (
	"context"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// A conversation and a call from outside both read the pages other people
// shared with the person through tools.SharedMemory. Held to it here so that
// a method added to the interface breaks this file.
var (
	_ tools.SharedMemory = (*AskRun)(nil)
	_ tools.SharedMemory = (*directRun)(nil)
)

// canReadSharedPages is the rule both kinds of run share: the person may use
// circles, and the reader is an account.
func canReadSharedPages(operations tools.Operations, reader *models.User) bool {
	return reader != nil && operations != nil && operations.Permissions().Has(models.PermissionCircleUse)
}

// CanReadSharedPages is true for a conversation the person is in. Not in a
// dream, which never reads shared pages; not in a run nobody is at, whose
// words go to a mail or a schedule and not back to the person; and not in a
// chat of several people, whose members were not each given the same pages.
func (self *AskRun) CanReadSharedPages() bool {
	if self.GuardRecordingEvidence() || self.settings.Headless || self.settings.isUnattended {
		return false
	}
	if conversation := self.settings.Conversation; conversation != nil && conversation.IsChat() {
		return false
	}
	return canReadSharedPages(self.settings.Operations, self.settings.Owner)
}

func (self *AskRun) SharedPages(ctx context.Context) ([]*models.SharedRoot, error) {
	return sharedPagesOf(ctx, self.agent.settings.Database, self.settings.Owner.ID)
}

func (self *AskRun) ReadSharedPage(ctx context.Context, ownerUserId, path string) (*models.SharedPage, error) {
	return readSharedPageOf(ctx, self.agent.settings.Database, self.settings.Owner.ID, ownerUserId, path)
}

func (self *AskRun) SearchSharedPages(ctx context.Context, query string, limit int) ([]*models.SharedHit, error) {
	return searchSharedPagesOf(ctx, self.agent.settings.Database, self.settings.Owner.ID, query, limit)
}

// RestsOnSharedPages reads the conversation as stored, every round so far,
// for the words shared pages showed it and the words the person wrote.
func (self *AskRun) RestsOnSharedPages(ctx context.Context, text string) bool {
	conversation := self.settings.Conversation
	if conversation == nil {
		return false
	}
	var messages []*models.AgentMessage
	if err := self.agent.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		messages, err = tx.ListAgentMessages(conversation.ID, nil)
		return err
	}); err != nil {
		log.Warningf("cannot read the conversation %s to check a note against shared pages: %s", conversation.ID, err)
		return false
	}
	shown := map[string]string{}
	theirWords := map[string]bool{}
	for _, message := range messages {
		if message.Role == string(llm.RoleUser) {
			shown[message.ID] = message.Content
			theirWords[message.ID] = true
		}
	}
	return sharedWordsIn(messages).holds(RememberedFact{Text: text}, shown, theirWords)
}

// RestsOnSharedPages is false for a call from outside: there is no
// conversation it read shared pages into.
func (self *directRun) RestsOnSharedPages(context.Context, string) bool {
	return false
}

// CanReadSharedPages is true for a call from outside: a person at a harness,
// who asked for this, and the credential has decided what may be reached.
func (self *directRun) CanReadSharedPages() bool {
	return canReadSharedPages(self.operations, self.owner)
}

func (self *directRun) SharedPages(ctx context.Context) ([]*models.SharedRoot, error) {
	return sharedPagesOf(ctx, self.agent.settings.Database, self.owner.ID)
}

func (self *directRun) ReadSharedPage(ctx context.Context, ownerUserId, path string) (*models.SharedPage, error) {
	return readSharedPageOf(ctx, self.agent.settings.Database, self.owner.ID, ownerUserId, path)
}

func (self *directRun) SearchSharedPages(ctx context.Context, query string, limit int) ([]*models.SharedHit, error) {
	return searchSharedPagesOf(ctx, self.agent.settings.Database, self.owner.ID, query, limit)
}

func sharedPagesOf(ctx context.Context, database db.Database, readerUserId string) (roots []*models.SharedRoot, err error) {
	err = database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		roots, err = SharedPagesFor(tx, readerUserId)
		return err
	})
	return roots, err
}

func readSharedPageOf(ctx context.Context, database db.Database, readerUserId, ownerUserId, path string) (page *models.SharedPage, err error) {
	err = database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		page, err = ReadSharedPage(tx, readerUserId, ownerUserId, path)
		return err
	})
	return page, err
}

func searchSharedPagesOf(ctx context.Context, database db.Database, readerUserId, query string, limit int) (hits []*models.SharedHit, err error) {
	err = database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		hits, err = SearchSharedPages(tx, readerUserId, query, limit)
		return err
	})
	return hits, err
}

// sharedKnowledgeLines is the "Shared with you" section of this turn's index:
// nothing for a run that may not read shared pages, and nothing when none is
// shared.
func (self *AskRun) sharedKnowledgeLines(ctx context.Context) []string {
	if !self.CanReadSharedPages() {
		return nil
	}
	var lines []string
	if err := self.agent.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		lines, err = SharedIndexLines(tx, self.settings.Owner.ID)
		return err
	}); err != nil {
		log.Warningf("cannot read what is shared with %q: %s", self.settings.Owner.Username, err)
		return nil
	}
	return lines
}
