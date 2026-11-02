package apigraph

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gorilla/mux"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// A program sending the person a file for their main chat uploads its bytes
// here, to the address share_file answered with: one PUT, signed for that one
// file and expiring, with no session, since a program over MCP has none and
// its token works only at the tools endpoint. The address grants nothing but
// the bytes of a file that is waiting for them.

// agentAttachmentUploadView takes the bytes of a file waiting for its upload,
// keeps them, and posts the file to the main chat.
func (self *graph) agentAttachmentUploadView(response http.ResponseWriter, request *http.Request) {
	attachmentId := mux.Vars(request)["attachmentId"]
	worker := self.agentWorker()
	if worker == nil || !worker.UploadAllowed(attachmentId, request.URL.Query().Get("upload")) {
		writeJSON(response, http.StatusNotFound, map[string]string{"error": "the upload address is not good, or no longer"})
		return
	}
	attachment, ok := self.agentAttachmentRow(response, attachmentId)
	if !ok {
		return
	}
	if attachment == nil {
		writeJSON(response, http.StatusNotFound, map[string]string{"error": "no such file"})
		return
	}
	if attachment.MessageID != models.AttachmentMessageUpload {
		writeJSON(response, http.StatusConflict, map[string]string{"error": "the file was uploaded already"})
		return
	}
	limit := self.config.Current().Agent.Limits.EffectiveMaxSentFileBytes()
	content, err := io.ReadAll(http.MaxBytesReader(response, request.Body, int64(limit)))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeJSON(response, http.StatusRequestEntityTooLarge, map[string]string{"error": fmt.Sprintf("the file is more than %d bytes", limit)})
			return
		}
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "the file could not be read"})
		return
	}
	if len(content) == 0 {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "the file is empty"})
		return
	}
	// The type the program named when it asked for the address, unless it
	// named none that says anything: then the request's, the name's and the
	// bytes', as for any upload.
	declared := attachment.ContentType
	if declared == "" || declared == "application/octet-stream" {
		declared = request.Header.Get("Content-Type")
	}
	contentType := contentTypeOf(attachment.Name, declared, content)
	ctx := request.Context()
	// The bytes go before the row says they are there: a row that says so
	// without them is a file nobody can open.
	if err := self.storage.PutFile(ctx, attachment.ID, content); err != nil {
		log.Errorf("storing the upload of file %q failed: %s", attachment.ID, err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "the file could not be stored"})
		return
	}
	var isFinished bool
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		isFinished, err = tx.FinishAgentAttachmentUpload(attachment.ID, contentType, int64(len(content)))
		return err
	}); err != nil {
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "the file could not be stored"})
		return
	}
	if !isFinished {
		writeJSON(response, http.StatusConflict, map[string]string{"error": "the file was uploaded already"})
		return
	}
	if err := worker.SendFileToMainChat(ctx, attachment.AgentID, attachment.ID, attachment.Caption, attachment.SentBy); err != nil {
		log.Errorf("posting file %q to the main chat failed: %s", attachment.ID, err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "the file was kept, and could not be posted to the main chat yet"})
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"attachmentId": attachment.ID, "name": attachment.Name, "contentType": contentType, "size": len(content),
		"sentTo": "the person's main chat",
	})
}

// SendAgentFileToMainChatArguments name the file to send, and the line to
// send with it.
type SendAgentFileToMainChatArguments struct {
	Name        string `json:"name"`
	ContentType string `json:"contentType" graphapi:"nullable"`
	Caption     string `json:"caption" graphapi:"nullable"`
}

// AgentFileUpload is a file waiting for its bytes, and where to send them.
type AgentFileUpload struct {
	AttachmentID string `json:"attachmentId"`
	Name         string `json:"name"`
	// UploadURL takes one PUT of the bytes, signed for this file, within an
	// hour of being made.
	UploadURL string `json:"uploadUrl"`
	// MaxBytes is the most the file may be.
	MaxBytes int64 `json:"maxBytes"`
}

// SendAgentFileToMainChatSentBy is who a file the person sends themselves
// is from, as the main chat says.
const SendAgentFileToMainChatSentBy = "you"

func (self *graph) SendAgentFileToMainChat(ctx context.Context, arguments SendAgentFileToMainChatArguments) (*AgentFileUpload, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	name := strings.TrimSpace(arguments.Name)
	if at := strings.LastIndexAny(name, `/\`); at >= 0 {
		name = name[at+1:]
	}
	if name == "" || name == "." || name == ".." {
		return nil, fmt.Errorf("%w: the file's name is needed", api.ErrInvalidArguments)
	}
	contentType := strings.TrimSpace(arguments.ContentType)
	if contentType == "" {
		contentType = mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	origin := ""
	if request := api.ContextRequest(ctx); request != nil {
		origin = self.originOf(request)
	}
	var attachment *models.AgentAttachment
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		attachment, err = tx.CreateAgentAttachment(&models.AgentAttachment{
			AgentID: found.ID, MessageID: models.AttachmentMessageUpload, Name: name, ContentType: contentType,
		})
		return err
	}); err != nil {
		return nil, err
	}
	link := worker.UploadLinkFrom(origin, attachment.ID)
	if link == "" {
		return nil, agent.ErrUnavailable
	}
	if err := worker.SendFileToMainChat(ctx, found.ID, attachment.ID, arguments.Caption, SendAgentFileToMainChatSentBy); err != nil {
		return nil, err
	}
	return &AgentFileUpload{
		AttachmentID: attachment.ID, Name: attachment.Name, UploadURL: link,
		MaxBytes: int64(self.config.Current().Agent.Limits.EffectiveMaxSentFileBytes()),
	}, nil
}
