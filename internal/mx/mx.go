// Package mx implements mail exchange processing.
package mx

import (
	"context"
	"net"
	"strconv"
	"time"

	"github.com/op/go-logging"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
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

	// SetAgentHook installs what is told about a message placed in a
	// mailbox, inside the delivery transaction. Nil means nobody is told,
	// which is the case while the agent is off.
	SetAgentHook(hook AgentHook)

	// RunInsightRules runs the rules that read the agent's insight — the
	// second phase, once the insight exists — against a message in the
	// Inbox. The first phase ran at delivery for the other rules.
	RunInsightRules(tx db.Transaction, mailbox *models.Mailbox, item *models.MailboxItem, mail *models.Mail, insight *models.MailInsight) error

	// AutoReplyRefusal is why a message in a mailbox must not be answered
	// automatically, or empty when it may be: the out-of-office reply's
	// ladder, with the given quiet period per sender (zero for the
	// out-of-office default).
	AutoReplyRefusal(tx db.Transaction, mailbox *models.Mailbox, recipient string, item *models.MailboxItem, mail *models.Mail, now time.Time, quiet time.Duration) (string, error)
}

// The headers a draft carries for the composer: what it answers or
// forwards, by item, and the Bcc the sent message will not show.
const (
	DraftHeaderBcc     = "X-TeaNode-Draft-Bcc"
	DraftHeaderReplyTo = "X-TeaNode-Draft-Reply-To-Item"
	DraftHeaderForward = "X-TeaNode-Draft-Forward-Item"
)

// AgentHook is told, inside the delivery transaction, that a message has
// been placed in a mailbox. It queues work in that transaction and never
// calls a model: a model outage must never bounce mail. Errors are its own
// to log; delivery does not wait on it.
type AgentHook interface {
	OnMailboxDelivery(tx db.Transaction, mailbox *models.Mailbox, item *models.MailboxItem, mail *models.Mail)
}
