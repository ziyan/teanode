package agent

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// Search by meaning: each message of a mailbox that searches this way gets
// a vector when it arrives (and every earlier one when the mailbox is
// granted), and a search embeds its words and ranks the mailbox's vectors
// by cosine in the server, a page of them at a time. No vector database: a
// personal mailbox has thousands of messages, not millions.

// embedCharacters is how much of a message goes into its vector: the subject
// and the start of the text, which is where the meaning is.
const embedCharacters = 4000

// embedPage is how many of a mailbox's vectors a search reads at a time, so
// that only one page is in memory; a search goes on through every page. A
// variable so that a test can make it small.
var embedPage = 2000

// embedText is what is embedded of a message.
func embedText(message *MessageContext) string {
	text := "Subject: " + message.Subject + "\nFrom: " + message.From + "\n\n" + message.Text
	if len(text) > embedCharacters {
		text = text[:embedCharacters]
	}
	return text
}

// runEmbed is the handler for an embed job; its subject is the message.
func (self *Agent) runEmbed(ctx context.Context, run *Run) error {
	if run.Mailbox == nil || run.Source == nil || !run.Source.Granted || !run.Source.Search {
		return nil
	}
	configuration := run.Configuration()
	if !FeatureAllowed(configuration, "search") {
		return nil
	}
	registry := run.Registry()
	if !registry.HasEmbedding() {
		return nil
	}
	selection, err := registry.Embedding()
	if err != nil {
		return err
	}
	var mail *models.Mail
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		if err := RequireBudget(tx, configuration, run.Agent, run.Owner, time.Now()); err != nil {
			return err
		}
		found, err := tx.GetMails([]string{run.Job.SubjectID}, nil)
		if err != nil {
			return err
		}
		if len(found) > 0 {
			mail = found[0]
		}
		return nil
	}); err != nil {
		return err
	}
	if mail == nil {
		return nil
	}
	message, err := BuildMessageContext(ctx, run.Storage(), mail, embedCharacters, false)
	if err != nil {
		return err
	}
	vectors, usage, err := selection.Embedder.Embed(ctx, llm.EmbedRequest{
		Model: selection.Model, Inputs: []string{embedText(message)},
		Dimensions: selection.Dimensions,
	})
	modelName := selection.Name
	RecordUsage(run.Database(), run.Agent.ID, run.Mailbox.ID, modelName, string(models.AgentJobEmbed), usage)
	if err != nil {
		return fmt.Errorf("embedding: %w", err)
	}
	if len(vectors) == 0 || len(vectors[0]) == 0 {
		return fmt.Errorf("the model answered with no vector")
	}
	return run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		return tx.PutMailEmbedding(run.Mailbox.ID, mail.ID, modelName, vectors[0])
	})
}

// backfillEmbeddings queues vectors for every message without one, newest
// first, when the mailbox searches by meaning.
func (self *Agent) backfillEmbeddings(ctx context.Context, run *Run) error {
	configuration := run.Configuration()
	if !run.Source.Search || !FeatureAllowed(configuration, "search") {
		return nil
	}
	registry := run.Registry()
	if !registry.HasEmbedding() {
		return nil
	}
	selection, err := registry.Embedding()
	if err != nil {
		return err
	}
	return run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		ids, err := tx.ListMailWithoutEmbedding(run.Mailbox.ID, selection.Name, 0)
		if err != nil {
			return err
		}
		for _, id := range ids {
			if _, err := self.Enqueue(tx, models.AgentJobEmbed, run.Agent.ID, run.Mailbox.ID, id); err != nil {
				return err
			}
		}
		return nil
	})
}

// meaningSearch ranks a mailbox's messages against a query and returns the
// closest, best first. Nil when the mailbox has no vectors.
func (self *Agent) meaningSearch(ctx context.Context, agent *models.Agent, mailboxId, query string, limit int) ([]string, error) {
	configuration := self.settings.Configuration()
	if !self.settings.Registry.HasEmbedding() || !FeatureAllowed(configuration, "search") {
		return nil, nil
	}
	selection, err := self.settings.Registry.Embedding()
	if err != nil {
		return nil, err
	}
	modelName := selection.Name
	readPage := func(offset int) (candidates []*db.MailEmbedding, err error) {
		err = self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
			candidates, err = tx.ListMailEmbeddingsPage(mailboxId, modelName, offset, embedPage)
			return err
		})
		return candidates, err
	}
	candidates, err := readPage(0)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	vectors, usage, err := selection.Embedder.Embed(ctx, llm.EmbedRequest{
		Model: selection.Model, Inputs: []string{strings.TrimSpace(query)},
		Dimensions: selection.Dimensions,
	})
	if agent != nil {
		RecordUsage(self.settings.Database, agent.ID, mailboxId, modelName, "search", usage)
	}
	if err != nil {
		return nil, fmt.Errorf("embedding the query: %w", err)
	}
	if len(vectors) == 0 {
		return nil, nil
	}
	var scores []mailScore
	for offset := 0; len(candidates) > 0; {
		scores = append(scores, scoreByCosine(vectors[0], candidates)...)
		// Only the best that could still be asked for are carried on from
		// page to page.
		scores = bestScores(scores, limit)
		offset += len(candidates)
		if len(candidates) < embedPage {
			break
		}
		if candidates, err = readPage(offset); err != nil {
			return nil, err
		}
	}
	return mailIDsOf(scores, limit), nil
}

// mailScore is how close one message is to a query.
type mailScore struct {
	id    string
	score float64
}

// scoreByCosine is the candidates above a floor that keeps a search for
// something the mailbox has none of from answering with its least unrelated
// messages, with how close each is.
func scoreByCosine(query []float32, candidates []*db.MailEmbedding) []mailScore {
	queryNorm := norm(query)
	if queryNorm == 0 {
		return nil
	}
	var scores []mailScore
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
		score := dot / (queryNorm * candidateNorm)
		if score >= meaningFloor {
			scores = append(scores, mailScore{candidate.MailID, score})
		}
	}
	return scores
}

// searchLimit is how many hits a search of this limit answers with.
func searchLimit(limit int) int {
	if limit <= 0 {
		return 20
	}
	return limit
}

// bestScores is the closest of the scores, best first, as many as a search
// with this limit answers with.
func bestScores(scores []mailScore, limit int) []mailScore {
	sort.SliceStable(scores, func(left, right int) bool { return scores[left].score > scores[right].score })
	return scores[:min(len(scores), searchLimit(limit))]
}

// mailIDsOf is the messages of scores that are already the best, in order.
func mailIDsOf(scores []mailScore, limit int) []string {
	ids := make([]string, 0, min(len(scores), searchLimit(limit)))
	for _, entry := range scores {
		if len(ids) >= searchLimit(limit) {
			break
		}
		ids = append(ids, entry.id)
	}
	return ids
}

// meaningFloor is the least similarity a message needs to count as found.
// Embedding models put unrelated text around 0.1 to 0.3 apart; a quarter
// keeps out noise without losing a paraphrase.
const meaningFloor = 0.25

func norm(vector []float32) float64 {
	var sum float64
	for _, value := range vector {
		sum += float64(value) * float64(value)
	}
	return math.Sqrt(sum)
}

// MeaningSearch is meaningSearch for the API and for tests.
func (self *Agent) MeaningSearch(ctx context.Context, agent *models.Agent, mailboxId, query string, limit int) ([]string, error) {
	return self.meaningSearch(ctx, agent, mailboxId, query, limit)
}
