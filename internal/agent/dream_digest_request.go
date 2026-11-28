package agent

import (
	"strings"

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
		"Coarse":            isCoarse,
	})
	if err != nil {
		return nil, err
	}
	if material.HasRecordingEvidence {
		prompt = recordingLearningInstructions + "\n\n" + prompt
	}
	return &digestRequest{Prompt: prompt, Shown: shown}, nil
}
