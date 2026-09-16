package tools

import (
	"context"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// Remembering is what a run offers the memory tool beyond the store: the
// meaning of what is written, where the deployment has a model that can
// say it. A run without one does not implement it, and the tool works as
// it always did -- by words.
type Remembering interface {
	// NoteFact gives a fact its vector as it is written and answers the
	// facts already on the same page that are near enough in meaning to be
	// the same thing said twice, nearest first. Nothing comes back where
	// there is no embedding model, or where nothing is that near.
	//
	// Merging stays within one page on purpose. Two short sentences about
	// two different people sit closer together than either does to a long
	// one about its own subject, so "near in meaning" is only evidence of
	// a duplicate when the two are already about the same thing.
	NoteFact(ctx context.Context, fact *models.AgentFact) []*models.AgentFact

	// NoteNode gives a page its vector when its words change.
	NoteNode(ctx context.Context, node *models.AgentNode)

	// ResolvePage is the page a fact belongs on: the one already there
	// under any of its names, or a new one.
	//
	// Every writer goes through this rather than making a page directly,
	// which is what keeps one thing to one page. A model asked to file
	// something about Alice will write "people/alice" one day and
	// "people/alice-chen" the next, and both should land on the page that
	// exists.
	ResolvePage(ctx context.Context, tx db.Transaction, path string, kind models.AgentNodeKind, name string) (*models.AgentNode, error)
}

// GraphSearching is finding a page or a fact by what it means rather than
// by the words it happens to use. A person asking about "the boat" means
// the page that says "Kittiwake", and no word of theirs appears in it.
//
// As with Remembering, a run on a deployment with no embedding model does
// not implement it and the search is by words alone.
type GraphSearching interface {
	SearchGraphByMeaning(ctx context.Context, words string, limit int) ([]*models.AgentNode, []*models.AgentFact)
}

// KnowledgeSearching is searching what the person pointed their agent at
// by meaning as well as by words.
//
// Two methods rather than one because the two paths are genuinely
// different. Where the database can rank vectors it does, and the words
// and the meaning are separate searches fused by rank. Where it cannot,
// reading a few hundred thousand vectors into memory to sort them is not
// a search, it is a memory leak with a result -- so the words find a
// bounded set and the meaning re-ranks that.
type KnowledgeSearching interface {
	// SearchKnowledgeByMeaning is the passages nearest the words, and
	// whether the database ranked them itself. False means the caller
	// should fall back to re-ranking.
	SearchKnowledgeByMeaning(ctx context.Context, sourceIds []string, words string, limit int) ([]*models.AgentChunk, bool)

	// RankChunksByMeaning puts a set the words found into the order the
	// meaning wants.
	RankChunksByMeaning(ctx context.Context, words string, chunks []*models.AgentChunk, limit int) []*models.AgentChunk
}
