// Package spamfilter is the seam between the server and whatever scores its
// mail.
//
// Two things implement Filter. The strainer, in internal/strainer, scores
// messages inside this process. The spamd adapter here hands the message to
// an external SpamAssassin daemon over a socket. The exchange knows only that
// it has a Filter, which is what lets an operator choose between them with
// one setting.
package spamfilter

import (
	"context"
	"net/netip"

	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/geoip"
)

// Filter scores one message.
//
// Implementations must be safe for concurrent use and must respect the
// context's deadline: a filter that hangs holds an SMTP transaction open.
type Filter interface {
	Close() error

	Check(ctx context.Context, message *Message) (*models.SpamFilterResult, error)
}

// Message is everything a filter may look at.
//
// Every field is a value this server has already computed by the time a
// message is scored. A filter must not derive any of them again: re-resolving
// a name or re-verifying a signature spends the cost twice for an answer that
// is already in memory, which is the main advantage of scoring in process
// rather than over a socket.
type Message struct {
	// Headers and Body are the message as mailparse.Split already produced
	// it. Filters read these directly rather than re-parsing. Only the spamd
	// adapter glues them back together, because a socket takes bytes.
	Headers []string
	Body    []byte

	// Authentication is what the server established before scoring began:
	// SPF, DKIM, DMARC, ARC and the sending host's mail servers. Nil for a
	// message that took a path where those checks do not run, so every
	// reader has to tolerate that.
	Authentication *models.AuthenticationResults

	// RemoteAddress is the address the message was delivered from.
	RemoteAddress netip.Addr

	// ReverseName is the connecting address's reverse DNS name, already
	// confirmed to resolve back to that address by internal/util/smtpd.
	// Empty means there is no confirmed name — either no PTR record at all,
	// or one that does not resolve back.
	ReverseName string

	// Location is where the connecting address is, when GeoIP is enabled.
	Location *geoip.Location

	// HelloName is the name the sending host gave in HELO or EHLO.
	HelloName string

	// ServerName is this server's own name, for noticing a sender that
	// claimed to be us.
	ServerName string
}
