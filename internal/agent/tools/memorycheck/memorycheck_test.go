package memorycheck_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/memorycheck"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// checkRun is a run in the main conversation, with a database, an agent
// and a person, and nothing else.
type checkRun struct {
	tools.Run
	database     db.Database
	owner        *models.User
	agent        *models.Agent
	conversation *models.AgentConversation
}

func (self *checkRun) Database() db.Database                   { return self.database }
func (self *checkRun) Owner() *models.User                     { return self.owner }
func (self *checkRun) Agent() *models.Agent                    { return self.agent }
func (self *checkRun) Conversation() *models.AgentConversation { return self.conversation }

type checkWorld struct {
	database  db.Database
	run       *checkRun
	tool      *tools.Tool
	workFact  *models.AgentFact
	movedFact *models.AgentFact
}

func newCheckWorld(t *testing.T) *checkWorld {
	t.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(t)
	t.Cleanup(closeDatabase)
	world := &checkWorld{database: database, run: &checkRun{database: database}}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if world.run.owner, err = tx.CreateUser(&models.User{Username: "check-owner", Timezone: "UTC", TimezoneMode: "fixed"}); err != nil {
			t.Fatal(err)
		}
		if world.run.agent, err = tx.CreateAgent(&models.Agent{UserID: world.run.owner.ID, Enabled: true}); err != nil {
			t.Fatal(err)
		}
		if world.run.conversation, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: world.run.agent.ID, Kind: models.AgentConversationMain, Surface: "drawer", LastAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
		agentId := world.run.agent.ID
		self, err := tx.PutAgentNode(&models.AgentNode{AgentID: agentId, Path: "self", Kind: models.NodePerson, Name: "Me"})
		if err != nil {
			t.Fatal(err)
		}
		work, err := tx.PutAgentNode(&models.AgentNode{AgentID: agentId, Path: "projects/lighthouse", Kind: models.NodeProject, Name: "Lighthouse"})
		if err != nil {
			t.Fatal(err)
		}
		add := func(node *models.AgentNode, text string, happenedAt *time.Time, isInferred bool) *models.AgentFact {
			fact, err := tx.AddAgentFact(&models.AgentFact{AgentID: agentId, NodeID: node.ID, Kind: models.FactPlain, Text: text, HappenedAt: happenedAt, Inferred: isInferred})
			if err != nil {
				t.Fatal(err)
			}
			return fact
		}
		add(self, "They keep bees on the roof.", nil, false)
		add(self, "They probably like honey.", nil, true)
		spring := time.Date(2021, 4, 1, 0, 0, 0, 0, time.UTC)
		add(self, "They ran a half marathon.", &spring, false)
		world.movedFact = add(self, "They live in Harbor Town.", nil, false)
		now := add(self, "They live in Hill Village.", nil, false)
		if _, err := tx.UpdateAgentFact(agentId, world.movedFact.ID, func(fact *models.AgentFact) error {
			fact.SupersededBy = now.ID
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		world.workFact = add(work, "The lighthouse build is on version 3.", nil, false)
	})
	for _, each := range tools.Build().All() {
		if each.Name == "memory_check" {
			world.tool = each
		}
	}
	if world.tool == nil {
		t.Fatal("memory_check is registered")
	}
	return world
}

func (self *checkWorld) call(t *testing.T, arguments string) map[string]any {
	t.Helper()
	result, err := self.tool.Run(tools.WithRun(context.Background(), self.run), &tools.Call{ID: "c1", Arguments: []byte(arguments)})
	if err != nil {
		t.Fatalf("%s: %s", arguments, err)
	}
	var answer map[string]any
	if err := json.Unmarshal([]byte(result.Content), &answer); err != nil {
		t.Fatalf("%s: %s", result.Content, err)
	}
	return answer
}

