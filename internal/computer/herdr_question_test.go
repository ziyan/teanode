package computer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readHerdrFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "herdr", name+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func optionLabels(question *HerdrQuestion) []string {
	labels := []string{}
	for _, option := range question.Options {
		labels = append(labels, option.OptionLabel)
	}
	return labels
}

func TestEveryFormOfBothAgentsIsRecognizedFromTheScreen(t *testing.T) {
	cases := []struct {
		fixture          string
		codingAgentKind  string
		questionKind     string
		isMultipleChoice bool
		labels           []string
		questionHas      string
	}{
		{"claude-single", CodingAgentKindClaude, HerdrQuestionKindQuestion, false,
			[]string{"Apple", "Banana", "Cherry", "Type something", "Chat about this"}, "Which fruit should we pick?"},
		{"claude-multi", CodingAgentKindClaude, HerdrQuestionKindQuestion, true,
			[]string{"Cheese", "Olives", "Basil", "Type something", "Chat about this"}, "Which toppings do you want?"},
		{"claude-multi-review", CodingAgentKindClaude, HerdrQuestionKindQuestion, false,
			[]string{"Submit answers", "Cancel"}, "Ready to submit your answers?"},
		{"claude-tool", CodingAgentKindClaude, HerdrQuestionKindToolApproval, false,
			[]string{"Yes", "Yes, and don’t ask again for: curl -sI https://example.com/", "Yes, and switch to auto mode · auto mode handles these prompts for you", "No"},
			"curl -sI https://example.com/ | head -1"},
		{"claude-edit", CodingAgentKindClaude, HerdrQuestionKindToolApproval, false,
			[]string{"Yes", "Yes, and switch to accept edits (auto-approve file edits and common file commands) for this session (shift+tab)", "No"},
			"Do you want to create hello.txt?"},
		{"claude-plan", CodingAgentKindClaude, HerdrQuestionKindPlanApproval, false,
			[]string{"Yes, and use auto mode", "Yes, manually approve edits", "Tell Claude what to change"}, "Verify with cat hello.txt."},
		{"codex-approval", CodingAgentKindCodex, HerdrQuestionKindToolApproval, false,
			[]string{"Yes, proceed (y)", "Yes, and don't ask again for commands that start with `curl -sI https:// example.com/` (p)", "No, and tell Codex what to do differently (esc)"},
			"$ curl -sI https://example.com/"},
	}
	for _, each := range cases {
		t.Run(each.fixture, func(t *testing.T) {
			question := recognizeQuestion(each.codingAgentKind, readHerdrFixture(t, each.fixture))
			if question == nil {
				t.Fatal("no question recognized")
			}
			if question.HerdrQuestionKind != each.questionKind || question.IsMultipleChoice != each.isMultipleChoice {
				t.Errorf("kind %q, multiple %v", question.HerdrQuestionKind, question.IsMultipleChoice)
			}
			if got := strings.Join(optionLabels(question), " | "); got != strings.Join(each.labels, " | ") {
				t.Errorf("options\n got: %s\nwant: %s", got, strings.Join(each.labels, " | "))
			}
			if !strings.Contains(question.QuestionText, each.questionHas) {
				t.Errorf("question text lacks %q:\n%s", each.questionHas, question.QuestionText)
			}
			if len(question.QuestionFingerprint) != 16 {
				t.Errorf("fingerprint %q", question.QuestionFingerprint)
			}
		})
	}
}

func TestAClaudeQuestionKeepsItsOptionDescriptionsAndKinds(t *testing.T) {
	question := recognizeQuestion(CodingAgentKindClaude, readHerdrFixture(t, "claude-single"))
	if question == nil {
		t.Fatal("no question recognized")
	}
	if question.Options[1].OptionDescription != "Soft and easy to peel, a quick source of energy." {
		t.Errorf("description %q", question.Options[1].OptionDescription)
	}
	if question.Options[3].HerdrOptionKind != HerdrOptionKindFreeText || question.Options[4].HerdrOptionKind != HerdrOptionKindChat {
		t.Errorf("kinds %+v", question.Options)
	}
	if strings.Contains(question.QuestionText, "Fruit") {
		t.Errorf("the form's header is not the question: %q", question.QuestionText)
	}
}

func TestAScreenWithoutAFormHasNoQuestion(t *testing.T) {
	for _, fixture := range []string{"claude-idle", "claude-working", "codex-idle"} {
		kind := CodingAgentKindClaude
		if strings.HasPrefix(fixture, "codex") {
			kind = CodingAgentKindCodex
		}
		if question := recognizeQuestion(kind, readHerdrFixture(t, fixture)); question != nil {
			t.Errorf("%s: recognized %+v", fixture, question)
		}
	}
}

func TestTheSameQuestionHasTheSameFingerprintAndAnotherDoesNot(t *testing.T) {
	first := recognizeQuestion(CodingAgentKindClaude, readHerdrFixture(t, "claude-single"))
	again := recognizeQuestion(CodingAgentKindClaude, "some output above\n"+readHerdrFixture(t, "claude-single"))
	other := recognizeQuestion(CodingAgentKindClaude, readHerdrFixture(t, "claude-multi"))
	if first.QuestionFingerprint != again.QuestionFingerprint {
		t.Error("the same question read twice differs")
	}
	if first.QuestionFingerprint == other.QuestionFingerprint {
		t.Error("two questions share a fingerprint")
	}
}
