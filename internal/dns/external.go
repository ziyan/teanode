package dns

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// externalAddressTimeout bounds each attempt. A server that cannot work out
// its own address should say so quickly rather than delay startup.
const externalAddressTimeout = 5 * time.Second

// ExternalAddresses are the addresses the outside world reaches this server
// on, which is what its DNS records have to point at.
type ExternalAddresses struct {
	// IPv4 is the address seen from the internet over IPv4, empty when there
	// is none or it could not be determined.
	IPv4 string `json:"ipv4,omitempty"`

	// IPv6 is the same over IPv6. A server with no IPv6 is entirely normal.
	IPv6 string `json:"ipv6,omitempty"`

	// Error explains why nothing could be determined, for the case where the
	// server has no outbound access at all.
	Error string `json:"error,omitempty"`
}

// Suggestion is what to show an operator when the address could not be
// determined, so the guidance still says what kind of value belongs there.
func (self ExternalAddresses) Suggestion() string {
	switch {
	case self.IPv4 != "" && self.IPv6 != "":
		return self.IPv4 + " and " + self.IPv6
	case self.IPv4 != "":
		return self.IPv4
	case self.IPv6 != "":
		return self.IPv6
	}
	return "this server's public address (it could not be determined automatically)"
}

// discoverExternalAddresses asks an outside service what address this server
// appears to come from.
//
// The server cannot work this out by itself: the address on its interface is
// usually a private one behind NAT, and even on a public host it may have
// several. What matters for an MX record is what a sending mail server sees,
// which only something outside can report.
//
// Each family is asked separately, because a host with both will otherwise
// always answer over whichever it prefers, and the operator needs both to
// publish an A and an AAAA record.
func discoverExternalAddresses(ctx context.Context, resolvers []string) ExternalAddresses {
	if len(resolvers) == 0 {
		return ExternalAddresses{Error: "no address discovery services are configured"}
	}

	var addresses ExternalAddresses
	var waitGroup sync.WaitGroup
	var mutex sync.Mutex

	for _, family := range []struct {
		network string
		assign  func(string)
	}{
		{"tcp4", func(value string) { addresses.IPv4 = value }},
		{"tcp6", func(value string) { addresses.IPv6 = value }},
	} {
		waitGroup.Add(1)
		go func(network string, assign func(string)) {
			defer waitGroup.Done()

			for _, service := range resolvers {
				value, err := askForAddress(ctx, network, service)
				if err != nil {
					log.Debugf("could not determine the %s address from %s: %s", network, service, err)
					continue
				}
				// In a function, so the unlock is deferred: assign is
				// passed in, and a caller's function that panicked would
				// otherwise leave this held and hang the wait below.
				func() {
					mutex.Lock()
					defer mutex.Unlock()
					assign(value)
				}()
				return
			}
		}(family.network, family.assign)
	}
	waitGroup.Wait()

	if addresses.IPv4 == "" && addresses.IPv6 == "" {
		addresses.Error = "could not determine this server's external address; it may have no outbound access"
	}
	return addresses
}

// askForAddress fetches this server's apparent address over one address
// family.
func askForAddress(ctx context.Context, network, service string) (string, error) {
	ctxWithTimeout, cancel := context.WithTimeout(ctx, externalAddressTimeout)
	defer cancel()

	// Pinning the transport to one family is the whole point: otherwise the
	// host answers over whichever it prefers and the other stays unknown.
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, address string) (net.Conn, error) {
				dialer := &net.Dialer{Timeout: externalAddressTimeout}
				return dialer.DialContext(ctx, network, address)
			},
		},
		Timeout: externalAddressTimeout,
	}

	return askWithClient(ctxWithTimeout, client, service, network)
}

// askWithClient is the fetch itself, without deciding how to connect. The
// address-family pinning above and the proxy path in outgoing.go differ only
// in the client they hand in.
//
// The network is the family the answer is expected in, and empty means either
// — which is what the proxy path passes, because the proxy decides.
func askWithClient(ctx context.Context, client *http.Client, service, network string) (string, error) {
	ctxWithTimeout, cancel := context.WithTimeout(ctx, externalAddressTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctxWithTimeout, http.MethodGet, service, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", "teanode")

	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = response.Body.Close()
	}()

	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("dns: %s returned %d", service, response.StatusCode)
	}

	// Read a bounded amount: this should be one address, and a service that
	// misbehaves must not be able to feed us a stream.
	buffer := make([]byte, 64)
	read, err := response.Body.Read(buffer)
	if read == 0 && err != nil {
		return "", err
	}

	value := strings.TrimSpace(string(buffer[:read]))
	parsed := net.ParseIP(value)
	if parsed == nil {
		return "", fmt.Errorf("dns: %s returned %q, which is not an address", service, value)
	}
	// Guard against a service that answers over the wrong family.
	if network == "tcp4" && parsed.To4() == nil {
		return "", fmt.Errorf("dns: %s returned an IPv6 address over IPv4", service)
	}
	if network == "tcp6" && parsed.To4() != nil {
		return "", fmt.Errorf("dns: %s returned an IPv4 address over IPv6", service)
	}
	return parsed.String(), nil
}
