package agent

import (
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// saidByModel is one streamed round of a scripted model saying text.
func saidByModel(text string) string {
	content, _ := json.Marshal(text)
	return fmt.Sprintf(`{"choices":[{"delta":{"content":%s},"finish_reason":"stop"}]}`, content)
}

// ideaCatalogForTest is three invented ideas: one on offer, one needing a
// tool nobody has, and one about something the person already does.
func ideaCatalogForTest(t *testing.T) {
	t.Helper()
	savedCatalog, savedChecks := ideaCatalog, ideaUsedChecks
	t.Cleanup(func() { ideaCatalog, ideaUsedChecks = savedCatalog, savedChecks })
	ideaUsedChecks = map[string]usedCheck{
		"baking": func(db.Transaction, *models.Agent, *models.User) (bool, error) { return false, nil },
		"kites":  func(db.Transaction, *models.Agent, *models.User) (bool, error) { return true, nil },
	}
	ideaCatalog = []*ideaEntry{
		{IdeaKey: "bread", IdeaCategory: "home", Emoji: "🏠", Headline: "Tell me the flour. I'll plan the bake.", Body: "Say what you have and I work out the timings.", OpeningRequest: "Plan a loaf for Saturday.", UsedCheck: "baking"},
		{IdeaKey: "garden", IdeaCategory: "home", Emoji: "🪴", Headline: "I'll water the garden.", Body: "I turn the sprinklers on.", OpeningRequest: "Water the garden.", NeededToolNames: []string{"sprinkler"}},
		{IdeaKey: "kite", IdeaCategory: "fun", Emoji: "🎲", Headline: "I'll find a windy day.", Body: "I read the forecast.", OpeningRequest: "When is it windy?", UsedCheck: "kites"},
	}
}

// The catalog in the repository is written to the rules every idea is
// held to, against the tools as they really are: each named tool exists,
// and one that acts toward somebody else or cannot be undone makes the
// entry say where it stops. A skill, named by a prefix, may be anything
// the skill says, so it is taken to act.
func TestTheCatalogIsWrittenToTheRules(t *testing.T) {
	toolRisks := map[string]tools.Risk{}
	for _, tool := range tools.Build().All() {
		toolRisks[tool.Name] = tool.Risk
	}
	isKey := map[string]bool{}
	for _, entry := range ideaCatalog {
		if isKey[entry.IdeaKey] {
			t.Errorf("%s is in the catalog twice", entry.IdeaKey)
		}
		isKey[entry.IdeaKey] = true
		if entry.UsedCheck != "" && ideaUsedChecks[entry.UsedCheck] == nil {
			t.Errorf("%s names the check %q, which there is not", entry.IdeaKey, entry.UsedCheck)
		}
		entryRisks := maps.Clone(toolRisks)
		for _, name := range entry.NeededToolNames {
			if prefix, isPrefix := strings.CutSuffix(name, "*"); isPrefix {
				entryRisks[prefix+"any"] = tools.RiskOutward
			}
		}
		if problem := checkIdea(entry.idea("agent", 1), entryRisks); problem != "" {
			t.Errorf("%s: %s", entry.IdeaKey, problem)
		}
	}
	if len(ideaCatalog) < 30 {
		t.Errorf("the catalog has %d ideas; a person should find something in it", len(ideaCatalog))
	}
}

// An idea may only promise what the person's tools do, and says where it
// stops to ask when a tool acts toward somebody else.
func TestAnIdeaPromisesOnlyWhatItsToolsDo(t *testing.T) {
	good := func() *models.AgentIdea {
		return &models.AgentIdea{
			IdeaKind: models.IdeaPersonal, IdeaCategory: "mail", Emoji: "📬",
			Headline: "I can answer the club's mail.", Body: "I draft each reply. Nothing is sent until you approve it.",
			OpeningRequest: "Draft replies to the club.", NeededToolNames: []string{"mail_send"},
			Evidence: []models.AgentIdeaEvidence{{EvidenceKind: "message", EvidenceID: "01x", EvidenceSummary: "a club newsletter"}},
		}
	}
	toolRisks := map[string]tools.Risk{"mail_send": tools.RiskOutward, "skill__garden__water": tools.RiskWrite}
	if problem := checkIdea(good(), toolRisks); problem != "" {
		t.Fatalf("a good idea was refused: %s", problem)
	}
	for _, each := range []struct {
		name   string
		change func(*models.AgentIdea)
		want   string
	}{
		{"a tool it lacks", func(idea *models.AgentIdea) { idea.NeededToolNames = []string{"haggle"} }, "does not have"},
		{"a skill it has, by prefix", func(idea *models.AgentIdea) { idea.NeededToolNames = []string{"skill__garden__*"} }, ""},
		{"a skill it lacks, by prefix", func(idea *models.AgentIdea) { idea.NeededToolNames = []string{"skill__pool__*"} }, "does not have"},
		{"acting without asking", func(idea *models.AgentIdea) { idea.Body = "I send each reply for you." }, "asks first"},
		{"promising too much", func(idea *models.AgentIdea) { idea.Headline = "I'll always answer the club." }, "promises too much"},
		{"an emoji of another area", func(idea *models.AgentIdea) { idea.Emoji = "🎲" }, "not one of the emoji"},
		{"no evidence", func(idea *models.AgentIdea) { idea.Evidence = nil }, "needs the evidence"},
		{"evidence of no kind", func(idea *models.AgentIdea) { idea.Evidence[0].EvidenceKind = "rumor" }, "not a kind of evidence"},
		{"a headline too long", func(idea *models.AgentIdea) { idea.Headline = strings.Repeat("a", 81) }, "longer than 80"},
		{"no request to start it", func(idea *models.AgentIdea) { idea.OpeningRequest = " " }, "request that starts it"},
	} {
		idea := good()
		each.change(idea)
		problem := checkIdea(idea, toolRisks)
		if each.want == "" && problem != "" || each.want != "" && !strings.Contains(problem, each.want) {
			t.Errorf("%s: %q, want %q", each.name, problem, each.want)
		}
	}
}

// A catalog idea is on offer while the person has its tools and does not
// already do what it is about. Started, dismissed and opened again, it
// keeps what became of it however often the catalog is read.
func TestAnIdeaIsOfferedWhileItFitsAndKeepsWhatBecameOfIt(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	provider := scriptedProvider([]string{saidByModel("ok")})
	defer provider.Close()
	worker, run := digestSplitWorld(t, database, provider.URL)
	ideaCatalogForTest(t)

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		ideas, err := worker.ListIdeas(t.Context(), tx, run.Agent, run.Owner, []models.AgentIdeaStatus{models.IdeaOpen}, nil)
		if err != nil || len(ideas) != 1 || ideas[0].IdeaKey != "bread" || ideas[0].Headline != "Tell me the flour. I'll plan the bake." {
			t.Fatalf("only bread is on offer: %+v %v", ideas, err)
		}
		bread := ideas[0]

		started, conversation, err := worker.StartIdea(t.Context(), tx, run.Agent, bread.ID, "")
		if err != nil || started.IdeaStatus != models.IdeaStarted || conversation == nil || started.StartedConversationID != conversation.ID {
			t.Fatalf("started in a conversation of its own: %+v %+v %v", started, conversation, err)
		}
		if conversation.Title != bread.Headline || conversation.Kind != models.AgentConversationNamed {
			t.Fatalf("the conversation is named after the idea: %+v", conversation)
		}
		messages, _ := tx.ListAgentMessages(conversation.ID, &db.Options{Limit: 10})
		if len(messages) != 0 {
			t.Fatalf("starting an idea says nothing: %+v", messages)
		}

		if _, err := worker.SetIdeaStatus(tx, run.Agent, bread.ID, models.IdeaStarted); err == nil {
			t.Fatal("started is starting's, not a status to set")
		}
		dismissed, err := worker.SetIdeaStatus(tx, run.Agent, bread.ID, models.IdeaDismissed)
		if err != nil || dismissed.IdeaStatus != models.IdeaDismissed || dismissed.ClosedAt == nil {
			t.Fatalf("dismissed: %+v %v", dismissed, err)
		}
		if err := worker.refreshIdeas(t.Context(), tx, run.Agent, run.Owner, true); err != nil {
			t.Fatal(err)
		}
		again, _ := tx.GetAgentIdea(run.Agent.ID, bread.ID)
		if again.IdeaStatus != models.IdeaDismissed {
			t.Fatalf("reading the catalog again does not bring it back: %+v", again)
		}
		reopened, err := worker.SetIdeaStatus(tx, run.Agent, bread.ID, models.IdeaOpen)
		if err != nil || reopened.IdeaStatus != models.IdeaOpen || reopened.ClosedAt != nil || reopened.StartedConversationID != "" {
			t.Fatalf("open again, afresh: %+v %v", reopened, err)
		}

		// The person now bakes: the idea expires.
		ideaUsedChecks["baking"] = func(db.Transaction, *models.Agent, *models.User) (bool, error) { return true, nil }
		if err := worker.refreshIdeas(t.Context(), tx, run.Agent, run.Owner, true); err != nil {
			t.Fatal(err)
		}
		if expired, _ := tx.GetAgentIdea(run.Agent.ID, bread.ID); expired.IdeaStatus != models.IdeaExpired {
			t.Fatalf("expired once they bake: %+v", expired)
		}
	})
}

