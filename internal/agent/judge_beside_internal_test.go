package agent

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// besideProvider is a fake provider for a turn whose depth is judged: it
// answers the judge's call, which is not streamed, with a depth it can
// hold back, and the turn's rounds, which are, in the order they come.
type besideProvider struct {
	mutex sync.Mutex

	// events is what the provider saw, in order: "judge asked",
	// "judge answered", and "round <model>" for each round.
	events []string
	// rounds is what each round asked for.
	rounds []besideRound

	// depth is what the judge answers. When isJudgeHeld, it answers only
	// once release is closed.
	depth       string
	isJudgeHeld bool
	release     chan struct{}

	// isFirstRoundWaiting makes the first round of the quick model wait
	// until the judge has answered, so that the judge is known to have
	// come back while that round was running.
	isFirstRoundWaiting bool

	firstRoundSeen     chan struct{}
	judgeAnswered      chan struct{}
	firstRoundOnce     sync.Once
	judgeAnsweredOnce  sync.Once
	isQuickAnswerFinal bool
}

// besideRound is what one round of the turn asked for. A deepened turn is
// given room to reason (see roundTokens), which is how a test sees the
// effort a round was asked with: a chat completion does not carry it.
type besideRound struct {
	model         string
	maximumTokens int
	toolNames     []string
	isResearching bool
}

const (
	besideDefaultTokens = 4000
	besideDeepTokens    = 32000
)

func newBesideProvider(depth string) *besideProvider {
	return &besideProvider{depth: depth, release: make(chan struct{}), firstRoundSeen: make(chan struct{}), judgeAnswered: make(chan struct{})}
}

func (self *besideProvider) record(event string) {
	self.mutex.Lock()
	self.events = append(self.events, event)
	self.mutex.Unlock()
}

func (self *besideProvider) seen() []string {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return append([]string{}, self.events...)
}

func (self *besideProvider) roundsSeen() []besideRound {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return append([]besideRound{}, self.rounds...)
}

func (self *besideProvider) handle(writer http.ResponseWriter, request *http.Request) {
	raw, _ := io.ReadAll(request.Body)
	if answerReactionChoice(writer, raw) {
		return
	}
	var body struct {
		Model               string `json:"model"`
		Stream              bool   `json:"stream"`
		MaxTokens           int    `json:"max_tokens"`
		MaxCompletionTokens int    `json:"max_completion_tokens"`
		Messages            []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		} `json:"messages"`
		Tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}
	_ = json.Unmarshal(raw, &body)
	if !body.Stream {
		writer.Header().Set("Content-Type", "application/json")
		if !strings.Contains(string(raw), "decide how much looking into") {
			_, _ = writer.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{}"},"finish_reason":"stop"}],"usage":{}}`))
			return
		}
		self.record("judge asked")
		if self.isJudgeHeld {
			select {
			case <-self.release:
			case <-request.Context().Done():
				return
			case <-time.After(20 * time.Second):
				return
			}
		}
		self.record("judge answered")
		self.judgeAnsweredOnce.Do(func() { close(self.judgeAnswered) })
		content, _ := json.Marshal(`{"depth": "` + self.depth + `", "reason": "they are asking for care", "searches": ["a planned search"]}`)
		_, _ = writer.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":` + string(content) + `},"finish_reason":"stop"}],"usage":{}}`))
		return
	}
	round := besideRound{model: body.Model, maximumTokens: max(body.MaxTokens, body.MaxCompletionTokens)}
	for _, tool := range body.Tools {
		round.toolNames = append(round.toolNames, tool.Function.Name)
	}
	hasToolResult := false
	for _, message := range body.Messages {
		if message.Role == "tool" {
			hasToolResult = true
		}
		if message.Role == "system" {
			if text, isText := message.Content.(string); isText && strings.Contains(text, "## Researching") {
				round.isResearching = true
			}
		}
	}
	self.mutex.Lock()
	self.events = append(self.events, "round "+body.Model)
	self.rounds = append(self.rounds, round)
	self.mutex.Unlock()
	if body.Model == "quick" {
		self.firstRoundOnce.Do(func() { close(self.firstRoundSeen) })
	}
	writer.Header().Set("Content-Type", "text/event-stream")
	if body.Model == "quick" && !hasToolResult && !self.isQuickAnswerFinal {
		if self.isFirstRoundWaiting {
			select {
			case <-self.judgeAnswered:
			case <-request.Context().Done():
				return
			case <-time.After(20 * time.Second):
			}
			// The judge's reply is read by the turn while this round is
			// still being answered; this leaves it no chance to lose.
			time.Sleep(200 * time.Millisecond)
		}
		_, _ = writer.Write([]byte("data: " + `{"id":"s1","model":"quick","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"datetime","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}` + "\n\ndata: [DONE]\n\n"))
		return
	}
	_, _ = writer.Write([]byte("data: " + `{"id":"s2","model":"` + body.Model + `","choices":[{"delta":{"content":"Here you are."},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":3}}` + "\n\ndata: [DONE]\n\n"))
}

