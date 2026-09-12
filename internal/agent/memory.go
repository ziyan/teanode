package agent

import (
	"context"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// Memory is what the agent keeps about the person between conversations:
// durable facts, each addressed to the runs that should read it. The top
// of it is folded into every prompt by audience, so most turns need no
// search; the tool is for adding, changing and looking further.

// The bounds.
const (
	// promptMemories is how many memories a prompt carries: twenty for the
	// conversation, thirty for a run that has no way to ask for more.
	promptMemories    = 20
	promptRunMemories = 30

	// promptCorrections is how many corrections a run is shown.
	promptCorrections = 20

	// recalled is how many memories the words of a turn may bring back
	// beside the ones the prompt already carries, and recallWordLength
	// the shortest word worth looking one up by: "the" would find
	// everything and mean nothing.
	recalled          = 5
	recallWordLength  = 4
	recallWordsPerAsk = 12
)

// recallForTurn puts what the agent already knows about the words in
// front of it. The prompt carries the top of memory, which is what was
// used lately rather than what this turn is about; a person who asks
// about somebody the agent was told about in March would otherwise get
// "I do not know" unless the model thought to search. So the words of
// the turn are searched here, before the first round, and whatever they
// touch is put in the recalled overlay. It costs one query and no
// round-trip to the model, which is why it happens every turn instead of
// being asked for.
func (self *AskRun) recallForTurn(ctx context.Context) {
	// Memories written before there was an embedding model, or before
	// this one, catch up a few at a time rather than all at once.
	if _, err := self.agent.EmbedMemories(ctx, self.settings.Agent, memoryBackfill); err != nil {
		log.Warningf("cannot give the memories of %q their vectors: %s", self.settings.Owner.Username, err)
	}
	words := recallWords(self.settings.Message)
	if len(words) == 0 {
		return
	}
	var found []*models.AgentMemory
	if err := self.agent.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		found, err = tx.RecallAgentMemories(self.settings.Agent.ID, words, recalled*4)
		return err
	}); err != nil {
		log.Warningf("cannot recall the memories of %q: %s", self.settings.Owner.Username, err)
		return
	}
	// Whichever the words touch most, and never one the prompt already
	// carries: the model would only read it twice.
	sort.SliceStable(found, func(left, right int) bool {
		return recallScore(found[left], words) > recallScore(found[right], words)
	})
	// And whatever the turn is about, which is not the same question: a
	// person asking about "the boat" means the memory that says
	// "Kittiwake", and no word of theirs appears in it. Nearest first,
	// then the word matches, so the closest thing comes first where
	// there is an embedding model and nothing changes where there is not.
	found = append(self.nearestMemories(ctx, self.settings.Message), found...)
	kept := 0
	var used []string
	seen := map[string]bool{}
	for _, memory := range found {
		// Addressed to this conversation, as the prompt's own list is. A
		// memory a person wrote for sorting their mail and not for
		// talking to is not read out here because a word of it happened
		// to appear in what they said.
		if kept >= recalled || seen[memory.ID] || self.inPrompt(memory.ID) || !memory.Addressed(models.AudienceAsk) {
			continue
		}
		seen[memory.ID] = true
		self.Recall(memory.Line() + " (memory " + memory.ID + ")")
		used = append(used, memory.ID)
		kept++
	}
	if len(used) > 0 {
		if err := self.agent.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
			return tx.TouchAgentMemories(used, time.Now())
		}); err != nil {
			log.Warningf("cannot mark memories used: %s", err)
		}
	}
}

// recallWords are the words of a turn worth remembering by: long enough
// to mean something, and not so many that one message searches for
// everything.
func recallWords(message string) []string {
	var words []string
	seen := map[string]bool{}
	for _, word := range strings.FieldsFunc(strings.ToLower(message), func(character rune) bool {
		return !unicode.IsLetter(character) && !unicode.IsDigit(character)
	}) {
		if len([]rune(word)) < recallWordLength || seen[word] || recallStopWords[word] {
			continue
		}
		seen[word] = true
		words = append(words, word)
		if len(words) >= recallWordsPerAsk {
			break
		}
	}
	return words
}

