// Package artifact is what the agent makes for the person to open beside
// the conversation: a page, a drawing or a document it wrote, kept as a
// file of the conversation, served to its owner alone and in a sandbox. A
// page may draw charts with the library this server offers it, in the
// dashboard's own look.
package artifact

import (
	"context"
	"fmt"
	"regexp"
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
				Description: "Make something the person opens beside the conversation rather than reads in it: a page (HTML), a picture (SVG), or a document (Markdown). Use it for a chart, a table wider than a message, a report, a mock-up. A page is one self-contained document: nothing loads from another server. It gets the dashboard's look on its own (classes .card, .grid, .muted, .good/.bad/.warn, plain tables, a .chart box 320px tall), and when it calls teanode.chart(element, option) — a plain ECharts 5 option: title, tooltip, legend, xAxis and yAxis with the series, or a pie series — the chart is drawn in the dashboard's colours and type, sized to its box, following the person's theme. Say in the answer that it is there; the person sees it under the tool line.",
				Parameters: tools.Object(map[string]any{
					"title":   tools.StringProperty("what to call it, a few words"),
					"kind":    tools.EnumProperty("what it is", "html", "svg", "markdown"),
					"content": tools.StringProperty("the whole thing: the HTML document, the SVG element, or the Markdown"),
				}, "title", "kind", "content"),
				Guidance: "artifact: a chart is a page with <div class=\"chart\"></div> and a <script> that calls teanode.chart(element, option); link nothing — the library and the look come with the page — and let the theme choose colours and type; name every unit. Nothing loads from another server; keep a page under 200 kB.",
				Run:      runArtifact,
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
		return nil, fmt.Errorf("the artifact %s; nothing from another server loads in it — inline every script and style, and draw a chart with /assets/echarts.min.js and teanode.chart from /assets/artifact.js", reason)
	}
	var contentType, extension string
	switch arguments.Kind {
	case "html":
		contentType, extension = "text/html; charset=utf-8", ".html"
		content = completePage(content)
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

// The files this server offers a page, by their addresses.
const (
	artifactStylesheet   = "/assets/artifact.css"
	artifactChartLibrary = "/assets/echarts.min.js"
	artifactScript       = "/assets/artifact.js"
)

// loaded matches what a page loads by address: a script's src or a link's
// href, whatever else the tag carries and in whatever order.
var loaded = regexp.MustCompile(`(?is)<(script|link)\b[^>]*?\b(?:src|href)\s*=\s*["']?([^"'\s>]+)`)

// reachesOut says how a page would load something from elsewhere, which
// the sandbox it is shown in refuses; better told now than shown blank.
// What this server offers is not elsewhere.
func reachesOut(content string) string {
	for _, match := range loaded.FindAllStringSubmatch(content, -1) {
		address := match[2]
		switch address {
		case artifactStylesheet, artifactChartLibrary, artifactScript:
			continue
		}
		return "loads " + strings.ToLower(match[1]) + " " + address + " from another server"
	}
	lower := strings.ToLower(content)
	for _, pattern := range []string{`@import url(`, `@import "http`, `@import 'http`} {
		if strings.Contains(lower, pattern) {
			return "loads " + strings.Trim(pattern, `<>="@`) + " from another server"
		}
	}
	if strings.Contains(lower, `src="http`) || strings.Contains(lower, `src='http`) || strings.Contains(lower, `src="//`) {
		return "loads something from another server"
	}
	// The sandbox stops every request but one: the page navigating itself
	// away, which would carry whatever it holds in the address. So a page
	// may not go anywhere: no links out, no refresh, no location.
	for _, pattern := range []string{`http-equiv="refresh"`, `http-equiv='refresh'`, `location.href`, `location.assign`, `location.replace`, `location=`, `location =`, `window.open(`, `href="http`, `href='http`, `href="//`, `href='//`, `<form`} {
		if strings.Contains(lower, pattern) {
			return "would leave the page (" + strings.Trim(pattern, `<>="'`) + "); a page stays where it is and names an address as text"
		}
	}
	return ""
}

// completePage gives a page what this server offers: the dashboard's look,
// and the chart library with its helper when the page draws a chart. Put at
// the head of the document, before anything the page runs, and only when
// the page did not link them itself.
func completePage(content string) string {
	var links []string
	if !strings.Contains(content, artifactStylesheet) {
		links = append(links, `<link rel="stylesheet" href="`+artifactStylesheet+`">`)
	}
	lower := strings.ToLower(content)
	if strings.Contains(lower, "teanode.chart") || strings.Contains(lower, "echarts") {
		if !strings.Contains(content, artifactChartLibrary) {
			links = append(links, `<script src="`+artifactChartLibrary+`"></script>`)
		}
		if !strings.Contains(content, artifactScript) {
			links = append(links, `<script src="`+artifactScript+`"></script>`)
		}
	}
	if len(links) == 0 {
		return content
	}
	inserted := strings.Join(links, "")
	if index := strings.Index(lower, "<head>"); index >= 0 {
		return content[:index+len("<head>")] + inserted + content[index+len("<head>"):]
	}
	if index := strings.Index(lower, "<head "); index >= 0 {
		if end := strings.Index(content[index:], ">"); end >= 0 {
			return content[:index+end+1] + inserted + content[index+end+1:]
		}
	}
	if index := strings.Index(lower, "<body"); index >= 0 {
		return content[:index] + "<head>" + inserted + "</head>" + content[index:]
	}
	return inserted + content
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
