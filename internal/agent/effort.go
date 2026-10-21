package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/deferutil"
)

// How hard a turn somebody typed thinks and looks before it answers.
//
// Measured before it was written: on questions from a chat archive about
// how the person's own systems work, turning the model's reasoning up alone
// did not make the answers better -- it reasoned out what is generally true
// of such systems instead of finding what the person's own people had
// written, at up to three times the cost. What the turn needs is to be told
// how to look, and when. So before a turn, a model judges how deep the
// message deserves, from the message and the conversation before it, the
// way the person would: whether they are pushing back on the last answer,
// asking for care, or only saying thanks. The depth decides how much the
// turn reasons and whether it gets the research procedure. See
// docs/planning/adaptive-effort-execplan.md.

// Depths a message may deserve.
const (
	depthAnswer = "answer" // nothing needs finding
	depthLook   = "look"   // one clear answer the notes or a search most likely hold
	depthDig    = "dig"    // looked into properly
)

// depthRecentMessages is how much of the conversation the judgement reads:
// enough to see a pushback on the last answer, not a whole history.
// depthRecentCharacters is how much of each of those messages it reads, and
// depthMessageCharacters how much of the message being judged; a message
// cut to either ends with an ellipsis.
const (
	depthRecentMessages    = 6
	depthRecentCharacters  = 600
	depthMessageCharacters = 4000
)

// plannedSearchCharacters is the longest a planned search may be. A longer
// one is a paraphrase of the message rather than a search, and is dropped
// with a line in the log saying so.
const plannedSearchCharacters = 120

// deepenedTurn is what a depth turns on. Only digging changes the turn:
// measured on answered questions from a chat archive, the research
// procedure without reasoning, and medium reasoning with it, did no better
// than the plain turn, and high reasoning with the procedure no more often
// right but less often inventing, at seven times the cost. So it is kept
// for the messages that deserve it, and the rest are answered as before.
func deepenedTurn(depth string) (effort string, research bool) {
	if depth == depthDig {
		return llm.EffortHigh, true
	}
	return "", false
}

// chooseDepth applies the server's setting to a turn somebody typed: an
// effort for every turn alike, or, where it is auto, the depth judged for
// this message. A turn whose caller already chose -- an evaluation, a run
// with nobody present -- is left as it is.
func (self *AskRun) chooseDepth() {
	settings := self.settings
	// A turn a finished background command woke was not typed, and its
	// message is the wake's framing: judged, the fast model restated the
	// person's earlier request and the note said so a second time.
	if settings.Headless || isWokenSurface(settings.Surface) || settings.Effort != "" || settings.Research {
		return
	}
	// A spoken turn is answered as it comes. The judgement is a second
	// a turn before the model is asked anything, and what it is for is
	// deciding when to dig, which takes minutes a person on a call does
	// not want to wait through.
	if settings.Surface == "voice" {
		return
	}
	switch setting := self.agent.settings.Configuration().Agent.Effort; setting {
	case "":
		return
	case llm.EffortLow:
		settings.Effort = setting
		return
	case llm.EffortMedium, llm.EffortHigh:
		settings.Effort, settings.Research = setting, true
		return
	}
	// A turn that starts on the quick model answers a greeting in about a
	// second and a half, and the judgement took two to three before it:
	// most of what the person waited for. The judgement is made beside
	// the turn's first round instead, and what it finds is taken at the
	// next round boundary; see takeLateDepth. A turn that does not start
	// on the quick model is judged first, as it always was.
	if startingModel(self.agent.settings.Configuration(), settings) != "" {
		self.judgeBesideTurn()
		return
	}
	judgedFrom := time.Now()
	judgement := self.judgeDepth()
	self.logDepth(judgement, judgedFrom)
	settings.Effort, settings.Research = deepenedTurn(judgement.depth)
	if judgement.depth != depthAnswer {
		self.plan = &RetrievalPlan{Searches: judgement.searches, IsBroad: judgement.isBroad}
	}
	if judgement.depth != depthDig {
		return
	}
	// Said in the transcript as well as live, so that a reload, or a
	// person reading the conversation later, can see why this answer took
	// longer and looked further than the others. Written once the message
	// it is about is, by the turn; see sayDepth.
	self.hasDepthNote, self.depthReason = true, judgement.reason
}

// logDepth says in the log what a judgement found and how long it took.
func (self *AskRun) logDepth(judgement depthJudgement, judgedFrom time.Time) {
	log.Infof("the agent of %q judged a message worth %s in %s: %s", self.settings.Owner.Username, judgement.depth, time.Since(judgedFrom).Round(time.Millisecond), judgement.reason)
}

