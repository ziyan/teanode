package agent

import (
	"context"
	"sync/atomic"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// recordingEvidenceSafety persists across the calls and nested turns of one dream.
type recordingEvidenceSafety struct{ hasRecordingEvidence atomic.Bool }
type recordingEvidenceSafetyKey struct{}

func withRecordingEvidenceSafety(ctx context.Context) context.Context {
	if recordingEvidenceSafetyOf(ctx) != nil {
		return ctx
	}
	return context.WithValue(ctx, recordingEvidenceSafetyKey{}, &recordingEvidenceSafety{})
}
func recordingEvidenceSafetyOf(ctx context.Context) *recordingEvidenceSafety {
	safety, _ := ctx.Value(recordingEvidenceSafetyKey{}).(*recordingEvidenceSafety)
	return safety
}
func (self *recordingEvidenceSafety) isRestricted() bool {
	return self != nil && self.hasRecordingEvidence.Load()
}
func (self *recordingEvidenceSafety) restrict() {
	if self != nil {
		self.hasRecordingEvidence.Store(true)
	}
}
func containsRecordingEvidence(documents []*models.AgentDocument) bool {
	for _, document := range documents {
		if document != nil {
			if isRecording, _ := document.Metadata["isUntrustedRecording"].(bool); isRecording {
				return true
			}
		}
	}
	return false
}

// recordingLookupAllowed names the tools a dream may still call once it
// holds recording evidence: the lookups, and the person's own mail,
// calendar and conversations, which the run reads and nothing else does.
// None of them reaches outside the server, and the run is read-only by
// then (ReadOnly), so each is offered and dispatched only for reading.
// Without the mail and the calendar, a person whose self page carries one
// fact from a recording had every dream held to memory, and the nightly
// look for ideas never found anything in their mail again.
func recordingLookupAllowed(tool *tools.Tool) bool {
	switch tool.Name {
	case "memory", "knowledge", "recording", "tool_search", "result_more", "mail_search", "mail_read", "calendar", "conversation":
		return true
	}
	return false
}
func (self *AskRun) recordingEvidenceRestricted() bool {
	return self.settings.recordingEvidenceSafety.isRestricted()
}

func checkRecordingPromptEvidence(ctx context.Context, transaction db.Transaction, agentId string, nodes []*models.AgentNode, facts []*models.AgentFact, documentIds []string) error {
	safety := recordingEvidenceSafetyOf(ctx)
	if safety == nil {
		return nil
	}
	var evidence []models.Evidence
	for _, node := range nodes {
		if node != nil {
			evidence = append(evidence, models.Evidence{Kind: models.EvidenceMemory, ID: node.ID})
		}
	}
	for _, fact := range facts {
		if fact != nil {
			evidence = append(evidence, fact.Evidence...)
		}
	}
	for _, documentId := range documentIds {
		evidence = append(evidence, models.Evidence{Kind: models.EvidenceDocument, ID: documentId})
	}
	hasRecordingEvidence, err := tools.RecordingEvidenceOf(transaction, agentId, evidence)
	if hasRecordingEvidence || err != nil {
		safety.restrict()
	}
	return err
}
