package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/agent/tools"
)

// Thinking harder: a turn starts on a small, fast model, the operator's
// model for calls when it is spoken (agent.voice.askModel) or the quick
// model when it is typed (agent.models.quick). A question beyond it is
// handed, mid-turn, to the model typed turns otherwise use: the same
// conversation, the same tools. The small model writes, or on a call says,
// a few words first, so that the person sees or hears something while the
// larger one reads.

const thinkHarderToolName = "think_harder"

func (self *Agent) thinkHarderTool() *Tool {
	return &Tool{
		Name:   thinkHarderToolName,
		Family: FamilyGeneral,
		Risk:   tools.RiskRead,
		Description: "Hand the rest of this turn to a larger model that reasons better. Use it when the question is hard: several steps of reasoning, a careful judgement or advice, a calculation, code, comparing options, a summary of something long, or anything you are not sure you would get right. " +
			"Before calling it, write one short line (on a call, say it aloud), such as \"Let me think about that properly.\" Call it at the start, before other tools; the larger model then answers, with the same tools.",
		Parameters: tools.Object(map[string]any{
			"reason": tools.StringProperty("in a few words, why this needs more thought"),
		}, "reason"),
		Guidance: "think_harder: you are a small, fast model. Answer greetings, small talk, quick facts, lookups and simple actions yourself (one or two tool calls). Call think_harder at once, after one short line, for anything that needs reasoning: several steps, planning, advice or a judgement, comparing options, math, code, a summary of something long, a multi-step task across tools, or anything you are not sure you would get right.",
		Run:      runThinkHarder,
	}
}

func runThinkHarder(ctx context.Context, call *Call) (*Result, error) {
	arguments, err := tools.DecodeArguments[struct {
		Reason string `json:"reason"`
	}](call)
	if err != nil {
		return nil, err
	}
	run, isTurn := tools.MustRun(ctx).(*AskRun)
	if !isTurn {
		return nil, fmt.Errorf("only a turn of the agent can hand itself to a larger model")
	}
	if run.isThinkingHarder.Swap(true) {
		return nil, fmt.Errorf("you are the larger model already: answer the question yourself")
	}
	log.Infof("%q's turn goes to the larger model: %s", run.settings.Owner.Username, strings.TrimSpace(arguments.Reason))
	content := "From here the larger model answers. You are that model now: answer the person's question carefully, with tools where they help."
	if isSpokenSurface(run.settings.Surface) {
		content += " Keep the answer written for the ear."
	}
	return &Result{
		Content: content,
		Note:    "thinking harder",
	}, nil
}
