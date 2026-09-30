package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"unicode"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// A page's overview is several sections under "## " headings: what the
// thing is, what has been happening, how it relates to others, its parts,
// what stands out. Each section is embedded on its own, so that a question
// about one of them finds the page by the section that answers it, and
// recall carries that section rather than always the first.

// overviewSectionPages is how many pages' overviews the embedding pass
// reads, the most important first: every page that has one, on a graph of
// the size this runs on.
const overviewSectionPages = 5000

// overviewSectionHashLength is how much of the hash of a section's words
// its id carries: enough that a rewritten section is a new id.
const overviewSectionHashLength = 12

// overviewSection is one section of a page's overview.
type overviewSection struct {
	// Number is the section's place in the overview, from one.
	Number int

	Heading string
	Text    string

	// ID is the page, the number and a hash of the words; a vector is
	// stored under it, so a vector of old words is never taken for new.
	ID string
}

// overviewSectionsOf is a page's overview cut into its sections, in order;
// empty for a page with none.
func overviewSectionsOf(node *models.AgentNode) []overviewSection {
	overview := strings.TrimSpace(node.Overview)
	if overview == "" {
		return nil
	}
	var sections []overviewSection
	for index, part := range strings.Split("\n"+overview, "\n## ") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		// What comes before the first heading is a section with none.
		heading, text := "", part
		if index > 0 {
			heading, text, _ = strings.Cut(part, "\n")
		}
		heading = strings.TrimSpace(heading)
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		number := len(sections) + 1
		sections = append(sections, overviewSection{
			Number: number, Heading: heading, Text: text,
			ID: overviewSectionId(node.ID, number, heading, text),
		})
	}
	return sections
}

// overviewSectionId is the id a section's vector is stored under.
func overviewSectionId(nodeId string, number int, heading, text string) string {
	sum := sha256.Sum256([]byte(heading + "\n" + text))
	return nodeId + "/" + strconv.Itoa(number) + "/" + hex.EncodeToString(sum[:])[:overviewSectionHashLength]
}

// nodeOfOverviewSection is the page a section id belongs to.
func nodeOfOverviewSection(sectionId string) string {
	nodeId, _, _ := strings.Cut(sectionId, "/")
	return nodeId
}

// overviewSectionText is what is embedded of a section: the page's name
// and the heading, so "how it relates" carries what it is about.
func overviewSectionText(node *models.AgentNode, section overviewSection) string {
	name := strings.TrimSpace(node.Name)
	if name == "" {
		name = node.Path
	}
	return cutRunes(name+" — "+section.Heading+"\n"+section.Text, graphEmbedCharacters)
}

// overviewSectionFor is the section of a page's overview recall carries
// for a question: the one the question's meaning matched, where that
// section is still the page's; else the one sharing the most of the
// question's words; else the first, which says what the thing is. Cut to
// so many characters, its heading included; empty for a page with no
// overview.
func overviewSectionFor(node *models.AgentNode, matchedSectionId, question string, characters int) string {
	sections := overviewSectionsOf(node)
	if len(sections) == 0 {
		return ""
	}
	chosen := sections[0]
	isMatched := false
	for _, section := range sections {
		if matchedSectionId != "" && section.ID == matchedSectionId {
			chosen, isMatched = section, true
			break
		}
	}
	if !isMatched {
		// The page's own name is in every section, and mostly in the
		// first: the words that choose a section are the rest.
		named := questionWords(node.Name + " " + node.Path + " " + strings.Join(node.Aliases, " "))
		words := withoutWords(questionWords(question), named)
		best := 0
		for _, section := range sections {
			if shared := sharedWordCount(words, section.Heading+" "+section.Text); shared > best {
				chosen, best = section, shared
			}
		}
	}
	rendered := chosen.Text
	if chosen.Heading != "" {
		rendered = "## " + chosen.Heading + "\n\n" + chosen.Text
	}
	if cut := cutRunes(rendered, characters); cut != rendered {
		rendered = strings.TrimSpace(cut) + "…"
	}
	return rendered
}

// questionStopWords are words a question uses that say nothing about which
// section answers it.
var questionStopWords = map[string]bool{
	"the": true, "and": true, "for": true, "are": true, "was": true, "were": true, "what": true, "which": true,
	"who": true, "whom": true, "whose": true, "when": true, "where": true, "why": true, "how": true, "does": true,
	"did": true, "has": true, "have": true, "had": true, "with": true, "that": true, "this": true, "these": true,
	"those": true, "from": true, "into": true, "about": true, "there": true, "their": true, "they": true,
	"them": true, "his": true, "her": true, "its": true, "our": true, "your": true, "you": true, "can": true,
	"could": true, "would": true, "should": true, "will": true, "been": true, "being": true, "any": true,
	"all": true, "some": true, "most": true, "more": true, "much": true, "many": true, "not": true,
}

