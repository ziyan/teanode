package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/security"
)

// A file sent to the person's main chat: by a program over MCP, by the agent
// from another conversation, or by a run nobody is present for. It is posted
// the way a file the agent hands over in a turn is shown, so the dashboard,
// the phone, its notifications and a linked chat app read it with no new
// shape: a line saying who sent it (models.FileSentMarker), the agent's call
// of share_file, the file as that call's answer, and the caption.

const (
	// fileSentSurface is the surface the line is published under: like an
	// alert's, it opens the drawer.
	fileSentSurface = "file_sent"

	// mainChatWaitingBatch is how many waiting files one sweep posts.
	mainChatWaitingBatch = 20

	// UploadFor is how long a program has to send the bytes of a file it
	// is sending to the main chat.
	UploadFor = time.Hour
)

var _ tools.MainChatSending = (*AskRun)(nil)
var _ tools.MainChatSending = (*directRun)(nil)

// SendToMainChat marks the file as sent to the main chat and posts it now
// when no turn runs there, or with the next sweep when one does
// (tools.MainChatSending).
func (self *AskRun) SendToMainChat(ctx context.Context, attachmentId, caption, sentBy string) error {
	return self.agent.SendFileToMainChat(ctx, self.settings.Agent.ID, attachmentId, caption, sentBy)
}

// UploadLink is the address a program sends the file's bytes to
// (tools.MainChatSending); "" when the turn did not come through an address
// this server knows.
func (self *AskRun) UploadLink(attachmentId string) string {
	return self.agent.UploadLinkFrom(self.settings.Origin, attachmentId)
}

// SendToMainChat is AskRun's (tools.MainChatSending).
func (self *directRun) SendToMainChat(ctx context.Context, attachmentId, caption, sentBy string) error {
	return self.agent.SendFileToMainChat(ctx, self.agentModel.ID, attachmentId, caption, sentBy)
}

// UploadLink is AskRun's (tools.MainChatSending).
func (self *directRun) UploadLink(attachmentId string) string {
	return self.agent.UploadLinkFrom(self.origin, attachmentId)
}

// UploadLinkFrom is the full address, on the origin given, that takes one PUT
// of a waiting file's bytes, signed for that file until UploadFor has passed;
// "" without an origin or a secret to sign with.
func (self *Agent) UploadLinkFrom(origin, attachmentId string) string {
	if origin == "" {
		return ""
	}
	upload := self.ShareAttachment(uploadSigned(attachmentId), time.Now().Add(UploadFor))
	if upload == "" {
		return ""
	}
	return origin + "/api/v1/agent/attachments/" + url.PathEscape(attachmentId) + "/upload?upload=" + url.QueryEscape(upload)
}

// UploadAllowed says whether the query value lets the bytes of the file be
// sent now. It is signed over a name of its own, so an address that opens a
// file cannot be used to replace it.
func (self *Agent) UploadAllowed(attachmentId, upload string) bool {
	return self.SharedAttachment(uploadSigned(attachmentId), upload)
}

func uploadSigned(attachmentId string) string {
	return "upload\n" + attachmentId
}

// SendFileToMainChat marks one of the agent's files as sent to the person's
// main chat, with a line and who sent it, and posts it now unless a turn
// runs there; the sweep posts it once that turn ends.
func (self *Agent) SendFileToMainChat(ctx context.Context, agentId, attachmentId, caption, sentBy string) error {
	var attachment *models.AgentAttachment
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if err := tx.QueueAgentAttachmentForMainChat(agentId, attachmentId, strings.TrimSpace(caption), strings.TrimSpace(sentBy)); err != nil {
			return err
		}
		attachment, err = tx.GetAgentAttachment(attachmentId)
		return err
	}); err != nil {
		return err
	}
	if attachment == nil || attachment.MessageID == models.AttachmentMessageUpload {
		return nil
	}
	if err := self.postToMainChat(ctx, attachment); err != nil && !errors.Is(err, errTurnRunning) {
		return err
	}
	return nil
}

// postWaitingFiles posts the files a turn in the main chat held up.
func (self *Agent) postWaitingFiles(ctx context.Context) {
	var waiting []*models.AgentAttachment
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		waiting, err = tx.ListAgentAttachmentsWaitingForMainChat(mainChatWaitingBatch)
		return err
	}); err != nil {
		log.Warningf("cannot list the files waiting for a main chat: %s", err)
		return
	}
	for _, attachment := range waiting {
		if ctx.Err() != nil {
			return
		}
		if err := self.postToMainChat(ctx, attachment); err != nil && !errors.Is(err, errTurnRunning) {
			log.Warningf("cannot post file %q to its main chat: %s", attachment.ID, err)
		}
	}
}

