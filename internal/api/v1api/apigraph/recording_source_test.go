package apigraph

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func TestRecordingSourceKeepsItsInternalName(test *testing.T) {
	database, release := dbtest.AcquireDatabase(test)
	defer release()
	var owner *models.User
	var source *models.AgentKnowledgeSource
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		var err error
		owner, err = transaction.CreateUser(&models.User{Username: "recording-source-owner"})
		if err != nil {
			test.Fatal(err)
		}
		assistant, err := transaction.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true})
		if err != nil {
			test.Fatal(err)
		}
		source, err = transaction.PutAgentSource(&models.AgentKnowledgeSource{AgentID: assistant.ID, Kind: models.SourceRecording, Name: "recording:invented", Enabled: true})
		if err != nil {
			test.Fatal(err)
		}
	})
	principal := &api.Principal{User: owner, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionAgentUse}})}
	resolver := &graph{database: database}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), principal), transaction)
		if _, err := resolver.SaveAgentKnowledgeSource(ctx, SaveAgentKnowledgeSourceArguments{Kind: "recording", Name: "invented"}); !errors.Is(err, api.ErrInvalidArguments) || strings.Contains(err.Error(), "Finance") {
			test.Fatal("manual recording source creation returned unrelated instructions", err)
		}
		if _, err := resolver.SaveAgentKnowledgeSource(ctx, SaveAgentKnowledgeSourceArguments{SourceID: source.ID, Name: "renamed source"}); !errors.Is(err, api.ErrInvalidArguments) {
			test.Fatal("recording internal name could change", err)
		}
		isEnabled := false
		saved, err := resolver.SaveAgentKnowledgeSource(ctx, SaveAgentKnowledgeSourceArguments{SourceID: source.ID, Name: source.Name, Enabled: &isEnabled, Cron: new("0 7 * * *")})
		if err != nil || saved.Name != source.Name || saved.Enabled || saved.Cron != "0 7 * * *" {
			test.Fatal("recording source pause or schedule changed semantics", saved, err)
		}
		if saved.NextRunAt == nil {
			test.Fatal("recording schedule could not start after manual ingestion")
		}
		unchanged, err := resolver.SaveAgentKnowledgeSource(ctx, SaveAgentKnowledgeSourceArguments{SourceID: source.ID})
		if err != nil || unchanged.Cron != saved.Cron || unchanged.NextRunAt == nil || !unchanged.NextRunAt.Equal(*saved.NextRunAt) {
			test.Fatal("omitted schedule changed its next run", unchanged, err)
		}
		manual, err := resolver.SaveAgentKnowledgeSource(ctx, SaveAgentKnowledgeSourceArguments{SourceID: source.ID, Cron: new("")})
		if err != nil || manual.Cron != "" || manual.NextRunAt != nil {
			test.Fatal("explicit empty schedule did not restore manual ingestion", manual, err)
		}
		if _, err := resolver.SaveAgentKnowledgeSource(ctx, SaveAgentKnowledgeSourceArguments{SourceID: source.ID, Cron: new("invalid")}); !errors.Is(err, api.ErrInvalidArguments) {
			test.Fatal("invalid recording schedule was saved", err)
		}
	})
}
