// Package apimedia stores the pictures an operator puts in a template and
// serves them to the mail programs that later ask for them.
//
// Not GraphQL, in both directions and for different reasons. Uploading is a
// file, and a base64 blob inside a JSON mutation is a third larger and cannot
// be streamed. Serving is a request from somebody else's mail program, which
// speaks HTTP and nothing else, has no session and never will, and wants
// bytes with a content type.
package apimedia

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/op/go-logging"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
	"github.com/ziyan/teanode/internal/web"
)

var log = logging.MustGetLogger("apimedia")

// maximumSize is what an upload may be.
//
// A megabyte is generous for a logo — a good one is tens of kilobytes — and
// small enough that a mistake, or somebody pointing a script at this, does not
// fill the disk before anybody notices. It is enforced by refusing to read
// past it rather than by believing a declared length.
const maximumSize = 1 << 20

// displayable are the types that may be stored and served.
//
// An allow list, not a deny list: this is served over HTTPS from the
// operator's own domain, so anything the browser might treat as a document
// would run with that origin. SVG is the reason the list is written out rather
// than being "anything beginning image/" — an SVG is a document that can carry
// script, and refusing one format is cheaper than being sure about sanitising
// it.
var displayable = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/gif":  true,
	"image/webp": true,
}

type media struct {
	database db.Database
	config   config.Store
	storage  storage.Storage
}

// New builds the media component.
func New(database db.Database, configuration config.Store, files storage.Storage) (web.Component, error) {
	return &media{database: database, config: configuration, storage: files}, nil
}

func (self *media) AddRoutes(router *mux.Router) error {
	router.Path(api.PathMediaUpload).Methods(http.MethodPost).HandlerFunc(self.uploadView)
	router.Path(api.PathMediaFile).Methods(http.MethodGet).HandlerFunc(self.fileView)
	router.Path(api.PathMediaLink).Methods(http.MethodGet).HandlerFunc(self.linkView)
	return nil
}

// linkView serves a picture at an address belonging to one sent message, and
// notes that it was fetched.
//
// Recorded before the bytes go out, and a failure to record is not a failure
// to serve: a picture that does not load because a database was busy is worse
// than a count that missed one.
func (self *media) linkView(response http.ResponseWriter, request *http.Request) {
	token := mux.Vars(request)["token"]
	link, err := self.database.GetMediaLink(token)
	if err != nil {
		log.Errorf("failed to resolve the address %s: %s", token, err)
		http.Error(response, "not found", http.StatusNotFound)
		return
	}
	if link == nil {
		http.Error(response, "not found", http.StatusNotFound)
		return
	}

	if err := self.database.RecordMediaLinkOpen(token, time.Now(), remoteAddress(request), request.UserAgent()); err != nil {
		log.Warningf("failed to record the open of %s: %s", token, err)
	}

	// Not cacheable, unlike the picture at its own address. A mail program
	// that cached this would fetch it once and the count would stop, and the
	// bytes are already cached under the other address anyway.
	// Not cacheable, and said so here because nothing downstream may decide
	// otherwise. This message's address had to survive a CDN: one cached copy
	// and every open after the first is answered by somebody else, invisibly,
	// so the count stops at one and the operator reads that as nobody looking
	// again.
	self.serve(response, request, link.MediaID, "no-store")
}

// remoteAddress is who asked, without the port. For most mail it is a proxy —
// Gmail fetches through its own — so it says less than it appears to.
func remoteAddress(request *http.Request) string {
	address := request.RemoteAddr
	if host, _, err := net.SplitHostPort(address); err == nil {
		return host
	}
	return address
}

