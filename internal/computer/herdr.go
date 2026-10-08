package computer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/ziyan/teanode/internal/util/security"
)

// The person's herdr sessions: the Claude Code and Codex sessions they keep
// open in herdr's panes, which their agent works in beside them.
//
// The agent reads what a session said and did, looks at its screen, types
// an instruction into it in view of the person, and answers the question a
// session waits on with the option the person chose, from wherever they
// are. Nothing here starts a coding agent of its own: there is one process,
// one history and one screen, which the person and their agent share.
//
// What state a session is in is decided here, not taken from herdr: herdr
// reads mostly the window title and called a Claude Code question form
// idle. In order, a question recognized on the screen (or, for Codex, one
// waiting in its history file), TeaNode's own hooks when the person
// installed them, Codex's history file, the screen's own "esc to
// interrupt", and herdr's state last.
//
// The program watches every pane, and says without being asked when a
// question comes and goes, and when a session someone watches finishes.
// What it has said is said again after a reconnect until the server
// acknowledges it, as background commands' endings are. A person runs herdr
// on each of their computers, and each computer's program speaks for its
// own.

// FeatureHerdr is the person's herdr sessions on this computer: the herdr_*
// actions, and the herdr messages the program sends unasked.
const FeatureHerdr = "herdr"

// The states of a herdr session, as this program decides them.
const (
	HerdrSessionStateIdle    = "idle"
	HerdrSessionStateWorking = "working"
	HerdrSessionStateAsking  = "asking"
	HerdrSessionStateUnknown = "unknown"
)

// The events the program says unasked.
const (
	HerdrEventKindAsking   = "asking"
	HerdrEventKindAnswered = "answered"
	HerdrEventKindSettled  = "settled"
)

// The bounds of watching herdr.
const (
	// herdrPollEvery is how often every pane is looked at. Herdr's own
	// state is not enough to look only when it changes.
	herdrPollEvery = 3 * time.Second
	// herdrAnswerSettle is how long a form is given to go away once its
	// answer is pressed.
	herdrAnswerSettle = 1500 * time.Millisecond
	// herdrKeyGap is the pause between keys pressed into a form, which
	// redraws after each.
	herdrKeyGap = 250 * time.Millisecond
	// herdrWaitMost bounds a wait, herdrWaitEvery is how often it looks,
	// and herdrWaitUnseen is how long it waits for a turn to show before it
	// takes a session that never looked busy as finished.
	herdrWaitMost   = 600 * time.Second
	herdrWaitEvery  = 2 * time.Second
	herdrWaitUnseen = 10 * time.Second
	// herdrOpenSettle is how long open waits for herdr to recognize the
	// coding agent it started.
	herdrOpenSettle = 15 * time.Second
	// herdrWatchUnseen is how long a watched session that was never seen
	// working is given before it is said to have finished: a turn shorter
	// than a poll is not seen at all.
	herdrWatchUnseen = 30 * time.Second
	// mostHerdrEventsKept bounds what waits for the server's
	// acknowledgement.
	mostHerdrEventsKept = 128
	// How many turns read gives when none are asked for, and at most, and
	// the most lines screen gives.
	herdrDefaultTurnCount = 10
	herdrMostTurnCount    = 100
	herdrMostLineCount    = 2000
)

// HerdrSession is one coding session in one of herdr's panes.
type HerdrSession struct {
	PaneID string `json:"paneId"`
	// PaneName is the pane as the person finds it in herdr: its
	// workspace's label, its tab's when the workspace has several, and the
	// agent's name, or which agent it is, when a tab holds more than one.
	// The id says nothing to a person; this is what is shown, and either
	// names a pane to every action.
	PaneName          string `json:"paneName"`
	CodingAgentKind   string `json:"codingAgentKind"`
	CodingSessionID   string `json:"codingSessionId"`
	HerdrSessionState string `json:"herdrSessionState"`
	// HerdrAgentStatus is what herdr itself said, for when the two
	// disagree.
	HerdrAgentStatus string `json:"herdrAgentStatus"`
	PaneTitle        string `json:"paneTitle"`
	WorkingDirectory string `json:"workingDirectory"`
	TranscriptPath   string `json:"transcriptPath,omitempty"`
	// IsWatched says somebody asked to be told when it next finishes.
	IsWatched bool           `json:"isWatched,omitempty"`
	Question  *HerdrQuestion `json:"question,omitempty"`

	// isScreenUnread says the screen could not be read this time, so what
	// was seen of the pane before stands.
	isScreenUnread bool
}

// HerdrArguments are what every herdr action may be given.
type HerdrArguments struct {
	PaneID              string `json:"paneId,omitempty"`
	TurnCount           int    `json:"turnCount,omitempty"`
	LineCount           int    `json:"lineCount,omitempty"`
	Text                string `json:"text,omitempty"`
	WaitSeconds         int    `json:"waitSeconds,omitempty"`
	QuestionFingerprint string `json:"questionFingerprint,omitempty"`
	OptionNumbers       []int  `json:"optionNumbers,omitempty"`
	// OptionLabels, when given, are the labels of the options chosen, as
	// the person was shown them: an answer whose labels differ is
	// refused, so what they confirmed is what is pressed.
	OptionLabels []string `json:"optionLabels,omitempty"`
	FreeText     string   `json:"freeText,omitempty"`
	// Origin is the server's own note of who watches, handed back when
	// the session finishes.
	Origin json.RawMessage `json:"origin,omitempty"`
	// HerdrEventIDs are the events the server acknowledges.
	HerdrEventIDs []string `json:"herdrEventIds,omitempty"`
	// IsRemoval takes the hooks out rather than putting them in.
	IsRemoval bool `json:"isRemoval,omitempty"`
	// Directory, CodingAgentKind and AgentName open a session: where, which
	// coding agent ("claude" or "codex"), and what to call it; the
	// directory's name when none is given.
	Directory       string `json:"directory,omitempty"`
	CodingAgentKind string `json:"codingAgentKind,omitempty"`
	AgentName       string `json:"agentName,omitempty"`
}

