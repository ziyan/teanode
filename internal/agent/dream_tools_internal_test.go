package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// What the night is allowed to touch.
//
// The night was refused the person's computer for as long as it existed,
// on the grounds that an unattended run which can execute programs on
// somebody's machine is a different risk from a conversation they are
// watching. The owner read that reasoning and accepted the risk, so the
// night now gets the whole kit -- and the thing most likely to undo that
// quietly is somebody putting the read-only pair back in front of it,
// which is what these tests are here to catch. The call that describes a
// checkout looks the same from a distance and is not the night, so it is
// tested from the other side.

// offeringProvider is a model that answers every round with an empty
// object and keeps the names of the tools it was sent, so a test can say
// what a run was offered rather than guessing from the settings.
func offeringProvider() (*httptest.Server, func() [][]string) {
	var mutex sync.Mutex
	var rounds [][]string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body struct {
			Stream bool `json:"stream"`
			Tools  []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		_ = json.NewDecoder(request.Body).Decode(&body)
		if !body.Stream {
			// Not a round of the loop: a conversation being named, and
			// nothing this is about.
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{}"},"finish_reason":"stop"}],"usage":{}}`))
			return
		}
		names := make([]string, 0, len(body.Tools))
		for _, tool := range body.Tools {
			names = append(names, tool.Function.Name)
		}
		mutex.Lock()
		rounds = append(rounds, names)
		mutex.Unlock()
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("data: {\"id\":\"s1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"content\":\"{}\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":1}}\n\ndata: [DONE]\n\n"))
	}))
	return server, func() [][]string {
		mutex.Lock()
		defer mutex.Unlock()
		return append([][]string(nil), rounds...)
	}
}

// scriptedProvider is a model that answers each round from a script of
// server-sent events, so a test can have it reach for a tool and then
// answer.
func scriptedProvider(rounds []string) *httptest.Server {
	var mutex sync.Mutex
	round := 0
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body struct {
			Stream bool `json:"stream"`
		}
		_ = json.NewDecoder(request.Body).Decode(&body)
		if !body.Stream {
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{}"},"finish_reason":"stop"}],"usage":{}}`))
			return
		}
		mutex.Lock()
		said := rounds[min(round, len(rounds)-1)]
		round++
		mutex.Unlock()
		writer.Header().Set("Content-Type", "text/event-stream")
		for _, line := range strings.Split(said, "\n") {
			_, _ = writer.Write([]byte("data: " + line + "\n\n"))
		}
		_, _ = writer.Write([]byte("data: [DONE]\n\n"))
	}))
}

// toolsNamed is one round's tools as a set.
func toolsNamed(names []string) map[string]bool {
	found := map[string]bool{}
	for _, name := range names {
		found[name] = true
	}
	return found
}

// A call of the night is offered everything the person has, with the
// computer in the round from the start.
//
// Everything: the two lookup tools it always had, and the rest of the kit
// besides. And from the start rather than behind tool_search, because a
// night that has to search for the machine before it may use it spends a
// round of its allowance discovering what the prompt could have told it.
func TestTheNightIsOfferedThePersonsWholeToolKit(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	provider, rounds := offeringProvider()
	defer provider.Close()
	worker, run := digestSplitWorld(t, database, provider.URL)

	// A computer of the person's, attached as the daemon attaches one. It
	// is never asked anything here; what matters is that it is there.
	laptop := &fakeComputer{agent: worker, agentId: run.Agent.ID, answers: func(string, json.RawMessage) (bool, string) {
		return true, `{}`
	}}
	worker.AttachComputer(run.Agent.ID, laptop, "laptop", "linux", "~", "")

	budget := newDreamBudget(run.Configuration(), run.Agent, 1, 0)
	if _, err := worker.dreamThought(context.Background(), run, budget, "Read a batch", "Answer with {}.", true); err != nil {
		t.Fatalf("dreamThought: %s", err)
	}

	asked := rounds()
	if len(asked) == 0 {
		t.Fatal("the night asked the model nothing")
	}
	// The computer's family is in the round itself, not waiting behind
	// tool_search, and so is the graph the night has always filed to.
	offered := toolsNamed(asked[0])
	for _, wanted := range []string{"memory", "shell", "filesystem", "terminal"} {
		if !offered[wanted] {
			t.Errorf("the night is offered %s: %v", wanted, asked[0])
		}
	}
	// And it is not the pair any more. A short prompt still defers the
	// tools nobody named -- knowledge among them, which tool_search
	// loads -- so what says the filter is gone is the breadth of what is
	// left rather than any one name.
	for _, wanted := range []string{"web_search", "schedule", "share_file"} {
		if !offered[wanted] {
			t.Errorf("the night is no longer filtered to the lookup pair, so %s is offered too: %v", wanted, asked[0])
		}
	}
	if len(offered) <= len(lookupTools) {
		t.Errorf("the night is offered more than it could look a page up with: %v", asked[0])
	}
}

