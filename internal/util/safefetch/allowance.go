package safefetch

import (
	"crypto/tls"
	"net"
	"net/http"
	"strings"
	"syscall"
)

// What an operator has decided this server may reach although the guard
// would otherwise refuse it.
//
// The guard exists because a URL out of a message is written by a stranger,
// and the network this server sits in is one they cannot reach. That does
// not hold for every fetch: equipment on the operator's own network -- a
// camera controller, a printer, something with an API and no public name --
// is a thing they may legitimately want a skill or the browser to reach, and
// the address of it is theirs rather than a stranger's.
//
// So an allowance is a list the operator writes, and it is handed to the few
// callers whose addresses come from the operator rather than from the post.
// Nothing that acts on an address out of a message takes one: the remote
// image proxy, the one-click unsubscribe, web_fetch. Those stay shut,
// because widening them is what the guard is for.

// Allowance is a set of addresses permitted although they are private.
type Allowance struct {
	networks []*net.IPNet
	// hosts are permitted by name, without being resolved: a name that is
	// allowed is allowed wherever it points, which is what an operator
	// means by naming their own equipment.
	hosts map[string]bool
}

// ParseAllowance reads an operator's list: an address, a CIDR, or a name.
// What cannot be read as either of the first two is taken as a name.
func ParseAllowance(entries []string) *Allowance {
	if len(entries) == 0 {
		return nil
	}
	self := &Allowance{hosts: map[string]bool{}}
	for _, entry := range entries {
		entry = strings.TrimSpace(strings.ToLower(entry))
		if entry == "" {
			continue
		}
		if _, network, err := net.ParseCIDR(entry); err == nil {
			self.networks = append(self.networks, network)
			continue
		}
		if ip := net.ParseIP(entry); ip != nil {
			bits := 32
			if ip.To4() == nil {
				bits = 128
			}
			self.networks = append(self.networks, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
			continue
		}
		self.hosts[entry] = true
	}
	if len(self.networks) == 0 && len(self.hosts) == 0 {
		return nil
	}
	return self
}

// Networks is what was allowed by address, for a caller that has to do its
// own checking -- the browser's proxy, which sees addresses rather than
// making the connections itself.
func (self *Allowance) Networks() []*net.IPNet {
	if self == nil {
		return nil
	}
	return self.networks
}

// Hosts is what was allowed by name, for the same reason. A copy, because
// a caller keeping it for the life of a browser must not be able to widen
// what this allows by writing into it.
func (self *Allowance) Hosts() map[string]bool {
	copied := map[string]bool{}
	if self == nil {
		return copied
	}
	for host := range self.hosts {
		copied[host] = true
	}
	return copied
}

// PermitsAddress says whether one dialled address is allowed by this list.
func (self *Allowance) PermitsAddress(address string) bool {
	if self == nil {
		return false
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, network := range self.networks {
		if network.Contains(ip) {
			return true
		}
	}
	return self.permitsResolved(ip)
}

// permitsResolved says whether one of the allowed names points here now.
//
// The guard runs on the address being dialled, which is the whole reason it
// cannot be fooled by a name that resolves differently a moment later -- and
// it is also why a name in the operator's list did nothing on its own: by
// the time the check runs there is no name left, only the address it
// resolved to. The browser has a name to check before it dials; a skill's
// http step does not.
//
// So the names are resolved here instead, and only here: this is reached
// only for an address the guard is about to refuse, which means a private
// one. A public host cannot arrive at this code at all, so a name that
// resolves outward gains nothing by being listed, and the rebinding this
// package exists to stop still cannot happen -- what is permitted is the
// address an operator's own name points at, at the moment of the dial.
func (self *Allowance) permitsResolved(ip net.IP) bool {
	if len(self.hosts) == 0 {
		return false
	}
	for host := range self.hosts {
		resolved, err := net.LookupIP(host)
		if err != nil {
			continue
		}
		for _, candidate := range resolved {
			if candidate.Equal(ip) {
				return true
			}
		}
	}
	return false
}

// PermitsHost says whether a name is allowed as it stands.
func (self *Allowance) PermitsHost(host string) bool {
	if self == nil {
		return false
	}
	return self.hosts[strings.TrimSpace(strings.ToLower(host))]
}

// AllowAddressWith is AllowAddress, with what the operator permitted.
func AllowAddressWith(allowance *Allowance, address string) error {
	if allowance.PermitsAddress(address) {
		return nil
	}
	return AllowAddress(address)
}

// ClientAllowing is Client, reaching what the operator permitted as well.
//
// The check stays in the dialler's Control function for the reason Client
// explains: it is the only place a redirect cannot get around, and it sees
// the address rather than a name that may have resolved differently.
func ClientAllowing(allowance *Allowance) *http.Client {
	if allowance == nil {
		return Client()
	}
	client := Client()
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		return client
	}
	dialer := &net.Dialer{
		Timeout:   Timeout,
		KeepAlive: Timeout,
		Control: func(_, address string, _ syscall.RawConn) error {
			return AllowAddressWith(allowance, address)
		},
	}
	transport.DialContext = dialer.DialContext
	return client
}

// ClientAllowingUnverified is ClientAllowing, and for the hosts an operator
// has named it does not check the certificate.
//
// This is a real loss and worth saying plainly: for those hosts, anything
// that can answer at the address can pretend to be them, and TLS becomes
// encryption without identity. It exists because some equipment cannot be
// verified at all -- a controller that ships a certificate for 127.0.0.1 can
// never present a valid one for the address it is actually reached at -- and
// the alternative is not reaching it.
//
// Scoped twice over. Only the hosts on the list, and only the callers that
// are handed one: a skill going to the endpoint it declares. Nothing that
// follows an address out of somebody else's mail is given it.
func ClientAllowingUnverified(allowance *Allowance, unverified *Allowance, host string) *http.Client {
	client := ClientAllowing(allowance)
	if !unverified.PermitsHost(host) && !unverified.PermitsAddress(host) {
		return client
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		return client
	}
	transport.TLSClientConfig = &tls.Config{
		// Named, deliberately, for this host and no other: the client this
		// returns is built per request.
		InsecureSkipVerify: true, // #nosec G402 -- the operator named this host
		MinVersion:         tls.VersionTLS12,
	}
	return client
}
