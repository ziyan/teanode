package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every skill the real registry publishes must parse, because a registry
// entry that verifies and then cannot be read is a release that ships a
// broken install.
func TestTheRealSkillsParse(t *testing.T) {
	files, err := filepath.Glob("testdata/*.md")
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	for _, file := range files {
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

	content, _ = os.ReadFile("testdata/unifi-protect.md")
	protect, err := Parse(content)
	if err != nil {
		t.Fatalf("unifi-protect: %v", err)
	}
	if protect.Profiles["protect"] == nil || protect.Profiles["protect"].Type != "bearer" {
		t.Fatalf("it shares one authentication: %+v", protect.Profiles)
	}
	if len(protect.Secrets) != 1 || protect.Secrets[0].Key != "UNIFI_PROTECT_TOKEN" {
		t.Fatalf("it declares the secret it needs: %+v", protect.Secrets)
	}
	if protect.Tools[0].ActionField != "action" || len(protect.Tools[0].Actions) == 0 {
		t.Fatalf("it routes one tool on a parameter: %+v", protect.Tools[0])
	}
}

// Each of these is a way a skill could fetch or run the wrong thing, and
// each must be refused at parse rather than found out on first use.
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
