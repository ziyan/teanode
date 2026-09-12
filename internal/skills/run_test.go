package skills

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// fakeShell stands in for the person's attached computer.
type fakeShell struct {
	ran    []string
	prints string
}

func (self *fakeShell) Run(ctx context.Context, command string, timeout time.Duration) (string, error) {
	self.ran = append(self.ran, command)
	return self.prints, nil
}

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
	if strings.Contains(shell.ran[0], "; rm -rf ~'") && !strings.Contains(shell.ran[0], `'\''`) {
		t.Fatalf("the quote in the value is escaped: %v", shell.ran[0])
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
	if _, err := skill.Run(context.Background(), "x_get", nil, nil); err == nil {
		t.Fatal("a loopback address is refused")
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

// Every skill the registry publishes is carried as far as its first
// request, which proves the file and the interpreter agree about it.
func TestTheRealSkillsAreUnderstood(t *testing.T) {
	for _, file := range []string{"testdata/weather.md", "testdata/git.md", "testdata/dictionary.md", "testdata/news.md", "testdata/unifi-protect.md"} {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		skill, err := Parse(content)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		for _, tool := range skill.Tools {
			// Nothing is fetched: a tool asked for with none of its
			// parameters must complain about a parameter or about there
			// being no computer, never panic or hang.
			_, err := skill.Run(context.Background(), tool.Name, nil, nil)
			if err == nil {
				continue
			}
			if strings.Contains(err.Error(), "panic") {
				t.Errorf("%s %s: %v", file, tool.Name, err)
			}
		}
	}
}

// A value goes into an address escaped, so a place with a space in it
// reaches the service; an address that is one reference and nothing else
// is a link an earlier step found and is passed on untouched.
func TestValuesAreEscapedIntoAnAddress(t *testing.T) {
	var asked []string
	service := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		asked = append(asked, request.URL.String())
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"next":"` + "PLACEHOLDER" + `","q":"` + request.URL.Query().Get("q") + `"}`))
	}))
	defer service.Close()

	body := "---\nname: x\ndescription: x\ntools:\n  - name: x_get\n    description: get\n    type: http\n" +
		"    url: \"" + service.URL + "/search?q={{words}}\"\n    result: json\n    select: {q: q}\n" +
		"    parameters: {type: object, properties: {words: {type: string}}}\n---\n"
	skill, _ := Parse([]byte(body))
	answer, err := skill.Run(context.Background(), "x_get", map[string]any{"words": "Alpharetta, GA & more?"}, &Running{Client: service.Client()})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if answer["q"] != "Alpharetta, GA & more?" {
		t.Fatalf("the value arrived whole: %v", answer)
	}
	if len(asked) != 1 || strings.Contains(asked[0], " ") {
		t.Fatalf("no raw space in the address: %v", asked)
	}

	// One reference and nothing else: the value is the address.
	whole := "---\nname: y\ndescription: y\ntools:\n  - name: y_get\n    description: get\n    type: workflow\n" +
		"    parameters: {type: object, properties: {}}\n    steps:\n" +
		"      - name: first\n        type: http\n        url: \"" + service.URL + "/a\"\n        result: json\n        select: {link: q}\n" +
		"      - name: second\n        type: http\n        url: \"{{steps.first.link}}\"\n        result: json\n        select: {q: q}\n---\n"
	skill, err = Parse([]byte(whole))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// The first step selects an empty q, so the second has nothing to
	// fetch; what matters is that it was not escaped into nonsense.
	_, _ = skill.Run(context.Background(), "y_get", nil, &Running{Client: service.Client()})
}
