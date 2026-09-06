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

// SpamRuleSet is one channel's stored pattern rules.
//
// Stored in the database rather than on an instance's disk so that every
// instance evaluates the same rules; see the migration that creates the table
// for why that matters.
type SpamRuleSet struct {
	// Channel is where the rules came from.
	Channel string `json:"channel"`

	// Version is what the source called this set, and is what an instance
	// compares against the copy it has parsed.
	Version string `json:"version"`

	// Content is the rule text as stored. Absent from listings, which do not
	// display it.
	Content []byte `json:"-"`

	// RulesLoaded and RulesSkipped are how much of it this server could use.
	// A published set contains rules that need plugins this server does not
	// have, and patterns its regular expression engine will not compile.
	RulesLoaded  int `json:"rulesLoaded"`
	RulesSkipped int `json:"rulesSkipped"`

	UpdatedAt time.Time `json:"updatedAt,omitempty"`

	// Error is why the last attempt to update this channel failed, or empty.
	Error string `json:"error,omitempty"`
}
