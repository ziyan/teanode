package apigraph

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

const inventedSourceType = `---
name: invented-notes
description: notes kept by an invented tool
requires: [notetool]
settings:
  - {name: notebook, description: which notebook, type: string, pattern: "^[a-z]+$"}
  - {name: tags, description: only these tags, type: array, items: {type: string}, default: []}
containers:
  - command: [notetool, list, "{{settings.notebook}}"]
    parse: jsonl
    name: "{{item.name}}.jsonl"
    fields: {id: "{{item.id}}"}
records:
  - command: [notetool, read, "{{container.id}}"]
    parse: jsonl
    record: {id: "{{item.id}}", text: "{{item.text}}"}
---
For people.
`

const inventedFolderType = `---
name: invented-folder
description: a folder, read by the files reader
reader: files
settings:
  - {name: path, description: the folder, type: string}
  - {name: exclude, description: left unread, type: array, items: {type: string}, default: []}
  - {name: readEveryCheckout, description: every checkout, type: boolean, default: false}
---
`

// An operator adds a type of their own; a person adds a source of it, whose
// settings are checked against it; a type naming a built-in reader fills
// the fields that reader reads; and a type in use is not taken away.
func TestASourceIsAddedOfAType(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	var owner *models.User
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if owner, err = tx.CreateUser(&models.User{Username: "type-owner", Name: "Alice Example"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if _, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Name: "Bertie"}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
	})
	operator := &api.Principal{User: owner, Permissions: models.NewEffectivePermissions([]models.Grant{
		{Permission: models.PermissionAgentUse}, {Permission: models.PermissionServerManage},
	})}
	person := &api.Principal{User: owner, Permissions: models.NewEffectivePermissions([]models.Grant{
		{Permission: models.PermissionAgentUse},
	})}
	resolver := &graph{database: database}
	as := func(principal *api.Principal, run func(ctx context.Context)) {
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
			run(api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), principal), tx))
		})
	}

	as(person, func(ctx context.Context) {
		if _, err := resolver.AddLocalAgentSourceType(ctx, AddLocalAgentSourceTypeArguments{Content: inventedSourceType}); err == nil {
			t.Errorf("a person without server:manage added a type")
		}
	})
	as(operator, func(ctx context.Context) {
		if _, err := resolver.AddLocalAgentSourceType(ctx, AddLocalAgentSourceTypeArguments{Content: "---\nname: broken\n---\n"}); err == nil {
			t.Errorf("a type that does not parse was added")
		}
		for _, content := range []string{inventedSourceType, inventedFolderType} {
			added, err := resolver.AddLocalAgentSourceType(ctx, AddLocalAgentSourceTypeArguments{Content: content})
			if err != nil {
				t.Fatalf("AddLocalAgentSourceType: %s", err)
			}
			if !added.IsLocal || !added.Readable || added.Publisher != "local" {
				t.Errorf("the added type is %+v", added)
			}
		}
	})

	var sourceId string
	as(person, func(ctx context.Context) {
		listed, err := resolver.ListAgentSourceTypes(ctx)
		if err != nil || len(listed) != 2 {
			t.Fatalf("ListAgentSourceTypes: %v, %s", listed, err)
		}
		notes := listed[1]
		if notes.Name != "invented-notes" || len(notes.Settings) != 2 || !notes.Settings[0].IsRequired || notes.Settings[1].IsRequired || string(notes.Settings[1].Default) != "[]" {
			t.Errorf("the type's settings are %+v %+v", notes.Settings[0], notes.Settings[1])
		}

		for what, arguments := range map[string]SaveAgentKnowledgeSourceArguments{
			"a type nobody installed":  {Type: "not-installed", Computer: "laptop", Settings: json.RawMessage(`{"notebook": "work"}`)},
			"a setting left out":       {Type: "invented-notes", Computer: "laptop", Settings: json.RawMessage(`{}`)},
			"a setting that is not":    {Type: "invented-notes", Computer: "laptop", Settings: json.RawMessage(`{"notebook": "work", "colour": "red"}`)},
			"a value off its pattern":  {Type: "invented-notes", Computer: "laptop", Settings: json.RawMessage(`{"notebook": "Work!"}`)},
			"no computer to run it on": {Type: "invented-notes", Settings: json.RawMessage(`{"notebook": "work"}`)},
		} {
			if _, err := resolver.SaveAgentKnowledgeSource(ctx, arguments); err == nil {
				t.Errorf("%s was accepted", what)
			}
		}

		saved, err := resolver.SaveAgentKnowledgeSource(ctx, SaveAgentKnowledgeSourceArguments{
			Type: "invented-notes", Computer: "laptop", Settings: json.RawMessage(`{"notebook": "work", "tags": "a, b"}`),
		})
		if err != nil {
			t.Fatalf("SaveAgentKnowledgeSource: %s", err)
		}
		sourceId = saved.ID
		if saved.Kind != models.SourceComputer || saved.Specification.Format != models.FormatTyped || saved.Specification.Type != "invented-notes" || saved.Name != "invented-notes" {
			t.Errorf("the source is %+v", saved)
		}
		if !sameJSON(saved.Specification.Settings, `{"notebook":"work","tags":["a","b"]}`) {
			t.Errorf("the settings kept are %s", saved.Specification.Settings)
		}

		// Changing one setting of it keeps its type; giving no settings
		// keeps the ones it had.
		renamed, err := resolver.SaveAgentKnowledgeSource(ctx, SaveAgentKnowledgeSourceArguments{SourceID: sourceId, Name: "work notes"})
		if err != nil || !sameJSON(renamed.Specification.Settings, `{"notebook":"work","tags":["a","b"]}`) {
			t.Errorf("a rename changed the settings: %v, %s", renamed, err)
		}

		folder, err := resolver.SaveAgentKnowledgeSource(ctx, SaveAgentKnowledgeSourceArguments{
			Type: "invented-folder", Computer: "laptop", Settings: json.RawMessage(`{"path": "~/projects", "exclude": ["*.log"], "readEveryCheckout": true}`),
		})
		if err != nil {
			t.Fatalf("SaveAgentKnowledgeSource: %s", err)
		}
		specification := folder.Specification
		if folder.Kind != models.SourceComputer || specification.Format != models.FormatFiles || specification.Path != "~/projects" || len(specification.Exclude) != 1 || !specification.ReadEveryCheckout {
			t.Errorf("a folder of a type is %+v", folder)
		}
	})

	as(operator, func(ctx context.Context) {
		if _, err := resolver.RemoveAgentSourceType(ctx, AgentSourceTypeArguments{Name: "invented-notes"}); !errors.Is(err, api.ErrInvalidArguments) {
			t.Errorf("a type in use was removed, or refused for the wrong reason: %v", err)
		}
	})
	as(person, func(ctx context.Context) {
		if _, err := resolver.DeleteAgentKnowledgeSource(ctx, DeleteAgentKnowledgeSourceArguments{SourceID: sourceId}); err != nil {
			t.Fatalf("DeleteAgentKnowledgeSource: %s", err)
		}
	})
	as(operator, func(ctx context.Context) {
		if removed, err := resolver.RemoveAgentSourceType(ctx, AgentSourceTypeArguments{Name: "invented-notes"}); err != nil || !removed {
			t.Errorf("a type nobody uses was not removed: %v", err)
		}
	})
}

func sameJSON(got json.RawMessage, want string) bool {
	var left, right any
	if json.Unmarshal(got, &left) != nil || json.Unmarshal([]byte(want), &right) != nil {
		return false
	}
	return reflect.DeepEqual(left, right)
}
