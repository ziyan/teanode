package tools

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// No tool reads a field of the run's conversation directly.
//
// A run from a harness over the protocol, like the night, is part of no
// conversation, so Conversation() is nil there. Reading .ID off it took the
// request down: the harness saw its connection dropped, with no answer and no
// error. ConversationIDOf is the nil-safe way, and this keeps the unsafe form
// from coming back in a tool nobody thinks to call from outside a turn.
func TestNoToolReadsAFieldOfAMissingConversation(test *testing.T) {
	unsafe := regexp.MustCompile(`Conversation\(\)\.[A-Za-z]`)
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for number, line := range strings.Split(string(content), "\n") {
			code, _, _ := strings.Cut(line, "//")
			if unsafe.MatchString(code) {
				test.Errorf("%s:%d reads a field of Conversation(), which is nil outside a conversation; use ConversationIDOf", path, number+1)
			}
		}
		return nil
	})
	if err != nil {
		test.Fatalf("walking the tools: %s", err)
	}
}