// The agent offers one idea a day at most, the best one the person has
// not seen, to somebody who has the dashboard open and has gone quiet.
func TestTheAgentOffersTheBestUnseenIdeaAtMostDaily(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	provider := scriptedProvider([]string{saidByModel("ok")})
	defer provider.Close()
	worker, run := digestSplitWorld(t, database, provider.URL)
	ideaCatalogForTest(t)

	now := time.Now()
	reason := worker.ideaReason()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agent, _ := tx.GetAgent(run.Agent.ID)
		if isDue, _ := reason.isDue(t.Context(), tx, agent, run.Owner, 10*time.Minute, now); isDue {
			t.Fatal("not before the introduction is over")
		}
		if err := tx.MarkAgentSpokeFirst(run.Agent.ID, nil, &now, nil); err != nil {
			t.Fatal(err)
		}
		agent, _ = tx.GetAgent(run.Agent.ID)
		if isDue, _ := reason.isDue(t.Context(), tx, agent, run.Owner, time.Minute, now); isDue {
			t.Fatal("not while they are busy")
		}
		if isDue, err := reason.isDue(t.Context(), tx, agent, run.Owner, 10*time.Minute, now); err != nil || !isDue {
			t.Fatalf("a quiet person with an idea unseen: %v %v", isDue, err)
		}
		message, err := reason.checkIn(t.Context(), tx, agent, run.Owner, now, nil)
		if err != nil || !strings.HasPrefix(message, models.SpeakFirstMarker) || !strings.Contains(message, "Tell me the flour.") || !strings.Contains(message, `"Plan a loaf for Saturday."`) {
			t.Fatalf("the turn is told the idea and offers its request: %q %v", message, err)
		}
		ideas, _ := tx.ListAgentIdeas(agent.ID, []models.AgentIdeaStatus{models.IdeaOpen}, nil)
		if len(ideas) != 1 || ideas[0].ShownAt == nil {
			t.Fatalf("offered is shown, and still open: %+v", ideas)
		}
		if isDue, _ := reason.isDue(t.Context(), tx, agent, run.Owner, 10*time.Minute, now); isDue {
			t.Fatal("nothing unseen is left")
		}
		if _, err := worker.Enqueue(tx, models.AgentJobSpeakFirst, agent.ID, "", SpeakFirstIdea); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.UpdateAgentIdea(agent.ID, ideas[0].ID, func(idea *models.AgentIdea) error { idea.ShownAt = nil; return nil }); err != nil {
			t.Fatal(err)
		}
		if isDue, _ := reason.isDue(t.Context(), tx, agent, run.Owner, 10*time.Minute, now); isDue {
			t.Fatal("one offer a day")
		}
	})
}

