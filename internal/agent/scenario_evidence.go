package agent

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// Tracing an answer to what it rests on. A question may name the inputs
// that support its expected answer: a record by its id, a message of a
// conversation step as "<step id>#<number>", counting from one, or a
// whole conversation step by its id. At a checkpoint the facts recall
// carried that say an expected claim are followed back through their
// evidence to those inputs. A fact the agent derived from other facts, a
// reflection or a theme, adds nothing of its own: it is followed to the
// facts it names and counts their inputs, so ten facts repeating one
// record are one source, not ten confirmations.

// ScenarioEvidenceReport is where the facts behind a question's expected
// answer came from.
type ScenarioEvidenceReport struct {
	// NamedEvidence is what the question says supports the answer.
	NamedEvidence []string `json:"namedEvidence"`

	// CitedEvidence is the inputs the supporting facts rest on, each once.
	CitedEvidence []string `json:"citedEvidence"`

	// MissingEvidence is what was named and no supporting fact rests on.
	MissingEvidence []string `json:"missingEvidence,omitempty"`

	// SupportingFactCount is how many carried facts say an expected claim,
	// and IndependentSourceCount how many inputs they rest on: when the
	// first is larger, facts repeat one another rather than confirm.
	SupportingFactCount    int `json:"supportingFactCount"`
	IndependentSourceCount int `json:"independentSourceCount"`

	// UnsourcedFactCount is how many supporting facts lead back to no
	// input at all: derived from nothing that can be found, or carrying no
	// evidence.
	UnsourcedFactCount int `json:"unsourcedFactCount"`

	// IsEvidenceTraced is whether every named input is among the cited.
	IsEvidenceTraced bool `json:"isEvidenceTraced"`
}

// scenarioOrigins names a run's inputs by what a scenario file calls them:
// documents by their record id, messages by their step and number, and
// conversations by their step. A chat thread is filed as one document,
// named by its thread, so a fact read from it cites the thread; threads
// says which thread each chat record was a post of.
type scenarioOrigins struct {
	documents     map[string]string
	messages      map[string]string
	conversations map[string]string
	threads       map[string]string
}

// scenarioThreads is the thread of each chat record in a scenario.
func scenarioThreads(scenario *Scenario) map[string]string {
	threads := map[string]string{}
	for _, step := range scenario.Steps {
		for _, record := range step.Records {
			var post struct {
				ID     string `json:"id"`
				Thread string `json:"thread"`
			}
			if err := json.Unmarshal(record, &post); err == nil && post.ID != "" && post.Thread != "" {
				threads[post.ID] = post.Thread
			}
		}
	}
	return threads
}

// readScenarioOrigins reads the names of everything the run filed. It is
// read from the database rather than kept as the run goes, so that asking
// a finished run again names the same inputs.
func readScenarioOrigins(ctx context.Context, database db.Database, agentId string, threads map[string]string) (*scenarioOrigins, error) {
	origins := &scenarioOrigins{documents: map[string]string{}, messages: map[string]string{}, conversations: map[string]string{}, threads: threads}
	err := database.TransactionContext(ctx, func(tx db.Transaction) error {
		documents, err := tx.ListAgentChunkTexts(agentId, 100000)
		if err != nil {
			return err
		}
		documentIds := make([]string, 0, len(documents))
		for documentId := range documents {
			documentIds = append(documentIds, documentId)
		}
		found, err := tx.GetAgentDocuments(agentId, documentIds)
		if err != nil {
			return err
		}
		for _, document := range found {
			// A record is filed under its file and its id.
			if _, recordId, ok := strings.Cut(document.ExternalID, "#"); ok && recordId != "" {
				origins.documents[document.ID] = recordId
			}
		}
		conversations, err := tx.ListAgentConversations(agentId, []models.AgentConversationKind{models.AgentConversationNamed}, &db.Options{Limit: 10000})
		if err != nil {
			return err
		}
		for _, conversation := range conversations {
			origins.conversations[conversation.ID] = conversation.Title
			messages, err := tx.ListAgentMessages(conversation.ID, &db.Options{Limit: 10000})
			if err != nil {
				return err
			}
			for index, message := range messages {
				origins.messages[message.ID] = conversation.Title + "#" + strconv.Itoa(index+1)
			}
		}
		return nil
	})
	return origins, err
}

