package apigraph

import (
	"context"
	"net"
	"strconv"
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
}

func (self *graph) GetMailProgramSettings(ctx context.Context) (*MailProgramSettings, error) {
	if _, err := self.requireSignedIn(ctx); err != nil {
		return nil, err
	}
	configuration := self.config.Current()
	return &MailProgramSettings{
		IMAPHost:       configuration.SubmissionHost(),
		IMAPPort:       portOf(configuration.Listen.IMAP, 0),
		IMAPSPort:      portOf(configuration.Listen.IMAPS, 0),
		SubmissionHost: configuration.SubmissionHost(),
		SubmissionPort: portOf(":"+configuration.SubmissionPort(), 587),
	}, nil
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
