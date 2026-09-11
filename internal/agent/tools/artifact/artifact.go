// Package artifact is what the agent makes for the person to open beside
// the conversation: a page, a drawing or a document it wrote, and a chart
// drawn here from its data. Both are kept as files of the conversation,
// served to its owner alone and in a sandbox.
package artifact

import (
	"context"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

func init() {
	tools.Register(func() []*tools.Tool {
		return []*tools.Tool{
			{
				Name: "artifact", Family: tools.FamilyGeneral, Core: true, Risk: tools.RiskWrite,
				Description: "Make something the person opens beside the conversation rather than reads in it: a page (self-contained HTML, styles inline, no outside scripts or fonts; a chart is drawn as inline SVG or on a canvas with a script in the page), a picture (SVG), or a document (Markdown). Use it for a chart, a table wider than a message, a report, a mock-up. Say in the answer that it is there; the person sees it under the tool line.",
				Parameters: tools.Object(map[string]any{
					"title":   tools.StringProperty("what to call it, a few words"),
					"kind":    tools.EnumProperty("what it is", "html", "svg", "markdown"),
					"content": tools.StringProperty("the whole thing: the HTML document, the SVG element, or the Markdown"),
				}, "title", "kind", "content"),
				Guidance: "artifact: a page must be one self-contained document — no scripts, styles or fonts from elsewhere, since none load; a chart of data is the chart tool's job; keep it under 200 kB.",
				Run:      runArtifact,
			},
			{
				Name: "chart", Family: tools.FamilyGeneral, Core: true, Risk: tools.RiskWrite,
				Description: "Draw a chart of data for the person to see beside the conversation: bars, a line, or a pie, from labels and numbers. The drawing is made here, so it always shows; say in the answer that it is there.",
				Parameters: tools.Object(map[string]any{
					"title":  tools.StringProperty("what the chart shows, a few words"),
					"kind":   tools.EnumProperty("the kind", "bar", "line", "pie"),
					"labels": tools.ArrayProperty("one label per point, in order", tools.StringProperty("a label")),
					"series": tools.ArrayProperty("one or more series of numbers, each as long as labels", tools.Object(map[string]any{
						"name":   tools.StringProperty("what the series is"),
						"values": tools.ArrayProperty("the numbers, one per label", map[string]any{"type": "number"}),
					}, "values")),
					"unit": tools.StringProperty("what the numbers are, for the axis: messages, EUR, hours"),
				}, "title", "kind", "labels", "series"),
				Run: runChart,
			},
		}
	})
}

// Artifacts: what the model makes for the person to open beside the
// conversation. Stored as a file of the conversation, served to its owner
// alone and in a sandbox, shown under the tool line that made it.

type artifactArguments struct {
	Title   string `json:"title"`
	Kind    string `json:"kind"`
	Content string `json:"content"`
}

// artifactBytes bounds one artifact.
const artifactBytes = 200 << 10

func runArtifact(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[artifactArguments](call)
	if err != nil {
		return nil, err
	}
	title := strings.TrimSpace(arguments.Title)
	content := strings.TrimSpace(arguments.Content)
	if title == "" || content == "" {
		return nil, fmt.Errorf("an artifact needs a title and its content")
	}
	if len(content) > artifactBytes {
		return nil, fmt.Errorf("the artifact is larger than %d bytes; make it smaller", artifactBytes)
	}
	if reason := reachesOut(content); reason != "" {
		return nil, fmt.Errorf("the artifact %s; nothing from another server loads in it — inline every script and style, and draw a chart yourself with SVG shapes, or use the chart tool", reason)
	}
	var contentType, extension string
	switch arguments.Kind {
	case "html":
		contentType, extension = "text/html; charset=utf-8", ".html"
	case "svg":
		contentType, extension = "image/svg+xml", ".svg"
	case "markdown":
		contentType, extension = "text/markdown; charset=utf-8", ".md"
	default:
		return nil, fmt.Errorf("%q is not html, svg or markdown", arguments.Kind)
	}
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	store := run.Storage()
	if store == nil {
		return nil, fmt.Errorf("nowhere to keep an artifact")
	}
	var created *models.AgentAttachment
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		created, err = tx.CreateAgentAttachment(&models.AgentAttachment{
			AgentID:        run.Agent().ID,
			ConversationID: run.Conversation().ID,
			MessageID:      artifactMessage,
			Name:           safeFilename(title) + extension,
			ContentType:    contentType,
			Size:           int64(len(content)),
		})
		if err != nil {
			return err
		}
		return store.PutFile(ctx, created.ID, []byte(content))
	}); err != nil {
		return nil, err
	}
	result, err := tools.JSONResult(map[string]any{
		"artifact_id": created.ID, "title": title, "kind": arguments.Kind,
		"url": "/api/v1/agent/attachments/" + created.ID,
	})
	if err != nil {
		return nil, err
	}
	result.Note = "made " + title
	return result, nil
}

// reachesOut says how a page would load something from elsewhere, which
// the sandbox it is shown in refuses; better told now than shown blank.
func reachesOut(content string) string {
	lower := strings.ToLower(content)
	for _, pattern := range []string{`<script src=`, `<script type="module" src=`, `<link rel="stylesheet"`, `<link href=`, `@import url(`, `@import "http`, `@import 'http`} {
		if strings.Contains(lower, pattern) {
			return "loads " + strings.Trim(pattern, `<>="@`) + " from another server"
		}
	}
	if strings.Contains(lower, `src="http`) || strings.Contains(lower, `src='http`) || strings.Contains(lower, `src="//`) {
		return "loads something from another server"
	}
	return ""
}

// artifactMessage marks an attachment as an artifact of the conversation
// rather than a file that came with a turn, so the sweep leaves it and the
// drawer knows what it is.
const artifactMessage = "artifact"

// safeFilename is a title as a file name: letters, digits and dashes.
func safeFilename(title string) string {
	var builder strings.Builder
	for _, character := range strings.ToLower(title) {
		switch {
		case character >= 'a' && character <= 'z', character >= '0' && character <= '9':
			builder.WriteRune(character)
		case character == ' ' || character == '-' || character == '_':
			builder.WriteRune('-')
		}
	}
	name := strings.Trim(builder.String(), "-")
	if name == "" {
		name = "artifact"
	}
	if len(name) > 60 {
		name = name[:60]
	}
	return name
}
