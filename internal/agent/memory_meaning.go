package agent

import (
	"context"
	"sort"
	"strings"

	"github.com/ziyan/teanode/internal/llm"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// Finding a memory by what it means, not by the words it happens to use.
// A person who says "the boat" is asking about the memory that says
// "Kittiwake", and no amount of word matching will join those two. Each
// memory gets a vector when it is written, the turn's words get one when
// it begins, and the nearest are put in front of the model beside
// whatever the words themselves found.
//
// It is an addition, never a replacement: a deployment with no embedding
// model configured recalls by words alone and nothing here runs.

// The bounds.
const (
	// memoryEmbedCharacters is how much of a memory is embedded, which is
	// all of one worth keeping.
	memoryEmbedCharacters = 4000

	// memoryNeighbours is how many of the nearest memories a turn brings
	// back, and memoryFloor how near one must be to be worth bringing:
	// below it the answer is "nothing of yours is about this".
	memoryNeighbours = 5
	memoryFloor      = 0.25

	// memoryTwins is how near two memories must be for one to be called a
	// likely copy of the other when it is written.
	memoryTwins = 0.92

	// memoryBackfill is how many memories without a vector are given one
	// on a turn, so an agent that has been remembering for months catches
	// up over a few conversations rather than in one long pause.
	memoryBackfill = 10
)

// memoryText is what is embedded of a memory: what it is called, what it
// says, and what it was tagged with.
func memoryText(memory *models.AgentMemory) string {
	text := strings.TrimSpace(memory.Title + "\n" + memory.Content)
	if len(memory.Tags) > 0 {
		text += "\n" + strings.Join(memory.Tags, " ")
	}
	if len(text) > memoryEmbedCharacters {
		text = text[:memoryEmbedCharacters]
	}
	return text
}

// EmbedMemories gives vectors to memories that have none, or whose vector
// an older model made, and says how many it wrote. Nothing happens where
// no embedding model is configured.
func (self *Agent) EmbedMemories(ctx context.Context, agent *models.Agent, limit int) (int, error) {
	embedder, model, modelName, ok := self.embedderFor()
	if !ok {
		return 0, nil
	}
	var waiting []*models.AgentMemory
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		waiting, err = tx.ListAgentMemoriesWithoutVector(agent.ID, modelName, limit)
		return err
	}); err != nil {
		return 0, err
	}
	if len(waiting) == 0 {
		return 0, nil
	}
	texts := make([]string, 0, len(waiting))
	for _, memory := range waiting {
		texts = append(texts, memoryText(memory))
	}
	configuration := self.settings.Configuration()
	callContext, cancel := context.WithTimeout(ctx, configuration.Agent.Limits.RequestTimeout.Duration())
	defer cancel()
	vectors, usage, err := embedder.Embed(callContext, model, texts)
	RecordUsage(self.settings.Database, agent.ID, "", modelName, "embed", usage)
	if err != nil {
		return 0, err
	}
	written := 0
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		for index, memory := range waiting {
			if index >= len(vectors) || len(vectors[index]) == 0 {
				continue
			}
			if err := tx.PutAgentMemoryVector(memory.ID, modelName, vectors[index]); err != nil {
				return err
			}
			written++
		}
		return nil
	}); err != nil {
		return written, err
	}
	return written, nil
}

// embedderFor is the embedding model as this deployment has it, and
// whether there is one at all.
func (self *Agent) embedderFor() (embedder llm.Embedder, model, modelName string, ok bool) {
	configuration := self.settings.Configuration()
	if self.settings.Registry == nil || configuration.Agent.Models.Embedding == "" {
		return nil, "", "", false
	}
	found, name, err := self.settings.Registry.Embedding()
	if err != nil {
		log.Warningf("no embedding model for memories: %s", err)
		return nil, "", "", false
	}
	return found, name, configuration.Agent.Models.Embedding, true
}

