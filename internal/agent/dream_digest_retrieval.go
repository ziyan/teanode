package agent

import (
	"context"
	"strings"

	"github.com/ziyan/teanode/internal/agent/reading"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

type digestDocument struct {
	DocumentID string
	Heading    string
	Opening    string
}

type digestMaterial struct {
	IndexLines []string
	Documents  []digestDocument
	SourceName string
	SourceRoot string
}

func (self *Agent) retrieveDigestMaterial(ctx context.Context, run *Run, documents []*models.AgentDocument, isCoarse bool) *digestMaterial {
	material := &digestMaterial{}
	if err := run.Database().TransactionContext(ctx, func(transaction db.Transaction) error {
		lines, err := memoryLines(transaction, run.Agent.ID, models.AudienceAsk, 10, false)
		material.IndexLines = lines
		return err
	}); err != nil {
		log.Debugf("cannot read the index for a dream: %s", err)
	}
	for _, document := range documents {
		heading := document.Cite()
		if author := document.Author(); author != "" {
			heading += " — by " + author
		}
		if document.HappenedAt != nil {
			heading += " — " + document.HappenedAt.Format("2 Jan 2006")
		}
		material.Documents = append(material.Documents, digestDocument{DocumentID: document.ID, Heading: heading, Opening: self.openingOf(ctx, run, document, isCoarse)})
	}
	if len(documents) > 0 {
		_ = run.Database().TransactionContext(ctx, func(transaction db.Transaction) error {
			source, err := transaction.GetAgentSource(run.Agent.ID, documents[0].SourceID)
			if err == nil && source != nil {
				material.SourceName, material.SourceRoot = source.Name, strings.Trim(source.RootPath, "/")
			}
			return nil
		})
	}
	return material
}

// openingOf is as much of a document as the digest reads: its first
// passage, or its title alone where the night is working coarsely.
func (self *Agent) openingOf(ctx context.Context, run *Run, document *models.AgentDocument, coarse bool) string {
	if coarse {
		return ""
	}
	var chunks []*models.AgentChunk
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		chunks, err = tx.ListAgentChunks(run.Agent.ID, document.ID)
		return err
	}); err != nil || len(chunks) == 0 {
		return ""
	}
	return cutRunes(chunks[0].Text, 1200)
}

// chatNamesOf is what the person may be called in a chat archive: their
// username, and each word of their name, in lower case. A thread whose
// participants include one of these is one they took part in.
func chatNamesOf(owner *models.User) []string {
	return reading.ChatNamesOf(owner)
}