// traceScenarioEvidence follows the carried facts that say an expected
// claim back to the inputs they rest on.
func traceScenarioEvidence(ctx context.Context, database db.Database, agentId string, origins *scenarioOrigins, carried []*RecalledPage, question *ScenarioQuestion) (*ScenarioEvidenceReport, error) {
	report := &ScenarioEvidenceReport{NamedEvidence: question.Evidence, CitedEvidence: []string{}}
	var supporting []*models.AgentFact
	for _, page := range carried {
		for _, fact := range page.Facts {
			for _, claim := range question.Expects {
				if scenarioClaimReaches(claim, page.Path) && len(claim.Words) > 0 && scenarioSays(fact.Text, claim.Words) {
					supporting = append(supporting, fact)
					break
				}
			}
		}
	}
	report.SupportingFactCount = len(supporting)
	cited := map[string]bool{}
	err := database.TransactionContext(ctx, func(tx db.Transaction) error {
		for _, fact := range supporting {
			inputs, err := scenarioInputsOf(tx, agentId, origins, fact, map[string]bool{})
			if err != nil {
				return err
			}
			if len(inputs) == 0 {
				report.UnsourcedFactCount++
			}
			for _, input := range inputs {
				cited[input] = true
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for input := range cited {
		report.CitedEvidence = append(report.CitedEvidence, input)
	}
	sort.Strings(report.CitedEvidence)
	report.IndependentSourceCount = len(report.CitedEvidence)
	for _, named := range question.Evidence {
		if !scenarioEvidenceCited(named, origins.threads[named], cited) {
			report.MissingEvidence = append(report.MissingEvidence, named)
		}
	}
	report.IsEvidenceTraced = len(question.Evidence) > 0 && len(report.MissingEvidence) == 0
	return report, nil
}

// scenarioInputsOf is the inputs a fact rests on. Evidence of the agent's
// own reasoning is followed to the facts it names; a page it names, or a
// fact already followed, adds nothing.
func scenarioInputsOf(tx db.Transaction, agentId string, origins *scenarioOrigins, fact *models.AgentFact, followed map[string]bool) ([]string, error) {
	if followed[fact.ID] {
		return nil, nil
	}
	followed[fact.ID] = true
	var inputs []string
	var derivedFrom []string
	for _, evidence := range fact.Evidence {
		switch evidence.Kind {
		case models.EvidenceDocument:
			if recordId, ok := origins.documents[evidence.ID]; ok {
				inputs = append(inputs, recordId)
			}
		case models.EvidenceConversation:
			if reference, ok := origins.messages[evidence.ID]; ok {
				inputs = append(inputs, reference)
			} else if stepId, ok := origins.conversations[evidence.ID]; ok {
				inputs = append(inputs, stepId)
			}
		case models.EvidenceMemory:
			derivedFrom = append(derivedFrom, evidence.ID)
		}
	}
	if len(derivedFrom) == 0 {
		return inputs, nil
	}
	facts, err := tx.GetAgentFacts(agentId, derivedFrom)
	if err != nil {
		return nil, err
	}
	for _, source := range facts {
		more, err := scenarioInputsOf(tx, agentId, origins, source, followed)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, more...)
	}
	return inputs, nil
}

// scenarioEvidenceCited is whether a named input is cited. A chat post is
// met by its thread, and a step named whole by any of its messages.
func scenarioEvidenceCited(named, thread string, cited map[string]bool) bool {
	if cited[named] || (thread != "" && cited[thread]) {
		return true
	}
	if strings.Contains(named, "#") {
		return false
	}
	for input := range cited {
		if strings.HasPrefix(input, named+"#") {
			return true
		}
	}
	return false
}

// scenarioClaimReaches is whether a page is the one a claim names or under
// it; any page when it names none.
func scenarioClaimReaches(claim *ScenarioClaim, path string) bool {
	return claim.Path == "" || strings.EqualFold(path, claim.Path) || strings.HasPrefix(strings.ToLower(path), strings.ToLower(claim.Path)+"/")
}