// askBeside runs one turn of the surface against the provider, with the
// server's effort on auto and a quick model, and returns the run once it
// has finished and the notes of the conversation.
func askBeside(t *testing.T, provider *besideProvider, surface string, beforeFinish func()) (*Agent, *AskRun, []*models.AgentMessage) {
	t.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(t)
	t.Cleanup(closeDatabase)
	server := httptest.NewServer(http.HandlerFunc(provider.handle))
	t.Cleanup(server.Close)
	worker, person := digestSplitWorldWith(t, database, server.URL, func(configuration *config.Configuration) {
		configuration.Agent.Effort = "auto"
		configuration.Agent.Models.Quick = "p:quick"
	})
	var conversation *models.AgentConversation
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if conversation, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: person.Agent.ID, Kind: models.AgentConversationMain, LastAt: time.Now()}); err != nil {
			t.Fatalf("CreateAgentConversation: %s", err)
		}
	})
	turn, err := worker.Ask(&AskSettings{Agent: person.Agent, Owner: person.Owner, Operations: &digestSplitOperations{}, Conversation: conversation, Message: "what time is it", Surface: surface})
	if err != nil {
		t.Fatalf("Ask: %s", err)
	}
	if beforeFinish != nil {
		go beforeFinish()
	}
	select {
	case <-turn.done:
	case <-time.After(30 * time.Second):
		t.Fatalf("the turn did not finish; saw %v", provider.seen())
	}
	var stored []*models.AgentMessage
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if stored, err = tx.ListAgentMessages(conversation.ID, nil); err != nil {
			t.Fatalf("ListAgentMessages: %s", err)
		}
	})
	return worker, turn, stored
}

func depthNotes(stored []*models.AgentMessage) []string {
	var notes []string
	for _, message := range stored {
		if message.Role == models.AgentMessageNote && message.Name == string(models.NoteDepth) {
			notes = append(notes, message.Content)
		}
	}
	return notes
}

func indexOfEvent(events []string, event string) int {
	for index, found := range events {
		if found == event {
			return index
		}
	}
	return -1
}

// A turn that starts on the quick model does not wait for the judge: its
// first round is asked while the judge is still thinking, which is the
// two or three seconds a greeting used to wait.
func TestAQuickModelTurnIsAskedBeforeTheJudgeAnswers(t *testing.T) {
	provider := newBesideProvider("answer")
	provider.isJudgeHeld = true
	provider.isQuickAnswerFinal = true
	// The judge is held until the first round has been asked.
	_, turn, _ := askBeside(t, provider, "drawer", func() {
		<-provider.firstRoundSeen
		close(provider.release)
	})
	turn.agent.Wait()
	events := provider.seen()
	round, answered := indexOfEvent(events, "round quick"), indexOfEvent(events, "judge answered")
	if round < 0 || indexOfEvent(events, "judge asked") < 0 {
		t.Fatalf("the quick model and the judge were both asked: %v", events)
	}
	if answered >= 0 && answered < round {
		t.Errorf("the first round waited for the judge: %v", events)
	}
}

// A judge that comes back after the turn has ended changes nothing: the
// turn is over, and the result is dropped.
func TestAJudgeThatAnswersAfterTheTurnEndedIsDropped(t *testing.T) {
	provider := newBesideProvider("dig")
	provider.isJudgeHeld = true
	provider.isQuickAnswerFinal = true
	worker, turn, stored := askBeside(t, provider, "drawer", nil)
	if events := provider.seen(); indexOfEvent(events, "judge answered") >= 0 {
		t.Fatalf("the judge answered before it was released: %v", events)
	}
	close(provider.release)
	worker.Wait()
	if notes := depthNotes(stored); len(notes) != 0 {
		t.Errorf("a depth note for a turn that had ended: %v", notes)
	}
	if turn.settings.Effort != "" || turn.settings.Research || turn.isThinkingHarder.Load() {
		t.Errorf("a late dig changed a finished turn: %+v", turn.settings)
	}
	for _, round := range provider.roundsSeen() {
		if round.model != "quick" {
			t.Errorf("a round on %s", round.model)
		}
	}
}

