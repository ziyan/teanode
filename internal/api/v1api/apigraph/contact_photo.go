package apigraph

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/contacts"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// The picture on a contact's card, served as a picture.
//
// It lives inside the card as base64, and a card carrying a photograph is
// four hundred kilobytes. Handing that to a browser inside a listing would
// mean a page that costs megabytes to draw a column of faces, so the listing
// says only whether there is one and this serves it, one request per contact,
// cached by the card's own ETag.
func (self *graph) contactPhotoView(response http.ResponseWriter, request *http.Request) {
	// The same way every other handler beside the GraphQL endpoint
	// establishes who is calling: the username the authentication
	// middleware put on the request, resolved to an account.
	username := api.UsernameFromRequest(request)
	if username == "" || username == config.LocalUsername {
		http.Error(response, "not signed in", http.StatusUnauthorized)
		return
	}
	user, err := self.database.GetUserByUsername(username)
	if err != nil {
		http.Error(response, "cannot read that just now", http.StatusInternalServerError)
		return
	}
	if user == nil || user.Disabled() {
		http.Error(response, "not signed in", http.StatusUnauthorized)
		return
	}
	allowed := false
	if err := self.database.Transaction(func(tx db.Transaction) error {
		permissions, err := tx.EffectivePermissions(user.ID)
		if err != nil {
			return err
		}
		allowed = permissions.Has(models.PermissionContactsUse)
		return nil
	}); err != nil {
		http.Error(response, "cannot read that just now", http.StatusInternalServerError)
		return
	}
	if !allowed {
		http.Error(response, "not allowed", http.StatusForbidden)
		return
	}
	contactId := strings.TrimSpace(mux.Vars(request)["contactId"])

	// Through the address book, which is checked to be this person's: a
	// contact identifier is the file name a client chose and is unique
	// only inside one book, so it says nothing about whose it is.
	var found *models.Contact
	if err := self.database.TransactionContext(request.Context(), func(tx db.Transaction) error {
		books, err := tx.ListAddressBooks(user.ID)
		if err != nil {
			return err
		}
		for _, book := range books {
			contact, err := tx.GetContact(book.ID, contactId)
			if err != nil {
				return err
			}
			if contact != nil {
				found = contact
				return nil
			}
		}
		return nil
	}); err != nil {
		log.Errorf("cannot read a contact's picture: %s", err)
		http.Error(response, "cannot read that just now", http.StatusInternalServerError)
		return
	}
	if found == nil {
		http.Error(response, "no such contact", http.StatusNotFound)
		return
	}

	// The card's own version names the picture: a picture cannot change
	// without the card changing, so a browser that has it never asks again.
	tag := `"` + found.ETag + `"`
	if matchesTag(request.Header.Get("If-None-Match"), tag) {
		// The headers a cached entry needs to stay fresh go on the 304 as
		// well, which RFC 9110 asks for: without them a store can lose the
		// validator it was keeping.
		response.Header().Set("ETag", tag)
		response.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
		response.WriteHeader(http.StatusNotModified)
		return
	}

	picture, mediaType, err := contacts.Photo([]byte(found.Card))
	if err != nil || len(picture) == 0 {
		http.Error(response, "that contact has no picture", http.StatusNotFound)
		return
	}

	response.Header().Set("Content-Type", mediaType)
	response.Header().Set("Content-Length", strconv.Itoa(len(picture)))
	response.Header().Set("ETag", tag)
	// Private, because it is one person's address book, and revalidated by
	// the ETag rather than held for a fixed time.
	response.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
	// It is a picture and nothing else, whatever it turns out to contain.
	// The type is chosen from a short list rather than taken from the card,
	// and this says the same thing a second way: nothing here loads
	// anything, and nothing here runs.
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	response.Header().Set("Content-Disposition", "inline")
	if request.Method == http.MethodHead {
		return
	}
	if _, err := response.Write(picture); err != nil {
		log.Debugf("a contact's picture could not be written: %s", err)
	}
}

// matchesTag says whether an If-None-Match names the version being served.
//
// The header may be a list, and a proxy may have weakened the validator by
// marking it W/; comparing the whole string against one tag made every one of
// those revalidate in full, every time.
func matchesTag(given, tag string) bool {
	given = strings.TrimSpace(given)
	if given == "" {
		return false
	}
	if given == "*" {
		return true
	}
	for _, candidate := range strings.Split(given, ",") {
		candidate = strings.TrimSpace(candidate)
		candidate = strings.TrimPrefix(candidate, "W/")
		if candidate == tag {
			return true
		}
	}
	return false
}
