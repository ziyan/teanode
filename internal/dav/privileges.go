package dav

import (
	"bytes"
	"context"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// Telling a phone which calendars it may not change.
//
// A calendar shared with somebody below edit is read only for them. The
// protocol has a property for that, current-user-privilege-set, and a phone
// that reads it grays out the editing instead of letting the person type an
// event and fail after. The protocol library writes that property but
// hard-codes "read and write" for every calendar, so the answer to a PROPFIND
// is taken in a buffer and the write privilege is taken out of the calendars
// the person may only read.
//
// That is a courtesy and not the protection. The refusal is made before the
// library sees a change (refuseReadOnly) and again where it is kept
// (mayWrite), so a client that ignores the property is still refused.

// changesSomething reports whether a method does anything but ask. Everything
// that is not one of the questions is treated as a change, so a method added
// to WebDAV later is refused for a read only calendar rather than let through.
func changesSomething(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, "PROPFIND", "REPORT":
		return false
	}
	return true
}

// refuseReadOnly answers 403 to a change to a calendar the person reaches but
// may not change, before the library has read the request. A calendar they do
// not reach at all, or one that is not there, is left to the library to say
// not found.
func (self *component) refuseReadOnly(response http.ResponseWriter, request *http.Request, backing *calendarBackend, signedIn *session) bool {
	found, err := backing.calendarAt(request.Context(), signedIn, request.URL.Path, models.ShareLevelBusy)
	if err != nil {
		return false
	}
	if refusal := mayWrite(found); refusal != nil {
		httpStatus, refusalText := statusOf(refusal)
		http.Error(response, refusalText, httpStatus)
		return true
	}
	return false
}

// readOnlyCalendars are the addresses of the calendars the person reaches and
// may not change, which are the ones a PROPFIND must not say they can.
func (self *component) readOnlyCalendars(ctx context.Context, signedIn *session) (map[string]bool, error) {
	readOnly := map[string]bool{}
	err := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		reachable, err := tx.ListReachableCalendars(signedIn.userID)
		if err != nil {
			return err
		}
		for _, each := range reachable {
			if each.ShareLevel.CanRead() && !each.ShareLevel.CanWrite() {
				readOnly[calendarPath(signedIn.userID, each.Calendar.ID)] = true
			}
		}
		return nil
	})
	return readOnly, err
}

// bufferedResponse holds an answer until the handler is done, so that it can
// be rewritten.
type bufferedResponse struct {
	header     http.Header
	httpStatus int
	body       bytes.Buffer
}

func (self *bufferedResponse) Header() http.Header { return self.header }

func (self *bufferedResponse) WriteHeader(httpStatus int) {
	if self.httpStatus == 0 {
		self.httpStatus = httpStatus
	}
}

func (self *bufferedResponse) Write(content []byte) (int, error) {
	if self.httpStatus == 0 {
		self.httpStatus = http.StatusOK
	}
	return self.body.Write(content)
}

// responseBlock is one <response> of a multistatus answer. The library writes
// the elements in the DAV: namespace without a prefix, repeating the namespace
// on each element, so every pattern here allows attributes on the tag.
var responseBlock = regexp.MustCompile(`(?s)<response\b[^>]*>.*?</response>`)

var hrefOfBlock = regexp.MustCompile(`<href\b[^>]*>([^<]*)</href>`)

var writePrivilege = regexp.MustCompile(`<write\b[^>]*/>|<write\b[^>]*>\s*</write>`)

// withoutWritePrivilege is a multistatus answer with the write privilege taken
// out of the responses for the calendars named. Anything it does not
// recognize it leaves exactly as it was.
func withoutWritePrivilege(body []byte, readOnly map[string]bool) []byte {
	return responseBlock.ReplaceAllFunc(body, func(block []byte) []byte {
		found := hrefOfBlock.FindSubmatch(block)
		if found == nil || !readOnly[strings.TrimSpace(string(found[1]))] {
			return block
		}
		return writePrivilege.ReplaceAll(block, nil)
	})
}

// serveWithPrivileges runs the library's handler and, when the person reaches
// a calendar they may only read, rewrites its PROPFIND answer to say so.
func (self *component) serveWithPrivileges(response http.ResponseWriter, request *http.Request, signedIn *session, handler http.Handler) {
	readOnly, err := self.readOnlyCalendars(request.Context(), signedIn)
	if err != nil || len(readOnly) == 0 {
		// Nothing to rewrite, which is nearly every account; or something
		// that cannot be checked, in which case the library's answer stands
		// and the refusal where changes are kept is what protects.
		if err != nil {
			log.Warningf("cannot read which calendars are read only: %s", err)
		}
		handler.ServeHTTP(response, request)
		return
	}
	buffered := &bufferedResponse{header: http.Header{}}
	handler.ServeHTTP(buffered, request)
	body := buffered.body.Bytes()
	if buffered.httpStatus == http.StatusMultiStatus {
		body = withoutWritePrivilege(body, readOnly)
	}
	for name, values := range buffered.header {
		response.Header()[name] = values
	}
	if response.Header().Get("Content-Length") != "" {
		response.Header().Set("Content-Length", strconv.Itoa(len(body)))
	}
	if buffered.httpStatus != 0 {
		response.WriteHeader(buffered.httpStatus)
	}
	_, _ = response.Write(body)
}
