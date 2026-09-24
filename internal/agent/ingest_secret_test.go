package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A typed source's secrets go to its computer opened, only those its type
// declares, and a required one not yet filled in stops the reading with a
// word on how to fill it.
func TestATypedSourceIsSentItsSecrets(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()
	configuration := config.Default()
	configuration.Server.Secret = "a secret long enough to seal things with"
	settings := &Settings{Database: database, Configuration: func() *config.Configuration { return configuration }, Instance: "test"}
	worker := New(settings)
	run := &Run{settings: settings}

	var source *models.AgentKnowledgeSource
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "typed-owner"})
		if err != nil {
			t.Fatal(err)
		}
		person, err := tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.PutAgentSourceType(&models.AgentSourceType{Name: "invented-api", Publisher: "local", IsLocal: true, Content: `---
name: invented-api
description: an invented web API
secrets:
  - {key: token, description: an API token, scope: person}
authenticationProfiles:
  api: {type: bearer, token: "{{secret:token}}"}
containers:
  - fixed: [{}]
    name: all.jsonl
records:
  - request: {url: "https://api.example.com/items", auth: api}
    parse: {json: {items: items}}
    record: {id: "{{item.id}}"}
---
`}); err != nil {
			t.Fatal(err)
		}
		settingsJSON, _ := json.Marshal(map[string]any{})
		if source, err = tx.PutAgentSource(&models.AgentKnowledgeSource{AgentID: person.ID, Name: "items", Kind: models.SourceComputer, Enabled: true,
			Specification: models.AgentKnowledgeSpecification{Type: "invented-api", Format: models.FormatTyped, Computer: "laptop", Settings: settingsJSON}}); err != nil {
			t.Fatal(err)
		}
	})

	if _, _, _, err := worker.typedSourceParts(t.Context(), run, source); err == nil || !strings.Contains(err.Error(), `knowledge secret set "items" token`) {
		t.Fatalf("a missing token did not say how to set it: %v", err)
	}
	sealed, err := worker.SealSecret("the-token")
	if err != nil {
		t.Fatal(err)
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		for key, value := range map[string]string{"token": sealed, "undeclared": sealed} {
			if err := tx.PutAgentSourceSecret(source.AgentID, &models.AgentSourceSecret{SourceID: source.ID, Key: key, Value: value}); err != nil {
				t.Fatal(err)
			}
		}
	})
	_, _, secrets, err := worker.typedSourceParts(t.Context(), run, source)
	if err != nil {
		t.Fatalf("typedSourceParts: %s", err)
	}
	if len(secrets) != 1 || secrets["token"] != "the-token" {
		t.Fatalf("sent %v", secrets)
	}
}
