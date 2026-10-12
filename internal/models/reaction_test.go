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

// Each side reacts to what the other said: the person to the agent's
// messages, the agent to the person's, and nobody to a tool's result.
func TestEachSideReactsToWhatTheOtherSaid(t *testing.T) {
	for role, expected := range map[string][2]bool{
		"user": {false, true}, "assistant": {true, false}, "tool": {false, false}, "note": {false, false}, "system": {false, false}, "compaction": {false, false},
	} {
		message := &models.AgentMessage{Role: role}
		if message.CanBeReactedToBy(models.ReactorPerson) != expected[0] {
			t.Errorf("the person on a %s message: want %v", role, expected[0])
		}
		if message.CanBeReactedToBy(models.ReactorAgent) != expected[1] {
			t.Errorf("the agent on a %s message: want %v", role, expected[1])
		}
	}
}
