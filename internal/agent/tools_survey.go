package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/agent/tools"
)

// surveyTool is the survey as the agent calls it. Built here rather than
// registered in a package of its own, as the subagent tool is, because
// carrying it out starts runs, which only the agent can do. A package of
// its own could reach the survey only through the API, and every
// operation a tool runs there holds one database transaction open for
// as long as it takes: a quarter of an hour, for a survey.
func (self *Agent) surveyTool() *Tool {
	return &Tool{
		Name:   "survey",
		Family: FamilyGeneral,
		// Read: the runs it makes may only look, and what it costs is
		// runs of kind survey, counted like any other.
		Risk: tools.RiskRead,
		Description: "Answer a broad question about a whole area of what you know -- a theme, a project and everything under it, or everything -- by asking each page's overview in that area for its part of the answer, several at once, and combining the parts into one report with citations. " +
			"For questions about the whole: \"what are the strengths and weaknesses of X\", \"how does X fit together\", \"what patterns run through Y\", \"what should change\". Not for a single fact, which `memory` finds in one call. " +
			"It takes minutes and costs about one call per page in scope plus one; scope it to a theme or a page when the question is about one area. The report ends with the pages it covered and any it could not.",
		Parameters: tools.Object(map[string]any{
			"question": tools.StringProperty("the question, in full, as the person would want it answered"),
			"scope":    tools.StringProperty("a page path to survey under: a theme (themes/...), a project, any page; empty for everything"),
		}, "question"),
		Guidance: "survey: for a broad question about a whole area -- strengths and weaknesses, how the parts fit together, what patterns run through it -- survey that area rather than piecing an answer together from a few recalled facts; it returns a report worth keeping, so offer to keep it as an artifact.",
		Run:      self.runSurvey,
	}
}

func (self *Agent) runSurvey(ctx context.Context, call *Call) (*Result, error) {
	arguments, err := tools.DecodeArguments[struct {
		Question string `json:"question"`
		Scope    string `json:"scope"`
	}](call)
	if err != nil {
		return nil, err
	}
	question := strings.TrimSpace(arguments.Question)
	if question == "" {
		return nil, fmt.Errorf("say what the survey is to answer")
	}
	parent, ok := tools.MustRun(ctx).(*AskRun)
	if !ok {
		return nil, fmt.Errorf("a survey can only be started from a turn")
	}
	surveyed, err := self.Survey(ctx, parent.settings.Agent, parent.settings.Owner, question, arguments.Scope)
	if err != nil {
		return nil, err
	}
	result, err := tools.JSONResult(map[string]any{
		"report":  surveyed.Report,
		"covered": surveyed.CoveredPaths,
		"failed":  surveyed.FailedPaths,
		// Where the working is, a run per page and the one that
		// combined them, for a person who wants to see what was read.
		"runs": surveyed.RunIDs,
		"note": "Give them the report, keeping its citations. It is long and worth keeping: offer to keep it with the artifact tool as a Markdown document.",
	})
	if err != nil {
		return nil, err
	}
	result.Note = fmt.Sprintf("surveyed %d pages", len(surveyed.CoveredPaths))
	return result, nil
}
