package apigraph

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// The file behind a document a knowledge source indexed.
//
// A picture or a file a record came with is a document like any other --
// searchable, quotable, cited as evidence on a page -- and until this
// there was no way for a browser to see the thing itself. The bytes are
// in object storage under the document's key, which is where they have
// been since the source read them; this hands them to the person whose
// documents they are and to nobody else.

// agentDocumentFileView serves one indexed document's bytes.
func (self *graph) agentDocumentFileView(response http.ResponseWriter, request *http.Request) {
	// The same check the conversation's files go through: signed in, may
	// use an agent, and has one.
	_, found, ok := self.agentAttachmentPerson(response, request)
	if !ok {
		return
	}
	documentId := strings.TrimSpace(mux.Vars(request)["documentId"])
	var document *models.AgentDocument
	if err := self.database.TransactionContext(request.Context(), func(tx db.Transaction) (err error) {
		// By the caller's own agent, so that a document identifier taken
		// from somewhere else finds nothing rather than somebody else's
		// screenshot.
		document, err = tx.GetAgentDocument(found.ID, documentId)
		return err
	}); err != nil {
		log.Errorf("cannot read an indexed document: %s", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "failed to read the file"})
		return
	}
	if document == nil || document.AgentID != found.ID {
		writeJSON(response, http.StatusNotFound, map[string]string{"error": "no such file"})
		return
	}
	// A document whose bytes were never kept -- a file read as text, or
	// one whose source could not be reached when it was filed -- has
	// nothing to serve. Said plainly rather than as a failure: the next
	// pass of its source may fill the key in.
	if document.StorageKey == "" {
		writeJSON(response, http.StatusNotFound, map[string]string{"error": "that document has no file kept for it"})
		return
	}
	content, err := agent.DocumentBytes(request.Context(), self.storage, document)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeJSON(response, http.StatusNotFound, map[string]string{"error": "the file is gone"})
			return
		}
		log.Errorf("cannot read the file of %s: %s", document.ID, err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "failed to read the file"})
		return
	}

	contentType := document.ContentType()
	// A picture is shown in the page. Anything else is handed over as a
	// file, under a type that says nothing about how to run it, so that a
	// browser never runs what a person's archive happened to contain.
	disposition := "attachment"
	if agent.IsImageAttachment(contentType) {
		disposition = "inline"
		// It is a picture and nothing else, whatever it turns out to
		// contain: nothing here loads anything, and nothing here runs.
		response.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	} else {
		contentType = "application/octet-stream"
	}
	response.Header().Set("Content-Type", contentType)
	response.Header().Set("Content-Length", strconv.Itoa(len(content)))
	response.Header().Set("Content-Disposition", fmt.Sprintf("%s; filename=%q", disposition, documentFileName(document)))
	response.Header().Set("X-Content-Type-Options", "nosniff")
	// Private, because it is one person's archive. Held for an hour,
	// because the bytes are keyed by their own hash and cannot change
	// under a reader.
	response.Header().Set("Cache-Control", "private, max-age=3600")
	response.WriteHeader(http.StatusOK)
	if _, err := response.Write(content); err != nil {
		log.Debugf("the file of %s could not be written: %s", document.ID, err)
	}
}

// documentFileName is what the file is called when it is saved: the
// document's title, which for an attachment is the name the record gave
// it, and the identifier where a source recorded no name.
func documentFileName(document *models.AgentDocument) string {
	name := strings.TrimSpace(document.Title)
	if name == "" {
		name = document.ID
	}
	// A quotation mark or a newline in the name would break the header
	// apart, and a path would suggest a directory to save into.
	name = strings.NewReplacer(`"`, "", "\\", "", "\r", "", "\n", "", "/", "-").Replace(name)
	if name == "" {
		return document.ID
	}
	return name
}
