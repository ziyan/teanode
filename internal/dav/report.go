package dav

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/ziyan/teanode/internal/models"
)

// Answering a REPORT here rather than letting the protocol library do it.
//
// The library serializes a card by re-encoding the parsed form with its own
// encoder, and that encoder is the one this server replaced: it does not
// quote a parameter value that needs quoting, so a card carrying
// EMAIL;TYPE="work;main" comes back unquoted, and a client that reads that
// and writes it back stores an EMAIL property with no value at all. A GET is
// already answered from the stored bytes for the same reason; a REPORT is the
// request a phone actually uses, so it has to be too.
//
// Two reports are answered: addressbook-multiget, which asks for cards by
// name, and addressbook-query, which asks for the ones matching a filter.
// Anything else is left to the library.

// reportRequest is as much of a REPORT body as needs reading.
type reportRequest struct {
	XMLName xml.Name
	Props   struct {
		Names []xml.Name `xml:",any"`
	} `xml:"DAV: prop"`
	Hrefs []string `xml:"DAV: href"`
}

// multistatus is the answer.
type multistatus struct {
	XMLName   xml.Name      `xml:"DAV: multistatus"`
	Responses []davResponse `xml:"DAV: response"`
}

type davResponse struct {
	Href     string     `xml:"DAV: href"`
	PropStat []propStat `xml:"DAV: propstat"`
	Status   string     `xml:"DAV: status,omitempty"`
}

type propStat struct {
	Prop   properties `xml:"DAV: prop"`
	Status string     `xml:"DAV: status"`
}

type properties struct {
	ETag        string `xml:"DAV: getetag,omitempty"`
	ContentType string `xml:"DAV: getcontenttype,omitempty"`
	Length      string `xml:"DAV: getcontentlength,omitempty"`
	AddressData string `xml:"urn:ietf:params:xml:ns:carddav address-data,omitempty"`
}

// serveReport answers a REPORT if it is one this package handles, and says so.
func (self *component) serveReport(writer http.ResponseWriter, request *http.Request,
	backing *backend, body []byte) bool {
	var asked reportRequest
	if err := xml.Unmarshal(body, &asked); err != nil {
		return false
	}
	wantsData := false
	wantsETag := false
	wantsLength := false
	for _, name := range asked.Props.Names {
		switch {
		case name.Space == "urn:ietf:params:xml:ns:carddav" && name.Local == "address-data":
			wantsData = true
		case name.Local == "getetag":
			wantsETag = true
		case name.Local == "getcontentlength":
			wantsLength = true
		}
	}

	ctx := request.Context()
	signedIn := backing.who(ctx)
	var answers []davResponse

	switch {
	case asked.XMLName.Space == "urn:ietf:params:xml:ns:carddav" && asked.XMLName.Local == "addressbook-multiget":
		// Named cards. Each href is the client's, so each is resolved the
		// long way round -- through the address book, which is checked to
		// belong to whoever is signed in -- rather than trusted.
		for _, href := range asked.Hrefs {
			path := strings.TrimSpace(href)
			contact, err := backing.storedCard(ctx, path)
			if err != nil {
				status, _ := statusOf(err)
				answers = append(answers, davResponse{Href: path, Status: statusLine(status)})
				continue
			}
			answers = append(answers, found(signedIn, contact, wantsETag, wantsLength, wantsData))
		}

	case asked.XMLName.Space == "urn:ietf:params:xml:ns:carddav" && asked.XMLName.Local == "addressbook-query":
		// Every card in the book. A filter is not applied here: the
		// library's own matching reads the parsed card, and answering with
		// everything is allowed -- a client is expected to cope with more
		// than it asked for -- where answering with a mangled card is not.
		contacts, err := backing.storedCards(ctx, request.URL.Path)
		if err != nil {
			status, message := statusOf(err)
			http.Error(writer, message, status)
			return true
		}
		for _, contact := range contacts {
			answers = append(answers, found(signedIn, contact, wantsETag, wantsLength, wantsData))
		}

	default:
		return false
	}

	encoded, err := xml.Marshal(&multistatus{Responses: answers})
	if err != nil {
		log.Errorf("a report could not be written: %s", err)
		http.Error(writer, "this server could not do that just now", http.StatusInternalServerError)
		return true
	}
	writer.Header().Set("Content-Type", "application/xml; charset=utf-8")
	writer.WriteHeader(http.StatusMultiStatus)
	if _, err := writer.Write([]byte(xml.Header)); err != nil {
		return true
	}
	_, _ = writer.Write(encoded)
	return true
}

// found is one card in an answer, with the bytes exactly as they are stored.
func found(signedIn *session, contact *models.Contact, etag, length, data bool) davResponse {
	held := properties{}
	if etag {
		held.ETag = strconv.Quote(contact.ETag)
	}
	if length {
		held.Length = strconv.Itoa(len(contact.Card))
	}
	if data {
		held.ContentType = "text/vcard; charset=utf-8"
		held.AddressData = contact.Card
	}
	return davResponse{
		Href:     contactPath(signedIn.userID, contact.AddressBookID, contact.ID),
		PropStat: []propStat{{Prop: held, Status: statusLine(http.StatusOK)}},
	}
}

func statusLine(status int) string {
	return fmt.Sprintf("HTTP/1.1 %d %s", status, http.StatusText(status))
}
