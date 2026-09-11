// Package mailsend sends a draft, with the person's word.
package mailsend

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/agent/tools/mailbox"
	"github.com/ziyan/teanode/internal/models"
)

func init() {
	tools.Register(func() []*tools.Tool {
		return []*tools.Tool{
			{
				Name: "mail_send", Family: tools.FamilyMailbox, Core: true, Risk: tools.RiskOutward,
				Permissions: []models.Permission{models.PermissionMailSend},
				Description: "Send a draft. Leaves the server, so it always asks the person first.",
				Parameters: tools.Object(map[string]any{
					"draft_id": tools.StringProperty("the draft, from mail_draft"),
				}, "draft_id"),
				Preview: func(arguments json.RawMessage) string {
					return "Send the draft " + strings.TrimSpace(string(arguments))
				},
				Run: runMailSend,
			},
		}
	})
}

type mailSendArguments struct {
	DraftID string `json:"draft_id"`
}

func runMailSend(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	arguments, err := tools.DecodeArguments[mailSendArguments](call)
	if err != nil {
		return nil, err
	}
	if arguments.DraftID == "" {
		return nil, fmt.Errorf("which draft? give draft_id")
	}
	if !call.Confirmed {
		return nil, fmt.Errorf("sending needs the person's confirmation")
	}
	operations := run.Operations()
	views, err := mailbox.GrantedMailboxes(ctx, operations)
	if err != nil {
		return nil, err
	}
	view, _, err := mailbox.MailboxOfItem(ctx, operations, views, arguments.DraftID)
	if err != nil {
		return nil, err
	}
	var draft struct {
		GetMailboxDraft *struct {
			From          string   `json:"from"`
			FromName      string   `json:"fromName"`
			To            []string `json:"to"`
			Cc            []string `json:"cc"`
			Bcc           []string `json:"bcc"`
			Subject       string   `json:"subject"`
			Text          string   `json:"text"`
			HTML          string   `json:"html"`
			ReplyToItemID string   `json:"replyToItemId"`
			ForwardItemID string   `json:"forwardItemId"`
			Attachments   []struct {
				Index int `json:"index"`
			} `json:"attachments"`
		} `json:"GetMailboxDraft"`
	}
	if err := operations.Execute(ctx, `query ($itemId: String!) { GetMailboxDraft(itemId: $itemId) { from fromName to cc bcc subject text html replyToItemId forwardItemId attachments { index } } }`, map[string]any{"itemId": arguments.DraftID}, &draft); err != nil {
		return nil, err
	}
	if draft.GetMailboxDraft == nil {
		return nil, fmt.Errorf("there is no draft %q", arguments.DraftID)
	}
	stored := draft.GetMailboxDraft
	keep := make([]int, 0, len(stored.Attachments))
	for _, attachment := range stored.Attachments {
		keep = append(keep, attachment.Index)
	}
	message := map[string]any{"from": stored.From, "fromName": stored.FromName, "to": stored.To, "cc": stored.Cc, "bcc": stored.Bcc, "subject": stored.Subject, "textContent": stored.Text, "htmlContent": stored.HTML, "draftItemId": arguments.DraftID, "keepAttachments": keep}
	if stored.ReplyToItemID != "" {
		message["replyToItemId"] = stored.ReplyToItemID
	}
	if stored.ForwardItemID != "" {
		message["forwardItemId"] = stored.ForwardItemID
	}
	var result struct {
		SendMailboxMessage struct {
			Mail *struct {
				ID string `json:"id"`
			} `json:"mail"`
		} `json:"SendMailboxMessage"`
	}
	if err := operations.Execute(ctx, `mutation ($mailboxId: String!, $message: MailboxMessageParametersInput!) { SendMailboxMessage(mailboxId: $mailboxId, message: $message) { mail { id } } }`, map[string]any{"mailboxId": view.Mailbox.ID, "message": message}, &result); err != nil {
		return nil, err
	}
	sent := ""
	if result.SendMailboxMessage.Mail != nil {
		sent = result.SendMailboxMessage.Mail.ID
	}
	answer, err := tools.JSONResult(map[string]any{"sent": true, "mail_id": sent, "to": stored.To, "subject": stored.Subject})
	if err != nil {
		return nil, err
	}
	answer.Note = fmt.Sprintf("sent %q to %s", stored.Subject, strings.Join(stored.To, ", "))
	return answer, nil
}
