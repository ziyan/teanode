// Package openpage moves the dashboard the person is reading the agent in
// to a page of itself: a page of the agent's memory, a message, a settings
// tab. Asked to be shown a page, the agent had reached for the browser tool
// and opened the dashboard in a browser of its own, which the person never
// sees.
package openpage

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/models"
)

// PathLength is the longest path the tool takes.
const PathLength = 600

// pagePrefixes are the parts of the dashboard the tool may show: the
// person's own pages. Not an operator's page, and never anywhere off the
// dashboard. The dashboard holds the same list (web/src/components/showPage.ts).
var pagePrefixes = []string{"/settings/knowledge", "/mailbox", "/settings/agent", "/settings"}

func init() {
	tools.Register(func() []*tools.Tool {
		return []*tools.Tool{
			{
				Name:          "open_page",
				Family:        tools.FamilyGeneral,
				Core:          true,
				Risk:          tools.RiskRead,
				DashboardOnly: true,
				Description: "Show the person a page of the dashboard they are reading you in, by taking it there: a page of your memory (/settings/knowledge/people/some-person), a message (/mailbox/starred/ITEM_ID), your settings (/settings/agent), theirs (/settings/preference). " +
					"Use it when they ask to be shown or taken to a page. To point at a page in an answer, link it instead: [name](memory:PATH), [subject](mail:ITEM_ID). Never open the dashboard with the browser tool.",
				Parameters: tools.Object(map[string]any{
					"path":   tools.StringProperty("the dashboard path, starting with /settings/knowledge/, /mailbox/, /settings/agent/ or /settings/; a memory:PATH or mail:ITEM_ID link is taken too"),
					"reason": tools.StringProperty("a few words on why, for the line the drawer shows"),
				}, "path"),
				Run: runOpenPage,
			},
		}
	})
}

type openPageArguments struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

func runOpenPage(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[openPageArguments](call)
	if err != nil {
		return nil, err
	}
	path, err := PagePath(arguments.Path)
	if err != nil {
		return nil, err
	}
	showing, ok := tools.MustRun(ctx).(tools.Showing)
	if !ok {
		return nil, fmt.Errorf("this conversation is not in the dashboard; give the person a link instead")
	}
	showing.ShowPage(path)
	note := "Opened " + path
	if reason := strings.TrimSpace(arguments.Reason); reason != "" {
		note += ": " + reason
	}
	return &tools.Result{Content: fmt.Sprintf(`{"shown": %q}`, path), Note: note}, nil
}

// PagePath is the dashboard path a call names, or why it is not one the
// tool may show: a path under one of pagePrefixes, in segments of letters,
// digits and -._~ alone, so that no scheme, host, query, "..", or a second
// slash that a browser would read as a host, is carried to the dashboard.
func PagePath(written string) (string, error) {
	written = strings.TrimSpace(written)
	for _, scheme := range []string{"memory", "mail"} {
		if target, ok := strings.CutPrefix(written, scheme+":"); ok {
			path := models.DashboardPath(scheme, target)
			if path == "" {
				return "", fmt.Errorf("%q is not a %s: link to anything", written, scheme)
			}
			return PagePath(path)
		}
	}
	if written == "" {
		return "", fmt.Errorf("say which page: a path such as /settings/knowledge/people/some-person")
	}
	if len(written) > PathLength {
		return "", fmt.Errorf("the path is longer than %d characters", PathLength)
	}
	if !strings.HasPrefix(written, "/") {
		return "", fmt.Errorf("%q is not a path of the dashboard; it starts with one of %s", written, strings.Join(pagePrefixes, ", "))
	}
	for _, segment := range strings.Split(strings.TrimSuffix(written[1:], "/"), "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", fmt.Errorf("%q has an empty or relative part", written)
		}
		for _, character := range segment {
			if !unicode.IsLetter(character) && !unicode.IsDigit(character) && !strings.ContainsRune("-._~", character) {
				return "", fmt.Errorf("%q has %q in it; a dashboard path is letters, digits and -._~ between slashes, with no query", written, character)
			}
		}
	}
	path := strings.TrimSuffix(written, "/")
	for _, prefix := range pagePrefixes {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return path, nil
		}
	}
	return "", fmt.Errorf("%q is not a page the agent may show; it starts with one of %s", written, strings.Join(pagePrefixes, ", "))
}
