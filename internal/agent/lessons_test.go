package agent_test

import (
	"strconv"
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
			{Role: string(llm.RoleTool), ToolCallID: "c1", Name: "shell", Content: "<untrusted-data>\n" + `{"exitCode":1,"stderr":"engine node 20 required"}` + "\n</untrusted-data>"},
			{Role: string(llm.RoleAssistant), ToolCalls: []models.AgentToolCall{{ID: "c2", Name: "shell", Arguments: `{"command":"nvm use 20 && npm run build"}`}}},
			{Role: string(llm.RoleTool), ToolCallID: "c2", Name: "shell", Content: "<untrusted-data>\n" + `{"exitCode":` + secondExitCode + `,"stdout":"built in 12s"}` + "\n</untrusted-data>"},
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
// second command, said twice, and the filing with nothing.
func lessonAnswering(prompt string) string {
	if strings.Contains(prompt, "Write down what this work taught") {
		lesson := `{"appliesWhen": "building the web dashboard", "approach": "switch to Node 20 with nvm before npm run build",
			"avoid": "the default Node", "verification": "the build finished", "scope": "", "verifiedByCalls": [2], "topic": "dashboard build"}`
		return `{"lessons": [` + lesson + `, ` + lesson + `]}`
	}
	return `{"facts":[]}`
}

// A conversation in which a command showed an approach worked files a
// lesson under lessons/, with the command as its evidence and a vector of
// its own, once though the model said it twice; the conversation pass reads
// it with its commands and how each ended, from results kept as they are in
// a transcript.
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
		if count := dbtest.QueryString(t, world.database, `SELECT count(*)::text FROM agent_fact_vector WHERE fact_id = '`+lesson.ID+`'`); count != "1" {
			t.Errorf("the lesson has %s vectors, want its own", count)
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

// longWorkDone puts a long working session in the conversation: a command
// that worked at its start, then enough commands with long output that the
// session is more than one call can read.
func longWorkDone(t *testing.T, world *rememberWorld) {
	t.Helper()
	messages := []*models.AgentMessage{
		{Role: string(llm.RoleUser), Content: "please set up the mail relay"},
		{Role: string(llm.RoleAssistant), ToolCalls: []models.AgentToolCall{{ID: "first", Name: "shell", Arguments: `{"command":"postconf -e relayhost=[relay.example.org]:587"}`}}},
		{Role: string(llm.RoleTool), ToolCallID: "first", Name: "shell", Content: "<untrusted-data>\n" + `{"exitCode":0,"stdout":"relayhost set"}` + "\n</untrusted-data>"},
	}
	for number := 1; number <= 80; number++ {
		callId := "check" + strconv.Itoa(number)
		messages = append(messages,
			&models.AgentMessage{Role: string(llm.RoleAssistant), ToolCalls: []models.AgentToolCall{{ID: callId, Name: "shell", Arguments: `{"command":"tail -n 500 /var/log/mail.log"}`}}},
			&models.AgentMessage{Role: string(llm.RoleTool), ToolCallID: callId, Name: "shell",
				Content: "<untrusted-data>\n" + `{"exitCode":0,"stdout":` + strconv.Quote(strings.Repeat("relay accepted a message\n", 200)) + "}\n</untrusted-data>"})
	}
	messages = append(messages, &models.AgentMessage{Role: string(llm.RoleAssistant), Content: "The relay is set up and delivering."})
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		for _, message := range messages {
			message.ConversationID = world.conversation.ID
			if _, err := tx.AppendAgentMessage(message); err != nil {
				t.Fatalf("AppendAgentMessage: %s", err)
			}
		}
	})
}

// longSessionAnswering answers the part that shows the first command with
// a lesson citing it, and every other part with a lesson citing that same
// first command, which is not in them; laterPartAnswer, when given, is how
// the parts after the first answer instead.
func longSessionAnswering(laterPartAnswer string) func(prompt string) string {
	return func(prompt string) string {
		if !strings.Contains(prompt, "Write down what this work taught") {
			return `{"facts":[]}`
		}
		if strings.Contains(prompt, "[command 1] shell") {
			return `{"lessons": [{"appliesWhen": "sending mail through a relay", "approach": "set relayhost with postconf -e",
				"avoid": "", "verification": "postconf accepted it", "scope": "", "verifiedByCalls": [1], "topic": "mail relay"}]}`
		}
		if laterPartAnswer != "" {
			return laterPartAnswer
		}
		return `{"lessons": [{"appliesWhen": "checking the relay log", "approach": "read its last lines",
			"avoid": "", "verification": "the log showed it", "scope": "", "verifiedByCalls": [1], "topic": "relay log"}]}`
	}
}

