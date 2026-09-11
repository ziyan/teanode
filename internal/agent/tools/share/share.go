// Package share is how the agent hands the person a file: the photo on
// a message, a file from their computer, something already in the
// conversation. What it hands over becomes a file of the conversation,
// shown under the tool line — a picture inline, a video playing — and
// sent to a chat app as a photo, a video or a document. A picture it
// fetches it may look at as well, which is how it answers what is in
// the photo somebody sent.
package share

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/agent/tools/computer"
	"github.com/ziyan/teanode/internal/agent/tools/mailbox"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

func init() {
	tools.Register(func() []*tools.Tool {
		return []*tools.Tool{
			{
				Name: "share_file", Family: tools.FamilyGeneral, Core: true, Risk: tools.RiskRead,
				Description: "Hand the person a file, shown in the conversation — a picture inline, a video playing, anything else to open — and sent to their chat app if that is where they are. From a message (source mail: the item and the attachment's number or name as mail_read lists them), from their attached computer (source computer: the path), or one already in this conversation (source conversation: its attachment id). With look, a picture is shown to you as well, so you can say what is in it. Say in the answer what you handed over; the person sees it under the tool line.",
				Parameters: tools.Object(map[string]any{
					"source":        tools.EnumProperty("where the file is", "mail", "computer", "conversation"),
					"item_id":       tools.StringProperty("mail: the message's item id"),
					"attachment":    tools.StringProperty("mail: the attachment's number or name as mail_read lists them"),
					"computer":      tools.StringProperty("computer: which one, when several are attached"),
					"path":          tools.StringProperty("computer: the file's path; ~ is the person's home"),
					"attachment_id": tools.StringProperty("conversation: the attachment's id"),
					"look":          tools.BooleanProperty("also show the picture to you, for a picture"),
					"caption":       tools.StringProperty("a line to send with it, optional"),
				}, "source"),
				Guidance: "share_file: hands the person the file itself; use it when they ask to see or have something, and with look when you need to see a picture to answer.",
				Run:      runShare,
			},
		}
	})
}

type shareArguments struct {
	Source       string `json:"source"`
	ItemID       string `json:"item_id"`
	Attachment   string `json:"attachment"`
	Computer     string `json:"computer"`
	Path         string `json:"path"`
	AttachmentID string `json:"attachment_id"`
	Look         bool   `json:"look"`
	Caption      string `json:"caption"`
}

// The bounds of a hand-over.
const (
	// shareBytes is the largest file handed over: what a chat app takes
	// and a browser shows without fuss.
	shareBytes = 32 << 20
	// lookBytes is the largest picture shown to the model.
	lookBytes = 5 << 20
	// sharedMessage marks an attachment as a file the agent handed over.
	sharedMessage = "shared"
	// fetchWait is how long the computer has to send a file across.
	fetchWait = 2 * time.Minute
)

// fetched is a file as its source gave it.
type fetched struct {
	name        string
	contentType string
	content     []byte
	existing    *models.AgentAttachment
}

func runShare(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[shareArguments](call)
	if err != nil {
		return nil, err
	}
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	store := run.Storage()
	if store == nil {
		return nil, fmt.Errorf("nowhere to keep a file")
	}
	var file *fetched
	switch arguments.Source {
	case "mail":
		file, err = fromMail(ctx, run, arguments)
	case "computer":
		file, err = fromComputer(ctx, run, arguments)
	case "conversation":
		file, err = fromConversation(ctx, run, arguments)
	default:
		return nil, fmt.Errorf("%q is not mail, computer or conversation", arguments.Source)
	}
	if err != nil {
		return nil, err
	}
	if len(file.content) > shareBytes {
		return nil, fmt.Errorf("%s is %d bytes, more than %d; it is too large to hand over", file.name, len(file.content), shareBytes)
	}
	attachment := file.existing
	if attachment == nil {
		if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
			attachment, err = tx.CreateAgentAttachment(&models.AgentAttachment{
				AgentID:        run.Agent().ID,
				ConversationID: run.Conversation().ID,
				MessageID:      sharedMessage,
				Name:           safeName(file.name),
				ContentType:    file.contentType,
				Size:           int64(len(file.content)),
			})
			if err != nil {
				return err
			}
			return store.PutFile(ctx, attachment.ID, file.content)
		}); err != nil {
			return nil, err
		}
	}
	answer := map[string]any{
		"attachment_id": attachment.ID, "name": attachment.Name, "content_type": attachment.ContentType, "size": attachment.Size,
		"url": "/api/v1/agent/attachments/" + attachment.ID,
	}
	if strings.TrimSpace(arguments.Caption) != "" {
		answer["caption"] = strings.TrimSpace(arguments.Caption)
	}
	var images []llm.ContentPart
	if arguments.Look {
		switch {
		case !tools.IsImage(attachment.ContentType):
			answer["look"] = "not a picture; only a picture can be shown to you"
		case len(file.content) > lookBytes:
			answer["look"] = fmt.Sprintf("the picture is %d bytes, too large to show you", len(file.content))
		default:
			images = append(images, llm.ContentPart{Type: "image", MediaType: attachment.ContentType, Data: file.content})
			answer["look"] = "the picture follows for you to look at"
		}
	}
	result, err := tools.JSONResult(answer)
	if err != nil {
		return nil, err
	}
	result.Note = "handed over " + attachment.Name
	result.Images = images
	return result, nil
}

