package agent_test

import (
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// workDone puts a stretch of work in the conversation: a build that failed,
// then one that worked.
func workDone(t *testing.T, world *rememberWorld, secondExitCode string) {
	t.Helper()
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		for _, message := range []*models.AgentMessage{
			{Role: string(llm.RoleUser), Content: "please build the dashboard"},
			{Role: string(llm.RoleAssistant), ToolCalls: []models.AgentToolCall{{ID: "c1", Name: "shell", Arguments: `{"command":"npm run build"}`}}},
			{Role: string(llm.RoleTool), ToolCallID: "c1", Name: "shell", Content: `{"exitCode":1,"stderr":"engine node 20 required"}`},
			{Role: string(llm.RoleAssistant), ToolCalls: []models.AgentToolCall{{ID: "c2", Name: "shell", Arguments: `{"command":"nvm use 20 && npm run build"}`}}},
			{Role: string(llm.RoleTool), ToolCallID: "c2", Name: "shell", Content: `{"exitCode":` + secondExitCode + `,"stdout":"built in 12s"}`},
			{Role: string(llm.RoleAssistant), Content: "It builds with Node 20."},
		} {
			message.ConversationID = world.conversation.ID
			if _, err := tx.AppendAgentMessage(message); err != nil {
				t.Fatalf("AppendAgentMessage: %s", err)
			}
		}
	})
}

// lessonAnswering answers the lessons pass with one lesson citing the
// second command, and the filing with nothing.
func lessonAnswering(prompt string) string {
	if strings.Contains(prompt, "Write down what this work taught") {
		return `{"lessons": [{"appliesWhen": "building the web dashboard", "approach": "switch to Node 20 with nvm before npm run build",
			"avoid": "the default Node", "verification": "the build finished", "scope": "", "verifiedByCalls": [2], "topic": "dashboard build"}]}`
	}
	return `{"facts":[]}`
}

// A conversation in which a command showed an approach worked files a
// lesson under lessons/, with the command as its evidence; the conversation
// pass reads it with its commands and how each ended.
func TestWorkThatACommandVerifiedTeachesALesson(t *testing.T) {
	world := newRememberWorldThatEmbeds(t, lessonAnswering)
	workDone(t, world, "0")
	world.remember(t)

	prompt := world.promptSaying(t, "Write down what this work taught")
	if !strings.Contains(prompt, "[command 1] shell") || !strings.Contains(prompt, "(exit code 1)") || !strings.Contains(prompt, "(exit code 0)") {
		t.Errorf("the lessons pass was not shown the commands and how they ended:\n%s", prompt)
	}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		page, err := tx.GetAgentNode(world.agent.ID, "lessons/dashboard-build")
		if err != nil || page == nil {
			t.Fatalf("no lessons page: %v", err)
		}
		facts, err := tx.ListAgentFacts(world.agent.ID, page.ID, false, 10)
		if err != nil || len(facts) != 1 {
			t.Fatalf("%d lessons: %v", len(facts), err)
		}
		lesson := facts[0]
		if lesson.Kind != models.FactLesson || !strings.HasPrefix(lesson.Text, "When building the web dashboard: switch to Node 20") ||
			len(lesson.Evidence) != 1 || lesson.Evidence[0].ID != world.conversation.ID || !strings.Contains(lesson.Evidence[0].Quote, "exit code 0") {
			t.Errorf("the lesson: %+v", lesson)
		}
	})
}

// The same stretch of work with no command that succeeded asks nothing
// about lessons: what the assistant said it did is not evidence.
func TestWorkNoCommandVerifiedTeachesNothing(t *testing.T) {
	world := newRememberWorldThatEmbeds(t, lessonAnswering)
	workDone(t, world, "2")
	world.remember(t)

	world.asked.Lock()
	defer world.asked.Unlock()
	for _, prompt := range world.prompts {
		if strings.Contains(prompt, "Write down what this work taught") {
			t.Fatal("the lessons pass was asked about work no command bore out")
		}
	}
}
