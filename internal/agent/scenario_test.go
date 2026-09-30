package agent_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/storage"
)

// A scenario runs its records through filing with their own dates, runs
// its dream through the worker, and at the checkpoint reports each
// question: what recall carried, the graded answer, and where each claim
// is said.
func TestAScenarioFilesDreamsAndReports(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.Contains(request.URL.Path, "embeddings") {
			writeMeaning(writer, request)
			return
		}
		var body struct {
			Stream   bool `json:"stream"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(request.Body).Decode(&body)
		prompt := ""
		for _, message := range body.Messages {
			if message.Role == "user" {
				prompt = message.Content
			}
		}
		// An empty list of facts is an answer every reading call can read.
		answer := `{"facts": []}`
		switch {
		case strings.Contains(prompt, "Grade"):
			answer = `{"verdict": "correct", "reason": "it names the store"}`
		case strings.Contains(prompt, "Where is the job state kept?"):
			answer = "In Burrowdb."
		}
		encoded, _ := json.Marshal(answer)
		if body.Stream {
			writer.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprintf(writer,
				"data: {\"id\":\"s1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"content\":%s},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":50,\"completion_tokens\":10}}\n\ndata: [DONE]\n\n",
				encoded)
			return
		}
		_, _ = fmt.Fprintf(writer,
			`{"choices":[{"message":{"role":"assistant","content":%s},"finish_reason":"stop"}],"usage":{"prompt_tokens":50,"completion_tokens":10}}`,
			encoded)
	}))
	defer provider.Close()

	configuration := config.Default()
	configuration.Agent.Providers = []config.AgentProvider{{Name: "fake", Kind: "openai", BaseURL: provider.URL, APIKey: "k"}}
	configuration.Agent.Models.Default = "fake:writer"
	configuration.Agent.Models.Scan = "fake:scan"
	configuration.Agent.Models.Embedding = "fake:meaning"
	store, err := storage.Open(&storage.Settings{Directory: t.TempDir()})
	if err != nil {
		t.Fatalf("storage.Open: %s", err)
	}

	scenarioFile := filepath.Join(t.TempDir(), "scenario.json")
	if err := os.WriteFile(scenarioFile, []byte(`{
		"name": "small",
		"steps": [
			{"id": "decision", "stepKind": "records", "records": [
				{"id": "d1", "kind": "note", "at": "2031-03-02T10:00:00Z", "author": "build lead", "title": "Storage",
				 "text": "Job state is kept in Burrowdb, pinned to version 4.2."}
			]},
			{"id": "upgrade", "stepKind": "conversation", "messages": [
				{"role": "user", "content": "please upgrade the schema"},
				{"role": "assistant", "toolCalls": [{"id": "c1", "toolName": "shell", "arguments": "{\"command\":\"tool migrate --offline\"}"}]},
				{"role": "tool", "toolCallId": "c1", "toolName": "shell", "content": "{\"exitCode\":0,\"stdout\":\"done\"}"},
				{"role": "assistant", "content": "The offline upgrade finished."}
			]},
			{"id": "dream", "stepKind": "dream"},
			{"id": "check", "stepKind": "checkpoint", "questions": [
				{"id": "store", "question": "Where is the job state kept?", "kind": "direct",
				 "expects": [{"words": ["Burrowdb"]}], "forbids": [],
				 "expectedAnswer": "In Burrowdb.", "outdatedClaims": [{"words": ["Ferrox"]}]}
			]}
		]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	scenario, err := agent.ReadScenario(scenarioFile)
	if err != nil {
		t.Fatalf("ReadScenario: %s", err)
	}

	report, err := agent.RunScenario(t.Context(), &agent.ScenarioSettings{
		Database: database, Storage: store, Configuration: configuration,
		Scenario: scenario, RecordsDirectory: filepath.Join(t.TempDir(), "records"),
		AnswerSources: []string{"memory"},
	})
	if err != nil {
		t.Fatalf("RunScenario: %s", err)
	}
	if len(report.Steps) != 4 {
		t.Fatalf("steps reported: %d", len(report.Steps))
	}
	if filed := report.Steps[0].FiledCount; filed != 1 {
		t.Fatalf("filed %d records, want 1", filed)
	}
	if happened := dbtest.QueryString(t, database, `SELECT to_char("happened_at" AT TIME ZONE 'UTC', 'YYYY-MM-DD') FROM "agent_document"`); happened != "2031-03-02" {
		t.Fatalf("the document happened on %q, not the record's date", happened)
	}
	if dreams := dbtest.QueryString(t, database, `SELECT count(*)::text FROM "agent_dream" WHERE "finished_at" IS NOT NULL`); dreams != "1" {
		t.Fatalf("finished dreams: %s", dreams)
	}
	// The conversation is stored as a turn stores one, its tool result
	// fenced, and remembered.
	if fenced := dbtest.QueryString(t, database, `SELECT count(*)::text FROM "agent_message" WHERE "role" = 'tool' AND "content" LIKE '<untrusted-data>%'`); fenced != "1" {
		t.Fatalf("fenced tool results: %s", fenced)
	}
	if remembered := dbtest.QueryString(t, database, `SELECT count(*)::text FROM "agent_job" WHERE "kind" = 'remember' AND "status" = 'done'`); remembered != "1" {
		t.Fatalf("finished remembering: %s", remembered)
	}
	questions := report.Steps[3].Questions
	if len(questions) != 1 {
		t.Fatalf("questions reported: %d", len(questions))
	}
	question := questions[0]
	if len(question.Answers) != 1 || question.Answers[0].AnswerFrom != "memory" || question.Answers[0].AnswerVerdict == "" {
		t.Fatalf("answers: %+v", question.Answers)
	}
	// The fake model files no facts, so the statement is only in the
	// source; the outdated one is nowhere.
	if count := question.ExpectedLayers[0].LayerCounts["source document"]; count != 1 {
		t.Fatalf("expected claim layers: %+v", question.ExpectedLayers[0].LayerCounts)
	}
	if len(question.OutdatedLayers[0].LayerCounts) != 0 {
		t.Fatalf("outdated claim layers: %+v", question.OutdatedLayers[0].LayerCounts)
	}
}
