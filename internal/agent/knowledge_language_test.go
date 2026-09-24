package agent

import (
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// The agent's own work is written in its notes language when one is set,
// else in a language somebody set, never in the one the dashboard happens
// to be shown in; the prompts that write notes say which.
func TestNotesAreWrittenInTheirOwnLanguage(test *testing.T) {
	owner := &models.User{Name: "Fixture Owner", LocaleSeen: "zh"}
	if language := KnowledgeLanguage(&models.Agent{}, owner); language != "" {
		test.Fatalf("the dashboard's language must not reach the agent's own work, got %q", language)
	}
	if language := Language(&models.Agent{}, owner); language != "zh" {
		test.Fatalf("a conversation follows the dashboard's language, got %q", language)
	}
	chosen := &models.User{Name: "Fixture Owner", Locale: "de", LocaleSeen: "zh"}
	if language := KnowledgeLanguage(&models.Agent{}, chosen); language != "de" {
		test.Fatalf("with nothing set on the agent its work follows the account's chosen locale, got %q", language)
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