// HerdrEvent is something the program says unasked: a question came, a
// question went, a watched session finished.
type HerdrEvent struct {
	HerdrEventID   string          `json:"herdrEventId"`
	HerdrEventKind string          `json:"herdrEventKind"`
	HerdrSession   *HerdrSession   `json:"herdrSession"`
	Origin         json.RawMessage `json:"origin,omitempty"`
	EventAt        time.Time       `json:"eventAt"`
	// AnswerText is what a question that went was answered with, when that
	// is known: the answer given through TeaNode, or the one Claude Code
	// wrote into its history when the person answered at the keyboard.
	AnswerText     string `json:"answerText,omitempty"`
	isAcknowledged bool
}

// HerdrReadResult is the last turns of a session.
type HerdrReadResult struct {
	HerdrSession *HerdrSession `json:"herdrSession"`
	Turns        []*HerdrTurn  `json:"turns"`
	IsTruncated  bool          `json:"isTruncated"`
}

// HerdrScreenResult is a session's screen.
type HerdrScreenResult struct {
	HerdrSession *HerdrSession `json:"herdrSession"`
	ScreenText   string        `json:"screenText"`
}

// HerdrSendResult says the text was typed.
type HerdrSendResult struct {
	HerdrSession *HerdrSession `json:"herdrSession"`
	IsSent       bool          `json:"isSent"`
}

// HerdrWaitResult is a session once it stopped working, or when the wait
// ran out.
type HerdrWaitResult struct {
	HerdrSession *HerdrSession `json:"herdrSession"`
	IsTimedOut   bool          `json:"isTimedOut"`
}

// HerdrAnswerResult says whether the question went once the answer was
// pressed.
type HerdrAnswerResult struct {
	HerdrSession     *HerdrSession `json:"herdrSession"`
	IsAnswerAccepted bool          `json:"isAnswerAccepted"`
	// AnsweredWith is what was chosen, in words.
	AnsweredWith string `json:"answeredWith"`
}

// Herdr is this program's view of the person's herdr, held across its
// connections to the server.
type Herdr struct {
	client *herdrClient
	home   string

	// refreshes is held while the panes are looked at, so that two looks
	// do not record what they saw out of order and say a question came and
	// went that never did.
	refreshes sync.Mutex

	mutex    sync.Mutex
	sessions map[string]*HerdrSession
	// answers are what questions were answered with through TeaNode, by
	// fingerprint, until the question is seen to go and the answer is told
	// with it.
	answers map[string]string
	// appearances are the questions waiting, by pane: the same question
	// asked again later is another appearance, with another fingerprint,
	// so it is told to the person again, and an answer to the first is not
	// pressed into the second. They are kept in a file, so a question
	// waiting when the program restarts keeps its fingerprint and is not
	// told again.
	appearances map[string]*herdrAppearance
	watches     map[string][]*herdrWatch
	events      []*HerdrEvent
	notify      func(*HerdrEvent)
	listener    int64

	cancel context.CancelFunc
	done   chan struct{}
}

type herdrAppearance struct {
	QuestionFingerprint string `json:"questionFingerprint"`
	AppearanceID        string `json:"appearanceId"`
	// IsTold says the server heard that it was asked.
	IsTold bool `json:"isTold"`
	// missCount is how many looks in a row have not seen it.
	missCount int
}

// herdrAppearancesPath is where the appearances are kept, under the home
// directory.
var herdrAppearancesPath = filepath.Join(".local", "state", "teanode", "herdr-appearances.json")

// loadAppearances reads the appearances kept by the program before.
func loadAppearances(home string) map[string]*herdrAppearance {
	appearances := map[string]*herdrAppearance{}
	data, err := os.ReadFile(filepath.Join(home, herdrAppearancesPath))
	if err == nil {
		_ = json.Unmarshal(data, &appearances)
	}
	return appearances
}

// saveAppearancesLocked keeps them. Called with the lock held; the file is
// small, a few lines a waiting question.
func (self *Herdr) saveAppearancesLocked() {
	data, err := json.Marshal(self.appearances)
	if err != nil {
		return
	}
	path := filepath.Join(self.home, herdrAppearancesPath)
	if os.MkdirAll(filepath.Dir(path), 0o700) == nil {
		_ = writeFileAtomically(path, data)
	}
}

type herdrWatch struct {
	Origin         json.RawMessage `json:"origin"`
	Since          time.Time       `json:"since"`
	HasSeenWorking bool            `json:"hasSeenWorking"`
}

// herdrWatchesPath is where the watches are kept, under the home
// directory: a program restarted (upgraded, or its server deployed) while a
// watched session works still says when it finishes.
var herdrWatchesPath = filepath.Join(".local", "state", "teanode", "herdr-watches.json")

// loadWatches reads the watches kept by the program before. Each is taken
// as having seen its session work: a turn that ended while the program was
// down is said to have finished at the first look.
func loadWatches(home string) map[string][]*herdrWatch {
	watches := map[string][]*herdrWatch{}
	data, err := os.ReadFile(filepath.Join(home, herdrWatchesPath))
	if err == nil {
		_ = json.Unmarshal(data, &watches)
	}
	for _, kept := range watches {
		for _, watch := range kept {
			watch.HasSeenWorking = true
		}
	}
	return watches
}

// saveWatchesLocked keeps them. Called with the lock held.
func (self *Herdr) saveWatchesLocked() {
	data, err := json.Marshal(self.watches)
	if err != nil {
		return
	}
	path := filepath.Join(self.home, herdrWatchesPath)
	if os.MkdirAll(filepath.Dir(path), 0o700) == nil {
		_ = writeFileAtomically(path, data)
	}
}

// NewHerdr is the view of the herdr running for the person whose home
// directory this is. Start watches it.
func NewHerdr(home string) *Herdr {
	return &Herdr{client: newHerdrClient(home), home: home, sessions: map[string]*HerdrSession{},
		appearances: loadAppearances(home), watches: loadWatches(home), answers: map[string]string{}}
}

