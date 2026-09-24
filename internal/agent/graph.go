package agent

// Graph access separates prompt context, recall selection, database retrieval,
// embedding calls, ranking, indexing and fact updates. The bounds stay shared
// so each path uses the same limits while its implementation can be reviewed alone.

// The bounds.
const (
	// indexTokens is how much of a prompt the index may take, and
	// recallTokens how much the overlay after the history may. Both are
	// estimates from the same counter the compactor uses.
	indexTokens  = 1500
	recallTokens = 1200

	// runIndexTokens is what a run with nobody present carries. Larger,
	// because such a run cannot ask for more.
	runIndexTokens = 1000

	// selfSummary is how much of the self page always goes in. It is the
	// one page worth its tokens on every turn: who the person is.
	selfSummary = 2000

	// recallCandidates is how many rows each of the four searches offers
	// the fusion below.
	recallCandidates = 20

	// recallFactTokens is what the overlay holds back for the facts the
	// question matched outright, so that the pages cannot spend it all
	// first. They go in as one block, which is held back too.
	recallFactTokens = 400

	// recallPages is how many pages the overlay expands, recallFacts how
	// many loose facts it carries beside them, and pageFacts the most of
	// the facts the question hit on a page that its block shows.
	//
	// pageFactsConsidered bounds the rows read before choosing which facts to
	// show. Reading more than pageFacts prevents the oldest rows from excluding
	// a later fact that directly matches the question.
	recallPages         = 5
	recallFacts         = 10
	pageFacts           = 5
	pageFactsConsidered = 60

	// pageFactsLeast is how many facts an expanded page shows at the
	// least: the facts the question hit on it, and its first facts where
	// it hit fewer than this (a page it hit nothing on shows its first),
	// so a single matched sentence still arrives with what the page is
	// about. A page is no longer filled up to pageFacts with its oldest
	// lines: on a long page those are its opening remarks, and they took
	// the room of the sentences the question asked for.
	pageFactsLeast = 2

	// recallPagesUnhit is how many pages the overlay expands that the
	// fact search hit nothing on: pages found by their name or their
	// meaning alone. They carry their opening and pageFactsLeast facts.
	// Bounded because such a page -- a month with a summary and no facts
	// of its own, say -- ranked high and filled every page slot while the
	// facts that answered the question were left out.
	recallPagesUnhit = 2

	// recallBlocks is how many lines the overlay carries in all, and
	// recallGraphBlocks how many of them the graph may fill: the
	// passages of the person's own files are gathered after the graph is
	// and take the rest.
	//
	// One budget, because two of them disagreed. The chooser offered up
	// to fifteen blocks -- five pages and ten loose facts -- into an
	// overlay that kept the last ten lines, so the pages it had ranked
	// highest were exactly the ones dropped, and every fact on them had
	// `used_at` moved for a prompt that never carried them.
	recallBlocks      = 10
	recallGraphBlocks = recallBlocks - recallChunks

	// meaningFloorGraph is the least similarity worth calling a match.
	// Embedding models put unrelated text between a tenth and three
	// tenths apart; a quarter keeps out noise without losing a paraphrase.
	meaningFloorGraph = 0.25

	// twinFloor is how near two facts on one page must be for one to be
	// called the other said twice.
	twinFloor = 0.92

	// graphEmbedCharacters is how much of a page or a fact is embedded.
	graphEmbedCharacters = 4000

	// turnMeaningsKept is how many different questions one turn keeps the
	// vector of. A handful: recall asks one question of two stores, and
	// the tools ask a few more. Bounded so that a model which searched
	// forty times does not hold forty vectors until the turn ends.
	turnMeaningsKept = 16

	// recallChunks is how many passages of the person's own files and
	// chat a turn brings back without being asked, recallChunkCharacters
	// how much of each, and recallKnowledgeTokens the whole budget for
	// them. Small: the point is to answer a question about their own work
	// from their own work, not to fill the prompt with a codebase.
	recallChunks          = 3
	recallChunkCharacters = 600
	recallKnowledgeTokens = 700

	// reciprocalRankConstant is the k of reciprocal rank fusion. Sixty is
	// the number the method was published with and needs no tuning: it is
	// what stops the first row of a short list from outweighing the whole
	// of a long one.
	reciprocalRankConstant = 60
)
