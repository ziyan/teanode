package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// fakeRun is the least a run can be, for a tool under test.
type fakeRun struct {
	owner  *models.User
	loaded map[string]bool
}

func (self *fakeRun) Owner() *models.User                     { return self.owner }
func (self *fakeRun) Agent() *models.Agent                    { return nil }
func (self *fakeRun) Conversation() *models.AgentConversation { return nil }
func (self *fakeRun) Operations() Operations                  { return nil }
func (self *fakeRun) Database() db.Database                   { return nil }
func (self *fakeRun) Configuration() *config.Configuration    { return nil }
func (self *fakeRun) Surface() string                         { return "cli" }
func (self *fakeRun) Headless() bool                          { return false }
func (self *fakeRun) CanAsk() bool                            { return !self.Headless() }
func (self *fakeRun) ReadOnly() bool                          { return false }
func (self *fakeRun) Offered() []*Tool                        { return nil }
func (self *fakeRun) Loaded() map[string]bool                 { return self.loaded }
func (self *fakeRun) Load(name string)                        { self.loaded[name] = true }

func (self *fakeRun) Storage() storage.Storage { return nil }
func (self *fakeRun) Recall(string)            {}
func (self *fakeRun) Recalled() []string       { return nil }
func (self *fakeRun) Ask(context.Context, string, string, []string) (string, error) {
	return "", nil
}
func (self *fakeRun) Enqueue(db.Transaction, models.AgentJobKind, string, string) error { return nil }
func (self *fakeRun) DraftReply(context.Context, *models.AgentDraftRequest) (*models.AgentDraft, error) {
	return nil, nil
}
func (self *fakeRun) DiscardDraft(context.Context, db.Transaction, string) error { return nil }
func (self *fakeRun) MeaningSearch(context.Context, string, string, int) ([]string, error) {
	return nil, nil
}

// A run in the context is the run a tool gets back; none is an error a
// tool can report, never a nil to fall over.
func TestRunTravelsInTheContext(t *testing.T) {
	if _, err := RunFrom(context.Background()); err == nil {
		t.Fatal("a context without a run should say so")
	}
	run := &fakeRun{owner: &models.User{Username: "alice"}, loaded: map[string]bool{}}
	got, err := RunFrom(WithRun(context.Background(), run))
	if err != nil || got.Owner().Username != "alice" {
		t.Fatalf("got %v, %v", got, err)
	}
}

// Factories register from init and the catalog is what they make, in
// order; a factory is asked again for every catalog built.
func TestRegistryBuildsFromFactoriesInOrder(t *testing.T) {
	Reset()
	defer Reset()
	made := 0
	Register(func() []*Tool {
		made++
		return []*Tool{{Name: "second_tool", Family: FamilyGeneral}}
	})
	Register(func() []*Tool { return []*Tool{{Name: "third_tool", Family: FamilyGeneral}} })
	catalog := Build()
	names := []string{}
	for _, tool := range catalog.All() {
		names = append(names, tool.Name)
	}
	if len(names) != 2 || names[0] != "second_tool" || names[1] != "third_tool" {
		t.Fatalf("catalog %v", names)
	}
	Build()
	if made != 2 {
		t.Fatalf("the factory should be asked per build, was asked %d times", made)
	}
}

// The schema helpers and the argument decoder are what every tool
// package builds on; a model's mangled JSON is repaired on the way in.
func TestDecodeArgumentsRepairs(t *testing.T) {
	type arguments struct {
		Query string `json:"query"`
	}
	decoded, err := DecodeArguments[arguments](&Call{Arguments: []byte(`{"query": "plumber",}`)})
	if err != nil || decoded.Query != "plumber" {
		t.Fatalf("decoded %v, %v", decoded, err)
	}
	schema := Object(map[string]any{"query": StringProperty("the words")}, "query")
	if schema["required"].([]string)[0] != "query" {
		t.Fatal("required")
	}
}

// The operator's policy names families, and the validator prints the list
// it accepts. A family missing from that list validates only because
// anything tool-shaped does, and a name in the list that is no family
// switches off nothing at all.
func TestEveryFamilyIsAPolicyName(t *testing.T) {
	families := []Family{FamilyMailbox, FamilyDomains, FamilyAudit, FamilyPeople, FamilyServer, FamilyAccount, FamilyGeneral, FamilyServers, FamilyBrowser, FamilyComputer, FamilySkills}
	for _, family := range families {
		found := false
		for _, name := range config.AgentToolFamilies {
			if name == string(family) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("the family %q is not in config.AgentToolFamilies, so the policy cannot name it", family)
		}
	}
	for _, name := range config.AgentToolFamilies {
		found := false
		for _, family := range families {
			if string(family) == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("config.AgentToolFamilies offers %q, which is no family", name)
		}
	}
}

