package dav

import (
	"encoding/xml"
	"net/http"
	"strings"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// The sync-collection report (RFC 6578), for the phones that keep a calendar
// in step with it rather than with a change tag: an Android phone's calendar
// app asks each calendar only for its sync-token and the reports it answers,
// and reads nothing from a calendar that has neither.
//
// The server keeps no record of what was deleted, so it cannot say what
// changed since an older token. The token is the calendar's change tag
// (changetag.go): asked with the current token, nothing has changed; asked
// with no token, every event is listed; asked with any other token, it says
// the token is no longer valid, which is how the protocol tells a client to
// start again from no token and read the whole calendar.

// syncTokenPrefix makes a change tag a URI, which a sync-token must be.
const syncTokenPrefix = "urn:teanode:sync:"

func syncTokenOf(tag string) string { return syncTokenPrefix + tag }

type syncCollectionRequest struct {
	SyncToken string `xml:"DAV: sync-token"`
	Props     struct {
		Names []xml.Name `xml:",any"`
	} `xml:"DAV: prop"`
}

type syncMultistatus struct {
	XMLName   xml.Name           `xml:"DAV: multistatus"`
	Responses []calendarResponse `xml:"DAV: response"`
	SyncToken string             `xml:"DAV: sync-token"`
}

// serveCalendarSync answers a sync-collection report on a calendar.
func (self *component) serveCalendarSync(writer http.ResponseWriter, request *http.Request, backing *calendarBackend, body []byte) {
	if !addressesACalendar(request.URL.Path) {
		http.Error(writer, "that report is about a calendar, not one event in it", http.StatusForbidden)
		return
	}
	var asked syncCollectionRequest
	if err := xml.Unmarshal(body, &asked); err != nil {
		http.Error(writer, "that request could not be read", http.StatusBadRequest)
		return
	}
	ctx := request.Context()
	signedIn := backing.who(ctx)
	found, err := backing.calendarAt(ctx, signedIn, request.URL.Path, models.ShareLevelTitles)
	if err != nil {
		status, message := statusOf(err)
		http.Error(writer, message, status)
		return
	}
	var tag string
	var objects []*models.CalendarObject
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if tag, err = tx.CalendarChangeTag(found.ID); err != nil {
			return unexpectedCalendar(err)
		}
		objects, err = tx.ReadCalendarObjects(found.ID, found.shareLevel)
		return unexpectedCalendar(err)
	}); err != nil {
		status, message := statusOf(err)
		http.Error(writer, message, status)
		return
	}
	current := syncTokenOf(tag)
	given := strings.TrimSpace(asked.SyncToken)
	if given != "" && given != current {
		writer.Header().Set("Content-Type", "application/xml; charset=utf-8")
		writer.WriteHeader(http.StatusForbidden)
		_, _ = writer.Write([]byte(xml.Header + `<error xmlns="DAV:"><valid-sync-token/></error>`))
		return
	}

	wantsData, wantsETag, wantsLength := false, false, false
	for _, name := range asked.Props.Names {
		switch {
		case name.Space == calendarNS && name.Local == "calendar-data":
			wantsData = true
		case name.Local == "getetag":
			wantsETag = true
		case name.Local == "getcontentlength":
			wantsLength = true
		}
	}
	// A listing with nothing in it would tell a client nothing to compare.
	if !wantsData && !wantsLength {
		wantsETag = true
	}
	var answers []calendarResponse
	if given == "" {
		for _, object := range objects {
			answers = append(answers, foundEvent(signedIn, object, wantsETag, wantsLength, wantsData))
		}
	}
	encoded, err := xml.Marshal(&syncMultistatus{Responses: answers, SyncToken: current})
	if err != nil {
		log.Errorf("a calendar sync report could not be written: %s", err)
		http.Error(writer, "this server could not do that just now", http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Content-Type", "application/xml; charset=utf-8")
	writer.WriteHeader(http.StatusMultiStatus)
	if _, err := writer.Write([]byte(xml.Header)); err != nil {
		return
	}
	_, _ = writer.Write(encoded)
}
