package dav

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/emersion/go-vcard"
	"github.com/emersion/go-webdav/carddav"

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
		if len(asked.Hrefs) > maximumHrefs {
			http.Error(writer, "that report asks for too many contacts at once", http.StatusForbidden)
			return true
		}
		for _, href := range asked.Hrefs {
			// Percent-decoded before it is used as a path, and given back
			// percent-encoded. A client sends back what the listing gave
			// it, and the listing is a URL: a contact named "ada k.vcf"
			// is listed as ada%20k.vcf, so taking the href literally
			// looked for a file with a percent sign in its name and never
			// found it.
			path := decodeHref(strings.TrimSpace(href))
			contact, err := backing.storedCard(ctx, path)
			if err != nil {
				status, _ := statusOf(err)
				answers = append(answers, davResponse{Href: encodeHref(path), Status: statusLine(status)})
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
		var filter queryFilter
		if err := xml.Unmarshal(body, &filter); err != nil {
			http.Error(writer, "that filter could not be read", http.StatusBadRequest)
			return true
		}
		for _, contact := range contacts {
			matched, err := matchesQuery(&filter, contact)
			if err != nil {
				log.Debugf("a contact could not be matched against a filter: %s", err)
			}
			if !matched {
				continue
			}
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
		Href:     encodeHref(contactPath(signedIn.userID, contact.AddressBookID, contact.ID)),
		PropStat: []propStat{{Prop: held, Status: statusLine(http.StatusOK)}},
	}
}

func statusLine(status int) string {
	return fmt.Sprintf("HTTP/1.1 %d %s", status, http.StatusText(status))
}

// maximumHrefs is how many contacts one report may name. Each costs a look
// through the database, and a body of eight megabytes holds a great many
// hrefs; a phone asks for the handful that changed.
const maximumHrefs = 5000

// decodeHref is the path a client meant. An href is a URL reference, so what
// arrives is percent-encoded, and a full URL is allowed as well as a path.
func decodeHref(href string) string {
	parsed, err := url.Parse(href)
	if err != nil {
		return href
	}
	return parsed.Path
}

// encodeHref writes a path as a client may read it back and send it again.
func encodeHref(path string) string {
	return (&url.URL{Path: path}).EscapedPath()
}

// queryFilter is the filter of an addressbook-query, as it arrives.
//
// It is read here rather than by the protocol library because the library
// reads the whole request and then answers it with its own encoder, which is
// the one this server replaced. Only the parts a client actually sends are
// read: a filter of property filters, each with text matches.
type queryFilter struct {
	Filter struct {
		Test        string `xml:"test,attr"`
		PropFilters []struct {
			Name         string    `xml:"name,attr"`
			Test         string    `xml:"test,attr"`
			IsNotDefined *struct{} `xml:"urn:ietf:params:xml:ns:carddav is-not-defined"`
			TextMatches  []struct {
				Text      string `xml:",chardata"`
				Negate    string `xml:"negate-condition,attr"`
				MatchType string `xml:"match-type,attr"`
			} `xml:"urn:ietf:params:xml:ns:carddav text-match"`
		} `xml:"urn:ietf:params:xml:ns:carddav prop-filter"`
	} `xml:"urn:ietf:params:xml:ns:carddav filter"`
}

// asQuery is the filter in the form the library's matcher takes.
func (self *queryFilter) asQuery() *carddav.AddressBookQuery {
	query := &carddav.AddressBookQuery{FilterTest: carddav.FilterTest(self.Filter.Test)}
	for _, filter := range self.Filter.PropFilters {
		made := carddav.PropFilter{
			Name:         filter.Name,
			Test:         carddav.FilterTest(filter.Test),
			IsNotDefined: filter.IsNotDefined != nil,
		}
		for _, match := range filter.TextMatches {
			made.TextMatches = append(made.TextMatches, carddav.TextMatch{
				Text:            strings.TrimSpace(match.Text),
				NegateCondition: match.Negate == "yes",
				MatchType:       carddav.MatchType(match.MatchType),
			})
		}
		query.PropFilters = append(query.PropFilters, made)
	}
	return query
}

// matchesQuery says whether a stored card satisfies the filter.
//
// The card is decoded only in order to be matched; what is served is still
// the stored text. Answering a filtered query with the whole book would be
// allowed by the letter of the protocol -- a client must cope with more than
// it asked for -- but a client using the query as a directory search would
// then show every contact as a match, and every search would carry the whole
// address book across the network.
func matchesQuery(filter *queryFilter, contact *models.Contact) (bool, error) {
	if len(filter.Filter.PropFilters) == 0 {
		// No filter at all: everything, which is what a client asking for
		// the whole book sends.
		return true, nil
	}
	card, err := vcard.NewDecoder(strings.NewReader(contact.Card)).Decode()
	if err != nil {
		return true, err
	}
	return carddav.Match(filter.asQuery(), &carddav.AddressObject{Card: card})
}
