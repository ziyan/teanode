// Package mailsend sends a draft, with the person's word.
package mailsend

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
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
				// The card is read by somebody deciding whether a message
				// leaves the building, so it says the message: who it goes
				// to and what it is called. It used to print the call --
				// "Send the draft {\"draft_id\":\"01m2ep00bed1yyxq763yn865wq\"}"
				// -- which asks a person to approve an identifier.
				PreviewIn: func(ctx context.Context, arguments json.RawMessage) string {
					var call mailSendArguments
					if err := json.Unmarshal(arguments, &call); err != nil || call.DraftID == "" {
						return "Send a draft"
					}
					run, err := tools.RunFrom(ctx)
					if err != nil {
						return "Send a draft"
					}
					operations := run.Operations()
					views, err := mailbox.GrantedMailboxes(ctx, operations)
					if err != nil || len(views) == 0 {
						return "Send a draft"
					}
					_, found, err := findDraft(ctx, operations, views, call.DraftID)
					if err != nil || found == nil {
						return "Send a draft"
					}
					subject := strings.TrimSpace(found.Subject)
					if subject == "" {
						subject = "a message with no subject"
					} else {
						subject = strconv.Quote(subject)
					}
					recipients := append(append(append([]string{}, found.To...), found.Cc...), found.Bcc...)
					if len(recipients) == 0 {
						return "Send " + subject
					}
					return fmt.Sprintf("Send %s to %s", subject, strings.Join(recipients, ", "))
				},
				Run: runMailSend,
			},
		}
	})
}

type mailSendArguments struct {
	DraftID string `json:"draft_id"`
}

// findDraft is the draft a name refers to and the mailbox it is in.
//
// A key names a draft within one mailbox, so with several granted they are
// asked in turn -- and the draft is carried back with the answer, because
// finding it walks the Drafts folder reading messages, and doing that once
// per caller is how one send became three walks.
func findDraft(ctx context.Context, operations tools.Operations, views []*mailbox.MailboxView, name string) (*mailbox.MailboxView, *mailbox.DraftView, error) {
	if len(views) == 0 {
		return nil, nil, fmt.Errorf("no mailbox is granted")
	}
	var first error
	for _, view := range views {
		found, err := mailbox.FindDraft(ctx, operations, view, name)
		if err == nil && found != nil {
			return view, found, nil
		}
		if first == nil {
			first = err
		}
	}
	if first == nil {
		first = fmt.Errorf("there is no draft %q; it may have been sent or thrown away", name)
	}
	return nil, nil, first
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
	// The draft wherever it is now. What was confirmed is "send this
	// draft", and between the card being shown and the person pressing it
	// the draft may have been saved again -- opening it in the composer is
	// enough -- which leaves the item id it was called with naming nothing.
	view, found, err := findDraft(ctx, operations, views, arguments.DraftID)
	if err != nil {
		return nil, err
	}
	itemId := found.ItemID
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
	if err := operations.Execute(ctx, `query ($itemId: String!) { GetMailboxDraft(itemId: $itemId) { from fromName to cc bcc subject text html replyToItemId forwardItemId attachments { index } } }`, map[string]any{"itemId": itemId}, &draft); err != nil {
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
	message := map[string]any{"from": stored.From, "fromName": stored.FromName, "to": stored.To, "cc": stored.Cc, "bcc": stored.Bcc, "subject": stored.Subject, "textContent": stored.Text, "htmlContent": stored.HTML, "draftItemId": itemId, "keepAttachments": keep}
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
