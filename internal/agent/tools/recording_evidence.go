package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// RecordingEvidenceObserver holds unattended turns to lookups after trusted evidence is shown.
type RecordingEvidenceObserver interface{ ObserveRecordingEvidence() }

// MarkRecordingEvidence signals trusted recording provenance without parsing model text.
func MarkRecordingEvidence(run Run) {
	if observer, canObserve := run.(RecordingEvidenceObserver); canObserve {
		observer.ObserveRecordingEvidence()
	}
}

// WithRecordingDocumentEvidence checks owned provenance before a document result is shown.
func WithRecordingDocumentEvidence(ctx context.Context, run Run, result *Result, documentIds []string) (*Result, error) {
	evidence := make([]models.Evidence, 0, len(documentIds))
	for _, documentId := range documentIds {
		evidence = append(evidence, models.Evidence{Kind: models.EvidenceDocument, ID: documentId})
	}
	return WithRecordingMemoryEvidence(ctx, run, result, nil, nil, evidence)
}

// WithRecordingMemoryEvidence follows owned facts and overview citations, with bounded traversal.
func WithRecordingMemoryEvidence(ctx context.Context, run Run, result *Result, nodes []*models.AgentNode, facts []*models.AgentFact, evidence []models.Evidence) (*Result, error) {
	guard, isGuarded := run.(interface{ GuardRecordingEvidence() bool })
	if !isGuarded || !guard.GuardRecordingEvidence() {
		return result, nil
	}
	pending := append([]models.Evidence{}, evidence...)
	for _, node := range nodes {
		if node != nil {
			pending = append(pending, models.Evidence{Kind: models.EvidenceMemory, ID: node.ID})
		}
	}
	for _, fact := range facts {
		if fact != nil {
			pending = append(pending, fact.Evidence...)
		}
	}
	if len(pending) == 0 {
		return result, nil
	}
	hasRecordingEvidence := false
	err := run.Database().TransactionContext(ctx, func(transaction db.Transaction) error {
		var err error
		hasRecordingEvidence, err = RecordingEvidenceOf(transaction, run.Agent().ID, pending)
		return err
	})
	result.HasRecordingEvidence = result.HasRecordingEvidence || hasRecordingEvidence || err != nil
	if result.HasRecordingEvidence {
		MarkRecordingEvidence(run)
	}
	return result, err
}

// WithRecordingRevisionEvidence preserves provenance from retained history after a fact is deleted.
func WithRecordingRevisionEvidence(ctx context.Context, run Run, result *Result, node *models.AgentNode, revisions []*models.AgentRevision) (*Result, error) {
	guard, isGuarded := run.(interface{ GuardRecordingEvidence() bool })
	if !isGuarded || !guard.GuardRecordingEvidence() {
		return result, nil
	}
	var evidence []models.Evidence
	for _, revision := range revisions {
		for _, snapshot := range []map[string]any{revision.Before, revision.After} {
			for _, key := range []string{"evidence", "overviewEvidence"} {
				stored, hasEvidence := snapshot[key]
				if !hasEvidence {
					continue
				}
				encoded, err := json.Marshal(stored)
				if err != nil {
					result.HasRecordingEvidence = true
					MarkRecordingEvidence(run)
					return result, err
				}
				var citations []models.Evidence
				if err := json.Unmarshal(encoded, &citations); err != nil {
					result.HasRecordingEvidence = true
					MarkRecordingEvidence(run)
					return result, err
				}
				evidence = append(evidence, citations...)
			}
		}
	}
	return WithRecordingMemoryEvidence(ctx, run, result, []*models.AgentNode{node}, nil, evidence)
}

// RecordingEvidenceOf refuses missing or unreadable provenance instead of treating it as trusted authority.
func RecordingEvidenceOf(transaction db.Transaction, agentId string, evidence []models.Evidence) (bool, error) {
	pending := append([]models.Evidence{}, evidence...)
	visited := map[string]bool{}
	for len(pending) > 0 {
		citation := pending[0]
		pending = pending[1:]
		evidenceId := strings.Split(strings.Trim(citation.ID, "[]"), "#")[0]
		// A passage of a recording is recorded words by definition.
		if citation.Kind == models.EvidenceRecordingPassage {
			return true, nil
		}
		if citation.Kind != models.EvidenceDocument && citation.Kind != models.EvidenceMemory {
			continue
		}
		key := string(citation.Kind) + ":" + evidenceId
		if visited[key] {
			continue
		}
		visited[key] = true
		if len(visited) > 1000 {
			return true, fmt.Errorf("recording evidence provenance exceeded its traversal bound")
		}
		switch citation.Kind {
		case models.EvidenceDocument:
			document, err := transaction.GetAgentDocument(agentId, evidenceId)
			if err != nil {
				return true, err
			}
			if document == nil {
				return true, fmt.Errorf("recording evidence provenance is unavailable")
			}
			if isRecording, _ := document.Metadata["isUntrustedRecording"].(bool); isRecording {
				return true, nil
			}
			if document.SourceID != "" {
				source, err := transaction.GetAgentSource(agentId, document.SourceID)
				if err != nil {
					return true, err
				}
				if source == nil {
					return true, fmt.Errorf("recording source provenance is unavailable")
				}
				if source.Kind == models.SourceRecording {
					return true, nil
				}
			}
		case models.EvidenceMemory:
			facts, err := transaction.GetAgentFacts(agentId, []string{evidenceId})
			if err != nil {
				return true, err
			}
			if len(facts) > 0 {
				for _, fact := range facts {
					pending = append(pending, fact.Evidence...)
				}
				continue
			}
			nodes, err := transaction.GetAgentNodes(agentId, []string{evidenceId})
			if err != nil {
				return true, err
			}
			if len(nodes) == 0 {
				return true, fmt.Errorf("recording memory provenance is unavailable")
			}
			for _, node := range nodes {
				pending = append(pending, node.OverviewEvidence...)
				facts, err := transaction.ListAgentFacts(agentId, node.ID, true, 1000)
				if err != nil {
					return true, err
				}
				if len(facts) >= 1000 {
					return true, fmt.Errorf("recording memory provenance exceeded its page bound")
				}
				for _, fact := range facts {
					pending = append(pending, fact.Evidence...)
				}
			}
		}
	}
	return false, nil
}
