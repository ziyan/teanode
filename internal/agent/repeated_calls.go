package agent

import (
	"encoding/json"
	"strings"
)

// A model that cannot see that a change went through tends to make it again,
// and again, filling the person's data with copies of one change. So a call
// that changes something, made with the same
// arguments as an earlier one in the turn, is watched. The second time it
// answers exactly what the first did, the answer says the change is already
// done; a third time it is not run at all. A call whose answer moved (a
// browser click on "next", keys typed into a terminal that printed something
// new) is a different step each time and goes ahead.

// repeatedCallNotice is added to the answer of a changing call that answered
// exactly what the same call answered earlier in the turn.
const repeatedCallNotice = "\nrepeated_call: this exact call already ran earlier in this turn and answered the same, so what it does is done. Do not send it again."

// repeatedCallRefusal is the answer to a changing call that has already
// answered the same twice in the turn. It is an error, so a model that keeps
// sending it stops the turn as a failure repeated three times does.
const repeatedCallRefusal = `{"error": "repeated_call: this exact call already ran twice in this turn and answered the same both times, so it was not run again. What it does is done; go on with the rest or answer the person."}`

// changingCallAnswer is what one changing call last answered in the turn, and
// how many times in a row it answered that.
type changingCallAnswer struct {
	answer      string
	answerCount int
}

// repeatedCallKey is a call as the guard compares it: the tool's name and its
// arguments with spacing and key order settled, so the same call written two
// ways is one call.
func repeatedCallKey(toolName string, arguments json.RawMessage) string {
	var decoded any
	if err := json.Unmarshal(arguments, &decoded); err == nil {
		if settled, err := json.Marshal(decoded); err == nil {
			return toolName + " " + string(settled)
		}
	}
	return toolName + " " + strings.TrimSpace(string(arguments))
}

// isRepeatedChangeSettled says whether the changing call has already answered
// the same twice in this turn, and so is not to run again.
func (self *AskRun) isRepeatedChangeSettled(key string) bool {
	self.changingCallAnswersMutex.Lock()
	defer self.changingCallAnswersMutex.Unlock()
	previous := self.changingCallAnswers[key]
	return previous != nil && previous.answerCount >= 2
}

// noteChangingCallAnswer keeps what a changing call answered and says whether
// it is exactly what the same call answered the time before.
func (self *AskRun) noteChangingCallAnswer(key, answer string) bool {
	self.changingCallAnswersMutex.Lock()
	defer self.changingCallAnswersMutex.Unlock()
	if self.changingCallAnswers == nil {
		self.changingCallAnswers = map[string]*changingCallAnswer{}
	}
	previous := self.changingCallAnswers[key]
	if previous != nil && previous.answer == answer {
		previous.answerCount++
		return true
	}
	self.changingCallAnswers[key] = &changingCallAnswer{answer: answer, answerCount: 1}
	return false
}