// nearestMemories are the memories nearest in meaning to the words, best
// first. Nothing comes back where there is no embedding model, no vector
// yet, or nothing near enough to be about the same thing.
func (self *AskRun) nearestMemories(ctx context.Context, words string) []*models.AgentMemory {
	embedder, model, modelName, ok := self.agent.embedderFor()
	if !ok || strings.TrimSpace(words) == "" {
		return nil
	}
	var candidates []*models.AgentMemory
	if err := self.agent.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		candidates, err = tx.ListAgentMemoriesWithVector(self.settings.Agent.ID, modelName)
		return err
	}); err != nil {
		log.Warningf("cannot read the memories of %q: %s", self.settings.Owner.Username, err)
		return nil
	}
	if len(candidates) == 0 {
		return nil
	}
	if len(words) > memoryEmbedCharacters {
		words = words[:memoryEmbedCharacters]
	}
	configuration := self.agent.settings.Configuration()
	callContext, cancel := context.WithTimeout(ctx, configuration.Agent.Limits.RequestTimeout.Duration())
	defer cancel()
	vectors, usage, err := embedder.Embed(callContext, model, []string{words})
	RecordUsage(self.agent.settings.Database, self.settings.Agent.ID, "", modelName, "ask", usage)
	if err != nil || len(vectors) == 0 {
		if err != nil {
			log.Warningf("cannot embed the turn to recall by meaning: %s", err)
		}
		return nil
	}
	return nearest(vectors[0], candidates, memoryNeighbours)
}

// nearest ranks memories by cosine against a vector, keeping only those
// near enough to be about the same thing.
func nearest(query []float32, candidates []*models.AgentMemory, limit int) []*models.AgentMemory {
	queryNorm := norm(query)
	if queryNorm == 0 {
		return nil
	}
	type scored struct {
		memory *models.AgentMemory
		score  float64
	}
	ranked := make([]scored, 0, len(candidates))
	for _, candidate := range candidates {
		if len(candidate.Vector) != len(query) {
			continue
		}
		candidateNorm := norm(candidate.Vector)
		if candidateNorm == 0 {
			continue
		}
		var dot float64
		for index := range query {
			dot += float64(query[index]) * float64(candidate.Vector[index])
		}
		if score := dot / (queryNorm * candidateNorm); score >= memoryFloor {
			ranked = append(ranked, scored{memory: candidate, score: score})
		}
	}
	sort.SliceStable(ranked, func(left, right int) bool { return ranked[left].score > ranked[right].score })
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	found := make([]*models.AgentMemory, 0, len(ranked))
	for _, entry := range ranked {
		found = append(found, entry.memory)
	}
	return found
}

// NoteMemory gives a memory its vector as it is written, and answers
// whatever was already near enough to be the same fact kept twice. The
// prompt asks the model to search before it adds; this is what catches
// the times it does not.
func (self *AskRun) NoteMemory(ctx context.Context, memory *models.AgentMemory) []*models.AgentMemory {
	embedder, model, modelName, ok := self.agent.embedderFor()
	if !ok || memory == nil {
		return nil
	}
	configuration := self.agent.settings.Configuration()
	callContext, cancel := context.WithTimeout(ctx, configuration.Agent.Limits.RequestTimeout.Duration())
	defer cancel()
	vectors, usage, err := embedder.Embed(callContext, model, []string{memoryText(memory)})
	RecordUsage(self.agent.settings.Database, self.settings.Agent.ID, "", modelName, "ask", usage)
	if err != nil || len(vectors) == 0 {
		if err != nil {
			log.Warningf("cannot embed a memory: %s", err)
		}
		return nil
	}
	var candidates []*models.AgentMemory
	if err := self.agent.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		if err := tx.PutAgentMemoryVector(memory.ID, modelName, vectors[0]); err != nil {
			return err
		}
		var err error
		candidates, err = tx.ListAgentMemoriesWithVector(self.settings.Agent.ID, modelName)
		return err
	}); err != nil {
		log.Warningf("cannot keep a memory's vector: %s", err)
		return nil
	}
	var others []*models.AgentMemory
	for _, candidate := range candidates {
		if candidate.ID != memory.ID {
			others = append(others, candidate)
		}
	}
	twins := nearest(vectors[0], others, memoryNeighbours)
	kept := make([]*models.AgentMemory, 0, len(twins))
	for _, twin := range twins {
		if similarity(vectors[0], twin.Vector) >= memoryTwins {
			kept = append(kept, twin)
		}
	}
	return kept
}

// similarity is the cosine between two vectors, and zero where they
// cannot be compared.
func similarity(left, right []float32) float64 {
	if len(left) != len(right) {
		return 0
	}
	leftNorm, rightNorm := norm(left), norm(right)
	if leftNorm == 0 || rightNorm == 0 {
		return 0
	}
	var dot float64
	for index := range left {
		dot += float64(left[index]) * float64(right[index])
	}
	return dot / (leftNorm * rightNorm)
}
