package apigraph

import (
	"context"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// Passwords is open only to a run the person started with their own tap. The
// surface a request names is the client's word: a command line token that
// says "drawer" starts a run that cannot list or read anything in Passwords,
// and neither can a run an operator started as the person or in their
// conversation. The dashboard's session and the phone app can.
func TestOnlyThePersonsOwnTapStartsARunThatMayUsePasswords(t *testing.T) {
	world := newChatWorld(t)
	world.withModel(t, []string{dentistRound})
	var conversation *models.AgentConversation
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		var err error
		if conversation, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: world.carolAgent.ID, Kind: models.AgentConversationNamed, Surface: "drawer", LastAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	})

	personGrants := models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionAgentUse}})
	operatorGrants := models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionAgentUse}, {Permission: models.PermissionAgentAct}})
	for name, test := range map[string]struct {
		principal      *api.Principal
		conversationID string
		surface        string
		expected       bool
	}{
		"the dashboard's session":                   {principal: &api.Principal{User: world.frank, Permissions: personGrants, CredentialKind: models.CredentialSession}, surface: "drawer", expected: true},
		"the dashboard's session naming the cli":    {principal: &api.Principal{User: world.frank, Permissions: personGrants, CredentialKind: models.CredentialSession}, surface: "cli", expected: true},
		"the phone app":                             {principal: &api.Principal{User: world.frank, Permissions: personGrants, CredentialKind: models.CredentialApp}, surface: "ios", expected: true},
		"a command line token naming the drawer":    {principal: &api.Principal{User: world.frank, Permissions: personGrants, CredentialKind: models.CredentialToken}, surface: "drawer"},
		"a command line token naming the phone":     {principal: &api.Principal{User: world.frank, Permissions: personGrants, CredentialKind: models.CredentialToken}, surface: "ios"},
		"a token naming the extension":              {principal: &api.Principal{User: world.frank, Permissions: personGrants, CredentialKind: models.CredentialToken}, surface: "extension"},
		"a request with no credential kind":         {principal: &api.Principal{User: world.frank, Permissions: personGrants}, surface: "drawer"},
		"an operator signed in as the person":       {principal: &api.Principal{User: world.frank, Impersonator: world.heidi, Permissions: personGrants, CredentialKind: models.CredentialSession}, surface: "drawer"},
		"an operator speaking into their dashboard": {principal: &api.Principal{User: world.frank, Permissions: operatorGrants, CredentialKind: models.CredentialSession}, conversationID: conversation.ID, surface: "drawer"},
		"an operator's phone speaking into theirs":  {principal: &api.Principal{User: world.frank, Permissions: operatorGrants, CredentialKind: models.CredentialApp}, conversationID: conversation.ID, surface: "ios"},
	} {
		t.Run(name, func(t *testing.T) {
			var turn *AgentTurnView
			dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
				ctx := api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), test.principal), tx)
				var err error
				if turn, err = world.graph.AskAgent(ctx, AskAgentArguments{Message: "What is on tomorrow?", ConversationID: test.conversationID, Surface: test.surface}); err != nil {
					t.Fatalf("AskAgent: %s", err)
				}
			})
			run := world.worker.FindRun(turn.RunID)
			if run == nil {
				t.Fatal("the run is not known")
			}
			if got := run.IsStartedByPerson(); got != test.expected {
				t.Fatalf("IsStartedByPerson = %v, want %v", got, test.expected)
			}
		})
	}
}
