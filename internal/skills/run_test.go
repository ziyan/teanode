package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/util/safefetch"
)

// fakeShell stands in for the person's attached computer.
type fakeShell struct {
	ran     []string
	prints  string
	windows bool
}

func (self *fakeShell) Run(ctx context.Context, command string, timeout time.Duration) (string, error) {
	self.ran = append(self.ran, command)
	return self.prints, nil
}

func (self *fakeShell) Windows() bool { return self.windows }

// The weather skill is a workflow whose second step uses what the first
// one found, which is the shape most skills are.
func TestAWorkflowFeedsOneStepIntoTheNext(t *testing.T) {
	var asked []string
	service := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		asked = append(asked, request.URL.Path+"?"+request.URL.RawQuery)
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(request.URL.Path, "/search"):
			_, _ = writer.Write([]byte(`[{"lat":"41.50","lon":"-71.30","display_name":"Fairhaven"}]`))
		case strings.HasPrefix(request.URL.Path, "/points/"):
			_, _ = writer.Write([]byte(`{"properties":{"periods":[{"shortForecast":"Partly cloudy","temperature":68}]}}`))
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer service.Close()

	body := "---\nname: weather\ndescription: the weather\ntools:\n" +
		"  - name: get_weather\n    description: the forecast\n    type: workflow\n" +
		"    parameters: {type: object, properties: {location: {type: string}}, required: [location]}\n" +
		"    steps:\n" +
		"      - name: place\n        type: http\n        url: \"" + service.URL + "/search?q={{location}}\"\n        result: json\n        select: {lat: \"0.lat\", lon: \"0.lon\"}\n" +
		"      - name: forecast\n        type: http\n        url: \"" + service.URL + "/points/{{steps.place.lat}},{{steps.place.lon}}\"\n        result: json\n        select: {summary: \"properties.periods[0].shortForecast\", degrees: \"properties.periods[0].temperature\"}\n" +
		"---\n"
	skill, err := Parse([]byte(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	answer, err := skill.Run(context.Background(), "get_weather", map[string]any{"location": "Fairhaven, GA"}, &Running{Client: service.Client()})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	forecast, _ := answer["forecast"].(map[string]any)
	if forecast["summary"] != "Partly cloudy" || forecast["degrees"] != float64(68) {
		t.Fatalf("the second step selected from what it fetched: %v", answer)
	}
	if len(asked) != 2 || !strings.Contains(asked[0], "Fairhaven") || !strings.Contains(asked[1], "41.50,-71.30") {
		t.Fatalf("the first step's answer went into the second's address: %v", asked)
	}
	// Something the schema asks for and the call left out.
	if _, err := skill.Run(context.Background(), "get_weather", nil, &Running{Client: service.Client()}); err == nil || !strings.Contains(err.Error(), "location") {
		t.Fatalf("a missing parameter is named: %v", err)
	}
}

// A command is quoted before it travels, and never runs here.
func TestAShellToolGoesToTheComputerQuoted(t *testing.T) {
	body := "---\nname: git\ndescription: git\ntools:\n" +
		"  - name: git_log\n    description: the log\n    type: shell\n    command: [git, log, \"--grep={{words}}\"]\n" +
		"    parameters: {type: object, properties: {words: {type: string}}}\n---\n"
	skill, err := Parse([]byte(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	shell := &fakeShell{prints: "one commit"}
	answer, err := skill.Run(context.Background(), "git_log", map[string]any{"words": "fix'; rm -rf ~"}, &Running{Shell: shell})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if answer["text"] != "one commit" {
		t.Fatalf("what it printed comes back: %v", answer)
	}
	if len(shell.ran) != 1 || !strings.HasPrefix(shell.ran[0], "'git' 'log' ") {
		t.Fatalf("every word is quoted: %v", shell.ran)
	}
	if want := `'git' 'log' '--grep=fix'\''; rm -rf ~'`; shell.ran[0] != want {
		t.Fatalf("the command is quoted exactly:\n want %s\n  got %s", want, shell.ran[0])
	}
	// The quoting above is /bin/sh's. cmd gives single quotes no meaning,
	// so a value carrying & would start another command there; until that
	// is written, a Windows computer is refused rather than trusted.
	windows := &fakeShell{windows: true}
	if _, err := skill.Run(context.Background(), "git_log", map[string]any{"words": "x"}, &Running{Shell: windows}); err == nil || len(windows.ran) != 0 {
		t.Fatalf("a Windows computer is refused, not run on: %v %v", err, windows.ran)
	}

	// With no computer attached there is nowhere to run it, and the skill
	// says so rather than failing obscurely.
	if _, err := skill.Run(context.Background(), "git_log", map[string]any{"words": "x"}, nil); err == nil || !strings.Contains(err.Error(), "teanode computer start") {
		t.Fatalf("it says what to do: %v", err)
	}
}

// A secret the operator filled in reaches the request; one that is missing
// is an error rather than an empty header.
func TestSecretsAndAuthentication(t *testing.T) {
	var seen string
	service := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		seen = request.Header.Get("Authorization")
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"ok":true}`))
	}))
	defer service.Close()
	body := "---\nname: tracker\ndescription: tracker\nsecrets:\n  - key: TRACKER_TOKEN\n" +
		"authenticationProfiles:\n  tracker: {type: bearer, token: \"{{secret:TRACKER_TOKEN}}\"}\n" +
		"tools:\n  - name: tracker_get\n    description: get\n    type: http\n    url: \"" + service.URL + "/thing\"\n    auth: tracker\n    result: json\n    parameters: {type: object, properties: {}}\n---\n"
	skill, err := Parse([]byte(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := skill.Run(context.Background(), "tracker_get", nil, &Running{Client: service.Client(), Secrets: Secrets{"TRACKER_TOKEN": "abc123"}}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if seen != "Bearer abc123" {
		t.Fatalf("the secret went on the request: %q", seen)
	}
	if _, err := skill.Run(context.Background(), "tracker_get", nil, &Running{Client: service.Client()}); err == nil || !strings.Contains(err.Error(), "TRACKER_TOKEN") {
		t.Fatalf("a secret nobody filled in is named: %v", err)
	}
}

// One tool routing on one of its parameters, which is how a skill offers
// six operations without offering six tools.
func TestARoutedToolPicksItsSteps(t *testing.T) {
	service := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"where":"` + request.URL.Path + `"}`))
	}))
	defer service.Close()
	body := "---\nname: box\ndescription: box\ntools:\n" +
		"  - name: box_ops\n    description: ops\n    type: workflow\n    actionField: action\n" +
		"    parameters: {type: object, properties: {action: {type: string}}, required: [action]}\n" +
		"    actions:\n" +
		"      list: [{name: list, type: http, url: \"" + service.URL + "/list\", result: json, select: {where: where}}]\n" +
		"      get: [{name: get, type: http, url: \"" + service.URL + "/get\", result: json, select: {where: where}}]\n---\n"
	skill, err := Parse([]byte(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	answer, err := skill.Run(context.Background(), "box_ops", map[string]any{"action": "get"}, &Running{Client: service.Client()})
	if err != nil || answer["where"] != "/get" {
		t.Fatalf("the named action ran: %v %v", answer, err)
	}
	if _, err := skill.Run(context.Background(), "box_ops", map[string]any{"action": "burn"}, &Running{Client: service.Client()}); err == nil {
		t.Fatal("an action it does not have is refused")
	}
}

// A service that answers badly is reported with what it said, and one
// that answers something huge is cut.
func TestWhatAServiceAnswersIsBoundedAndReported(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Error(writer, "no such place", http.StatusNotFound)
	}))
	defer failing.Close()
	body := "---\nname: x\ndescription: x\ntools:\n  - name: x_get\n    description: get\n    type: http\n    url: \"" + failing.URL + "/a\"\n    result: json\n    parameters: {type: object, properties: {}}\n---\n"
	skill, _ := Parse([]byte(body))
	_, err := skill.Run(context.Background(), "x_get", nil, &Running{Client: failing.Client()})
	if err == nil || !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "no such place") {
		t.Fatalf("the status and what it said: %v", err)
	}
}

