package agent

import (
	"encoding/json"

	"github.com/ziyan/teanode/internal/llm"
)

// RememberAnswer is what the run answers with.
type RememberAnswer struct {
	Facts      []RememberedFact `json:"facts"`
	Links      []RememberedLink `json:"links"`
	Supersedes []SupersededFact `json:"supersedes"`
}

// RememberedFact is one thing the conversation taught.
type RememberedFact struct {
	Path      string `json:"path"`
	NodeKind  string `json:"node_kind"`
	NodeName  string `json:"node_name"`
	Kind      string `json:"kind"`
	Text      string `json:"text"`
	Happened  string `json:"happened"`
	Quote     string `json:"quote"`
	MessageID string `json:"message_id"`
}

// RememberedLink joins two pages.
type RememberedLink struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Relation string `json:"relation"`

	// Note is the sentence that justifies the link, in the words somebody
	// would use to say it, including details the relation alone cannot carry.
	Note string `json:"note"`
}

// SupersededFact is a fact this conversation has replaced.
type SupersededFact struct {
	Path   string `json:"path"`
	Number int    `json:"number"`

	// ReplacedBy is which of this answer's facts replaces it, counting
	// from 1. Without it, any fact the answer filed on the page stood in
	// for the line being retired, whatever it said: a fact about something
	// else retired a true one. Nil for a retraction, which carries a quote
	// instead.
	ReplacedBy *int `json:"replaced_by"`

	// Quote and MessageID are what says the line is no longer true, for a
	// retirement that files nothing in its place.
	//
	// "I go to a different dentist now" retires what the page says about
	// the dentist without saying who the new one is, and that is a real
	// thing for somebody to say. What it cannot be is a retirement nobody
	// said: without these, an answer naming a fact retired it on the
	// strength of naming it, and an answer carrying nothing else could
	// empty a page. Checked exactly as a fact's citation is -- the words
	// have to occur in something the run was actually shown.
	Quote     string `json:"quote"`
	MessageID string `json:"message_id"`
}

func parseRememberAnswer(responseText string) *RememberAnswer {
	extracted, err := llm.ExtractJSON(responseText)
	if err != nil {
		// A run that answered with prose taught nothing this time. Not a
		// failure: the mark still moves, and the next conversation is a
		// fresh try.
		log.Debugf("the filing run answered with no object: %s", err)
		return &RememberAnswer{}
	}
	answer := &RememberAnswer{}
	if err := json.Unmarshal([]byte(extracted), answer); err != nil {
		log.Debugf("the filing run's object is not what was asked for: %s", err)
		return &RememberAnswer{}
	}
	return answer
}
