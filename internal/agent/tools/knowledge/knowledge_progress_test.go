package knowledge_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func TestSourceControlsPreserveConcurrentProgress(test *testing.T) {
	for _, control := range []string{"pause", "resume", "sync"} {
		test.Run(control, func(test *testing.T) {
			run, database, closeDatabase := world(test, "fixture-owner")
			defer closeDatabase()
			var source *models.AgentKnowledgeSource
			dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
				var err error
				source, err = transaction.PutAgentSource(&models.AgentKnowledgeSource{
					AgentID: run.agent.ID, Kind: models.SourceComputer, Name: "fixture-source", Enabled: true,
					Specification: models.AgentKnowledgeSpecification{Computer: "fixture-device", Path: "/fixture", Format: models.FormatFiles},
				})
				if err != nil {
					test.Fatal(err)
				}
			})
			ctx, cancel := context.WithTimeout(tools.WithRun(test.Context(), run), 10*time.Second)
			defer cancel()
			tool := find(test, "knowledge")
			controlErrors := make(chan error, 1)
			dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
				if err := transaction.MarkAgentSourceRun(source.ID, map[string]any{"after": "completed-page"}, db.SourceCounts{Documents: 7}, true, "", nil); err != nil {
					test.Fatal(err)
				}
				go func() {
					_, err := tool.Run(ctx, &tools.Call{ID: "fixture-call", Arguments: []byte(fmt.Sprintf(`{"action":%q,"source":"fixture-source"}`, control))})
					controlErrors <- err
				}()
				select {
				case err := <-controlErrors:
					test.Fatalf("control did not wait for progress: %v", err)
				case <-time.After(100 * time.Millisecond):
				}
			})
			if err := <-controlErrors; err != nil {
				test.Fatal(err)
			}
			dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
				current, err := transaction.GetAgentSource(source.AgentID, source.ID)
				if err != nil || current == nil {
					test.Fatalf("current source=%+v, %v", current, err)
				}
				if current.Cursor["after"] != "completed-page" || current.DocumentCount != 7 || !current.More || current.Generation != source.Generation+1 || current.Enabled != (control != "pause") {
					test.Fatalf("control overwrote progress or failed to advance generation: %+v", current)
				}
			})
		})
	}
}
