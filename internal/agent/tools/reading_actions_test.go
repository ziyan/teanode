package tools

import (
	"encoding/json"
	"slices"
	"testing"
)

func notebookTool() *Tool {
	return &Tool{
		Name: "notebook",
		Risk: RiskWrite,
		Parameters: Object(map[string]any{
			"action": EnumProperty("which part of the notebook", "read", "search", "write", "erase"),
			"text":   StringProperty("what to write"),
		}, "action"),
		RiskOf: func(arguments json.RawMessage) Risk {
			var call struct {
				Action string `json:"action"`
			}
			if json.Unmarshal(arguments, &call) == nil && (call.Action == "read" || call.Action == "search") {
				return RiskRead
			}
			return RiskWrite
		},
	}
}

func TestWithReadingActionsKeepsOnlyReads(t *testing.T) {
	t.Parallel()

	tool := notebookTool()
	narrowed := tool.WithReadingActions()
	if got := ActionsOf(narrowed); !slices.Equal(got, []string{"read", "search"}) {
		t.Fatalf("reading actions are %v, want read and search", got)
	}
	action := narrowed.Parameters["properties"].(map[string]any)["action"].(map[string]any)
	if action["description"] != "which part of the notebook" {
		t.Errorf("the action's description became %v", action["description"])
	}
	if _, isText := narrowed.Parameters["properties"].(map[string]any)["text"]; !isText {
		t.Error("the other parameters were dropped")
	}
	// The tool everybody else is offered is not changed.
	if got := ActionsOf(tool); len(got) != 4 {
		t.Errorf("the original tool now has %v", got)
	}
}

func TestWithReadingActionsLeavesAToolWithNoReads(t *testing.T) {
	t.Parallel()

	tool := notebookTool()
	tool.RiskOf = nil
	if narrowed := tool.WithReadingActions(); narrowed != tool {
		t.Errorf("a tool with no reading action was narrowed to %v", ActionsOf(narrowed))
	}
	reading := &Tool{Name: "lookup", Risk: RiskRead, Parameters: tool.Parameters}
	if narrowed := reading.WithReadingActions(); narrowed != reading {
		t.Error("a tool whose every action reads was copied")
	}
}
