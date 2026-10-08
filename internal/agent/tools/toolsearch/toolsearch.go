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

// alreadyAvailableLimit is how many of the tools the model already has
// a search names, besides what it loads.
const alreadyAvailableLimit = 3

// isNamedBy says whether a query names a tool the model holds: a word of
// it, of three letters or more, is the tool's name or a word of the name.
// Search matches a word anywhere in a description, so "a" or "the" would
// match every tool held. tool_search itself is never named: told to call
// it, the model would only search again.
func isNamedBy(tool *tools.Tool, query string) bool {
	if tool.Name == "tool_search" {
		return false
	}
	name := strings.ToLower(tool.Name)
	nameWords := append(strings.FieldsFunc(name, func(character rune) bool {
		return character == '_' || character == '-' || character == '.'
	}), name)
	for _, word := range strings.Fields(strings.ToLower(query)) {
		word = strings.Trim(word, ".,;:!?\"'()")
		if len(word) < 3 {
			continue
		}
		for _, nameWord := range nameWords {
			if word == nameWord {
				return true
			}
		}
	}
	return false
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
	// The tools the model has already that match: a model that searched
	// for a tool it holds searched again and again, each search a round.
	alreadyAvailableNames := []string{}
	for _, tool := range tools.Search(sent, arguments.Query, len(sent)) {
		if len(alreadyAvailableNames) < alreadyAvailableLimit && isNamedBy(tool, arguments.Query) {
			alreadyAvailableNames = append(alreadyAvailableNames, tool.Name)
		}
	}
	found := tools.Search(deferred, arguments.Query, arguments.Limit)
	loaded := make([]map[string]any, 0, len(found))
	for _, tool := range found {
		run.Load(tool.Name)
		loaded = append(loaded, map[string]any{"name": tool.Name, "family": tool.Family, "risk": tool.Risk, "description": tool.Description, "parameters": tool.Parameters})
	}
	if len(loaded) == 0 {
		if len(alreadyAvailableNames) > 0 {
			return tools.TextResult("nothing more to load for %q: you have %s already, so call it now rather than searching again", arguments.Query, strings.Join(alreadyAvailableNames, ", ")), nil
		}
		return tools.TextResult("no tool matches %q; the tools already loaded are all there is for that. tool_search finds tools by what they do, not information: to look something up, use web_search", arguments.Query), nil
	}
	answer := map[string]any{"loaded": loaded, "note": "these tools are available from the next call on"}
	if len(alreadyAvailableNames) > 0 {
		answer["alreadyAvailable"] = alreadyAvailableNames
		answer["note"] = "these tools are available from the next call on; the ones in alreadyAvailable you have already and can call now"
	}
	result, err := tools.JSONResult(answer)
	if err != nil {
		return nil, err
	}
	result.Note = fmt.Sprintf("loaded %d tool(s)", len(loaded))
	return result, nil
}
