package dav

import (
	"bytes"
	"context"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/ziyan/teanode/internal/db"
)

// Telling a phone that a calendar or an address book has changed.
//
// A sync adapter does not read a whole collection every time it looks. It asks
// the collection for a change tag, getctag from the namespace Apple's calendar
// server defined, and reads the collection again only when the tag differs
// from the one it kept. Some read nothing at all without it: an Android
// phone's calendar app signed in, found the calendars, asked each for its tag,
// was told it had none, and stopped there. The protocol library has no such
// property, so the answer to a PROPFIND is taken in a buffer, and the tag is
// put in the found part of each collection's response, out of the part that
// says not found.
//
// The tag is the number of objects and a digest of their names and versions
// (db CalendarChangeTag), so it changes when one is added, changed or deleted
// and not otherwise. A calendar also answers a sync-token made from the same
// tag, and the reports it answers, for the phones that sync with the
// sync-collection report instead (sync_collection.go).

const changeTagNamespace = "http://calendarserver.org/ns/"

// propstatBlock is one <propstat> of a response, and statusOfBlock its status
// line. The library writes them without prefixes, repeating the namespace on
// each element, as responseBlock in privileges.go describes.
var propstatBlock = regexp.MustCompile(`(?s)<propstat\b[^>]*>.*?</propstat>`)

var statusOfBlock = regexp.MustCompile(`<status\b[^>]*>([^<]*)</status>`)

// missingChangeTag is the tag asked for and not known to the library, as it
// writes one in the not found part: the element, empty.
var missingChangeTag = regexp.MustCompile(`<getctag\b[^>]*xmlns="` + regexp.QuoteMeta(changeTagNamespace) + `"[^>]*(?:/>|>\s*</getctag>)`)

var emptyProp = regexp.MustCompile(`(?s)<prop\b[^>]*>\s*</prop>`)

// isAllProp says the PROPFIND asks for every property: an allprop body, or no
// body at all, which RFC 4918 reads the same way.
func isAllProp(body []byte) bool {
	trimmed := bytes.TrimSpace(body)
	return len(trimmed) == 0 || regexp.MustCompile(`<(?:[A-Za-z0-9_-]+:)?allprop\b`).Match(trimmed)
}

// collectionOf is the calendar or address book an href names, by its kind
// (calendarsSegment or contactsSegment) and id, when it names one of the
// signed-in person's collections and not an object in one.
func collectionOf(href, userId string) (kind, collectionId string, ok bool) {
	path := strings.TrimSpace(href)
	if !strings.HasSuffix(path, "/") {
		return "", "", false
	}
	segments := strings.Split(strings.Trim(strings.TrimPrefix(path, Prefix), "/"), "/")
	if len(segments) != 3 || segments[0] != userId || segments[2] == "" {
		return "", "", false
	}
	switch segments[1] {
	case calendarsSegment, contactsSegment:
		return segments[1], segments[2], true
	}
	return "", "", false
}

// collectionTag is a collection's change tag and its kind (calendarsSegment
// or contactsSegment).
type collectionTag struct {
	kind string
	tag  string
}

// changeTagsOf are the tags of the collections a multistatus answer names, by
// href, read in one transaction.
func (self *component) changeTagsOf(ctx context.Context, signedIn *session, body []byte) (map[string]collectionTag, error) {
	type collection struct{ kind, id string }
	named := map[string]collection{}
	for _, block := range responseBlock.FindAll(body, -1) {
		found := hrefOfBlock.FindSubmatch(block)
		if found == nil {
			continue
		}
		href := strings.TrimSpace(string(found[1]))
		if kind, collectionId, ok := collectionOf(href, signedIn.userID); ok {
			named[href] = collection{kind: kind, id: collectionId}
		}
	}
	tags := map[string]collectionTag{}
	if len(named) == 0 {
		return tags, nil
	}
	err := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		for href, each := range named {
			var tag string
			var err error
			if each.kind == calendarsSegment {
				tag, err = tx.CalendarChangeTag(each.id)
			} else {
				tag, err = tx.AddressBookChangeTag(each.id)
			}
			if err != nil {
				return err
			}
			tags[href] = collectionTag{kind: each.kind, tag: tag}
		}
		return nil
	})
	return tags, err
}