// uploadView stores a picture. An operator's action, so it is behind the
// session the rest of the dashboard is behind.
func (self *media) uploadView(response http.ResponseWriter, request *http.Request) {
	if api.UsernameFromRequest(request) == "" && self.claimed() {
		http.Error(response, "not logged in", http.StatusUnauthorized)
		return
	}

	// The limit is applied to the request body before anything is read, so a
	// caller cannot make this allocate by lying about the length.
	request.Body = http.MaxBytesReader(response, request.Body, maximumSize+1024)
	if err := request.ParseMultipartForm(maximumSize + 1024); err != nil {
		http.Error(response, "the upload is too large or malformed", http.StatusBadRequest)
		return
	}

	domainId := strings.TrimSpace(request.FormValue("domainId"))
	var domain *models.Domain
	if err := self.database.Transaction(func(tx db.Transaction) error {
		var err error
		domain, err = tx.GetDomain(domainId)
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

	// One byte past the limit, so a file exactly at it is accepted and one
	// over it is refused rather than truncated and stored as a broken image.
	content, err := io.ReadAll(io.LimitReader(file, maximumSize+1))
	if err != nil {
		http.Error(response, "the upload could not be read", http.StatusBadRequest)
		return
	}
	if len(content) > maximumSize {
		http.Error(response, fmt.Sprintf("the file is larger than %d KB", maximumSize/1024), http.StatusRequestEntityTooLarge)
		return
	}
	if len(content) == 0 {
		http.Error(response, "the file is empty", http.StatusBadRequest)
		return
	}

	// What the bytes are, not what the browser said they were. A file named
	// .png whose content is HTML is refused here, which is the only place it
	// can be refused: after this it is a name in a database.
	contentType := detectContentType(content)
	if !displayable[contentType] {
		http.Error(response, "only PNG, JPEG, GIF and WebP images can be uploaded", http.StatusUnsupportedMediaType)
		return
	}

	checksum := sha256.Sum256(content)
	stored := &models.Media{
		ID:          config.NewID(),
		DomainID:    domain.ID,
		Filename:    filename(header.Filename),
		ContentType: contentType,
		Size:        int64(len(content)),
		Checksum:    hex.EncodeToString(checksum[:]),
	}

	// Bytes first. A row pointing at bytes that are not there answers 404 for
	// ever; bytes with no row are unreachable and cost only the space.
	if err := self.storage.PutFile(request.Context(), stored.ID, content); err != nil {
		log.Errorf("failed to store media %s: %s", stored.ID, err)
		http.Error(response, "the file could not be stored", http.StatusInternalServerError)
		return
	}
	created, err := self.database.CreateMedia(stored)
	if err != nil {
		log.Errorf("failed to record media %s: %s", stored.ID, err)
		http.Error(response, "the file could not be stored", http.StatusInternalServerError)
		return
	}

	log.Noticef("%s uploaded %s (%s, %d bytes) for %s",
		api.UsernameFromRequest(request), created.ID, created.ContentType, created.Size, domain.Domain)
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(response).Encode(map[string]any{
		"id":          created.ID,
		"filename":    created.Filename,
		"contentType": created.ContentType,
		"size":        created.Size,
		// Where it is while the operator is editing. On send this is replaced
		// by an address of its own, under the sending domain.
		"url": api.MediaPath(created.ID),
	}); err != nil {
		log.Debugf("failed to write the upload reply: %s", err)
	}
}

// fileView serves the bytes. No session: the caller is a mail program, or the
// dashboard's preview, and neither has one.
func (self *media) fileView(response http.ResponseWriter, request *http.Request) {
	// The bytes at this address never change: a new upload is a new
	// identifier. A year, and a mail program that opens the same message
	// twice asks once.
	self.serve(response, request, mux.Vars(request)["mediaId"], "public, max-age=31536000, immutable")
}

// serve answers with a stored picture, and is shared with the per-message
// addresses that resolve to one.
//
// The caching is the caller's to decide and is not defaulted here. The two
// callers want opposite things — one address is permanent and the other must
// never be cached at all — and a default would be silently wrong for one of
// them, which is how the per-message address ended up cacheable and counted
// its opens once.
func (self *media) serve(response http.ResponseWriter, request *http.Request, mediaId, caching string) {
	stored, err := self.database.GetMedia(mediaId)
	if err != nil {
		log.Errorf("failed to look up media %s: %s", mediaId, err)
		http.Error(response, "not found", http.StatusNotFound)
		return
	}
	if stored == nil {
		http.Error(response, "not found", http.StatusNotFound)
		return
	}

	content, err := self.storage.GetFile(request.Context(), stored.ID)
	if err != nil {
		if !errors.Is(err, storage.ErrNotFound) {
			log.Errorf("failed to read media %s: %s", stored.ID, err)
		}
		http.Error(response, "not found", http.StatusNotFound)
		return
	}

	// The stored type, which was decided by reading the bytes at upload and is
	// on the allow list. Never sniffed again by the browser.
	response.Header().Set("Content-Type", stored.ContentType)
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", stored.Filename))
	response.Header().Set("Cache-Control", caching)
	response.WriteHeader(http.StatusOK)
	if _, err := response.Write(content); err != nil {
		log.Debugf("failed to write media %s: %s", stored.ID, err)
	}
}

// detectContentType reads the bytes rather than the name.
//
// http.DetectContentType looks at the first 512 bytes and returns a type with
// parameters, which are dropped here so the comparison is against the bare
// type.
func detectContentType(content []byte) string {
	detected := http.DetectContentType(content)
	if semicolon := strings.IndexByte(detected, ';'); semicolon >= 0 {
		detected = detected[:semicolon]
	}
	return strings.ToLower(strings.TrimSpace(detected))
}

// filename keeps what the operator called the file, without letting it be a
// path, a quote or a control character — it is echoed in a header.
func filename(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "\\", "/")
	if slash := strings.LastIndex(value, "/"); slash >= 0 {
		value = value[slash+1:]
	}
	value = strings.Map(func(letter rune) rune {
		if letter < 0x20 || letter == '"' || letter == 0x7f {
			return -1
		}
		return letter
	}, value)
	if value == "" || value == "." || value == ".." {
		return "image"
	}
	if len(value) > 128 {
		value = value[:128]
	}
	return value
}

// claimed reports whether this server has an account, in which case a
// request without one is refused. A server with none is open by design, so
// that the first person to arrive can claim it.
func (self *media) claimed() bool {
	count, err := self.database.CountUsers()
	if err != nil {
		log.Errorf("could not count the accounts: %s", err)
		return true
	}
	return count > 0
}
