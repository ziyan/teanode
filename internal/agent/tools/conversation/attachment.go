package conversation

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// readAttachment is a file the person attached, read a page at a time: a turn
// shows the first page of a long text file and names this action for the rest.
// The text is what was read out of the file when it was uploaded, so a file
// that has none (a picture, a scan, a video) answers with that and nothing
// else.
func readAttachment(ctx context.Context, run tools.Run, arguments conversationArguments) (*tools.Result, error) {
	attachmentId := strings.TrimSpace(arguments.AgentAttachmentID)
	if attachmentId == "" {
		return nil, fmt.Errorf("read_attachment needs the agent_attachment_id of a file, which the message it came with gives")
	}
	if arguments.Offset < 0 {
		return nil, fmt.Errorf("offset is 0 or the next_offset of the part before")
	}
	var attachment *models.AgentAttachment
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		attachment, err = tx.GetAgentAttachment(attachmentId)
		return err
	}); err != nil {
		return nil, err
	}
	// Somebody else's file is not theirs to read, and an id that is not a
	// file reads the same way from here.
	if attachment == nil || attachment.AgentID != run.Agent().ID {
		return nil, fmt.Errorf("there is no file with the agent_attachment_id %q", attachmentId)
	}
	answer := map[string]any{"agent_attachment_id": attachment.ID, "name": attachment.Name, "content_type": attachment.ContentType}
	if attachment.Text == "" {
		answer["text"] = ""
		answer["note"] = "no text was read out of this file; it is not a text file, or a PDF with a text layer"
	} else {
		part, nextOffset := tools.PageOfText(attachment.Text, arguments.Offset, tools.AttachmentPageCharacters)
		answer["text"] = part
		answer["total_characters"] = utf8.RuneCountInString(attachment.Text)
		if nextOffset > 0 {
			answer["next_offset"] = nextOffset
		}
	}
	result, err := tools.JSONResult(answer)
	if err != nil {
		return nil, err
	}
	// What a file says is what its author wrote, not an instruction to the
	// agent.
	result.Untrusted = true
	result.Note = "read " + attachment.Name
	return result, nil
}
