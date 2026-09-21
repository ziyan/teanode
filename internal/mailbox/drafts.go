package mailbox

import (
	"context"
	"fmt"

	"github.com/ziyan/teanode/internal/agent/feedback"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// RemoveDraft removes an item from an already authorized mailbox. Missing items
// and items moved out of the mailbox are left alone. Stored bytes follow normal
// retention so rolling back this command restores a readable draft.
func RemoveDraft(ctx context.Context, transaction db.Transaction, mailboxId, itemId string) error {
	return transaction.TransactionContext(ctx, func(command db.Transaction) error {
		item, err := command.LockItem(itemId)
		if err != nil || item == nil {
			return err
		}
		folder, err := command.GetFolder(item.FolderID)
		if err != nil || folder == nil || folder.MailboxID != mailboxId {
			return err
		}
		if !item.Draft {
			return fmt.Errorf("%w: item is not a draft", db.ErrInvalidArguments)
		}
		held, err := command.ListAgentReplies(&db.AgentReplyFilter{DraftItemID: item.ID, Statuses: []models.AgentReplyStatus{models.AgentReplyHeld}}, &db.Options{Limit: 1})
		if err != nil {
			return err
		}
		if len(held) > 0 {
			var wasCancelled bool
			updated, err := command.UpdateAgentReply(held[0].ID, func(reply *models.AgentReply) error {
				// The reply may have changed while waiting for its row lock.
				if reply.Status == models.AgentReplyHeld && reply.DraftItemID == item.ID {
					reply.Status = models.AgentReplyCancelled
					reply.Reason = "taken over by the person"
					reply.DraftItemID = ""
					wasCancelled = true
				}
				return nil
			})
			if err != nil {
				return err
			}
			if wasCancelled {
				if err := feedback.RecordReplyDeclined(command, updated, "taken over by the person, who wrote their own"); err != nil {
					return err
				}
			}
		}
		_, err = command.DeleteItems([]string{item.ID})
		return err
	})
}
