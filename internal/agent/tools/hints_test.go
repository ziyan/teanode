package tools

import (
	"encoding/json"
	"testing"

	"github.com/ziyan/teanode/internal/mcp"
)

// Every tool says what it does in the protocol's terms: read off its risk
// where it gives none, its own where it does. A tool whose calls differ
// keeps the protocol's cautious defaults, and anything reaching another
// service is open-world.
func TestEveryToolHasHints(t *testing.T) {
	isFalse := false
	for _, testCase := range []struct {
		name            string
		tool            *Tool
		readOnly        bool
		destructive     *bool
		openWorld       *bool
		isOpenWorldTrue bool
	}{
		{"read", &Tool{Risk: RiskRead, Family: FamilyMailbox}, true, nil, &isFalse, false},
		{"write", &Tool{Risk: RiskWrite, Family: FamilyMailbox}, false, &isFalse, &isFalse, false},
		{"outward", &Tool{Risk: RiskOutward, Family: FamilyMailbox}, false, &isFalse, nil, true},
		{"a read from a skill", &Tool{Risk: RiskRead, Family: FamilySkills}, true, nil, nil, true},
		{"calls that differ", &Tool{Risk: RiskRead, Family: FamilyMailbox, RiskOf: func(json.RawMessage) Risk { return RiskDestructive }}, false, nil, nil, false},
	} {
		hints := testCase.tool.Hints()
		if hints.ReadOnlyHint != testCase.readOnly {
			t.Errorf("%s: read-only %v", testCase.name, hints.ReadOnlyHint)
		}
		if (hints.DestructiveHint == nil) != (testCase.destructive == nil) || (hints.DestructiveHint != nil && *hints.DestructiveHint != *testCase.destructive) {
			t.Errorf("%s: destructive %v", testCase.name, hints.DestructiveHint)
		}
		if testCase.isOpenWorldTrue {
			if hints.OpenWorldHint == nil || !*hints.OpenWorldHint {
				t.Errorf("%s: should be open-world", testCase.name)
			}
		} else if (hints.OpenWorldHint == nil) != (testCase.openWorld == nil) || (hints.OpenWorldHint != nil && *hints.OpenWorldHint != *testCase.openWorld) {
			t.Errorf("%s: open-world %v", testCase.name, hints.OpenWorldHint)
		}
	}
	destructive := (&Tool{Risk: RiskDestructive}).Hints()
	if destructive.DestructiveHint == nil || !*destructive.DestructiveHint {
		t.Error("a destructive tool says so")
	}
	given := &mcp.ToolAnnotations{Title: "Given", ReadOnlyHint: true}
	if (&Tool{Risk: RiskOutward, Annotations: given}).Hints() != given {
		t.Error("a tool's own annotations are used as they are")
	}
	// Every registered tool has some.
	for _, tool := range Build().All() {
		if tool.Hints() == nil {
			t.Errorf("%s has no hints", tool.Name)
		}
	}
}
