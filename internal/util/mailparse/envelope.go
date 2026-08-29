package mailparse

import (
	"crypto/tls"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/util/geoip"
)

// Envelope contains a mail
type Envelope struct {
	// id of this envelope, for debug reference
	ID string

	// ip, rdns and location of client
	IP       net.IP
	RDNS     string
	Location *geoip.Location

	// hello sent by client
	Hello string

	// using credential to send outgoing email
	CredentialID  string
	CredentialKey string

	// using domain to send outgoing email without credential
	DomainID string

	// sender specified by client
	Sender string

	// recipients specified by client
	Recipients []string

	// if recipient is special, like dsn or dmarc
	SpecialPrefix string
	SpecialID     string

	// whether the connection was tls
	TLS *tls.ConnectionState

	// timestamp of mail
	ReceivedAt time.Time

	// size of received
	Size uint64

	// splitted headers and body
	Headers []string
	Body    []byte
}

func (self *Envelope) String() string {
	return fmt.Sprintf("<Envelope(%s)>", strings.Join([]string{
		fmt.Sprintf("id=%q", self.ID),
		fmt.Sprintf("ip=%q", self.IP),
		fmt.Sprintf("rdns=%q", self.RDNS),
		fmt.Sprintf("location=%q", self.Location),
		fmt.Sprintf("hello=%q", self.Hello),
		fmt.Sprintf("sender=%q", self.Sender),
		fmt.Sprintf("recipients=%q", self.Recipients),
		fmt.Sprintf("size=%d", self.Size),
	}, ", "))
}
