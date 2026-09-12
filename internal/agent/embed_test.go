package agent_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// A message arriving in a mailbox that searches by meaning gets a vector;
// a search embeds its words and finds the message that says the same
// thing in other words, and not the one that does not.
func TestEmbeddingsAndSearchByMeaning(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	// A toy embedding: three axes — plumbing, sailing, money — from words.
	vectorOf := func(text string) []float32 {
		text = strings.ToLower(text)
		vector := []float32{0.01, 0.01, 0.01}
		for _, word := range []string{"plumber", "pipe", "leak", "roof", "invoice"} {
			if strings.Contains(text, word) {
				vector[0] += 1
			}
		}
		for _, word := range []string{"sail", "boat", "regatta"} {
			if strings.Contains(text, word) {
				vector[1] += 1
			}
		}
		for _, word := range []string{"invoice", "pay", "euro", "bill"} {
			if strings.Contains(text, word) {
				vector[2] += 1
			}
		}
		return vector
	}
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !strings.HasSuffix(request.URL.Path, "/embeddings") {
			http.Error(writer, "only embeddings here", 404)
			return
		}
		var body struct {
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(request.Body).Decode(&body)
		data := make([]map[string]any, 0, len(body.Input))
		for index, input := range body.Input {
			data = append(data, map[string]any{"index": index, "embedding": vectorOf(input)})
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"data": data, "usage": map[string]int{"prompt_tokens": 7, "total_tokens": 7}})
	}))
	defer provider.Close()

	configuration := config.Default()
	configuration.Agent.Enabled = true
	configuration.Agent.Providers = []config.AgentProvider{{Name: "fake", Kind: "openai", BaseURL: provider.URL, APIKey: "k"}}
	configuration.Agent.Models.Default = "fake:thinker"
	configuration.Agent.Models.Embedding = "fake:embedder"
	registry, err := llm.Open(&configuration.Agent)
	if err != nil {
		t.Fatalf("llm.Open: %s", err)
	}
	store, err := storage.Open(&storage.Settings{Directory: t.TempDir()})
	if err != nil {
		t.Fatalf("storage.Open: %s", err)
	}
	worker := agent.New(&agent.Settings{Database: database, Storage: store, Registry: registry, Configuration: func() *config.Configuration { return configuration }, Instance: "test", Tick: time.Hour})

	var owner *models.User
	var mailbox *models.Mailbox
	var found *models.Agent
	var inbox *models.MailboxFolder
	var ids []string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if owner, err = tx.CreateUser(&models.User{Username: "alice"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if found, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if mailbox, err = tx.CreateMailbox(&models.Mailbox{UserID: owner.ID, Name: "Personal", Agent: &models.AgentMailbox{Granted: true, Search: true}}); err != nil {
			t.Fatalf("CreateMailbox: %s", err)
		}
		if inbox, err = tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindInbox); err != nil || inbox == nil {
			t.Fatalf("GetFolderByKind: %v %s", inbox, err)
		}
		for _, entry := range []struct{ subject, body string }{
			{"Your bill", "The pipe under the sink leaks again; the repair comes to 120 euro."},
			{"Regatta", "The boat is ready; we sail on Sunday."},
		} {
			mail, err := tx.CreateMail(&models.Mail{Subject: entry.subject, Kind: models.MailKindIncoming, Sender: "x@example.net", ReceivedAt: time.Now()}, nil)
			if err != nil {
				t.Fatalf("CreateMail: %s", err)
			}
			if err := store.Put(context.Background(), mail.ID, []string{"Content-Type: text/plain"}, []byte(entry.body)); err != nil {
				t.Fatalf("store.Put: %s", err)
			}
			if _, err := tx.AddItem(inbox.ID, mail.ID, "", models.MailboxItemFlags{}); err != nil {
				t.Fatalf("AddItem: %s", err)
			}
			worker.OnMailboxDelivery(tx, mailbox, nil, mail)
			ids = append(ids, mail.ID)
		}
		jobs, _ := tx.ListAgentJobs(&db.AgentJobFilter{AgentID: found.ID, Kinds: []models.AgentJobKind{models.AgentJobEmbed}}, nil)
		if len(jobs) != 2 {
			t.Fatalf("two embed jobs were expected, got %d", len(jobs))
		}
	})
	if err := worker.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %s", err)
	}
	worker.Wait()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		vectors, err := tx.ListMailEmbeddings(mailbox.ID, "fake:embedder", 10)
		if err != nil || len(vectors) != 2 {
			t.Fatalf("two vectors were expected: %d %v", len(vectors), err)
		}
		missing, _ := tx.ListMailWithoutEmbedding(mailbox.ID, "fake:embedder", 10)
		if len(missing) != 0 {
			t.Fatalf("nothing should be missing a vector: %v", missing)
		}
	})
	// "the invoice from the plumber" never appears in either message.
	hits, err := worker.MeaningSearch(context.Background(), found, mailbox.ID, "the invoice from the plumber", 5)
	if err != nil {
		t.Fatalf("MeaningSearch: %s", err)
	}
	if len(hits) != 1 || hits[0] != ids[0] {
		t.Fatalf("the plumbing bill should be found and the regatta not: %v (bill %s)", hits, ids[0])
	}
}
