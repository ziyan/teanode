package agent

import (
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
)

// A call over MCP says how much of an answer it takes in one piece, and a tool
// that sizes its page with ResultCharactersOf sees that, not the bound a
// conversation keeps: a client cuts a longer answer itself, without saying
// where. A run that said nothing keeps the usual bound.
func TestADirectCallsBudgetReachesTheTool(t *testing.T) {
	var run tools.Run = &directRun{surface: "mcp", resultCharacters: 7744}
	if budget := tools.ResultCharactersOf(run); budget != 7744 {
		t.Fatalf("a tool called over MCP sized its answer to %d", budget)
	}
	run = &directRun{surface: "mcp"}
	if budget := tools.ResultCharactersOf(run); budget != tools.ResultCharacters {
		t.Fatalf("a call that named no budget sized its answer to %d", budget)
	}
}
