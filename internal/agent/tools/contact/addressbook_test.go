package contact_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/contact"
)

// Taking a picture off a contact reaches the person's phone and has no undo,
// so it asks first, as forgetting the contact does.
func TestRemovingAContactsPictureIsAnActionThatAsksFirst(test *testing.T) {
	tool := tools.Build().Get("contact_book")
	if tool == nil {
		test.Fatal("the agent has no contact_book tool")
	}
	properties, _ := tool.Parameters["properties"].(map[string]any)
	action, _ := properties["action"].(map[string]any)
	if values, _ := json.Marshal(action["enum"]); !strings.Contains(string(values), `"remove-picture"`) {
		test.Fatalf("remove-picture is not an action: %s", values)
	}
	arguments := json.RawMessage(`{"action":"remove-picture","id":"contact-one"}`)
	if risk := tool.RiskOf(arguments); risk != tools.RiskDestructive {
		test.Fatalf("%v", risk)
	}
	if line := tool.Preview(arguments); !strings.Contains(line, "picture") {
		test.Fatalf("%q", line)
	}
	if risk := tool.RiskOf(json.RawMessage(`{"action":"list"}`)); risk != tools.RiskRead {
		test.Fatalf("%v", risk)
	}
}
