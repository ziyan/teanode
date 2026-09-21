package agent

import (
	"context"
	"sync"

	"github.com/ziyan/teanode/internal/agent/indexed"
	"github.com/ziyan/teanode/internal/models"
)

// SearchKnowledgeByMeaning is the passages nearest some words, and
// whether the database ranked them itself.
//
// Part of tools.KnowledgeSearching. False means this deployment has no
// vector index, and the caller should fall back to re-ranking what the
// words found: reading half a million vectors into memory to sort them is
// not a search.
func (self *AskRun) SearchKnowledgeByMeaning(ctx context.Context, sourceIds []string, words string, limit int) ([]*models.AgentChunk, bool) {
	return self.agent.searchChunksByMeaning(ctx, self.settings.Agent.ID,
		self.meaningOfQuestion(ctx, "search", words), sourceIds, limit)
}

// RankChunksByMeaning puts a set the words found into the order the
// meaning wants. What a deployment with no vector index does instead of a
// vector search: it finds everything the words find, in a better order,
// and misses a paraphrase that shares no word with the question.
func (self *AskRun) RankChunksByMeaning(ctx context.Context, words string, chunks []*models.AgentChunk, limit int) []*models.AgentChunk {
	if len(chunks) <= 1 {
		return chunks
	}
	return self.agent.rankChunksByMeaning(ctx, self.settings.Agent.ID,
		self.meaningOfQuestion(ctx, "search", words), chunks, limit)
}

// KnowledgeMeaning is the meaning half of a search of what was indexed,
// for a caller that is not in a turn: the API, and the command line and
// the dashboard through it.
//
// The person searching their own documents is ranked the same way their
// agent's tool is, which is the whole point of one search shared between
// the surfaces. It embeds the question, which a turn does once and keeps;
// there is nothing to keep here, so each search pays for its own.
func (self *Agent) KnowledgeMeaning(agentId string) indexed.Meaning {
	if self == nil || agentId == "" {
		return nil
	}
	return &knowledgeMeaning{agent: self, agentId: agentId}
}

type knowledgeMeaning struct {
	agent   *Agent
	agentId string

	// asked is the question already embedded, kept because a search that
	// finds no vector index falls back to re-ranking and asks for the
	// same words again. One of these is one search, so one answer is all
	// there is to keep; a turn keeps its own for the whole turn.
	mutex sync.Mutex
	words string
	asked *meaning
	tried bool
}

func (self *knowledgeMeaning) SearchKnowledgeByMeaning(ctx context.Context, sourceIds []string, words string, limit int) ([]*models.AgentChunk, bool) {
	return self.agent.searchChunksByMeaning(ctx, self.agentId, self.questionOf(ctx, words), sourceIds, limit)
}

func (self *knowledgeMeaning) RankChunksByMeaning(ctx context.Context, words string, chunks []*models.AgentChunk, limit int) []*models.AgentChunk {
	if len(chunks) <= 1 {
		return chunks
	}
	return self.agent.rankChunksByMeaning(ctx, self.agentId, self.questionOf(ctx, words), chunks, limit)
}

func (self *knowledgeMeaning) questionOf(ctx context.Context, words string) *meaning {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.tried && self.words == words {
		// Including a nil: a deployment with no embedder, or a call that
		// failed, is not worth asking twice for one search.
		return self.asked
	}
	self.words = words
	self.asked = self.agent.meaningOf(ctx, self.agentId, "search", words)
	self.tried = true
	return self.asked
}
