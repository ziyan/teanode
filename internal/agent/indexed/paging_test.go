package indexed_test

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/indexed"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// passageCount is how many passages the words find: more than the first
// round of a search reads by words, so that paging has to go past it.
const passageCount = 120

// meaningCount is how many of them the meaning finds. Fewer than the
// passages, as when the similarity floor leaves the rest out.
const meaningCount = 30

// nearMeaning is a meaning that finds every fourth passage, the last
// first, and leaves the rest out as a similarity floor would.
type nearMeaning struct {
	chunks         []*models.AgentChunk
	hasVectorIndex bool
}

func (self *nearMeaning) near() []*models.AgentChunk {
	var near []*models.AgentChunk
	for _, chunk := range self.chunks {
		if chunk.Number%4 == 0 {
			near = append(near, chunk)
		}
	}
	sort.Slice(near, func(left, right int) bool { return near[left].Number > near[right].Number })
	return near
}

func (self *nearMeaning) SearchKnowledgeByMeaning(ctx context.Context, sourceIds []string, documentPrefix string, words string, limit int) ([]*models.AgentChunk, bool) {
	if !self.hasVectorIndex {
		return nil, false
	}
	near := self.near()
	return near[:min(limit, len(near))], true
}

func (self *nearMeaning) RankChunksByMeaning(ctx context.Context, words string, chunks []*models.AgentChunk, limit int) []*models.AgentChunk {
	isFound := map[string]bool{}
	for _, chunk := range chunks {
		isFound[chunk.ID] = true
	}
	var ordered []*models.AgentChunk
	isOrdered := map[string]bool{}
	for _, chunk := range self.near() {
		if isFound[chunk.ID] {
			ordered = append(ordered, chunk)
			isOrdered[chunk.ID] = true
		}
	}
	for _, chunk := range chunks {
		if !isOrdered[chunk.ID] {
			ordered = append(ordered, chunk)
		}
	}
	return ordered[:min(limit, len(ordered))]
}

// indexedPassages is an agent with one document of passageCount passages,
// every one of them about kestrels, some more than others.
func indexedPassages(test *testing.T) (db.Database, string, []*models.AgentChunk, func()) {
	test.Helper()
	database, release := dbtest.AcquireDatabase(test)
	var agentId string
	var chunks []*models.AgentChunk
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "paging-owner", Name: "Alice Example"})
		if err != nil {
			test.Fatalf("CreateUser: %s", err)
		}
		agent, err := tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Name: "Bertie"})
		if err != nil {
			test.Fatalf("CreateAgent: %s", err)
		}
		agentId = agent.ID
		source, err := tx.PutAgentSource(&models.AgentKnowledgeSource{
			AgentID: agentId, Kind: models.SourceArchive, Name: "field notes", Enabled: true,
			Specification: models.AgentKnowledgeSpecification{Computer: "laptop", Path: "~/notes", Format: models.FormatRecords},
		})
		if err != nil {
			test.Fatalf("PutAgentSource: %s", err)
		}
		document, err := tx.PutAgentDocument(&models.AgentDocument{
			AgentID: agentId, SourceID: source.ID, ExternalID: "notes/kestrels.md",
			Kind: models.DocumentFile, Title: "kestrels", Hash: "hash-of-kestrels",
		})
		if err != nil {
			test.Fatalf("PutAgentDocument: %s", err)
		}
		// Three strengths of match, so that many passages tie on the words
		// and the order between them is the database's to keep.
		passages := make([]*models.AgentChunk, 0, passageCount)
		for number := 1; number <= passageCount; number++ {
			passages = append(passages, &models.AgentChunk{
				Text:      strings.Repeat("kestrel ", 1+number%3) + fmt.Sprintf("seen from hide %d", number),
				Segmented: true,
			})
		}
		if err := tx.ReplaceAgentChunks(document, passages); err != nil {
			test.Fatalf("ReplaceAgentChunks: %s", err)
		}
		if chunks, err = tx.ListAgentChunks(agentId, document.ID); err != nil {
			test.Fatalf("ListAgentChunks: %s", err)
		}
	})
	if len(chunks) != passageCount {
		test.Fatalf("the document was cut into %d passages, not %d", len(chunks), passageCount)
	}
	return database, agentId, chunks, release
}