// A draft offers facts from the person's own pages, stated rather than
// inferred, including one that changed with what it reads now; never a
// work page's.
func TestADraftAsksAboutThePersonsOwnStatedFacts(t *testing.T) {
	world := newCheckWorld(t)
	answer := world.call(t, `{"action":"draft"}`)
	facts, _ := answer["facts"].([]any)
	if len(facts) != 4 {
		t.Fatalf("the three stated facts and the one that changed: %v", answer)
	}
	sawChange := false
	for _, each := range facts {
		fact := each.(map[string]any)
		text := fact["text"].(string)
		if strings.Contains(text, "honey") || strings.Contains(text, "lighthouse") {
			t.Fatalf("not an inferred fact, not a work page's: %v", fact)
		}
		if fact["fact_id"] == world.movedFact.ID {
			sawChange = true
			if fact["sort"] != "changed" || fact["now_reads"] != "They live in Hill Village." {
				t.Fatalf("the changed fact says what it reads now: %v", fact)
			}
		}
	}
	if !sawChange {
		t.Fatalf("one fact that changed: %v", facts)
	}
}

// The conversation of a check: a question asked and confirmed, one asked
// and corrected and filed, one the person added; list is empty after, the
// facts asked about are not drafted again, and the overlay counts them.
func TestAMemoryCheckRecordsTheReplies(t *testing.T) {
	world := newCheckWorld(t)
	first := world.call(t, `{"action":"ask","question":"What do you keep on the roof?","answer":"Bees.","fact_ids":["`+world.movedFact.ID+`"]}`)["question_id"].(string)
	second := world.call(t, `{"action":"ask","question":"Where do you live?","answer":"Harbor Town."}`)["question_id"].(string)
	if unanswered := world.call(t, `{"action":"list"}`)["unanswered"].([]any); len(unanswered) != 2 {
		t.Fatalf("two waiting: %v", unanswered)
	}
	overlay := world.tool.Overlay(tools.WithRun(context.Background(), world.run))
	if !strings.Contains(overlay, "2 question(s)") || !strings.Contains(overlay, second) {
		t.Fatalf("the overlay carries the open questions: %s", overlay)
	}
	world.call(t, `{"action":"record","question_id":"`+first+`","reply":"confirmed"}`)
	world.call(t, `{"action":"record","question_id":"`+second+`","reply":"corrected","answer":"Hill Village.","outdated_answer":"Harbor Town."}`)
	world.call(t, `{"action":"filed","question_id":"`+second+`"}`)
	world.call(t, `{"action":"add","question":"What car do you drive now?","answer":"A green van.","outdated_answer":"A red hatchback."}`)
	if unanswered := world.call(t, `{"action":"list"}`)["unanswered"].([]any); len(unanswered) != 0 {
		t.Fatalf("nothing waiting: %v", unanswered)
	}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		questions, err := tx.ListAgentEvaluationQuestions(world.run.agent.ID, nil)
		if err != nil || len(questions) != 3 {
			t.Fatalf("three questions: %v %v", questions, err)
		}
		byText := map[string]*models.AgentEvaluationQuestion{}
		for _, question := range questions {
			byText[question.QuestionText] = question
		}
		corrected := byText["Where do you live?"]
		if corrected.QuestionState != models.EvaluationQuestionCorrected || corrected.ExpectedAnswer != "Hill Village." ||
			corrected.OutdatedAnswer != "Harbor Town." || corrected.QuestionKind != models.EvaluationQuestionChanged || !corrected.IsAnswerFiledAfter {
			t.Fatalf("the correction, as a change, filed after: %+v", corrected)
		}
		added := byText["What car do you drive now?"]
		if added.QuestionState != models.EvaluationQuestionConfirmed || added.QuestionKind != models.EvaluationQuestionChanged {
			t.Fatalf("what the person supplied is confirmed: %+v", added)
		}
	})
	for _, each := range world.call(t, `{"action":"draft"}`)["facts"].([]any) {
		if each.(map[string]any)["fact_id"] == world.movedFact.ID {
			t.Fatal("a fact already asked about is not drafted again")
		}
	}
	if _, err := world.tool.Run(tools.WithRun(context.Background(), world.run), &tools.Call{ID: "c2", Arguments: []byte(`{"action":"record","question_id":"` + first + `","reply":"corrected"}`)}); err == nil {
		t.Fatal("a correction without the answer is refused")
	}
}

