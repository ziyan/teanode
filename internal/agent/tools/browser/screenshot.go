package browser

import (
	"context"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// screenshotMessage marks a screenshot handed to the person as a file the
// agent handed over, the way share_file marks one: the conversation shows
// it under the tool line.
const screenshotMessage = "shared"

// screenshotResult is a screenshot as a picture: the model is shown it, and,
// when asked to show it, the person is handed it in the conversation. It was
// once the picture's bytes written out as text, which a model reads as a
// long string of letters and never as a picture, at the cost of the tokens
// of a small book.
func screenshotResult(ctx context.Context, image []byte, isShown bool) (*tools.Result, error) {
	answer := map[string]any{"content_type": "image/png", "size": len(image), "picture": "the screenshot follows for you to look at"}
	if isShown {
		if attachment := handScreenshot(ctx, image); attachment != nil {
			answer["given_to_the_person"] = true
			answer["attachment_id"] = attachment.ID
		} else {
			answer["given_to_the_person"] = false
		}
	}
	result, err := tools.JSONResult(answer)
	if err != nil {
		return nil, err
	}
	result.Images = []llm.ContentPart{{Type: "image", MediaType: "image/png", Data: image}}
	result.Untrusted = true
	result.Note = "took a screenshot"
	return result, nil
}

// handScreenshot keeps a screenshot as a file of the conversation, or
// nothing where there is no conversation to keep it in.
func handScreenshot(ctx context.Context, image []byte) *models.AgentAttachment {
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil
	}
	store := run.Storage()
	conversationId := tools.ConversationIDOf(run)
	if store == nil || run.Agent() == nil || conversationId == "" {
		return nil
	}
	var attachment *models.AgentAttachment
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		attachment, err = tx.CreateAgentAttachment(&models.AgentAttachment{
			AgentID:        run.Agent().ID,
			ConversationID: conversationId,
			MessageID:      screenshotMessage,
			Name:           "screenshot.png",
			ContentType:    "image/png",
			Size:           int64(len(image)),
		})
		if err != nil {
			return err
		}
		return store.PutFile(ctx, attachment.ID, image)
	}); err != nil {
		return nil
	}
	return attachment
}
