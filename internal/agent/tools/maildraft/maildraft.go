// Package maildraft writes a draft into Drafts for the person to send.
package maildraft

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/agent/tools/mailbox"
	"github.com/ziyan/teanode/internal/models"
)

func init() {
	tools.Register(func() []*tools.Tool {
		return []*tools.Tool{
			{
				Name: "mail_draft", Family: tools.FamilyMailbox, Core: true, Risk: tools.RiskWrite,
				Permissions: []models.Permission{models.PermissionMailSend},
				Description: "Write a draft: a new message, a reply, a reply to all, or a forward. It is saved in Drafts, in its conversation, for the person to send; nothing goes out. Gives back draft_id for mail_send.",
				Parameters: tools.Object(map[string]any{
					"mode":        tools.EnumProperty("what kind of message", "new", "reply", "reply_all", "forward"),
					"in_reply_to": tools.StringProperty("for reply, reply_all and forward: the item_id of the message"),
					"mailbox":     mailbox.MailboxProperty,
					"from":        tools.StringProperty("one of the mailbox's addresses; the one the message was sent to by default"),
					"to":          tools.ArrayProperty("recipients; filled in for a reply", tools.StringProperty("an address")),
					"cc":          tools.ArrayProperty("copies", tools.StringProperty("an address")),
					"bcc":         tools.ArrayProperty("blind copies", tools.StringProperty("an address")),
					"subject":     tools.StringProperty("the subject; filled in for a reply or a forward"),
					"text":        tools.StringProperty("the body, plain text, in the person's voice; no placeholders"),
					"draft_id":    tools.StringProperty("a draft to revise instead of making a new one"),
				}, "mode", "text"),
				Run: runMailDraft,
			},
		}
	})
}

type mailDraftArguments struct {
	Mode      string   `json:"mode"`
	InReplyTo string   `json:"in_reply_to"`
	Mailbox   string   `json:"mailbox"`
	From      string   `json:"from"`
	To        []string `json:"to"`
	Cc        []string `json:"cc"`
	Bcc       []string `json:"bcc"`
	Subject   string   `json:"subject"`
	Text      string   `json:"text"`
	DraftID   string   `json:"draft_id"`
}

func runMailDraft(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	arguments, err := tools.DecodeArguments[mailDraftArguments](call)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(arguments.Text) == "" {
		return nil, fmt.Errorf("a draft needs text")
	}
	operations := run.Operations()
	views, err := mailbox.GrantedMailboxes(ctx, operations)
	if err != nil {
		return nil, err
	}
	var view *mailbox.MailboxView
	var original *mailbox.ThreadView
	var originalItemId string
	mode := arguments.Mode
	if mode == "" {
		mode = "new"
	}
	if mode != "new" {
		if arguments.InReplyTo == "" {
			return nil, fmt.Errorf("%s needs in_reply_to", mode)
		}
		if view, original, err = mailbox.MailboxOfItem(ctx, operations, views, arguments.InReplyTo); err != nil {
			return nil, err
		}
		originalItemId = arguments.InReplyTo
	} else if view, err = mailbox.FindMailbox(views, arguments.Mailbox); err != nil {
		return nil, err
	}
	mine := map[string]bool{}
	for _, address := range view.Mailbox.Addresses {
		mine[strings.ToLower(address.Address)] = true
	}
	var originalMail *struct {
		ID         string    `json:"id"`
		From       string    `json:"from"`
		FromName   string    `json:"fromName"`
		Recipients []string  `json:"recipients"`
		Subject    string    `json:"subject"`
		ReceivedAt time.Time `json:"receivedAt"`
		MessageID  string    `json:"messageId"`
	}
	if original != nil {
		for _, entry := range original.Items {
			if entry.Item.ID == originalItemId {
				originalMail = entry.Item.Mail
			}
		}
	}
	from := strings.TrimSpace(arguments.From)
	if from == "" {
		if originalMail != nil {
			for _, recipient := range originalMail.Recipients {
				if mine[strings.ToLower(recipient)] {
					from = strings.ToLower(recipient)
				}
			}
		}
		if from == "" && len(view.Mailbox.Addresses) > 0 {
			from = view.Mailbox.Addresses[0].Address
		}
	}
	if !mine[strings.ToLower(from)] {
		return nil, fmt.Errorf("%q is not an address of mailbox %q", from, view.Mailbox.Name)
	}
	to, cc, bcc := arguments.To, arguments.Cc, arguments.Bcc
	subject := strings.TrimSpace(arguments.Subject)
	message := map[string]any{"from": from, "textContent": arguments.Text}
	switch mode {
	case "reply", "reply_all":
		if originalMail == nil {
			return nil, fmt.Errorf("the message to answer is gone")
		}
		if len(to) == 0 {
			to = []string{originalMail.From}
		}
		if mode == "reply_all" && len(arguments.Cc) == 0 {
			for _, recipient := range originalMail.Recipients {
				if !mine[strings.ToLower(recipient)] && !strings.EqualFold(recipient, originalMail.From) {
					cc = append(cc, recipient)
				}
			}
		}
		if subject == "" {
			subject = tools.ReplySubject(originalMail.Subject)
		}
		message["replyToItemId"] = originalItemId
	case "forward":
		if originalMail == nil {
			return nil, fmt.Errorf("the message to forward is gone")
		}
		if subject == "" {
			subject = "Fwd: " + tools.ThreadSubject(originalMail.Subject)
		}
		if len(to) == 0 {
			return nil, fmt.Errorf("a forward needs to")
		}
		message["forwardItemId"] = originalItemId
	case "new":
		if len(to) == 0 {
			return nil, fmt.Errorf("a new message needs to")
		}
	default:
		return nil, fmt.Errorf("%q is not a mode of mail_draft", mode)
	}
	message["to"], message["cc"], message["bcc"], message["subject"] = to, cc, bcc, subject
	if arguments.DraftID != "" {
		message["draftItemId"] = arguments.DraftID
	}
	var result struct {
		SaveMailboxDraft struct {
			ID string `json:"id"`
		} `json:"SaveMailboxDraft"`
	}
	if err := operations.Execute(ctx, `mutation ($mailboxId: String!, $message: MailboxMessageParametersInput!) { SaveMailboxDraft(mailboxId: $mailboxId, message: $message) { id } }`, map[string]any{"mailboxId": view.Mailbox.ID, "message": message}, &result); err != nil {
		return nil, err
	}
	preview := arguments.Text
	if len(preview) > 300 {
		preview = preview[:300] + "…"
	}
	answer, err := tools.JSONResult(map[string]any{"draft_id": result.SaveMailboxDraft.ID, "mailbox": view.Mailbox.Name, "from": from, "to": to, "cc": cc, "bcc": bcc, "subject": subject, "preview": preview, "note": "saved in Drafts; the person sends it, or mail_send with their confirmation"})
	if err != nil {
		return nil, err
	}
	answer.Note = fmt.Sprintf("drafted %q to %s", subject, strings.Join(to, ", "))
	return answer, nil
}
