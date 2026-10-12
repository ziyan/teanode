package agent_test

import (
	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
	"strings"
	"testing"
	"time"
)

func TestAssistantTurnWithoutHostedCollections(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	model, _ := fakeModel(test, []string{declinedRound})
	defer model.Close()
	configuration := config.Default()
	configuration.Services = config.AssistantServices()
	configuration.Agent.Enabled = true
	configuration.Agent.Features.Dreaming = new(false)
	configuration.Agent.Providers = []config.AgentProvider{{Name: "fake", Kind: "openai", BaseURL: model.URL, APIKey: "test"}}
	configuration.Agent.Models.Default = "fake:thinker"
	registry, err := llm.Open(&configuration.Agent)
	if err != nil {
		test.Fatal(err)
	}
	spool, err := storage.Open(&storage.Settings{Directory: test.TempDir()})
	if err != nil {
		test.Fatal(err)
	}
	defer func() { _ = spool.Close() }()
	worker := agent.New(&agent.Settings{Database: database, Storage: spool, Registry: registry, Configuration: func() *config.Configuration { return configuration }, Instance: "test", Tick: time.Hour})
	var owner *models.User
	var found *models.Agent
	var conversation *models.AgentConversation
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		owner, err = transaction.CreateUser(&models.User{Username: "assistant-owner"})
		if err != nil {
			test.Fatal(err)
		}
		found, err = transaction.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true})
		if err != nil {
			test.Fatal(err)
		}
		conversation, err = transaction.CreateAgentConversation(&models.AgentConversation{AgentID: found.ID, Kind: models.AgentConversationMain, LastAt: time.Now()})
		if err != nil {
			test.Fatal(err)
		}
	})
	operations := &fakeOperations{permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionAgentUse}, {Permission: models.PermissionMailRead}, {Permission: models.PermissionCalendarUse}, {Permission: models.PermissionContactsUse}})}
	run, err := worker.Ask(&agent.AskSettings{Agent: found, Owner: owner, Operations: operations, Conversation: conversation, Message: "Hello", Surface: "cli"})
	if err != nil {
		test.Fatal(err)
	}
	hasAnswer := false
	for _, event := range collect(run) {
		if event.Kind == agent.EventMessage && event.Text != "" {
			hasAnswer = true
		}
	}
	if !hasAnswer {
		test.Fatal("assistant had no answer")
	}
	operations.mutex.Lock()
	defer operations.mutex.Unlock()
	for _, document := range operations.documents {
		for _, operation := range []string{"ListMailboxes", "ListCalendars", "ListAddressBooks"} {
			if strings.Contains(document, operation) {
				test.Fatalf("disabled collection read: %s", operation)
			}
		}
	}
	for _, table := range []string{"mailbox", "calendar", "addressbook"} {
		if dbtest.QueryString(test, database, "SELECT count(*)::text FROM "+table) != "0" {
			test.Fatalf("assistant provisioned %s", table)
		}
	}
}
