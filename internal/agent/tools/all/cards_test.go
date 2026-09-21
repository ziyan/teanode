package all_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/all"
)

// Every tool that can stop and ask says what it is about to do, in words.
//
// The card is the one moment a person decides, and a card that prints the
// call asks them to approve an identifier: "Send the draft
// {"draft_id":"01m2ep00bed1yyxq763yn865wq"}" is not a question anybody can
// answer. This is the guard on that -- a tool added later with no sentence
// of its own fails here rather than reaching somebody as JSON.
func TestEveryToolThatAsksSaysWhatItWouldDo(t *testing.T) {
	t.Parallel()

	for _, tool := range tools.Build().All() {
		if tool.Risk == tools.RiskRead && tool.RiskOf == nil {
			continue
		}
		// An empty call: the card is drawn before anything is checked, so
		// it has to say something even for a call that would be refused.
		// Nothing a model can emit takes the run down. A card is drawn
		// before anything checks the call, so it meets arguments that are
		// empty, half-filled, the wrong type and not JSON at all -- the
		// filesystem card used to take the first letter of an action that
		// was not there and panic.
		for _, arguments := range []string{
			`{}`, `{"action":"remove"}`, `not json`, `null`, `[]`, `"a string"`,
			`{"action":5,"name":[],"item_ids":"not a list","path":null}`,
			`{"action":"","name":"","id":""}`,
		} {
			line := tool.PreviewLine(context.Background(), json.RawMessage(arguments))
			if strings.TrimSpace(line) == "" {
				t.Errorf("%s: the card says nothing for %s", tool.Name, arguments)
				continue
			}
			if strings.Contains(line, `":`) || strings.Contains(line, "{\"") {
				t.Errorf("%s: the card reads like a call rather than a sentence: %q", tool.Name, line)
			}
		}
	}
}
