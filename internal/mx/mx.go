// Package mx implements mail exchange processing.
package mx

import (
	"context"
	"net"
	"strconv"

	"github.com/op/go-logging"

	"github.com/ziyan/teanode/internal/util/mailparse"
	"github.com/ziyan/teanode/internal/util/smtpc"
)

var log = logging.MustGetLogger("mx")

type Settings struct {
	// server identity
	Server  string
	Service string

	// MailServers are the names mail arrives at for this server — what every
	// configured domain's MX records point at. Used to notice that a domain
	// being delivered to is served by this same server, which would loop.
	//
	// This was one name, filled from server.name, and that is not what an MX
	// record points at: the panel tells an operator to publish MX records
	// naming server.mailServers, so on any deployment where the two differ
	// the check compared against a name no MX record carries and never fired.
	MailServers []string

	// secret for signing addresses
	Secret []byte

	// log directory
	LogDirectory string

	// socks5 proxy for outbound smtp connections (optional)
	SOCKS5Proxy string

	// Relay hands outgoing mail to one server rather than delivering it by
	// MX lookup. Nil when mail is delivered directly.
	Relay *RelaySettings

	// whether to disable sending mail over smtp
	DisableSendMail bool
}

// RelaySettings describes the one server outgoing mail is handed to.
type RelaySettings struct {
	Host string
	Port uint16

	// TLS is how the connection is encrypted, and whether the certificate is
	// checked. See smtpc.TLSMode.
	TLS smtpc.TLSMode

	Username string
	Password string
}

// Address is the relay as a dialable host:port.
func (self *RelaySettings) Address() string {
	return net.JoinHostPort(self.Host, strconv.Itoa(int(self.Port)))
}

type Exchange interface {
	Close() error

	// handle received mail
	HandleEnvelope(ctx context.Context, envelope *mailparse.Envelope) error
}
