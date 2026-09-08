package apimedia

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/bimi"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// The logo this server publishes for one of its own domains.
//
// A BIMI record names an address, and the file at it has to be reachable over
// HTTPS. For somebody running a mail server and no web server, that address is
// the hardest part of the whole exercise — so this server hosts it. The
// address may be on any name; it does not have to be the domain's own, and
// this one is on the mail host, which already has a certificate.
//
// An SVG is a document rather than a picture, and one served from this
// server's name would run with this server's origin. The media store next door
// refuses SVG outright for that reason. What makes this different is not
// trust in the operator but the check: a BIMI logo must satisfy SVG Tiny
// Portable/Secure, which forbids script, event handlers, external references,
// embedded photographs and animation — so a file that passes the check the
// specification demands has nothing in it that runs. The validation is not a
// nicety on top of the feature; it is what makes hosting the file safe. It is
// in internal/bimi/logo.go, with tests, and this is its only caller that
// stores anything.

// logoUploadView accepts a logo for a domain, checks it, and stores it.
func (self *media) logoUploadView(response http.ResponseWriter, request *http.Request) {
	if api.UsernameFromRequest(request) == "" && self.claimed() {
		http.Error(response, "not logged in", http.StatusUnauthorized)
		return
	}
	domainId := strings.TrimSpace(mux.Vars(request)["domainId"])

	// Read no more than a mark can be, before anything is allocated: a caller
	// cannot make this hold a gigabyte by lying about the length.
	request.Body = http.MaxBytesReader(response, request.Body, bimi.MaximumLogoSize+4096)
	if err := request.ParseMultipartForm(bimi.MaximumLogoSize + 4096); err != nil {
		http.Error(response, "the upload is too large or malformed", http.StatusBadRequest)
		return
	}

	var domain *models.Domain
	if err := self.database.Transaction(func(tx db.Transaction) error {
		found, err := tx.GetDomain(domainId)
		domain = found
		return err
	}); err != nil {
		http.Error(response, "cannot read the domain", http.StatusInternalServerError)
		return
	}
	if domain == nil {
		http.Error(response, "no such domain", http.StatusBadRequest)
		return
	}

	file, header, err := request.FormFile("file")
	if err != nil {
		http.Error(response, "no file", http.StatusBadRequest)
		return
	}
	defer func() {
		_ = file.Close()
	}()

	content, err := io.ReadAll(io.LimitReader(file, bimi.MaximumLogoSize+1))
	if err != nil {
		http.Error(response, "the upload could not be read", http.StatusBadRequest)
		return
	}

	// The message says which rule refused it, because a receiver will refuse
	// the same file silently and the sender will never learn why.
	logo, err := bimi.ValidateLogo(content)
	if err != nil {
		writeJSON(response, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}

	publication := &db.BimiPublication{
		DomainID: domain.ID,
		FileID:   config.NewID(),
		Filename: filename(header.Filename),
		Title:    logo.Title,
	}

	// Bytes first, for the reason the media store gives: a row pointing at
	// bytes that are not there answers 404 for ever, and bytes with no row
	// cost only the space.
	if err := self.storage.PutFile(request.Context(), publication.FileID, content); err != nil {
		log.Errorf("failed to store the logo of %q: %s", domain.Domain, err)
		http.Error(response, "the file could not be stored", http.StatusInternalServerError)
		return
	}

	previous := ""
	if err := self.database.Transaction(func(tx db.Transaction) error {
		if existing, err := tx.GetBimiPublication(domain.ID); err != nil {
			return err
		} else if existing != nil {
			previous = existing.FileID
			publication.CreatedAt = existing.CreatedAt
		}
		return tx.SaveBimiPublication(publication)
	}); err != nil {
		log.Errorf("failed to record the logo of %q: %s", domain.Domain, err)
		http.Error(response, "the file could not be stored", http.StatusInternalServerError)
		return
	}
	// The one it replaced. Its address is in nobody's DNS record any more,
	// and leaving it would keep serving the old mark to whoever asked.
	removeLogoFile(request.Context(), self.storage, previous)

	log.Noticef("%s published a logo for %q", api.UsernameFromRequest(request), domain.Domain)
	writeJSON(response, http.StatusOK, map[string]string{
		"fileId":   publication.FileID,
		"filename": publication.Filename,
		"title":    publication.Title,
		"url":      api.BimiLogoPath(publication.FileID),
	})
}

// domainLogoView serves the same file to the dashboard, so the operator can
// see what they published. Behind a session, and by the domain's name rather
// than the file's, because the page knows which domain it is showing and
// should not have to know which file that means today.
func (self *media) domainLogoView(response http.ResponseWriter, request *http.Request) {
	if api.UsernameFromRequest(request) == "" && self.claimed() {
		http.Error(response, "not logged in", http.StatusUnauthorized)
		return
	}
	domainId := strings.TrimSpace(mux.Vars(request)["domainId"])
	var publication *db.BimiPublication
	if err := self.database.Transaction(func(tx db.Transaction) error {
		found, err := tx.GetBimiPublication(domainId)
		publication = found
		return err
	}); err != nil {
		log.Errorf("failed to look up the logo of %q: %s", domainId, err)
		http.Error(response, "not found", http.StatusNotFound)
		return
	}
	if publication == nil {
		http.Error(response, "no logo", http.StatusNotFound)
		return
	}
	self.serveLogo(response, request, publication)
}

// logoView serves it, to whoever followed the DNS record.
//
// Public, like the media file next door and for the same reason: what fetches
// this is a receiving mail system, which has no session. There is nothing
// personal in it — it is a mark somebody published on purpose — and the
// address carries no identifier but the file's own.
func (self *media) logoView(response http.ResponseWriter, request *http.Request) {
	fileId := strings.TrimSpace(mux.Vars(request)["fileId"])
	var publication *db.BimiPublication
	if err := self.database.Transaction(func(tx db.Transaction) error {
		found, err := tx.GetBimiPublicationByFile(fileId)
		publication = found
		return err
	}); err != nil {
		log.Errorf("failed to look up the logo %s: %s", fileId, err)
		http.Error(response, "not found", http.StatusNotFound)
		return
	}
	if publication == nil {
		http.Error(response, "not found", http.StatusNotFound)
		return
	}

	self.serveLogo(response, request, publication)
}

// serveLogo writes the bytes, with the headers that make a stranger's
// document safe to draw: the type it was checked as, no sniffing, and a
// policy that allows it nothing.
func (self *media) serveLogo(response http.ResponseWriter, request *http.Request, publication *db.BimiPublication) {
	content, err := self.storage.GetFile(request.Context(), publication.FileID)
	if err != nil {
		if !errors.Is(err, storage.ErrNotFound) {
			log.Errorf("failed to read the logo %s: %s", publication.FileID, err)
		}
		http.Error(response, "not found", http.StatusNotFound)
		return
	}

	response.Header().Set("Content-Type", "image/svg+xml")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	// Belt as well as braces. The file passed a check that forbids everything
	// a document can do; this says so to the browser as well, for the reader
	// who opens the address directly.
	response.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	// A mark changes rarely, and receivers fetch it often.
	response.Header().Set("Cache-Control", "public, max-age=86400")
	response.Header().Set("Content-Length", fmt.Sprint(len(content)))
	if _, err := response.Write(content); err != nil {
		log.Debugf("failed to write the logo %s: %s", publication.FileID, err)
	}
}

// writeJSON answers with a small object, which is what an upload's caller
// reads: the address to put in the record, or the reason the file was
// refused.
func writeJSON(response http.ResponseWriter, status int, value map[string]string) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	if err := json.NewEncoder(response).Encode(value); err != nil {
		log.Debugf("failed to write the answer: %s", err)
	}
}

// removeLogoFile drops bytes nothing points at any more. A failure is logged
// and not otherwise minded: the row is already gone, so the file is merely
// taking up space.
func removeLogoFile(ctx context.Context, files storage.Storage, fileId string) {
	if fileId == "" {
		return
	}
	if err := files.DeleteFile(ctx, fileId); err != nil && !errors.Is(err, storage.ErrNotFound) {
		log.Warningf("failed to remove the replaced logo %s: %s", fileId, err)
	}
}
