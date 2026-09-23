package apigraph

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

// A tool called over MCP reads back as who called what, and what came back.
func TestADirectCallIsFiledAsWhoCalledWhat(test *testing.T) {
	title, messages := mcpTranscript("An assistant", "shell", json.RawMessage(`{"command": "ls -la"}`), "total 0", nil)

	if title != `An assistant: shell {"command":"ls -la"}` {
		test.Errorf("the title is %q", title)
	}
	if len(messages) != 3 {
		test.Fatalf("%d messages, not the request, the call and the answer", len(messages))
	}
	if !strings.Contains(messages[0].Content, "An assistant called shell over MCP") {
		test.Errorf("the request reads %q", messages[0].Content)
	}
	if call := messages[1].ToolCalls; len(call) != 1 || call[0].Name != "shell" || call[0].Arguments != `{"command":"ls -la"}` {
		test.Errorf("the call reads %+v", call)
	}
	if messages[2].Role != "tool" || messages[2].Content != "total 0" {
		test.Errorf("the answer reads %+v", messages[2])
	}
}

// A failed call says so in its title, where a list is scanned.
func TestAFailedCallSaysSoInItsTitle(test *testing.T) {
	title, messages := mcpTranscript("An assistant", "todo", json.RawMessage(`{"action":"list"}`), "", errors.New("no list here"))
	if !strings.HasSuffix(title, "(failed)") {
		test.Errorf("the title %q does not say it failed", title)
	}
	if !strings.Contains(messages[2].Content, "no list here") {
		test.Errorf("the answer does not carry the failure: %q", messages[2].Content)
	}
}

// What is kept is capped, and a title fits its column, without splitting a
// character.
func TestALargeCallIsCutToWhatIsWorthKeeping(test *testing.T) {
	huge := strings.Repeat("字", mcpRecordedCharacters+50)
	title, messages := mcpTranscript(strings.Repeat("名", 300), "filesystem", json.RawMessage(`{"action":"read"}`), huge, nil)
	if count := len([]rune(title)); count > mcpTitleCharacters {
		test.Errorf("the title is %d characters", count)
	}
	if !strings.Contains(messages[2].Content, "50 more characters not kept here") {
		test.Errorf("a cut answer does not say it was cut")
	}
	if !utf8.ValidString(title) {
		test.Error("the title was cut through a character")
	}
	for _, message := range messages {
		if !utf8.ValidString(message.Content) {
			test.Error("a kept message was cut through a character")
		}
	}
}

// A call that can carry a secret keeps neither its arguments nor its answer.
//
// A token made through a tool is shown once and kept nowhere, and a password
// goes in as an argument; a record of the call is read long after, by
// whoever opens the activity list.
func TestASecretIsNotKeptInTheRecord(test *testing.T) {
	arguments, answer := keptOf(json.RawMessage(`{"password":"a-password-for-a-test"}`), `{"secret":"a-token-for-a-test"}`, nil, true)
	if strings.Contains(string(arguments), "a-password") || strings.Contains(answer, "a-token") {
		test.Errorf("kept %s and %q", arguments, answer)
	}
	if answer != mcpSecretNotKept {
		test.Errorf("the answer reads %q", answer)
	}
	ordinary, kept := keptOf(json.RawMessage(`{"command":"ls"}`), "total 0", nil, false)
	if string(ordinary) != `{"command":"ls"}` || kept != "total 0" {
		test.Errorf("an ordinary call was changed: %s, %q", ordinary, kept)
	}
}
