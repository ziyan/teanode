package apigraph

import (
	"net/http"
	"time"
)

// socketRecheckInterval is how long an open socket goes on trusting what
// signed it in before asking again. A variable so that a test can shorten it.
var socketRecheckInterval = time.Minute

// socketCredential is what signed a long-lived socket in: the token it said
// in its first message, or the request's own cookie when it said none.
//
// A socket looked at its caller once, at the hello, and every ping after that
// only moved the deadline, so revoking a token, ending a session, disabling
// the account or changing the password left the sockets already open working
// for as long as they cared to stay. The socket asks again with the same
// function the hello used, and closes when the answer is no longer the same
// person.
//
// Used only from the goroutine that reads the socket.
type socketCredential struct {
	graph    *graph
	request  *http.Request
	token    string
	username string

	checkedAt time.Time
}

func (self *graph) newSocketCredential(request *http.Request, token, username string) *socketCredential {
	return &socketCredential{graph: self, request: request, token: token, username: username, checkedAt: time.Now()}
}

// StillHolds says whether the credential still names the same person. It asks
// at most once every socketRecheckInterval, and says yes in between.
func (self *socketCredential) StillHolds() bool {
	if time.Since(self.checkedAt) < socketRecheckInterval {
		return true
	}
	self.checkedAt = time.Now()
	return self.resolve() == self.username
}

func (self *socketCredential) resolve() string {
	if self.token != "" {
		return self.graph.usernameOfToken(self.request, self.token)
	}
	if self.graph.authenticator == nil {
		return ""
	}
	username, _ := self.graph.authenticator.Authenticate(self.request)
	return username
}
