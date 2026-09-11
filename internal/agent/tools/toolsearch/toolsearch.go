// Package toolsearch loads tools that are listed but not sent, when the
// catalog is long: the model names what it wants to do and the matching
// tools join the request from the next round.
package toolsearch

import (
	"context"
	"fmt"

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
				Description: "Load tools that are listed but not loaded. Give a few words about what you want to do (\"move mail\", \"domain dns\"); the matching tools become available for the rest of the turn. " +
					"This finds tools, not facts: to look something up, use web_search.",
				Parameters: tools.Object(map[string]any{
					"query": tools.StringProperty("a few words: what you want to do, not what you want to know"),
					"limit": tools.IntegerProperty("how many to load, 10 by default"),
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
	_, deferred := tools.Split(run.Offered(), run.Loaded(), false)
	if len(deferred) == 0 {
		_, deferred = tools.Split(run.Offered(), run.Loaded(), true)
	}
	found := tools.Search(deferred, arguments.Query, arguments.Limit)
	loaded := make([]map[string]any, 0, len(found))
	for _, tool := range found {
		run.Load(tool.Name)
		loaded = append(loaded, map[string]any{"name": tool.Name, "family": tool.Family, "risk": tool.Risk, "description": tool.Description, "parameters": tool.Parameters})
	}
	if len(loaded) == 0 {
		return tools.TextResult("no tool matches %q; the tools already loaded are all there is for that. tool_search finds tools by what they do, not information: to look something up, use web_search", arguments.Query), nil
	}
	result, err := tools.JSONResult(map[string]any{"loaded": loaded, "note": "these tools are available from the next call on"})
	if err != nil {
		return nil, err
	}
	result.Note = fmt.Sprintf("loaded %d tool(s)", len(loaded))
	return result, nil
}
