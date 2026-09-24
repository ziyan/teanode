package agent

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

// parseRememberAnswer reads the filing run's answer. One that is not an
// object, was cut off, or has no `facts` is not valid, and is not taken
// for an answer that found nothing: that moved the conversation's mark
// past a window nobody had read, and nothing ever came back for it.
func parseRememberAnswer(responseText string) modelAnswer[RememberAnswer] {
	return readModelAnswer[RememberAnswer](responseText, "facts")
}
