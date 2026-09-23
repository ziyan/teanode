// Package indexed is searching and reading what a person's sources put in
// front of their agent: the documents a pass read, and the passages they
// were cut into.
//
// One search, in a package of its own, because the agent's knowledge tool
// cannot import the agent and the API cannot import the tool. The tool,
// `teanode agent knowledge search` and the dashboard all call Search and
// Read, so what one of them finds the others find, in the same order;
// each surface decides only how to print it.
package indexed

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// The bounds.
const (
	// SearchLimit is how many passages a search answers with when the
	// caller asks for no number, and SearchMost the ceiling on what one
	// can ask for: a passage is two thousand characters, so a hundred of
	// them is already more than anybody reads.
	SearchLimit = 12
	SearchMost  = 100

	// ReadLimit is how much of a document one read returns by default,
	// and ReadMost the ceiling. A read that does not reach the end says
	// where to start the next one.
	ReadLimit = 6000
	ReadMost  = 100000

	// SymbolLimit is how many definitions an identifier in the words is
	// worth answering with.
	SymbolLimit = 10

	// PassageShown is how much of a passage a surface shows while it is
	// listing several: enough to see whether it is the right one, short
	// enough that a dozen of them fill neither a turn nor a terminal. The
	// passage comes back whole; this is what the printing cuts it to.
	PassageShown = 700

	// fuseConstant is the constant of the reciprocal rank fusion below.
	// Sixty is what the method was published with, and what the graph's
	// own fusion uses.
	fuseConstant = 60
)

// Meaning is the half of a search that knows what a passage means rather
// than which words are in it. The agent's own run implements it; a caller
// with no model to embed a question with passes nil and gets the words
// alone, which is what a deployment with no embedder has always had.
type Meaning interface {
	// SearchKnowledgeByMeaning is the passages nearest the words, and
	// whether the database ranked them itself. False means the caller
	// should fall back to re-ranking what the words found.
	SearchKnowledgeByMeaning(ctx context.Context, sourceIds []string, words string, limit int) ([]*models.AgentChunk, bool)

	// RankChunksByMeaning puts a set the words found into the order the
	// meaning wants.
	RankChunksByMeaning(ctx context.Context, words string, chunks []*models.AgentChunk, limit int) []*models.AgentChunk
}

// Query is what to look for.
type Query struct {
	// Words are what was typed: words, a name, or an identifier out of a
	// log.
	Words string

	// SourceIds narrows the search to those sources; empty searches all
	// of them.
	SourceIds []string

	// Limit is how many passages to answer with; zero is SearchLimit.
	Limit int
}

// Passage is one passage a search found, with everything needed to cite
// the document it came from without reading that document.
type Passage struct {
	DocumentID string `json:"documentId"`

	// ExternalID is what the source calls it: a path in a checkout, a
	// file and a record in an archive.
	ExternalID string `json:"externalId"`

	// Title is how the document is named in an answer, which says
	// "(private)" where it came from somewhere only this person can see.
	Title string `json:"title"`

	URL    string `json:"url"`
	Kind   string `json:"kind"`
	Author string `json:"author"`

	// SourceID is the source it came from and Source that source's name.
	SourceID string `json:"sourceId"`
	Source   string `json:"source"`

	// HappenedAt is when the document says it happened, where it says.
	HappenedAt *time.Time `json:"happenedAt"`

	// Private marks a document not to be quoted to anybody else.
	Private bool `json:"private"`

	// Number is which passage of the document this is, so that a citation
	// can name the passage as well as the document.
	Number int `json:"number"`

	// Text is the whole passage, uncut. A surface with a turn to fill
	// shortens it; one with a page does not have to.
	Text string `json:"text"`

	// Score is what put this passage where it is in the list. It is a
	// rank, not a similarity: it says this passage beat that one in this
	// search, and means nothing between two searches.
	Score float64 `json:"score"`
}

// Definition is one place an identifier in the words is defined.
//
// A name pasted out of a log resolves to a file and a line exactly, where
// a cosine would put twenty vaguely related files in front of it.
type Definition struct {
	Symbol string `json:"symbol"`
	Kind   string `json:"kind"`
	Line   int    `json:"line"`

	DocumentID string `json:"documentId"`
	ExternalID string `json:"externalId"`
	Title      string `json:"title"`
}