// A judge that says dig while the quick model is on its first round moves
// the rest of the turn to the ask model, as think_harder would, with the
// effort and the research procedure of a turn that digs, and the
// transcript says why.
func TestADigJudgedBesideTheFirstRoundMovesTheRestToTheAskModel(t *testing.T) {
	provider := newBesideProvider("dig")
	provider.isJudgeHeld = true
	provider.isFirstRoundWaiting = true
	_, turn, stored := askBeside(t, provider, "drawer", func() {
		<-provider.firstRoundSeen
		close(provider.release)
	})
	rounds := provider.roundsSeen()
	if len(rounds) != 2 || rounds[0].model != "quick" || rounds[1].model != "thinker" {
		t.Fatalf("rounds %+v, want quick then thinker", rounds)
	}
	quick, thinker := rounds[0], rounds[1]
	if quick.maximumTokens != besideDefaultTokens || quick.isResearching {
		t.Errorf("the first round was already deepened: %+v", quick)
	}
	if thinker.maximumTokens != besideDeepTokens || turn.settings.Effort != llm.EffortHigh {
		t.Errorf("the ask model was not given the deep effort: %d tokens, effort %q", thinker.maximumTokens, turn.settings.Effort)
	}
	if !thinker.isResearching {
		t.Errorf("the ask model was not given the research procedure")
	}
	offered := func(names []string) bool {
		for _, name := range names {
			if name == thinkHarderToolName {
				return true
			}
		}
		return false
	}
	if !offered(quick.toolNames) || offered(thinker.toolNames) {
		t.Errorf("think_harder offered to the quick model %v and to the ask model %v", offered(quick.toolNames), offered(thinker.toolNames))
	}
	if notes := depthNotes(stored); len(notes) != 1 || notes[0] != "they are asking for care" {
		t.Errorf("depth notes %v", notes)
	}
	if turn.plan != nil {
		t.Errorf("a plan arrived after recall began and was kept: %+v", turn.plan)
	}
}

// A judge that says answer or look leaves the turn on the quick model, as
// it was.
func TestAnAnswerOrLookJudgedBesideTheFirstRoundLeavesTheQuickModel(t *testing.T) {
	for _, depth := range []string{"answer", "look"} {
		provider := newBesideProvider(depth)
		provider.isJudgeHeld = true
		provider.isFirstRoundWaiting = true
		_, _, stored := askBeside(t, provider, "drawer", func() {
			<-provider.firstRoundSeen
			close(provider.release)
		})
		rounds := provider.roundsSeen()
		if len(rounds) != 2 {
			t.Fatalf("%s: rounds %+v", depth, rounds)
		}
		for _, round := range rounds {
			if round.model != "quick" || round.maximumTokens != besideDefaultTokens || round.isResearching {
				t.Errorf("%s: the turn left the quick model or was deepened: %+v", depth, round)
			}
		}
		if notes := depthNotes(stored); len(notes) != 0 {
			t.Errorf("%s: depth notes %v", depth, notes)
		}
	}
}

// A turn that is not eligible for the quick model is judged first, as it
// always was: the judge is asked and answers before the first round.
func TestATurnNotOnTheQuickModelIsJudgedFirst(t *testing.T) {
	provider := newBesideProvider("dig")
	askBeside(t, provider, "cli", nil)
	events := provider.seen()
	answered, round := indexOfEvent(events, "judge answered"), -1
	for index, event := range events {
		if strings.HasPrefix(event, "round ") {
			round = index
			break
		}
	}
	if answered < 0 || round < 0 || answered > round {
		t.Fatalf("the judge did not answer before the first round: %v", events)
	}
	if first := provider.roundsSeen()[0]; first.model != "thinker" || first.maximumTokens != besideDeepTokens || !first.isResearching {
		t.Errorf("the first round was not deepened by the judgement: %+v", first)
	}
}

// The retrieval plan a judge returns beside the turn is only useful before
// recall has run: taken before, recall follows it; taken after, it is
// dropped. A judgement that has not come is not waited for.
func TestALatePlanIsKeptOnlyBeforeRecallBegins(t *testing.T) {
	for _, hasRecalled := range []bool{false, true} {
		late := make(chan depthJudgement, 1)
		run := &AskRun{settings: &AskSettings{}, lateDepth: late}
		run.takeLateDepth(hasRecalled)
		if run.lateDepth == nil {
			t.Fatalf("a judgement that had not come was given up on")
		}
		late <- depthJudgement{depth: depthLook, searches: []string{"a planned search"}}
		run.takeLateDepth(hasRecalled)
		if run.lateDepth != nil {
			t.Errorf("the judgement was not taken")
		}
		if hasRecalled && run.plan != nil {
			t.Errorf("a plan that came after recall was kept: %+v", run.plan)
		}
		if !hasRecalled && (run.plan == nil || len(run.plan.Searches) != 1) {
			t.Errorf("a plan that came before recall was dropped: %+v", run.plan)
		}
		if run.settings.Effort != "" || run.settings.Research || run.isThinkingHarder.Load() {
			t.Errorf("a look deepened the turn: %+v", run.settings)
		}
	}
}
