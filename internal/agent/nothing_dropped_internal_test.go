package agent

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// A message longer than a chunk is read in parts, in order, and every
// character of it is in some part: the note is written from all of it.
func TestCompactionReadsALongMessageInParts(t *testing.T) {
	var lines []string
	for index := range 4000 {
		lines = append(lines, fmt.Sprintf("line %d of a long tool answer", index))
	}
	long := strings.Join(lines, "\n")
	history := []llm.ChatMessage{
		{Role: llm.RoleUser, Content: "short question"},
		{Role: llm.RoleTool, Name: "reader", Content: long},
	}
	chunks := chunkHistory(history, 3000)
	if len(chunks) < 3 {
		t.Fatalf("a long message was kept in %d chunks", len(chunks))
	}
	var rendered strings.Builder
	for _, chunk := range chunks {
		text := renderMessages(chunk)
		if llm.EstimateTokens(text) > 3300 {
			t.Errorf("a chunk weighs %d tokens", llm.EstimateTokens(text))
		}
		rendered.WriteString(text)
	}
	for _, index := range []int{0, 1999, 3999} {
		if !strings.Contains(rendered.String(), fmt.Sprintf("line %d of a long tool answer", index)) {
			t.Errorf("line %d is in no chunk", index)
		}
	}
	if !strings.Contains(rendered.String(), "[part 2 of ") {
		t.Errorf("the parts are not marked")
	}
}

// A message that fits a chunk is not split.
func TestCompactionKeepsAShortMessageWhole(t *testing.T) {
	history := []llm.ChatMessage{{Role: llm.RoleUser, Content: "one"}, {Role: llm.RoleAssistant, Content: "two"}}
	chunks := chunkHistory(history, 3000)
	if len(chunks) != 1 || len(chunks[0]) != 2 || chunks[0][0].Content != "one" {
		t.Fatalf("chunks: %+v", chunks)
	}
}

// What an agent says in a chat is posted whole: split at a blank line, a line
// end or a space into messages of at most the limit, every word kept in order.
func TestALongChatAnswerIsSplitNotCut(t *testing.T) {
	var paragraphs []string
	for index := range 40 {
		paragraphs = append(paragraphs, fmt.Sprintf("Paragraph %d. %s", index, strings.Repeat("word ", 40)))
	}
	words := strings.Join(paragraphs, "\n\n")
	parts := splitChatBody(words, 1000)
	if len(parts) < 8 {
		t.Fatalf("parts: %d", len(parts))
	}
	for _, part := range parts {
		if len([]rune(part)) > 1000 || strings.TrimSpace(part) != part || part == "" {
			t.Fatalf("a part of %d characters: %q", len([]rune(part)), part)
		}
	}
	if joined := strings.Join(parts, "\n\n"); strings.Join(strings.Fields(joined), " ") != strings.Join(strings.Fields(words), " ") {
		t.Fatal("the parts are not the words")
	}
	if got := splitChatBody("short", 1000); len(got) != 1 || got[0] != "short" {
		t.Fatalf("a short answer: %q", got)
	}
	unbroken := strings.Repeat("é", 2500)
	if got := splitChatBody(unbroken, 1000); len(got) != 3 || strings.Join(got, "") != unbroken {
		t.Fatalf("a word with no break is cut at the limit: %d parts", len(got))
	}
}

// A long transcript is summarized a part at a time, cut at line ends, and the
// parts are the whole transcript.
func TestACallTranscriptIsSplitAtLineEnds(t *testing.T) {
	var lines []string
	for index := range 300 {
		lines = append(lines, fmt.Sprintf("speaker%d: %s", index%3, strings.Repeat("said ", 20)))
	}
	transcript := strings.Join(lines, "\n") + "\n"
	parts := splitLinesInto(transcript, 5000)
	if len(parts) < 5 || strings.Join(parts, "") != transcript {
		t.Fatalf("%d parts, joined equal: %v", len(parts), strings.Join(parts, "") == transcript)
	}
	for _, part := range parts {
		if len(part) > 5000 || !strings.HasSuffix(part, "\n") {
			t.Fatalf("a part of %d characters ends %q", len(part), part[len(part)-5:])
		}
	}
	if got := splitLinesInto("one\ntwo\n", 5000); len(got) != 1 {
		t.Fatalf("a short transcript is one part: %v", got)
	}
}

// A chat of the owner's is compacted while it is quiet, like any
// conversation; a member's turn is not.
func TestAChatIsDueForQuietCompaction(t *testing.T) {
	conversation := &models.AgentConversation{ID: "conversation1"}
	owner := &AskRun{settings: &AskSettings{Conversation: conversation, Chat: &ChatTurn{}}, lastHistoryTokens: askSoftHistoryTokens + 1}
	if !owner.isIdleCompactionDue() {
		t.Error("an owner's chat turn with a long history is not compacted")
	}
	member := &AskRun{settings: &AskSettings{Conversation: conversation, Chat: &ChatTurn{IsMemberTurn: true}}, lastHistoryTokens: askSoftHistoryTokens + 1}
	if member.isIdleCompactionDue() {
		t.Error("a member's turn is compacted")
	}
}
