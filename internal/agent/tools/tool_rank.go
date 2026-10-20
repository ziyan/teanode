package tools

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

// How tool_search ranks the catalog.
//
// A word counts by how rare it is in the catalog: "get", "search" and
// "list" are in most descriptions, so they say little, while "calendar" or
// "dns" picks out the few tools that are about it. A word in a tool's name
// counts for more than one in its family, and that for more than one in its
// description (BM25F). A query that names a tool, by its whole name or by
// every word of it, puts that tool first, since the prompt lists the tools
// that wait by name and a model often asks for one of them.
//
// What is loaded is only the matches close to the best one. Every tool
// loaded is in every request for the rest of the turn, and a search used to
// load every tool sharing a word with the query: six a search, of which the
// model called about one in ten. When the best match is a tool the model
// holds already, a weak match behind it is not worth a definition in every
// request, so it is measured against that tool too.
const (
	// SearchLoadedLimit is how many tools one search loads when the model
	// does not say, and SearchLoadedMost how many it may ask for.
	SearchLoadedLimit = 5
	SearchLoadedMost  = 10

	// searchHeldLimit is how many of the tools the model holds a search
	// names back.
	searchHeldLimit = 3

	// searchCloseFraction is how near the best score a match must come to
	// be loaded.
	searchCloseFraction = 0.4

	// The weight of a word by where in a tool it is.
	searchNameWeight        = 3.0
	searchFamilyWeight      = 1.5
	searchDescriptionWeight = 1.0

	// searchDescriptionRunes is how much of a description is read: the
	// first sentences say what a tool is for, and the rest how to call it.
	searchDescriptionRunes = 600

	// BM25's saturation and length normalization.
	searchSaturation    = 1.2
	searchLengthBalance = 0.75

	// The bonus of a tool the query names by its whole name, and of one
	// every word of whose name is in the query, with a little more for each
	// word, so that "calendar agenda" puts calendar_agenda before calendar.
	searchWholeNameBonus   = 10.0
	searchNameWordsBonus   = 3.0
	searchNameWordBonusAdd = 1.0
)

// searchSelectPrefix starts a query that names the tools to load, comma
// separated: "select:calendar_agenda,contact_book".
const searchSelectPrefix = "select:"

// searchStopWords say nothing about which tool is wanted.
var searchStopWords = map[string]bool{}

func init() {
	for _, word := range strings.Fields("a an the to for of and or in on at by with from into about as is are be it its this that these those my your their our me you we they them do does what how which who when where why can could should would will just please some any all more most new use using via tool tools") {
		searchStopWords[word] = true
	}
}

// ToolSearchFound is what a search found: the tools waiting behind
// tool_search to load, best first, and the tools the model holds already
// that the query names, which it should call rather than search again.
type ToolSearchFound struct {
	Loadable []*Tool
	Held     []*Tool
}

// SearchTools ranks the held and the deferred tools against the query.
// Limit bounds what is loaded; zero is SearchLoadedLimit.
func SearchTools(held, deferred []*Tool, query string, limit int) ToolSearchFound {
	if limit <= 0 {
		limit = SearchLoadedLimit
	}
	limit = min(limit, SearchLoadedMost)
	query = strings.TrimSpace(query)
	if names, isSelect := strings.CutPrefix(strings.ToLower(query), searchSelectPrefix); isSelect {
		return selectTools(held, deferred, names, limit)
	}
	index := newToolIndex(append(append([]*Tool{}, held...), deferred...))
	heldScores := index.rank(held, query)
	deferredScores := index.rank(deferred, query)
	found := ToolSearchFound{}
	deferredBest, heldBest := 0.0, 0.0
	if len(deferredScores) > 0 {
		deferredBest = deferredScores[0].score
	}
	if len(heldScores) > 0 {
		heldBest = heldScores[0].score
	}
	for _, scored := range heldScores {
		if len(found.Held) < searchHeldLimit && scored.isNamed && scored.score >= deferredBest {
			found.Held = append(found.Held, scored.tool)
		}
	}
	floor := searchCloseFraction * max(deferredBest, heldBest)
	for _, scored := range deferredScores {
		if len(found.Loadable) >= limit || scored.score < floor {
			break
		}
		found.Loadable = append(found.Loadable, scored.tool)
	}
	return found
}

