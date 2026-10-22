package apigraph

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

const personCardFinanceFields = `finance { sectionState hasMore financeAccounts { financeAccountId accountName accountMask accountKind financeShareLevel currentBalance creditUsage { usageShare } } }`

func (self *personCardWorld) financeCardOf(test *testing.T, viewer, person *models.User) map[string]any {
	test.Helper()
	data := self.run(test, viewer, fmt.Sprintf(`query { ReadPersonCard(userId: %q) { %s } }`, person.ID, personCardFinanceFields), nil)
	return data["ReadPersonCard"].(map[string]any)
}

// The card shows the finance accounts a person shares with the caller, each as
// its level shows it, and not shared when none is; and the person's agent,
// reading the same card, is never shown them.
func TestReadPersonCardShowsTheFinanceTheyShareButNotToTheAgent(test *testing.T) {
	world := newPersonCardWorld(test)
	checkingID, cardID := seedFinance(test, world.database, world.ada)

	if state := stateOf(world.financeCardOf(test, world.ben, world.ada), "finance"); state != "notShared" {
		test.Fatalf("nothing is shared: %q", state)
	}

	dbtest.RunTransactionOn(test, world.database, func(tx db.Transaction) {
		for accountID, level := range map[string]models.FinanceShareLevel{checkingID: models.FinanceShareLevelTransactions, cardID: models.FinanceShareLevelBalance} {
			if _, err := tx.ShareFinanceAccount(world.ada.ID, &models.FinanceShare{
				FinanceAccountID: accountID, GranteeUserID: world.ben.ID, FinanceShareLevel: level,
			}); err != nil {
				test.Fatal(err)
			}
		}
	})
	card := world.financeCardOf(test, world.ben, world.ada)
	section := sectionOf(card, "finance")
	accounts, _ := section["financeAccounts"].([]any)
	if section["sectionState"] != "shown" || len(accounts) != 2 || section["hasMore"] != false {
		test.Fatalf("the accounts they share: %v", section)
	}
	checking, cardAccount := accounts[0].(map[string]any), accounts[1].(map[string]any)
	if checking["accountName"] != "Everyday Checking" || checking["financeShareLevel"] != "transactions" || checking["currentBalance"] != "1200.5000" ||
		cardAccount["accountKind"] != "credit" || cardAccount["financeShareLevel"] != "balance" || cardAccount["creditUsage"] == nil {
		test.Errorf("each as its level shows it: %v", accounts)
	}
	limited := world.run(test, world.ben, fmt.Sprintf(`query { ReadPersonCard(userId: %q, itemLimit: 1) { %s } }`, world.ada.ID, personCardFinanceFields), nil)
	if kept := sectionOf(limited["ReadPersonCard"].(map[string]any), "finance"); len(kept["financeAccounts"].([]any)) != 1 || kept["hasMore"] != true {
		test.Errorf("one account kept, and the card says there is more: %v", kept)
	}

	// A share is one way: ada's card of ben shows her nothing.
	if state := stateOf(world.financeCardOf(test, world.ada, world.ben), "finance"); state != "notShared" {
		test.Errorf("a share is one way: %q", state)
	}

	// The agent of the person reads the same card and is shown none of it.
	operations := &agentOperations{graph: world.graph, user: world.ben, permissions: &models.EffectivePermissions{Everywhere: []models.Permission{
		models.PermissionAgentUse, models.PermissionCircleUse,
	}}}
	var found struct {
		ReadPersonCard map[string]any `json:"ReadPersonCard"`
	}
	if err := operations.Execute(test.Context(), fmt.Sprintf(`query { ReadPersonCard(userId: %q) { %s } }`, world.ada.ID, personCardFinanceFields), nil, &found); err != nil {
		test.Fatal(err)
	}
	agentSection := sectionOf(found.ReadPersonCard, "finance")
	if agentSection["sectionState"] != "unavailable" || len(agentSection["financeAccounts"].([]any)) != 0 || strings.Contains(fmt.Sprint(agentSection), "Everyday Checking") {
		test.Errorf("the agent is shown the finance shared with the person: %v", agentSection)
	}
}

// A person who may not use the agent area is told it is unavailable, not that
// nothing is shared.
func TestReadPersonCardLeavesOutFinanceForSomebodyWhoMayNotUseIt(test *testing.T) {
	world := newPersonCardWorld(test)
	world.join(test, world.ada, world.home, world.cora)
	if state := stateOf(world.financeCardOf(test, world.cora, world.ada), "finance"); state != "unavailable" {
		test.Errorf("finance for somebody without agent:use is %q", state)
	}
}
