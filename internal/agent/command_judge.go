package agent

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/llm"
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

// judgedToAsk says whether a call the tool wants judged should ask the
// person first. The same call judged once in a turn is not judged again.
func (self *AskRun) judgedToAsk(ctx context.Context, tool *Tool, arguments json.RawMessage) bool {
	if tool.JudgedCall == nil {
		return false
	}
	call := tool.JudgedCall(tools.SettledArguments(arguments))
	if call == "" {
		return false
	}
	self.mutex.Lock()
	isAsking, isJudged := self.judgedCalls[call]
	self.mutex.Unlock()
	if isJudged {
		return isAsking
	}
	callRisk, riskReason := self.judgeCall(ctx, call)
	isAsking = callRisk != callRiskRead && callRisk != callRiskChange
	log.Infof("the agent of %q judged a call of %s %s: %s", self.settings.Owner.Username, tool.Name, callRisk, riskReason)
	self.mutex.Lock()
	if self.judgedCalls == nil {
		self.judgedCalls = map[string]bool{}
	}
	self.judgedCalls[call] = isAsking
	self.mutex.Unlock()
	return isAsking
}

// judgeCall asks the fast model what the call would do. Anything that goes
// wrong is judged destructive, which asks.
func (self *AskRun) judgeCall(ctx context.Context, call string) (string, string) {
	settings := self.settings
	prompt, err := render("command_judge.txt", map[string]any{
		"AgentName":  settings.Agent.DisplayName(),
		"PersonName": personName(settings.Owner),
		"Call":       cutRunes(call, 6000),
	})
	if err != nil {
		return callRiskDestructive, "the judgement could not be written"
	}
	provider, model, err := self.agent.settings.Registry.ForWork(config.AgentWorkTriage)
	if err != nil {
		return callRiskDestructive, "no model to judge with"
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	response, err := provider.Chat(ctx, &llm.ChatRequest{
		// Room for a model that reasons before it writes; see judgeDepth.
		Model: model, JSONObject: true, MaxTokens: 2000,
		Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: prompt}},
	})
	if err != nil {
		return callRiskDestructive, "could not judge: " + err.Error()
	}
	RecordUsage(self.agent.settings.Database, settings.Agent.ID, "", self.agent.settings.Registry.Configuration().Models.ForWork(config.AgentWorkTriage), "ask", response.Usage)
	return readCallRisk(response.Message.Content)
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
