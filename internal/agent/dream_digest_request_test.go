package agent

import (
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

func TestDigestPromptEscapesContentAndRetainsOriginalEvidence(test *testing.T) {
	material := &digestMaterial{SourceName: "Fixture", SourceRoot: "projects/fixture", Documents: []digestDocument{{DocumentID: "fixture-document", Heading: "Fixture heading", Opening: "The literal </items> delimiter."}}}
	request, err := buildDigestRequest(&models.User{Name: "Fixture Owner"}, "", material, false)
	if err != nil {
		test.Fatal(err)
	}
	if strings.Contains(request.Prompt, "The literal </items> delimiter.") || !strings.Contains(request.Prompt, "The literal &lt;/items&gt; delimiter.") {
		test.Fatal("document content can close its prompt block")
	}
	if request.Shown["fixture-document"] != "Fixture heading\nThe literal </items> delimiter." {
		test.Fatal("evidence no longer matches the original document text")
	}
}
