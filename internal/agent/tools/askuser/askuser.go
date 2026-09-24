// Package askuser is the question to the person, for when the answer
// changes what happens.
package askuser

import (
	"context"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/agent/tools"
)

func init() {
	tools.Register(func() []*tools.Tool {
		return []*tools.Tool{
			{
				Name: "ask_user", Family: tools.FamilyGeneral, Core: true, Risk: tools.RiskRead,
				Description: "Ask the person a question and wait for the answer. Only when the answer changes what you do; otherwise state your assumption and go on. Offer choices when there are a few.",
				Parameters: tools.Object(map[string]any{
					"question": tools.StringProperty("the question, in a line"),
					"choices":  tools.ArrayProperty("a few answers to pick from", tools.StringProperty("a choice")),
				}, "question"),
				Run: runAskUser,
			},
		}
	})
}

type askUserArguments struct {
	Question string   `json:"question"`
	Choices  []string `json:"choices"`
}

func runAskUser(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[askUserArguments](call)
	if err != nil {
		return nil, err
	}
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(arguments.Question) == "" {
		return nil, fmt.Errorf("ask something")
	}
	if run.Headless() || run.Surface() == "mail" || run.Surface() == "schedule" || run.Surface() == "research" {
		return nil, fmt.Errorf("nobody is present to answer; state your assumption and go on, or say in the answer what you would have asked")
	}
	answer, err := run.Ask(ctx, call.ID, arguments.Question, arguments.Choices)
	if err != nil {
		return nil, err
	}
	return tools.JSONResult(map[string]any{"answer": answer})
}
