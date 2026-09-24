package agent

import (
	"context"
	"errors"

	"github.com/ziyan/teanode/internal/llm"
)

// Prose can be converted with one model call, but tool-shaped text is not
// retried as prose. A partial JSON decode is rejected as a whole, and so
// is an answer that is not a reading at all: `{}`, an error object, or a
// cut-off `{"facts":` that the repair would close into an empty list. Any
// of those, taken as "read, nothing found", marked the batch read and
// went round the two tries givingUpOn gives it.
func (self *Agent) parseDigestResponse(ctx context.Context, run *Run, budget *dreamBudget, responseText string) (*RememberAnswer, error) {
	if _, err := llm.ExtractJSON(responseText); err != nil && len(responseText) > 200 && !textualToolCall(responseText) {
		words, err := self.digestObjectFromWords(ctx, run, budget, responseText)
		if err != nil {
			return nil, err
		}
		responseText = words
	}
	answer := readModelAnswer[RememberAnswer](responseText, "facts")
	if !answer.IsValid {
		return nil, errors.New(answer.Problem)
	}
	return &answer.Value, nil
}

// digestObjectFromWords asks once more, with no tools, for the object a
// reading in words should have ended with, and hands back what it said.
func (self *Agent) digestObjectFromWords(ctx context.Context, run *Run, budget *dreamBudget, words string) (string, error) {
	prompt, err := render("digest_object.txt", map[string]any{
		"KnowledgeLanguage": languageName(KnowledgeLanguage(run.Agent, run.Owner)),
		"PersonName":        personName(run.Owner),
		"Reading":           cutRunes(words, 12000),
	})
	if err != nil {
		return "", err
	}
	thinking, err := self.dreamThought(ctx, run, budget, "Wrote the object for a reading given in words", prompt, false)
	if err != nil {
		return "", err
	}
	return thinking.Text, nil
}
