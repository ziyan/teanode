package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// refused are the published skills this server will not install, and why.
// They are kept as fixtures so that a change to what the registry
// publishes shows up here rather than on somebody's server.
// refused are published skills this server will not install, and why. It
// is empty: the one that was here took the address of a camera system as a
// parameter of the tool and sent the operator's token there, and the
// registry has since taken the host from a secret instead. The shape is
// kept because a fixture that starts failing is how the next one is
// noticed; TestACredentialCannotBeSentToAChosenHost holds the rule itself.
var refused = map[string]string{}

// Every other skill the real registry publishes must parse, because an
// entry that verifies and then cannot be read is a release that ships a
// broken install.
func TestTheRealSkillsParse(t *testing.T) {
	files, err := filepath.Glob("testdata/*.md")
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
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
			t.Errorf("%s: %v", file, err)
			continue
		}
		if len(skill.Tools) == 0 {
			t.Errorf("%s declares no tools", file)
		}
		for _, tool := range skill.Tools {
			if tool.Description == "" {
				t.Errorf("%s: %s has no description", file, tool.Name)
			}
		}
	}
}

func TestTheShapesEachSkillUses(t *testing.T) {
	content, _ := os.ReadFile("testdata/git.md")
	git, err := Parse(content)
	if err != nil {
		t.Fatalf("git: %v", err)
	}
	if git.Tools[0].Type != KindShell || len(git.Tools[0].Command) == 0 {
		t.Fatalf("git declares shell tools: %+v", git.Tools[0])
	}

	content, _ = os.ReadFile("testdata/weather.md")
	weather, err := Parse(content)
	if err != nil {
		t.Fatalf("weather: %v", err)
	}
	if weather.Tools[0].Type != KindWorkflow || len(weather.Tools[0].Steps) < 2 {
		t.Fatalf("weather is a workflow of steps: %+v", weather.Tools[0])
	}
	if weather.Tools[0].Steps[1].Select["forecast"] == "" {
		t.Fatalf("its steps select from what they fetched: %+v", weather.Tools[0].Steps[1])
	}

	// Profiles, secrets and routing, on a skill shaped like the published
	// one but with the camera system's address settled by a secret rather
	// than by whoever calls it.
	routed := "---\nname: cameras\ndescription: cameras\nsecrets:\n  - key: CAMERA_TOKEN\n  - key: CAMERA_HOST\n" +
		"authenticationProfiles:\n  cameras: {type: bearer, token: \"{{secret:CAMERA_TOKEN}}\"}\n" +
		"tools:\n  - name: camera_ops\n    description: ops\n    type: workflow\n    actionField: action\n" +
		"    parameters: {type: object, properties: {action: {type: string}, cameraId: {type: string}}, required: [action]}\n" +
		"    actions:\n" +
		"      list: [{name: list, type: http, url: \"https://{{secret:CAMERA_HOST}}/api/cameras\", auth: cameras, result: json}]\n" +
		"      get: [{name: get, type: http, url: \"https://{{secret:CAMERA_HOST}}/api/cameras/{{cameraId}}\", auth: cameras, result: json}]\n---\n"
	cameras, err := Parse([]byte(routed))
	if err != nil {
		t.Fatalf("cameras: %v", err)
	}
	if cameras.Profiles["cameras"] == nil || cameras.Profiles["cameras"].Type != "bearer" {
		t.Fatalf("it shares one authentication: %+v", cameras.Profiles)
	}
	if len(cameras.Secrets) != 2 {
		t.Fatalf("it declares the secrets it needs: %+v", cameras.Secrets)
	}
	if cameras.Tools[0].ActionField != "action" || len(cameras.Tools[0].Actions) != 2 {
		t.Fatalf("it routes one tool on a parameter: %+v", cameras.Tools[0])
	}
}

