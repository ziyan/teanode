// Package maildraft writes a draft into Drafts for the person to send.
package maildraft

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/agent/tools/mailbox"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

func init() {
	tools.Register(func() []*tools.Tool {
		return []*tools.Tool{
			{
				Name: "mail_draft", Family: tools.FamilyMailbox, Core: true, Risk: tools.RiskWrite,
				Permissions: []models.Permission{models.PermissionMailSend},
				Description: "Write a draft: a new message, a reply, a reply to all, or a forward. It is saved in Drafts, in its conversation, for the person to send; nothing goes out. Gives back draft_id for mail_send.",
				Guidance:    "mail_draft: plain text is the ordinary answer to a message and needs no html. Reach for html when the person asked for something made rather than said -- a summary with headings, a table of figures, an announcement. Mail is not the web: no script runs, nothing loads from another server, and no CSS variable survives, so write the colours out; and a picture has to travel with the message, so it must already be a file of this conversation -- one the person handed you, or one share_file fetched off their computer or out of a message -- and named in images. Most clients refuse SVG, so a drawing has to arrive as a PNG or a JPEG. Lay a wide thing out with a table rather than with flex or grid, which older clients ignore; keep to system fonts and to colours that read on white, since many clients paint their own background.",
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
					"html":        tools.StringProperty("optional: the same message styled, as HTML. Send it with text, never instead of it -- text is what a reader with no HTML gets. Plain text is right for almost everything; use this when the person asked for styling, or when what they asked for wants a heading, a table or a card. Write an ordinary document with a <style> block: the server moves the stylesheet into the elements, which is what makes it survive a mail client"),
					"images":      tools.ArrayProperty("optional: files of this conversation to put in the body -- a picture you were given, or one you made. Refer to each by name in the html: <img src=\"cid:chart.png\">", tools.StringProperty("an attachment id of this conversation")),
					"draft_id":    tools.StringProperty("a draft to revise instead of making a new one"),
				}, "mode", "text"),
				Preview: tools.PreviewOf(func(call struct {
					Mode    string   `json:"mode"`
					Subject string   `json:"subject"`
					To      []string `json:"to"`
				}) string {
					// Nothing goes out, so the card is about what appears
					// in Drafts rather than about a message being sent.
					named := tools.Named(call.Subject, "a message")
					if to := tools.Some(call.To, 3); to != "" {
						named += " to " + to
					}
					switch call.Mode {
					case "reply", "reply_all":
						return "Write a reply in Drafts: " + named
					case "forward":
						return "Write a forward in Drafts: " + named
					}
					return "Write " + named + " in Drafts"
				}),
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
	HTML      string   `json:"html"`
	Images    []string `json:"images"`
	DraftID   string   `json:"draft_id"`
}

// ownFiles refuses anything that is not a file of this conversation.
func ownFiles(ctx context.Context, run tools.Run, ids []string) error {
	return run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		for _, id := range ids {
			attachment, err := tx.GetAgentAttachment(strings.TrimSpace(id))
			if err != nil {
				return err
			}
			if attachment == nil || attachment.AgentID != run.Agent().ID ||
				(attachment.ConversationID != "" && attachment.ConversationID != run.Conversation().ID) {
				return fmt.Errorf("there is no file %q in this conversation", id)
			}
			if !tools.IsImage(attachment.ContentType) {
				return fmt.Errorf("%s is %s; only a picture goes in the body", attachment.Name, attachment.ContentType)
			}
		}
		return nil
	})
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
	// Both forms when the model wrote both: the message goes out as
	// multipart/alternative, and a reader with no HTML still gets the words.
	message := map[string]any{"from": from, "textContent": arguments.Text}
	if strings.TrimSpace(arguments.HTML) != "" {
		message["htmlContent"] = arguments.HTML
	}
	if len(arguments.Images) > 0 {
		if strings.TrimSpace(arguments.HTML) == "" {
			return nil, fmt.Errorf("a picture in the body needs html to refer to it")
		}
		// This conversation's files, for the reason share_file asks the
		// same: the ids of files in other conversations are readable, and
		// a turn somebody else started in a group chat must not be able to
		// put a picture from a private conversation into a message.
		if err := ownFiles(ctx, run, arguments.Images); err != nil {
			return nil, err
		}
		message["inlineImages"] = arguments.Images
	}
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
		// The draft being revised is named by its key; what the mutation
		// wants is the item holding it now.
		existing, err := mailbox.FindDraft(ctx, operations, view, arguments.DraftID)
		if err != nil {
			return nil, err
		}
		message["draftItemId"] = existing.ItemID
	}
	var result struct {
		SaveMailboxDraft struct {
			ID string `json:"id"`
		} `json:"SaveMailboxDraft"`
	}
	if err := operations.Execute(ctx, `mutation ($mailboxId: String!, $message: MailboxMessageParametersInput!) { SaveMailboxDraft(mailboxId: $mailboxId, message: $message) { id } }`, map[string]any{"mailboxId": view.Mailbox.ID, "message": message}, &result); err != nil {
		return nil, err
	}
	// The draft's own name, which is what to hold on to: saving it again --
	// which the composer does the moment the person opens it -- writes a new
	// message with a new item id, and an answer carrying that id would be
	// stale before anybody read it.
	var saved struct {
		GetMailboxDraft struct {
			Key string `json:"key"`
		} `json:"GetMailboxDraft"`
	}
	draftId := result.SaveMailboxDraft.ID
	if err := operations.Execute(ctx, `query ($itemId: String!) { GetMailboxDraft(itemId: $itemId) { key } }`, map[string]any{"itemId": draftId}, &saved); err == nil && saved.GetMailboxDraft.Key != "" {
		draftId = saved.GetMailboxDraft.Key
	}
	preview := arguments.Text
	if len(preview) > 300 {
		preview = preview[:300] + "…"
	}
	answer, err := tools.JSONResult(map[string]any{"draft_id": draftId, "mailbox": view.Mailbox.Name, "from": from, "to": to, "cc": cc, "bcc": bcc, "subject": subject, "preview": preview, "note": "saved in Drafts; the person sends it, or mail_send with their confirmation"})
	if err != nil {
		return nil, err
	}
	answer.Note = fmt.Sprintf("drafted %q to %s", subject, strings.Join(to, ", "))
	return answer, nil
}