// Start looks at every pane from now until Close.
func (self *Herdr) Start(ctx context.Context) {
	ctx, self.cancel = context.WithCancel(ctx)
	self.done = make(chan struct{})
	go func() {
		defer close(self.done)
		ticker := time.NewTicker(herdrPollEvery)
		defer ticker.Stop()
		for {
			_, _ = self.refresh(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// Close stops watching.
func (self *Herdr) Close() {
	if self.cancel != nil {
		self.cancel()
		<-self.done
	}
}

// listen makes notify the way events are told, and says again every one
// not yet acknowledged. It returns what undoes it.
func (self *Herdr) listen(notify func(*HerdrEvent)) func() {
	self.mutex.Lock()
	self.listener++
	listener := self.listener
	self.notify = notify
	// A question that came and was answered while no server listened is
	// not told: it would reach the person already answered.
	isAnsweredByKey := map[string]bool{}
	for _, event := range self.events {
		if !event.isAcknowledged && event.HerdrEventKind == HerdrEventKindAnswered && event.HerdrSession.Question != nil {
			isAnsweredByKey[event.HerdrSession.PaneID+"\x00"+event.HerdrSession.Question.QuestionFingerprint] = true
		}
	}
	var unheard []*HerdrEvent
	for _, event := range self.events {
		if event.isAcknowledged {
			continue
		}
		if event.HerdrEventKind == HerdrEventKindAsking && isAnsweredByKey[event.HerdrSession.PaneID+"\x00"+event.HerdrSession.Question.QuestionFingerprint] {
			event.isAcknowledged = true
			continue
		}
		unheard = append(unheard, event)
	}
	self.mutex.Unlock()
	for _, event := range unheard {
		notify(event)
	}
	return func() {
		self.mutex.Lock()
		if self.listener == listener {
			self.notify = nil
		}
		self.mutex.Unlock()
	}
}

// acknowledge marks events the server has heard.
func (self *Herdr) acknowledge(ids []string) *SessionResult {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	kept := self.events[:0]
	isAppearanceChanged := false
	for _, event := range self.events {
		for _, id := range ids {
			if event.HerdrEventID != id {
				continue
			}
			event.isAcknowledged = true
			if event.HerdrEventKind != HerdrEventKindAsking || event.HerdrSession.Question == nil {
				continue
			}
			if appearance := self.appearances[event.HerdrSession.PaneID]; appearance != nil &&
				appearanceFingerprint(appearance) == event.HerdrSession.Question.QuestionFingerprint && !appearance.IsTold {
				appearance.IsTold, isAppearanceChanged = true, true
			}
		}
		if !event.isAcknowledged {
			kept = append(kept, event)
		}
	}
	self.events = kept
	if isAppearanceChanged {
		self.saveAppearancesLocked()
	}
	return &SessionResult{OK: true}
}

// tellLocked queues an event and tells the server, when one is connected.
// Called with the lock held; the telling happens after it is let go.
func (self *Herdr) tellLocked(kind string, session *HerdrSession, origin json.RawMessage, told *[]*HerdrEvent) {
	event := &HerdrEvent{HerdrEventID: security.NewULID(), HerdrEventKind: kind, HerdrSession: session, Origin: origin, EventAt: time.Now()}
	self.events = append(self.events, event)
	if len(self.events) > mostHerdrEventsKept {
		self.events = self.events[len(self.events)-mostHerdrEventsKept:]
	}
	*told = append(*told, event)
}

// refresh looks at every pane, records what it finds, and says what
// changed: a question that came or went, a watched session that finished.
func (self *Herdr) refresh(ctx context.Context) ([]*HerdrSession, error) {
	self.refreshes.Lock()
	defer self.refreshes.Unlock()
	trimHookEvents(self.home)
	agents, err := self.client.listAgents(ctx)
	if err != nil {
		return nil, err
	}
	reports := readHookReports(self.home)
	names := self.paneNames(ctx, agents)
	observed := make([]*HerdrSession, 0, len(agents))
	for _, agent := range agents {
		session := self.observe(ctx, agent, reports)
		session.PaneName = names[agent.PaneID]
		observed = append(observed, session)
	}
	sort.Slice(observed, func(left, right int) bool {
		if observed[left].PaneName != observed[right].PaneName {
			return observed[left].PaneName < observed[right].PaneName
		}
		return observed[left].PaneID < observed[right].PaneID
	})

	self.mutex.Lock()
	var told []*HerdrEvent
	seen := map[string]bool{}
	for _, session := range observed {
		seen[session.PaneID] = true
		self.recordLocked(session, &told)
	}
	for paneId, before := range self.sessions {
		if seen[paneId] {
			continue
		}
		// The pane closed, or the agent in it ended.
		gone := *before
		gone.HerdrSessionState, gone.Question = HerdrSessionStateUnknown, nil
		if before.Question != nil {
			self.tellLocked(HerdrEventKindAnswered, before, nil, &told)
		}
		for _, watch := range self.watches[paneId] {
			self.tellLocked(HerdrEventKindSettled, &gone, watch.Origin, &told)
		}
		if self.watches[paneId] != nil {
			delete(self.watches, paneId)
			self.saveWatchesLocked()
		}
		delete(self.sessions, paneId)
		if self.appearances[paneId] != nil {
			delete(self.appearances, paneId)
			self.saveAppearancesLocked()
		}
	}
	// Watches kept from before a restart, of panes that closed meanwhile.
	for paneId, kept := range self.watches {
		if seen[paneId] {
			continue
		}
		gone := &HerdrSession{PaneID: paneId, HerdrSessionState: HerdrSessionStateUnknown}
		for _, watch := range kept {
			self.tellLocked(HerdrEventKindSettled, gone, watch.Origin, &told)
		}
		delete(self.watches, paneId)
		self.saveWatchesLocked()
	}
	notify := self.notify
	for _, session := range observed {
		session.IsWatched = len(self.watches[session.PaneID]) > 0
	}
	self.mutex.Unlock()
	if notify != nil {
		for _, event := range told {
			notify(event)
		}
	}
	return observed, nil
}

// recordLocked keeps what was seen of one pane and queues what it changed.
func (self *Herdr) recordLocked(session *HerdrSession, told *[]*HerdrEvent) {
	if previous := self.sessions[session.PaneID]; session.isScreenUnread && previous != nil && previous.Question != nil {
		// One read that failed is not a question answered: it would be
		// told again, under another fingerprint, at the next look.
		session.Question, session.HerdrSessionState = previous.Question, previous.HerdrSessionState
		self.sessions[session.PaneID] = session
		return
	}
	if session.Question == nil {
		// Gone for two looks before it is gone: a screen read while it
		// redraws shows no form for a moment.
		if appearance := self.appearances[session.PaneID]; appearance != nil {
			appearance.missCount++
			if appearance.missCount >= 2 {
				delete(self.appearances, session.PaneID)
				self.saveAppearancesLocked()
			}
		}
	} else {
		// The question as recognized names what it asks; the fingerprint
		// handed out names this appearance of it.
		question := *session.Question
		appearance := self.appearances[session.PaneID]
		if appearance == nil || appearance.QuestionFingerprint != question.QuestionFingerprint {
			appearance = &herdrAppearance{QuestionFingerprint: question.QuestionFingerprint, AppearanceID: security.NewULID()}
			self.appearances[session.PaneID] = appearance
			self.saveAppearancesLocked()
		}
		appearance.missCount = 0
		question.QuestionFingerprint = appearanceFingerprint(appearance)
		session.Question = &question
	}
	before := self.sessions[session.PaneID]
	self.sessions[session.PaneID] = session
	var beforeFingerprint, nowFingerprint string
	if before != nil && before.Question != nil {
		beforeFingerprint = before.Question.QuestionFingerprint
	}
	if session.Question != nil {
		nowFingerprint = session.Question.QuestionFingerprint
	}
	if beforeFingerprint != nowFingerprint {
		if beforeFingerprint != "" {
			// Said with the question that went, so its card can be found,
			// and with its answer where that is known.
			answered := *session
			answered.Question = before.Question
			answerText := self.answers[beforeFingerprint]
			delete(self.answers, beforeFingerprint)
			if answerText == "" && session.CodingAgentKind == CodingAgentKindClaude && session.TranscriptPath != "" {
				answerText = claudeAnswerOf(session.TranscriptPath, before.Question.QuestionText)
			}
			self.tellLocked(HerdrEventKindAnswered, &answered, nil, told)
			(*told)[len(*told)-1].AnswerText = answerText
		}
		// One the server heard before this program restarted is not told
		// again.
		if appearance := self.appearances[session.PaneID]; nowFingerprint != "" && (before != nil || appearance == nil || !appearance.IsTold) {
			self.tellLocked(HerdrEventKindAsking, session, nil, told)
		}
	}
	watches := self.watches[session.PaneID]
	var kept []*herdrWatch
	isWatchChanged := false
	for _, watch := range watches {
		if session.HerdrSessionState == HerdrSessionStateWorking {
			isWatchChanged = isWatchChanged || !watch.HasSeenWorking
			watch.HasSeenWorking = true
			kept = append(kept, watch)
			continue
		}
		if !watch.HasSeenWorking && time.Since(watch.Since) < herdrWatchUnseen {
			kept = append(kept, watch)
			continue
		}
		self.tellLocked(HerdrEventKindSettled, session, watch.Origin, told)
		isWatchChanged = true
	}
	if len(kept) == 0 {
		delete(self.watches, session.PaneID)
	} else {
		self.watches[session.PaneID] = kept
	}
	if isWatchChanged {
		self.saveWatchesLocked()
	}
}

// appearanceFingerprint is the fingerprint of one appearance of a question.
func appearanceFingerprint(appearance *herdrAppearance) string {
	hash := sha256.Sum256([]byte(appearance.QuestionFingerprint + "\n" + appearance.AppearanceID))
	return hex.EncodeToString(hash[:])[:16]
}

// observe decides one pane's state from everything there is to look at.
func (self *Herdr) observe(ctx context.Context, agent *herdrAgent, reports map[string]*hookReport) *HerdrSession {
	session := &HerdrSession{
		PaneID: agent.PaneID, CodingAgentKind: agent.Agent, HerdrAgentStatus: agent.AgentStatus,
		PaneTitle: agent.TerminalTitleStripped, WorkingDirectory: agent.ForegroundCwd,
		HerdrSessionState: HerdrSessionStateUnknown,
	}
	if session.WorkingDirectory == "" {
		session.WorkingDirectory = agent.Cwd
	}
	// Under the home directory, from ~, as a terminal's prompt says it.
	if rest, isUnderHome := strings.CutPrefix(session.WorkingDirectory, self.home); isUnderHome && (rest == "" || strings.HasPrefix(rest, "/")) {
		session.WorkingDirectory = "~" + rest
	}
	if agent.AgentSession != nil {
		session.CodingSessionID = agent.AgentSession.Value
		session.TranscriptPath = findTranscript(self.home, agent.Agent, session.CodingSessionID)
	}
	screen, err := self.client.readAgent(ctx, agent.PaneID, "visible", 0)
	if err == nil {
		session.Question = recognizeQuestion(agent.Agent, screen)
	} else {
		session.isScreenUnread = true
	}
	var lifecycle *codexLifecycle
	if agent.Agent == CodingAgentKindCodex && session.TranscriptPath != "" {
		lifecycle, _ = readCodexLifecycle(session.TranscriptPath)
	}
	if session.Question == nil && lifecycle != nil && lifecycle.question != nil && !lifecycle.isWorking {
		session.Question = lifecycle.question
	}
	hook := hookStateOf(reports, session.CodingSessionID, time.Now())
	switch {
	case session.Question != nil:
		session.HerdrSessionState = HerdrSessionStateAsking
	case hook == hookStateWorking:
		session.HerdrSessionState = HerdrSessionStateWorking
	case hook == hookStateIdle:
		session.HerdrSessionState = HerdrSessionStateIdle
	case lifecycle != nil && lifecycle.isWorking:
		session.HerdrSessionState = HerdrSessionStateWorking
	case err == nil && isWorkingOnScreen(screen):
		session.HerdrSessionState = HerdrSessionStateWorking
	case agent.AgentStatus == "working":
		session.HerdrSessionState = HerdrSessionStateWorking
	case agent.AgentStatus == "idle" || agent.AgentStatus == "done":
		session.HerdrSessionState = HerdrSessionStateIdle
	}
	return session
}

// isWorkingOnScreen says the agent shows a turn running: both say "esc to
// interrupt" while they work, in the lines above their composer.
func isWorkingOnScreen(screen string) bool {
	lines := strings.Split(strings.TrimRight(screen, "\n "), "\n")
	tail := strings.Join(lines[max(len(lines)-14, 0):], " ")
	return strings.Contains(tail, "esc to interrupt")
}

// session looks at one pane now.
// A pane is named by its id or by its name, which is matched whatever its
// case; a name two panes share names neither.
func (self *Herdr) session(ctx context.Context, paneId string) (*HerdrSession, error) {
	paneId = strings.TrimSpace(paneId)
	if paneId == "" {
		return nil, errors.New("which pane? name it as list gives it")
	}
	sessions, err := self.refresh(ctx)
	if err != nil {
		return nil, err
	}
	var named []*HerdrSession
	for _, session := range sessions {
		if session.PaneID == paneId {
			return session, nil
		}
		if strings.EqualFold(session.PaneName, paneId) {
			named = append(named, session)
		}
	}
	switch len(named) {
	case 1:
		return named[0], nil
	case 0:
		return nil, fmt.Errorf("no coding agent is running in %q; list says which panes have one", paneId)
	}
	ids := make([]string, 0, len(named))
	for _, session := range named {
		ids = append(ids, session.PaneID)
	}
	return nil, fmt.Errorf("%d panes are called %q; name one by its id: %s", len(named), paneId, strings.Join(ids, ", "))
}

// herdrAgentNames are what the coding agents are called to a person.
var herdrAgentNames = map[string]string{CodingAgentKindClaude: "Claude Code", CodingAgentKindCodex: "Codex"}

// paneNames names each pane with a coding agent in it as the person finds
// it in herdr. Without herdr's labels, a pane is named by its id.
func (self *Herdr) paneNames(ctx context.Context, agents []*herdrAgent) map[string]string {
	workspaces, workspaceErr := self.client.listWorkspaces(ctx)
	tabs, tabErr := self.client.listTabs(ctx)
	names := map[string]string{}
	if workspaceErr != nil || tabErr != nil {
		for _, agent := range agents {
			names[agent.PaneID] = agent.PaneID
		}
		return names
	}
	workspaceOf := map[string]*herdrWorkspace{}
	for _, workspace := range workspaces {
		workspaceOf[workspace.WorkspaceID] = workspace
	}
	tabOf := map[string]*herdrTab{}
	for _, tab := range tabs {
		tabOf[tab.TabID] = tab
	}
	// The agents of each tab, in pane order, to tell several apart.
	agentsOfTab := map[string][]*herdrAgent{}
	sorted := slices.Clone(agents)
	sort.Slice(sorted, func(left, right int) bool { return sorted[left].PaneID < sorted[right].PaneID })
	for _, agent := range sorted {
		agentsOfTab[agent.TabID] = append(agentsOfTab[agent.TabID], agent)
	}
	for _, agent := range sorted {
		workspace := workspaceOf[agent.WorkspaceID]
		if workspace == nil || strings.TrimSpace(workspace.Label) == "" {
			names[agent.PaneID] = agent.PaneID
			continue
		}
		parts := []string{strings.TrimSpace(workspace.Label)}
		if tab := tabOf[agent.TabID]; tab != nil && workspace.TabCount > 1 {
			label := strings.TrimSpace(tab.Label)
			if _, err := strconv.Atoi(label); err == nil || label == "" {
				label = "tab " + label
			}
			parts = append(parts, strings.TrimSpace(label))
		}
		inTab := agentsOfTab[agent.TabID]
		switch {
		// A name that only repeats the workspace's says nothing more.
		case strings.TrimSpace(agent.Name) != "" && !strings.EqualFold(strings.TrimSpace(agent.Name), strings.TrimSpace(workspace.Label)):
			parts = append(parts, strings.TrimSpace(agent.Name))
		case len(inTab) > 1:
			name := herdrAgentNames[agent.Agent]
			if name == "" {
				name = agent.Agent
			}
			sameKind, ordinal := 0, 0
			// Those with a name of their own are told apart by it.
			for _, other := range inTab {
				if other.Agent == agent.Agent && strings.TrimSpace(other.Name) == "" {
					sameKind++
					if other.PaneID == agent.PaneID {
						ordinal = sameKind
					}
				}
			}
			if sameKind > 1 {
				name += " " + strconv.Itoa(ordinal)
			}
			parts = append(parts, name)
		}
		names[agent.PaneID] = strings.Join(parts, " › ")
	}
	return names
}

// named is how a session is called in what is said about it.
func (self *HerdrSession) named() string {
	if self.PaneName != "" && self.PaneName != self.PaneID {
		return self.PaneName + " (" + self.PaneID + ")"
	}
	return self.PaneID
}

// RunHerdr does one herdr action.
func RunHerdr(ctx context.Context, herdr *Herdr, action string, arguments *HerdrArguments) (any, error) {
	if herdr == nil {
		return nil, errors.New("this program does not watch herdr")
	}
	switch action {
	case "herdr_list":
		sessions, err := herdr.refresh(ctx)
		if err != nil {
			return nil, err
		}
		return sessions, nil
	case "herdr_read":
		return herdr.read(ctx, arguments)
	case "herdr_screen":
		return herdr.screen(ctx, arguments)
	case "herdr_send":
		return herdr.send(ctx, arguments)
	case "herdr_wait":
		return herdr.wait(ctx, arguments)
	case "herdr_answer":
		return herdr.answer(ctx, arguments)
	case "herdr_watch":
		return herdr.watch(ctx, arguments)
	case "herdr_acknowledge":
		return herdr.acknowledge(arguments.HerdrEventIDs), nil
	case "herdr_open":
		return herdr.open(ctx, arguments)
	case "herdr_close":
		return herdr.close(ctx, arguments)
	case "herdr_setup":
		return setUpHooks(herdr.home, arguments.IsRemoval)
	}
	return nil, fmt.Errorf("%q is not something this program does", action)
}

func (self *Herdr) read(ctx context.Context, arguments *HerdrArguments) (*HerdrReadResult, error) {
	session, err := self.session(ctx, arguments.PaneID)
	if err != nil {
		return nil, err
	}
	if session.TranscriptPath == "" {
		return nil, fmt.Errorf("%s has no history file to read yet; screen shows what it shows", session.named())
	}
	turnCount := arguments.TurnCount
	if turnCount <= 0 {
		turnCount = herdrDefaultTurnCount
	}
	turns, isTruncated, err := readTranscriptTail(session.TranscriptPath, session.CodingAgentKind, min(turnCount, herdrMostTurnCount))
	if err != nil {
		return nil, fmt.Errorf("cannot read the history of %s: %w", session.named(), err)
	}
	return &HerdrReadResult{HerdrSession: session, Turns: turns, IsTruncated: isTruncated}, nil
}

func (self *Herdr) screen(ctx context.Context, arguments *HerdrArguments) (*HerdrScreenResult, error) {
	session, err := self.session(ctx, arguments.PaneID)
	if err != nil {
		return nil, err
	}
	if arguments.LineCount <= 0 {
		text, err := self.client.readAgent(ctx, session.PaneID, "visible", 0)
		if err != nil {
			return nil, err
		}
		return &HerdrScreenResult{HerdrSession: session, ScreenText: text}, nil
	}
	// The last lines are cut here: herdr given a count of lines answers
	// with none.
	text, err := self.client.readAgent(ctx, session.PaneID, "recent_unwrapped", 0)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	lines = lines[max(len(lines)-min(arguments.LineCount, herdrMostLineCount), 0):]
	return &HerdrScreenResult{HerdrSession: session, ScreenText: strings.Join(lines, "\n")}, nil
}

func (self *Herdr) send(ctx context.Context, arguments *HerdrArguments) (*HerdrSendResult, error) {
	if strings.TrimSpace(arguments.Text) == "" {
		return nil, errors.New("send needs text")
	}
	if hasControlCharacters(arguments.Text, true) {
		return nil, errors.New("the text holds control characters, which would press keys rather than type; send words")
	}
	session, err := self.session(ctx, arguments.PaneID)
	if err != nil {
		return nil, err
	}
	// A session that works takes what is typed as a message in its turn:
	// Claude Code and Codex both read it while they work. One that asks
	// would take it as the answer.
	switch {
	case session.HerdrSessionState == HerdrSessionStateAsking:
		return nil, fmt.Errorf("%s is asking a question; answer it first", session.named())
	case session.HerdrAgentStatus == "blocked":
		return nil, fmt.Errorf("%s shows something waiting for an answer that was not recognized; look at its screen", session.named())
	}
	if err := self.client.call(ctx, "agent.prompt", map[string]any{"target": session.PaneID, "text": arguments.Text}, nil); err != nil {
		return nil, fmt.Errorf("the text may not have been typed into %s; look at its screen before sending again: %w", session.named(), err)
	}
	return &HerdrSendResult{HerdrSession: session, IsSent: true}, nil
}

func (self *Herdr) wait(ctx context.Context, arguments *HerdrArguments) (*HerdrWaitResult, error) {
	wait := time.Duration(arguments.WaitSeconds) * time.Second
	if wait <= 0 {
		wait = 30 * time.Second
	}
	started := time.Now()
	deadline := started.Add(min(wait, herdrWaitMost))
	hasSeenWorking := false
	for {
		session, err := self.session(ctx, arguments.PaneID)
		if err != nil {
			return nil, err
		}
		// Right after a send the session may not show its turn yet; a wait
		// that returned then would say it finished before it started.
		isWorking := session.HerdrSessionState == HerdrSessionStateWorking
		if isWorking {
			hasSeenWorking = true
		} else if hasSeenWorking || session.HerdrSessionState == HerdrSessionStateAsking || time.Since(started) >= herdrWaitUnseen {
			return &HerdrWaitResult{HerdrSession: session}, nil
		}
		if time.Now().After(deadline) {
			return &HerdrWaitResult{HerdrSession: session, IsTimedOut: isWorking}, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(herdrWaitEvery):
		}
	}
}

func (self *Herdr) watch(ctx context.Context, arguments *HerdrArguments) (*HerdrSession, error) {
	if len(arguments.Origin) == 0 {
		return nil, errors.New("watch needs to know whom to tell")
	}
	session, err := self.session(ctx, arguments.PaneID)
	if err != nil {
		return nil, err
	}
	self.mutex.Lock()
	self.watches[session.PaneID] = append(self.watches[session.PaneID], &herdrWatch{
		Origin: arguments.Origin, Since: time.Now(), HasSeenWorking: session.HerdrSessionState == HerdrSessionStateWorking,
	})
	self.saveWatchesLocked()
	self.mutex.Unlock()
	// A copy: the one looked at is the one kept, which an event may be
	// carrying to the server now.
	watched := *session
	watched.IsWatched = true
	return &watched, nil
}

// hasControlCharacters says text holds a character a terminal reads as a
// key: escape, carriage return, tab, a control letter. A line break and a
// tab are allowed where they are typed as text, in a message, and not in an
// answer typed into a form, where tab moves on.
func hasControlCharacters(text string, isLineBreakAllowed bool) bool {
	for _, character := range text {
		if (character == '\n' || character == '\t') && isLineBreakAllowed {
			continue
		}
		if unicode.IsControl(character) {
			return true
		}
	}
	return false
}

// HerdrCloseResult says a session's pane was closed.
type HerdrCloseResult struct {
	HerdrSession *HerdrSession `json:"herdrSession"`
	IsClosed     bool          `json:"isClosed"`
}

// herdrAgentNamePattern is what herdr takes as an agent's name.
var herdrAgentNamePattern = regexp.MustCompile(`[^a-z0-9_-]+`)

// open starts a coding agent in a pane of its own, in a directory of the
// person's: a new tab of the workspace named after the directory when there
// is one, a new workspace otherwise, so it is found where the person would
// look. A coding agent that stops at a question as it starts (whether to
// trust the folder) is open, and asking it.
func (self *Herdr) open(ctx context.Context, arguments *HerdrArguments) (*HerdrSession, error) {
	kind := strings.ToLower(strings.TrimSpace(arguments.CodingAgentKind))
	if kind != CodingAgentKindClaude && kind != CodingAgentKindCodex {
		return nil, fmt.Errorf("open which coding agent? %q is not claude or codex", arguments.CodingAgentKind)
	}
	directory := strings.TrimSpace(arguments.Directory)
	if directory == "" {
		return nil, errors.New("open it in which directory?")
	}
	directory = resolve(self.home, directory)
	if info, err := os.Stat(directory); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory on this computer", directory)
	}
	label := strings.TrimSpace(arguments.AgentName)
	if label == "" {
		label = filepath.Base(directory)
	}
	workspaces, err := self.client.listWorkspaces(ctx)
	if err != nil {
		return nil, err
	}
	var created struct {
		RootPane struct {
			PaneID string `json:"pane_id"`
		} `json:"root_pane"`
	}
	workspaceId := ""
	for _, workspace := range workspaces {
		if strings.EqualFold(strings.TrimSpace(workspace.Label), filepath.Base(directory)) {
			workspaceId = workspace.WorkspaceID
			break
		}
	}
	if workspaceId != "" {
		err = self.client.call(ctx, "tab.create", map[string]any{"workspace_id": workspaceId, "cwd": directory, "label": label}, &created)
	} else {
		err = self.client.call(ctx, "workspace.create", map[string]any{"cwd": directory, "label": filepath.Base(directory)}, &created)
	}
	if err != nil {
		return nil, fmt.Errorf("cannot open a pane in %s: %w", directory, err)
	}
	paneId := created.RootPane.PaneID
	if paneId == "" {
		return nil, errors.New("herdr opened a pane without saying which")
	}
	// A name of herdr's kind, not taken by another agent.
	agents, err := self.client.listAgents(ctx)
	if err != nil {
		return nil, err
	}
	isTaken := map[string]bool{}
	for _, agent := range agents {
		isTaken[agent.Name] = true
	}
	base := strings.Trim(herdrAgentNamePattern.ReplaceAllString(strings.ToLower(label), "-"), "-")
	if base == "" || base[0] < 'a' || base[0] > 'z' {
		base = kind + "-" + base
	}
	base = strings.Trim(firstCharacters(base, 28), "-…")
	name := base
	for index := 2; isTaken[name]; index++ {
		name = base + "-" + strconv.Itoa(index)
	}
	err = self.client.call(ctx, "agent.start", map[string]any{"name": name, "kind": kind, "pane_id": paneId, "timeout_ms": 60000}, nil)
	// One that stopped at a question as it started is open, and asking it.
	var refused *herdrError
	isAsking := errors.As(err, &refused) && refused.Code == "agent_not_ready"
	if err != nil && !isAsking {
		return nil, fmt.Errorf("cannot start %s in %s: %w", herdrAgentNames[kind], directory, err)
	}
	// Herdr names the coding agent in a pane a moment after it starts;
	// answered before, the session would read as nobody's, in no state.
	deadline := time.Now().Add(herdrOpenSettle)
	for {
		session, err := self.session(ctx, paneId)
		if (err == nil && session.CodingAgentKind != "") || time.Now().After(deadline) {
			return session, err
		}
		select {
		case <-ctx.Done():
			return session, nil
		case <-time.After(time.Second):
		}
	}
}

// close ends a session's coding agent and its pane. Not while it works:
// a turn cut off halfway leaves its work half done.
func (self *Herdr) close(ctx context.Context, arguments *HerdrArguments) (*HerdrCloseResult, error) {
	session, err := self.session(ctx, arguments.PaneID)
	if err != nil {
		return nil, err
	}
	if session.HerdrSessionState == HerdrSessionStateWorking {
		return nil, fmt.Errorf("%s is working; wait for it to finish, or interrupt it at the keyboard, before closing it", session.named())
	}
	if err := self.client.call(ctx, "pane.close", map[string]any{"pane_id": session.PaneID}, nil); err != nil {
		return nil, fmt.Errorf("cannot close %s: %w", session.named(), err)
	}
	_, _ = self.refresh(ctx)
	return &HerdrCloseResult{HerdrSession: session, IsClosed: true}, nil
}

// herdrStep is one thing pressed into a pane while answering: keys, or text
// typed.
type herdrStep struct {
	keys []string
	text string
}

func (self *Herdr) answer(ctx context.Context, arguments *HerdrArguments) (*HerdrAnswerResult, error) {
	session, err := self.session(ctx, arguments.PaneID)
	if err != nil {
		return nil, err
	}
	question := session.Question
	if question == nil {
		return nil, fmt.Errorf("%s is not asking anything now; this question was already answered or has changed", session.named())
	}
	if hasControlCharacters(arguments.FreeText, false) {
		return nil, errors.New("the answer holds control characters, which would press keys in the form rather than type; send words")
	}
	if strings.TrimSpace(arguments.QuestionFingerprint) != question.QuestionFingerprint {
		return nil, fmt.Errorf("this question was already answered or has changed; %s now asks %q", session.named(), firstCharacters(question.QuestionText, 120))
	}
	if len(arguments.OptionLabels) > 0 {
		if len(arguments.OptionLabels) != len(arguments.OptionNumbers) {
			return nil, errors.New("give one label for each option chosen")
		}
		for index, number := range arguments.OptionNumbers {
			if !slices.ContainsFunc(question.Options, func(option HerdrQuestionOption) bool {
				return option.OptionNumber == number && option.OptionLabel == arguments.OptionLabels[index]
			}) {
				return nil, fmt.Errorf("option %d of the question in %s is not %q; list it again", number, session.named(), arguments.OptionLabels[index])
			}
		}
	}
	steps, answeredWith, err := answerSteps(question, arguments.OptionNumbers, arguments.FreeText)
	if err != nil {
		return nil, err
	}
	for index, step := range steps {
		if index > 0 {
			time.Sleep(herdrKeyGap)
		}
		if step.text != "" {
			err = self.client.sendText(ctx, session.PaneID, step.text)
		} else {
			err = self.client.sendKeys(ctx, session.PaneID, step.keys)
		}
		if err != nil {
			return nil, fmt.Errorf("answering %s stopped partway; look at its screen: %w", session.named(), err)
		}
	}
	time.Sleep(herdrAnswerSettle)
	after, err := self.session(ctx, session.PaneID)
	if err != nil {
		return nil, err
	}
	// Several answers are reviewed before they are sent; the person chose
	// them already, so the review is submitted for them.
	if question.IsMultipleChoice && after.Question != nil && strings.Contains(after.Question.QuestionText, "Ready to submit your answers?") {
		if err := self.client.sendKeys(ctx, session.PaneID, []string{"1"}); err == nil {
			time.Sleep(herdrAnswerSettle)
			if again, err := self.session(ctx, session.PaneID); err == nil {
				after = again
			}
		}
	}
	// Some forms move the cursor to the number pressed and wait for enter
	// (Codex's question whether to trust a folder), where most take the
	// number as the answer. Still the same question, with the cursor on the
	// option chosen, is one of the first kind.
	if after.Question != nil && after.Question.QuestionFingerprint == question.QuestionFingerprint && !question.IsMultipleChoice &&
		!question.IsFromTranscript && !question.IsNumberless && len(arguments.OptionNumbers) == 1 &&
		after.Question.CursorOptionNumber == arguments.OptionNumbers[0] && strings.TrimSpace(arguments.FreeText) == "" {
		if err := self.client.sendKeys(ctx, session.PaneID, []string{"enter"}); err == nil {
			time.Sleep(herdrAnswerSettle)
			if again, err := self.session(ctx, session.PaneID); err == nil {
				after = again
			}
		}
	}
	isAccepted := after.Question == nil || after.Question.QuestionFingerprint != question.QuestionFingerprint
	if isAccepted {
		self.mutex.Lock()
		self.answers[question.QuestionFingerprint] = answeredWith
		self.mutex.Unlock()
	}
	return &HerdrAnswerResult{HerdrSession: after, IsAnswerAccepted: isAccepted, AnsweredWith: answeredWith}, nil
}

// answerSteps are the keys that answer a question with the options chosen,
// or with text, as the coding agents take them: a form's option is chosen
// by its number, one of several is ticked by its number and the choice
// moved on from with right, typed text goes after its option's number and
// before enter, and a question Codex asked in its history is answered by
// typing the answer as the person's next message.
func answerSteps(question *HerdrQuestion, optionNumbers []int, freeText string) ([]herdrStep, string, error) {
	freeText = strings.TrimSpace(freeText)
	byNumber := map[int]HerdrQuestionOption{}
	freeTextOption := 0
	for _, option := range question.Options {
		byNumber[option.OptionNumber] = option
		if option.HerdrOptionKind == HerdrOptionKindFreeText && freeTextOption == 0 {
			freeTextOption = option.OptionNumber
		}
	}
	// Text alone means the option that takes text.
	if len(optionNumbers) == 0 && freeText != "" {
		if freeTextOption == 0 {
			return nil, "", errors.New("this question takes no typed answer; choose one of its options")
		}
		optionNumbers = []int{freeTextOption}
	}
	if len(optionNumbers) == 0 {
		return nil, "", errors.New("choose an option, by its number")
	}
	var chosen []HerdrQuestionOption
	for _, number := range optionNumbers {
		option, ok := byNumber[number]
		if !ok {
			return nil, "", fmt.Errorf("the question has no option %d", number)
		}
		if option.HerdrOptionKind == HerdrOptionKindFreeText && freeText == "" {
			return nil, "", fmt.Errorf("option %d takes text; say what to type", number)
		}
		chosen = append(chosen, option)
	}
	described := make([]string, 0, len(chosen))
	for _, option := range chosen {
		if option.HerdrOptionKind == HerdrOptionKindFreeText {
			described = append(described, strconv.Quote(freeText))
			continue
		}
		described = append(described, strconv.Itoa(option.OptionNumber)+". "+option.OptionLabel)
	}
	answeredWith := strings.Join(described, ", ")

	if question.IsFromTranscript {
		typed := freeText
		if chosen[0].HerdrOptionKind != HerdrOptionKindFreeText || typed == "" {
			labels := make([]string, 0, len(chosen))
			for _, option := range chosen {
				if option.HerdrOptionKind == HerdrOptionKindFreeText {
					labels = append(labels, freeText)
					continue
				}
				labels = append(labels, option.OptionLabel)
			}
			typed = strings.Join(labels, ", ")
		}
		return []herdrStep{{text: typed}, {keys: []string{"enter"}}}, answeredWith, nil
	}
	if question.IsNumberless {
		if len(chosen) != 1 || chosen[0].HerdrOptionKind != HerdrOptionKindChoice {
			return nil, "", errors.New("this question takes one of its options")
		}
		key, distance := "down", chosen[0].OptionNumber-question.CursorOptionNumber
		if distance < 0 {
			key, distance = "up", -distance
		}
		var keys []string
		for range distance {
			keys = append(keys, key)
		}
		return []herdrStep{{keys: append(keys, "enter")}}, answeredWith, nil
	}
	for _, option := range chosen {
		if option.OptionNumber > 9 {
			return nil, "", fmt.Errorf("option %d cannot be chosen by its number; answer it at the keyboard", option.OptionNumber)
		}
	}
	if question.IsMultipleChoice {
		var steps []herdrStep
		for _, option := range chosen {
			if option.HerdrOptionKind != HerdrOptionKindChoice {
				return nil, "", errors.New("a question that takes several answers is answered with its options only")
			}
			steps = append(steps, herdrStep{keys: []string{strconv.Itoa(option.OptionNumber)}})
		}
		return append(steps, herdrStep{keys: []string{"right"}}), answeredWith, nil
	}
	if len(chosen) != 1 {
		return nil, "", errors.New("this question takes one answer")
	}
	option := chosen[0]
	if option.HerdrOptionKind == HerdrOptionKindFreeText {
		return []herdrStep{{keys: []string{strconv.Itoa(option.OptionNumber)}}, {text: freeText}, {keys: []string{"enter"}}}, answeredWith, nil
	}
	return []herdrStep{{keys: []string{strconv.Itoa(option.OptionNumber)}}}, answeredWith, nil
}
