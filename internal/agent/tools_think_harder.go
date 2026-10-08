package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/agent/tools"
)

// Thinking harder: a spoken turn is answered by the operator's model for
// calls (agent.voice.askModel), which starts speaking soon because it is
// small. A question beyond it is handed, mid-turn, to the model typed turns
// use: the same conversation, the same tools, the answer still spoken. The
// small model says a few words first, so that the person hears something
// while the larger one reads.

const thinkHarderToolName = "think_harder"

func (self *Agent) thinkHarderTool() *Tool {
	return &Tool{
		Name:   thinkHarderToolName,
		Family: FamilyGeneral,
		Risk:   tools.RiskRead,
		Description: "Hand the rest of this spoken turn to a larger model that reasons better. Use it when the question is hard: several steps of reasoning, a careful judgement or advice, a calculation, code, comparing options, a summary of something long, or anything you are not sure you would get right. " +
			"Before calling it, say one short sentence aloud, such as \"Let me think about that properly.\" Call it at the start, before other tools; the larger model then answers, with the same tools.",
		Parameters: tools.Object(map[string]any{
			"reason": tools.StringProperty("in a few words, why this needs more thought"),
		}, "reason"),
		Guidance: "think_harder: you are a small, fast model answering a call. Answer quick, simple things yourself; for anything hard, say a short line and call think_harder at once rather than answering it yourself.",
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
		return nil, fmt.Errorf("only a spoken turn can hand itself to a larger model")
	}
	if run.isThinkingHarder.Swap(true) {
		return nil, fmt.Errorf("you are the larger model already: answer the question yourself")
	}
	log.Infof("%q's spoken turn goes to the larger model: %s", run.settings.Owner.Username, strings.TrimSpace(arguments.Reason))
	return &Result{
		Content: "From here the larger model answers. You are that model now: answer the person's question carefully, with tools where they help, and keep the answer written for the ear.",
		Note:    "thinking harder",
	}, nil
}
