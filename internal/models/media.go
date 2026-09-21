package models

import "time"

// Media is a file an operator uploaded to put in a template: a logo, a header,
// a signature.
//
// The bytes are not here. They go to the same storage the messages do, for the
// same reason — a database holding every picture is one nobody can restore
// quickly — and this row is what says which file that is, who it belongs to
// and what to answer with when it is asked for.
type Media struct {
	// ID of the Media, stable for its lifetime, and the name the bytes are
	// stored under.
	ID string `json:"id,omitempty"`

	// Timestamp when the Media was uploaded
	CreatedAt time.Time `json:"createdAt,omitempty"`

	// Timestamp when the Media was last modified
	ModifiedAt time.Time `json:"modifiedAt,omitempty"`

	// The Domain this Media belongs to. A picture uploaded for one domain is
	// served from that domain's own name and may only be put in that domain's
	// templates.
	DomainID string `json:"domainId,omitempty"`

	// Filename is what it was uploaded as, kept so a list of them reads as the
	// operator's own files rather than as identifiers.
	Filename string `json:"filename,omitempty"`

	// ContentType is what to answer with, decided by reading the bytes rather
	// than by believing what the browser said they were.
	ContentType string `json:"contentType,omitempty"`

	// Size in bytes.
	Size int64 `json:"size,omitempty"`

	// Checksum of the bytes, as hex. It is what tells an operator two
	// uploads are the same file, and what a later deduplication would use.
	Checksum string `json:"checksum,omitempty"`
}
