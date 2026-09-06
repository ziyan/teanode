package models

import "time"

// SpamTrainingLabelSpam and SpamTrainingLabelHam are what a message can be
// marked as. "Ham" is the word the field has used for "not spam" since the
// first Bayesian mail filters, and is kept because every other tool an
// operator meets uses it.
const (
	SpamTrainingLabelSpam = "spam"
	SpamTrainingLabelHam  = "ham"
)

// SpamTraining records that a message was used to teach the classifier.
//
// Keeping this, rather than only the counts, is what makes marking a message
// idempotent and un-marking exact: without it, pressing "this is spam" twice
// would count the message twice and quietly bias the corpus with no way to
// tell.
type SpamTraining struct {
	// MailID is the message that was marked.
	MailID string `json:"mailId"`

	// Label is SpamTrainingLabelSpam or SpamTrainingLabelHam.
	Label string `json:"label"`

	CreatedAt  time.Time `json:"createdAt,omitempty"`
	ModifiedAt time.Time `json:"modifiedAt,omitempty"`
}

// SpamTokenDelta is a change to one token's counts.
type SpamTokenDelta struct {
	Token     string
	SpamCount int64
	HamCount  int64
}

// SpamTokenCount is what has been learned about one token.
type SpamTokenCount struct {
	SpamCount int64
	HamCount  int64
}
