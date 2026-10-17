package apigraph

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"

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
	return self.agentAttachmentPersonFor(response, request, false)
}

// agentAttachmentPersonFor is agentAttachmentPerson, and for the two
// endpoints a chat uses -- a file sent and a file shown -- it also lets in a
// person who may use circles whose agent is off.
func (self *graph) agentAttachmentPersonFor(response http.ResponseWriter, request *http.Request, isChatEnough bool) (*models.User, *models.Agent, bool) {
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
		// A person who chats hands the files they send there to an agent of
		// their own that need not be on: it is where their uploads are kept
		// until a message claims them.
		if isChatEnough && permissions.Has(models.PermissionCircleUse) {
			found, err = tx.EnsureAgent(user.ID)
			return err
		}
		if !permissions.Has(models.PermissionAgentUse) {
			return api.ErrPermissionDenied
		}
		found, err = tx.GetAgentByUser(user.ID)
		if err == nil && found != nil && !found.Active() {
			found = nil
		}
		return err
	}); err != nil || found == nil {
		writeJSON(response, http.StatusForbidden, map[string]string{"error": "you have no agent to hand a file to"})
		return nil, nil, false
	}
	return user, found, true
}

// canReadChatAttachment says whether the file came with a chat message the
// person can read: one written while they were in the chat. A file is the
// uploader's own, and the others in the chat are shown it with the message
// it was sent with.
func (self *graph) canReadChatAttachment(attachment *models.AgentAttachment, user *models.User) (bool, error) {
	if attachment.MessageID == "" {
		return false, nil
	}
	var isReadable bool
	err := self.database.Transaction(func(tx db.Transaction) error {
		message, err := tx.GetChatMessage(attachment.MessageID)
		if err != nil || message == nil {
			return err
		}
		chat, err := tx.GetChat(message.ChatID)
		if err != nil || chat == nil {
			return err
		}
		member := chat.Member(user.ID)
		isReadable = member != nil && isReadableByMember(message, member)
		return nil
	})
	return isReadable, err
}

func (self *graph) agentAttachmentsView(response http.ResponseWriter, request *http.Request) {
	user, found, ok := self.agentAttachmentPersonFor(response, request, true)
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
	impersonator, err := self.impersonatorOf(request, user)
	if err != nil {
		writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "not signed in"})
		return
	}
	ctx := db.ContextWithAuditPrincipal(request.Context(), self.auditPrincipal(request, user, impersonator))
	result := &AgentAttachmentUploadResult{Attachments: []*models.AgentAttachment{}}
	isTaxDocument := request.URL.Query().Get(api.AgentAttachmentTaxDocumentParameter) == "true"
	// A file sent in a chat is kept for a person whose agent is off, but what
	// is read out of it is meant for the model, and there is none for them to
	// spend.
	isAgentOn := found.Active()
	if isAgentOn {
		if err := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
			permissions, err := tx.EffectivePermissions(user.ID)
			if err != nil {
				return err
			}
			isAgentOn = permissions.Has(models.PermissionAgentUse)
			return nil
		}); err != nil {
			writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "the file could not be stored"})
			return
		}
	}
	for _, upload := range uploads {
		var created *models.AgentAttachment
		// Read out of the file before the transaction opens rather than
		// inside it. It walks the whole upload and it needs nothing from
		// the database, so doing it under the transaction held one open
		// for the length of a file.
		contentType := contentTypeOf(upload.Filename, upload.ContentType, upload.Content)
		// A paystub's or a tax form's text holds the person's taxpayer
		// number, and is read from the file each time it is needed.
		text := ""
		if !isTaxDocument && isAgentOn {
			text = agent.ExtractAttachmentText(ctx, upload.Filename, upload.ContentType, upload.Content)
		}
		if err := self.database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
			created, err = tx.CreateAgentAttachment(&models.AgentAttachment{
				AgentID:       found.ID,
				Name:          upload.Filename,
				ContentType:   contentType,
				Size:          int64(len(upload.Content)),
				Text:          text,
				IsTaxDocument: isTaxDocument,
			})
			if err != nil {
				return err
			}
			// The bytes go before the row is committed, not after: a row
			// that outlives its bytes is an attachment nobody can open,
			// while bytes that outlive a rolled back row are reclaimed by
			// retention. The id is the row's, so this cannot move out.
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
	// A PDF by its bytes before anything else: a mail client sends one as
	// application/octet-stream under any name.
	if corrected := agent.AttachmentContentType(declared, content); corrected != declared {
		return corrected
	}
	// The name's extension first — a command line sends no type — then
	// the bytes.
	if byName := mime.TypeByExtension(strings.ToLower(filepath.Ext(name))); byName != "" {
		return byName
	}
	return http.DetectContentType(content)
}

