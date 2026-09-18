package llm

import (
	"encoding/json"
	"testing"
)

// A call the model wrote out in its template's XML is a call; two of them
// are two; a JSON one is read too; and words with no call stay words.
func TestTextualToolCallsAreRead(t *testing.T) {
	content := "Let me check.\n<tool_call>\n<function=memory>\n<parameter=action>\nget\n</parameter>\n<parameter=path>\nprojects/mujin\n</parameter>\n</function>\n</tool_call>\n<tool_call>\n<function=memory>\n<parameter=action>\nsearch\n</parameter>\n<parameter=query>\nZhongshan container customer site\n</parameter>\n<parameter=limit>\n5\n</parameter>\n</function>\n</tool_call>"
	calls, rest := TextualToolCalls(content)
	if len(calls) != 2 || rest != "Let me check." {
		t.Fatalf("calls %+v rest %q", calls, rest)
	}
	var second map[string]any
	if err := json.Unmarshal([]byte(calls[1].Arguments), &second); err != nil {
		t.Fatal(err)
	}
	if second["action"] != "search" || second["query"] != "Zhongshan container customer site" || second["limit"] != float64(5) {
		t.Fatalf("second call's arguments %v", second)
	}
	if calls[0].ID == calls[1].ID || calls[0].Name != "memory" {
		t.Fatalf("calls %+v", calls)
	}

	calls, rest = TextualToolCalls(`<tool_call>{"name": "memory", "arguments": {"action": "get", "path": "self"}}</tool_call>`)
	if len(calls) != 1 || calls[0].Arguments != `{"action": "get", "path": "self"}` || rest != "" {
		t.Fatalf("json call %+v rest %q", calls, rest)
	}

	calls, rest = TextualToolCalls("The portal is the customer site.")
	if calls != nil || rest != "The portal is the customer site." {
		t.Fatalf("words %+v %q", calls, rest)
	}
}