// Found is what a search found.
type Found struct {
	Passages    []*Passage    `json:"passages"`
	Definitions []*Definition `json:"definitions"`

	// Meaningful says the passages were ranked by what they mean as well
	// as by the words in them. False is a deployment with no embedding
	// model, or one whose database cannot rank vectors: what the words
	// find, which misses a paraphrase sharing no word.
	Meaningful bool `json:"meaningful"`
}

// Extract is a document and a slice of its text.
type Extract struct {
	DocumentID string `json:"documentId"`
	ExternalID string `json:"externalId"`
	Title      string `json:"title"`
	URL        string `json:"url"`
	Kind       string `json:"kind"`
	Author     string `json:"author"`

	SourceID string `json:"sourceId"`
	Source   string `json:"source"`

	HappenedAt *time.Time `json:"happenedAt"`
	Private    bool       `json:"private"`

	// From is where in the document this slice starts and Text is the
	// slice. Total is how long the whole document is, so a reader knows
	// how much is left.
	From  int    `json:"from"`
	Text  string `json:"text"`
	Total int    `json:"total"`

	// Next is where to start the read that carries on from this one, and
	// zero where this slice reached the end.
	Next int `json:"next"`
}

// Search finds passages, and the definitions of any identifier among the
// words.
//
// The transaction reads the passages, the documents and the sources; the
// Meaning, where there is one, opens a connection of its own, as it does
// in a turn.
func Search(ctx context.Context, tx db.Transaction, meaning Meaning, agentId string, query Query) (*Found, error) {
	words := strings.TrimSpace(query.Words)
	found := &Found{Passages: []*Passage{}, Definitions: []*Definition{}}
	if words == "" {
		return found, nil
	}
	limit := query.Limit
	if limit <= 0 {
		limit = SearchLimit
	}
	if limit > SearchMost {
		limit = SearchMost
	}

	definitions, err := lookUpSymbols(tx, agentId, words)
	if err != nil {
		return nil, err
	}
	found.Definitions = definitions

	ranked, meaningful, err := findChunks(ctx, tx, meaning, agentId, query.SourceIds, words, limit)
	if err != nil {
		return nil, err
	}
	found.Meaningful = meaningful
	if len(ranked) == 0 {
		return found, nil
	}

	documents, err := documentsOf(tx, agentId, ranked)
	if err != nil {
		return nil, err
	}
	sources, err := sourceNames(tx, agentId)
	if err != nil {
		return nil, err
	}
	for _, scored := range ranked {
		document := documents[scored.chunk.DocumentID]
		if document == nil {
			// A passage whose document has gone is not a hit anybody can
			// read; a pass that deletes documents leaves nothing behind,
			// so this is the race and not the rule.
			continue
		}
		found.Passages = append(found.Passages, &Passage{
			DocumentID: document.ID, ExternalID: document.ExternalID, Title: document.Cite(),
			URL: document.URL, Kind: string(document.Kind), Author: document.Author(),
			SourceID: document.SourceID, Source: sources[document.SourceID],
			HappenedAt: document.HappenedAt, Private: document.Private,
			Number: scored.chunk.Number, Text: scored.chunk.Text, Score: scored.score,
		})
	}
	return found, nil
}

