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
				Description: "Write a draft: a new message, a reply, a reply to all, or a forward. It is saved in Drafts, in its conversation, for the person to send; nothing goes out. Gives back draft_id for mail_send, and for `discard`, which throws the draft away for good as the Discard button does.",
				Parameters: tools.Object(map[string]any{
					"mode":        tools.EnumProperty("what kind of message, or discard to throw a draft away", "new", "reply", "reply_all", "forward", "discard"),
					"in_reply_to": tools.StringProperty("for reply, reply_all and forward: the item_id of the message"),
					"mailbox":     mailbox.MailboxProperty,
					"from":        tools.StringProperty("one of the mailbox's addresses; the one the message was sent to by default"),
					"to":          tools.ArrayProperty("recipients; filled in for a reply", tools.StringProperty("an address")),
					"cc":          tools.ArrayProperty("copies", tools.StringProperty("an address")),
					"bcc":         tools.ArrayProperty("blind copies", tools.StringProperty("an address")),
					"subject":     tools.StringProperty("the subject; filled in for a reply or a forward"),
					"text":        tools.StringProperty("the body, plain text, in the person's voice; no placeholders"),
					"html":        tools.StringProperty("optional: the same message styled, as HTML. Send it with text, never instead of it -- text is what a reader with no HTML gets. Plain text is right for almost everything; use this when the person asked for styling, or when what they asked for wants a heading, a table or a card. Write an ordinary document with a <style> block: the server moves the stylesheet into the elements, which is what makes it survive a mail client. Mail is not the web: write colours out, lay out with tables, use system fonts and colours that read on white"),
					"images":      tools.ArrayProperty("optional: files of this conversation to put in the body -- a picture you were given, or one you made. Refer to each by name in the html: <img src=\"cid:chart.png\">", tools.StringProperty("an attachment id of this conversation")),
					"draft_id":    tools.StringProperty("a draft to revise instead of making a new one; for discard, the draft to throw away"),
				}, "mode"),
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
					case "discard":
						return "Discard a draft"
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

// ownFiles refuses anything that is not a file of this conversation, and
// answers the names of those that are drawings (SVG), which most mail
// clients refuse.
func ownFiles(ctx context.Context, run tools.Run, ids []string) ([]string, error) {
	var drawingNames []string
	err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		for _, id := range ids {
			attachment, err := tx.GetAgentAttachment(strings.TrimSpace(id))
			if err != nil {
				return err
			}
			if attachment == nil || attachment.AgentID != run.Agent().ID ||
				(attachment.ConversationID != "" && attachment.ConversationID != tools.ConversationIDOf(run)) {
				return fmt.Errorf("there is no file %q in this conversation", id)
			}
			if !tools.IsImage(attachment.ContentType) {
				return fmt.Errorf("%s is %s; only a picture goes in the body", attachment.Name, attachment.ContentType)
			}
			if strings.EqualFold(strings.TrimSpace(attachment.ContentType), "image/svg+xml") {
				drawingNames = append(drawingNames, attachment.Name)
			}
		}
		return nil
	})
	return drawingNames, err
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
	operations := run.Operations()
	views, err := mailbox.GrantedMailboxes(ctx, operations)
	if err != nil {
		return nil, err
	}
	if arguments.Mode == "discard" {
		return discard(ctx, operations, views, arguments.DraftID)
	}
	if strings.TrimSpace(arguments.Text) == "" {
		return nil, fmt.Errorf("a draft needs text")
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
	var drawingNames []string
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
		if drawingNames, err = ownFiles(ctx, run, arguments.Images); err != nil {
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
	fields := map[string]any{"draft_id": draftId, "mailbox": view.Mailbox.Name, "from": from, "to": to, "cc": cc, "bcc": bcc, "subject": subject, "preview": preview, "note": "saved in Drafts; the person sends it, or mail_send with their confirmation"}
	if problems := htmlProblems(arguments.HTML, drawingNames); len(problems) > 0 {
		fields["html_problems"] = problems
	}
	answer, err := tools.JSONResult(fields)
	if err != nil {
		return nil, err
	}
	answer.Note = fmt.Sprintf("drafted %q to %s", subject, strings.Join(to, ", "))
	return answer, nil
}

// htmlProblems are what in a message's html a mail client will not keep,
// each said with what to do instead. They come back with the draft rather
// than sit in the prompt of every turn: only a message with html in it can
// have them, and the model is told right after writing the thing that is
// wrong, with the draft to revise. drawingNames are the pictures given in
// images that are drawings (SVG), which most clients refuse.
func htmlProblems(html string, drawingNames []string) []string {
	var problems []string
	lowered := strings.ToLower(html)
	if strings.Contains(lowered, "<script") {
		problems = append(problems, "no script runs in mail, so what the script draws will be missing; build it from a table, or send a picture")
	}
	if strings.Contains(lowered, "var(--") {
		problems = append(problems, "no CSS variable survives in mail; write the colours out")
	}
	for _, layout := range []string{"display:flex", "display: flex", "display:inline-flex", "display: inline-flex", "display:grid", "display: grid"} {
		if strings.Contains(lowered, layout) {
			problems = append(problems, "older mail clients ignore flex and grid; lay a wide thing out with a table")
			break
		}
	}
	for _, remote := range []string{`src="http`, `src='http`, "url(http", `url("http`, "url('http"} {
		if strings.Contains(lowered, remote) {
			problems = append(problems, "nothing loads from another server in mail; a picture has to travel with the message, as a file of this conversation named in images and referred to as cid:name")
			break
		}
	}
	if len(drawingNames) > 0 {
		problems = append(problems, fmt.Sprintf("most mail clients refuse SVG (%s); a drawing has to arrive as a PNG or a JPEG", strings.Join(drawingNames, ", ")))
	}
	return problems
}

// discard throws a draft away the way the composer's Discard button does:
// the item is deleted outright rather than moved to Trash, where a draft
// would only sit as a message to restore. The draft is found by the key it
// keeps across saves, because the item id mail_draft was given names one
// save and is stale the moment the person opens the draft.
func discard(ctx context.Context, operations tools.Operations, views []*mailbox.MailboxView, draftId string) (*tools.Result, error) {
	if strings.TrimSpace(draftId) == "" {
		return nil, fmt.Errorf("which draft? give draft_id")
	}
	var found *mailbox.DraftView
	var view *mailbox.MailboxView
	for _, candidate := range views {
		if draft, err := mailbox.FindDraft(ctx, operations, candidate, draftId); err == nil && draft != nil {
			found, view = draft, candidate
			break
		}
	}
	if found == nil {
		return nil, fmt.Errorf("there is no draft %q; it may have been sent or thrown away already", draftId)
	}
	var deleted map[string]any
	if err := operations.Execute(ctx, `mutation ($itemIds: [String!]!) { DeleteMailboxItems(itemIds: $itemIds) }`, map[string]any{"itemIds": []string{found.ItemID}}, &deleted); err != nil {
		return nil, err
	}
	answer, err := tools.JSONResult(map[string]any{"draft_id": draftId, "mailbox": view.Mailbox.Name, "subject": found.Subject, "note": "discarded; the draft is gone for good"})
	if err != nil {
		return nil, err
	}
	answer.Note = "discarded " + tools.Named(found.Subject, "a draft")
	return answer, nil
}
