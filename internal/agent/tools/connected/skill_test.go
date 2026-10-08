package connected_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/connected"
	"github.com/ziyan/teanode/internal/models"
)

// skillOperations answers ListAgentSkills with skillCount installed skills
// of five tools each, every tool described at the length a real one is.
type skillOperations struct {
	skillCount int
}

func (self *skillOperations) Permissions() *models.EffectivePermissions {
	return models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionAgentUse}})
}

func (self *skillOperations) Execute(_ context.Context, _ string, _ map[string]any, response any) error {
	skills := make([]map[string]any, 0, self.skillCount)
	for skillNumber := 1; skillNumber <= self.skillCount; skillNumber++ {
		skillTools := make([]map[string]any, 0, 5)
		for toolNumber := 1; toolNumber <= 5; toolNumber++ {
			skillTools = append(skillTools, map[string]any{
				"name":        fmt.Sprintf("garden%d_tool%d", skillNumber, toolNumber),
				"description": strings.Repeat(fmt.Sprintf("Waters bed %d of garden %d on the schedule given, and says when it last rained. ", toolNumber, skillNumber), 4),
				"kind":        "http", "needsComputer": false,
			})
		}
		skills = append(skills, map[string]any{
			"name": fmt.Sprintf("garden%d", skillNumber), "description": "Looks after a garden's watering.", "version": "1.0.0",
			"publisher": "registry", "enabled": true, "readable": true, "scope": "", "secrets": []string{}, "personalSecrets": []string{},
			"tools": skillTools,
		})
	}
	encoded, err := json.Marshal(map[string]any{"ListAgentSkills": skills})
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, response)
}

type skillRun struct {
	tools.Run
	operations *skillOperations
}

func (self *skillRun) Operations() tools.Operations { return self.operations }

// A list of every skill names each one's tools without describing them,
// says how to read one skill's tools, and list with that skill's name
// gives its tools and what each does.
//
// A list described every tool of every skill, more than twenty thousand
// characters for a dozen skills, which a client cut short without a word.
func TestAListOfSkillsNamesTheirToolsAndOneSkillDescribesThem(t *testing.T) {
	var skill *tools.Tool
	for _, each := range tools.Build().All() {
		if each.Name == "skill" {
			skill = each
		}
	}
	if skill == nil {
		t.Fatal("skill is not registered")
	}
	ctx := tools.WithRun(context.Background(), &skillRun{operations: &skillOperations{skillCount: 12}})
	call := func(arguments string) string {
		t.Helper()
		result, err := skill.Run(ctx, &tools.Call{ID: "c1", Arguments: []byte(arguments)})
		if err != nil {
			t.Fatalf("%s: %s", arguments, err)
		}
		return result.Content
	}

	every := call(`{"action":"list"}`)
	t.Logf("a list of 12 skills: %d characters", len(every))
	if len(every) > 5000 {
		t.Errorf("a list of 12 skills is %d characters", len(every))
	}
	if !strings.Contains(every, `"name":"garden12_tool5","needsComputer":false}`) || strings.Contains(every, "Waters bed") {
		t.Fatalf("a list names every tool, says whether it needs a computer, and describes none: %s", every)
	}
	if !strings.Contains(every, "list with name gives one skill's tools and what each does") {
		t.Fatalf("and says how to read one skill's tools: %s", every)
	}

	one := call(`{"action":"list","name":"garden7"}`)
	if !strings.Contains(one, "Waters bed 3 of garden 7") || strings.Contains(one, "garden8") {
		t.Fatalf("list with a name describes that skill's tools alone: %s", one)
	}
	// One shape and one set of names in both answers.
	for _, answer := range []string{every, one} {
		for _, wanted := range []string{`"isEnabled":true`, `"isReadable":true`, `"publisher":"registry"`} {
			if !strings.Contains(answer, wanted) {
				t.Errorf("an answer without %s: %s", wanted, answer)
			}
		}
		if strings.Contains(answer, `"enabled"`) || strings.Contains(answer, `"readable"`) {
			t.Errorf("an answer names a field another way: %s", answer)
		}
	}
	if _, err := skill.Run(ctx, &tools.Call{ID: "c2", Arguments: []byte(`{"action":"list","name":"orchard"}`)}); err == nil {
		t.Fatalf("a name that is not installed is refused")
	}
}