func (self *graph) agentAttachmentView(response http.ResponseWriter, request *http.Request) {
	attachmentId := mux.Vars(request)["attachmentId"]
	// A shared file opens without a sign-in: the address itself is signed,
	// names this one file, and expires. A chat app hands it to a browser
	// that has no session here, and so does the drawer framed into another
	// site, which cannot put a header on a picture or a framed page.
	if share := request.URL.Query().Get("share"); share != "" && (request.Method == http.MethodGet || request.Method == http.MethodHead) {
		worker := self.agentWorker()
		if worker == nil || !worker.SharedAttachment(attachmentId, share) {
			writeJSON(response, http.StatusNotFound, map[string]string{"error": "the link is not good, or no longer"})
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
		self.serveAgentAttachment(response, request, attachment)
		return
	}
	user, found, ok := self.agentAttachmentPersonFor(response, request, true)
	if !ok {
		return
	}
	attachment, ok := self.agentAttachmentRow(response, attachmentId)
	if !ok {
		return
	}
	if attachment != nil && attachment.AgentID != found.ID {
		isReadable, err := self.canReadChatAttachment(attachment, user)
		if err != nil {
			writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "failed to read the file"})
			return
		}
		if !isReadable {
			attachment = nil
		}
	}
	if attachment == nil {
		writeJSON(response, http.StatusNotFound, map[string]string{"error": "no such file"})
		return
	}
	self.serveAgentAttachment(response, request, attachment)
}

// agentAttachmentRow is the attachment's row, nil when there is none; the
// second value is false when the database failed and the answer is sent.
func (self *graph) agentAttachmentRow(response http.ResponseWriter, attachmentId string) (*models.AgentAttachment, bool) {
	var attachment *models.AgentAttachment
	if err := self.database.Transaction(func(tx db.Transaction) (err error) {
		attachment, err = tx.GetAgentAttachment(attachmentId)
		return err
	}); err != nil {
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "failed to read the file"})
		return nil, false
	}
	return attachment, true
}

// serveAgentAttachment writes the file, as a page, a picture or a
// download by what it is.
func (self *graph) serveAgentAttachment(response http.ResponseWriter, request *http.Request, attachment *models.AgentAttachment) {
	content, err := self.storage.GetFile(request.Context(), attachment.ID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeJSON(response, http.StatusNotFound, map[string]string{"error": "the file is gone"})
			return
		}
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "failed to read the file"})
		return
	}
	writeAgentAttachment(response, request, attachment, content)
}

// writeAgentAttachment writes a file already read, whole or the byte range
// asked for. A phone's video player asks for ranges, and will not play from
// a server that answers every one with the whole file.
func writeAgentAttachment(response http.ResponseWriter, request *http.Request, attachment *models.AgentAttachment, content []byte) {
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
	case agent.IsImageAttachment(contentType), agent.IsMediaAttachment(contentType):
		// A picture, a video or a sound plays in the page; a browser
		// runs nothing in those.
		disposition = "inline"
	case attachment.MessageID == "artifact":
		disposition = "inline"
		response.Header().Set("Content-Security-Policy", artifactPolicy(request.Host))
	default:
		contentType = "application/octet-stream"
	}
	response.Header().Set("Content-Type", contentType)
	response.Header().Set("Content-Disposition", fmt.Sprintf("%s; filename=%q", disposition, attachment.Name))
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.Header().Set("Cache-Control", "private, max-age=3600")
	// The length, a range, HEAD and If-Modified-Since. The name is left out
	// so the type set above is the one sent.
	http.ServeContent(response, request, "", attachment.CreatedAt, bytes.NewReader(content))
}

// hostName is what a host header may look like to be written into a policy:
// a name or an address, with a port.
var hostName = regexp.MustCompile(`^[A-Za-z0-9.:\[\]-]+$`)

// artifactPolicy is the sandbox an artifact is shown in: an origin that is
// nobody's, no request to anywhere, so a script in it can draw and reach
// nothing else — except this server's /assets/, where the chart library
// and the dashboard's look for a page live. The host is the request's
// own, without a scheme, which the policy takes as "the page's or safer";
// anything that is not a host name is left out rather than written into
// a header.
func artifactPolicy(host string) string {
	assets := ""
	if hostName.MatchString(host) {
		assets = " " + host + "/assets/"
	}
	return "sandbox allow-scripts; default-src 'none'; script-src 'unsafe-inline'" + assets + "; style-src 'unsafe-inline'" + assets + "; img-src data: blob:; font-src data:" + assets + "; media-src data:"
}

// ShareAgentAttachmentArguments name one file.
type ShareAgentAttachmentArguments struct {
	AttachmentID string `json:"attachmentId"`
}

// ShareAgentAttachment signs an address for one of the caller's own files.
//
// The drawer framed into another site has no session cookie of this origin,
// and a picture or a framed page is fetched by the browser itself, which
// cannot be told to send a header. The obvious answer -- put the person's
// token in the address -- writes their token into this server's access log,
// into the log of anything in front of it, and into whatever they copy the
// link into. This is signed for one file and expires instead, so the worst
// it can leak is that file.
func (self *graph) ShareAgentAttachment(ctx context.Context, arguments ShareAgentAttachmentArguments) (string, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return "", err
	}
	worker := self.agentWorker()
	if worker == nil {
		return "", agent.ErrUnavailable
	}
	var attachment *models.AgentAttachment
	if err := self.database.Transaction(func(tx db.Transaction) (err error) {
		attachment, err = tx.GetAgentAttachment(arguments.AttachmentID)
		return err
	}); err != nil {
		return "", err
	}
	if attachment == nil || attachment.AgentID != found.ID {
		return "", api.ErrNotFound
	}
	share := worker.ShareAttachment(attachment.ID, time.Now().Add(agent.ShareToOpenFor))
	if share == "" {
		return "", agent.ErrUnavailable
	}
	return api.PathAgentAttachments + "/" + attachment.ID + "?share=" + url.QueryEscape(share), nil
}