// judgeBesideTurn judges the message in a goroutine of its own and leaves
// the result in lateDepth, for the turn to take at a round boundary. The
// goroutine only hands a result over: it never writes the turn's settings
// or its retrieval plan, which belong to the turn's own goroutine. The
// channel holds one result, so a turn that ended first drops it, and the
// judgement stops with the turn's context. A judgement that fails or times
// out is logged by judgeDepth and arrives as an answer, which changes
// nothing.
func (self *AskRun) judgeBesideTurn() {
	late := make(chan depthJudgement, 1)
	self.lateDepth = late
	self.agent.waitGroup.Add(1)
	go func() {
		defer deferutil.Recover()
		defer self.agent.waitGroup.Done()
		judgedFrom := time.Now()
		judgement := self.judgeDepth()
		self.logDepth(judgement, judgedFrom)
		late <- judgement
	}()
}

// takeLateDepth takes the judgement made beside the turn if it has come,
// without waiting for it. The turn calls it at the start of each round,
// where it reads isThinkingHarder, and hasRecalled says recall has begun.
//
// The retrieval plan is only useful before recall runs, so one that
// arrives after is dropped; recall never waits for the judgement. A dig
// moves the rest of the turn to the larger model exactly as if the quick
// model had called think_harder, with the effort and research procedure
// of a turn judged to dig, and says why in the transcript.
//
// A dig that arrives after the quick model handed the turn over itself, at
// an earlier boundary, is dropped: the larger model has begun its answer
// with the prompt and the reasoning it was given, and changing both under
// it in the middle would only discard its cached prompt. One that arrives
// at the same boundary as the hand-off is applied, since the larger model
// has not been asked anything yet.
func (self *AskRun) takeLateDepth(hasRecalled bool) {
	if self.lateDepth == nil {
		return
	}
	var judgement depthJudgement
	select {
	case judgement = <-self.lateDepth:
		self.lateDepth = nil
	default:
		return
	}
	settings := self.settings
	if judgement.depth != depthAnswer && !hasRecalled {
		self.plan = &RetrievalPlan{Searches: judgement.searches, IsBroad: judgement.isBroad}
	}
	if judgement.depth != depthDig {
		return
	}
	if settings.isPastQuickModel {
		log.Debugf("the judgement to dig came after the larger model began, and was dropped")
		return
	}
	settings.Effort, settings.Research = deepenedTurn(depthDig)
	self.isThinkingHarder.Store(true)
	self.hasDepthNote, self.depthReason = true, judgement.reason
	// The turn has begun, so the note goes under the rounds so far. A note
	// that cannot be written is not worth failing an answer already under
	// way for.
	if err := self.agent.settings.Database.TransactionContext(self.ctx, self.sayDepth); err != nil {
		log.Warningf("cannot write why the agent of %q looked further: %s", settings.Owner.Username, err)
	}
}

// depthJudgement is what the fast model said of a message: how deep it
// deserves, why, and how recall should search for it (see
// followRetrievalPlan).
type depthJudgement struct {
	depth, reason string

	// searches is at most two focused searches beside the message's own
	// words, and isBroad says the message asks about a whole area.
	searches []string
	isBroad  bool
}

// plannedSearchCount is how many searches beside the message's own words a
// judgement may plan.
const plannedSearchCount = 2

// judgeDepth asks the fast model how deep the message deserves, from the
// message and the conversation before it, and how recall should search for
// it. A judgement that fails leaves the turn as it would have been without
// one.
func (self *AskRun) judgeDepth() depthJudgement {
	settings := self.settings
	var recent []string
	_ = self.agent.settings.Database.TransactionContext(self.ctx, func(tx db.Transaction) error {
		// A question judged on its own, for an evaluation, has no
		// conversation before it.
		if settings.Conversation == nil {
			return nil
		}
		stored, err := tx.ListAgentMessages(settings.Conversation.ID, nil)
		if err != nil {
			return err
		}
		for _, message := range withoutPrivateTurns(stored) {
			var who string
			switch message.Role {
			case string(llm.RoleUser):
				who = personName(settings.Owner)
			case string(llm.RoleAssistant):
				who = settings.Agent.DisplayName()
			default:
				continue
			}
			// Marked where cut, so that the judgement does not read the
			// start of a long message as all of it.
			if text := strings.TrimSpace(message.Content); text != "" {
				recent = append(recent, who+": "+cutMarked(text, depthRecentCharacters))
			}
		}
		return nil
	})
	// The newest message is the one being judged, and may already be
	// stored as the last of these.
	if count := len(recent); count > 0 && strings.HasSuffix(recent[count-1], cutMarked(strings.TrimSpace(settings.Message), depthRecentCharacters)) {
		recent = recent[:count-1]
	}
	if len(recent) > depthRecentMessages {
		recent = recent[len(recent)-depthRecentMessages:]
	}
	prompt, err := render("effort_judge.txt", map[string]any{
		"AgentName":  settings.Agent.DisplayName(),
		"PersonName": personName(settings.Owner),
		"Recent":     recent,
		"Message":    cutMarked(settings.Message, depthMessageCharacters),
		"Language":   languageName(Language(settings.Agent, settings.Owner)),
	})
	if err != nil {
		return depthJudgement{depth: depthAnswer}
	}
	provider, model, err := self.agent.settings.Registry.ForWork(config.AgentWorkTriage)
	if err != nil {
		return depthJudgement{depth: depthAnswer}
	}
	ctx, cancel := context.WithTimeout(self.ctx, 15*time.Second)
	defer cancel()
	response, err := provider.Chat(ctx, &llm.ChatRequest{
		// Room for a model that reasons before it writes: at two hundred
		// the reasoning used it all and the answer came back empty.
		Model: model, JSONObject: true, MaxTokens: 2000,
		Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: prompt}},
	})
	if err != nil {
		log.Infof("could not judge how deep to look: %s", err)
		return depthJudgement{depth: depthAnswer}
	}
	self.countJudgement(self.agent.settings.Registry.Configuration().Models.ForWork(config.AgentWorkTriage), response.Usage)
	judgement := readDepth(response.Message.Content)
	if judgement.reason == "" {
		log.Infof("could not read how deep to look from %q", cutRunes(response.Message.Content, 200))
	}
	return judgement
}

