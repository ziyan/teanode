package models

import (
	"time"

	"github.com/ziyan/teanode/internal/util/dmarc"
	"github.com/ziyan/teanode/internal/util/geoip"
)

// Report is a collected DMARC report for a Domain.
type Report struct {
	// ID of the Report
	ID string `json:"id,omitempty"`

	// Timestamp when the Report was created
	CreatedAt time.Time `json:"createdAt,omitempty"`

	// Timestamp when the Report was last modified
	ModifiedAt time.Time `json:"modifiedAt,omitempty"`

	// The Mail that the Report was parsed from
	MailID string `json:"mailId,omitempty"`
	Mail   *Mail  `json:"-"`

	// Domain that the Report belongs to
	DomainID string  `json:"domainId,omitempty"`
	Domain   *Domain `json:"-"`

	// Report start date
	BeginAt time.Time `json:"beginAt,omitempty"`

	// Report end date
	EndAt time.Time `json:"endAt,omitempty"`

	// Number of instances
	Count uint64 `json:"count,omitempty"`

	// IP address of the sender
	IP string `json:"ip,omitempty"`

	// Reverse DNS name of the IP address
	RDNS string `json:"rdns,omitempty"`

	// Location of the sender
	Location *geoip.Location `json:"location,omitempty"`

	// From domain identifier
	FromDomain string `json:"fromDomain,omitempty"`

	// Sender domain identifier
	SenderDomain string `json:"senderDomain,omitempty"`

	// Disposition reported, what happened to the mail
	Disposition string `json:"disposition,omitempty"`

	// DKIM alignment
	DKIMAligned bool `json:"dkimAligned,omitempty"`

	// SPF alignment
	SPFAligned bool `json:"spfAligned,omitempty"`

	// Parsed original DMARC feedback
	Feedback *dmarc.Feedback `json:"feedback,omitempty"`
}
