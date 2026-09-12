package tools

import (
	"context"

	"github.com/ziyan/teanode/internal/models"
)

// Remembering is what a run offers the memory tool beyond the store: the
// meaning of a memory, where the deployment has a model that can say it.
// A run without one does not implement it, and the tool works as it
// always did.
type Remembering interface {
	// NoteMemory gives a memory its vector and answers the memories
	// already near enough in meaning to be the same fact kept twice,
	// nearest first. Nothing comes back where there is no embedding
	// model, or where nothing is that near.
	NoteMemory(ctx context.Context, memory *models.AgentMemory) []*models.AgentMemory
}
