package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// permissioned is a run whose person holds exactly what it is given, for the
// question a merged tool has to answer that a single tool never did: the
// catalog admitted this tool on the union of its actions, so which of them
// may this person actually take?
type permissioned struct {
	fakeRun
	permissions *models.EffectivePermissions
}

func (self *permissioned) Operations() Operations { return self }
func (self *permissioned) Execute(context.Context, string, map[string]any, any) error {
	return nil
}
func (self *permissioned) Permissions() *models.EffectivePermissions { return self.permissions }

func heldBy(permissions ...models.Permission) *models.EffectivePermissions {
	return &models.EffectivePermissions{Everywhere: permissions}
}

// the tools under test: a read, a write and a destructive one over the same
// thing, each asking for a different permission.
func exampleActions(ran *string) []Action {
	return []Action{
		{Name: "list", Tool: &Tool{
			Name: "thing_list", Risk: RiskRead,
			Description: "Everything of that kind.",
			Permissions: []models.Permission{models.PermissionUserManage, models.PermissionGroupManage},
			Parameters:  Object(map[string]any{"limit": IntegerProperty("how many")}),
			Run: func(context.Context, *Call) (*Result, error) {
				*ran = "list"
				return TextResult("listed"), nil
			},
		}},
		{Name: "add", Tool: &Tool{
			Name: "thing_add", Risk: RiskWrite,
			Description: "Add one.",
			Permissions: []models.Permission{models.PermissionUserManage},
			Parameters:  Object(map[string]any{"name": StringProperty("for add: what to call it")}, "name"),
			Run: func(context.Context, *Call) (*Result, error) {
				*ran = "add"
				return TextResult("added"), nil
			},
		}},
		{Name: "remove", Tool: &Tool{
			Name: "thing_remove", Risk: RiskDestructive,
			Description: "Take one away for good.",
			Permissions: []models.Permission{models.PermissionUserManage},
			Parameters:  Object(map[string]any{"name": StringProperty("for remove: which one")}, "name"),
			Run: func(context.Context, *Call) (*Result, error) {
				*ran = "remove"
				return TextResult("removed"), nil
			},
		}},
	}
}

func exampleMerged(ran *string) *Tool {
	return Merge(MergedTool{
		Name: "thing", Family: FamilyPeople,
		Description: "The things.",
		Actions:     exampleActions(ran),
	})
}

// What the model is shown: one name, one action to choose, and every field
// any of the actions takes.
func TestAMergedToolSaysWhatEachActionDoes(t *testing.T) {
	t.Parallel()
	ran := ""
	merged := exampleMerged(&ran)

	for _, want := range []string{"The things.", "list — Everything of that kind.", "remove — Take one away for good."} {
		if !strings.Contains(merged.Description, want) {
			t.Errorf("the description should carry %q:\n%s", want, merged.Description)
		}
	}
	properties, _ := merged.Parameters["properties"].(map[string]any)
	for _, field := range []string{"action", "limit", "name"} {
		if _, ok := properties[field]; !ok {
			t.Errorf("the parameters should carry %q: %v", field, properties)
		}
	}
	action, _ := properties["action"].(map[string]any)
	values, _ := action["enum"].([]string)
	if len(values) != 3 || values[0] != "list" {
		t.Fatalf("the actions are the enumeration: %v", values)
	}
	// Two actions describe "name" differently; both descriptions survive,
	// because a model reading one field has to know which action meant it.
	name, _ := properties["name"].(map[string]any)
	if text, _ := name["description"].(string); !strings.Contains(text, "for add") || !strings.Contains(text, "for remove") {
		t.Fatalf("both descriptions of the shared field: %q", text)
	}
	if required, _ := merged.Parameters["required"].([]string); len(required) != 1 || required[0] != "action" {
		t.Fatalf("only the action is required of the tool itself: %v", merged.Parameters["required"])
	}
}

