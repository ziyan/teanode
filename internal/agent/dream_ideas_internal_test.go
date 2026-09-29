package agent

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// The ideas a night found are read from the object it ends with, fenced
// or not, and an answer that is not that object is an error rather than a
// night that found nothing.
func TestTheIdeaStepReadsTheObjectItEndsWith(t *testing.T) {
	object := `{"ideas":[{"headline":"I can remind you before the boiler service.","body":"I put it in your calendar a week ahead.","opening_request":"Remind me about the boiler.","suggestion_reason":"the service is due","idea_category":"home","emoji":"🔧","evidence":[{"evidenceKind":"page","evidenceId":"things/boiler","evidenceSummary":"the boiler"}],"needed_tool_names":["calendar"],"expires_on":"2030-01-02"}]}`
	for _, each := range []struct {
		name string
		text string
	}{
		{"bare", object},
		{"fenced", "I looked through the mail and kept one.\n\n```json\n" + object + "\n```"},
	} {
		proposals, err := readDreamIdeas(each.text)
		if err != nil || len(proposals) != 1 {
			t.Fatalf("%s: %+v %v", each.name, proposals, err)
		}
		proposal := proposals[0]
		if proposal.OpeningRequest != "Remind me about the boiler." || proposal.IdeaCategory != "home" || proposal.ExpiresOn != "2030-01-02" ||
			len(proposal.NeededToolNames) != 1 || len(proposal.Evidence) != 1 || proposal.Evidence[0].EvidenceID != "things/boiler" {
			t.Fatalf("%s: every field is read under the tool's own name: %+v", each.name, proposal)
		}
	}
	if proposals, err := readDreamIdeas(`{"ideas": []}`); err != nil || len(proposals) != 0 {
		t.Fatalf("an empty list is a night that found nothing: %+v %v", proposals, err)
	}
	for _, garbage := range []string{"", "I kept two ideas.", `{"error": "no tools"}`, `{"ideas": [{"headline": "cut off`, `{"ideas": "none"}`} {
		if _, err := readDreamIdeas(garbage); err == nil {
			t.Errorf("%q is not the object", garbage)
		}
	}
}

// A night's ideas go through the check any idea passes: one refused is
// reported with its reason, a good one is kept, and no more are kept than
// the night may keep.
func TestADreamsIdeasGoThroughTheCheckEveryIdeaPasses(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	provider := scriptedProvider([]string{saidByModel(`{"isHonest": true, "problem": ""}`)})
	defer provider.Close()
	worker, run := digestSplitWorld(t, database, provider.URL)
	ideaCatalogForTest(t)
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if _, err := tx.PutAgentNode(&models.AgentNode{AgentID: run.Agent.ID, Path: "things/boiler", Kind: models.NodeThing, Name: "Boiler"}); err != nil {
			t.Fatal(err)
		}
	})
	boiler := []models.AgentIdeaEvidence{{EvidenceKind: "page", EvidenceID: "things/boiler", EvidenceSummary: "the boiler"}}
	proposal := func(headline string, evidence []models.AgentIdeaEvidence) IdeaProposal {
		return IdeaProposal{
			IdeaCategory: "home", Emoji: "🔧", Headline: headline,
			Body: "I list what the service needs and when, and remind you a week ahead.", OpeningRequest: "Remind me before the boiler service.",
			Evidence: evidence,
		}
	}
	noEvidence := proposal("I'll remind you before the boiler service is due.", nil)
	badDay := proposal("I'll book a slot for the chimney sweep in spring.", boiler)
	badDay.ExpiresOn = "next spring"
	good := proposal("I'll remind you before the boiler service is due.", boiler)
	beyondMost := proposal("I'll keep a list of the parts the boiler has had.", boiler)

	summary := worker.keepDreamIdeas(t.Context(), run, []IdeaProposal{noEvidence, badDay, good, beyondMost}, 1)
	if !strings.HasPrefix(summary, "kept 1 of 4; refused: ") || !strings.Contains(summary, "needs the evidence") || !strings.Contains(summary, "2006-01-02") {
		t.Fatalf("the summary says what was kept and why the rest were refused: %q", summary)
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		ideas, err := tx.ListAgentIdeas(run.Agent.ID, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		var personal []*models.AgentIdea
		for _, idea := range ideas {
			if idea.IdeaKind == models.IdeaPersonal {
				personal = append(personal, idea)
			}
		}
		if len(personal) != 1 || personal[0].Headline != good.Headline || personal[0].IdeaStatus != models.IdeaOpen || personal[0].ExpiresAt == nil {
			t.Fatalf("only the good one is kept, and no more than the night may keep: %+v", personal)
		}
	})
}

// The step looks at most once a day, found by its own run, and a night
// that has spent its share still looks when it is due.
func TestTheIdeaStepLooksOnceADayWhateverTheReadingSpent(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	answer := `{"ideas":[{"headline":"I'll remind you before the boiler service is due.","body":"I list what the service needs and when, and remind you a week ahead.","opening_request":"Remind me before the boiler service.","suggestion_reason":"the service is due","idea_category":"home","emoji":"🔧","evidence":[{"evidenceKind":"page","evidenceId":"things/boiler","evidenceSummary":"the boiler"}]}]}`
	scripted := scriptedProvider([]string{saidByModel(answer), saidByModel(`{"isHonest": true, "problem": ""}`)})
	defer scripted.Close()
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		scripted.Config.Handler.ServeHTTP(writer, request)
	}))
	defer provider.Close()
	worker, run := digestSplitWorld(t, database, provider.URL)
	ideaCatalogForTest(t)
	run.Agent.IsIdeasEnabled = true
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if _, err := tx.PutAgentNode(&models.AgentNode{AgentID: run.Agent.ID, Path: "things/boiler", Kind: models.NodeThing, Name: "Boiler"}); err != nil {
			t.Fatal(err)
		}
		isDue, err := ideasDue(tx, run.Agent.ID, time.Now())
		if err != nil || !isDue {
			t.Fatalf("an agent that never looked is due: %v %v", isDue, err)
		}
	})

	// A night whose share of the day was gone before it began.
	spent := &dreamBudget{exhausted: true}
	worker.dreamIdeas(t.Context(), run, spent)
	if calls.Load() == 0 {
		t.Fatal("a spent share does not stop the one look a day")
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		runs, err := tx.ListAgentRuns(run.Agent.ID, &db.AgentRunFilter{Query: dreamIdeasTitle}, nil)
		if err != nil || len(runs) != 1 || runs[0].Title != dreamIdeasTitle+": kept 1 of 1" {
			t.Fatalf("the run says what it kept: %+v %v", runs, err)
		}
		for _, each := range []struct {
			now    time.Time
			isDue  bool
			reason string
		}{
			{time.Now(), false, "just looked"},
			{time.Now().Add(dreamIdeasApart - time.Hour), false, "under the gap"},
			{time.Now().Add(dreamIdeasApart + time.Minute), true, "past the gap"},
		} {
			isDue, err := ideasDue(tx, run.Agent.ID, each.now)
			if err != nil || isDue != each.isDue {
				t.Errorf("%s: due %v, want %v (%v)", each.reason, isDue, each.isDue, err)
			}
		}
	})

	before := calls.Load()
	worker.dreamIdeas(t.Context(), run, &dreamBudget{})
	if calls.Load() != before {
		t.Fatal("a second night the same day does not look again")
	}

	// A provider that will not bill the account stops even this call.
	refused := &dreamBudget{refused: true}
	if refused.reserveBeyondShare() {
		t.Fatal("a refused provider refuses the look for ideas too")
	}
}
