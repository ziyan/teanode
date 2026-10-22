package api

import (
	"net/http"
	"net/url"
	"strings"
)

// FromThisServer reports whether a handshake or a write came from a page this
// server served. A request with no Origin at all is not a browser, so nothing
// attached a cookie to it on its own; it is let through here and has to
// prove who it is some other way.
func FromThisServer(request *http.Request) bool {
	origin := request.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}
	return strings.EqualFold(parsed.Host, request.Host)
}

// IsCrossSiteWrite reports whether a browser says a request that changes
// something was started by a page that is not this server's own.
//
// The session cookie is SameSite=Lax, which keeps it off a form posted from
// an unrelated site but not off one posted from a sibling host of the same
// registrable domain. Such a page can make a browser send a form post with
// the cookie on it, and the server then acts as the person.
//
// Sec-Fetch-Site is the browser's own word for it and cannot be set by a
// page; older browsers lack it but always send Origin on a write.
func IsCrossSiteWrite(request *http.Request) bool {
	switch request.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		return false
	}
	if site := request.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		return true
	}
	return !FromThisServer(request)
}
