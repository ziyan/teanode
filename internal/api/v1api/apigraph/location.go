package apigraph

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
)

// Where a person is and what they read in, learned from the browser.
//
// The dashboard sends the browser's time zone and the language the person
// reads it in with every API call. The server keeps them on the account so
// that a run with nobody present, a scheduled brief or a held reply's
// notification, tells time and writes in the person's own terms. Written
// when either changes, and otherwise at most once an hour per account: a
// person crossing a border is not an administrative change, and a write on
// every request would be a write on every request.

// TimezoneHeader carries the browser's IANA zone name.
const TimezoneHeader = "X-Timezone"

// LanguageHeader carries the language the dashboard is shown in, which the
// person may have chosen over the browser's own; Accept-Language is only
// the fallback for a client that does not send it.
const LanguageHeader = "X-Language"

// locationTouchInterval is how often, at most, one account's location is
// written.
const locationTouchInterval = time.Hour

type locationTouches struct {
	mutex sync.Mutex
	last  map[string]locationTouch
}

// locationTouch is what was last written for one account, and when.
type locationTouch struct {
	at       time.Time
	timezone string
	locale   string
}

// withLocation wraps a handler so that a signed-in request's zone and
// language headers are recorded before the request is served.
func (self *graph) withLocation(handler http.HandlerFunc) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		self.touchLocation(request)
		handler(response, request)
	}
}

func (self *graph) touchLocation(request *http.Request) {
	username := request.Header.Get(api.AuthenticatedUsernameHeader)
	if username == "" {
		return
	}
	timezone := strings.TrimSpace(request.Header.Get(TimezoneHeader))
	if timezone != "" {
		if _, err := time.LoadLocation(timezone); err != nil {
			timezone = ""
		}
	}
	locale := firstLanguage(request.Header.Get(LanguageHeader))
	if locale == "" {
		locale = firstLanguage(request.Header.Get("Accept-Language"))
	}
	if timezone == "" && locale == "" {
		return
	}
	now := time.Now()
	self.locations.mutex.Lock()
	if self.locations.last == nil {
		self.locations.last = map[string]locationTouch{}
	}
	if last, ok := self.locations.last[username]; ok && now.Sub(last.at) < locationTouchInterval &&
		last.timezone == timezone && last.locale == locale {
		self.locations.mutex.Unlock()
		return
	}
	self.locations.last[username] = locationTouch{at: now, timezone: timezone, locale: locale}
	self.locations.mutex.Unlock()

	if err := self.database.Transaction(func(tx db.Transaction) error {
		user, err := tx.GetUserByUsername(username)
		if err != nil || user == nil {
			return err
		}
		return tx.TouchUserLocation(user.ID, timezone, locale, now)
	}); err != nil {
		log.Warningf("cannot record where %s is: %s", username, err)
	}
}

// firstLanguage is the first tag of an Accept-Language header, reduced to
// its language, or empty.
func firstLanguage(header string) string {
	first, _, _ := strings.Cut(header, ",")
	first, _, _ = strings.Cut(strings.TrimSpace(first), ";")
	first = strings.ToLower(strings.TrimSpace(first))
	if first == "" || first == "*" || len(first) > 16 {
		return ""
	}
	return first
}
