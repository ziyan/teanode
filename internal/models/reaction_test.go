package models_test

import (
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// A reaction is one or two emoji, short enough to draw on a bubble, and
// nothing else: not a word, not a third emoji, not nothing.
func TestValidateReactionEmoji(t *testing.T) {
	for _, accepted := range []string{"👍", "❤️", "👍🏽", "🇯🇵", "👨\u200d👩\u200d👧", "❤️🙏", "1️⃣"} {
		if err := models.ValidateReactionEmoji(accepted); err != nil {
			t.Errorf("%q was refused: %s", accepted, err)
		}
	}
	for _, refused := range []string{"", "ok", "👍 ", "👍👍👍", "a👍", "©", strings.Repeat("👨\u200d👩\u200d👧", 2)} {
		if err := models.ValidateReactionEmoji(refused); err == nil {
			t.Errorf("%q was accepted", refused)
		}
	}
}

// Only what somebody said carries a reaction.
func TestOnlySaidMessagesCanBeReactedTo(t *testing.T) {
	for role, expected := range map[string]bool{"user": true, "assistant": true, "tool": false, "note": false, "system": false, "compaction": false} {
		if (&models.AgentMessage{Role: role}).CanBeReactedTo() != expected {
			t.Errorf("a %s message: want %v", role, expected)
		}
	}
}