// The address guard is the one the rest of the server fetches through, so
// a skill cannot use this server to reach the network it sits in.
func TestAPrivateAddressIsRefused(t *testing.T) {
	body := "---\nname: x\ndescription: x\ntools:\n  - name: x_get\n    description: get\n    type: http\n    url: \"http://127.0.0.1:9/secret\"\n    result: json\n    parameters: {type: object, properties: {}}\n---\n"
	skill, _ := Parse([]byte(body))
	_, err := skill.Run(context.Background(), "x_get", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "not a public address") {
		t.Fatalf("the address guard should refuse it by name, got %v", err)
	}
}

func TestPickReadsAPath(t *testing.T) {
	var value any
	_ = json.Unmarshal([]byte(`{"properties":{"periods":[{"t":68},{"t":70}]},"list":[{"a":"b"}]}`), &value)
	for path, want := range map[string]any{
		"properties.periods[0].t": float64(68),
		"properties.periods.1.t":  float64(70),
		"list[0].a":               "b",
	} {
		got, ok := pick(value, path)
		if !ok || got != want {
			t.Errorf("%s: want %v, got %v (%v)", path, want, got, ok)
		}
	}
	if _, ok := pick(value, "properties.nowhere"); ok {
		t.Error("a path that matches nothing says so")
	}
}

