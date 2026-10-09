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
	"errors"
	"fmt"
	"path"
	"slices"
	"sort"
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

	// fuseConstant is the constant of the reciprocal rank fusion below.
	// Sixty is what the method was published with, and what the graph's
	// own fusion uses.
	fuseConstant = 60

	// wordsPoolFirst and meaningPoolFirst are how many passages the
	// first round of a search reads by words and by meaning, before the
	// two are fused. Each further round reads twice as many as the one
	// before. They are what a search of SearchLimit passages has always
	// read, and they do not depend on the page asked for, so the first
	// page of a search is ranked the same way whichever page is asked.
	wordsPoolFirst   = SearchLimit * 4
	meaningPoolFirst = SearchLimit * 2
)

// Meaning is the half of a search that knows what a passage means rather
// than which words are in it. The agent's own run implements it; a caller
// with no model to embed a question with passes nil and gets the words
// alone, which is what a deployment with no embedder has always had.
type Meaning interface {
	// SearchKnowledgeByMeaning is the passages nearest the words, and
	// whether the database ranked them itself. False means the caller
	// should fall back to re-ranking what the words found.
	SearchKnowledgeByMeaning(ctx context.Context, sourceIds []string, documentPrefix string, words string, limit int) ([]*models.AgentChunk, bool)

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

	// Offset is how many passages of the ranking to pass over, to read
	// the page after one already shown; zero is the first page.
	Offset int

	// Directory narrows the search to what a source read under one
	// directory, written as the source's path joined with a folder in it
	// ("~/code/seedling/cmd"), the way Directories names them; the
	// source whose path holds it most closely is searched, on
	// ComputerName where that is given.
	Directory    string
	ComputerName string
}

// DirectoryHits is a directory the passages a search found are in, and
// how many: where they cluster, before any of them is read.
type DirectoryHits struct {
	// Directory is the source's path joined with the folder, which is
	// what Query.Directory takes to search inside it.
	Directory    string `json:"directory"`
	SourceID     string `json:"sourceId"`
	Source       string `json:"source"`
	PassageCount int    `json:"passageCount"`
}

