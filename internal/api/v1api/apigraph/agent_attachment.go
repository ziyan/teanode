package apigraph

import (
	"errors"
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// Files for a conversation with the agent go up as a multipart body, the
// way a draft's do, and come back down by id to their owner. An upload is
// bound to nobody's turn until a turn names it; one that never is goes
// with the sweep.

// AgentAttachmentUploadResult is what an upload answers with.
type AgentAttachmentUploadResult struct {
	Attachments []*models.AgentAttachment `json:"attachments"`
}

// agentAttachmentPerson is the signed-in person and their agent, for the
// two file endpoints, settled before a byte of the body is read.
func (self *graph) agentAttachmentPerson(response http.ResponseWriter, request *http.Request) (*models.User, *models.Agent, bool) {
	username := api.UsernameFromRequest(request)
	var user *models.User
	if username != "" && username != config.LocalUsername {
		found, err := self.database.GetUserByUsername(username)
		if err != nil {
			http.Error(response, "failed to read the account", http.StatusInternalServerError)
			return nil, nil, false
		}
		if found == nil || found.Disabled() {
			username = ""
		}
		user = found
	}
	if username == "" || user == nil {
		writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "not signed in"})
		return nil, nil, false
	}
	var found *models.Agent
	if err := self.database.Transaction(func(tx db.Transaction) error {
		permissions, err := tx.EffectivePermissions(user.ID)
		if err != nil {
			return err
		}
		if !permissions.Has(models.PermissionAgentUse) {
			return api.ErrPermissionDenied
		}
		found, err = tx.GetAgentByUser(user.ID)
		return err
	}); err != nil || found == nil || !found.Active() {
		writeJSON(response, http.StatusForbidden, map[string]string{"error": "you have no agent to hand a file to"})
		return nil, nil, false
	}
	return user, found, true
}

func (self *graph) agentAttachmentsView(response http.ResponseWriter, request *http.Request) {
	user, found, ok := self.agentAttachmentPerson(response, request)
	if !ok {
		return
	}
	limit := self.config.Current().Agent.Limits.MaxAttachmentBytes.Bytes()
	if limit > 0 {
		request.Body = http.MaxBytesReader(response, request.Body, int64(limit)+multipartOverhead)
	}
	uploads, err := readUploads(request, limit)
	if err != nil {
		status := http.StatusBadRequest
		var tooLarge *http.MaxBytesError
		if errors.Is(err, errTooLarge) || errors.As(err, &tooLarge) {
			status = http.StatusRequestEntityTooLarge
			err = fmt.Errorf("the files come to more than %s", self.config.Current().Agent.Limits.MaxAttachmentBytes)
		}
		writeJSON(response, status, map[string]string{"error": err.Error()})
		return
	}
	ctx := request.Context()
	result := &AgentAttachmentUploadResult{Attachments: []*models.AgentAttachment{}}
	for _, upload := range uploads {
		var created *models.AgentAttachment
		if err := self.database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
			created, err = tx.CreateAgentAttachment(&models.AgentAttachment{
				AgentID:     found.ID,
				Name:        upload.Filename,
				ContentType: contentTypeOf(upload.Filename, upload.ContentType, upload.Content),
				Size:        int64(len(upload.Content)),
				Text:        agent.ExtractAttachmentText(upload.Filename, upload.ContentType, upload.Content),
			})
			if err != nil {
				return err
			}
			return self.storage.PutFile(ctx, created.ID, upload.Content)
		}); err != nil {
			log.Errorf("storing a file for the agent of %q failed: %s", user.Username, err)
			writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "the file could not be stored"})
			return
		}
		result.Attachments = append(result.Attachments, created)
	}
	writeJSON(response, http.StatusOK, result)
}

// contentTypeOf is the type the browser said, or one sniffed from the
// bytes when it said nothing.
func contentTypeOf(name, declared string, content []byte) string {
	declared = strings.TrimSpace(declared)
	if declared != "" && declared != "application/octet-stream" {
		return declared
	}
	// The name's extension first — a command line sends no type — then
	// the bytes.
	if byName := mime.TypeByExtension(strings.ToLower(filepath.Ext(name))); byName != "" {
		return byName
	}
	return http.DetectContentType(content)
}

func (self *graph) agentAttachmentView(response http.ResponseWriter, request *http.Request) {
	_, found, ok := self.agentAttachmentPerson(response, request)
	if !ok {
		return
	}
	attachmentId := mux.Vars(request)["attachmentId"]
	var attachment *models.AgentAttachment
	if err := self.database.Transaction(func(tx db.Transaction) (err error) {
		attachment, err = tx.GetAgentAttachment(attachmentId)
		return err
	}); err != nil {
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "failed to read the file"})
		return
	}
	if attachment == nil || attachment.AgentID != found.ID {
		writeJSON(response, http.StatusNotFound, map[string]string{"error": "no such file"})
		return
	}
	content, err := self.storage.GetFile(request.Context(), attachment.ID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeJSON(response, http.StatusNotFound, map[string]string{"error": "the file is gone"})
			return
		}
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "failed to read the file"})
		return
	}
	contentType := attachment.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	// A picture is shown in the page. An artifact — a page, a drawing, a
	// document the agent made — is shown too, but in a sandbox of its own:
	// an origin that is nobody's, no request to anywhere, so a script in it
	// can draw a chart and reach nothing else. Anything else is handed over
	// as a file, so a browser never runs what somebody uploaded.
	disposition := "attachment"
	switch {
	case agent.IsImageAttachment(contentType):
		disposition = "inline"
	case attachment.MessageID == "artifact":
		disposition = "inline"
		response.Header().Set("Content-Security-Policy", "sandbox allow-scripts; default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data: blob:; font-src data:; media-src data:")
	default:
		contentType = "application/octet-stream"
	}
	response.Header().Set("Content-Type", contentType)
	response.Header().Set("Content-Length", strconv.Itoa(len(content)))
	response.Header().Set("Content-Disposition", fmt.Sprintf("%s; filename=%q", disposition, attachment.Name))
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.Header().Set("Cache-Control", "private, max-age=3600")
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write(content)
}
