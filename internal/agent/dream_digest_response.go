package agent

import (
	"context"
	"encoding/json"

	"github.com/ziyan/teanode/internal/llm"
)

// Prose can be converted with one model call, but tool-shaped text is not
// retried as prose. A partial JSON decode is rejected as a whole.
func (self *Agent) parseDigestResponse(ctx context.Context, run *Run, budget *dreamBudget, responseText string) (*RememberAnswer, error) {
	extracted, err := llm.ExtractJSON(responseText)
	if err != nil && len(responseText) > 200 && !textualToolCall(responseText) {
		extracted, err = self.digestObjectFromWords(ctx, run, budget, responseText)
	}
	if err != nil {
		return nil, err
	}
	answer := &RememberAnswer{}
	if err := json.Unmarshal([]byte(extracted), answer); err != nil {
		return nil, err
	}
	return answer, nil
}

// digestObjectFromWords asks once more, with no tools, for the object a
// reading in words should have ended with, and hands back the JSON in it.
func (self *Agent) digestObjectFromWords(ctx context.Context, run *Run, budget *dreamBudget, words string) (string, error) {
	prompt, err := render("digest_object.txt", map[string]any{
		"PersonName": personName(run.Owner),
		"Reading":    cutRunes(words, 12000),
	})
	if err != nil {
		return "", err
	}
	thinking, err := self.dreamThought(ctx, run, budget, "Wrote the object for a reading given in words", prompt, false)
	if err != nil {
		return "", err
	}
	return llm.ExtractJSON(thinking.Text)
}
