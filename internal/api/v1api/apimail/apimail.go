// Package apimail serves a stored message in its original form.
//
// Not GraphQL, because this is a file: the browser has to be able to follow a
// link to it and save it, and a base64 blob inside a JSON reply is neither
// streamable nor something "Save as" understands.
package apimail

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"github.com/op/go-logging"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/storage"
	"github.com/ziyan/teanode/internal/util/mailparse"
	"github.com/ziyan/teanode/internal/web"
)

var log = logging.MustGetLogger("apimail")

type mail struct {
	database db.Database
	config   config.Store
	storage  storage.Storage
}

// New builds the raw message component.
func New(database db.Database, configuration config.Store, messages storage.Storage) (web.Component, error) {
	return &mail{database: database, config: configuration, storage: messages}, nil
}

func (self *mail) AddRoutes(router *mux.Router) error {
	router.Path(api.PathMailRaw).Methods(http.MethodGet).HandlerFunc(self.rawView)
	router.Path(api.PathMailAttachment).Methods(http.MethodGet).HandlerFunc(self.attachmentView)
	router.Path(api.PathMailRemote).Methods(http.MethodGet).HandlerFunc(self.remoteView)
	return nil
}

// attachmentView serves one part of a stored message: the file behind a
// download link, and the image behind a cid: reference in the HTML.
//
// By position rather than by name, because a filename in a message is
// whatever the sender wrote — it can repeat, be empty, or be a path — and a
// stored message never changes, so its parts keep their order.
func (self *mail) attachmentView(response http.ResponseWriter, request *http.Request) {
	if err := self.requireOperator(request); err != nil {
		http.Error(response, "not logged in", http.StatusUnauthorized)
		return
	}

	variables := mux.Vars(request)
	index, err := strconv.Atoi(variables["index"])
	if err != nil || index < 0 {
		http.Error(response, "not found", http.StatusNotFound)
		return
	}

	headers, body, err := self.load(request, variables["mailId"])
	if err != nil {
		self.fail(response, variables["mailId"], err)
		return
	}

	part, err := mailparse.PartAt(headers, body, index)
	if err != nil {
		self.fail(response, variables["mailId"], err)
		return
	}

	// The sender's content type, but never one the browser will run as part
	// of this origin: a message is written by a stranger, and an attachment
	// they called text/html would be a page on the dashboard's own origin.
	contentType := part.ContentType
	disposition := "attachment"
	if isSafeToDisplay(contentType) {
		// An image the message refers to has to render in place rather than
		// download, which is the whole point of resolving cid: to here.
		disposition = "inline"
	} else {
		contentType = "application/octet-stream"
	}

	response.Header().Set("Content-Type", contentType)
	response.Header().Set("Content-Disposition",
		fmt.Sprintf("%s; filename=%q", disposition, attachmentFilename(part.Filename, index)))
	// A stranger's file must not be sniffed into something executable.
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.WriteHeader(http.StatusOK)
	if _, err := response.Write(part.Content); err != nil {
		log.Debugf("failed to write attachment %d of %q: %s", index, variables["mailId"], err)
	}
}

// displayable are the types served as themselves and shown in place. Images
// and PDFs, because that is what a message embeds and what a reader expects
// to look at rather than save; everything else is handed over as a download,
// so nothing a sender chose decides how the browser treats it.
var displayable = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/gif": true,
	"image/webp": true, "image/bmp": true, "image/x-icon": true,
	"application/pdf": true,
}

func isSafeToDisplay(contentType string) bool {
	return displayable[strings.ToLower(strings.TrimSpace(contentType))]
}

// attachmentFilename keeps a sender's filename from being a path or a quote,
// falling back to the part's position when there is nothing usable left.
func attachmentFilename(filename string, index int) string {
	filename = strings.TrimSpace(filename)
	filename = strings.ReplaceAll(filename, "\\", "/")
	if slash := strings.LastIndex(filename, "/"); slash >= 0 {
		filename = filename[slash+1:]
	}
	filename = strings.Map(func(letter rune) rune {
		if letter < 0x20 || letter == '"' || letter == 0x7f {
			return -1
		}
		return letter
	}, filename)
	if filename == "" || filename == "." || filename == ".." {
		return fmt.Sprintf("attachment-%d", index)
	}
	return filename
}

// rawView writes the message as it arrived.
//
// The path is not public, so the authentication middleware has already turned
// away anyone without a session; this checks the operator again because a
// server with no accounts is open by design and the check has to say so in one
// place rather than be assumed.
func (self *mail) rawView(response http.ResponseWriter, request *http.Request) {
	if err := self.requireOperator(request); err != nil {
		http.Error(response, "not logged in", http.StatusUnauthorized)
		return
	}

	mailId := mux.Vars(request)["mailId"]
	headers, body, err := self.load(request, mailId)
	if err != nil {
		self.fail(response, mailId, err)
		return
	}

	// message/rfc822 is what an .eml file is, and naming it after the message
	// means a folder of saved ones is still sorted and identifiable.
	response.Header().Set("Content-Type", "message/rfc822")
	response.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=%q", sanitizeFilename(mailId)+".eml"))
	response.WriteHeader(http.StatusOK)

	// The blank line between the headers and the body is what makes this a
	// message rather than two pieces of one.
	if _, err := response.Write([]byte(strings.Join(headers, "\r\n") + "\r\n\r\n")); err != nil {
		log.Debugf("failed to write the headers of %q: %s", mailId, err)
		return
	}
	if _, err := response.Write(body); err != nil {
		log.Debugf("failed to write the body of %q: %s", mailId, err)
	}
}

// requireOperator refuses a caller who is not one. The middleware has already
// turned away anyone without a session; this says the rule in one place
// rather than leaving it assumed, because a server with no accounts is open
// by design.
func (self *mail) requireOperator(request *http.Request) error {
	if len(self.config.Current().Users) > 0 && api.UsernameFromRequest(request) == "" {
		return api.ErrNotLoggedIn
	}
	return nil
}

// load reads a stored message, or says why it could not.
func (self *mail) load(request *http.Request, mailId string) ([]string, []byte, error) {
	var headers []string
	var body []byte
	err := self.database.Transaction(func(tx db.Transaction) error {
		stored, err := tx.GetMail(mailId, nil)
		if err != nil {
			return err
		}
		if stored == nil {
			return api.ErrNotFound
		}
		headers, body, err = self.storage.Get(request.Context(), stored.ID)
		return err
	})
	return headers, body, err
}

func (self *mail) fail(response http.ResponseWriter, mailId string, err error) {
	switch {
	case errors.Is(err, api.ErrNotFound), errors.Is(err, storage.ErrNotFound),
		errors.Is(err, mailparse.ErrNoSuchPart):
		http.Error(response, "not found", http.StatusNotFound)
	default:
		log.Errorf("failed to read %q: %s", mailId, err)
		http.Error(response, "could not read the message", http.StatusInternalServerError)
	}
}

// sanitizeFilename keeps the identifier to characters that cannot end a quoted
// filename early or climb out of a directory.
func sanitizeFilename(value string) string {
	cleaned := strings.Map(func(letter rune) rune {
		switch {
		case letter >= 'a' && letter <= 'z', letter >= 'A' && letter <= 'Z', letter >= '0' && letter <= '9':
			return letter
		case letter == '-', letter == '_':
			return letter
		default:
			return -1
		}
	}, value)
	if cleaned == "" {
		return "message"
	}
	return cleaned
}
