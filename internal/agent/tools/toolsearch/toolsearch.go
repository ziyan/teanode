// Package toolsearch loads tools that are listed but not sent, when the
// catalog is long: the model names what it wants to do and the matching
// tools join the request from the next round.
package toolsearch

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
				Name:   "tool_search",
				Family: tools.FamilyGeneral,
				Core:   true,
				Risk:   tools.RiskRead,
				Description: "Load tools that are listed but not loaded. Name the tools you want from the list (\"select:calendar_agenda,contact_book\", or just their names), or give a few words about what you want to do (\"move mail\", \"domain dns\"); the closest matches become available for the rest of the turn. " +
					"Never search for a tool you have already: call it. This finds tools, not facts: to look something up, use web_search.",
				Parameters: tools.Object(map[string]any{
					"query": tools.StringProperty("tool names from the list, or a few words: what you want to do, not what you want to know"),
					"limit": tools.IntegerProperty("the most to load, 5 by default and 10 at most"),
				}, "query"),
				Run: runToolSearch,
			},
		}
	})
}

type toolSearchArguments struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

func runToolSearch(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[toolSearchArguments](call)
	if err != nil {
		return nil, err
	}
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	sent, deferred := tools.Split(run.Offered(), run.Loaded(), false)
	if len(deferred) == 0 {
		sent, deferred = tools.Split(run.Offered(), run.Loaded(), true)
	}
	found := tools.SearchTools(sent, deferred, arguments.Query, arguments.Limit)
	// The tools the model has already that the query names: a model that
	// searched for a tool it holds searched again and again, each search a
	// round.
	alreadyAvailableNames := make([]string, 0, len(found.Held))
	for _, tool := range found.Held {
		alreadyAvailableNames = append(alreadyAvailableNames, tool.Name)
	}
	// What a loaded tool is for, in a line: its whole definition is in the
	// next request, and repeating it here kept a second copy of every
	// schema in the history for the rest of the conversation.
	loaded := make([]map[string]any, 0, len(found.Loadable))
	for _, tool := range found.Loadable {
		run.Load(tool.Name)
		loaded = append(loaded, map[string]any{"name": tool.Name, "family": tool.Family, "risk": tool.Risk, "description": firstSentence(tool.Description)})
	}
	if len(loaded) == 0 {
		if len(alreadyAvailableNames) > 0 {
			return tools.TextResult("nothing more to load for %q: you have %s already, so call it now rather than searching again", arguments.Query, strings.Join(alreadyAvailableNames, ", ")), nil
		}
		return tools.TextResult("no tool matches %q; the tools already loaded are all there is for that. tool_search finds tools by what they do, not information: to look something up, use web_search", arguments.Query), nil
	}
	answer := map[string]any{"loaded": loaded, "note": "these tools are available from the next call on, with their full definitions"}
	if len(alreadyAvailableNames) > 0 {
		answer["alreadyAvailable"] = alreadyAvailableNames
		answer["note"] = "these tools are available from the next call on, with their full definitions; the ones in alreadyAvailable you have already and can call now"
	}
	result, err := tools.JSONResult(answer)
	if err != nil {
		return nil, err
	}
	result.Note = fmt.Sprintf("loaded %d tool(s)", len(loaded))
	return result, nil
}

// firstSentence is what a tool is for: the first sentence of its
// description.
func firstSentence(text string) string {
	text = strings.TrimSpace(text)
	if index := strings.Index(text, ". "); index > 0 {
		return text[:index+1]
	}
	return text
}
