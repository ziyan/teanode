package agent

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// Whether a command a skill runs on the person's own computer asks first.
//
// The shell tool runs any command there without asking, and a skill that
// wraps a command is no more dangerous than the command. What the person
// wants a say in is the call that speaks for them or cannot be taken back,
// and nothing in a command's shape says which that is: a list of safe
// commands is walked past by any wrapper. So before such a call runs, the
// fast model reads what it would do and judges it. A judgement that fails
// asks, which is what every such call did before.

// The risks a call may be judged to have.
const (
	callRiskRead        = "read"
	callRiskChange      = "change"
	callRiskOutward     = "outward"
	callRiskDestructive = "destructive"
)

// judgedReason says why a call the tool wants judged should ask the person
// first, in the words they allow it by when they are not there, or nothing
// when it need not ask. The same call judged once in a turn is not judged
// again.
func (self *AskRun) judgedReason(ctx context.Context, tool *Tool, arguments json.RawMessage) models.UnattendedRisk {
	if tool.JudgedCall == nil {
		return ""
	}
	call := tool.JudgedCall(tools.SettledArguments(arguments))
	if call == "" {
		return ""
	}
	self.mutex.Lock()
	unattendedRisk, isJudged := self.judgedCalls[call]
	self.mutex.Unlock()
	if isJudged {
		return unattendedRisk
	}
	callRisk, riskReason := self.judgeCall(ctx, call)
	switch callRisk {
	case callRiskRead, callRiskChange:
		unattendedRisk = ""
	case callRiskOutward:
		unattendedRisk = models.UnattendedRiskOutward
	default:
		// Destructive, and anything the judgement could not name, which
		// is treated as the worst.
		unattendedRisk = models.UnattendedRiskDestructive
	}
	log.Infof("the agent of %q judged a call of %s %s: %s", self.settings.Owner.Username, tool.Name, callRisk, riskReason)
	self.mutex.Lock()
	if self.judgedCalls == nil {
		self.judgedCalls = map[string]models.UnattendedRisk{}
	}
	self.judgedCalls[call] = unattendedRisk
	self.mutex.Unlock()
	return unattendedRisk
}

// judgeCall asks the fast model what the call would do. Anything that goes
// wrong is judged destructive, which asks.
func (self *AskRun) judgeCall(ctx context.Context, call string) (string, string) {
	callRisk, riskReason, usage, isJudged := self.agent.judgeCallText(ctx, self.settings.Agent.DisplayName(), personName(self.settings.Owner), call)
	if isJudged {
		self.countJudgement(self.agent.settings.Registry.Configuration().Models.ForWork(config.AgentWorkTriage), usage)
	}
	return callRisk, riskReason
}

// judgeCallText is the judgement itself, for a turn or for a watch: what
// the call would do, why, what the judgement cost, and whether a judgement
// was had at all. Anything that goes wrong is judged destructive.
func (self *Agent) judgeCallText(ctx context.Context, agentName, person, call string) (string, string, llm.Usage, bool) {
	prompt, err := render("command_judge.txt", map[string]any{
		"AgentName":  agentName,
		"PersonName": person,
		"Call":       cutRunes(call, 6000),
	})
	if err != nil {
		return callRiskDestructive, "the judgement could not be written", llm.Usage{}, false
	}
	provider, model, err := self.settings.Registry.ForWork(config.AgentWorkTriage)
	if err != nil {
		return callRiskDestructive, "no model to judge with", llm.Usage{}, false
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	response, err := provider.Chat(ctx, &llm.ChatRequest{
		// Room for a model that reasons before it writes; see judgeDepth.
		Model: model, JSONObject: true, MaxTokens: 2000,
		Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: prompt}},
	})
	if err != nil {
		return callRiskDestructive, "could not judge: " + err.Error(), llm.Usage{}, false
	}
	callRisk, riskReason := readCallRisk(response.Message.Content)
	return callRisk, riskReason, response.Usage, true
}

// readCallRisk reads the judgement; anything it cannot read asks.
func readCallRisk(text string) (string, string) {
	judged := readModelAnswer[struct {
		CallRisk   string `json:"callRisk"`
		RiskReason string `json:"riskReason"`
	}](text, "callRisk")
	if !judged.IsValid {
		return callRiskDestructive, "could not read the judgement"
	}
	switch callRisk := strings.ToLower(strings.TrimSpace(judged.Value.CallRisk)); callRisk {
	case callRiskRead, callRiskChange, callRiskOutward, callRiskDestructive:
		return callRisk, strings.TrimSpace(judged.Value.RiskReason)
	}
	return callRiskDestructive, "an answer that is not a risk"
}
