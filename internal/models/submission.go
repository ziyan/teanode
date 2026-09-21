package models

import "time"

// Submission records local acceptance and the mailbox changes still owed to it.
// It survives deletion of the stored mail so an old identifier cannot send again.
type Submission struct {
	OwnerID        string
	SubmissionID   string
	MailboxID      string
	RequestDigest  string
	MailID         string
	SentItemID     string
	DraftItemID    string
	ReplyItemID    string
	ForwardItemID  string
	AcceptedAt     time.Time
	ReconciledAt   *time.Time
	ReconcileAfter *time.Time
}

// DomainSubmission is acceptance by a domain operator or the host console.
// PrincipalID separates account identifiers from the console's identity.
type DomainSubmission struct {
	PrincipalID   string
	SubmissionID  string
	DomainID      string
	RequestDigest string
	MailID        string
	AcceptedAt    time.Time
}
