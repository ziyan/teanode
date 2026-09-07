// Package models holds the structs shared between the database, the API and
// the mail path. They carry no behaviour beyond enum helpers.
package models

import (
	"time"

	"github.com/ziyan/teanode/internal/util/geoip"
)

type MailStatus string

const (
	MailStatusUnknown  MailStatus = ""
	MailStatusReceived MailStatus = "received"
	MailStatusAccepted MailStatus = "accepted"
	MailStatusRejected MailStatus = "rejected"
)

func (self MailStatus) String() string {
	return string(self)
}

func GetMailStatus(value string) MailStatus {
	switch value {
	case "received":
		return MailStatusReceived
	case "accepted":
		return MailStatusAccepted
	case "rejected":
		return MailStatusRejected
	}
	return MailStatusUnknown
}

type MailKind string

const (
	MailKindUnknown  MailKind = ""
	MailKindIncoming MailKind = "incoming"
	MailKindOutgoing MailKind = "outgoing"
	MailKindExchange MailKind = "exchange"
	MailKindDSN      MailKind = "dsn"
	MailKindRUA      MailKind = "rua"
	MailKindRUF      MailKind = "ruf"
	MailKindDraft    MailKind = "draft"
)

func (self MailKind) String() string {
	return string(self)
}

func GetMailKind(value string) MailKind {
	switch value {
	case "incoming":
		return MailKindIncoming
	case "outgoing":
		return MailKindOutgoing
	case "exchange":
		return MailKindExchange
	case "dsn":
		return MailKindDSN
	case "rua":
		return MailKindRUA
	case "ruf":
		return MailKindRUF
	case "draft":
		return MailKindDraft
	}
	return MailKindUnknown
}

// Mail is an inbound mail that is being processed; one or more Delivery will be created for it.
type Mail struct {
	// ID of the Mail
	ID string `json:"id,omitempty"`

	// Timestamp when the Mail was created
	CreatedAt time.Time `json:"createdAt,omitempty"`

	// Timestamp when the Mail was last modified
	ModifiedAt time.Time `json:"modifiedAt,omitempty"`

	// Domain that the Mail belongs to. The identifier is stored; the pointer
	// is resolved from the configuration when the Mail is loaded, and is nil
	// when the domain has since been removed from the configuration.
	DomainID string  `json:"domainId,omitempty"`
	Domain   *Domain `json:"-"`

	// Credential that was used to send this Mail, if any. Resolved from the
	// configuration in the same way, and nil once it is deleted.
	CredentialID string      `json:"credentialId,omitempty"`
	Credential   *Credential `json:"-"`

	// For DSN mail, the Delivery that caused the bounce, if any
	DeliveryID string    `json:"deliveryId,omitempty"`
	Delivery   *Delivery `json:"-"`

	// For debugging, the envelope ID when the Mail was received
	EnvelopeID string `json:"envelopeId,omitempty"`

	// Hello string when the Mail was received
	Hello string `json:"hello,omitempty"`

	// IP address from where the Mail was received
	IP string `json:"ip,omitempty"`

	// Reverse DNS name of the IP address
	RDNS string `json:"rdns,omitempty"`

	// TLS version used when receiving the Mail
	TLSVersion string `json:"tlsVersion,omitempty"`

	// TLS cipher suite used when receiving the Mail
	TLSCipherSuite string `json:"tlsCipherSuite,omitempty"`

	// Location of the Mail sender
	Location *geoip.Location `json:"location,omitempty"`

	// Sender address, also known as return path or bounce address
	Sender string `json:"sender,omitempty"`

	// Recipient addresses
	Recipients []string `json:"recipients,omitempty"`

	// Message ID from the header of the Mail
	MessageID string `json:"messageId,omitempty"`

	// From field from the header of the Mail
	From string `json:"from,omitempty"`

	// Subject line from the header of the Mail
	Subject string `json:"subject,omitempty"`

	// Raw data, not saved in database
	Headers []string `json:"-"`
	Body    []byte   `json:"-"`

	// ThreadID is the conversation this message is part of, derived from
	// In-Reply-To and References: the root message's own id when it starts
	// one. What "show me the thread" reads.
	ThreadID string `json:"threadId,omitempty"`

	// UnreferencedAt is when the last mailbox item holding this message went
	// away — or its arrival, for a message no mailbox took. Nil while any
	// item references it. Retention prunes what has been unreferenced for
	// longer than the retention period, nothing else.
	UnreferencedAt *time.Time `json:"unreferencedAt,omitempty"`

	// Kind gains draft for a message being written.

	// Size of the received Mail
	Size uint64 `json:"size,omitempty"`

	// Status of the Mail, one of: received, accepted, rejected
	Status MailStatus `json:"status,omitempty"`

	// Authentication result of the Mail
	AuthenticationResults AuthenticationResults `json:"authenticationResults,omitempty"`

	// Timestamp when the Mail was received
	ReceivedAt time.Time `json:"receivedAt,omitempty"`

	// Kind of Mail, one of: incoming, outgoing, exchange, dsn, rua, ruf
	Kind MailKind `json:"kind,omitempty"`

	// One or more Delivery created from this Mail
	Deliveries []*Delivery `json:"deliveries,omitempty"`
}