// Every skill this server will install is carried as far as its first
// request, which proves the file and the interpreter agree about it.
// Nothing is fetched: each tool is asked for with no arguments and must
// complain, and the client refuses to dial at all.
func TestTheRealSkillsAreUnderstood(t *testing.T) {
	files, err := filepath.Glob("testdata/*.md")
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	refusing := &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("this test must not reach %s", request.URL.Host)
	})}
	for _, file := range files {
		if _, no := refused[file]; no {
			continue
		}
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		skill, err := Parse(content)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		for _, tool := range skill.Tools {
			if _, err := skill.Run(context.Background(), tool.Name, nil, &Running{Client: refusing}); err == nil {
				t.Errorf("%s %s: asked for with nothing, it should complain", file, tool.Name)
			}
		}
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (self roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return self(request)
}

// The condition on a step decides whether it runs. It used to be filled in
// and then read as a word, so "enabled == true" was never false and both
// branches of a routed skill ran, one undoing the other.
func TestAConditionDecidesWhetherAStepRuns(t *testing.T) {
	var ran []string
	service := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		ran = append(ran, request.URL.Path)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"ok":true}`))
	}))
	defer service.Close()

	body := "---\nname: x\ndescription: x\ntools:\n  - name: x_set\n    description: set\n    type: workflow\n" +
		"    parameters: {type: object, properties: {enabled: {type: boolean}}, required: [enabled]}\n    steps:\n" +
		"      - name: turn_on\n        type: http\n        url: \"" + service.URL + "/on\"\n        if: \"enabled == true\"\n        result: json\n" +
		"      - name: turn_off\n        type: http\n        url: \"" + service.URL + "/off\"\n        if: \"enabled == false\"\n        result: json\n---\n"
	skill, err := Parse([]byte(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, want := range []struct {
		enabled bool
		path    string
	}{{true, "/on"}, {false, "/off"}} {
		ran = nil
		if _, err := skill.Run(context.Background(), "x_set", map[string]any{"enabled": want.enabled}, &Running{Client: service.Client()}); err != nil {
			t.Fatalf("enabled=%v: %v", want.enabled, err)
		}
		if len(ran) != 1 || ran[0] != want.path {
			t.Fatalf("enabled=%v should run only %s, ran %v", want.enabled, want.path, ran)
		}
	}

	// A condition this cannot carry out is refused when the skill is read,
	// not found out when somebody calls it.
	bad := strings.Replace(body, "enabled == true", "enabled and true", 1)
	if _, err := Parse([]byte(bad)); err == nil {
		t.Fatal("a condition it cannot work out must be refused")
	}
}

// A condition naming something no step produced is not an error: "only if
// the last step found one" is what the field is for.
func TestAConditionOnSomethingMissingSkips(t *testing.T) {
	var ran []string
	service := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		ran = append(ran, request.URL.Path)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{}`))
	}))
	defer service.Close()
	body := "---\nname: x\ndescription: x\ntools:\n  - name: x_go\n    description: go\n    type: workflow\n" +
		"    parameters: {type: object, properties: {}}\n    steps:\n" +
		"      - name: first\n        type: http\n        url: \"" + service.URL + "/a\"\n        result: json\n        select: {found: nothing.here}\n" +
		"      - name: second\n        type: http\n        url: \"" + service.URL + "/b\"\n        if: \"{{steps.first.found}}\"\n        result: json\n---\n"
	skill, err := Parse([]byte(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := skill.Run(context.Background(), "x_go", nil, &Running{Client: service.Client()}); err != nil {
		t.Fatalf("a missing value skips rather than failing: %v", err)
	}
	if len(ran) != 1 || ran[0] != "/a" {
		t.Fatalf("the second step should have been skipped: %v", ran)
	}
}

// A schema that cannot be written as JSON would break every request from
// every person on the server, so it is refused when the skill is read.
func TestASchemaThatCannotBeSentIsRefused(t *testing.T) {
	body := "---\nname: x\ndescription: x\ntools:\n  - name: x_go\n    description: go\n    type: http\n" +
		"    url: \"https://example.com/a\"\n    parameters: {type: object, properties: {2024: {type: string}}}\n---\n"
	if _, err := Parse([]byte(body)); err == nil || !strings.Contains(err.Error(), "cannot be sent to a model") {
		t.Fatalf("want a refusal about sending it to a model, got %v", err)
	}
}

// A credential goes where the skill says, never where the caller says.
func TestACredentialCannotBeSentToAChosenHost(t *testing.T) {
	chosen := "---\nname: x\ndescription: x\nsecrets:\n  - key: TOKEN\n" +
		"authenticationProfiles:\n  it: {type: bearer, token: \"{{secret:TOKEN}}\"}\n" +
		"tools:\n  - name: x_get\n    description: get\n    type: http\n    url: \"https://{{host}}/a\"\n    auth: it\n    result: json\n" +
		"    parameters: {type: object, properties: {host: {type: string}}}\n---\n"
	if _, err := Parse([]byte(chosen)); err == nil || !strings.Contains(err.Error(), "chosen by whoever calls it") {
		t.Fatalf("a host from a parameter must be refused: %v", err)
	}
	// The same skill with the host from a secret the operator sets is
	// fine: the operator settles both where it goes and what is sent.
	settled := strings.ReplaceAll(chosen, "{{host}}", "{{secret:HOST}}")
	settled = strings.Replace(settled, "  - key: TOKEN\n", "  - key: TOKEN\n  - key: HOST\n", 1)
	if _, err := Parse([]byte(settled)); err != nil {
		t.Fatalf("a host from a secret is fine: %v", err)
	}
	// A host each person names for themselves, carrying a credential of
	// the operator's, is the same hole by another route: one person would
	// choose where everybody's token is sent.
	theirs := strings.Replace(settled, "  - key: HOST\n", "  - key: HOST\n    scope: person\n", 1)
	if _, err := Parse([]byte(theirs)); err == nil || !strings.Contains(err.Error(), "scope them the same way") {
		t.Fatalf("a person's host with the operator's token must be refused: %v", err)
	}
	// Both of them the person's own is fine: their host, their token.
	both := strings.Replace(theirs, "  - key: TOKEN\n", "  - key: TOKEN\n    scope: person\n", 1)
	if _, err := Parse([]byte(both)); err != nil {
		t.Fatalf("a person's own host and token together are fine: %v", err)
	}
}

// A secret is declared once, with one scope, and under a key that can be
// filled in: anything else is a skill nobody could ever use.
func TestSecretsAreDeclaredOnceAndCanBeFilledIn(t *testing.T) {
	shape := "---\nname: x\ndescription: x\nsecrets:\n%s" +
		"tools:\n  - name: x_get\n    description: get\n    type: http\n    url: \"https://example.com/a\"\n" +
		"    headers: {X: \"{{secret:TOKEN}}\"}\n    result: json\n    parameters: {type: object, properties: {}}\n---\n"
	for _, refused := range []struct {
		secrets string
		says    string
	}{
		{"  - key: TOKEN\n  - key: TOKEN\n    scope: person\n", "twice"},
		{"  - key: TOKEN\n  - key: " + strings.Repeat("L", 201) + "\n", "longer than"},
		{"  - key: TOKEN\n    scope: nobody\n", "not operator or person"},
	} {
		if _, err := Parse([]byte(fmt.Sprintf(shape, refused.secrets))); err == nil || !strings.Contains(err.Error(), refused.says) {
			t.Fatalf("want a refusal about %q, got %v", refused.says, err)
		}
	}
	// A key written with a stray space is the key without it: a reference
	// is trimmed when it is read, and so is a key on its way into the
	// table, so anything else would name something unfillable.
	skill, err := Parse([]byte(fmt.Sprintf(shape, "  - key: \" TOKEN \"\n    scope: person\n")))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	mine := skill.PersonalSecrets(ScopeAsDeclared)
	if len(mine) != 1 || mine[0].Key != "TOKEN" {
		t.Fatalf("the key is trimmed where it is declared: %+v", mine)
	}
	if got := skill.SecretsFor("x_get"); len(got) != 1 || got[0] != "TOKEN" {
		t.Fatalf("the tool asks for TOKEN: %v", got)
	}
	if got := skill.SecretsFor("x_absent"); got != nil {
		t.Fatalf("a tool that does not exist asks for nothing: %v", got)
	}
}

// One tool waiting on a value of the person's own must not hold back the
// skill's other tools, which may want nothing at all.
func TestEachToolAsksOnlyForWhatItUses(t *testing.T) {
	body := "---\nname: x\ndescription: x\nsecrets:\n  - key: MINE\n    scope: person\n  - key: OURS\n" +
		"authenticationProfiles:\n  it: {type: bearer, token: \"{{secret:OURS}}\"}\n" +
		"tools:\n" +
		"  - name: x_one\n    description: one\n    type: http\n    url: \"https://example.com/a\"\n" +
		"    headers: {X: \"{{secret:MINE}}\"}\n    result: json\n    parameters: {type: object, properties: {}}\n" +
		"  - name: x_two\n    description: two\n    type: workflow\n    parameters: {type: object, properties: {}}\n" +
		"    steps:\n      - name: only\n        type: http\n        url: \"https://example.com/b\"\n        auth: it\n        result: json\n" +
		"  - name: x_three\n    description: three\n    type: http\n    url: \"https://example.com/c\"\n" +
		"    result: json\n    parameters: {type: object, properties: {}}\n---\n"
	skill, err := Parse([]byte(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for name, want := range map[string][]string{
		"x_one":   {"MINE"},
		"x_two":   {"OURS"},
		"x_three": nil,
	} {
		got := skill.SecretsFor(name)
		if len(got) != len(want) || (len(want) == 1 && got[0] != want[0]) {
			t.Fatalf("%s asks for %v, want %v", name, got, want)
		}
	}
}

// A picture is kept beside the answer, not written into it.
//
// A model reads a tool's answer as text. A JPEG turned into text is a few
// hundred thousand characters that say nothing about what is in the
// picture, so a step that asks for an image hands the bytes to the caller
// and leaves a line in the answer saying so.
func TestAPictureIsKeptBesideTheAnswer(t *testing.T) {
	// The first two bytes of a JPEG, and enough after them to be a file.
	picture := append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, make([]byte, 4096)...)
	service := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "image/jpeg; charset=binary")
		_, _ = writer.Write(picture)
	}))
	defer service.Close()

	body := "---\nname: cameras\ndescription: cameras\ntools:\n" +
		"  - name: look\n    description: look at a camera\n    type: http\n" +
		"    url: \"" + service.URL + "/snapshot\"\n    result: image\n" +
		"    parameters: {type: object, properties: {}}\n---\n"
	skill, err := Parse([]byte(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	running := &Running{Client: service.Client()}
	answer, err := skill.Run(context.Background(), "look", nil, running)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(running.Files) != 1 {
		t.Fatalf("the picture is handed to the caller: %v", running.Files)
	}
	kept := running.Files[0]
	if !kept.Look {
		t.Fatalf("a picture is one a model can be shown")
	}
	// The media type without the parameters the service wrote after it,
	// because that is what a provider is given.
	if kept.MediaType != "image/jpeg" || len(kept.Data) != len(picture) {
		t.Fatalf("whole, and named by what it is: %s, %d bytes", kept.MediaType, len(kept.Data))
	}
	if kept.Step != "look" {
		t.Fatalf("named after the step that fetched it: %q", kept.Step)
	}
	// And nothing in the answer carries the bytes.
	written, err := json.Marshal(answer)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(written) > 300 {
		t.Fatalf("the answer is a line about a picture, not a picture: %d bytes", len(written))
	}
	if answer["content_type"] != "image/jpeg" || answer["bytes"] != len(picture) {
		t.Fatalf("it says what was fetched: %v", answer)
	}
	if strings.Contains(string(written), "\xff\xd8") {
		t.Fatalf("the bytes stayed out of the answer: %s", written)
	}
}

// Half a picture is not a picture, and something that is not one at all is
// not passed off as one.
func TestAPictureThatIsNotOneIsRefused(t *testing.T) {
	for _, each := range []struct {
		what        string
		contentType string
		size        int
		maxBytes    int
		says        string
	}{
		{"a page where a picture was asked for", "text/html", 32, 0, "answered with text/html"},
		{"a service that says nothing", "", 32, 0, "answered with application/octet-stream"},
		{"a picture longer than the step reads", "image/png", 4096, 512, "longer than the"},
		{"an empty picture", "image/png", 0, 0, "empty picture"},
	} {
		t.Run(each.what, func(t *testing.T) {
			service := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if each.contentType != "" {
					writer.Header().Set("Content-Type", each.contentType)
				} else {
					// Go writes one from the bytes unless it is told not to.
					writer.Header()["Content-Type"] = nil
				}
				_, _ = writer.Write(make([]byte, each.size))
			}))
			defer service.Close()

			body := "---\nname: cameras\ndescription: cameras\ntools:\n" +
				"  - name: look\n    description: look\n    type: http\n" +
				"    url: \"" + service.URL + "/snapshot\"\n    result: image\n"
			if each.maxBytes > 0 {
				body += "    maxBytes: " + fmt.Sprint(each.maxBytes) + "\n"
			}
			body += "    parameters: {type: object, properties: {}}\n---\n"
			skill, err := Parse([]byte(body))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			running := &Running{Client: service.Client()}
			_, err = skill.Run(context.Background(), "look", nil, running)
			if err == nil || !strings.Contains(err.Error(), each.says) {
				t.Fatalf("refused, saying why: %v", err)
			}
			if len(running.Files) != 0 {
				t.Fatalf("and nothing was kept: %v", running.Files)
			}
		})
	}
}