// selectTools loads the tools a select: query names, in its order, and
// names back the ones held.
func selectTools(held, deferred []*Tool, names string, limit int) ToolSearchFound {
	byName := func(tools []*Tool) map[string]*Tool {
		found := make(map[string]*Tool, len(tools))
		for _, tool := range tools {
			found[strings.ToLower(tool.Name)] = tool
		}
		return found
	}
	heldByName, deferredByName := byName(held), byName(deferred)
	found := ToolSearchFound{}
	for _, name := range strings.Split(names, ",") {
		name = strings.TrimSpace(name)
		if tool := deferredByName[name]; tool != nil && len(found.Loadable) < limit {
			found.Loadable = append(found.Loadable, tool)
		} else if tool := heldByName[name]; tool != nil && tool.Name != "tool_search" && len(found.Held) < searchHeldLimit {
			found.Held = append(found.Held, tool)
		}
	}
	return found
}

// toolIndex is the catalog read for one search: each tool's words by where
// they are, and how rare each word is.
type toolIndex struct {
	words             map[*Tool]*toolWords
	inverseFrequency  map[string]float64
	descriptionLength float64
}

type toolWords struct {
	name, family, description map[string]int
	descriptionCount          int
	nameWords                 []string
}

func newToolIndex(catalog []*Tool) *toolIndex {
	index := &toolIndex{words: make(map[*Tool]*toolWords, len(catalog)), inverseFrequency: map[string]float64{}}
	documentCounts := map[string]int{}
	descriptionTotal := 0
	for _, tool := range catalog {
		description := []rune(tool.Description)
		if len(description) > searchDescriptionRunes {
			description = description[:searchDescriptionRunes]
		}
		words := &toolWords{
			nameWords:   toolNameWords(tool.Name),
			family:      countWords(searchWords(string(tool.Family))),
			description: countWords(searchWords(string(description))),
		}
		words.name = countWords(words.nameWords)
		for _, count := range words.description {
			words.descriptionCount += count
		}
		descriptionTotal += words.descriptionCount
		index.words[tool] = words
		seen := map[string]bool{}
		for _, field := range []map[string]int{words.name, words.family, words.description} {
			for word := range field {
				if !seen[word] {
					seen[word] = true
					documentCounts[word]++
				}
			}
		}
	}
	toolCount := float64(len(catalog))
	for word, count := range documentCounts {
		index.inverseFrequency[word] = math.Log(1 + (toolCount-float64(count)+0.5)/(float64(count)+0.5))
	}
	if len(catalog) > 0 {
		index.descriptionLength = float64(descriptionTotal) / toolCount
	}
	return index
}

// scoredTool is one tool's score against a query, and whether the query
// names it.
type scoredTool struct {
	tool    *Tool
	score   float64
	isNamed bool
}

