package apimail

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"github.com/ziyan/teanode/internal/util/safefetch"
)

// The remote image proxy.
//
// A message from a stranger links to images on the sender's server. Letting
// the browser fetch them directly hands that server the reader's address, the
// reader's user agent, and the fact that this particular message was opened at
// this particular moment — which is what a tracking pixel is for. Fetching
// them here instead means the sender learns that the mail server looked, which
// it could have worked out anyway, and nothing about who was reading.
//
// It also means the dashboard's content security policy can say img-src 'self'
// and mean it: with no proxy, allowing remote images means allowing the page
// to load an image from anywhere, and a policy with https: in it is not a
// policy.
//
// Only after the reader asks. Nothing here fires on its own — the blocked
// images stay blocked until somebody presses the button, which is the same
// decision as before; this only changes who does the fetching.

const (
	// A fetch that has not answered by now is not going to. Short, because a
	// page of blocked images makes one of these per image and a reader is
	// waiting for all of them.
	remoteTimeout = 10 * time.Second

	// Enough for any image a message legitimately embeds, and small enough
	// that a hostile server cannot use this to fill a disk or a pipe.
	remoteMaximumSize = 8 << 20
)

// remoteView fetches one image a message linked to, for a reader who asked.
//
// SSRF is the whole risk here: the address comes out of mail written by a
// stranger, and this server sits inside a network that stranger cannot reach.
// The guards, in the order they apply:
//
//	the scheme must be http or https  — no file:, no gopher:, no data:
//	every resolved address is checked  — not just the first, and the check is
//	                                     on the addresses actually dialled
//	                                     rather than on the hostname, so a
//	                                     name resolving to 127.0.0.1 fails
//	redirects are followed but re-checked — a public host redirecting to
//	                                     169.254.169.254 is the classic way in
//	only images come back              — the reply is served as an image or
//	                                     not at all, so this cannot be used to
//	                                     read an internal page's HTML
//	the reply is capped and the request has no credentials of any kind
func (self *mail) remoteView(response http.ResponseWriter, request *http.Request) {
	if err := self.requireOperator(request); err != nil {
		http.Error(response, "not logged in", http.StatusUnauthorized)
		return
	}

	// The message has to exist and be one this operator can see. Not because
	// the fetch needs it, but because an endpoint that fetches any address
	// for anybody with a session is a worse thing to have than one that does
	// it only from a message they were already reading.
	variables := mux.Vars(request)
	if _, _, err := self.load(request, variables["mailId"]); err != nil {
		self.fail(response, variables["mailId"], err)
		return
	}

	target, err := safefetch.ParseTarget(request.URL.Query().Get("url"))
	if err != nil {
		http.Error(response, "not a fetchable address", http.StatusBadRequest)
		return
	}

	timed, cancel := context.WithTimeout(request.Context(), remoteTimeout)
	defer cancel()

	outgoing, err := http.NewRequestWithContext(timed, http.MethodGet, target.String(), nil)
	if err != nil {
		http.Error(response, "not a fetchable address", http.StatusBadRequest)
		return
	}
	// No cookies, no authorization, no referer: nothing that could carry this
	// server's identity to a stranger, and nothing that says which message
	// was open.
	outgoing.Header.Set("User-Agent", "teanode")
	outgoing.Header.Set("Accept", "image/*")

	fetched, err := safefetch.Client().Do(outgoing)
	if err != nil {
		log.Debugf("failed to fetch remote image %q: %s", target.Redacted(), err)
		http.Error(response, "could not fetch it", http.StatusBadGateway)
		return
	}
	defer func() {
		if err := fetched.Body.Close(); err != nil {
			log.Debugf("failed to close remote image body: %s", err)
		}
	}()

	if fetched.StatusCode != http.StatusOK {
		http.Error(response, "could not fetch it", http.StatusBadGateway)
		return
	}

	// Served as an image or not at all. Without this the proxy would happily
	// relay an internal service's JSON to whoever wrote the message — which
	// is the same hole SSRF opens, one step later.
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(fetched.Header.Get("Content-Type"), ";")[0]))
	if !displayable[contentType] || !strings.HasPrefix(contentType, "image/") {
		http.Error(response, "not an image", http.StatusUnsupportedMediaType)
		return
	}

	response.Header().Set("Content-Type", contentType)
	response.Header().Set("X-Content-Type-Options", "nosniff")
	// Not cached by anything shared: which images were fetched is a record of
	// which message somebody opened.
	response.Header().Set("Cache-Control", "private, max-age=300")
	response.WriteHeader(http.StatusOK)

	if _, err := io.Copy(response, io.LimitReader(fetched.Body, remoteMaximumSize)); err != nil {
		log.Debugf("failed to write remote image %q: %s", target.Redacted(), err)
	}
}
