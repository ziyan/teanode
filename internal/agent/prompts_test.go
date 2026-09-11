package agent

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/models"
)

// A prompt is what the agent is; a change to it should show up as a diff in
// review, not as a surprise in production. Each prompt is rendered from
// fixed input and compared with a golden file under testdata/prompts.
// Run with -update to rewrite the golden files after a deliberate change.

var updateGolden = flag.Bool("update", false, "rewrite the golden prompt files")

func goldenInput() *TriageInput {
	configuration := config.Default()
	configuration.Server.Name = "mail.example.com"
	configuration.Agent.Instructions = "Never answer a parent automatically."
	agent := &models.Agent{
		Name:         "Bertie",
		Enabled:      true,
		Instructions: "I am the treasurer of the sailing club. Anything from the club is important.",
		Voice:        &models.AgentVoice{Tone: "casual", Signoff: "— Z"},
		Categories:   []models.AgentCategory{{Name: "club", Description: "anything from the sailing club"}},
	}
	owner := &models.User{Username: "alice", Name: "Alice Example", Locale: "en"}
	mailbox := &models.Mailbox{Name: "Personal"}
	source := &models.AgentMailbox{Granted: true, Triage: &models.AgentTriage{Enabled: true, ReplyExpectation: "direct"}}
	message := &MessageContext{
		From:        "Maria <maria@example.net>",
		To:          "alice@example.com",
		Date:        "2026-09-10 09:00 UTC",
		Subject:     "Thursday?",
		Facts:       []string{"SPF: pass", "DKIM: pass"},
		Text:        "Can you do Thursday at 3? I need the key back too.",
		Attachments: []string{"plan.pdf (application/pdf)"},
	}
	return &TriageInput{
		Configuration: configuration,
		Agent:         agent,
		Owner:         owner,
		Mailbox:       mailbox,
		Source:        source,
		Message:       message,
		Memories:      []string{"Mail from the landlord's agency is urgent even when it looks routine."},
		Corrections:   []string{"A message from newsletter@example.org was sorted as work; Alice filed it as newsletter."},
		ResearchNotes: []string{"Look up parcel numbers with the tracker."},
	}
}

func compareGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "prompts", name)
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no golden file %s; run with -update to write it: %s", path, err)
	}
	if string(want) != got {
		t.Fatalf("%s differs from the golden file; run with -update if the change is deliberate.\n--- got ---\n%s", name, got)
	}
}

func TestTriagePromptGolden(t *testing.T) {
	messages, err := TriagePrompt(goldenInput())
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || messages[0].Role != "system" || messages[1].Role != "user" {
		t.Fatalf("expected a system and a user message, got %d", len(messages))
	}
	compareGolden(t, "triage.system.txt", messages[0].Content)
	compareGolden(t, "triage.user.txt", messages[1].Content)

	// The parts that must be there whatever the wording.
	system := messages[0].Content
	for _, want := range []string{"Bertie", "Alice Example", "mail.example.com", "<house-instructions>", "Never answer a parent", "<instructions>", "treasurer", "Sign off with: — Z"} {
		if !strings.Contains(system, want) {
			t.Fatalf("the conduct lacks %q", want)
		}
	}
	user := messages[1].Content
	for _, want := range []string{"- club: anything from the sailing club", "- newsletter:", "landlord's agency", "filed it as newsletter", "Look up parcel numbers", "<message>", "Can you do Thursday", "not only in copy"} {
		if !strings.Contains(user, want) {
			t.Fatalf("the triage prompt lacks %q", want)
		}
	}
}

