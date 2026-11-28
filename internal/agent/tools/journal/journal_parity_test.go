package journal_test

import (
	"context"
	"encoding/json"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/all"
	"github.com/ziyan/teanode/internal/api/v1api/apigraph"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/models"
)

// recording is the person's operations that keep the name of every operation
// it was asked to run, and answer nothing.
type recording struct {
	lock       sync.Mutex
	operations map[string]bool
}

var operationName = regexp.MustCompile(`^\s*(?:query|mutation)[^{]*\{\s*(\w+)`)

func (self *recording) Permissions() *models.EffectivePermissions {
	return &models.EffectivePermissions{Everywhere: []models.Permission{models.PermissionJournalUse}}
}

func (self *recording) Execute(_ context.Context, document string, _ map[string]any, result any) error {
	self.lock.Lock()
	defer self.lock.Unlock()
	if match := operationName.FindStringSubmatch(document); match != nil {
		self.operations[match[1]] = true
	}
	if result == nil {
		return nil
	}
	return json.Unmarshal([]byte(`{}`), result)
}

type personsTurn struct {
	tools.Run
	operations tools.Operations
}

func (self *personsTurn) Headless() bool                       { return false }
func (self *personsTurn) CanAsk() bool                         { return true }
func (self *personsTurn) Surface() string                      { return "" }
func (self *personsTurn) IsStartedByPerson() bool              { return true }
func (self *personsTurn) Operations() tools.Operations         { return self.operations }
func (self *personsTurn) Configuration() *config.Configuration { return config.Default() }
func (self *personsTurn) Owner() *models.User {
	return &models.User{ID: "u-ada", Username: "ada", Timezone: "UTC"}
}

// Every operation of the journal on the API that the person has is done by an
// action of the agent's tool, or is deliberately the person's alone. The
// tool is run, action by action, in a turn the person started, and the
// operations it sends are the ones that are counted: so an action cannot be
// named in the tool and not reach the API, nor reach one of the operations
// below.
func TestEveryJournalOperationIsAnActionOrThePersonsAlone(t *testing.T) {
	operations := map[string]bool{}
	for _, kind := range []reflect.Type{reflect.TypeFor[apigraph.JournalQuery](), reflect.TypeFor[apigraph.JournalMutation]()} {
		for index := 0; index < kind.NumMethod(); index++ {
			operations[kind.Method(index).Name] = true
		}
	}

	// The operations the tool has no action for, and why. The agent gets what
	// the person gets, so these are few.
	personsAlone := map[string]string{
		"ReadJournalSettings":   "update_settings answers with the settings it changed, and nothing else reads them",
		"WriteJournalNarrative": "a model call the person did not ask for; a correction writes it again",
	}

	recorded := &recording{operations: map[string]bool{}}
	run := &personsTurn{operations: recorded}
	// The calls to make: each action, and add_words once more for a period.
	arguments := []struct{ action, arguments string }{
		{"day", `{}`}, {"period", `{"period_kind":"week"}`}, {"days", `{}`}, {"search", `{"query":"harbor"}`},
		{"add_words", `{"text":"More."}`}, {"add_words", `{"text":"More.","period_kind":"week"}`},
		{"set_mood", `{"mood":"good"}`}, {"touch_up", `{}`},
		{"correct", `{"correction_text":"It was Ben."}`}, {"pin", `{"record":"mail:m1"}`},
		{"stop_touch_up", `{}`}, {"remove_correction", `{"correction_index":0}`}, {"delete_narrative", `{}`},
		{"remove_pin", `{"link_id":"l1"}`}, {"update_settings", `{"is_weather_on":false}`}, {"erase", `{}`},
	}
	merged := tools.Build().Get("journal")
	if merged == nil {
		t.Fatal("no journal tool")
	}
	actions := make([]string, 0, len(arguments))
	for _, call := range arguments {
		actions = append(actions, call.action)
		var fields map[string]any
		if err := json.Unmarshal([]byte(call.arguments), &fields); err != nil {
			t.Fatal(err)
		}
		fields["action"] = call.action
		encoded, _ := json.Marshal(fields)
		// Errors are expected: the answers are empty. Only the operations
		// that were sent matter here.
		_, _ = merged.Run(tools.WithRun(context.Background(), run), &tools.Call{ID: "call", Arguments: encoded})
	}
	sort.Strings(actions)

	for operation := range operations {
		_, isAlone := personsAlone[operation]
		switch {
		case isAlone && recorded.operations[operation]:
			t.Errorf("%s is the person's alone, and the tool sent it", operation)
		case !isAlone && !recorded.operations[operation]:
			t.Errorf("%s has no action in the journal tool (%s), and is not listed as the person's alone", operation, strings.Join(actions, ", "))
		}
	}
	for operation := range recorded.operations {
		if !operations[operation] && operation != "ReadHealthSettings" {
			t.Errorf("the tool sent %s, which is not an operation of the journal", operation)
		}
	}
	for operation, reason := range personsAlone {
		if !operations[operation] {
			t.Errorf("the table lists %s (%s), which is not an operation of the journal", operation, reason)
		}
	}
}