// Describing a checkout is not the night and keeps the pair it had.
//
// The two calls used to share one set of tools, and sharing a name is how
// a reach granted to one of them arrives silently at the other. This is
// the ingest's side of that: it looks a page up so a link can point
// somewhere real, and it has no business on anybody's machine.
func TestDescribingACheckoutStillOnlyLooksThingsUp(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	provider, rounds := offeringProvider()
	defer provider.Close()
	worker, run := digestSplitWorld(t, database, provider.URL)
	laptop := &fakeComputer{agent: worker, agentId: run.Agent.ID, answers: func(string, json.RawMessage) (bool, string) {
		return true, `{}`
	}}
	worker.AttachComputer(run.Agent.ID, laptop, "laptop", "linux", "~", "")

	if _, err := worker.think(context.Background(), run, "Described the checkout ~/src/portal", "Answer with {}.",
		lookupTools, roundsFor(run.Configuration(), models.AgentJobIngest), models.AgentJobIngest, config.AgentWorkScan); err != nil {
		t.Fatalf("think: %s", err)
	}

	asked := rounds()
	if len(asked) == 0 {
		t.Fatal("describing a checkout asked the model nothing")
	}
	offered := toolsNamed(asked[0])
	if !offered["memory"] {
		t.Errorf("it still looks a page up before it answers: %v", asked[0])
	}
	// Nothing beyond the pair, which for a short prompt means nothing
	// beyond the one of them the model is given without asking; knowledge
	// is behind tool_search, as it was.
	for _, forbidden := range []string{"shell", "filesystem", "terminal", "web_search", "schedule", "share_file"} {
		if offered[forbidden] {
			t.Errorf("%s has no business describing a checkout: %v", forbidden, asked[0])
		}
	}
}

// And the machine answers it.
//
// Being offered the tool is half of it: every unattended run used to be
// turned away at the computer itself, with "the computer is not reached by
// a run with nobody present", so a night holding the tool would have got a
// refusal and nothing else. This is the other half -- the command reaches
// the daemon and what it printed comes back.
func TestTheNightCanRunSomethingOnTheMachine(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	provider := scriptedProvider([]string{
		`{"id":"s1","model":"m","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"shell","arguments":"{\"command\":\"wc -l records/posts.jsonl\"}"}}]},"finish_reason":"tool_calls"}]}
{"id":"s1","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":1}}`,
		`{"id":"s2","model":"m","choices":[{"delta":{"content":"{}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":1}}`,
	})
	defer provider.Close()
	worker, run := digestSplitWorld(t, database, provider.URL)

	var mutex sync.Mutex
	var commands []string
	laptop := &fakeComputer{agent: worker, agentId: run.Agent.ID, answers: func(action string, arguments json.RawMessage) (bool, string) {
		var asked struct {
			Command string `json:"command"`
		}
		_ = json.Unmarshal(arguments, &asked)
		mutex.Lock()
		commands = append(commands, action+": "+asked.Command)
		mutex.Unlock()
		return true, `{"stdout":"402 records/posts.jsonl\n","stderr":"","exitCode":0}`
	}}
	worker.AttachComputer(run.Agent.ID, laptop, "laptop", "linux", "~", "")

	budget := newDreamBudget(run.Configuration(), run.Agent, 1, 0)
	thinking, err := worker.dreamThought(context.Background(), run, budget, "Read a batch", "Answer with {}.", true)
	if err != nil {
		t.Fatalf("dreamThought: %s", err)
	}

	mutex.Lock()
	ran := append([]string(nil), commands...)
	mutex.Unlock()
	if len(ran) != 1 || ran[0] != "shell: wc -l records/posts.jsonl" {
		t.Fatalf("the command should have reached the machine: %v", ran)
	}

	// And what it printed is in the transcript, where the person can read
	// what their agent did while they slept.
	var said string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		messages, err := tx.ListAgentMessages(thinking.Conversation.ID, nil)
		if err != nil {
			t.Fatalf("ListAgentMessages: %s", err)
		}
		for _, message := range messages {
			said += message.Content
		}
	})
	if !strings.Contains(said, "402 records/posts.jsonl") {
		t.Fatalf("what the machine printed belongs in the transcript: %q", said)
	}
}

// sendingOperations is the API as a person who may send mail, so that the
// tools which leave the server are in the night's catalog at all.
type sendingOperations struct {
	digestSplitOperations
}

func (self *sendingOperations) Permissions() *models.EffectivePermissions {
	return models.NewEffectivePermissions([]models.Grant{
		{Permission: models.PermissionMailRead},
		{Permission: models.PermissionMailSend},
	})
}

// What leaves the server is still refused, because nobody can say yes.
//
// Giving the night every tool put mail_send in front of it: it is a core
// tool, so it is in the round from the start, and nothing filters it out
// any more. What stops it is the confirmation card, which cannot be shown
// to an empty room -- so the call comes back refused and the night is told
// to say what it would have done. This is the line the whole change rests
// on, and it is worth a test that fails loudly if somebody ever decides an
// unattended run may confirm its own calls.
func TestTheNightIsRefusedWhatLeavesTheServer(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	provider := scriptedProvider([]string{
		`{"id":"s1","model":"m","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"mail_send","arguments":"{\"draft\":\"d1\"}"}}]},"finish_reason":"tool_calls"}]}
{"id":"s1","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":1}}`,
		`{"id":"s2","model":"m","choices":[{"delta":{"content":"{}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":1}}`,
	})
	defer provider.Close()
	worker, run := digestSplitWorld(t, database, provider.URL)
	worker.SetOperationsFactory(func(context.Context, *models.User) (Operations, error) {
		return &sendingOperations{}, nil
	})

	budget := newDreamBudget(run.Configuration(), run.Agent, 1, 0)
	thinking, err := worker.dreamThought(context.Background(), run, budget, "Read a batch", "Answer with {}.", true)
	if err != nil {
		t.Fatalf("dreamThought: %s", err)
	}
	var said string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		messages, err := tx.ListAgentMessages(thinking.Conversation.ID, nil)
		if err != nil {
			t.Fatalf("ListAgentMessages: %s", err)
		}
		for _, message := range messages {
			said += message.Content
		}
	})
	if !strings.Contains(said, "needs_confirmation") || !strings.Contains(said, "nobody is present") {
		t.Fatalf("sending should come back refused: %q", said)
	}
}
