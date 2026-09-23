package knowledge_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// The agent finds the installed types and the settings each asks for,
// and adds a source of one, its settings checked; a type nobody installed
// is refused and nothing is added.
func TestTheAgentAddsASourceOfAType(t *testing.T) {
	run, database, closeDatabase := world(t, "carol")
	defer closeDatabase()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if _, err := tx.PutAgentSourceType(&models.AgentSourceType{Name: "invented-chat", Publisher: "local", IsLocal: true, Content: `---
name: invented-chat
description: an invented chat tool's channels
requires: [chattool]
settings:
  - {name: team, description: which team, type: string, pattern: "^[a-z]+$"}
containers:
  - command: [chattool, channels, "{{settings.team}}"]
    parse: jsonl
    name: "{{item.name}}.jsonl"
    fields: {id: "{{item.id}}"}
records:
  - command: [chattool, posts, "{{container.id}}"]
    parse: jsonl
    record: {id: "{{item.id}}", kind: chat, text: "{{item.text}}"}
---
`}); err != nil {
			t.Fatalf("PutAgentSourceType: %s", err)
		}
	})

	ctx := tools.WithRun(context.Background(), run)
	tool := find(t, "knowledge")
	call := func(arguments string) (string, error) {
		t.Helper()
		result, err := tool.Run(ctx, &tools.Call{ID: "c1", Arguments: []byte(arguments)})
		if err != nil {
			return "", err
		}
		return result.Content, nil
	}

	listed, err := call(`{"action":"types"}`)
	if err != nil || !strings.Contains(listed, "invented-chat") || !strings.Contains(listed, "setting team (string, required)") || !strings.Contains(listed, "chattool") {
		t.Fatalf("types: %q, %v", listed, err)
	}
	if _, err := call(`{"action":"add","type":"invented-chat","computer":"laptop","settings":{"team":"Not A Team"}}`); err == nil {
		t.Fatalf("a setting off its pattern was accepted")
	}
	if _, err := call(`{"action":"add","type":"not-installed","computer":"laptop","settings":{}}`); err == nil || !strings.Contains(err.Error(), "nothing was added") {
		t.Fatalf("a type nobody installed: %v", err)
	}
	if _, err := call(`{"action":"add","type":"invented-chat","computer":"laptop","settings":{"team":"garden"},"rootPath":"chat"}`); err != nil {
		t.Fatalf("add: %s", err)
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		source, err := tx.GetAgentSourceByName(run.agent.ID, "invented-chat")
		if err != nil || source == nil {
			t.Fatalf("GetAgentSourceByName: %v %s", source, err)
		}
		if source.Specification.Format != models.FormatTyped || source.Specification.Computer != "laptop" || source.RootPath != "chat" || !strings.Contains(string(source.Specification.Settings), "garden") {
			t.Fatalf("the source is %+v", source)
		}
	})
}