// questionWords is the distinct words of a question worth matching: three
// letters or more, lower case, without the words every question uses.
func questionWords(question string) []string {
	seen := map[string]bool{}
	var words []string
	for _, word := range strings.FieldsFunc(strings.ToLower(question), func(character rune) bool {
		return !unicode.IsLetter(character) && !unicode.IsDigit(character)
	}) {
		if len([]rune(word)) < 3 || questionStopWords[word] || seen[word] {
			continue
		}
		seen[word] = true
		words = append(words, word)
	}
	return words
}

// withoutWords is the words that are not among the others.
func withoutWords(words, others []string) []string {
	isOther := make(map[string]bool, len(others))
	for _, other := range others {
		isOther[other] = true
	}
	kept := make([]string, 0, len(words))
	for _, word := range words {
		if !isOther[word] {
			kept = append(kept, word)
		}
	}
	return kept
}

// sharedWordCount is how many of the words the text holds.
func sharedWordCount(words []string, text string) int {
	lowered := strings.ToLower(text)
	count := 0
	for _, word := range words {
		if strings.Contains(lowered, word) {
			count++
		}
	}
	return count
}

// overviewSectionBatch is how many sections go to the embedding model in
// one call.
const overviewSectionBatch = 100

// EmbedOverviewSections gives a vector to every overview section that has
// none, most important pages first and at most so many, and removes the
// vectors of sections no overview holds any more. It says how many it
// wrote.
func (self *Agent) EmbedOverviewSections(ctx context.Context, agent *models.Agent, limit int) (int, error) {
	_, _, modelName, _, ok := self.embedderFor()
	if !ok {
		return 0, nil
	}
	var nodes []*models.AgentNode
	var stored []string
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if nodes, err = tx.ListAgentNodesWithOverviews(agent.ID, overviewSectionPages); err != nil {
			return err
		}
		stored, err = tx.ListAgentOverviewSectionVectorIds(agent.ID, modelName)
		return err
	}); err != nil {
		return 0, err
	}
	isStored := make(map[string]bool, len(stored))
	for _, id := range stored {
		isStored[id] = true
	}
	type missingSection struct {
		node    *models.AgentNode
		section overviewSection
	}
	isWanted := map[string]bool{}
	var missing []missingSection
	for _, node := range nodes {
		for _, section := range overviewSectionsOf(node) {
			isWanted[section.ID] = true
			if !isStored[section.ID] && len(missing) < limit {
				missing = append(missing, missingSection{node: node, section: section})
			}
		}
	}
	var gone []string
	for _, id := range stored {
		if !isWanted[id] {
			gone = append(gone, id)
		}
	}
	if len(gone) > 0 {
		if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
			return tx.DeleteAgentOverviewSectionVectors(agent.ID, modelName, gone)
		}); err != nil {
			return 0, err
		}
	}

	written := 0
	for start := 0; start < len(missing); start += overviewSectionBatch {
		batch := missing[start:min(start+overviewSectionBatch, len(missing))]
		texts := make([]string, 0, len(batch))
		for _, each := range batch {
			texts = append(texts, overviewSectionText(each.node, each.section))
		}
		vectors, _, ok := self.embed(ctx, agent.ID, "embed", texts)
		if !ok {
			return written, nil
		}
		if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
			for index, each := range batch {
				if index >= len(vectors) || len(vectors[index]) == 0 {
					continue
				}
				if err := tx.PutAgentOverviewSectionVector(agent.ID, each.node.ID, each.section.ID, modelName, vectors[index]); err != nil {
					return err
				}
				written++
			}
			return nil
		}); err != nil {
			return written, err
		}
	}
	return written, nil
}

// nearestOverviewSectionsTo is the overview sections nearest a question in
// meaning, best first, as section ids.
func (self *Agent) nearestOverviewSectionsTo(ctx context.Context, agentId string, question *meaning, limit int) ([]string, error) {
	if question == nil {
		return nil, errNotAskedByMeaning
	}
	var ids []string
	err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		scores, err := tx.Nearest(db.AgentOverviewSectionTable, agentId, question.ModelName, question.Vector, limit, db.VectorQuery{Floor: meaningFloorGraph})
		if err != nil {
			return err
		}
		ids = idsOf(scores)
		return nil
	})
	return ids, err
}
