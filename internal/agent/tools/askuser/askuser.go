// Package askuser is the question to the person, for when the answer
// changes what happens.
package askuser

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/agent/tools"
)

func init() {
	tools.Register(func() []*tools.Tool {
		return []*tools.Tool{
			{
				Name: "ask_user", Family: tools.FamilyGeneral, Core: true, Risk: tools.RiskRead,
				Description: "Ask the person a question and wait for the answer. Only when the answer changes what you do; otherwise state your assumption and go on. Offer choices when there are a few; they can always type their own answer instead, or choose to chat about it.",
				Parameters: tools.Object(map[string]any{
					"question": tools.StringProperty("the question, in a line"),
					"choices":  tools.ArrayProperty("a few answers to pick from", tools.StringProperty("a choice")),
				}, "question"),
				Run: runAskUser,
			},
		}
	})
}

// ChatAboutIt is the answer a question card sends when the person chose
// to talk rather than pick: the dashboard's "Chat about it". The same in
// every language, so the tool knows it whatever the button said.
const ChatAboutIt = "[chat about it]"

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
	if !run.CanAsk() || run.Surface() == "mail" || run.Surface() == "schedule" || run.Surface() == "research" {
		return nil, fmt.Errorf("nobody is present to answer; state your assumption and go on, or say in the answer what you would have asked")
	}
	answer, err := run.Ask(ctx, call.ID, arguments.Question, arguments.Choices)
	if errors.Is(err, tools.ErrLeftOpen) {
		// Not a failure: they are not looking. The question waits for
		// them, and their answer comes back as a turn of its own.
		return tools.JSONResult(map[string]any{
			"answer":     "",
			"isLeftOpen": true,
			"note":       "They have not answered yet. The question stays open on their screen, and their answer will start a new turn with it. End your turn now with at most a short line, and do not ask it again.",
		})
	}
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(answer) == ChatAboutIt {
		// A choice of its own, not an answer: they want the conversation
		// back. A model told only "[chat about it]" asked the next question
		// anyway.
		return tools.JSONResult(map[string]any{
			"answer":            "",
			"isChattingInstead": true,
			"note":              "They chose to chat about it instead of answering. Ask nothing more now: end your turn with one short line inviting them to say what they want to about it, and take up what they write next.",
		})
	}
	return tools.JSONResult(map[string]any{"answer": answer})
}