// A draft asks about what the person can confirm without looking it up:
// a fact about somebody they know that they told the agent, not one read
// out of a work archive, however it sits on a page about a person.
func TestADraftLeavesOutWhatWasReadFromWork(t *testing.T) {
	world := newCheckWorld(t)
	var told, read, readOnSelf *models.AgentFact
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		self, err := tx.GetAgentNode(world.run.agent.ID, "self")
		if err != nil || self == nil {
			t.Fatalf("self: %v %v", self, err)
		}
		// On the self page, but read out of a repository: not theirs to
		// confirm offhand either.
		if readOnSelf, err = tx.AddAgentFact(&models.AgentFact{AgentID: world.run.agent.ID, NodeID: self.ID, Kind: models.FactPlain, Text: "They reviewed a change to the build.",
			Evidence: []models.Evidence{{Kind: models.EvidenceRepository, ID: "repository-1"}}}); err != nil {
			t.Fatal(err)
		}
		page, err := tx.PutAgentNode(&models.AgentNode{AgentID: world.run.agent.ID, Path: "people/sam-rivers", Kind: models.NodePerson, Name: "Sam Rivers"})
		if err != nil {
			t.Fatal(err)
		}
		if told, err = tx.AddAgentFact(&models.AgentFact{AgentID: world.run.agent.ID, NodeID: page.ID, Kind: models.FactPlain, Text: "Sam is their brother.",
			Evidence: []models.Evidence{{Kind: models.EvidenceConversation, ID: "conversation-1"}}}); err != nil {
			t.Fatal(err)
		}
		if read, err = tx.AddAgentFact(&models.AgentFact{AgentID: world.run.agent.ID, NodeID: page.ID, Kind: models.FactPlain, Text: "Sam changed the build script.",
			Evidence: []models.Evidence{{Kind: models.EvidenceCommit, ID: "commit-1"}}}); err != nil {
			t.Fatal(err)
		}
	})
	sawTold := false
	for round := 0; round < 3; round++ {
		for _, each := range world.call(t, `{"action":"draft"}`)["facts"].([]any) {
			factId := each.(map[string]any)["fact_id"]
			if factId == read.ID || factId == readOnSelf.ID {
				t.Fatal("a fact read from a commit or a repository is not asked about")
			}
			if factId == told.ID {
				sawTold = true
			}
		}
	}
	if !sawTold {
		t.Fatal("what the person told the agent about somebody is asked about first")
	}
}

// A check counts its questions from its own check-in: one asked for right
// after another ended does not start out "five questions so far".
func TestANewCheckCountsItsOwnQuestions(t *testing.T) {
	world := newCheckWorld(t)
	for _, question := range []string{"What do you keep on the roof?", "Where do you live?"} {
		world.call(t, `{"action":"ask","question":"`+question+`","answer":"Something."}`)
	}
	if overlay := world.tool.Overlay(tools.WithRun(context.Background(), world.run)); !strings.Contains(overlay, "2 question(s)") {
		t.Fatalf("the first check has put two: %s", overlay)
	}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		if _, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: world.run.conversation.ID, Role: "user",
			Content: models.SpeakFirstMarker + " " + models.MemoryCheckOpening + ": a few questions."}); err != nil {
			t.Fatal(err)
		}
	})
	if overlay := world.tool.Overlay(tools.WithRun(context.Background(), world.run)); overlay != "" {
		t.Fatalf("a new check has put none yet: %s", overlay)
	}
	world.call(t, `{"action":"ask","question":"What car do you drive?","answer":"A van."}`)
	if overlay := world.tool.Overlay(tools.WithRun(context.Background(), world.run)); !strings.Contains(overlay, "1 question(s)") {
		t.Fatalf("and one once it asks: %s", overlay)
	}
}