// The floor is the gentlest action, so a read-only run may still be offered
// the tool; the call is judged by the action it names.
func TestAMergedToolIsPricedByTheActionInHand(t *testing.T) {
	t.Parallel()
	ran := ""
	merged := exampleMerged(&ran)

	if merged.Risk != RiskRead {
		t.Fatalf("the floor is the gentlest of them: %s", merged.Risk)
	}
	if got := merged.RiskFor(json.RawMessage(`{"action":"list"}`)); got != RiskRead {
		t.Errorf("listing reads: %s", got)
	}
	if got := merged.RiskFor(json.RawMessage(`{"action":"add","name":"x"}`)); got != RiskWrite {
		t.Errorf("adding writes: %s", got)
	}
	if got := merged.RiskFor(json.RawMessage(`{"action":"remove","name":"x"}`)); got != RiskDestructive {
		t.Errorf("removing is destructive: %s", got)
	}

	// A call that says something the tool does not do, or says nothing,
	// takes the strictest class any action carries rather than the floor --
	// so that nothing walks past the question by being written badly, which
	// is the shape of SEC-48, where the gate and the act read different
	// bytes. (Arguments that are not JSON at all never reach the question:
	// the loop hands them to the tool, which refuses them, and the test
	// below is what proves nothing runs.)
	for _, arguments := range []string{`{"action":"burn"}`, `{}`, `{"name":"x"}`} {
		if got := merged.RiskFor(json.RawMessage(arguments)); got != RiskDestructive {
			t.Errorf("%q should cost the most, not %s", arguments, got)
		}
	}
	// And what a model actually writes badly: single quotes, which the
	// repair reads, so the gate sees the same action the tool will.
	if got := merged.RiskFor(json.RawMessage(`{'action':'remove','name':'x'}`)); got != RiskDestructive {
		t.Errorf("a sloppily written removal is still a removal: %s", got)
	}
}

// The catalog offers the tool to anybody one of its actions would admit.
// Each action then asks for its own.
func TestEachActionChecksItsOwnPermission(t *testing.T) {
	t.Parallel()
	ran := ""
	merged := exampleMerged(&ran)

	if len(merged.Permissions) != 2 {
		t.Fatalf("the union of what the actions ask: %v", merged.Permissions)
	}
	if !AllowedByPermissions(merged, heldBy(models.PermissionGroupManage)) {
		t.Fatal("somebody who may manage groups is offered the tool, because listing admits them")
	}

	// They may list, because that action takes group management too.
	run := &permissioned{fakeRun: fakeRun{owner: &models.User{Username: "alice"}, loaded: map[string]bool{}}, permissions: heldBy(models.PermissionGroupManage)}
	ctx := WithRun(context.Background(), run)
	if _, err := merged.Run(ctx, &Call{Arguments: json.RawMessage(`{"action":"list"}`)}); err != nil {
		t.Fatalf("listing: %s", err)
	}
	if ran != "list" {
		t.Fatalf("the action that ran: %q", ran)
	}
	// They may not add, because that one asks for something else, and the
	// refusal says so in words the model can pass on.
	ran = ""
	_, err := merged.Run(ctx, &Call{Arguments: json.RawMessage(`{"action":"add","name":"x"}`)})
	if err == nil || !strings.Contains(err.Error(), "may do") {
		t.Fatalf("adding should be refused: %v", err)
	}
	if ran != "" {
		t.Fatalf("and nothing should have run: %q", ran)
	}
}

// An action nobody declared is not a way in.
func TestAnUnknownActionDoesNothing(t *testing.T) {
	t.Parallel()
	ran := ""
	merged := exampleMerged(&ran)
	run := &permissioned{fakeRun: fakeRun{owner: &models.User{Username: "alice"}, loaded: map[string]bool{}}, permissions: heldBy(models.PermissionUserManage)}
	ctx := WithRun(context.Background(), run)

	for _, arguments := range []string{`{"action":"burn"}`, `{}`, `{"name":"x"}`, `{"action":`, ``, `null`} {
		if _, err := merged.Run(ctx, &Call{Arguments: json.RawMessage(arguments)}); err == nil {
			t.Errorf("%s should be refused", arguments)
		}
		if ran != "" {
			t.Fatalf("and nothing should have run: %q", ran)
		}
	}
}
