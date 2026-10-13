package tools

import (
	"context"
	"errors"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

type recordingEvidenceTestRun struct {
	Run
	database            db.Database
	person              *models.Agent
	isGuarded           bool
	hasObservedEvidence bool
}

func (self *recordingEvidenceTestRun) Database() db.Database        { return self.database }
func (self *recordingEvidenceTestRun) Agent() *models.Agent         { return self.person }
func (self *recordingEvidenceTestRun) ObserveRecordingEvidence()    { self.hasObservedEvidence = true }
func (self *recordingEvidenceTestRun) GuardRecordingEvidence() bool { return self.isGuarded }

type failedRecordingEvidenceDatabase struct{ db.Database }

func (self *failedRecordingEvidenceDatabase) TransactionContext(context.Context, func(db.Transaction) error) error {
	return errors.New("synthetic provenance read failure")
}

func TestRecordingEvidenceProvenanceFailureIsClosedWithoutChangingHumanReads(test *testing.T) {
	run := &recordingEvidenceTestRun{database: &failedRecordingEvidenceDatabase{}, person: &models.Agent{ID: "fixture-agent"}, isGuarded: true}
	result, err := WithRecordingDocumentEvidence(test.Context(), run, TextResult("unshown document"), []string{"fixture-document"})
	if err == nil || result == nil || !result.HasRecordingEvidence {
		test.Fatal("unreadable provenance permitted tools", result, err)
	}
	run.isGuarded = false
	result, err = WithRecordingDocumentEvidence(test.Context(), run, TextResult("ordinary document"), []string{"fixture-document"})
	if err != nil || result.HasRecordingEvidence {
		test.Fatal("human reads changed", result, err)
	}
}

func TestRecordingEvidenceFollowsOwnedFactsOverviewsAndRetainedHistory(test *testing.T) {
	database, release := dbtest.AcquireDatabase(test)
	defer release()
	var person *models.Agent
	var recordingDocument, ordinaryDocument, foreignDocument *models.AgentDocument
	var page *models.AgentNode
	var fact *models.AgentFact
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		owner, err := transaction.CreateUser(&models.User{Username: "fixture-owner"})
		if err != nil {
			test.Fatal(err)
		}
		person, err = transaction.CreateAgent(&models.Agent{UserID: owner.ID})
		if err != nil {
			test.Fatal(err)
		}
		source, err := transaction.PutAgentSource(&models.AgentKnowledgeSource{AgentID: person.ID, Name: "fixture-recording-source", Kind: models.SourceRecording})
		if err != nil {
			test.Fatal(err)
		}
		recordingDocument, err = transaction.PutAgentDocument(&models.AgentDocument{AgentID: person.ID, SourceID: source.ID, ExternalID: "fixture-recording", Title: "Fixture recording", Hash: "fixture-recording-hash", Kind: models.DocumentJournal})
		if err != nil {
			test.Fatal(err)
		}
		ordinarySource, err := transaction.PutAgentSource(&models.AgentKnowledgeSource{AgentID: person.ID, Name: "fixture-ordinary-source", Kind: models.SourceArchive, Specification: models.AgentKnowledgeSpecification{Computer: "fixture-computer", Path: "fixture-notes", Format: models.FormatJournal}})
		if err != nil {
			test.Fatal(err)
		}
		ordinaryDocument, err = transaction.PutAgentDocument(&models.AgentDocument{AgentID: person.ID, SourceID: ordinarySource.ID, ExternalID: "fixture-note", Title: "Fixture note", Hash: "fixture-note-hash", Kind: models.DocumentJournal})
		if err != nil {
			test.Fatal(err)
		}
		page, err = transaction.PutAgentNode(&models.AgentNode{AgentID: person.ID, Path: "projects/fixture", Name: "Fixture", Kind: models.NodeProject})
		if err != nil {
			test.Fatal(err)
		}
		fact, err = transaction.AddAgentFact(&models.AgentFact{AgentID: person.ID, NodeID: page.ID, Text: "An uncertain proposal", Kind: models.FactPlain, Evidence: []models.Evidence{{Kind: models.EvidenceDocument, ID: recordingDocument.ID}}})
		if err != nil {
			test.Fatal(err)
		}
		otherOwner, err := transaction.CreateUser(&models.User{Username: "fixture-other-owner"})
		if err != nil {
			test.Fatal(err)
		}
		otherAgent, err := transaction.CreateAgent(&models.Agent{UserID: otherOwner.ID})
		if err != nil {
			test.Fatal(err)
		}
		foreignSource, err := transaction.PutAgentSource(&models.AgentKnowledgeSource{AgentID: otherAgent.ID, Name: "fixture-foreign-source", Kind: models.SourceArchive, Specification: models.AgentKnowledgeSpecification{Computer: "fixture-computer", Path: "fixture-notes", Format: models.FormatJournal}})
		if err != nil {
			test.Fatal(err)
		}
		foreignDocument, err = transaction.PutAgentDocument(&models.AgentDocument{AgentID: otherAgent.ID, SourceID: foreignSource.ID, ExternalID: "foreign-fixture", Title: "Other fixture", Hash: "foreign-fixture-hash", Kind: models.DocumentJournal})
		if err != nil {
			test.Fatal(err)
		}
	})
	guarded := &recordingEvidenceTestRun{database: database, person: person, isGuarded: true}
	for _, citation := range []models.Evidence{{Kind: models.EvidenceDocument, ID: recordingDocument.ID}, {Kind: models.EvidenceMemory, ID: fact.ID}, {Kind: models.EvidenceMemory, ID: page.ID}} {
		dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
			hasRecordingEvidence, err := RecordingEvidenceOf(transaction, person.ID, []models.Evidence{citation})
			if err != nil || !hasRecordingEvidence {
				test.Fatal("owned recording provenance was lost", citation.Kind, err)
			}
		})
	}
	result, err := WithRecordingDocumentEvidence(test.Context(), guarded, TextResult("ordinary evidence"), []string{ordinaryDocument.ID})
	if err != nil || result.HasRecordingEvidence {
		test.Fatal("ordinary evidence was restricted", err)
	}
	result, err = WithRecordingDocumentEvidence(test.Context(), guarded, TextResult("foreign evidence"), []string{foreignDocument.ID})
	if err == nil || !result.HasRecordingEvidence {
		test.Fatal("foreign provenance was accepted", err)
	}
	var revisions []*models.AgentRevision
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if err := transaction.DeleteAgentFact(person.ID, fact.ID); err != nil {
			test.Fatal(err)
		}
		var err error
		revisions, err = transaction.ListAgentRevisions(person.ID, page.ID, 20)
		if err != nil {
			test.Fatal(err)
		}
	})
	result, err = WithRecordingRevisionEvidence(test.Context(), guarded, TextResult("retained history"), page, revisions)
	if err != nil || !result.HasRecordingEvidence {
		test.Fatal("deleted fact history lost recording provenance", err)
	}
}

func TestRecordingRevisionDecodeFailureMarksObserverImmediately(test *testing.T) {
	run := &recordingEvidenceTestRun{isGuarded: true}
	result, err := WithRecordingRevisionEvidence(test.Context(), run, TextResult("unshown history"), nil, []*models.AgentRevision{{Before: map[string]any{"evidence": "invalid citation list"}}})
	if err == nil || result == nil || !result.HasRecordingEvidence || !run.hasObservedEvidence {
		test.Fatal("revision failure did not restrict the observer before returning", err)
	}
}