// rank scores the tools against the query, best first, leaving out the
// ones nothing in it matches and tool_search itself: told to call it, the
// model would only search again.
func (self *toolIndex) rank(tools []*Tool, query string) []scoredTool {
	queryWords := searchWords(query)
	queryHas := map[string]bool{}
	for _, word := range queryWords {
		queryHas[word] = true
	}
	// Every word of the query as written, for a tool named whole.
	written := map[string]bool{}
	for _, word := range strings.Fields(strings.ToLower(query)) {
		written[strings.Trim(word, "+.,;:!?\"'()[]`")] = true
	}
	scores := make([]scoredTool, 0, len(tools))
	for _, tool := range tools {
		if tool.Name == "tool_search" {
			continue
		}
		words := self.words[tool]
		score := self.score(words, queryHas)
		isWholeName := written[strings.ToLower(tool.Name)]
		isEveryNameWord := len(words.nameWords) > 0
		for _, word := range words.nameWords {
			isEveryNameWord = isEveryNameWord && queryHas[word]
		}
		switch {
		case isWholeName:
			score += searchWholeNameBonus
		case isEveryNameWord:
			score += searchNameWordsBonus + searchNameWordBonusAdd*float64(len(words.nameWords))
		}
		if score > 0 {
			scores = append(scores, scoredTool{tool: tool, score: score, isNamed: isWholeName || isEveryNameWord})
		}
	}
	sort.SliceStable(scores, func(left, right int) bool { return scores[left].score > scores[right].score })
	return scores
}

func (self *toolIndex) score(words *toolWords, queryHas map[string]bool) float64 {
	lengthRatio := 1.0
	if self.descriptionLength > 0 {
		lengthRatio = float64(words.descriptionCount) / self.descriptionLength
	}
	normalizer := searchSaturation * (1 - searchLengthBalance + searchLengthBalance*lengthRatio)
	total := 0.0
	for word := range queryHas {
		frequency := searchNameWeight*float64(words.name[word]) + searchFamilyWeight*float64(words.family[word]) +
			searchDescriptionWeight*float64(words.description[word])
		if frequency == 0 {
			continue
		}
		total += self.inverseFrequency[word] * frequency * (searchSaturation + 1) / (frequency + normalizer)
	}
	return total
}

// toolNameWords are the words of a tool's name, without the skill__ and
// mcp__ that say where it came from: skill__notes__notes_search is notes
// and search.
func toolNameWords(name string) []string {
	var words []string
	for _, word := range searchWords(strings.NewReplacer("__", " ", "_", " ", "-", " ", ".", " ").Replace(name)) {
		if word != "skill" && word != "mcp" {
			words = append(words, word)
		}
	}
	return words
}

// searchWords are the words of a text as the search compares them:
// lowercase, split at anything not a letter or a digit and between the
// words of a camelCase name, without the words that say nothing, and with
// an English plural or verb ending taken off, so "events" finds "event".
func searchWords(text string) []string {
	var words []string
	var current []rune
	flush := func() {
		if len(current) > 1 {
			word := string(current)
			if !searchStopWords[word] {
				words = append(words, stemWord(word))
			}
		}
		current = current[:0]
	}
	previous := rune(0)
	for _, character := range text {
		switch {
		case unicode.IsUpper(character) && unicode.IsLower(previous):
			flush()
			current = append(current, unicode.ToLower(character))
		case unicode.IsLetter(character) || unicode.IsDigit(character):
			current = append(current, unicode.ToLower(character))
		default:
			flush()
		}
		previous = character
	}
	flush()
	return words
}

func countWords(words []string) map[string]int {
	counts := make(map[string]int, len(words))
	for _, word := range words {
		counts[word]++
	}
	return counts
}

// stemWord takes an English plural ending off a word long enough to keep
// its meaning without it: "entries" is "entry", "searches" is "search",
// "events" is "event", and "address" stays. Only plurals: taking "ing" or
// "ed" off as well made "shared" into "shar", which matches nothing.
func stemWord(word string) string {
	if len(word) <= 4 {
		return word
	}
	switch {
	case strings.HasSuffix(word, "ies"):
		return strings.TrimSuffix(word, "ies") + "y"
	case strings.HasSuffix(word, "sses"), strings.HasSuffix(word, "xes"), strings.HasSuffix(word, "zes"),
		strings.HasSuffix(word, "ches"), strings.HasSuffix(word, "shes"):
		return strings.TrimSuffix(word, "es")
	case strings.HasSuffix(word, "s") && !strings.HasSuffix(word, "ss") && !strings.HasSuffix(word, "us"):
		return strings.TrimSuffix(word, "s")
	}
	return word
}
