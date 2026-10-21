package tools

import "sync"

// RequestLedger is what the server remembers of the requests a person's
// tab made, so that the agent is shown only the ones the server saw
// created with an address that is not the server's own.
//
// The tab's network events arrive in windows, and a page can push a
// request's first events out of the window while the request goes on: a
// WebSocket opened by the panel's frame, whose Authorization header is in a
// later frame event, is no longer recognized as a request to the server
// once its creation event is gone. So a request is shown only when its
// creation was seen, and what was never seen created is not shown.
//
// A session is one debugging session of one tab in the extension; its key
// comes with the extension's answer. The zero value is ready to use.
type RequestLedger struct {
	mutex    sync.Mutex
	sessions map[string]*RequestSession
	// order lists the session keys, least recently used first.
	order []string
}

const (
	// ledgerSessionsKept is how many sessions the ledger remembers; a person
	// has a few tabs, and the least recently used one is forgotten first.
	ledgerSessionsKept = 8
	// ledgerRequestsKept is how many requests one session remembers. The
	// oldest is forgotten first, and a forgotten request is not shown.
	ledgerRequestsKept = 20000
)

// RequestSession is the requests one debugging session has shown the
// server, each with the places (hostnames, or the markers the browser tool
// uses for what it cannot place) its events named.
type RequestSession struct {
	places map[string][]string
	// order lists the request ids, oldest first.
	order []string
}

// With runs the function on the session with this key, made empty when the
// ledger has not seen the key before, while holding the ledger's lock.
func (self *RequestLedger) With(sessionKey string, function func(session *RequestSession)) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.sessions == nil {
		self.sessions = map[string]*RequestSession{}
	}
	session := self.sessions[sessionKey]
	if session == nil {
		session = &RequestSession{places: map[string][]string{}}
		self.sessions[sessionKey] = session
		self.order = append(self.order, sessionKey)
		for len(self.order) > ledgerSessionsKept {
			delete(self.sessions, self.order[0])
			self.order = self.order[1:]
		}
	} else {
		for index, key := range self.order {
			if key == sessionKey {
				self.order = append(append(self.order[:index:index], self.order[index+1:]...), sessionKey)
				break
			}
		}
	}
	function(session)
}

// Reset forgets every session, as when the agent lets the protocol go.
func (self *RequestLedger) Reset() {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.sessions = nil
	self.order = nil
}

// PlacesOf are the places named by the events of a request in any session,
// and whether the request is known in any of them.
func (self *RequestLedger) PlacesOf(requestID string) ([]string, bool) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	var places []string
	isKnown := false
	for _, session := range self.sessions {
		if known, ok := session.places[requestID]; ok {
			isKnown = true
			places = append(places, known...)
		}
	}
	return places, isKnown
}

// Create remembers a request whose creation was seen, with the place it was
// created for. A request already known only gains the place.
func (self *RequestSession) Create(requestID, place string) {
	if _, ok := self.places[requestID]; !ok {
		self.order = append(self.order, requestID)
		for len(self.order) > ledgerRequestsKept {
			delete(self.places, self.order[0])
			self.order = self.order[1:]
		}
	}
	self.addPlace(requestID, place)
}

// Add gives a request that is known one more place, and does nothing for a
// request whose creation was not seen.
func (self *RequestSession) Add(requestID, place string) {
	if _, ok := self.places[requestID]; ok {
		self.addPlace(requestID, place)
	}
}

// addPlace notes a place once: the same events are read again on every poll.
func (self *RequestSession) addPlace(requestID, place string) {
	for _, known := range self.places[requestID] {
		if known == place {
			return
		}
	}
	self.places[requestID] = append(self.places[requestID], place)
}

// Places are the places a request's events named, and whether it is known.
func (self *RequestSession) Places(requestID string) ([]string, bool) {
	places, ok := self.places[requestID]
	return places, ok
}