// readDepth reads the judgement; anything it cannot read is no judgement.
// A search planned is kept only where it is short and says something the
// message does not already say word for word.
func readDepth(text string) depthJudgement {
	judged := readModelAnswer[struct {
		Depth    string   `json:"depth"`
		Reason   string   `json:"reason"`
		Searches []string `json:"searches"`
		IsBroad  bool     `json:"isBroad"`
	}](text, "depth")
	if !judged.IsValid {
		return depthJudgement{depth: depthAnswer}
	}
	switch depth := strings.ToLower(strings.TrimSpace(judged.Value.Depth)); depth {
	case depthAnswer, depthLook, depthDig:
		judgement := depthJudgement{depth: depth, reason: strings.TrimSpace(judged.Value.Reason), isBroad: judged.Value.IsBroad}
		seen := map[string]bool{}
		for _, search := range judged.Value.Searches {
			search = strings.TrimSpace(search)
			key := strings.ToLower(search)
			if search == "" || seen[key] || len(judgement.searches) >= plannedSearchCount {
				continue
			}
			if len([]rune(search)) > plannedSearchCharacters {
				log.Debugf("dropped a planned search longer than %d characters: %q", plannedSearchCharacters, search)
				continue
			}
			seen[key] = true
			judgement.searches = append(judgement.searches, search)
		}
		return judgement
	}
	return depthJudgement{depth: depthAnswer}
}

// sayDepth writes the judgement under the message it was about, and says
// it live: called by the turn once that message is in the transcript, so
// that a reload shows the note after the words that asked for the care.
func (self *AskRun) sayDepth(tx db.Transaction) error {
	if !self.hasDepthNote {
		return nil
	}
	if _, err := tx.AppendAgentMessage(models.NewAgentNote(self.settings.Conversation.ID, models.NoteDepth, self.depthReason)); err != nil {
		return err
	}
	self.sayNote(models.NoteDepth, self.depthReason)
	return nil
}

// JudgedPlan is what the depth judgement said of a question judged on its
// own: how deep it deserves, why, and the retrieval plan a live turn would
// follow for it.
type JudgedPlan struct {
	Depth  string
	Reason string
	Plan   *RetrievalPlan

	// Cost is what the judgement cost, in the configured currency.
	Cost float64
}

// JudgeRetrievalPlan asks the depth judgement about a question on its own,
// as a turn's first message, for an evaluation to replay the plan a live
// turn would follow. It is one call to the fast model, made only when
// asked for and priced like any other; a replay given the plan asks
// nothing.
func (self *Agent) JudgeRetrievalPlan(ctx context.Context, found *models.Agent, owner *models.User, question string) (*JudgedPlan, error) {
	if self == nil || found == nil || owner == nil {
		return nil, ErrUnavailable
	}
	question = strings.TrimSpace(question)
	if question == "" {
		return nil, fmt.Errorf("a question is needed")
	}
	run := &AskRun{agent: self, settings: &AskSettings{Agent: found, Owner: owner, Message: question}, promptMemories: map[string]bool{}}
	run.ctx = ctx
	judgement := run.judgeDepth()
	judged := &JudgedPlan{Depth: judgement.depth, Reason: judgement.reason, Plan: &RetrievalPlan{Searches: []string{}}, Cost: run.judgementUsage.Cost}
	if judgement.depth != depthAnswer {
		judged.Plan = &RetrievalPlan{Searches: append([]string{}, judgement.searches...), IsBroad: judgement.isBroad}
	}
	return judged, nil
}
