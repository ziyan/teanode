package apigraph

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A chat source is made only by the switch on the chat itself
// (SetChatAgentReading), so creating one here is refused with words that say
// where the switch is, not words about linking an institution.
func TestAChatSourceIsNotMadeFromTheSourceList(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	var owner *models.User
	dbtest.RunTransactionOn(t, database, func(transaction db.Transaction) {
		var err error
		if owner, err = transaction.CreateUser(&models.User{Username: "chat-source-owner", Name: "Alice Example"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if _, err = transaction.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Name: "Bertie"}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
	})
	person := &api.Principal{User: owner, Permissions: models.NewEffectivePermissions([]models.Grant{
		{Permission: models.PermissionAgentUse},
	})}
	resolver := &graph{database: database}
	dbtest.RunTransactionOn(t, database, func(transaction db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), person), transaction)
		_, err := resolver.SaveAgentKnowledgeSource(ctx, SaveAgentKnowledgeSourceArguments{Kind: string(models.SourceChat)})
		if !errors.Is(err, api.ErrInvalidArguments) {
			t.Fatalf("a chat source was made from the list: %v", err)
		}
		for _, wanted := range []string{"remembered from the chat itself", "My agent remembers this chat", "members panel"} {
			if !strings.Contains(err.Error(), wanted) {
				t.Errorf("the refusal lacks %q: %s", wanted, err)
			}
		}
		if strings.Contains(err.Error(), "linking an institution") {
			t.Errorf("the refusal is worded for a finance source: %s", err)
		}
	})
}