// AuthenticationResults holds authentication results of a Mail.
type AuthenticationResults struct {
	// Authentication results related to sender mail servers
	SenderMX *MXResult `json:"senderMx,omitempty"`

	// Authentication results related to from mail servers
	FromMX *MXResult `json:"fromMx,omitempty"`

	// Authentication results related to DMARC
	DMARC *DMARCResult `json:"dmarc,omitempty"`

	// Authentication results related to SPF
	SPF *SPFResult `json:"spf,omitempty"`

	// Authentication results related to DKIM
	DKIMs []*DKIMResult `json:"dkims,omitempty"`

	// Authentication results related to ARC
	ARC *ARCResult `json:"arc,omitempty"`

	// Authentication results related antivirus scanning
	Antivirus *AntivirusResult `json:"antivirus,omitempty"`

	// Authentication results related to spam filter
	SpamFilter *SpamFilterResult `json:"spamFilter,omitempty"`

	// Authentication results related to content filter
	ContentFilter *ContentFilterResult `json:"contentFilter,omitempty"`

	// Error messages
	Errors []string `json:"errors,omitempty"`
}

// MXResult holds authentication results related to mail servers.
type MXResult struct {
	// Sender domain or from domain
	Domain string `json:"domain,omitempty"`

	// Discovered mail servers
	MailServers []string `json:"mailServers,omitempty"`
}

// DMARCResult holds authentication results related to DMARC.
type DMARCResult struct {
	// From domain name
	Domain string `json:"domain,omitempty"`

	// DMARC policy
	Policy string `json:"policy,omitempty"`

	// Subdomain DMARC policy
	SubdomainPolicy string `json:"subdomainPolicy,omitempty"`

	// PolicyDomain is the domain the policy was found at, set only when that
	// is not the From domain itself — a sender on a subdomain is governed by
	// the organizational domain above it, and saying which one answers "where
	// did this policy come from" without a second lookup.
	PolicyDomain string `json:"policyDomain,omitempty"`

	// DKIM alignment mode
	DKIMAlignment string `json:"dkimAlignment,omitempty"`

	// SPF alignment mode
	SPFAlignment string `json:"spfAlignment,omitempty"`

	// DMARC result
	Result string `json:"result,omitempty"`
}

// SPFResult holds authentication results related to SPF.
type SPFResult struct {
	// Sender domain name
	Domain string `json:"domain,omitempty"`

	// Sender IP address
	IP string `json:"ip,omitempty"`

	// SPF result
	Result string `json:"result,omitempty"`
}

// DKIMResult holds authentication results related to DKIM.
type DKIMResult struct {
	// DKIM result
	Result string `json:"result,omitempty"`

	// Domain name
	Domain string `json:"domain,omitempty"`

	// DKIM selector
	Selector string `json:"selector,omitempty"`

	// DKIM identifier
	Identifier string `json:"identifier,omitempty"`
}

// ARCResult holds authentication results related to ARC.
type ARCResult struct {
	// ARC result
	Result string `json:"result,omitempty"`

	// ARC instances
	Instances int `json:"instances,omitempty"`
}

// AntivirusResult holds authentication results related to antivirus scanning.
type AntivirusResult struct {
	// Found viruses
	Viruses []string `json:"viruses,omitempty"`
}

// SpamFilterResult holds authentication results related to spam filtering.
type SpamFilterResult struct {
	// Spam score
	Score float64 `json:"score"`

	// Spam symbols
	Symbols []string `json:"symbols,omitempty"`

	// Checks is the breakdown behind Score: which checks fired and what each
	// one cost. Empty for a message scored by an external SpamAssassin
	// daemon, whose protocol reports names without points.
	Checks []SpamFilterCheck `json:"checks,omitempty"`

	// Spam filter result
	Result string `json:"result,omitempty"`
}

// SpamFilterCheck is one check that fired, and what it contributed.
type SpamFilterCheck struct {
	// Symbol is the check's short name, for example SPF_FAIL.
	Symbol string `json:"symbol"`

	// Score is the points this check contributed. Negative for a check that
	// vouches for a message rather than accusing it.
	Score float64 `json:"score"`

	// Description is a sentence for a human reading the dashboard.
	Description string `json:"description,omitempty"`
}

// ContentFilterResult holds authentication results related to content filtering.
type ContentFilterResult struct {
	// Found unsafe extensions
	UnsafeExtensions []string `json:"unsafeExtensions,omitempty"`
}
