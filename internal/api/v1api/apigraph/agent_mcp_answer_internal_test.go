package apigraph

import (
	"strings"
	"testing"

	agenttools "github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/llm"
)

// A screenshot's picture goes to a program using the tools over MCP beside
// the words that say it follows; a part that is not a picture does not.
func TestAToolsPictureIsInItsMCPAnswer(t *testing.T) {
	answer := mcpAnswerOf(&agenttools.Result{
		Content:   `{"picture":"the screenshot follows for you to look at"}`,
		Untrusted: true,
		Images: []llm.ContentPart{
			{Type: "image", MediaType: "image/png", Data: []byte("a picture")},
			{Type: "text", Text: "not a picture"},
			{Type: "image", MediaType: "image/png"},
		},
	})
	if !answer.IsUntrusted || answer.Text == "" {
		t.Fatalf("the words were lost: %+v", answer)
	}
	if len(answer.Images) != 1 || answer.Images[0].MediaType != "image/png" || string(answer.Images[0].Data) != "a picture" {
		t.Fatalf("the pictures came back as %+v", answer.Images)
	}
}

// A picture bigger than a model takes is left out, and the answer says so.
func TestATooLargePictureIsLeftOutOfItsMCPAnswer(t *testing.T) {
	answer := mcpAnswerOf(&agenttools.Result{
		Content: "a whole page",
		Images:  []llm.ContentPart{{Type: "image", MediaType: "image/png", Data: make([]byte, mcpImageBytes+1)}},
	})
	if len(answer.Images) != 0 || !strings.Contains(answer.Text, "was left out") {
		t.Fatalf("the answer was %q with %d pictures", answer.Text, len(answer.Images))
	}
}
