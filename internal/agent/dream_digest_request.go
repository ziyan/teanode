package agent

import (
	"strings"
	"unicode/utf8"

	"github.com/ziyan/teanode/internal/models"
)

type digestRequest struct {
	Prompt string
	Shown  map[string]string
}

// The evidence map retains the original text; the prompt escapes delimiters
// so document contents cannot close the block that contains them.
func buildDigestRequest(owner *models.User, knowledgeLanguage string, material *digestMaterial, isCoarse bool) (*digestRequest, error) {
	var builder strings.Builder
	shown := make(map[string]string, len(material.Documents))
	for _, document := range material.Documents {
		builder.WriteString("[" + document.DocumentID + "] " + document.Heading + "\n")
		if document.Text != "" {
			builder.WriteString(unclosable(document.Text) + "\n")
		}
		builder.WriteString("\n")
		shown[document.DocumentID] = document.Heading + "\n" + document.Text
	}
	prompt, err := render("digest.txt", map[string]any{
		"KnowledgeLanguage": languageName(knowledgeLanguage),
		"SourceName":        material.SourceName,
		"SourceRoot":        material.SourceRoot,
		"PersonName":        personName(owner),
		"Index":             material.IndexLines,
		"Items":             builder.String(),
		"Most":              factsAllowedFor(shown),
		"Coarse":            isCoarse,
	})
	if err != nil {
		return nil, err
	}
	return &digestRequest{Prompt: prompt, Shown: shown}, nil
}

// factsAllowedFor is how many facts a reading of this much text may file:
// the batch's share, and more as the text grows, about one for each few
// hundred characters. A fixed number filed ten facts from twenty whole
// conversations and dropped the rest of what they said.
func factsAllowedFor(shown map[string]string) int {
	runes := 0
	for _, text := range shown {
		runes += utf8.RuneCountInString(text)
	}
	return max(digestFacts, runes/factRunes)
}

// factRunes is how much text one more fact may be filed for.
const factRunes = 600
