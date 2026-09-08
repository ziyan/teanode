package safefetch

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"syscall"
	"time"
)

// Fetching something a stranger named.

//
// A URL out of a message is written by whoever sent the message, and this
// server sits inside a network that person cannot reach. Server-side request
// forgery is the whole risk: a link to http://169.254.169.254/ on a cloud host
// serves credentials, and a link to something on the loopback address is a
// request to this program from itself.
//
// The guards, in the order they apply: the scheme must be http or https, so no
// file:, no gopher:, no data:; every resolved address is checked, on the
// address actually dialled rather than on the hostname, so a name that
// resolves to 127.0.0.1 fails; and redirects are followed but re-checked,
// because a public host redirecting inward is the classic way in.
//
// Used by the remote image proxy and by the one-click unsubscribe request:
// both take an address out of somebody else's mail and go to it.

const (
	// A fetch that has not answered by now is not going to.
	Timeout = 10 * time.Second
)

// ParseTarget accepts only what a message can legitimately link an
// image to. Everything else — a scheme with a local meaning, a userinfo
// section, a missing host — is refused before anything is dialled.
func ParseTarget(raw string) (*url.URL, error) {
	if raw == "" || len(raw) > 2048 {
		return nil, errors.New("safefetch: no address")
	}
	target, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if target.Scheme != "http" && target.Scheme != "https" {
		return nil, errors.New("safefetch: not an http address")
	}
	if target.Host == "" {
		return nil, errors.New("safefetch: no host")
	}
	// Credentials in the URL would be sent to the host, and a URL carrying
	// them is not an image reference; it is somebody trying something.
	if target.User != nil {
		return nil, errors.New("safefetch: credentials in the address")
	}
	return target, nil
}

// Client dials through a control function, which is the only place the
// address check can go that a redirect cannot get around: it runs for every
// connection the client makes, including the ones a redirect causes, and it
// sees the address actually being connected to rather than a name that was
// resolved a moment ago and might resolve differently now.
func Client() *http.Client {
	dialer := &net.Dialer{
		Timeout:   Timeout,
		KeepAlive: Timeout,
		Control: func(_, address string, _ syscall.RawConn) error {
			return AllowAddress(address)
		},
	}
	return &http.Client{
		Timeout: Timeout,
		Transport: &http.Transport{
			DialContext:           dialer.DialContext,
			TLSHandshakeTimeout:   Timeout,
			ResponseHeaderTimeout: Timeout,
			DisableKeepAlives:     true,
		},
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			// A redirect to somewhere unreachable is refused by the dialler
			// anyway; this stops a chain being used to spend time, and keeps
			// the scheme check applying at every hop.
			if len(via) >= 5 {
				return errors.New("safefetch: too many redirects")
			}
			if request.URL.Scheme != "http" && request.URL.Scheme != "https" {
				return errors.New("safefetch: redirected to a scheme this will not follow")
			}
			return nil
		},
	}
}

// AllowAddress refuses anything that is not a public address.
//
// On the address rather than on the hostname: a name is whatever DNS says at
// the moment it is asked, and a name that resolved publicly a moment ago can
// resolve to 127.0.0.1 for the connection that follows. This runs on the
// address being dialled, so there is no gap to race.
func AllowAddress(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return errors.New("safefetch: not an address")
	}
	if !ip.IsGlobalUnicast() ||
		ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() ||
		ip.IsUnspecified() ||
		isSharedAddressSpace(ip) {
		return errors.New("safefetch: not a public address")
	}
	return nil
}

// isSharedAddressSpace covers the ranges net.IP does not have a predicate for
// and which are still not the public internet: the carrier-grade NAT block,
// and IPv4-mapped IPv6 addresses whose embedded address is itself private.
func isSharedAddressSpace(ip net.IP) bool {
	if mapped := ip.To4(); mapped != nil {
		// 100.64.0.0/10, RFC 6598.
		if mapped[0] == 100 && mapped[1]&0xc0 == 64 {
			return true
		}
		// 192.0.0.0/24 and 198.18.0.0/15, both reserved and both routable
		// inside somebody's network.
		if mapped[0] == 192 && mapped[1] == 0 && mapped[2] == 0 {
			return true
		}
		if mapped[0] == 198 && mapped[1]&0xfe == 18 {
			return true
		}
		return false
	}
	// Unique local addresses, fc00::/7: private in every way that matters,
	// and net.IP.IsPrivate already says so. Kept for the mapped case above.
	return false
}