// Read is a document and the text from an offset.
//
// Nil and no error is a document this agent does not have: each surface
// says that its own way, and none of them can say it for somebody else's
// document, because the identifier is looked up under the agent it was
// asked for.
func Read(tx db.Transaction, agentId, documentId string, from, length int) (*Extract, error) {
	// A search cites "documentId#passage"; take either.
	id, _, _ := strings.Cut(strings.TrimSpace(documentId), "#")
	if id == "" {
		return nil, nil
	}
	document, err := tx.GetAgentDocument(agentId, id)
	if err != nil || document == nil {
		return nil, err
	}
	chunks, err := tx.ListAgentChunks(agentId, document.ID)
	if err != nil {
		return nil, err
	}
	var whole strings.Builder
	for _, chunk := range chunks {
		whole.WriteString(chunk.Text)
		whole.WriteByte('\n')
	}
	// Counted in characters rather than bytes: an offset into the middle
	// of a character hands back a replacement mark instead of the word it
	// was part of, and the offset this call returns is the one the next
	// call is made with.
	runes := []rune(whole.String())
	if from < 0 || from > len(runes) {
		from = 0
	}
	if length <= 0 {
		length = ReadLimit
	}
	if length > ReadMost {
		length = ReadMost
	}
	end := from + length
	if end > len(runes) {
		end = len(runes)
	}
	extract := &Extract{
		DocumentID: document.ID, ExternalID: document.ExternalID, Title: document.Cite(),
		URL: document.URL, Kind: string(document.Kind), Author: document.Author(),
		SourceID: document.SourceID, HappenedAt: document.HappenedAt, Private: document.Private,
		From: from, Text: string(runes[from:end]), Total: len(runes),
	}
	if end < len(runes) {
		extract.Next = end
	}
	if source, err := tx.GetAgentSource(agentId, document.SourceID); err != nil {
		return nil, err
	} else if source != nil {
		extract.Source = source.Name
	}
	return extract, nil
}

// scored is a passage and what put it where it is.
type scored struct {
	chunk *models.AgentChunk
	score float64
}

// findChunks is the hybrid search: words, and meaning where the
// deployment can say what a passage means.
//
// Where the database can rank vectors itself the two are separate
// searches fused by rank. Where it cannot, meaning re-ranks what the
// words found, which is honest and bounded: it finds everything the words
// find, in a better order, and misses a paraphrase sharing no word.
func findChunks(ctx context.Context, tx db.Transaction, meaning Meaning, agentId string, sourceIds []string, words string, limit int) ([]*scored, bool, error) {
	byWords, err := tx.SearchAgentChunks(agentId, sourceIds, words, limit*4)
	if err != nil {
		return nil, false, err
	}
	if meaning == nil {
		return rank(cutTo(byWords, limit)), false, nil
	}
	byMeaning, hasVectorIndex := meaning.SearchKnowledgeByMeaning(ctx, sourceIds, words, limit*2)
	if !hasVectorIndex {
		// No index: re-rank what the words found, rather than reading a
		// hundred thousand vectors into memory to sort them.
		reranked := meaning.RankChunksByMeaning(ctx, words, byWords, limit)
		return rank(cutTo(reranked, limit)), len(byWords) > 0, nil
	}
	return fuse(limit, byMeaning, byWords), true, nil
}

func cutTo(chunks []*models.AgentChunk, limit int) []*models.AgentChunk {
	if len(chunks) > limit {
		return chunks[:limit]
	}
	return chunks
}

// rank scores a single list by position, on the same scale the fusion
// below uses, so that a deployment with one search and one with two hand
// back numbers that mean the same thing.
func rank(chunks []*models.AgentChunk) []*scored {
	ranked := make([]*scored, 0, len(chunks))
	for position, chunk := range chunks {
		ranked = append(ranked, &scored{chunk: chunk, score: 1 / float64(fuseConstant+position+1)})
	}
	return ranked
}

// fuse ranks what two searches found by reciprocal rank: position rather
// than score, because a full-text rank and a cosine are not on one scale.
func fuse(limit int, lists ...[]*models.AgentChunk) []*scored {
	scores := map[string]float64{}
	byId := map[string]*models.AgentChunk{}
	var order []string
	for _, list := range lists {
		for position, chunk := range list {
			if _, seen := byId[chunk.ID]; !seen {
				order = append(order, chunk.ID)
			}
			scores[chunk.ID] += 1 / float64(fuseConstant+position+1)
			byId[chunk.ID] = chunk
		}
	}
	for index := 0; index < len(order); index++ {
		for other := index + 1; other < len(order); other++ {
			if scores[order[other]] > scores[order[index]] {
				order[index], order[other] = order[other], order[index]
			}
		}
	}
	ranked := make([]*scored, 0, limit)
	for _, id := range order {
		if len(ranked) >= limit {
			break
		}
		ranked = append(ranked, &scored{chunk: byId[id], score: scores[id]})
	}
	return ranked
}

