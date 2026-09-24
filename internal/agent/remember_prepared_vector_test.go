package agent_test

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sync"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
)

// The page is opened before the fact's embedding request. Renaming it in
// that request reproduces a real edit between preparation and filing.
func TestRememberDoesNotStoreMeaningPreparedForOldPageName(t *testing.T) {
	const words = "The boiler needs an annual inspection."
	message := regexp.MustCompile(`\[([a-zA-Z0-9]+)\] them: ` + regexp.QuoteMeta(words))
	world := newRememberWorldThatEmbeds(t, func(prompt string) string {
		found := message.FindStringSubmatch(prompt)
		if len(found) == 0 {
			return `{"facts": []}`
		}
		return fmt.Sprintf(`{"facts":[{"path":"things/boiler","node_kind":"thing","node_name":"Before",`+
			`"kind":"fact","text":%q,"message_id":%q,"quote":%q}]}`, words, found[1], words)
	})
	world.say(t, "user", words)

	renameResult := make(chan error, 1)
	var renameOnce sync.Once
	world.embeddingHook = func(request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			renameOnce.Do(func() { renameResult <- err })
			return
		}
		request.Body = io.NopCloser(bytes.NewReader(body))
		if !bytes.Contains(body, []byte("Before: "+words)) {
			return
		}
		renameOnce.Do(func() {
			renameResult <- world.database.Transaction(func(tx db.Transaction) error {
				page, err := tx.GetAgentNode(world.agent.ID, "things/boiler")
				if err != nil || page == nil {
					return fmt.Errorf("opened page: %v %v", page, err)
				}
				page.Name = "After"
				_, err = tx.PutAgentNode(page)
				return err
			})
		})
	}
	world.remember(t)
	select {
	case err := <-renameResult:
		if err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("fact embedding did not overlap a page rename")
	}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		page, err := tx.GetAgentNode(world.agent.ID, "things/boiler")
		if err != nil || page == nil || page.Name != "After" {
			t.Fatalf("renamed page: %v %v", page, err)
		}
		facts, err := tx.ListAgentFacts(world.agent.ID, page.ID, false, 10)
		if err != nil || len(facts) != 1 || facts[0].Text != words {
			t.Fatalf("filed fact: %v %v", facts, err)
		}
		missing, err := tx.ListAgentFactsWithoutVector(world.agent.ID, "fake:meaning", 100)
		if err != nil {
			t.Fatal(err)
		}
		foundMissing := false
		for _, fact := range missing {
			if fact.ID == facts[0].ID {
				foundMissing = true
			}
		}
		if !foundMissing {
			t.Error("fact kept an embedding of its old page name")
		}
		conversation, err := tx.GetAgentConversation(world.conversation.ID)
		if err != nil || conversation == nil || conversation.RememberedThrough == "" {
			t.Errorf("conversation was not marked after filing: %v %v", conversation, err)
		}
	})
}