// recallScore is how many of the words a memory touches.
func recallScore(memory *models.AgentMemory, words []string) int {
	haystack := strings.ToLower(memory.Title + " " + memory.Content + " " + strings.Join(memory.Tags, " "))
	score := 0
	for _, word := range words {
		if strings.Contains(haystack, word) {
			score++
		}
	}
	return score
}

// recallStopWords are long enough to pass the length test and still say
// nothing about what a turn is about.
var recallStopWords = map[string]bool{
	"about": true, "after": true, "again": true, "also": true, "another": true, "anything": true,
	"because": true, "been": true, "before": true, "could": true, "does": true, "doing": true,
	"done": true, "from": true, "have": true, "here": true, "into": true, "just": true,
	"like": true, "make": true, "many": true, "more": true, "much": true, "need": true,
	"only": true, "other": true, "over": true, "please": true, "same": true, "should": true,
	"some": true, "something": true, "still": true, "such": true, "than": true, "that": true,
	"them": true, "then": true, "there": true, "these": true, "they": true, "thing": true,
	"things": true, "this": true, "those": true, "very": true, "want": true, "what": true,
	"when": true, "where": true, "which": true, "while": true, "with": true, "would": true,
	"your": true,
}

// memoryLines is the memories for an audience as prompt lines, marked
// used. Each carries its id so the model can cite or change it.
func memoryLines(tx db.Transaction, agentId string, audience models.AgentAudience, limit int, withIds bool) ([]string, error) {
	memories, err := tx.ListAgentMemories(agentId, audience, limit)
	if err != nil {
		return nil, err
	}
	if len(memories) == 0 {
		return nil, nil
	}
	lines := make([]string, 0, len(memories))
	ids := make([]string, 0, len(memories))
	for _, memory := range memories {
		line := memory.Line()
		if withIds {
			line += " (memory " + memory.ID + ")"
		}
		lines = append(lines, line)
		ids = append(ids, memory.ID)
	}
	if err := tx.TouchAgentMemories(ids, time.Now()); err != nil {
		return nil, err
	}
	return lines, nil
}

// correctionLines is what the person corrected, newest first.
func correctionLines(tx db.Transaction, agentId string, kinds []models.AgentFeedbackKind) ([]string, error) {
	feedback, err := tx.ListAgentFeedback(agentId, kinds, promptCorrections)
	if err != nil {
		return nil, err
	}
	lines := make([]string, 0, len(feedback))
	for _, entry := range feedback {
		lines = append(lines, entry.Said)
	}
	return lines, nil
}

// inPrompt says whether a memory is already in the prompt's own list,
// which the turn's recall does not repeat.
func (self *AskRun) inPrompt(memoryId string) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.promptMemories[memoryId]
}

// memories is layer 3's tail: what the agent remembers for the
// conversation, pinned first.
func (self *AskRun) memories(ctx context.Context) []string {
	var lines []string
	var carried []string
	if err := self.agent.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		memories, err := tx.ListAgentMemories(self.settings.Agent.ID, models.AudienceAsk, promptMemories)
		if err != nil {
			return err
		}
		for _, memory := range memories {
			lines = append(lines, memory.Line()+" (memory "+memory.ID+")")
			carried = append(carried, memory.ID)
		}
		return tx.TouchAgentMemories(carried, time.Now())
	}); err != nil {
		log.Warningf("cannot read the memories of %q: %s", self.settings.Owner.Username, err)
	}
	self.mutex.Lock()
	if self.promptMemories == nil {
		self.promptMemories = map[string]bool{}
	}
	for _, id := range carried {
		self.promptMemories[id] = true
	}
	self.mutex.Unlock()
	return lines
}
