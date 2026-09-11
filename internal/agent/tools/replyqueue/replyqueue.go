// Package replyqueue is the replies the agent holds on the person's
// behalf, and the <pending> overlay that lists them.
package replyqueue

import (
	"context"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

func init() {
	tools.Register(func() []*tools.Tool {
		return []*tools.Tool{
			{
				Name: "reply_queue", Family: tools.FamilyMailbox, Core: true, Risk: tools.RiskWrite,
				Permissions: []models.Permission{models.PermissionMailRead},
				Description: "The replies the agent is holding to send on the person's behalf: list them, or cancel one before it goes.",
				Parameters: tools.Object(map[string]any{
					"action":   tools.EnumProperty("list or cancel", "list", "cancel"),
					"reply_id": tools.StringProperty("for cancel: the reply"),
				}, "action"),
				Run:     runReplyQueue,
				Overlay: pendingOverlay,
			},
		}
	})
}

type replyQueueArguments struct {
	Action  string `json:"action"`
	ReplyID string `json:"reply_id"`
}

func heldReplies(ctx context.Context, run tools.Run) ([]*models.AgentReply, error) {
	var held []*models.AgentReply
	err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		held, err = tx.ListAgentReplies(&db.AgentReplyFilter{AgentID: run.Agent().ID, Statuses: []models.AgentReplyStatus{models.AgentReplyHeld}}, &db.Options{Limit: 20})
		return err
	})
	return held, err
}

func runReplyQueue(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[replyQueueArguments](call)
	if err != nil {
		return nil, err
	}
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	location := tools.Location(run.Owner())
	switch arguments.Action {
	case "", "list":
		held, err := heldReplies(ctx, run)
		if err != nil {
			return nil, err
		}
		rows := make([]map[string]any, 0, len(held))
		for _, reply := range held {
			row := map[string]any{"reply_id": reply.ID, "to": reply.To, "subject": reply.Subject, "text": reply.Text}
			if reply.SendAfter != nil {
				row["sends_at"] = reply.SendAfter.In(location).Format("2006-01-02 15:04")
			}
			rows = append(rows, row)
		}
		return tools.JSONResult(map[string]any{"held": rows})
	case "cancel":
		if arguments.ReplyID == "" {
			return nil, fmt.Errorf("which reply? give reply_id")
		}
		var cancelled *models.AgentReply
		if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
			reply, err := tx.GetAgentReply(arguments.ReplyID)
			if err != nil {
				return err
			}
			if reply == nil || reply.AgentID != run.Agent().ID {
				return fmt.Errorf("there is no held reply %q", arguments.ReplyID)
			}
			if reply.Status != models.AgentReplyHeld {
				return fmt.Errorf("the reply is %s, not held", reply.Status)
			}
			if err := run.DiscardDraft(ctx, tx, reply.DraftItemID); err != nil {
				return err
			}
			cancelled, err = tx.UpdateAgentReply(reply.ID, func(reply *models.AgentReply) error {
				reply.Status = models.AgentReplyCancelled
				reply.Reason = "cancelled by the person, through the agent"
				reply.DraftItemID = ""
				return nil
			})
			return err
		}); err != nil {
			return nil, err
		}
		return tools.TextResult("cancelled the reply to %s about %q; nothing will be sent", cancelled.To, cancelled.Subject), nil
	}
	return nil, fmt.Errorf("%q is not an action of reply_queue", arguments.Action)
}

// pendingOverlay says what is about to go out, so "anything going out?" is
// answered from the prompt and a second reply to the same conversation is
// not drafted.
func pendingOverlay(ctx context.Context) string {
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return ""
	}
	held, err := heldReplies(ctx, run)
	if err != nil || len(held) == 0 {
		return ""
	}
	location := tools.Location(run.Owner())
	lines := make([]string, 0, len(held))
	for _, reply := range held {
		when := ""
		if reply.SendAfter != nil {
			when = " at " + reply.SendAfter.In(location).Format("15:04")
		}
		lines = append(lines, fmt.Sprintf("- reply %s to %s about %q%s", reply.ID, reply.To, reply.Subject, when))
	}
	return "<pending>\nReplies held for sending on the person's behalf:\n" + strings.Join(lines, "\n") + "\n</pending>"
}

// --- subscriptions -------------------------------------------------------
