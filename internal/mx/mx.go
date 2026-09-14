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

	// SetCalendarHook installs what is told about a message that may carry
	// an invitation.
	SetCalendarHook(hook CalendarHook)

	// RunInsightRules runs the rules that read the agent's insight — the
	// second phase, once the insight exists — against a message in the
	// Inbox. The first phase ran at delivery for the other rules.
	RunInsightRules(tx db.Transaction, mailbox *models.Mailbox, item *models.MailboxItem, mail *models.Mail, insight *models.MailInsight) error

	// AutoReplyRefusal is why a message in a mailbox must not be answered
	// automatically, or empty when it may be: the out-of-office reply's
	// ladder, with the given quiet period per sender (zero for the
	// out-of-office default).
	AutoReplyRefusal(tx db.Transaction, mailbox *models.Mailbox, recipient string, item *models.MailboxItem, mail *models.Mail, now time.Time) (string, error)
}

// The headers a draft carries for the composer: what it answers or
// forwards, by item, and the Bcc the sent message will not show.
const (
	DraftHeaderBcc     = "X-TeaNode-Draft-Bcc"
	DraftHeaderReplyTo = "X-TeaNode-Draft-Reply-To-Item"
	DraftHeaderForward = "X-TeaNode-Draft-Forward-Item"

	// DraftHeaderKey names the draft itself, across saves.
	//
	// Saving a draft writes a new message and removes the old one, which is
	// what IMAP means by editing one: a stored message never changes, so a
	// changed draft is a different message with a different item. That makes
	// an item id a name for one save rather than for the draft, and anything
	// holding an item id -- an agent that wrote a draft and is waiting to be
	// told to send it, a held reply -- is left pointing at a message that is
	// gone the moment the person opens the draft and the composer saves it.
	// This key is written on the first save and carried by every save after
	// it, so there is a name for the draft that outlives them.
	DraftHeaderKey = "X-TeaNode-Draft-Key"
)

// AgentHook is told, inside the delivery transaction, that a message has
// been placed in a mailbox. It queues work in that transaction and never
// calls a model: a model outage must never bounce mail. Errors are its own
// to log; delivery does not wait on it.
type AgentHook interface {
	OnMailboxDelivery(tx db.Transaction, mailbox *models.Mailbox, item *models.MailboxItem, mail *models.Mail)
}

// CalendarHook is told the same thing, and for the same reason: a message may
// carry an invitation, and working out whether it does means reading the
// message from storage. It notes the message in the delivery transaction and
// reads it afterwards.
//
// Separate from AgentHook because an invitation is not the agent's business:
// a person who has never turned an agent on still wants their meetings.
type CalendarHook interface {
	// recipient is the address this was delivered to. An invitation has to
	// name the person whose calendar it enters, and the address the sender
	// wrote to is the only place that is reliably known: a mailbox reached
	// by a catch-all advertises no address of its own.
	OnMailboxDelivery(tx db.Transaction, mailbox *models.Mailbox, recipient string, item *models.MailboxItem, mail *models.Mail)
}
