package agent

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/computer"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func benchmarkAgent(benchmark *testing.B) (db.Database, *Agent, *Run, *models.AgentKnowledgeSource) {
	benchmark.Helper()
	database, release := dbtest.AcquireDatabase(benchmark)
	benchmark.Cleanup(release)
	dbtest.Exec(benchmark, database, `CREATE EXTENSION IF NOT EXISTS pg_stat_statements`)
	configuration := config.Default()
	settings := &Settings{Database: database, Configuration: func() *config.Configuration { return configuration }}
	run := &Run{settings: settings}
	var source *models.AgentKnowledgeSource
	if err := database.Transaction(func(transaction db.Transaction) error {
		var err error
		run.Owner, err = transaction.CreateUser(&models.User{Username: "fixture-owner"})
		if err != nil {
			return err
		}
		run.Agent, err = transaction.CreateAgent(&models.Agent{UserID: run.Owner.ID, Enabled: true})
		if err != nil {
			return err
		}
		source, err = transaction.PutAgentSource(&models.AgentKnowledgeSource{AgentID: run.Agent.ID, Kind: models.SourceArchive, Name: "Fixture", Enabled: true, Specification: models.AgentKnowledgeSpecification{Computer: "fixture-computer", Path: "/fixture", Format: models.FormatFiles}})
		return err
	}); err != nil {
		benchmark.Fatal(err)
	}
	return database, &Agent{settings: settings}, run, source
}

func benchmarkSQLCount(benchmark *testing.B, database db.Database) int64 {
	benchmark.Helper()
	encodedCount := dbtest.QueryString(benchmark, database, `SELECT COALESCE(sum(calls), 0)::bigint::text FROM pg_stat_statements WHERE dbid = (SELECT oid FROM pg_database WHERE datname = current_database()) AND query NOT LIKE '%pg_stat_statements%'`)
	statementCount, err := strconv.ParseInt(encodedCount, 10, 64)
	if err != nil {
		benchmark.Fatal(err)
	}
	return statementCount
}

func BenchmarkSourcePage(benchmark *testing.B) {
	for _, fixture := range []struct {
		name          string
		documentCount int
		isUnchanged   bool
	}{{"changed_25", 25, false}, {"changed_100", 100, false}, {"unchanged_100", 100, true}} {
		benchmark.Run(fixture.name, func(benchmark *testing.B) {
			database, worker, run, source := benchmarkAgent(benchmark)
			page := ingestPage{NextCursor: "fixture-next"}
			for index := range fixture.documentCount {
				page.Entries = append(page.Entries, computer.ScanEntry{ExternalID: fmt.Sprintf("fixture-%d", index), Title: "Fixture document", Kind: "file", Hash: "initial", Text: strings.Repeat("Fixture workspace uses blue panels.\n", 64)})
			}
			if _, _, err := worker.fileComputerPage(benchmark.Context(), run, source, page, nil); err != nil {
				benchmark.Fatal(err)
			}
			for index := range page.Entries {
				page.Entries[index].Unchanged = fixture.isUnchanged
			}
			expectedDocumentCount := fixture.documentCount
			if fixture.isUnchanged {
				expectedDocumentCount = 0
			}
			initialCount := benchmarkSQLCount(benchmark, database)
			benchmark.ReportAllocs()
			benchmark.ResetTimer()
			for iteration := 0; iteration < benchmark.N; iteration++ {
				if !fixture.isUnchanged {
					for index := range page.Entries {
						page.Entries[index].Hash = fmt.Sprintf("revision-%d", iteration)
					}
				}
				nextCursor, counts, err := worker.fileComputerPage(benchmark.Context(), run, source, page, nil)
				if err != nil || nextCursor != page.NextCursor || counts.Seen != fixture.documentCount || counts.Documents != expectedDocumentCount {
					benchmark.Fatalf("page failed: %s, %+v, %v", nextCursor, counts, err)
				}
			}
			benchmark.StopTimer()
			benchmark.ReportMetric(float64(benchmarkSQLCount(benchmark, database)-initialCount)/float64(benchmark.N), "sql/op")
			benchmark.ReportMetric(float64(fixture.documentCount), "documents/op")
		})
	}
}

func BenchmarkGraphRetrieval(benchmark *testing.B) {
	for _, factCount := range []int{100, 1000} {
		benchmark.Run(fmt.Sprintf("facts_%d", factCount), func(benchmark *testing.B) {
			database, worker, run, _ := benchmarkAgent(benchmark)
			query := &meaning{ModelName: "fixture-vector", Vector: fixtureVector(0)}
			if err := database.Transaction(func(transaction db.Transaction) error {
				for pageIndex := range factCount / 10 {
					page, err := transaction.PutAgentNode(&models.AgentNode{AgentID: run.Agent.ID, Path: fmt.Sprintf("projects/fixture-%d", pageIndex), Kind: models.NodeProject, Name: "Fixture"})
					if err != nil {
						return err
					}
					if err := transaction.PutAgentNodeVector(run.Agent.ID, page.ID, query.ModelName, fixtureVector(pageIndex)); err != nil {
						return err
					}
					for factIndex := range 10 {
						fact, err := transaction.AddAgentFact(&models.AgentFact{AgentID: run.Agent.ID, NodeID: page.ID, Kind: models.FactPlain, Text: fmt.Sprintf("Fixture statement %d.", pageIndex*10+factIndex)})
						if err != nil {
							return err
						}
						if err := transaction.PutAgentFactVector(run.Agent.ID, fact.ID, query.ModelName, fixtureVector(pageIndex*10+factIndex)); err != nil {
							return err
						}
					}
				}
				return nil
			}); err != nil {
				benchmark.Fatal(err)
			}
			for _, table := range []db.VectorTable{db.AgentNodeTable, db.AgentFactTable} {
				if err := database.EnsureVectorIndex(table, query.ModelName, len(query.Vector)); err != nil {
					benchmark.Fatal(err)
				}
			}
			dbtest.Exec(benchmark, database, `ANALYZE`)
			if _, _, err := worker.nearestInGraphTo(benchmark.Context(), run.Agent.ID, query, 20); err != nil {
				benchmark.Fatal(err)
			}
			initialCount := benchmarkSQLCount(benchmark, database)
			benchmark.ReportAllocs()
			benchmark.ResetTimer()
			for iteration := 0; iteration < benchmark.N; iteration++ {
				nodes, facts, err := worker.nearestInGraphTo(benchmark.Context(), run.Agent.ID, query, 20)
				if err != nil || len(nodes) != min(20, factCount/10) || len(facts) != 20 {
					benchmark.Fatalf("retrieval failed: nodes=%d facts=%d err=%v", len(nodes), len(facts), err)
				}
			}
			benchmark.StopTimer()
			benchmark.ReportMetric(float64(benchmarkSQLCount(benchmark, database)-initialCount)/float64(benchmark.N), "sql/op")
		})
	}
}

func fixtureVector(index int) []float32 {
	vector := make([]float32, 32)
	for dimension := range vector {
		vector[dimension] = 1 + float32((index+dimension)%11)/10
	}
	return vector
}
