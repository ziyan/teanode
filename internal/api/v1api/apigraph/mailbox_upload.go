package apigraph

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

// Attaching files to a draft: one request carries a whole selection as
// multipart/form-data, and the draft is rewritten once with the previous
// parts and the new ones. The reply is the draft as it now stands, so the
// page can keep parts by the indexes the server gave them rather than by
// indexes it guessed.

// DraftUploadResult is what an upload answers with.
type DraftUploadResult struct {
	ItemID      string        `json:"itemId"`
	Attachments []*Attachment `json:"attachments"`
}

func (self *graph) draftAttachmentsView(response http.ResponseWriter, request *http.Request) {
	username := api.UsernameFromRequest(request)
	var user *models.User
	if username != "" && username != config.LocalUsername {
		found, err := self.database.GetUserByUsername(username)
		if err != nil {
			http.Error(response, "failed to read the account", http.StatusInternalServerError)
			return
		}
		if found == nil || found.Disabled() {
			username = ""
		}
		user = found
	}
	if username == "" {
		http.Error(response, "not signed in", http.StatusUnauthorized)
		return
	}

	// The files, read before the transaction so that a slow upload holds
	// no database connection. Bounded by the message-size limit: a file
	// that could not be sent cannot be attached either.
	limit := self.config.Current().SMTP.MaxMessageSize.Bytes()
	uploads, err := readUploads(request, limit)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errTooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		writeJSON(response, status, map[string]string{"error": err.Error()})
		return
	}

	variables := mux.Vars(request)
	ctx := request.Context()
	ctx = api.ContextWithRequest(ctx, request)
	ctx = api.ContextWithAuthenticatedUsername(ctx, username)
	ctx = db.ContextWithAuditPrincipal(ctx, auditPrincipal(request, user))

	var result *DraftUploadResult
	err = self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		ctx := api.ContextWithTransaction(ctx, tx)
		principal, err := self.resolvePrincipal(tx, username, user)
		if err != nil {
			return err
		}
		ctx = api.ContextWithPrincipal(ctx, principal)

		var mailbox *models.Mailbox
		parameters := &MailboxMessageParameters{}
		if itemId := variables["itemId"]; itemId != "" {
			// Continuing a draft: its fields and every part it holds, then
			// the new files after them.
			mailbox, err = self.requireDraftOwner(ctx, itemId)
			if err != nil {
				return err
			}
			previous, err := self.readDraft(ctx, mailbox, itemId)
			if err != nil {
				return err
			}
			parameters = &MailboxMessageParameters{
				From:          previous.From,
				FromName:      previous.FromName,
				To:            previous.To,
				Cc:            previous.Cc,
				Bcc:           previous.Bcc,
				Subject:       previous.Subject,
				HTMLContent:   previous.HTML,
				TextContent:   previous.Text,
				ReplyToItemID: previous.ReplyToItemID,
				ForwardItemID: previous.ForwardItemID,
				DraftItemID:   itemId,
			}
			for _, part := range previous.Parts {
				if !part.Inline {
					parameters.KeepAttachments = append(parameters.KeepAttachments, part.Index)
				}
			}
		} else {
			// The first draft of a message, made around the files; the
			// words come with the next save.
			mailbox, err = self.requireMailbox(ctx, models.PermissionMailWrite, variables["mailboxId"])
			if err != nil {
				return err
			}
		}
		if parameters.From == "" {
			if len(mailbox.Addresses) == 0 {
				return fmt.Errorf("%w: the mailbox has no address to send from", api.ErrInvalidArguments)
			}
			parameters.From = mailbox.Addresses[0].Address
		}
		item, err := self.saveDraft(ctx, tx, mailbox, parameters, uploads)
		if err != nil {
			return err
		}
		saved, err := self.readDraft(ctx, mailbox, item.ID)
		if err != nil {
			return err
		}
		result = &DraftUploadResult{ItemID: item.ID, Attachments: saved.Parts}
		if result.Attachments == nil {
			result.Attachments = []*Attachment{}
		}
		return nil
	})
	if err != nil {
		switch {
		case errors.Is(err, api.ErrNotFound):
			writeJSON(response, http.StatusNotFound, map[string]string{"error": "no such draft"})
		case errors.Is(err, api.ErrInvalidArguments):
			writeJSON(response, http.StatusBadRequest, map[string]string{"error": err.Error()})
		default:
			log.Errorf("attaching files to a draft for %q failed: %s", username, err)
			writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "the files could not be attached"})
		}
		return
	}
	writeJSON(response, http.StatusOK, result)
}

var errTooLarge = errors.New("the files come to more than a message may be")

// readUploads reads every "file" part of a multipart body, up to the limit
// across all of them.
func readUploads(request *http.Request, limit uint64) ([]*mailparse.Attachment, error) {
	reader, err := request.MultipartReader()
	if err != nil {
		return nil, fmt.Errorf("expected a multipart body: %w", err)
	}
	var uploads []*mailparse.Attachment
	var total uint64
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("could not read the upload: %w", err)
		}
		if part.FormName() != "file" || part.FileName() == "" {
			_ = part.Close()
			continue
		}
		remaining := int64(-1)
		if limit > 0 {
			remaining = int64(limit-total) + 1
		}
		var content []byte
		if remaining >= 0 {
			content, err = io.ReadAll(io.LimitReader(part, remaining))
		} else {
			content, err = io.ReadAll(part)
		}
		_ = part.Close()
		if err != nil {
			return nil, fmt.Errorf("could not read %q: %w", part.FileName(), err)
		}
		total += uint64(len(content))
		if limit > 0 && total > limit {
			return nil, errTooLarge
		}
		contentType := strings.TrimSpace(part.Header.Get("Content-Type"))
		if contentType == "application/octet-stream" {
			contentType = ""
		}
		uploads = append(uploads, &mailparse.Attachment{
			Filename:    part.FileName(),
			ContentType: contentType,
			Content:     content,
		})
	}
	if len(uploads) == 0 {
		return nil, errors.New("no file was sent")
	}
	return uploads, nil
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}
