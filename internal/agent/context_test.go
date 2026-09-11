package agent

import (
	"strings"
	"testing"
)

func TestStripQuotedCutsHistoryAndQuotedLines(t *testing.T) {
	text := "Thanks, Thursday works.\n\n> earlier line\nOne more thing: bring the key.\n\nOn Tue, 9 Sep 2026 at 10:00, Maria <maria@example.net> wrote:\n> Can you do Thursday?\n> Maria"
	got := stripQuoted(text)
	if got != "Thanks, Thursday works.\n\nOne more thing: bring the key." {
		t.Fatalf("got %q", got)
	}
	original := "Sure.\n\n-----Original Message-----\nFrom: x\nlots of quoted text"
	if got := stripQuoted(original); got != "Sure." {
		t.Fatalf("got %q", got)
	}
}

func TestHTMLToTextKeepsWordsDropsScriptsAndBreaksBlocks(t *testing.T) {
	html := `<html><head><style>p{color:red}</style></head><body><p>Hello <b>there</b></p><script>alert(1)</script><div>Second<br>line</div><ul><li>one</li><li>two</li></ul></body></html>`
	got := normalizeText(HTMLToText(html))
	for _, want := range []string{"Hello there", "Second\nline", "one", "two"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	for _, unwanted := range []string{"alert", "color:red"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("%q leaked into %q", unwanted, got)
		}
	}
}

func TestRenderListsWhatTheModelIsGiven(t *testing.T) {
	message := &MessageContext{From: "Maria <maria@example.net>", To: "alice@example.com", Date: "2026-09-10 09:00 UTC", Subject: "Thursday", Attachments: []string{"plan.pdf (application/pdf)"}, Facts: []string{"SPF: pass"}, Text: "Can you do Thursday?"}
	got := message.Render()
	for _, want := range []string{"From: Maria", "Subject: Thursday", "Attachments: plan.pdf (application/pdf)", "Server facts: SPF: pass", "Can you do Thursday?"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
}
