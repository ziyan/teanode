package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// The facts behind an answer are followed back to the scenario's inputs,
// and a fact derived from others counts their inputs, not one of its own.
//
// Three facts say the store is Burrowdb: one read from a record, one
// remembered from a message of a conversation, and a reflection the agent
// drew from the first. The reflection repeats the record and confirms
// nothing new, so three supporting facts rest on two inputs.
func TestScenarioEvidenceIsTracedToItsInputs(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	var agentId string
	var facts []*models.AgentFact
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "scenario", Name: "Scenario Person"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		found, err := tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true})
		if err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		agentId = found.ID
		if err := tx.EnsureAgentRoots(agentId); err != nil {
			t.Fatalf("EnsureAgentRoots: %s", err)
		}
		source, err := tx.PutAgentSource(&models.AgentKnowledgeSource{
			AgentID: agentId, Kind: models.SourceArchive, Name: "Scenario records", Enabled: true,
			Specification: models.AgentKnowledgeSpecification{Computer: "scenario", Path: "records", Format: models.FormatRecords},
		})
		if err != nil {
			t.Fatalf("PutAgentSource: %s", err)
		}
		documentIds := map[string]string{}
		for _, record := range []string{"decision-001", "unrelated-002"} {
			document, err := tx.PutAgentDocument(&models.AgentDocument{
				AgentID: agentId, SourceID: source.ID, ExternalID: "001-decision.jsonl#" + record,
				Kind: models.AgentDocumentKind("note"), Title: record, Hash: "hash-of-" + record,
			})
			if err != nil {
				t.Fatalf("PutAgentDocument: %s", err)
			}
			if err := tx.ReplaceAgentChunks(document, []*models.AgentChunk{{Text: "the text of " + record}}); err != nil {
				t.Fatalf("ReplaceAgentChunks: %s", err)
			}
			documentIds[record] = document.ID
		}
		conversation, err := tx.CreateAgentConversation(&models.AgentConversation{AgentID: agentId, Kind: models.AgentConversationNamed, Title: "upgrade"})
		if err != nil {
			t.Fatalf("CreateAgentConversation: %s", err)
		}
		var said *models.AgentMessage
		for _, content := range []string{"please check the store", "It is Burrowdb."} {
			if said, err = tx.AppendAgentMessage(&models.AgentMessage{ConversationID: conversation.ID, Role: "user", Content: content}); err != nil {
				t.Fatalf("AppendAgentMessage: %s", err)
			}
		}
		page, err := tx.GetAgentNode(agentId, models.PathSelf)
		if err != nil || page == nil {
			t.Fatalf("GetAgentNode: %v, %v", page, err)
		}
		add := func(text string, kind models.AgentFactKind, evidence models.Evidence) *models.AgentFact {
			fact, err := tx.AddAgentFact(&models.AgentFact{AgentID: agentId, NodeID: page.ID, Kind: kind, Text: text, Evidence: []models.Evidence{evidence}})
			if err != nil {
				t.Fatalf("AddAgentFact: %s", err)
			}
			return fact
		}
		read := add("Job state is kept in Burrowdb.", models.FactPlain, models.Evidence{Kind: models.EvidenceDocument, ID: documentIds["decision-001"]})
		told := add("The store is Burrowdb, as they said.", models.FactPlain, models.Evidence{Kind: models.EvidenceConversation, ID: said.ID})
		drawn := add("Everything about job state comes back to Burrowdb.", models.FactReflection, models.Evidence{Kind: models.EvidenceMemory, ID: read.ID})
		facts = []*models.AgentFact{read, told, drawn}
	})

	origins, err := readScenarioOrigins(t.Context(), database, agentId, map[string]string{"post-001": "decision-001"})
	if err != nil {
		t.Fatalf("readScenarioOrigins: %s", err)
	}
	carried := []*RecalledPage{{Path: models.PathSelf, Facts: facts}}
	question := &ScenarioQuestion{
		ID: "store", Expects: []*ScenarioClaim{{Words: []string{"Burrowdb"}}},
		Evidence: []string{"decision-001", "upgrade#2"},
	}
	report, err := traceScenarioEvidence(t.Context(), database, agentId, origins, carried, question)
	if err != nil {
		t.Fatalf("traceScenarioEvidence: %s", err)
	}
	if report.SupportingFactCount != 3 || report.IndependentSourceCount != 2 {
		t.Fatalf("three facts on two inputs, got %d facts on %d: %+v", report.SupportingFactCount, report.IndependentSourceCount, report.CitedEvidence)
	}
	if !report.IsEvidenceTraced || len(report.MissingEvidence) != 0 || report.UnsourcedFactCount != 0 {
		t.Fatalf("both named inputs are cited: %+v", report)
	}

	// An input nobody filed a fact from is reported missing.
	question.Evidence = []string{"decision-001", "unrelated-002"}
	report, err = traceScenarioEvidence(t.Context(), database, agentId, origins, carried, question)
	if err != nil {
		t.Fatalf("traceScenarioEvidence: %s", err)
	}
	if report.IsEvidenceTraced || len(report.MissingEvidence) != 1 || report.MissingEvidence[0] != "unrelated-002" {
		t.Fatalf("the unrelated record is missing: %+v", report)
	}

	// A chat post is met by its thread, which is what a fact read from
	// the thread cites.
	question.Evidence = []string{"post-001"}
	if report, err = traceScenarioEvidence(t.Context(), database, agentId, origins, carried, question); err != nil || !report.IsEvidenceTraced {
		t.Fatalf("the post's thread is cited: %+v, %v", report, err)
	}

	// A whole conversation step is met by any of its messages.
	question.Evidence = []string{"upgrade"}
	if report, err = traceScenarioEvidence(t.Context(), database, agentId, origins, carried, question); err != nil || !report.IsEvidenceTraced {
		t.Fatalf("the conversation step is cited: %+v, %v", report, err)
	}
}

// A question may name as evidence only what was filed before it: a record,
// a conversation step or one of its messages.
func TestScenarioEvidenceMustNameWhatWasFiledBefore(t *testing.T) {
	write := func(evidence string) string {
		path := filepath.Join(t.TempDir(), "scenario.json")
		content := `{"name": "named", "steps": [
			{"id": "notes", "stepKind": "records", "records": [{"id": "decision-001", "kind": "note", "text": "Burrowdb."}]},
			{"id": "upgrade", "stepKind": "conversation", "messages": [{"role": "user", "content": "It is Burrowdb."}]},
			{"id": "check", "stepKind": "checkpoint", "questions": [{"id": "store", "question": "Which store?",
			 "expectedAnswer": "Burrowdb.", "evidence": [` + evidence + `]}]},
			{"id": "later", "stepKind": "records", "records": [{"id": "later-003", "kind": "note", "text": "Later."}]}
		]}`
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	if _, err := ReadScenario(write(`"decision-001", "upgrade", "upgrade#1"`)); err != nil {
		t.Fatalf("a record, a step and a message filed before: %s", err)
	}
	for _, named := range []string{`"upgrade#2"`, `"later-003"`, `"nothing"`} {
		if _, err := ReadScenario(write(named)); err == nil {
			t.Fatalf("%s was not filed before the question and was accepted", named)
		}
	}
}
