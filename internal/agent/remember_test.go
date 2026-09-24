package agent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// rememberWorld is an agent with one conversation in it and a model that
// answers whatever the test tells it to.
//
// The answer is built from the prompt rather than canned, because a fact
// has to cite the message it came from and only the run itself knows what
// those identifiers are.
type rememberWorld struct {
	worker       *agent.Agent
	database     db.Database
	owner        *models.User
	agent        *models.Agent
	conversation *models.AgentConversation

	// Describing runs beside the tick, so more than one goroutine asks the
	// model at once and the prompts are guarded.
	asked   sync.Mutex
	prompts []string
	// Used by a race regression to change a page while its fact meaning
	// is being prepared, before the filing transaction starts.
	embeddingHook func(*http.Request)
}

// theirMessage finds the identifier of the person's own message in the
// transcript the run was given: the first [id] marker followed by "them".
var theirMessage = regexp.MustCompile(`\[([a-zA-Z0-9]+)\] them:`)
var agentMessage = regexp.MustCompile(`\[([a-zA-Z0-9]+)\] you:`)

// newRememberWorld is a world with no embedding model, which is what
// most of these want: nothing is compared by meaning, so every fact a
// run files stands as its own row.
func newRememberWorld(t *testing.T, answer func(prompt string) string) *rememberWorld {
	t.Helper()
	return rememberWorldFor(t, false, answer)
}

// newRememberWorldThatEmbeds is the same world with an embedding model,
// for the checks that only happen at the write boundary: the fold into a
// twin, and the negation guard in front of it.
func newRememberWorldThatEmbeds(t *testing.T, answer func(prompt string) string) *rememberWorld {
	t.Helper()
	return rememberWorldFor(t, true, answer)
}

