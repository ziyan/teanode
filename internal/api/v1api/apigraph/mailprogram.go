package apigraph

import (
	"context"
	"net"
	"strconv"
	"strings"
)

// MailProgramQuery is what a person types into a mail program: where this
// server listens for IMAP and for submission. For anyone signed in, since
// it is their own mailbox they are setting up.
type MailProgramQuery interface {
	// Where a mail program connects: hosts and ports for IMAP and SMTP
	GetMailProgramSettings(ctx context.Context) (*MailProgramSettings, error)
}

type MailProgramSettings struct {
	// Host for IMAP, which is the server's mail host
	IMAPHost string `json:"imapHost"`

	// Port with STARTTLS; zero when the listener is off
	IMAPPort int `json:"imapPort"`

	// Port with TLS from the first byte; zero when the listener is off
	IMAPSPort int `json:"imapsPort"`

	// Host and port for sending, with STARTTLS
	SubmissionHost string `json:"submissionHost"`
	SubmissionPort int    `json:"submissionPort"`

	// DAVHost is what a phone is given to find the address book and the
	// calendar: the name, and the port when it is not the usual one. A
	// client is pointed at the host and finds the rest itself, through the
	// .well-known addresses, which is the whole reason those exist.
	//
	// Empty when nothing here is reachable under a name of its own, in
	// which case a person is better told nothing than told a guess.
	DAVHost string `json:"davHost,omitempty"`
}

func (self *graph) GetMailProgramSettings(ctx context.Context) (*MailProgramSettings, error) {
	if _, err := self.requireSignedIn(ctx); err != nil {
		return nil, err
	}
	configuration := self.config.Current()
	settings := &MailProgramSettings{
		IMAPHost:       configuration.IMAPHost(),
		IMAPPort:       portOf(":"+configuration.IMAPPort(), 0),
		IMAPSPort:      portOf(":"+configuration.IMAPTLSPort(), 0),
		SubmissionHost: configuration.SubmissionHost(),
		SubmissionPort: portOf(":"+configuration.SubmissionPort(), 587),
	}
	// Where a phone finds the address book and the calendar. The same name
	// the server writes into mail, because that is the one chosen to be the
	// way in from outside -- and with its port, since a deployment reached
	// on one of its own would otherwise be given an address that answers
	// with something else entirely.
	domains, err := self.transaction(ctx).ListDomains()
	if err != nil {
		return nil, err
	}
	for _, domain := range domains {
		if strings.TrimSpace(domain.LinkHost) == "" {
			continue
		}
		settings.DAVHost = configuration.LinkAuthorityFor(domain, domains)
		break
	}
	return settings, nil
}

// portOf is the port a listen address names, or the fallback when it names
// none or is empty.
func portOf(address string, fallback int) int {
	if address == "" {
		return fallback
	}
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return fallback
	}
	number, err := strconv.Atoi(port)
	if err != nil {
		return fallback
	}
	return number
}
