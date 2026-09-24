package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A question is answered, the answer graded, and the verdict read from the
// grader; a grader that answers with something unreadable is ungraded,
// not wrong.
func TestAnAnswerIsGradedAgainstTheExpectedOne(t *testing.T) {
	for _, each := range []struct {
		name        string
		grade       string
		wantVerdict string
	}{
		{"a right answer", `{"answerVerdict": "correct", "verdictReason": "same boat"}`, AnswerCorrect},
		{"a stale answer", `{"answerVerdict": "stale", "verdictReason": "the old mooring"}`, AnswerStale},
		{"a verdict nobody asked for", `{"answerVerdict": "brilliant"}`, AnswerUngraded},
		{"no object at all", `It looks right to me.`, AnswerUngraded},
	} {
		t.Run(each.name, func(t *testing.T) {
			database, release := dbtest.AcquireDatabase(t)
			defer release()
			rounds := []string{}
			for _, said := range []string{"Marigold is moored at the pier.", each.grade} {
				content, _ := json.Marshal(said)
				rounds = append(rounds, fmt.Sprintf(`{"choices":[{"delta":{"content":%s},"finish_reason":"stop"}]}`, content))
			}
			provider := scriptedProvider(rounds)
			defer provider.Close()
			worker, run := digestSplitWorld(t, database, provider.URL)
			evaluation, err := worker.EvaluateAnswer(t.Context(), run.Agent, run.Owner,
				"Where is Marigold moored?", "At the pier.", "At the marina.", AnswerFromMemory)
			if err != nil {
				t.Fatalf("EvaluateAnswer: %s", err)
			}
			if evaluation.AnswerText != "Marigold is moored at the pier." || evaluation.AnswerVerdict != each.wantVerdict {
				t.Fatalf("answered %q, graded %q, want %q", evaluation.AnswerText, evaluation.AnswerVerdict, each.wantVerdict)
			}
		})
	}
}

// "not known" is decided without a grader: right for an abstain question,
// a miss for one with an answer.
func TestNotKnownIsAMissOrARightAbstain(t *testing.T) {
	for _, each := range []struct {
		expected    string
		wantVerdict string
	}{
		{"At the pier.", AnswerMissed},
		{"not known", AnswerNotKnown},
	} {
		database, release := dbtest.AcquireDatabase(t)
		content, _ := json.Marshal("Not known.")
		provider := scriptedProvider([]string{fmt.Sprintf(`{"choices":[{"delta":{"content":%s},"finish_reason":"stop"}]}`, content)})
		worker, run := digestSplitWorld(t, database, provider.URL)
		evaluation, err := worker.EvaluateAnswer(t.Context(), run.Agent, run.Owner, "Where is Marigold moored?", each.expected, "", AnswerFromMemory)
		provider.Close()
		release()
		if err != nil {
			t.Fatalf("EvaluateAnswer: %s", err)
		}
		if evaluation.AnswerVerdict != each.wantVerdict {
			t.Errorf("expected %q: graded %q, want %q", each.expected, evaluation.AnswerVerdict, each.wantVerdict)
		}
	}
}

// What to answer from is checked before anything is asked.
func TestAnAnswerFromNowhereIsRefused(t *testing.T) {
	if _, err := (&Agent{}).EvaluateAnswer(t.Context(), &models.Agent{}, &models.User{}, "Where?", "Here.", "", "rumour"); err == nil || !strings.Contains(err.Error(), "memory, sources or both") {
		t.Fatalf("an unknown source was not refused: %v", err)
	}
}
