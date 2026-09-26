package agent

import (
	"context"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
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
const depthRecentMessages = 6

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
	if settings.Headless || settings.Effort != "" || settings.Research {
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
	depth, reason := self.judgeDepth()
	settings.Effort, settings.Research = deepenedTurn(depth)
	log.Infof("the agent of %q judged a message worth %s: %s", settings.Owner.Username, depth, reason)
	if depth != depthDig {
		return
	}
	// Said in the transcript as well as live, so that a reload, or a
	// person reading the conversation later, can see why this answer took
	// longer and looked further than the others. Written once the message
	// it is about is, by the turn; see sayDepth.
	self.depthNote = "looking into this carefully"
	if reason != "" {
		self.depthNote += ": " + reason
	}
}

// judgeDepth asks the fast model how deep the message deserves, from the
// message and the conversation before it. A judgement that fails leaves
// the turn as it would have been without one.
func (self *AskRun) judgeDepth() (string, string) {
	settings := self.settings
	var recent []string
	_ = self.agent.settings.Database.TransactionContext(self.ctx, func(tx db.Transaction) error {
		stored, err := tx.ListAgentMessages(settings.Conversation.ID, nil)
		if err != nil {
			return err
		}
		for _, message := range stored {
			var who string
			switch message.Role {
			case string(llm.RoleUser):
				who = personName(settings.Owner)
			case string(llm.RoleAssistant):
				who = settings.Agent.DisplayName()
			default:
				continue
			}
			if text := strings.TrimSpace(message.Content); text != "" {
				recent = append(recent, who+": "+cutRunes(text, 600))
			}
		}
		return nil
	})
	// The newest message is the one being judged, and may already be
	// stored as the last of these.
	if count := len(recent); count > 0 && strings.HasSuffix(recent[count-1], strings.TrimSpace(cutRunes(settings.Message, 600))) {
		recent = recent[:count-1]
	}
	if len(recent) > depthRecentMessages {
		recent = recent[len(recent)-depthRecentMessages:]
	}
	prompt, err := render("effort_judge.txt", map[string]any{
		"AgentName":  settings.Agent.DisplayName(),
		"PersonName": personName(settings.Owner),
		"Recent":     recent,
		"Message":    cutRunes(settings.Message, 4000),
	})
	if err != nil {
		return depthAnswer, ""
	}
	provider, model, err := self.agent.settings.Registry.ForWork(config.AgentWorkTriage)
	if err != nil {
		return depthAnswer, ""
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
		return depthAnswer, ""
	}
	self.countJudgement(self.agent.settings.Registry.Configuration().Models.ForWork(config.AgentWorkTriage), response.Usage)
	depth, reason := readDepth(response.Message.Content)
	if reason == "" {
		log.Infof("could not read how deep to look from %q", cutRunes(response.Message.Content, 200))
	}
	return depth, reason
}

// readDepth reads the judgement; anything it cannot read is no judgement.
func readDepth(text string) (string, string) {
	judged := readModelAnswer[struct {
		Depth  string `json:"depth"`
		Reason string `json:"reason"`
	}](text, "depth")
	if !judged.IsValid {
		return depthAnswer, ""
	}
	switch depth := strings.ToLower(strings.TrimSpace(judged.Value.Depth)); depth {
	case depthAnswer, depthLook, depthDig:
		return depth, strings.TrimSpace(judged.Value.Reason)
	}
	return depthAnswer, ""
}

// sayDepth writes the judgement under the message it was about, and says
// it live: called by the turn once that message is in the transcript, so
// that a reload shows the note after the words that asked for the care.
func (self *AskRun) sayDepth(tx db.Transaction) error {
	if self.depthNote == "" {
		return nil
	}
	if _, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: self.settings.Conversation.ID, Role: models.AgentMessageNote, Content: self.depthNote}); err != nil {
		return err
	}
	self.emit(Event{Kind: EventNote, Note: self.depthNote})
	return nil
}
