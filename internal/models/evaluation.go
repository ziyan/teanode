package models

import "time"

// EvaluationQuestionKind is what a memory check question tests, the same
// kinds a question file uses.
type EvaluationQuestionKind string

// The kinds. Direct asks what a fact says; paraphrase asks it in other
// words; changed asks about something that is no longer so; multihop
// needs two facts together; abstain asks what memory should not know.
const (
	EvaluationQuestionDirect     EvaluationQuestionKind = "direct"
	EvaluationQuestionParaphrase EvaluationQuestionKind = "paraphrase"
	EvaluationQuestionChanged    EvaluationQuestionKind = "changed"
	EvaluationQuestionMultihop   EvaluationQuestionKind = "multihop"
	EvaluationQuestionAbstain    EvaluationQuestionKind = "abstain"
)

// EvaluationQuestionKinds is every kind, in the order they are listed.
var EvaluationQuestionKinds = []EvaluationQuestionKind{
	EvaluationQuestionDirect, EvaluationQuestionParaphrase, EvaluationQuestionChanged,
	EvaluationQuestionMultihop, EvaluationQuestionAbstain,
}

// EvaluationQuestionState is where a question stands with the person.
type EvaluationQuestionState string

// The states. Asked is put to the person and not yet answered. Confirmed
// is the answer the agent gave, which the person said was right;
// corrected carries the answer the person gave instead. Both take part in
// an evaluation. Dropped is a question the person did not want kept, and
// unsure one they could not answer; neither takes part.
const (
	EvaluationQuestionAsked     EvaluationQuestionState = "asked"
	EvaluationQuestionConfirmed EvaluationQuestionState = "confirmed"
	EvaluationQuestionCorrected EvaluationQuestionState = "corrected"
	EvaluationQuestionDropped   EvaluationQuestionState = "dropped"
	EvaluationQuestionUnsure    EvaluationQuestionState = "unsure"
)

// IsEvaluated says whether a question in this state takes part in an
// evaluation: its answer is one the person stands behind.
func (self EvaluationQuestionState) IsEvaluated() bool {
	return self == EvaluationQuestionConfirmed || self == EvaluationQuestionCorrected
}

// AgentEvaluationQuestion is one question of the person's memory check:
// what was asked, the answer the person stands behind, and how it was
// settled. See docs/planning/agent-speaks-first-execplan.md.
type AgentEvaluationQuestion struct {
	ID         string    `json:"id"`
	AgentID    string    `json:"agentId"`
	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`

	QuestionKind   EvaluationQuestionKind `json:"questionKind"`
	QuestionText   string                 `json:"questionText"`
	ExpectedAnswer string                 `json:"expectedAnswer"`

	// OutdatedAnswer is what was true once, for a question about
	// something that changed; an answer giving it is stale.
	OutdatedAnswer string `json:"outdatedAnswer,omitempty" graphapi:"nullable"`

	QuestionState EvaluationQuestionState `json:"questionState"`

	// SourceFactIDs are the facts a drafted question came from; empty for
	// one the person supplied.
	SourceFactIDs []string `json:"sourceFactIds"`

	// IsAnswerFiledAfter says the answer was filed into memory after the
	// question was written: the question is easy now, and its score is
	// reported apart.
	IsAnswerFiledAfter bool `json:"isAnswerFiledAfter"`

	ConversationID string     `json:"conversationId,omitempty" graphapi:"nullable"`
	AnsweredAt     *time.Time `json:"answeredAt,omitempty" graphapi:"nullable"`
}
