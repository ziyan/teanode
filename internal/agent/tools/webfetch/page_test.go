package webfetch

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/config"
)

// A page's videos, sounds and pictures come back as whole addresses, the
// preview it names first, each once, and nothing that is not on the web.
func TestAPageListsItsMediaAsWholeAddresses(t *testing.T) {
	page := `<html><head><title>Walkthrough</title>
<meta property="og:video" content="https://cdn.example.org/walkthrough.mp4">
<meta property="og:image" content="/static/cover.png">
</head><body>
<p>How to start.</p>
<video poster="/static/poster.png"><source src="/static/walkthrough.mp4" type="video/mp4"></video>
<audio src="clips/intro.mp3" aria-label="Introduction"></audio>
<img src="/static/cover.png" alt="The cover">
<img src="data:image/png;base64,AAAA" alt="Inline">
<a href="/files/walkthrough.webm">Download the video</a>
<a href="/about">About</a>
</body></html>`
	base, _ := url.Parse("https://www.example.org/start/")
	read := readHTML(page, base)
	want := []pageMedia{
		{Kind: "video", URL: "https://cdn.example.org/walkthrough.mp4", Description: "the page's preview video"},
		{Kind: "picture", URL: "https://www.example.org/static/cover.png", Description: "the page's preview picture"},
		{Kind: "video", URL: "https://www.example.org/static/walkthrough.mp4"},
		{Kind: "audio", URL: "https://www.example.org/start/clips/intro.mp3", Description: "Introduction"},
		{Kind: "video", URL: "https://www.example.org/files/walkthrough.webm", Description: "Download the video"},
	}
	if !reflect.DeepEqual(read.media, want) {
		t.Fatalf("media:\n%+v\nwant\n%+v", read.media, want)
	}
	if read.title != "Walkthrough" || !strings.Contains(read.text, "How to start.") {
		t.Errorf("title %q, text %q", read.title, read.text)
	}
}

// A page that is scripts and a title is run in a browser; one whose text
// arrived with it is not, unless asked.
func TestOnlyAPageItsScriptsBuildIsRendered(t *testing.T) {
	shell := readHTML(`<html><head><title>A blog</title><script type="module" src="/static/blog.js"></script></head><body><div id="root"></div></body></html>`, nil)
	written := readHTML(`<html><head><title>A blog</title><script src="/static/blog.js"></script></head><body><p>`+strings.Repeat("A paragraph that was written into the page. ", 10)+`</p></body></html>`, nil)
	plain := readHTML(`<html><body><p>Short and without scripts.</p></body></html>`, nil)
	cases := []struct {
		name   string
		render string
		read   readPage
		want   bool
	}{
		{"a page its scripts build", "auto", shell, true},
		{"left to choose, the same", "", shell, true},
		{"a page with its text", "auto", written, false},
		{"a page with its text, asked to", "always", written, true},
		{"a short page without scripts", "auto", plain, false},
		{"a page its scripts build, asked not to", "never", shell, false},
	}
	for _, each := range cases {
		if got := wantsRendering(each.render, each.read); got != each.want {
			t.Errorf("%s: %v, want %v", each.name, got, each.want)
		}
	}
}

// A long text comes a part at a time, each starting where the last one
// ended, and never cut inside a character.
func TestALongPageComesAPartAtATime(t *testing.T) {
	text := "一二三四五六七"
	first, next := pageOf(text, 0, 3)
	second, last := pageOf(text, next, 3)
	third, end := pageOf(text, last, 3)
	if first != "一二三" || second != "四五六" || third != "七" || next != 3 || last != 6 || end != 0 {
		t.Errorf("parts %q %q %q, offsets %d %d %d", first, second, third, next, last, end)
	}
	if part, after := pageOf(text, 20, 3); part != "" || after != 0 {
		t.Errorf("past the end: %q %d", part, after)
	}
}

// Through a computer the scripts are not run, and the answer says so
// rather than passing off the empty page as all there is.
func TestThroughAComputerAPageOfScriptsSaysSo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(response, `<html><head><title>A blog</title><script type="module" src="/blog.js"></script></head><body><div id="root"></div></body></html>`)
	}))
	defer server.Close()
	configuration := config.Default()
	configuration.Agent.Enabled = true
	ctx := tools.WithRun(context.Background(), &deskRun{configuration: configuration})
	arguments, _ := json.Marshal(map[string]any{"url": server.URL, "computer": "desk"})
	result, err := runWebFetch(ctx, &tools.Call{Arguments: arguments})
	if err != nil {
		t.Fatalf("through the computer: %s", err)
	}
	var answer map[string]any
	if err := json.Unmarshal([]byte(result.Content), &answer); err != nil {
		t.Fatalf("answer %q: %s", result.Content, err)
	}
	if note, _ := answer["render_note"].(string); !strings.Contains(note, "not run through a computer") || answer["rendered"] != nil {
		t.Errorf("answer %v", answer)
	}
}