// lookUpSymbols answers an identifier among the words exactly.
func lookUpSymbols(tx db.Transaction, agentId, words string) ([]*Definition, error) {
	var names []string
	for _, word := range strings.FieldsFunc(words, func(character rune) bool {
		return character == ' ' || character == ',' || character == '(' || character == ')' || character == '"'
	}) {
		word = strings.Trim(word, ".:;")
		if LooksLikeSymbol(word) {
			names = append(names, word)
			// A log line says "quoteworker.py:97 ComputeShippingQuote";
			// both halves are worth asking about.
			if before, _, cut := strings.Cut(word, "."); cut && before != "" {
				names = append(names, before)
			}
		}
	}
	definitions := []*Definition{}
	if len(names) == 0 {
		return definitions, nil
	}
	symbols, err := tx.LookupAgentSymbols(agentId, names, SymbolLimit)
	if err != nil || len(symbols) == 0 {
		return definitions, err
	}
	ids := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		ids = append(ids, symbol.DocumentID)
	}
	list, err := tx.GetAgentDocuments(agentId, ids)
	if err != nil {
		return nil, err
	}
	documents := make(map[string]*models.AgentDocument, len(list))
	for _, document := range list {
		documents[document.ID] = document
	}
	for _, symbol := range symbols {
		document := documents[symbol.DocumentID]
		if document == nil {
			continue
		}
		definitions = append(definitions, &Definition{
			Symbol: symbol.Symbol, Kind: symbol.Kind, Line: symbol.Line,
			DocumentID: document.ID, ExternalID: document.ExternalID, Title: document.Cite(),
		})
	}
	return definitions, nil
}

// LooksLikeSymbol says whether a word from a question is an identifier
// rather than an ordinary word: CamelCase, snake_case, a dotted name.
//
// Asked of every word of a search, so that a name pasted out of a log is
// looked up exactly before anything is ranked. Cheap and strict: a false
// positive costs one lookup that finds nothing.
func LooksLikeSymbol(word string) bool {
	if len(word) < 4 {
		return false
	}
	if strings.ContainsAny(word, "_.") && !strings.HasPrefix(word, ".") {
		return true
	}
	for index := 1; index < len(word); index++ {
		previous, current := word[index-1], word[index]
		if previous >= 'a' && previous <= 'z' && current >= 'A' && current <= 'Z' {
			return true
		}
	}
	return false
}

// documentsOf is the document each passage came from.
func documentsOf(tx db.Transaction, agentId string, ranked []*scored) (map[string]*models.AgentDocument, error) {
	ids := make([]string, 0, len(ranked))
	for _, found := range ranked {
		ids = append(ids, found.chunk.DocumentID)
	}
	documents, err := tx.GetAgentDocuments(agentId, ids)
	if err != nil {
		return nil, err
	}
	byId := make(map[string]*models.AgentDocument, len(documents))
	for _, document := range documents {
		byId[document.ID] = document
	}
	return byId, nil
}

// sourceNames is what each source is called, so a hit can say where it
// came from in the words the person named it with.
func sourceNames(tx db.Transaction, agentId string) (map[string]string, error) {
	sources, err := tx.ListAgentSources(agentId)
	if err != nil {
		return nil, err
	}
	names := make(map[string]string, len(sources))
	for _, source := range sources {
		names[source.ID] = source.Name
	}
	return names, nil
}

// Cite is how one passage is named where a citation has to be one line:
// the document's title, who wrote it, when it happened, and the
// identifier that reads it back.
func (self *Passage) Cite() string {
	var builder strings.Builder
	builder.WriteString(self.Title)
	if self.Author != "" {
		builder.WriteString(" — " + self.Author)
	}
	if self.HappenedAt != nil {
		builder.WriteString(" — " + self.HappenedAt.Format("2 Jan 2006"))
	}
	builder.WriteString("  [" + self.DocumentID + "#" + strconv.Itoa(self.Number) + "]")
	return builder.String()
}
