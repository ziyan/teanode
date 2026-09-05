package dns

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/miekg/dns"
	"golang.org/x/net/proxy"
)

// OutgoingIdentity is how this server's mail introduces itself to the servers
// it delivers to, and whether that introduction holds up.
//
// Three facts decide whether large receivers accept mail at all, and none of
// them was checked anywhere before this existed. They are not the same facts
// as the records a domain publishes: those say how mail reaches this server,
// and these say what a receiver sees when this server reaches out.
//
// The failure this was written for went unnoticed for weeks. Mail left from an
// address whose reverse-DNS name pointed at a different machine entirely, so
// forward-confirmed reverse DNS failed on every message, and the only sign of
// it was a rejection sitting in the delivery queue.
type OutgoingIdentity struct {
	// Address is what a receiver sees the connection come from, empty when it
	// could not be determined.
	Address string `json:"address,omitempty"`

	// Via says how mail leaves: "direct" from this machine, "proxy" through a
	// SOCKS5 proxy, or "relay" when another mail server sends it.
	Via string `json:"via"`

	// ReverseName is the name that address reverses to, empty when it has
	// none — which is itself the most common reason mail is refused.
	ReverseName string `json:"reverseName,omitempty"`

	// ForwardAddresses is what ReverseName resolves to. It has to contain
	// Address, or the reverse name is somebody else's and proves nothing.
	ForwardAddresses []string `json:"forwardAddresses,omitempty"`

	// Confirmed is the answer receivers actually compute: the address has a
	// reverse name, and that name resolves back to the same address.
	Confirmed bool `json:"confirmed"`

	// HelloName is the name this server announces in its SMTP greeting.
	HelloName string `json:"helloName,omitempty"`

	// HelloAddresses is what that name resolves to, and HelloMatches is
	// whether it includes the sending address. A greeting naming a host that
	// is somewhere else is a mismatch receivers notice.
	HelloAddresses []string `json:"helloAddresses,omitempty"`
	HelloMatches   bool     `json:"helloMatches"`

	// Unknown explains why this could not be established, empty when it
	// could. A relay is the ordinary reason: the relay sends the mail, from
	// its own address, and only its operator can say what that address is.
	Unknown string `json:"unknown,omitempty"`

	// CheckedAt is when this was last established.
	CheckedAt time.Time `json:"checkedAt"`
}

// Verified reports whether outgoing mail identifies itself acceptably: the
// sending address confirms, and the greeting names it.
func (self *OutgoingIdentity) Verified() bool {
	return self != nil && self.Confirmed && self.HelloMatches
}

// checkOutgoingIdentity establishes the three facts.
func (self *verifier) checkOutgoingIdentity(ctx context.Context) *OutgoingIdentity {
	configuration := self.config.Current()
	identity := &OutgoingIdentity{
		CheckedAt: time.Now(),
		HelloName: strings.TrimSuffix(configuration.Server.Name, "."),
		Via:       "direct",
	}

	// A relay sends the mail, from its own address, with its own reverse name
	// and its own greeting. Nothing here can be established from this side,
	// and guessing would be worse than saying so: an operator told their
	// reverse DNS is wrong when the mail does not leave from their address
	// would go and change something that was right.
	if configuration.SMTP.Relay.Enabled && strings.TrimSpace(configuration.SMTP.Relay.Host) != "" {
		identity.Via = "relay"
		identity.Unknown = fmt.Sprintf(
			"outgoing mail is handed to %s, which sends it from its own address. Whether that address is acceptable is a question for whoever runs it.",
			strings.TrimSpace(configuration.SMTP.Relay.Host))
		return identity
	}

	// The address mail leaves from is not the address mail arrives at
	// whenever a proxy carries the outgoing connections — the arrangement
	// anybody whose provider blocks port 25 ends up with. Asking through the
	// proxy is the only way to learn the address a receiver will see.
	proxyAddress := strings.TrimSpace(configuration.SMTP.SOCKS5Proxy)
	if proxyAddress != "" {
		identity.Via = "proxy"
		address, err := self.askThroughProxy(ctx, proxyAddress)
		if err != nil {
			identity.Unknown = fmt.Sprintf(
				"could not determine the address outgoing mail leaves from: the proxy at %s did not answer (%s)",
				proxyAddress, err)
			return identity
		}
		identity.Address = address
	} else {
		identity.Address = self.ExternalAddresses(ctx).IPv4
		if identity.Address == "" {
			identity.Unknown = "could not determine this server's external address"
			return identity
		}
	}

	identity.ReverseName = self.resolveReverse(ctx, identity.Address)
	if identity.ReverseName != "" {
		identity.ForwardAddresses, _ = self.resolveAddresses(ctx, identity.ReverseName)
		for _, address := range identity.ForwardAddresses {
			if address == identity.Address {
				identity.Confirmed = true
				break
			}
		}
	}

	if identity.HelloName != "" {
		identity.HelloAddresses, _ = self.resolveAddresses(ctx, identity.HelloName)
		for _, address := range identity.HelloAddresses {
			if address == identity.Address {
				identity.HelloMatches = true
				break
			}
		}
	}

	return identity
}

// resolveReverse asks what name an address reverses to. Empty means none,
// which is not an error: plenty of addresses have no reverse name, and having
// none is the finding.
func (self *verifier) resolveReverse(ctx context.Context, address string) string {
	name, err := dns.ReverseAddr(address)
	if err != nil {
		return ""
	}
	request := new(dns.Msg)
	request.SetQuestion(name, dns.TypePTR)
	result, _, err := self.client.ExchangeContext(ctx, request, self.settings.Nameserver)
	if err != nil {
		log.Debugf("failed to resolve the reverse name of %q: %s", address, err)
		return ""
	}
	for _, answer := range result.Answer {
		if record, ok := answer.(*dns.PTR); ok {
			return strings.TrimSuffix(record.Ptr, ".")
		}
	}
	return ""
}

// askThroughProxy asks an outside service what address it sees, over the same
// path outgoing mail takes.
//
// It has to be the same path, or the answer is this machine's address rather
// than the one a receiver will see, which is precisely the confusion this
// check exists to end.
func (self *verifier) askThroughProxy(ctx context.Context, proxyAddress string) (string, error) {
	services := self.config.Current().DNS.ExternalAddressServices
	if len(services) == 0 {
		return "", fmt.Errorf("dns: no address discovery services are configured")
	}

	dialer, err := proxy.SOCKS5("tcp", proxyAddress, nil, proxy.Direct)
	if err != nil {
		return "", err
	}
	contextDialer, ok := dialer.(proxy.ContextDialer)
	if !ok {
		return "", fmt.Errorf("dns: the proxy dialer does not take a context")
	}

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				return contextDialer.DialContext(ctx, network, address)
			},
		},
		Timeout: externalAddressTimeout,
	}

	var lastErr error
	for _, service := range services {
		// No family is pinned: whichever the proxy uses is the one a receiver
		// will see, and pinning would answer a different question.
		address, err := askWithClient(ctx, client, service, "")
		if err != nil {
			lastErr = err
			log.Debugf("could not determine the outgoing address from %s through the proxy: %s", service, err)
			continue
		}
		return address, nil
	}
	return "", lastErr
}