// postToMainChat writes the file into the main chat of its agent, under the
// lock turns start with, and in the transaction that marks it posted, so it
// is posted once.
func (self *Agent) postToMainChat(ctx context.Context, attachment *models.AgentAttachment) error {
	var main *models.AgentConversation
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		main, err = scheduleConversation(tx, attachment.AgentID, "")
		return err
	}); err != nil {
		return err
	}
	callId := "call_" + security.NewULID()
	checkIn := fileSentCheckIn(attachment)
	arguments, err := json.Marshal(map[string]any{"source": "conversation", "attachment_id": attachment.ID, "caption": attachment.Caption})
	if err != nil {
		return err
	}
	answer, err := json.Marshal(SharedFileAnswer(attachment))
	if err != nil {
		return err
	}
	said := fileSentSentence(attachment)
	isPosted := false
	isWritten, err := self.whileNoTurnRuns(main.ID, func() error {
		return self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
			posted, err := tx.PostedAgentAttachmentToMainChat(attachment.ID, main.ID)
			if err != nil || !posted {
				return err
			}
			for _, message := range []*models.AgentMessage{
				{ConversationID: main.ID, Role: "user", Content: checkIn},
				{ConversationID: main.ID, Role: "assistant", ToolCalls: []models.AgentToolCall{{ID: callId, Name: "share_file", Arguments: string(arguments)}}},
				{ConversationID: main.ID, Role: "tool", ToolCallID: callId, Name: "share_file", Content: string(answer)},
				{ConversationID: main.ID, Role: "assistant", Content: said},
			} {
				if _, err := tx.AppendAgentMessage(message); err != nil {
					return err
				}
			}
			isPosted = true
			return nil
		})
	})
	if err != nil {
		return err
	}
	if !isWritten {
		return errTurnRunning
	}
	if !isPosted {
		return nil
	}
	runId := security.NewULID()
	at := time.Now()
	for sequence, event := range []Event{
		{Kind: EventAsked, Text: checkIn, Note: fileSentSurface},
		{Kind: EventToolCall, Tool: "share_file", CallID: callId, Arguments: string(arguments)},
		{Kind: EventToolResult, Tool: "share_file", CallID: callId, Note: "handed over " + attachment.Name, Text: string(answer)},
		{Kind: EventMessage, Text: said},
		{Kind: EventDone},
	} {
		event.RunID, event.ConversationID, event.Sequence, event.At = runId, main.ID, sequence, at
		self.publish(event, true)
	}
	return nil
}

// SharedFileAnswer is a file as share_file answers with it, the shape the
// dashboard and the phone draw a file from: the url is the plain path, which
// they open with the person's session.
func SharedFileAnswer(attachment *models.AgentAttachment) map[string]any {
	answer := map[string]any{
		"attachment_id": attachment.ID, "name": attachment.Name, "content_type": attachment.ContentType, "size": attachment.Size,
		"url": "/api/v1/agent/attachments/" + attachment.ID,
	}
	if attachment.Caption != "" {
		answer["caption"] = attachment.Caption
	}
	// Who sent it, which also says it was sent to the main chat: a page or
	// a PDF sent there opens in place.
	if attachment.SentBy != "" {
		answer["sent_by"] = attachment.SentBy
	}
	return answer
}

// fileSentCheckIn is the marked line a sent file is posted under: who sent
// it, and that it is not the person speaking.
func fileSentCheckIn(attachment *models.AgentAttachment) string {
	sentBy := attachment.SentBy
	if sentBy == "" {
		sentBy = "the agent"
	}
	return fmt.Sprintf("%s %s sent the person %s for their main chat; you handed it over. This is not the person speaking.", models.FileSentMarker, sentBy, attachment.Name)
}

// fileSentSentence is what the agent says with the file: the caption, or the
// file's name and who sent it. It is what the phone's notification reads.
func fileSentSentence(attachment *models.AgentAttachment) string {
	if attachment.Caption != "" {
		return attachment.Caption
	}
	if attachment.SentBy != "" {
		return fmt.Sprintf("%s, from %s.", attachment.Name, attachment.SentBy)
	}
	return attachment.Name + "."
}
