package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
			_, _ = writer.Write([]byte(`[{"lat":"34.07","lon":"-84.29","display_name":"Alpharetta"}]`))
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
	answer, err := skill.Run(context.Background(), "get_weather", map[string]any{"location": "Alpharetta, GA"}, &Running{Client: service.Client()})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	forecast, _ := answer["forecast"].(map[string]any)
	if forecast["summary"] != "Partly cloudy" || forecast["degrees"] != float64(68) {
		t.Fatalf("the second step selected from what it fetched: %v", answer)
	}
	if len(asked) != 2 || !strings.Contains(asked[0], "Alpharetta") || !strings.Contains(asked[1], "34.07,-84.29") {
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
	mine := skill.PersonalSecrets()
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
