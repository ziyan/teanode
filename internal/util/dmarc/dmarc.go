// Package dmarc implements DMARC as specified in RFC 7489
package dmarc

import (
	"time"
)

type AlignmentMode string

const (
	AlignmentStrict  AlignmentMode = "s"
	AlignmentRelaxed AlignmentMode = "r"
)

type FailureOptions uint64

const (
	FailureAll  FailureOptions = 1 << iota // "0"
	FailureAny                             // "1"
	FailureDKIM                            // "d"
	FailureSPF                             // "s"
)

type Policy string

const (
	PolicyNone       Policy = "none"
	PolicyQuarantine Policy = "quarantine"
	PolicyReject     Policy = "reject"
)

type ReportFormat string

const (
	ReportFormatAFRF ReportFormat = "afrf"
)

// Record is a DMARC record, as defined in RFC 7489 section 6.3.
type Record struct {
	DKIMAlignment      AlignmentMode  // "aDkim"
	SPFAlignment       AlignmentMode  // "aSpf"
	FailureOptions     FailureOptions // "fo"
	Policy             Policy         // "p"
	Percent            *uint64        // "pct"
	ReportFormat       []ReportFormat // "rf"
	ReportInterval     time.Duration  // "ri"
	ReportURIAggregate []string       // "rua"
	ReportURIFailure   []string       // "ruf"
	SubdomainPolicy    Policy         // "sp"
}
