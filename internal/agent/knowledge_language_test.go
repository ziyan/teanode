package agent

import (
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// The notes are written in their own language when one is set, and in the
// language the agent writes to the person in otherwise; the prompts that
// write notes say which.
func TestNotesAreWrittenInTheirOwnLanguage(test *testing.T) {
	owner := &models.User{Name: "Fixture Owner", LocaleSeen: "zh"}
	if language := KnowledgeLanguage(&models.Agent{}, owner); language != "zh" {
		test.Fatalf("with nothing set the notes follow the person's language, got %q", language)
	}
	if language := KnowledgeLanguage(&models.Agent{Language: "ja"}, owner); language != "ja" {
		test.Fatalf("with nothing set the notes follow the agent's language, got %q", language)
	}
	if language := KnowledgeLanguage(&models.Agent{Language: "ja", KnowledgeLanguage: "en"}, owner); language != "en" {
		test.Fatalf("the notes' own language wins, got %q", language)
	}

	material := &digestMaterial{SourceName: "Fixture", Documents: []digestDocument{{DocumentID: "fixture-document", Heading: "Fixture heading"}}}
	request, err := buildDigestRequest(owner, "en", material, false)
	if err != nil {
		test.Fatal(err)
	}
	if !strings.Contains(request.Prompt, "Write in English, whatever language the material is in") {
		test.Fatal("the digest prompt does not say what language to write the notes in")
	}
	prompt, err := buildRememberPrompt(owner, "zh", nil, &rememberMaterial{})
	if err != nil {
		test.Fatal(err)
	}
	if !strings.Contains(prompt, "Write in Chinese, whatever language the material is in") {
		test.Fatal("the remember prompt does not say what language to write the notes in")
	}
}