func TestWhatIsRefused(t *testing.T) {
	for name, body := range map[string]string{
		"no header":                 "name: x\n",
		"unclosed header":           "---\nname: x\n",
		"no name":                   "---\ndescription: d\ntools: [{name: t, description: d, type: shell, command: [ls], parameters: {type: object}}]\n---\n",
		"name with spaces":          "---\nname: my skill\ndescription: d\ntools: [{name: t, description: d, type: shell, command: [ls], parameters: {type: object}}]\n---\n",
		"no tools":                  "---\nname: x\ndescription: d\ntools: []\n---\n",
		"tool with no parameters":   "---\nname: x\ndescription: d\ntools: [{name: t, description: d, type: shell, command: [ls]}]\n---\n",
		"tool with no description":  "---\nname: x\ndescription: d\ntools: [{name: t, type: shell, command: [ls], parameters: {type: object}}]\n---\n",
		"unknown tool type":         "---\nname: x\ndescription: d\ntools: [{name: t, description: d, type: magic, parameters: {type: object}}]\n---\n",
		"shell with no command":     "---\nname: x\ndescription: d\ntools: [{name: t, description: d, type: shell, parameters: {type: object}}]\n---\n",
		"http with no url":          "---\nname: x\ndescription: d\ntools: [{name: t, description: d, type: http, parameters: {type: object}}]\n---\n",
		"workflow with no steps":    "---\nname: x\ndescription: d\ntools: [{name: t, description: d, type: workflow, parameters: {type: object}}]\n---\n",
		"unknown parameter":         "---\nname: x\ndescription: d\ntools: [{name: t, description: d, type: shell, command: [echo, \"{{nowhere}}\"], parameters: {type: object, properties: {}}}]\n---\n",
		"step that never ran":       "---\nname: x\ndescription: d\ntools: [{name: t, description: d, type: workflow, parameters: {type: object}, steps: [{name: one, type: http, url: \"https://e.example/{{steps.two.value}}\"}]}]\n---\n",
		"undeclared secret":         "---\nname: x\ndescription: d\ntools: [{name: t, description: d, type: http, url: \"https://e.example\", headers: {Authorization: \"{{secret:NOPE}}\"}, parameters: {type: object}}]\n---\n",
		"undeclared auth":           "---\nname: x\ndescription: d\ntools: [{name: t, description: d, type: http, url: \"https://e.example\", auth: nowhere, parameters: {type: object}}]\n---\n",
		"two tools one name":        "---\nname: x\ndescription: d\ntools: [{name: t, description: d, type: shell, command: [ls], parameters: {type: object}}, {name: t, description: d, type: shell, command: [ls], parameters: {type: object}}]\n---\n",
		"select without json":       "---\nname: x\ndescription: d\ntools: [{name: t, description: d, type: http, url: \"https://e.example\", select: {a: b}, parameters: {type: object}}]\n---\n",
		"routes on a non-parameter": "---\nname: x\ndescription: d\ntools: [{name: t, description: d, type: workflow, actionField: action, actions: {a: [{name: s, type: http, url: \"https://e.example\"}]}, parameters: {type: object, properties: {}}}]\n---\n",
	} {
		if _, err := Parse([]byte(body)); err == nil {
			t.Errorf("%s: should be refused", name)
		}
	}
}

// A reference to an earlier step is fine; the check is about order.
func TestAStepMayUseAnEarlierOne(t *testing.T) {
	body := "---\nname: x\ndescription: d\ntools: [{name: t, description: d, type: workflow, parameters: {type: object, properties: {q: {type: string}}}, steps: [" +
		"{name: one, type: http, url: \"https://e.example/{{q}}\", result: json, select: {value: a.b}}," +
		"{name: two, type: http, url: \"https://e.example/{{steps.one.value}}\"}]}]\n---\n"
	if _, err := Parse([]byte(body)); err != nil {
		t.Fatalf("a later step may use an earlier one: %v", err)
	}
}

func TestTheProseIsKept(t *testing.T) {
	skill, err := Parse([]byte("---\nname: x\ndescription: d\ntools: [{name: t, description: d, type: shell, command: [ls], parameters: {type: object}}]\n---\n\nWhat this is for.\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !strings.Contains(skill.Prose, "What this is for") {
		t.Fatalf("the prose is kept for a person to read: %q", skill.Prose)
	}
}
