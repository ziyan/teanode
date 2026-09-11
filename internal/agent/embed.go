package agent

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// Search by meaning: each message of a mailbox that searches this way gets
// a vector when it arrives (and the newest ones when the mailbox is
// granted), and a search embeds its words and ranks the mailbox's vectors
// by cosine in the server. No vector database: a personal mailbox has
// thousands of messages, not millions, and the candidates fit in memory.

// The bounds.
const (
	// embedCharacters is how much of a message goes into its vector: the
	// subject and the start of the text, which is where the meaning is.
	embedCharacters = 4000

	// embedCandidates is how many of a mailbox's newest vectors a search
	// ranks.
	embedCandidates = 3000

	// embedBackfill is how many messages get vectors when a mailbox is
	// granted with search by meaning on.
	embedBackfill = 200
)

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
	if !FeatureAllowed(configuration, "search") || configuration.Agent.Models.Embedding == "" {
		return nil
	}
	registry := run.Registry()
	if registry == nil {
		return fmt.Errorf("no model registry")
	}
	embedder, model, err := registry.Embedding()
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
	callContext, cancel := context.WithTimeout(ctx, configuration.Agent.Limits.RequestTimeout.Duration())
	defer cancel()
	vectors, usage, err := embedder.Embed(callContext, model, []string{embedText(message)})
	modelName := configuration.Agent.Models.Embedding
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

// backfillEmbeddings queues vectors for the newest messages without one,
// when the mailbox searches by meaning.
func (self *Agent) backfillEmbeddings(ctx context.Context, run *Run) error {
	configuration := run.Configuration()
	if !run.Source.Search || !FeatureAllowed(configuration, "search") || configuration.Agent.Models.Embedding == "" {
		return nil
	}
	return run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		ids, err := tx.ListMailWithoutEmbedding(run.Mailbox.ID, configuration.Agent.Models.Embedding, embedBackfill)
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
	if self.settings.Registry == nil || configuration.Agent.Models.Embedding == "" || !FeatureAllowed(configuration, "search") {
		return nil, nil
	}
	embedder, model, err := self.settings.Registry.Embedding()
	if err != nil {
		return nil, err
	}
	modelName := configuration.Agent.Models.Embedding
	var candidates []*db.MailEmbedding
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		candidates, err = tx.ListMailEmbeddings(mailboxId, modelName, embedCandidates)
		return err
	}); err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	callContext, cancel := context.WithTimeout(ctx, configuration.Agent.Limits.RequestTimeout.Duration())
	defer cancel()
	vectors, usage, err := embedder.Embed(callContext, model, []string{strings.TrimSpace(query)})
	if agent != nil {
		RecordUsage(self.settings.Database, agent.ID, mailboxId, modelName, "search", usage)
	}
	if err != nil {
		return nil, fmt.Errorf("embedding the query: %w", err)
	}
	if len(vectors) == 0 {
		return nil, nil
	}
	return rankByCosine(vectors[0], candidates, limit), nil
}

// rankByCosine is the candidates closest to the query, best first, above a
// floor that keeps a search for something the mailbox has none of from
// answering with its least unrelated messages.
func rankByCosine(query []float32, candidates []*db.MailEmbedding, limit int) []string {
	type scored struct {
		id    string
		score float64
	}
	queryNorm := norm(query)
	if queryNorm == 0 {
		return nil
	}
	var ranked []scored
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
			ranked = append(ranked, scored{candidate.MailID, score})
		}
	}
	sort.SliceStable(ranked, func(left, right int) bool { return ranked[left].score > ranked[right].score })
	if limit <= 0 {
		limit = 20
	}
	ids := make([]string, 0, limit)
	for _, entry := range ranked {
		if len(ids) >= limit {
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

// searchMode says how a mailbox is searched: by meaning where it can be.
func searchMode(configuration *config.Configuration, source *models.AgentMailbox) string {
	if source != nil && source.Search && configuration.Agent.Models.Embedding != "" && FeatureAllowed(configuration, "search") {
		return "meaning"
	}
	return "keyword"
}

// MeaningSearch is meaningSearch for the API and for tests.
func (self *Agent) MeaningSearch(ctx context.Context, agent *models.Agent, mailboxId, query string, limit int) ([]string, error) {
	return self.meaningSearch(ctx, agent, mailboxId, query, limit)
}