func rememberWorldFor(t *testing.T, embedding bool, answer func(prompt string) string) *rememberWorld {
	t.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(t)
	t.Cleanup(closeDatabase)

	world := &rememberWorld{database: database}
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.Contains(request.URL.Path, "embeddings") {
			if world.embeddingHook != nil {
				world.embeddingHook(request)
			}
			writeMeaning(writer, request)
			return
		}
		var body struct {
			Stream   bool `json:"stream"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(request.Body).Decode(&body)
		// The last thing the person's side said: every call is a turn of the
		// loop now, so the persona goes first and the prompt after it.
		prompt := ""
		for _, message := range body.Messages {
			if message.Role == "user" {
				prompt = message.Content
			}
		}
		world.asked.Lock()
		world.prompts = append(world.prompts, prompt)
		world.asked.Unlock()
		content, _ := json.Marshal(answer(prompt))
		if body.Stream {
			// A round of the loop streams: one chunk with the whole answer.
			writer.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprintf(writer,
				"data: {\"id\":\"s1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"content\":%s},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":50,\"completion_tokens\":10}}\n\ndata: [DONE]\n\n",
				content)
			return
		}
		_, _ = fmt.Fprintf(writer,
			`{"choices":[{"message":{"role":"assistant","content":%s},"finish_reason":"stop"}],"usage":{"prompt_tokens":50,"completion_tokens":10}}`,
			content)
	}))
	t.Cleanup(provider.Close)

	configuration := config.Default()
	configuration.Agent.Enabled = true
	// No nightly run: a tick queues whatever is due, and whether a night
	// is due depends on the hour the test happens to run at. See
	// schedule_run_test.go for what that cost once.
	dreamingOff := false
	configuration.Agent.Features.Dreaming = &dreamingOff
	configuration.Agent.Providers = []config.AgentProvider{{Name: "fake", Kind: "openai", BaseURL: provider.URL, APIKey: "k"}}
	configuration.Agent.Models.Default = "fake:writer"
	if embedding {
		configuration.Agent.Models.Embedding = "fake:meaning"
	}
	registry, err := llm.Open(&configuration.Agent)
	if err != nil {
		t.Fatalf("llm.Open: %s", err)
	}
	store, err := storage.Open(&storage.Settings{Directory: t.TempDir()})
	if err != nil {
		t.Fatalf("storage.Open: %s", err)
	}
	world.worker = agent.New(&agent.Settings{
		Database: database, Storage: store, Registry: registry,
		Configuration: func() *config.Configuration { return configuration },
		Instance:      "test", Tick: time.Hour,
	})
	// Every call the filing run makes is a turn of the loop, which acts as
	// the person and so needs somebody to act as.
	operations := &fakeOperations{permissions: models.NewEffectivePermissions(nil)}
	world.worker.SetOperationsFactory(func(context.Context, *models.User) (agent.Operations, error) { return operations, nil })

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if world.owner, err = tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if _, err := tx.CreateAddressBook(&models.AddressBook{UserID: world.owner.ID, Name: "Contacts"}); err != nil {
			t.Fatalf("CreateAddressBook: %s", err)
		}
		if world.agent, err = tx.CreateAgent(&models.Agent{UserID: world.owner.ID, Enabled: true}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if world.conversation, err = tx.CreateAgentConversation(&models.AgentConversation{
			AgentID: world.agent.ID, Kind: models.AgentConversationMain,
			Title: "Monday", LastAt: time.Now().Add(-time.Hour),
		}); err != nil {
			t.Fatalf("CreateAgentConversation: %s", err)
		}
		for _, said := range []struct{ role, content string }{
			{"user", "My dentist is Dr Patel on Elm Street. Appointments are always Tuesdays."},
			{"assistant", "Noted. Tuesdays it is."},
		} {
			if _, err := tx.AppendAgentMessage(&models.AgentMessage{
				ConversationID: world.conversation.ID, Role: said.role, Content: said.content,
			}); err != nil {
				t.Fatalf("CreateAgentMessage: %s", err)
			}
		}
	})
	return world
}

// promptSaying is the prompt that carries some words: the worker asks the
// model more than one thing per sweep, and a test wants its own.
func (self *rememberWorld) promptSaying(t *testing.T, words string) string {
	t.Helper()
	self.asked.Lock()
	defer self.asked.Unlock()
	for _, prompt := range self.prompts {
		if strings.Contains(prompt, words) {
			return prompt
		}
	}
	t.Fatalf("no prompt carried %q; %d were asked", words, len(self.prompts))
	return ""
}

// filingPrompts is how many times the filing run asked the model.
func (self *rememberWorld) filingPrompts() int {
	self.asked.Lock()
	defer self.asked.Unlock()
	count := 0
	for _, prompt := range self.prompts {
		if strings.Contains(prompt, "What to file") {
			count++
		}
	}
	return count
}

// say appends a line to the conversation, as the person or as the agent.
func (self *rememberWorld) say(t *testing.T, role, content string) {
	t.Helper()
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		if _, err := tx.AppendAgentMessage(&models.AgentMessage{
			ConversationID: self.conversation.ID, Role: role, Content: content,
		}); err != nil {
			t.Fatalf("AppendAgentMessage: %s", err)
		}
	})
}

// rememberAgain queues the filing of the conversation and runs it at a
// moment of the test's choosing.
//
// The sweep queues at most once a minute and a test that files twice
// runs in rather less than that, so a second round asks for the job
// rather than waiting to be offered one.
func (self *rememberWorld) rememberAgain(t *testing.T, at time.Time) {
	t.Helper()
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		if _, err := self.worker.Enqueue(tx, models.AgentJobRemember, self.agent.ID, "", self.conversation.ID); err != nil {
			t.Fatalf("Enqueue: %s", err)
		}
	})
	if err := self.worker.TickAt(context.Background(), at); err != nil {
		t.Fatalf("Tick: %s", err)
	}
	self.worker.Wait()
}

// remember runs the sweep and the job, as the worker does on its own.
func (self *rememberWorld) remember(t *testing.T) {
	t.Helper()
	if err := self.worker.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %s", err)
	}
	self.worker.Wait()
	// The first tick queues, the second claims and runs.
	if err := self.worker.TickAt(context.Background(), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Tick: %s", err)
	}
	self.worker.Wait()
}

// The run after a conversation files what it taught, makes the page it
// belongs on, keeps the person in the address book, and moves the mark so
// the same words are not filed twice.
//
// This is the property the whole design exists for: nobody asked the
// model to remember anything, and it was filed anyway.
func TestRememberFilesWhatAConversationTaught(t *testing.T) {
	world := newRememberWorld(t, func(prompt string) string {
		said := theirMessage.FindStringSubmatch(prompt)
		if len(said) < 2 {
			return `{"facts":[]}`
		}
		return fmt.Sprintf(`{"facts":[
			{"path":"people/dr-patel","node_kind":"person","node_name":"Dr Patel","kind":"fact",
			 "text":"Their dentist, on Elm Street.","quote":"My dentist is Dr Patel on Elm Street","message_id":%q},
			{"path":"people/dr-patel","kind":"preference",
			 "text":"Wants dentist appointments on Tuesdays.","quote":"Appointments are always Tuesdays","message_id":%q}
		],"links":[],"supersedes":[]}`, said[1], said[1])
	})

	world.remember(t)

	// The sweep also describes a quiet conversation, so the filing run's
	// prompt is the one that asks for filing.
	prompt := world.promptSaying(t, "What to file")
	for _, wanted := range []string{"Dr Patel", "What was said", "What to file", "at most", "Alice"} {
		if !strings.Contains(strings.ToLower(prompt), strings.ToLower(wanted)) {
			t.Fatalf("the prompt should carry %q: %q", wanted, prompt)
		}
	}

	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		node, err := tx.GetAgentNode(world.agent.ID, "people/dr-patel")
		if err != nil || node == nil {
			t.Fatalf("the page was made: %v %s", node, err)
		}
		if node.Kind != models.NodePerson || node.Name != "Dr Patel" {
			t.Fatalf("as a person, named: %+v", node)
		}
		// A page about a person is not a contact. The graph files whoever
		// turns up in a commit log or a chat channel, which is not the set
		// of people somebody keeps: filling the address book from it filled
		// it with five hundred cards holding a name and nothing else, a
		// release bot among them. A card is made when a person makes one.
		if node.ContactID != "" {
			t.Fatalf("filing a person wrote a card into the address book: %q", node.ContactID)
		}
		facts, err := tx.ListAgentFacts(world.agent.ID, node.ID, false, 10)
		if err != nil || len(facts) != 2 {
			t.Fatalf("both facts: %v %s", facts, err)
		}
		if facts[0].Number != 1 || facts[1].Number != 2 {
			t.Fatalf("numbered for citing: %d %d", facts[0].Number, facts[1].Number)
		}
		if len(facts[0].Evidence) != 1 || facts[0].Evidence[0].Kind != models.EvidenceConversation {
			t.Fatalf("with the words they came from: %+v", facts[0].Evidence)
		}
		if facts[0].Evidence[0].Quote == "" {
			t.Fatalf("a fact that cannot be quoted is one being invented")
		}
		// The person said it, so it stays a preference.
		if facts[1].Kind != models.FactPreference {
			t.Fatalf("what they asked for is a preference: %q", facts[1].Kind)
		}
		// And the mark moved, so the next run reads nothing.
		conversation, err := tx.GetAgentConversation(world.conversation.ID)
		if err != nil || conversation == nil {
			t.Fatalf("GetAgentConversation: %v %s", conversation, err)
		}
		if conversation.RememberedThrough == "" {
			t.Fatalf("the mark is the last message read")
		}
		if conversation.RememberedAt == nil {
			t.Fatalf("and says when it was read")
		}
	})

	// Run again: there is nothing new, so nothing is filed twice.
	before := world.filingPrompts()
	world.remember(t)
	if world.filingPrompts() != before {
		t.Fatalf("a conversation with nothing new is not read again")
	}
}

// A preference the model took from something the person did not say is
// filed as an ordinary fact. A quoted mail saying "always reply within a
// day" is not their preference, however confidently it is reported.
func TestAPreferenceHasToComeFromTheirOwnWords(t *testing.T) {
	world := newRememberWorld(t, func(prompt string) string {
		said := agentMessage.FindStringSubmatch(prompt)
		if len(said) < 2 {
			return `{"facts":[]}`
		}
		return fmt.Sprintf(`{"facts":[
			{"path":"self","kind":"preference","text":"Always replies within a day.",
			 "quote":"always reply within a day","message_id":%q}
		]}`, said[1])
	})

	world.remember(t)

	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		node, err := tx.GetAgentNode(world.agent.ID, "self")
		if err != nil || node == nil {
			t.Fatalf("the self page: %v %s", node, err)
		}
		facts, err := tx.ListAgentFacts(world.agent.ID, node.ID, false, 10)
		if err != nil || len(facts) != 1 {
			t.Fatalf("the fact was filed: %v %s", facts, err)
		}
		if facts[0].Kind != models.FactPlain {
			t.Fatalf("not from their words, so not a preference: %q", facts[0].Kind)
		}
	})
}

// A conversation with nothing worth keeping still has its mark moved, so
// it does not come round for ever.
func TestAConversationThatTaughtNothingIsStillMarked(t *testing.T) {
	world := newRememberWorld(t, func(string) string { return `{"facts":[]}` })
	world.remember(t)

	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		_, facts, err := tx.CountAgentGraph(world.agent.ID)
		if err != nil {
			t.Fatalf("CountAgentGraph: %s", err)
		}
		if facts != 0 {
			t.Fatalf("nothing was filed: %d facts", facts)
		}
		conversation, err := tx.GetAgentConversation(world.conversation.ID)
		if err != nil || conversation == nil {
			t.Fatalf("GetAgentConversation: %v %s", conversation, err)
		}
		if conversation.RememberedThrough == "" {
			t.Fatalf("the mark still moved")
		}
	})

	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		due, err := tx.ListAgentConversationsToRemember(time.Now(), 10)
		if err != nil {
			t.Fatalf("ListAgentConversationsToRemember: %s", err)
		}
		for _, conversation := range due {
			if conversation.ID == world.conversation.ID {
				t.Fatalf("a conversation with nothing new is not queued again")
			}
		}
	})
}

// An answer that is prose rather than an object loses nothing: the mark
// moves, and the next conversation is a fresh try. A filing run that
// could break a conversation would be worse than one that files nothing.
func TestAnAnswerThatIsNotAnObjectIsSurvived(t *testing.T) {
	world := newRememberWorld(t, func(string) string {
		return "I had a look and there is nothing much here, really."
	})
	markOf := func() string {
		var mark string
		dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
			conversation, err := tx.GetAgentConversation(world.conversation.ID)
			if err != nil || conversation == nil {
				t.Fatalf("GetAgentConversation: %v %s", conversation, err)
			}
			mark = conversation.RememberedThrough
		})
		return mark
	}
	// The first unreadable answer is not taken as one that found
	// nothing: the mark stays, and the job is tried again.
	world.remember(t)
	if mark := markOf(); mark != "" {
		t.Fatalf("the mark moved past a window nobody read: %q", mark)
	}
	// The second in a row lets the window go, so that one bad window does
	// not block the conversation, and the run says it did.
	world.rememberAgain(t, time.Now().Add(2*time.Hour))
	if mark := markOf(); mark == "" {
		t.Fatalf("after the second unreadable answer the window is let go")
	}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		runs, err := tx.ListAgentConversations(world.agent.ID, []models.AgentConversationKind{models.AgentConversationRun}, nil)
		if err != nil {
			t.Fatalf("ListAgentConversations: %s", err)
		}
		for _, run := range runs {
			if strings.HasPrefix(run.Title, "Skipped ") && strings.Contains(run.Title, "could not be read twice") {
				return
			}
		}
		t.Fatalf("no run says what was skipped: %v", titlesOf(runs))
	})
}

func titlesOf(conversations []*models.AgentConversation) []string {
	titles := make([]string, 0, len(conversations))
	for _, conversation := range conversations {
		titles = append(titles, conversation.Title)
	}
	return titles
}

// A long backlog is read oldest first, sixty messages at a time, over as
// many runs as it takes, and the mark never stands past a message nobody
// read.
//
// It used to keep the *last* sixty and then move the mark to the end of
// the whole list, so a conversation with two hundred unread messages had
// its first hundred and forty marked filed without being read, and
// nothing ever came back for them. The mark is a promise that everything
// behind it has been read.
func TestABacklogIsReadOldestFirstAndNothingIsSkipped(t *testing.T) {
	world := newRememberWorld(t, func(string) string { return `{"facts": []}` })

	// A hundred and fifty worth reading, the two the world seeds
	// included, so the cursor lands inside the conversation twice before
	// it reaches the end.
	for said := 0; said < 74; said++ {
		world.say(t, "user", fmt.Sprintf("Message %d, about the move.", said))
		world.say(t, "assistant", fmt.Sprintf("Noted, %d.", said))
	}

	var messages []*models.AgentMessage
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		var err error
		if messages, err = tx.ListAgentMessages(world.conversation.ID, nil); err != nil {
			t.Fatalf("ListAgentMessages: %s", err)
		}
	})
	if len(messages) != 150 {
		t.Fatalf("a hundred and fifty to read, not %d", len(messages))
	}

	markIs := func(t *testing.T, wanted *models.AgentMessage) {
		t.Helper()
		dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
			conversation, err := tx.GetAgentConversation(world.conversation.ID)
			if err != nil || conversation == nil {
				t.Fatalf("GetAgentConversation: %v %s", conversation, err)
			}
			if conversation.RememberedThrough != wanted.ID {
				t.Fatalf("the mark is at %q, not at %q (%q)",
					conversation.RememberedThrough, wanted.ID, wanted.Content)
			}
		})
	}
	queued := func(t *testing.T) int {
		t.Helper()
		count := 0
		dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
			jobs, err := tx.ListAgentJobs(&db.AgentJobFilter{
				AgentID:  world.agent.ID,
				Kinds:    []models.AgentJobKind{models.AgentJobRemember},
				Statuses: []models.AgentJobStatus{models.AgentJobQueued},
			}, nil)
			if err != nil {
				t.Fatalf("ListAgentJobs: %s", err)
			}
			count = len(jobs)
		})
		return count
	}

	world.remember(t)
	markIs(t, messages[59])
	if queued(t) != 1 {
		t.Fatalf("the rest of the backlog is queued rather than waiting to be noticed")
	}

	world.rememberAgain(t, time.Now().Add(2*time.Hour))
	markIs(t, messages[119])

	world.rememberAgain(t, time.Now().Add(3*time.Hour))
	markIs(t, messages[149])
	if queued(t) != 0 {
		t.Fatalf("and with nothing left unread, nothing is queued")
	}
}

// The tea sentences. Long enough that the fake embedder, which reads a
// sentence as the words of five letters or more in it, puts the two above
// the twin floor: eight words in common, one word apart.
const (
	preferredTea = "They prefer drinking green tea throughout the working morning, before anything difficult."
	stoppedTea   = "They no longer prefer drinking green tea throughout the working morning, before anything difficult."
)

// A correction is not a duplicate. "They prefer tea" and "they no longer
// prefer tea" name the same things and sit on top of each other in the
// vector space, so the twin check offers them to each other and the name
// check has nothing to object to; folding them would throw away whichever
// of the two the run happened to see second.
//
// Both rows stay, and the page states the later one.
func TestANegationIsNeverFolded(t *testing.T) {
	world := newRememberWorldThatEmbeds(t, func(prompt string) string {
		for _, said := range theirSentence.FindAllStringSubmatch(prompt, -1) {
			if !strings.Contains(said[2], "green tea") {
				continue
			}
			return fmt.Sprintf(
				`{"facts": [{"path": "people/dana", "node_kind": "person", "node_name": "Dana", "kind": "fact", "text": %q, "message_id": %q, "quote": %q}]}`,
				said[2], said[1], said[2])
		}
		return `{"facts": []}`
	})

	world.say(t, "user", preferredTea)
	world.say(t, "assistant", "Noted.")
	world.remember(t)

	world.say(t, "user", stoppedTea)
	world.say(t, "assistant", "Noted, that has changed.")
	world.rememberAgain(t, time.Now().Add(2*time.Hour))

	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		node, err := tx.GetAgentNode(world.agent.ID, "people/dana")
		if err != nil || node == nil {
			t.Fatalf("Dana has a page: %v %s", node, err)
		}
		all, err := tx.ListAgentFacts(world.agent.ID, node.ID, true, 50)
		if err != nil {
			t.Fatalf("ListAgentFacts: %s", err)
		}
		if len(all) != 2 {
			lines := make([]string, 0, len(all))
			for _, fact := range all {
				lines = append(lines, fmt.Sprintf("#%d %s", fact.Number, fact.Text))
			}
			t.Fatalf("both statements are kept, not %d:\n%s", len(all), strings.Join(lines, "\n"))
		}
		var older, newer *models.AgentFact
		for _, fact := range all {
			if strings.Contains(fact.Text, "no longer") {
				newer = fact
			} else {
				older = fact
			}
		}
		if older == nil || newer == nil {
			t.Fatalf("one statement of each: %v", all)
		}
		if newer.SupersededBy != "" || newer.Dormant {
			t.Fatalf("the later statement is what the page says: %+v", newer)
		}
		if older.SupersededBy != newer.ID {
			t.Fatalf("the older stands behind the newer, not %q", older.SupersededBy)
		}
		if !older.Dormant {
			t.Fatalf("and is off the page")
		}
		// Kept means kept: the words and the evidence are still there for
		// a person who wants to know what changed and when.
		if older.Text == "" || len(older.Evidence) == 0 {
			t.Fatalf("the superseded row keeps what it said: %+v", older)
		}
		live, err := tx.ListAgentFacts(world.agent.ID, node.ID, false, 50)
		if err != nil {
			t.Fatalf("ListAgentFacts: %s", err)
		}
		if len(live) != 1 || live[0].ID != newer.ID {
			t.Fatalf("the page states one of the two: %v", live)
		}
	})
}

// A fact the conversation replaced is struck, and a strike is written
// into the page's history.
//
// The handler used to set the row dormant through the ordinary update,
// which files no revision at all: the line left the page, the person had
// no way of seeing that it had, and so no way of putting it back.
func TestSupersedingAFactLeavesARevision(t *testing.T) {
	world := newRememberWorld(t, func(prompt string) string {
		if !strings.Contains(prompt, "What to file") {
			return `{"facts":[]}`
		}
		// The second run, which is the one that replaces what the first
		// filed. Told apart by the words it is reading rather than by a
		// counter, because the model is asked from several goroutines.
		if strings.Contains(prompt, "different dentist") {
			// A retirement with nothing filed in its place, which is a
			// real thing for somebody to say -- and which now has to
			// carry the words they said it in, the way a fact does.
			said := theirMessage.FindStringSubmatch(prompt)
			if len(said) < 2 {
				return `{"facts":[]}`
			}
			return fmt.Sprintf(`{"facts":[],"links":[],"supersedes":[
				{"path":"people/dr-patel","number":1,
				 "quote":"I go to a different dentist now","message_id":%q}
			]}`, said[1])
		}
		said := theirMessage.FindStringSubmatch(prompt)
		if len(said) < 2 {
			return `{"facts":[]}`
		}
		return fmt.Sprintf(`{"facts":[
			{"path":"people/dr-patel","node_kind":"person","node_name":"Dr Patel","kind":"fact",
			 "text":"Their dentist, on Elm Street.","quote":"My dentist is Dr Patel on Elm Street","message_id":%q}
		],"links":[],"supersedes":[]}`, said[1])
	})

	world.remember(t)
	world.say(t, "user", "I go to a different dentist now.")
	world.say(t, "assistant", "Noted.")
	world.rememberAgain(t, time.Now().Add(2*time.Hour))

	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		node, err := tx.GetAgentNode(world.agent.ID, "people/dr-patel")
		if err != nil || node == nil {
			t.Fatalf("Dr Patel has a page: %v %s", node, err)
		}
		all, err := tx.ListAgentFacts(world.agent.ID, node.ID, true, 10)
		if err != nil || len(all) != 1 {
			t.Fatalf("the row is kept, not deleted: %v %s", all, err)
		}
		if !all[0].Dormant {
			t.Fatalf("what was superseded is off the page: %+v", all[0])
		}
		live, err := tx.ListAgentFacts(world.agent.ID, node.ID, false, 10)
		if err != nil || len(live) != 0 {
			t.Fatalf("and the page states nothing now: %v %s", live, err)
		}
		revisions, err := tx.ListAgentRevisions(world.agent.ID, node.ID, 50)
		if err != nil {
			t.Fatalf("ListAgentRevisions: %s", err)
		}
		struck := false
		for _, revision := range revisions {
			if revision.Kind == models.RevisionFactStruck {
				struck = true
			}
		}
		if !struck {
			t.Fatalf("the page's history says the line was struck: %v", revisions)
		}
	})
}
