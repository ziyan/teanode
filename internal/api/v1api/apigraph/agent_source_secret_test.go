package apigraph

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

const invented = `---
name: invented-api
description: an invented service read by its web API
runs: [computer]
settings:
  - {name: host, description: the host, type: string, pattern: "^[a-z.]+$"}
secrets:
  - {key: token, description: an API token, scope: person}
  - {key: extra, description: an optional value, scope: person, optional: true}
authenticationProfiles:
  api: {type: bearer, token: "{{secret:token}}"}
containers:
  - fixed: [{}]
    name: all.jsonl
records:
  - request: {url: "https://{{settings.host}}/items", auth: api}
    parse: {json: {items: items}}
    record: {id: "{{item.id}}"}
---
`

// A source's secrets are asked for by its type, kept for that source, never
// come back out, are refused for a key the type does not declare or a
// source that is somebody else's, and are forgotten when the type changes.
func TestASourceKeepsTheSecretsItsTypeAsksFor(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	var owner, stranger *models.User
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if owner, err = tx.CreateUser(&models.User{Username: "secret-owner", Name: "Alice Example"}); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Name: "Bertie"}); err != nil {
			t.Fatal(err)
		}
		if stranger, err = tx.CreateUser(&models.User{Username: "secret-stranger", Name: "Carol Example"}); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.CreateAgent(&models.Agent{UserID: stranger.ID, Enabled: true, Name: "Dora"}); err != nil {
			t.Fatal(err)
		}
		for _, content := range []string{invented, inventedFolderType} {
			if _, err := tx.PutAgentSourceType(&models.AgentSourceType{Name: map[bool]string{true: "invented-api", false: "invented-folder"}[content == invented], Publisher: "local", IsLocal: true, Content: content}); err != nil {
				t.Fatal(err)
			}
		}
	})
	person := func(user *models.User) *api.Principal {
		return &api.Principal{User: user, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionAgentUse}})}
	}
	resolver := &graph{database: database}
	as := func(user *models.User, run func(ctx context.Context, tx db.Transaction)) {
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
			run(api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), person(user)), tx), tx)
		})
	}

	var sourceId, agentId string
	as(owner, func(ctx context.Context, tx db.Transaction) {
		saved, err := resolver.SaveAgentKnowledgeSource(ctx, SaveAgentKnowledgeSourceArguments{
			Type: "invented-api", Computer: "laptop", Settings: json.RawMessage(`{"host": "api.example.com"}`),
		})
		if err != nil {
			t.Fatalf("a type with secrets is refused: %s", err)
		}
		sourceId, agentId = saved.ID, saved.AgentID

		asked, err := resolver.ListAgentKnowledgeSourceSecrets(ctx, AgentSourceSecretsArguments{SourceID: sourceId})
		if err != nil || len(asked) != 2 || asked[0].Key != "token" || asked[0].IsSet || asked[0].IsOptional || !asked[1].IsOptional {
			t.Fatalf("asked %+v, %v", asked, err)
		}
		if _, err := resolver.SetAgentKnowledgeSourceSecret(ctx, SetAgentSourceSecretArguments{SourceID: sourceId, Key: "password", Value: "x"}); !errors.Is(err, api.ErrInvalidArguments) {
			t.Errorf("a key the type does not ask for was kept: %v", err)
		}

		// What a server with its secret seals, written as it would be.
		if err := tx.PutAgentSourceSecret(agentId, &models.AgentSourceSecret{SourceID: sourceId, Key: "token", Value: "sealed"}); err != nil {
			t.Fatal(err)
		}
		asked, _ = resolver.ListAgentKnowledgeSourceSecrets(ctx, AgentSourceSecretsArguments{SourceID: sourceId})
		if !asked[0].IsSet {
			t.Errorf("a kept value is not shown as set")
		}
		// Clearing names one declared key: an empty one used to forget them all.
		for _, key := range []string{"", "password"} {
			if _, err := resolver.ClearAgentKnowledgeSourceSecret(ctx, ClearAgentSourceSecretArguments{SourceID: sourceId, Key: key}); !errors.Is(err, api.ErrInvalidArguments) {
				t.Errorf("clearing %q was taken: %v", key, err)
			}
		}
		if stored, _ := tx.ListAgentSourceSecrets(sourceId); len(stored) != 1 {
			t.Errorf("a refused clear forgot values: %v", stored)
		}
	})

	as(stranger, func(ctx context.Context, tx db.Transaction) {
		if _, err := resolver.ListAgentKnowledgeSourceSecrets(ctx, AgentSourceSecretsArguments{SourceID: sourceId}); !errors.Is(err, api.ErrNotFound) {
			t.Errorf("somebody else's source answered: %v", err)
		}
		if _, err := resolver.ClearAgentKnowledgeSourceSecret(ctx, ClearAgentSourceSecretArguments{SourceID: sourceId, Key: "token"}); !errors.Is(err, api.ErrNotFound) {
			t.Errorf("somebody else cleared a secret: %v", err)
		}
	})

	as(owner, func(ctx context.Context, tx db.Transaction) {
		if _, err := resolver.SaveAgentKnowledgeSource(ctx, SaveAgentKnowledgeSourceArguments{
			SourceID: sourceId, Type: "invented-folder", Settings: json.RawMessage(`{"path": "~/notes"}`),
		}); err != nil {
			t.Fatalf("SaveAgentKnowledgeSource: %s", err)
		}
		stored, err := tx.ListAgentSourceSecrets(sourceId)
		if err != nil || len(stored) != 0 {
			t.Errorf("another type kept the old type's secrets: %v %v", stored, err)
		}
	})
}