// readEveryPage pages through one search, limit passages at a time, and
// answers with the passages in the order the pages showed them.
func readEveryPage(test *testing.T, database db.Database, meaning indexed.Meaning, agentId string, limit int) []string {
	test.Helper()
	var seen []string
	offset := 0
	for page := 0; page <= passageCount; page++ {
		var found *indexed.Found
		dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
			var err error
			if found, err = indexed.Search(context.Background(), tx, meaning, agentId,
				indexed.Query{Words: "kestrel", Limit: limit, Offset: offset}); err != nil {
				test.Fatalf("Search: %s", err)
			}
		})
		for _, passage := range found.Passages {
			seen = append(seen, fmt.Sprintf("%s#%d", passage.DocumentID, passage.Number))
		}
		if found.Offset != offset {
			test.Fatalf("the page says it starts at %d, and was asked for %d", found.Offset, offset)
		}
		if found.NextOffset == 0 {
			if found.MoreCount != 0 || found.IsMoreCountLowerBound {
				test.Fatalf("the last page says there are %d more", found.MoreCount)
			}
			return seen
		}
		if found.NextOffset != offset+len(found.Passages) {
			test.Fatalf("the next page starts at %d, after %d passages from %d", found.NextOffset, len(found.Passages), offset)
		}
		if found.MoreCount < 1 {
			test.Fatalf("a page with a next one says there are %d more", found.MoreCount)
		}
		offset = found.NextOffset
	}
	test.Fatalf("paging did not reach the end in %d pages", passageCount)
	return nil
}

// Paging through a search shows every passage it can find once, in the
// order of the ranking, whatever size the pages are.
//
// The ranking fuses what the words and the meaning found, and a fusion of
// deeper lists is a different ranking: read naively, the second page of
// one would repeat a passage the first page showed and skip another.
func TestPagingThroughASearchShowsEveryPassageOnceInOrder(test *testing.T) {
	test.Parallel()
	database, agentId, chunks, release := indexedPassages(test)
	defer release()

	for _, meaning := range []struct {
		name    string
		meaning indexed.Meaning
	}{
		{"words alone", nil},
		{"words and meaning fused", &nearMeaning{chunks: chunks, hasVectorIndex: true}},
		{"words re-ranked by meaning", &nearMeaning{chunks: chunks}},
	} {
		small := readEveryPage(test, database, meaning.meaning, agentId, 7)
		large := readEveryPage(test, database, meaning.meaning, agentId, 25)
		if len(small) != passageCount {
			test.Errorf("%s: pages of 7 showed %d passages of %d", meaning.name, len(small), passageCount)
		}
		isShown := map[string]bool{}
		for _, passage := range small {
			if isShown[passage] {
				test.Errorf("%s: %s was shown twice", meaning.name, passage)
			}
			isShown[passage] = true
		}
		if strings.Join(small, " ") != strings.Join(large, " ") {
			test.Errorf("%s: pages of 7 and pages of 25 are one ranking read differently, and they differ:\n%v\n%v",
				meaning.name, small, large)
		}
	}
}

// A page says how many more passages there are: "at least" while the
// search has not read to the end of its lists, an exact count once it has.
func TestAPageSaysHowManyMorePassagesThereAre(test *testing.T) {
	test.Parallel()
	database, agentId, chunks, release := indexedPassages(test)
	defer release()
	meaning := &nearMeaning{chunks: chunks, hasVectorIndex: true}

	search := func(offset int) *indexed.Found {
		var found *indexed.Found
		dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
			var err error
			if found, err = indexed.Search(context.Background(), tx, meaning, agentId,
				indexed.Query{Words: "kestrel", Offset: offset}); err != nil {
				test.Fatalf("Search: %s", err)
			}
		})
		return found
	}

	first := search(0)
	if len(first.Passages) != indexed.SearchLimit || first.NextOffset != indexed.SearchLimit {
		test.Fatalf("the first page has %d passages and reads on from %d", len(first.Passages), first.NextOffset)
	}
	// The first round read 48 passages by words and 24 by meaning, which
	// is not all of either, so the count is a floor.
	if !first.IsMoreCountLowerBound || first.MoreCount < indexed.SearchLimit {
		test.Errorf("the first page says %d more, lower bound %v", first.MoreCount, first.IsMoreCountLowerBound)
	}

	last := search(passageCount - 5)
	if len(last.Passages) != 5 || last.NextOffset != 0 || last.MoreCount != 0 {
		test.Errorf("the last page has the last 5 passages and nothing after: %d passages, next %d, %d more",
			len(last.Passages), last.NextOffset, last.MoreCount)
	}
	// The meaning found meaningCount passages and left the rest under the
	// floor; the words found every one of them, so nothing past the end is
	// claimed, and nothing before it is missing.
	beforeLast := search(passageCount - 20)
	if beforeLast.MoreCount != 8 || beforeLast.IsMoreCountLowerBound || beforeLast.NextOffset != passageCount-8 {
		test.Errorf("twelve before the last eight say 8 more exactly: %d more, lower bound %v, next %d",
			beforeLast.MoreCount, beforeLast.IsMoreCountLowerBound, beforeLast.NextOffset)
	}

	past := search(passageCount + 10)
	if len(past.Passages) != 0 || past.NextOffset != 0 {
		test.Errorf("a page past the end is empty and has no next: %+v", past)
	}
	if nearCount := len(meaning.near()); nearCount != meaningCount {
		test.Errorf("the meaning finds %d passages, not %d", nearCount, meaningCount)
	}
}