// withChangeTags is a multistatus answer with each named collection's getctag
// moved from the not found part to the found part, filled in, and for a
// calendar its sync-token and supported-report-set too (sync_collection.go).
// With allprop, where nothing was named as not found, the getctag is added.
// Anything it does not recognize it leaves exactly as it was.
func withChangeTags(body []byte, tags map[string]collectionTag, isAll bool) []byte {
	return responseBlock.ReplaceAllFunc(body, func(block []byte) []byte {
		found := hrefOfBlock.FindSubmatch(block)
		if found == nil {
			return block
		}
		collection, ok := tags[strings.TrimSpace(string(found[1]))]
		// Only a collection the library found: a response with no properties
		// at all is a refusal, and a refusal is not told anything.
		if !ok || !bytes.Contains(block, []byte("<propstat")) {
			return block
		}
		answered := []foundProperty{{missing: missingChangeTag, value: `<getctag xmlns="` + changeTagNamespace + `">` + collection.tag + `</getctag>`}}
		if collection.kind == calendarsSegment {
			answered = append(answered,
				foundProperty{missing: missingSyncToken, value: `<sync-token xmlns="DAV:">` + syncTokenOf(collection.tag) + `</sync-token>`},
				foundProperty{missing: missingSupportedReportSet, value: calendarSupportedReportSet})
		}
		isAsked := make([]bool, len(answered))
		isAsked[0] = isAll
		rewritten := propstatBlock.ReplaceAllFunc(block, func(propstat []byte) []byte {
			status := statusOfBlock.FindSubmatch(propstat)
			if status == nil || !bytes.Contains(status[1], []byte(" 404 ")) {
				return propstat
			}
			for index, property := range answered {
				if property.missing.Match(propstat) {
					isAsked[index] = true
					propstat = property.missing.ReplaceAll(propstat, nil)
				}
			}
			// A not found part with nothing left in it says nothing.
			if emptyProp.Match(propstat) {
				return nil
			}
			return propstat
		})
		properties := ""
		for index, property := range answered {
			if isAsked[index] {
				properties += property.value
			}
		}
		if properties == "" {
			return block
		}
		return withFoundProperties(rewritten, properties)
	})
}

// foundProperty is a property the library does not answer, as it writes it
// in the not found part, and the value put in the found part instead.
type foundProperty struct {
	missing *regexp.Regexp
	value   string
}

// missingProperty matches a property the library wrote empty in a not found
// part.
func missingProperty(local, namespace string) *regexp.Regexp {
	return regexp.MustCompile(`<` + local + `\b[^>]*xmlns="` + regexp.QuoteMeta(namespace) + `"[^>]*(?:/>|>\s*</` + local + `>)`)
}

var (
	missingSyncToken          = missingProperty("sync-token", "DAV:")
	missingSupportedReportSet = missingProperty("supported-report-set", "DAV:")
)

// calendarSupportedReportSet names the reports a calendar answers.
const calendarSupportedReportSet = `<supported-report-set xmlns="DAV:">` +
	`<supported-report><report><calendar-multiget xmlns="` + calendarNS + `"/></report></supported-report>` +
	`<supported-report><report><calendar-query xmlns="` + calendarNS + `"/></report></supported-report>` +
	`<supported-report><report><sync-collection/></report></supported-report>` +
	`</supported-report-set>`

// withFoundProperties puts properties into a response's 200 part, making the
// part when the response has none.
func withFoundProperties(block []byte, properties string) []byte {
	isPlaced := false
	placed := propstatBlock.ReplaceAllFunc(block, func(propstat []byte) []byte {
		status := statusOfBlock.FindSubmatch(propstat)
		if isPlaced || status == nil || !bytes.Contains(status[1], []byte(" 200 ")) {
			return propstat
		}
		end := bytes.LastIndex(propstat, []byte("</prop>"))
		if end < 0 {
			return propstat
		}
		isPlaced = true
		return append(append(append([]byte{}, propstat[:end]...), properties...), propstat[end:]...)
	})
	if isPlaced {
		return placed
	}
	part := `<propstat xmlns="DAV:"><prop xmlns="DAV:">` + properties + `</prop><status>HTTP/1.1 200 OK</status></propstat>`
	end := bytes.LastIndex(placed, []byte("</response>"))
	if end < 0 {
		return placed
	}
	return append(append(append([]byte{}, placed[:end]...), part...), placed[end:]...)
}

// servePropfind runs the library's handler for a PROPFIND of a person's
// collections and rewrites the answer: the change tags of the calendars and
// address books it names, and, for a calendar the person may only read, no
// write privilege (privileges.go).
func (self *component) servePropfind(response http.ResponseWriter, request *http.Request, signedIn *session, requestBody []byte, readOnly map[string]bool, handler http.Handler) {
	buffered := &bufferedResponse{header: http.Header{}}
	handler.ServeHTTP(buffered, request)
	body := buffered.body.Bytes()
	if buffered.httpStatus == http.StatusMultiStatus {
		if len(readOnly) > 0 {
			body = withoutWritePrivilege(body, readOnly)
		}
		tags, err := self.changeTagsOf(request.Context(), signedIn, body)
		if err != nil {
			// The library's answer stands; the phone is only not told the tag.
			log.Warningf("cannot read the change tags of the collections asked about: %s", err)
		} else if len(tags) > 0 {
			body = withChangeTags(body, tags, isAllProp(requestBody))
		}
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