// lessonPages is how many lessons each page under lessons/ holds.
func lessonPages(t *testing.T, world *rememberWorld) map[string]int {
	t.Helper()
	pages := map[string]int{}
	for _, path := range []string{"lessons/mail-relay", "lessons/relay-log"} {
		dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
			page, err := tx.GetAgentNode(world.agent.ID, path)
			if err != nil || page == nil {
				return
			}
			facts, err := tx.ListAgentFacts(world.agent.ID, page.ID, false, 10)
			if err != nil {
				t.Fatalf("ListAgentFacts: %s", err)
			}
			pages[path] = len(facts)
		})
	}
	return pages
}

// A working session longer than one call is read in parts, each told which
// part it is, and the lesson from its start, which reading only its end
// used to lose, is filed; a part's lessons are verified by its own commands
// only, so one citing a command of another part is dropped.
func TestALongSessionIsReadInPartsAndItsStartTeaches(t *testing.T) {
	world := newRememberWorldThatEmbeds(t, longSessionAnswering(""))
	longWorkDone(t, world)
	world.remember(t)

	first := world.promptSaying(t, "This is part 1 of ")
	if !strings.Contains(first, "[command 1] shell") || strings.Contains(first, "The earlier parts were read separately") {
		t.Errorf("the first part does not start the session:\n%s", first[:2000])
	}
	second := world.promptSaying(t, "This is part 2 of ")
	if strings.Contains(second, "[command 1] shell") || !strings.Contains(second, "The earlier parts were read separately") {
		t.Errorf("the second part is not told the earlier parts were read separately:\n%s", second[:2000])
	}
	if !strings.Contains(second, "characters of the output left out here]") {
		t.Error("a long output was not shown with how much of it was left out")
	}
	if pages := lessonPages(t, world); pages["lessons/mail-relay"] != 1 || pages["lessons/relay-log"] != 0 {
		t.Errorf("lessons filed: %v, want the start's lesson and not the one citing another part's command", pages)
	}
}

// An answer that cannot be read loses that part's lessons alone: the
// first part's lesson is filed and the conversation is filed to its end.
func TestAnUnreadablePartLosesOnlyItsOwnLessons(t *testing.T) {
	world := newRememberWorldThatEmbeds(t, longSessionAnswering("the model wandered off"))
	longWorkDone(t, world)
	world.remember(t)

	if pages := lessonPages(t, world); pages["lessons/mail-relay"] != 1 {
		t.Errorf("lessons filed: %v, want the first part's", pages)
	}
	world.promptSaying(t, "This is part 3 of ")
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		conversation, err := tx.GetAgentConversation(world.conversation.ID)
		if err != nil {
			t.Fatalf("GetAgentConversation: %s", err)
		}
		messages, err := tx.ListAgentMessages(world.conversation.ID, nil)
		if err != nil {
			t.Fatalf("ListAgentMessages: %s", err)
		}
		if last := messages[len(messages)-1]; conversation.RememberedThrough != last.ID {
			t.Errorf("the mark is %q, want the last message %q", conversation.RememberedThrough, last.ID)
		}
	})
}

// When a later part's call fails, the lessons of the parts before it are
// filed and the mark stops where they stopped, short of the end of the
// window, so the rest is read by the next run.
func TestAPartNotAnsweredLeavesTheRestUnread(t *testing.T) {
	world := newRememberWorldThatEmbeds(t, longSessionAnswering(providerRefuses))
	longWorkDone(t, world)
	world.remember(t)

	if pages := lessonPages(t, world); pages["lessons/mail-relay"] != 1 {
		t.Errorf("lessons filed: %v, want the first part's", pages)
	}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		conversation, err := tx.GetAgentConversation(world.conversation.ID)
		if err != nil {
			t.Fatalf("GetAgentConversation: %s", err)
		}
		messages, err := tx.ListAgentMessages(world.conversation.ID, nil)
		if err != nil {
			t.Fatalf("ListAgentMessages: %s", err)
		}
		last := messages[len(messages)-1]
		if conversation.RememberedThrough == "" || conversation.RememberedThrough == last.ID {
			t.Errorf("the mark is %q, want it short of the last message %q", conversation.RememberedThrough, last.ID)
		}
		for _, message := range messages {
			if message.ID == conversation.RememberedThrough && message.Content != "please set up the mail relay" {
				t.Errorf("the mark is on %q, want the last message the lessons read", message.Content)
			}
		}
	})
}