// fromMail is a part of a message the person may read, by the number or
// the name mail_read lists.
func fromMail(ctx context.Context, run tools.Run, arguments shareArguments) (*fetched, error) {
	itemId := strings.TrimSpace(arguments.ItemID)
	wanted := strings.TrimSpace(arguments.Attachment)
	if itemId == "" || wanted == "" {
		return nil, fmt.Errorf("a message's item_id and the attachment's number or name are needed")
	}
	operations := run.Operations()
	views, err := mailbox.GrantedMailboxes(ctx, operations)
	if err != nil {
		return nil, err
	}
	_, thread, err := mailbox.MailboxOfItem(ctx, operations, views, itemId)
	if err != nil {
		return nil, err
	}
	mailId := ""
	for _, entry := range thread.Items {
		if entry.Item.ID == itemId {
			mailId = entry.Item.MailID
		}
	}
	if mailId == "" {
		return nil, fmt.Errorf("there is no message %q", itemId)
	}
	content, err := mailbox.GetContent(ctx, operations, mailId)
	if err != nil {
		return nil, err
	}
	if content == nil || len(content.Attachments) == 0 {
		return nil, fmt.Errorf("the message has no attachments")
	}
	index := -1
	if number, err := strconv.Atoi(wanted); err == nil && number >= 1 && number <= len(content.Attachments) {
		index = content.Attachments[number-1].Index
	} else {
		for _, attachment := range content.Attachments {
			if strings.EqualFold(attachment.Filename, wanted) {
				index = attachment.Index
			}
		}
	}
	if index < 0 {
		names := make([]string, 0, len(content.Attachments))
		for number, attachment := range content.Attachments {
			names = append(names, fmt.Sprintf("%d. %s", number+1, attachment.Filename))
		}
		return nil, fmt.Errorf("no attachment %q on the message; there are: %s", wanted, strings.Join(names, ", "))
	}
	headers, body, err := run.Storage().Get(ctx, mailId)
	if err != nil {
		return nil, err
	}
	part, err := mailparse.PartAt(headers, body, index)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(part.Filename)
	if name == "" {
		name = fmt.Sprintf("attachment-%d", index)
	}
	contentType := part.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return &fetched{name: name, contentType: contentType, content: part.Content}, nil
}

// fromComputer is a file the daemon on the person's computer sends
// across.
func fromComputer(ctx context.Context, run tools.Run, arguments shareArguments) (*fetched, error) {
	if strings.TrimSpace(arguments.Path) == "" {
		return nil, fmt.Errorf("the file's path is needed")
	}
	attached, err := computer.Of(run, arguments.Computer)
	if err != nil {
		return nil, err
	}
	data, err := attached.Ask(ctx, "filesystem", map[string]any{"action": "fetch", "path": arguments.Path}, fetchWait)
	if err != nil {
		return nil, err
	}
	var answer struct {
		Error       string `json:"error"`
		Name        string `json:"name"`
		ContentType string `json:"content_type"`
		Base64      string `json:"base64"`
	}
	if err := json.Unmarshal(data, &answer); err != nil {
		return nil, fmt.Errorf("the computer's answer could not be read: %w", err)
	}
	if answer.Error != "" {
		return nil, fmt.Errorf("%s", answer.Error)
	}
	content, err := base64.StdEncoding.DecodeString(answer.Base64)
	if err != nil {
		return nil, fmt.Errorf("the computer's answer could not be read: %w", err)
	}
	name := strings.TrimSpace(answer.Name)
	if name == "" {
		name = "file"
	}
	contentType := answer.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return &fetched{name: name, contentType: contentType, content: content}, nil
}

// fromConversation is a file already in the conversation: named, not
// copied; its bytes read only when the model wants to look.
func fromConversation(ctx context.Context, run tools.Run, arguments shareArguments) (*fetched, error) {
	id := strings.TrimSpace(arguments.AttachmentID)
	if id == "" {
		return nil, fmt.Errorf("the attachment_id is needed")
	}
	var attachment *models.AgentAttachment
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		attachment, err = tx.GetAgentAttachment(id)
		return err
	}); err != nil {
		return nil, err
	}
	if attachment == nil || attachment.AgentID != run.Agent().ID {
		return nil, fmt.Errorf("there is no attachment %q in this conversation", id)
	}
	file := &fetched{name: attachment.Name, contentType: attachment.ContentType, existing: attachment}
	if arguments.Look && tools.IsImage(attachment.ContentType) && attachment.Size <= lookBytes {
		content, err := run.Storage().GetFile(ctx, attachment.ID)
		if err != nil {
			return nil, err
		}
		file.content = content
	}
	return file, nil
}

// safeName is the file's name as it is kept: no path, nothing empty.
func safeName(name string) string {
	name = strings.TrimSpace(name)
	if at := strings.LastIndexAny(name, `/\`); at >= 0 {
		name = name[at+1:]
	}
	name = strings.Map(func(character rune) rune {
		if character < 0x20 || character == 0x7f {
			return -1
		}
		return character
	}, name)
	if name == "" || name == "." || name == ".." {
		return "file"
	}
	return name
}