// A result nobody can make anything of is refused when the skill is read,
// not when somebody calls it.
func TestAnUnknownResultIsRefusedWhenRead(t *testing.T) {
	body := "---\nname: cameras\ndescription: cameras\ntools:\n" +
		"  - name: look\n    description: look\n    type: http\n    url: https://example.com/x\n" +
		"    result: video\n    parameters: {type: object, properties: {}}\n---\n"
	if _, err := Parse([]byte(body)); err == nil || !strings.Contains(err.Error(), "json, text, image or file") {
		t.Fatalf("read and refused: %v", err)
	}
}

// A step that signs in is followed by steps that are signed in.
//
// Plenty of equipment has no other way in: a name and a password at one
// address, and everything else answered only to the session it hands back.
// The cookies live for the one run, so a session never outlives the call
// that opened it.
func TestSigningInCarriesIntoTheStepsAfterIt(t *testing.T) {
	var seen []string
	service := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/login" {
			http.SetCookie(writer, &http.Cookie{Name: "TOKEN", Value: "a-session", Path: "/"})
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"username":"someone"}`))
			return
		}
		cookie, err := request.Cookie("TOKEN")
		if err != nil {
			writer.WriteHeader(http.StatusUnauthorized)
			_, _ = writer.Write([]byte(`{"error":"no session"}`))
			return
		}
		seen = append(seen, cookie.Value)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"cameras":2}`))
	}))
	defer service.Close()

	body := "---\nname: console\ndescription: a console\ntools:\n" +
		"  - name: cameras\n    description: the cameras\n    type: workflow\n" +
		"    parameters: {type: object, properties: {}}\n" +
		"    steps:\n" +
		"      - name: sign_in\n        type: http\n        method: POST\n        url: \"" + service.URL + "/login\"\n        result: json\n        select: {who: username}\n" +
		"      - name: cameras\n        type: http\n        url: \"" + service.URL + "/cameras\"\n        result: json\n        select: {count: cameras}\n" +
		"---\n"
	skill, err := Parse([]byte(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// The guarded client is the one that carries the jar, so this is the
	// path a skill actually takes rather than a client handed in.
	running := &Running{Allowance: safefetch.ParseAllowance([]string{"127.0.0.1"})}
	answer, err := skill.Run(context.Background(), "cameras", nil, running)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	cameras, _ := answer["cameras"].(map[string]any)
	if cameras["count"] != float64(2) {
		t.Fatalf("the second step was signed in: %v", answer)
	}
	if len(seen) != 1 || seen[0] != "a-session" {
		t.Fatalf("it sent the session the first step was given: %v", seen)
	}

	// A run of its own starts with an empty jar: one call's session is
	// never another's.
	fresh := &Running{Allowance: safefetch.ParseAllowance([]string{"127.0.0.1"})}
	if fresh.cookies() == running.cookies() {
		t.Fatalf("each run has a jar of its own")
	}
}

// Doubled braces are a literal pair, so a skill can send a payload that is
// itself written in braces.
//
// Home Assistant's templates, a dashboard's queries, a webhook's body: all
// of them are {{ ... }}, and without an escape none of them could be sent
// -- the skill would read them as values it was supposed to provide and
// refuse the file for naming values it had never heard of.
func TestDoubledBracesAreALiteralPair(t *testing.T) {
	var sent string
	service := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		sent = string(body)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"rendered":"light.kitchen=on"}`))
	}))
	defer service.Close()

	body := "---\nname: house\ndescription: a house\ntools:\n" +
		"  - name: render\n    description: render\n    type: http\n    method: POST\n" +
		"    url: \"" + service.URL + "/template\"\n" +
		"    body:\n      template: \"{% for s in states.{{domain}} %}{{{{ s.entity_id }}}}={{{{ s.state }}}}{% endfor %}\"\n" +
		"    result: json\n    select: {text: rendered}\n" +
		"    parameters: {type: object, properties: {domain: {type: string}}, required: [domain]}\n---\n"
	skill, err := Parse([]byte(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	answer, err := skill.Run(context.Background(), "render", map[string]any{"domain": "light"}, &Running{Client: service.Client()})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if answer["text"] != "light.kitchen=on" {
		t.Fatalf("what the service rendered: %v", answer)
	}
	// The skill's own reference was filled in; the doubled ones arrived as
	// the single pair the service expects.
	var carried struct {
		Template string `json:"template"`
	}
	if err := json.Unmarshal([]byte(sent), &carried); err != nil {
		t.Fatalf("what was sent is not json: %s", sent)
	}
	if carried.Template != "{% for s in states.light %}{{ s.entity_id }}={{ s.state }}{% endfor %}" {
		t.Fatalf("the braces arrived as one pair: %q", carried.Template)
	}
}

// And nothing can arrive already wearing the disguise the escape uses.
func TestAFileCarryingAZeroByteIsRefused(t *testing.T) {
	body := "---\nname: house\ndescription: a\x00house\ntools:\n" +
		"  - name: x\n    description: x\n    type: http\n    url: https://example.com/\n" +
		"    parameters: {type: object, properties: {}}\n---\n"
	if _, err := Parse([]byte(body)); err == nil || !strings.Contains(err.Error(), "zero byte") {
		t.Fatalf("refused: %v", err)
	}
}

// A value cannot forge the escape and write braces of its own.
//
// The escape turns the hidden byte back into {{ on the way out, and values
// are written in before that happens. So a parameter carrying that byte
// would arrive at the service as a brace -- and at a service whose payloads
// are templates, a brace is not punctuation but a program: a skill
// rendering a fixed template with one word from the caller in it would run
// whatever the word said.
func TestAValueCannotForgeTheEscape(t *testing.T) {
	var sent string
	service := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		sent = string(body)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"ok":true}`))
	}))
	defer service.Close()

	body := "---\nname: house\ndescription: a house\ntools:\n" +
		"  - name: render\n    description: render\n    type: http\n    method: POST\n" +
		"    url: \"" + service.URL + "/template\"\n" +
		"    body:\n      template: \"{% for s in states.light %}{{words}}{% endfor %}\"\n" +
		"    result: json\n" +
		"    parameters: {type: object, properties: {words: {type: string}}, required: [words]}\n---\n"
	skill, err := Parse([]byte(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := skill.Run(context.Background(), "render",
		map[string]any{"words": "\x00{ states.persons \x00}"}, &Running{Client: service.Client()}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.Contains(sent, "{{") || strings.Contains(sent, "}}") {
		t.Fatalf("a value forged the escape: %s", sent)
	}
	// And the same value in an address cannot do it either.
	if !strings.Contains(sent, "states.persons") {
		t.Fatalf("the words themselves still arrive, without their braces: %s", sent)
	}
}

// A sign-in step's token does not go back to the model.
//
// A workflow that signs in first selects a token and puts it in the header of
// the step after it. Every step's answer was going back as the tool's answer
// -- "so that the model sees the working" -- which put that token in the
// provider's request, the stored conversation, and the run on the dashboard.
// The step says quiet, the step after it still signs its request with the
// token, and the answer carries what was asked for and not the credential.
func TestAQuietStepKeepsItsTokenOutOfTheAnswer(t *testing.T) {
	const token = "tok-live-do-not-show-a-model"
	var signed []string
	service := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(request.URL.Path, "/login") {
			_, _ = writer.Write([]byte(`{"access_token":"` + token + `"}`))
			return
		}
		signed = append(signed, request.Header.Get("Authorization"))
		_, _ = writer.Write([]byte(`{"rooms":[{"name":"Kitchen"}]}`))
	}))
	defer service.Close()

	body := "---\nname: house\ndescription: the house\ntools:\n" +
		"  - name: rooms\n    description: the rooms\n    type: workflow\n" +
		"    parameters: {type: object, properties: {}}\n" +
		"    steps:\n" +
		"      - name: sign_in\n        type: http\n        method: POST\n        url: \"" + service.URL + "/login\"\n        result: json\n        quiet: true\n        select: {token: access_token}\n" +
		"      - name: list\n        type: http\n        url: \"" + service.URL + "/rooms\"\n        result: json\n        headers: {Authorization: \"Bearer {{steps.sign_in.token}}\"}\n        select: {first: \"rooms.0.name\"}\n" +
		"---\n"
	skill, err := Parse([]byte(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	answer, err := skill.Run(context.Background(), "rooms", nil, &Running{Client: service.Client()})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(signed) != 1 || signed[0] != "Bearer "+token {
		t.Fatalf("the step after it still signed with the token: %v", signed)
	}
	// One step left to show, so its answer is the answer.
	if answer["first"] != "Kitchen" {
		t.Fatalf("what was asked for is in the answer: %v", answer)
	}
	if _, found := answer["sign_in"]; found {
		t.Fatalf("the quiet step is not in the answer: %v", answer)
	}
	if written := fmt.Sprintf("%v", answer); strings.Contains(written, token) {
		t.Fatalf("the token is nowhere in the answer: %s", written)
	}
}

// A skill that has not been republished still does not hand over its token.
//
// quiet is the way to say it, and the skills in the registry will say it. An
// installed one keeps running as it was written, though, so a field with a
// credential's name is kept back from the answer whether or not the step
// asked -- while the steps after it go on using the real value.
func TestACredentialFieldIsKeptBackFromAnUnchangedSkill(t *testing.T) {
	const token = "tok-live-still-secret"
	var signed []string
	service := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(request.URL.Path, "/login") {
			_, _ = writer.Write([]byte(`{"access_token":"` + token + `","expires":3600}`))
			return
		}
		signed = append(signed, request.Header.Get("Authorization"))
		_, _ = writer.Write([]byte(`{"rooms":[{"name":"Kitchen"}]}`))
	}))
	defer service.Close()

	// No quiet on the sign-in step: the skill as it was written before.
	body := "---\nname: house\ndescription: the house\ntools:\n" +
		"  - name: rooms\n    description: the rooms\n    type: workflow\n" +
		"    parameters: {type: object, properties: {}}\n" +
		"    steps:\n" +
		"      - name: sign_in\n        type: http\n        method: POST\n        url: \"" + service.URL + "/login\"\n        result: json\n        select: {token: access_token, expires: expires}\n" +
		"      - name: list\n        type: http\n        url: \"" + service.URL + "/rooms\"\n        result: json\n        headers: {Authorization: \"Bearer {{steps.sign_in.token}}\"}\n        select: {first: \"rooms.0.name\"}\n" +
		"---\n"
	skill, err := Parse([]byte(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	answer, err := skill.Run(context.Background(), "rooms", nil, &Running{Client: service.Client()})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(signed) != 1 || signed[0] != "Bearer "+token {
		t.Fatalf("the real token still signed the request after it: %v", signed)
	}
	if written := fmt.Sprintf("%v", answer); strings.Contains(written, token) {
		t.Fatalf("the token is nowhere in the answer: %s", written)
	}
	// The step is still shown, and what is not a credential is still in it.
	signIn, _ := answer["sign_in"].(map[string]any)
	if signIn == nil || signIn["token"] != "(kept back)" {
		t.Fatalf("the field is kept back rather than the step hidden: %v", answer)
	}
	if signIn["expires"] != float64(3600) {
		t.Fatalf("the rest of the step is untouched: %v", signIn)
	}
}

// A client given from outside still keeps the steps' cookies.
//
// A skill whose requests go through the person's computer is given that
// computer's client, and a skill that signs in in one step and fetches in the
// next has to send the sign-in's cookie with the fetch.
func TestAGivenClientKeepsTheStepsCookies(t *testing.T) {
	running := &Running{Client: &http.Client{}}
	first := running.client(time.Second, "example.com")
	second := running.client(time.Second, "example.com")
	if first.Jar == nil || first.Jar != second.Jar {
		t.Error("the steps do not share a cookie jar")
	}
	if running.Client.Jar != nil {
		t.Error("the client given was changed rather than copied")
	}
}
