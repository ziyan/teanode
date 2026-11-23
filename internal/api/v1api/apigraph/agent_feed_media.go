package apigraph

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// feedMediaView serves the picture of a web page shown on a card, as the
// server kept it, to the card's person and nobody else.
func (self *graph) feedMediaView(response http.ResponseWriter, request *http.Request) {
	username := api.UsernameFromRequest(request)
	if username == "" || username == config.LocalUsername {
		http.Error(response, "not signed in", http.StatusUnauthorized)
		return
	}
	variables := mux.Vars(request)
	var entry *models.FeedMedia
	isSignedIn := false
	if err := self.database.TransactionContext(request.Context(), func(transaction db.Transaction) error {
		user, err := transaction.GetUserByUsername(username)
		if err != nil {
			return err
		}
		if user == nil || user.Disabled() {
			return nil
		}
		isSignedIn = true
		item, err := transaction.GetAgentFeedItem(variables["feedItemId"])
		if err != nil {
			return err
		}
		// Somebody else's card is answered as no card at all.
		if item == nil || item.UserID != user.ID {
			return nil
		}
		for index := range item.FeedMedia {
			if item.FeedMedia[index].MediaKey == variables["mediaKey"] && item.FeedMedia[index].FeedMediaKind == models.FeedMediaPicture {
				entry = &item.FeedMedia[index]
			}
		}
		return nil
	}); err != nil {
		log.Errorf("cannot read a card's picture: %s", err)
		http.Error(response, "cannot read that just now", http.StatusInternalServerError)
		return
	}
	if !isSignedIn {
		http.Error(response, "not signed in", http.StatusUnauthorized)
		return
	}
	if entry == nil || entry.ImageFileID == "" {
		http.Error(response, "no such picture", http.StatusNotFound)
		return
	}

	tag := `"` + entry.ImageFileID + `"`
	response.Header().Set("ETag", tag)
	// Private because only the card's person is let in, and a day because a
	// changed picture has a new key.
	response.Header().Set("Cache-Control", "private, max-age=86400")
	if matchesTag(request.Header.Get("If-None-Match"), tag) {
		response.WriteHeader(http.StatusNotModified)
		return
	}
	content, err := self.storage.GetFile(request.Context(), entry.ImageFileID)
	if err != nil {
		if !errors.Is(err, storage.ErrNotFound) {
			log.Errorf("cannot read the file %s of a card's picture: %s", entry.ImageFileID, err)
			http.Error(response, "cannot read that just now", http.StatusInternalServerError)
			return
		}
		http.Error(response, "no such picture", http.StatusNotFound)
		return
	}
	response.Header().Set("Content-Type", "image/jpeg")
	response.Header().Set("Content-Length", strconv.Itoa(len(content)))
	// It is a picture and nothing else, whatever the file turns out to hold.
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	response.Header().Set("Content-Disposition", "inline")
	if request.Method == http.MethodHead {
		return
	}
	if _, err := response.Write(content); err != nil {
		log.Debugf("a card's picture could not be written: %s", err)
	}
}
