package agent

import (
	"context"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// embedChunks gives vectors to chunks that have none, and says how many
// it wrote and how many are left.
//
// Embedding is measured against its own budget rather than the day's
// token allowance. The first pass over a person's checkout and chat
// archive is on the order of a hundred million tokens at a thousandth of
// the price of a conversation; counting it against the same cap would
// stop the load on its first night and every night after.
func (self *Agent) embedChunks(ctx context.Context, agent *models.Agent, most int) (int, int64, error) {
	_, _, modelName, _, ok := self.embedderFor()
	if !ok {
		return 0, 0, nil
	}
	if most <= 0 {
		most = 2000
	}
	written := 0
	for written < most {
		if ctx.Err() != nil {
			break
		}
		batch := ingestEmbedBatch
		if remaining := most - written; remaining < batch {
			batch = remaining
		}
		var chunks []*models.AgentChunk
		if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
			chunks, err = tx.ListAgentChunksWithoutVector(agent.ID, modelName, batch)
			return err
		}); err != nil {
			return written, 0, err
		}
		if len(chunks) == 0 {
			break
		}
		texts := make([]string, 0, len(chunks))
		for _, chunk := range chunks {
			texts = append(texts, chunk.Text)
		}
		vectors, _, ok := self.embed(ctx, agent.ID, "ingest", texts)
		if !ok {
			break
		}
		writing := make([]db.AgentChunkVector, 0, len(chunks))
		for index, chunk := range chunks {
			if index >= len(vectors) || len(vectors[index]) == 0 {
				continue
			}
			writing = append(writing, db.AgentChunkVector{
				ChunkID: chunk.ID, SourceID: chunk.SourceID, Model: modelName, Vector: vectors[index],
			})
		}
		// A round that wrote nothing is a round that will write nothing
		// next time either: the same passages come back from the same
		// query, and the provider answered -- with empty vectors, which
		// some do for text they refuse -- so there is no error to stop
		// on. Without this the loop turned for as long as the job had,
		// asking the provider for the same batch over and over.
		if len(writing) == 0 {
			log.Warningf("the embedding model answered with nothing for %d passage(s) of agent %s", len(chunks), agent.ID)
			break
		}
		if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
			// In a fixed order, because two ingest runs happen at once and
			// their batches overlap.
			return tx.PutAgentChunkVectors(agent.ID, writing)
		}); err != nil {
			return written, 0, err
		}
		written += len(writing)
	}
	var left int64
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		left, err = tx.CountAgentChunksWithoutVector(agent.ID, modelName)
		return err
	}); err != nil {
		return written, 0, err
	}
	return written, left, nil
}
