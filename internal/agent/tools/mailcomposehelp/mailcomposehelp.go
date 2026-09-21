// Package mailcomposehelp writes the body of a reply for the person to use,
// without saving anything.
package mailcomposehelp

import (
	"context"
	"fmt"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/agent/tools/mailbox"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

func init() {
	tools.Register(func() []*tools.Tool {
		return []*tools.Tool{
			{
				Name: "mail_compose_help", Family: tools.FamilyMailbox, Risk: tools.RiskRead,
				Permissions: []models.Permission{models.PermissionMailRead},
				Description: "Write the body of a reply to a message without saving anything, for the person to use. Only where the mailbox lets the agent draft.",
				Parameters: tools.Object(map[string]any{
					"in_reply_to":  tools.StringProperty("the item_id of the message"),
					"instructions": tools.StringProperty("what the reply should do, in a line"),
				}, "in_reply_to"),
				Run: runMailComposeHelp,
			},
		}
	})
}

type composeHelpArguments struct {
	InReplyTo    string `json:"in_reply_to"`
	Instructions string `json:"instructions"`
}

func runMailComposeHelp(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[composeHelpArguments](call)
	if err != nil {
		return nil, err
	}
	if arguments.InReplyTo == "" {
		return nil, fmt.Errorf("which message? give in_reply_to")
	}
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	views, err := mailbox.GrantedMailboxes(ctx, run.Operations())
	if err != nil {
		return nil, err
	}
	view, thread, err := mailbox.MailboxOfItem(ctx, run.Operations(), views, arguments.InReplyTo)
	if err != nil {
		return nil, err
	}
	mailId := ""
	for _, entry := range thread.Items {
		if entry.Item.ID == arguments.InReplyTo {
			mailId = entry.Item.MailID
		}
	}
	var mail *models.Mail
	var mailbox *models.Mailbox
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		found, err := tx.GetMails([]string{mailId}, nil)
		if err != nil {
			return err
		}
		if len(found) > 0 {
			mail = found[0]
		}
		mailbox, err = tx.GetMailbox(view.Mailbox.ID)
		return err
	}); err != nil {
		return nil, err
	}
	if mail == nil || mailbox == nil {
		return nil, fmt.Errorf("the message is gone")
	}
	draft, err := run.DraftReply(ctx, &models.AgentDraftRequest{Agent: run.Agent(), Owner: run.Owner(), Mailbox: mailbox, Mail: mail, Instructions: arguments.Instructions})
	if err != nil {
		return nil, err
	}
	return tools.JSONResult(map[string]any{"text": draft.Text, "note": "nothing was saved; use mail_draft to save it"})
}

// --- folders ------------------------------------------------------------
