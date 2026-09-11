package tools

import (
	"context"
	"testing"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// fakeRun is the least a run can be, for a tool under test.
type fakeRun struct {
	owner  *models.User
	loaded map[string]bool
}

func (self *fakeRun) Owner() *models.User                     { return self.owner }
func (self *fakeRun) Agent() *models.Agent                    { return nil }
func (self *fakeRun) Conversation() *models.AgentConversation { return nil }
func (self *fakeRun) Operations() Operations                  { return nil }
func (self *fakeRun) Database() db.Database                   { return nil }
func (self *fakeRun) Configuration() *config.Configuration    { return nil }
func (self *fakeRun) Surface() string                         { return "cli" }
func (self *fakeRun) Headless() bool                          { return false }
func (self *fakeRun) ReadOnly() bool                          { return false }
func (self *fakeRun) Offered() []*Tool                        { return nil }
func (self *fakeRun) Loaded() map[string]bool                 { return self.loaded }
func (self *fakeRun) Load(name string)                        { self.loaded[name] = true }

// A run in the context is the run a tool gets back; none is an error a
// tool can report, never a nil to fall over.
func TestRunTravelsInTheContext(t *testing.T) {
	if _, err := RunFrom(context.Background()); err == nil {
		t.Fatal("a context without a run should say so")
	}
	run := &fakeRun{owner: &models.User{Username: "alice"}, loaded: map[string]bool{}}
	got, err := RunFrom(WithRun(context.Background(), run))
	if err != nil || got.Owner().Username != "alice" {
		t.Fatalf("got %v, %v", got, err)
	}
}

// Factories register from init and the catalog is what they make, in
// order; a factory is asked again for every catalog built.
func TestRegistryBuildsFromFactoriesInOrder(t *testing.T) {
	Reset()
	defer Reset()
	made := 0
	Register(func() []*Tool {
		made++
		return []*Tool{{Name: "second_tool", Family: FamilyGeneral}}
	})
	Register(func() []*Tool { return []*Tool{{Name: "third_tool", Family: FamilyGeneral}} })
	catalog := Build()
	names := []string{}
	for _, tool := range catalog.All() {
		names = append(names, tool.Name)
	}
	if len(names) != 2 || names[0] != "second_tool" || names[1] != "third_tool" {
		t.Fatalf("catalog %v", names)
	}
	Build()
	if made != 2 {
		t.Fatalf("the factory should be asked per build, was asked %d times", made)
	}
}

// The schema helpers and the argument decoder are what every tool
// package builds on; a model's mangled JSON is repaired on the way in.
func TestDecodeArgumentsRepairs(t *testing.T) {
	type arguments struct {
		Query string `json:"query"`
	}
	decoded, err := DecodeArguments[arguments](&Call{Arguments: []byte(`{"query": "plumber",}`)})
	if err != nil || decoded.Query != "plumber" {
		t.Fatalf("decoded %v, %v", decoded, err)
	}
	schema := Object(map[string]any{"query": StringProperty("the words")}, "query")
	if schema["required"].([]string)[0] != "query" {
		t.Fatal("required")
	}
}
