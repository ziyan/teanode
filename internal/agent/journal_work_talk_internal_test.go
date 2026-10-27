package agent

import (
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/journal"
)

// A project's day is told from its transcripts: the person's requests that
// say something, once each, and the assistant's last post of each stretch.
// The session's name says nothing of it.
func TestAProjectsDayIsWhatWasSaidInItsSessions(t *testing.T) {
	project := journal.WorkProject{Name: "harbor", DocumentID: "stretch1", StretchDocumentIDs: []string{"stretch1", "stretch2"}}
	chunkTexts := map[string][]string{
		"stretch1": {
			"09:00 ada: make the tide chart read the new feed\n09:05 Claude Code: reading the feed parser\n",
			"09:40 Claude Code: The chart reads the new feed; the old one is gone.\nTests pass: all 42.\n09:41 ada: ok",
		},
		"stretch2": {"14:00 ada: make the tide chart read the new feed\n14:02 ada: and add the moon phase to the legend\n14:30 Claude Code: The legend shows the moon phase."},
	}
	talk := journalProjectTalk(project, "agent_document:stretch1", chunkTexts, []string{"ada"})
	want := strings.Join([]string{
		"Project harbor, agent_document:stretch1:",
		"Their requests:",
		"- 09:00 make the tide chart read the new feed",
		"- 14:02 and add the moon phase to the legend",
		"The assistant's reports at the end of a stretch of work:",
		"- 09:40 The chart reads the new feed; the old one is gone. Tests pass: all 42.",
		"- 14:30 The legend shows the moon phase.",
	}, "\n")
	if talk != want {
		t.Errorf("the project's talk:\n%s\nwant:\n%s", talk, want)
	}
	if empty := journalProjectTalk(journal.WorkProject{Name: "quiet", StretchDocumentIDs: []string{"none"}}, "agent_document:none", chunkTexts, []string{"ada"}); empty != "" {
		t.Errorf("a project with no transcript: %q", empty)
	}
}

// Many lines are thinned evenly, keeping the first and the last.
func TestSpreadJournalLinesKeepsTheEnds(t *testing.T) {
	lines := []string{"a", "b", "c", "d", "e", "f", "g"}
	if got := strings.Join(spreadJournalLines(lines, 3), ""); got != "adg" {
		t.Errorf("spread: %s", got)
	}
	if got := strings.Join(spreadJournalLines(lines[:2], 3), ""); got != "ab" {
		t.Errorf("few lines: %s", got)
	}
}