// The question a call is judged by and the act it performs read the same
// bytes.
//
// They did not. A tool that tells its risks apart reads the arguments
// strictly and falls back to its own class when they will not parse; the tool
// then decodes the same arguments with the repair a model's output needs, and
// does the thing. So writing the call sloppily -- single quotes are enough --
// turned an outward call into an ordinary write and walked past the
// confirmation: a rule that forwards every message to a stranger, an
// invitation to a list of them, a token, a command on the person's machine.
func TestALooselyWrittenCallIsJudgedByWhatItDoes(t *testing.T) {
	t.Parallel()

	outward := &Tool{
		Name: "example", Risk: RiskWrite,
		RiskOf: func(arguments json.RawMessage) Risk {
			var asked struct {
				Tell []string `json:"tell"`
			}
			if err := json.Unmarshal(arguments, &asked); err == nil && len(asked.Tell) > 0 {
				return RiskOutward
			}
			return ""
		},
	}
	strict := json.RawMessage(`{"tell":["ada@example.com"]}`)
	loose := json.RawMessage(`{'tell':['ada@example.com'],}`)

	if got := outward.RiskFor(strict); got != RiskOutward {
		t.Fatalf("written strictly it is outward: %q", got)
	}
	if got := outward.RiskFor(loose); got != RiskOutward {
		t.Fatalf("and written loosely it is the same call: %q", got)
	}
	if !NeedsConfirmation(outward, loose, nil, nil) {
		t.Fatal("so it is asked about either way")
	}

	// And what the tool then reads is what was judged: the settled form is
	// what every reader gets.
	var asked struct {
		Tell []string `json:"tell"`
	}
	if err := json.Unmarshal(SettledArguments(loose), &asked); err != nil || len(asked.Tell) != 1 {
		t.Fatalf("the settled arguments are the ones the tool acts on: %v %v", asked, err)
	}
	// Arguments that are already JSON are handed back untouched.
	if string(SettledArguments(strict)) != string(strict) {
		t.Fatalf("an ordinary call is not rewritten: %s", SettledArguments(strict))
	}
	// Something that is not JSON at all is left for the tool to refuse in
	// its own words rather than silently becoming an empty call.
	if got := SettledArguments(json.RawMessage("not json")); string(got) != "not json" {
		t.Fatalf("nonsense is passed through: %s", got)
	}
}

// The confirmation card is read by somebody deciding, so a tool that says
// nothing about itself still gets a sentence rather than its own call.
//
// The card used to print "Run mail_act with {"action":"delete_forever",...}",
// which asks a person to approve JSON. A tool that can ask for a word should
// write its own line; this is what the ones that have not yet get.
func TestACardSaysSomethingWithoutAPreview(t *testing.T) {
	t.Parallel()

	tool := &Tool{Name: "queue_retry"}
	line := tool.PreviewLine(context.Background(), json.RawMessage(`{"delivery_id":"01m2","all":false,"note":""}`))
	if line != "Queue retry — delivery id: 01m2" {
		t.Fatalf("the call as a sentence, empty and false left out: %q", line)
	}

	// No arguments at all is the tool's name, not an empty dash.
	if line := tool.PreviewLine(context.Background(), json.RawMessage(`{}`)); line != "Queue retry" {
		t.Fatalf("nothing to say about: %q", line)
	}
	// Arguments that are not an object do not make it print Go's idea of them.
	if line := tool.PreviewLine(context.Background(), json.RawMessage(`not json`)); line != "Queue retry" {
		t.Fatalf("unreadable arguments: %q", line)
	}

	// A tool with a line of its own keeps it, and one that can look
	// something up wins over one that cannot.
	spoken := &Tool{Name: "mail_send", Preview: func(json.RawMessage) string { return "Send it" }}
	if line := spoken.PreviewLine(context.Background(), json.RawMessage(`{}`)); line != "Send it" {
		t.Fatalf("its own line: %q", line)
	}
	spoken.PreviewIn = func(context.Context, json.RawMessage) string { return `Send "Thursday?" to maria@example.net` }
	if line := spoken.PreviewLine(context.Background(), json.RawMessage(`{}`)); line != `Send "Thursday?" to maria@example.net` {
		t.Fatalf("the one that looked it up: %q", line)
	}
}