// ErrNoSourceHoldsDirectory is a directory no source of the person's
// reads.
var ErrNoSourceHoldsDirectory = errors.New("no source reads that directory")

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

	// Directories are where what the search ranked is, the most first:
	// the directories of a tree of files its passages fall in, so a large
	// tree can be searched one directory at a time. Empty where they all
	// fall in one.
	Directories []*DirectoryHits `json:"directories"`

	// Meaningful says the passages were ranked by what they mean as well
	// as by the words in them. False is a deployment with no embedding
	// model, or one whose database cannot rank vectors: what the words
	// find, which misses a paraphrase sharing no word.
	Meaningful bool `json:"meaningful"`

	// Offset is how many passages of the ranking came before these.
	Offset int `json:"offset"`

	// MoreCount is how many passages the search found past these, which
	// the next offset reads. Where IsMoreCountLowerBound is set the
	// search stopped counting there and there are at least that many.
	MoreCount             int  `json:"moreCount"`
	IsMoreCountLowerBound bool `json:"isMoreCountLowerBound"`

	// NextOffset is the offset that reads the passages after these, and
	// zero where there are none.
	NextOffset int `json:"nextOffset"`
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
// A page past the first is the same ranking read on from its offset, so
// that pages read one after another show every passage once, in order.
// The definitions are answered with the first page only: they are exact
// lookups, the same on every page.
//
// The transaction reads the passages, the documents and the sources; the
// Meaning, where there is one, opens a connection of its own, as it does
// in a turn.
func Search(ctx context.Context, tx db.Transaction, meaning Meaning, agentId string, query Query) (*Found, error) {
	words := strings.TrimSpace(query.Words)
	found := &Found{Passages: []*Passage{}, Definitions: []*Definition{}, Directories: []*DirectoryHits{}}
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
	offset := max(query.Offset, 0)
	found.Offset = offset

	if offset == 0 {
		definitions, err := lookUpSymbols(tx, agentId, words)
		if err != nil {
			return nil, err
		}
		found.Definitions = definitions
	}

	// Ranked past this page by another page's worth, so that the line
	// saying what is left can say how many rather than only that there
	// is more.
	sources, err := tx.ListAgentSources(agentId)
	if err != nil {
		return nil, err
	}
	sourceIds, documentPrefix := query.SourceIds, ""
	if strings.TrimSpace(query.Directory) != "" {
		source, prefix := sourceOfDirectory(sources, query.SourceIds, query.Directory, query.ComputerName)
		if source == nil {
			return nil, fmt.Errorf("%w: %s", ErrNoSourceHoldsDirectory, query.Directory)
		}
		sourceIds, documentPrefix = []string{source.ID}, prefix
	}
	ordered, isComplete, meaningful, err := findChunks(ctx, tx, meaning, agentId, sourceIds, documentPrefix, words, offset+limit*2)
	if err != nil {
		return nil, err
	}
	found.Meaningful = meaningful
	if found.Directories, err = directoriesOf(tx, agentId, sources, ordered); err != nil {
		return nil, err
	}
	if offset >= len(ordered) {
		return found, nil
	}
	end := min(offset+limit, len(ordered))
	ranked := ordered[offset:end]
	// What is left is what paging can still read: a passage the meaning
	// passed over for being too far from the question is in no list, and
	// is not counted.
	found.MoreCount = len(ordered) - end
	found.IsMoreCountLowerBound = !isComplete
	if found.MoreCount > 0 || !isComplete {
		found.NextOffset = end
	}

	documents, err := documentsOf(tx, agentId, ranked)
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	for _, source := range sources {
		names[source.ID] = source.Name
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
			SourceID: document.SourceID, Source: names[document.SourceID],
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
// deployment can say what a passage means. It answers with the ranking
// from the top to at least wanted passages, or to the last if there are
// fewer, and whether that is every passage the search can find.
//
// Where the database can rank vectors itself the two are separate
// searches fused by rank. Where it cannot, meaning re-ranks what the
// words found, which is honest and bounded: it finds everything the words
// find, in a better order, and misses a paraphrase sharing no word.
//
// The search goes in rounds. The first reads wordsPoolFirst passages by
// words and meaningPoolFirst by meaning and ranks them; each round after
// reads twice as many, ranks them the same way, and adds whatever the
// rounds before did not have, in its own order, behind them. A fused
// ranking of deeper lists is a different ranking, so a page read from it
// could repeat what the page before showed or skip what it did not; a
// round is ranked from lists of a fixed depth whatever page is asked for,
// so every page is a slice of one list.
func findChunks(ctx context.Context, tx db.Transaction, meaning Meaning, agentId string, sourceIds []string, documentPrefix string, words string, wanted int) ([]*scored, bool, bool, error) {
	var ordered []*scored
	seen := map[string]bool{}
	isMeaningful := false
	for round := 0; ; round++ {
		wordsDepth := wordsPoolFirst << round
		meaningDepth := meaningPoolFirst << round
		byWords, err := tx.SearchAgentChunks(agentId, sourceIds, documentPrefix, words, wordsDepth)
		if err != nil {
			return nil, false, false, err
		}
		// A list shorter than was asked for is all there is. For the
		// meaning that includes what the similarity floor left out, which
		// no deeper read would bring back either.
		isComplete := len(byWords) < wordsDepth
		var ranked []*scored
		switch byMeaning, hasVectorIndex := searchByMeaning(ctx, meaning, sourceIds, documentPrefix, words, meaningDepth); {
		case meaning == nil:
			ranked = rank(byWords)
		case !hasVectorIndex:
			// No index: re-rank what the words found, rather than reading
			// a hundred thousand vectors into memory to sort them.
			ranked = rank(meaning.RankChunksByMeaning(ctx, words, byWords, len(byWords)))
			isMeaningful = len(byWords) > 0
		default:
			ranked = fuse(byMeaning, byWords)
			isComplete = isComplete && len(byMeaning) < meaningDepth
			isMeaningful = true
		}
		for _, found := range ranked {
			if !seen[found.chunk.ID] {
				seen[found.chunk.ID] = true
				ordered = append(ordered, found)
			}
		}
		if isComplete || len(ordered) >= wanted {
			return ordered, isComplete, isMeaningful, nil
		}
	}
}

// searchByMeaning is the meaning's own list, where there is a meaning.
func searchByMeaning(ctx context.Context, meaning Meaning, sourceIds []string, documentPrefix string, words string, limit int) ([]*models.AgentChunk, bool) {
	if meaning == nil {
		return nil, false
	}
	return meaning.SearchKnowledgeByMeaning(ctx, sourceIds, documentPrefix, words, limit)
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
// Two at the same score go by identifier, so the same lists are always
// fused into the same order.
func fuse(lists ...[]*models.AgentChunk) []*scored {
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
	sort.SliceStable(order, func(left, right int) bool {
		if scores[order[left]] != scores[order[right]] {
			return scores[order[left]] > scores[order[right]]
		}
		return order[left] < order[right]
	})
	ranked := make([]*scored, 0, len(order))
	for _, id := range order {
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

// directoryHitsShown is the most directories a search names.
const directoryHitsShown = 8

// sourceOfDirectory is the source that reads a directory and the prefix
// of the names it gave what it read there: of the sources on the computer
// (where one is named) whose path holds the directory, the one whose path
// is longest. A source's path is matched as the person wrote it, so
// "~/code/seedling" finds a source at "~/code".
func sourceOfDirectory(sources []*models.AgentKnowledgeSource, sourceIds []string, directory, computerName string) (*models.AgentKnowledgeSource, string) {
	directory = cleanPath(directory)
	var best *models.AgentKnowledgeSource
	bestPrefix, bestLength := "", -1
	for _, source := range sources {
		root := cleanPath(source.Specification.Path)
		if root == "" || (len(sourceIds) > 0 && !slices.Contains(sourceIds, source.ID)) {
			continue
		}
		if computerName != "" && source.Specification.Computer != computerName {
			continue
		}
		var prefix string
		switch {
		case directory == root:
		case strings.HasPrefix(directory, root+"/"):
			prefix = strings.TrimPrefix(directory, root+"/") + "/"
		default:
			continue
		}
		if len(root) > bestLength {
			best, bestPrefix, bestLength = source, prefix, len(root)
		}
	}
	return best, bestPrefix
}

func cleanPath(directory string) string {
	directory = strings.TrimSpace(directory)
	if directory == "" {
		return ""
	}
	return path.Clean(directory)
}

// directoriesOf is where the ranked passages are, by the directory their
// document is in under its source, the directory with the most first.
// Only a source of files has directories worth naming; an archive's names
// are its own bookkeeping.
func directoriesOf(tx db.Transaction, agentId string, sources []*models.AgentKnowledgeSource, ranked []*scored) ([]*DirectoryHits, error) {
	documents, err := documentsOf(tx, agentId, ranked)
	if err != nil {
		return nil, err
	}
	sourceById := map[string]*models.AgentKnowledgeSource{}
	for _, source := range sources {
		sourceById[source.ID] = source
	}
	byDirectory := map[string]*DirectoryHits{}
	var order []string
	for _, found := range ranked {
		document := documents[found.chunk.DocumentID]
		if document == nil {
			continue
		}
		source := sourceById[document.SourceID]
		if source == nil || source.Specification.Path == "" || (source.Specification.Format != models.FormatFiles && source.Specification.Format != "") {
			continue
		}
		directory := cleanPath(source.Specification.Path)
		if folder := path.Dir(document.ExternalID); folder != "." && folder != "/" {
			directory += "/" + folder
		}
		hits := byDirectory[directory]
		if hits == nil {
			hits = &DirectoryHits{Directory: directory, SourceID: source.ID, Source: source.Name}
			byDirectory[directory] = hits
			order = append(order, directory)
		}
		hits.PassageCount++
	}
	if len(order) < 2 {
		return []*DirectoryHits{}, nil
	}
	// The most passages first; of two with as many, the one whose best
	// passage ranked higher, which is the one met first.
	sort.SliceStable(order, func(left, right int) bool {
		return byDirectory[order[left]].PassageCount > byDirectory[order[right]].PassageCount
	})
	directories := []*DirectoryHits{}
	for _, directory := range order[:min(len(order), directoryHitsShown)] {
		directories = append(directories, byDirectory[directory])
	}
	return directories, nil
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