// An idea found for the person is kept only when what prompted it is
// there, it is not one already kept, and a model finds it promises
// nothing its tools cannot do.
func TestAProposedIdeaIsKeptOnlyWithEvidenceAndAnHonestVerdict(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	provider := scriptedProvider([]string{
		saidByModel(`{"isHonest": true, "problem": ""}`),
		saidByModel(`{"isHonest": false, "problem": "nothing it has can call the plumber"}`),
	})
	defer provider.Close()
	worker, run := digestSplitWorld(t, database, provider.URL)
	ideaCatalogForTest(t)

	proposal := func(headline string, evidence ...models.AgentIdeaEvidence) *models.AgentIdea {
		return &models.AgentIdea{
			IdeaCategory: "home", Emoji: "🔧", Headline: headline,
			Body: "I list what the service needs and when, and remind you a week ahead.", OpeningRequest: "Remind me before the boiler service.",
			Evidence: evidence,
		}
	}
	boiler := models.AgentIdeaEvidence{EvidenceKind: "page", EvidenceID: "things/boiler", EvidenceSummary: "the boiler"}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if _, err := tx.PutAgentNode(&models.AgentNode{AgentID: run.Agent.ID, Path: "things/boiler", Kind: models.NodeThing, Name: "Boiler"}); err != nil {
			t.Fatal(err)
		}
		for _, each := range []struct {
			idea *models.AgentIdea
			want string
		}{
			{proposal("I'll remind you before the boiler service is due."), "needs the evidence"},
			{proposal("I'll remind you before the boiler service is due.", models.AgentIdeaEvidence{EvidenceKind: "page", EvidenceID: "things/kettle", EvidenceSummary: "a kettle"}), "no memory page"},
			{proposal("I'll remind you before the boiler service is due.", models.AgentIdeaEvidence{EvidenceKind: "message", EvidenceID: "01nothing", EvidenceSummary: "a letter"}), "no message"},
			{proposal("I'll remind you before the boiler service is due.", models.AgentIdeaEvidence{EvidenceKind: "conversation", EvidenceID: "01nobody", EvidenceSummary: "a chat"}), "no conversation"},
		} {
			if _, err := worker.ProposeIdea(t.Context(), tx, run.Agent, run.Owner, each.idea); err == nil || !strings.Contains(err.Error(), each.want) {
				t.Errorf("%q: %v", each.want, err)
			}
		}

		withAnyEmoji := proposal("I'll remind you before the boiler service is due.", boiler)
		withAnyEmoji.Emoji = "🛠️"
		kept, err := worker.ProposeIdea(t.Context(), tx, run.Agent, run.Owner, withAnyEmoji)
		if err != nil || kept.IdeaKind != models.IdeaPersonal || kept.IdeaStatus != models.IdeaOpen || !strings.HasPrefix(kept.IdeaKey, "personal_") {
			t.Fatalf("kept as a personal idea: %+v %v", kept, err)
		}
		if kept.Emoji != "🏠" {
			t.Fatalf("an emoji outside the category's is the category's own: %q", kept.Emoji)
		}
		if _, err := worker.ProposeIdea(t.Context(), tx, run.Agent, run.Owner, proposal("I'll remind you before the boiler service is due again.", boiler)); err == nil || !strings.Contains(err.Error(), "repeats") {
			t.Fatalf("the same idea twice: %v", err)
		}
		if _, err := worker.ProposeIdea(t.Context(), tx, run.Agent, run.Owner, proposal("I'll call the plumber when the boiler breaks.", boiler)); err == nil || !strings.Contains(err.Error(), "call the plumber") {
			t.Fatalf("the model's no, with its reason: %v", err)
		}
	})
}

// What the person did with a category's ideas moves the rest of it: the
// ones they dismiss sink, the ones they take up rise, and never past a
// kind.
func TestWhatThePersonDidMovesACategory(t *testing.T) {
	ideas := []*models.AgentIdea{
		{IdeaCategory: "home", IdeaStatus: models.IdeaDismissed},
		{IdeaCategory: "home", IdeaStatus: models.IdeaDismissed},
		{IdeaCategory: "travel", IdeaStatus: models.IdeaStarted},
		{IdeaCategory: "travel", IdeaStatus: models.IdeaOpen},
	}
	for index := 0; index < 20; index++ {
		ideas = append(ideas, &models.AgentIdea{IdeaCategory: "money", IdeaStatus: models.IdeaDismissed})
	}
	weights := categoryWeights(ideas)
	if weights["home"] >= 0 || weights["travel"] <= 0 || weights["work"] != 0 {
		t.Fatalf("home sinks, travel rises, work stays: %v", weights)
	}
	if personalIdeaRank+weights["money"] <= catalogIdeaRank+ideaCategoryMost {
		t.Fatalf("a personal idea stays above every catalog idea: %v", weights)
	}
}