func TestInterpretTriageRefusesWhatIsNotInTheVocabulary(t *testing.T) {
	agent := &models.Agent{Categories: []models.AgentCategory{{Name: "Club"}}}
	insight, err := InterpretTriage(&TriageAnswer{Category: "CLUB", Priority: "urgent", Summary: strings.Repeat("x", 400), ActionItems: []string{" bring the key ", ""}}, agent)
	if err != nil {
		t.Fatal(err)
	}
	if insight.Category != "club" || insight.Priority != "normal" || len(insight.Summary) != 300 || len(insight.ActionItems) != 1 {
		t.Fatalf("insight %+v", insight)
	}
	// A notification never needs a reply, whatever the model said: a
	// plan-expiry notice on a real mailbox was marked as waiting for one.
	if insight, _ := InterpretTriage(&TriageAnswer{Category: "notification", Priority: "high", NeedsReply: true}, agent); insight.NeedsReply {
		t.Fatal("a notification was left needing a reply")
	}
	if insight, _ := InterpretTriage(&TriageAnswer{Category: "personal", Priority: "normal", NeedsReply: true}, agent); !insight.NeedsReply {
		t.Fatal("a personal message that needs a reply lost it")
	}
	insight, _ = InterpretTriage(&TriageAnswer{Category: "spam", Priority: "HIGH"}, agent)
	if insight.Category != "other" || insight.Priority != "high" {
		t.Fatalf("insight %+v", insight)
	}
}

func TestSummarizePromptGolden(t *testing.T) {
	input := goldenInput()
	messages, err := SummarizePrompt(&SummarizeInput{
		Configuration: input.Configuration,
		Agent:         input.Agent,
		Owner:         input.Owner,
		Mailbox:       input.Mailbox,
		Source:        &models.AgentMailbox{Granted: true, Summaries: &models.AgentSummaries{Enabled: true, Style: "detailed"}},
		Subject:       "Thursday?",
		Previous:      "Maria asked whether Thursday works; Alice has not answered.",
		Messages:      []*MessageContext{input.Message},
		Memories:      []string{"Keep summaries of club mail to one line."},
	})
	if err != nil {
		t.Fatal(err)
	}
	compareGolden(t, "summarize.user.txt", messages[1].Content)
	user := messages[1].Content
	for _, want := range []string{"Thursday?", "<previous-summary>", "has not answered", "The messages since then", "Keep summaries of club mail", "<message>", "Can you do Thursday", "one line per open question"} {
		if !strings.Contains(user, want) {
			t.Fatalf("the summarize prompt lacks %q", want)
		}
	}
	if !strings.Contains(messages[0].Content, "Bertie") {
		t.Fatal("the conduct is missing")
	}
}

func TestThreadSubjectDropsTheAnswerPrefixes(t *testing.T) {
	for input, want := range map[string]string{"Re: Re: Roof": "Roof", "Fwd: RE: Roof": "Roof", "Roof": "Roof", "  AW: Roof ": "Roof"} {
		if got := threadSubject(input); got != want {
			t.Fatalf("threadSubject(%q) = %q", input, got)
		}
	}
}

func TestDraftPromptGolden(t *testing.T) {
	input := goldenInput()
	messages, err := DraftPrompt(&DraftInput{
		Configuration: input.Configuration,
		Agent:         input.Agent,
		Owner:         input.Owner,
		Mailbox:       input.Mailbox,
		Source:        &models.AgentMailbox{Granted: true, DraftReplies: true},
		Subject:       "Thursday?",
		Instructions:  "Say yes to Thursday, ask her to bring the plan.",
		Message:       input.Message,
		Earlier:       []*MessageContext{{From: "Alice", Date: "2026-09-09 18:00 UTC", Subject: "Thursday?", Text: "When suits you?"}},
		Notes:         "The plan is the club's mooring plan, due Friday.",
		Memories:      []string{"Always write to Maria in French."},
	})
	if err != nil {
		t.Fatal(err)
	}
	compareGolden(t, "draft.user.txt", messages[1].Content)
	user := messages[1].Content
	for _, want := range []string{"<instructions>", "Say yes to Thursday", "<notes>", "mooring plan", "When suits you?", "The message to answer", "Can you do Thursday", "Always write to Maria"} {
		if !strings.Contains(user, want) {
			t.Fatalf("the draft prompt lacks %q", want)
		}
	}
	// The full conduct, not the short one: a reply is written in the
	// person's voice, which the short conduct leaves out.
	if !strings.Contains(messages[0].Content, "Sign off with: — Z") {
		t.Fatal("the draft conduct should carry the voice")
	}
}
