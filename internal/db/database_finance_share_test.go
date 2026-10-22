package db_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// financeShareWorld is shareWorld with finance: ada has an agent and two
// finance accounts (a checking account and a card) with three transactions,
// ben has an agent too, and ada, ben and dave may use the agent (agent:use,
// which every finance operation needs); cora may not.
type financeShareWorld struct {
	*shareWorld
	agentId  string
	checking *models.FinanceAccount
	card     *models.FinanceAccount

	groceryTransactionID string
}

func newFinanceShareWorld(t *testing.T, tx db.Transaction) *financeShareWorld {
	t.Helper()
	world := &financeShareWorld{shareWorld: newShareWorld(t, tx)}
	for _, person := range []*models.User{world.ada, world.ben, world.dave} {
		dbtest.GrantPermission(t, tx, person.ID, models.PermissionAgentUse)
	}
	// Making an account makes its agent, off until the person turns it on.
	agent, err := tx.GetAgentByUser(world.ada.ID)
	if err != nil || agent == nil {
		t.Fatalf("GetAgentByUser: %+v %v", agent, err)
	}
	if agent, err = tx.UpdateAgent(agent.ID, func(found *models.Agent) error { found.Enabled = true; return nil }); err != nil {
		t.Fatalf("UpdateAgent: %s", err)
	}
	world.agentId = agent.ID
	source, err := tx.PutAgentSource(&models.AgentKnowledgeSource{
		AgentID: agent.ID, Kind: models.SourceWeb, Name: "institution",
		Specification: models.AgentKnowledgeSpecification{Start: "https://example.test/"},
	})
	if err != nil {
		t.Fatalf("PutAgentSource: %s", err)
	}
	if _, err := tx.ApplyFinanceSync(agent.ID, source.ID, sampleFinanceSync(), "2026-09-15"); err != nil {
		t.Fatalf("ApplyFinanceSync: %s", err)
	}
	accounts, err := tx.ListFinanceAccounts(agent.ID, "")
	if err != nil || len(accounts) != 2 {
		t.Fatalf("ListFinanceAccounts: %+v %v", accounts, err)
	}
	for _, account := range accounts {
		if account.AccountName == "Everyday Checking" {
			world.checking = account
		} else {
			world.card = account
		}
	}
	groceries, err := tx.CreateSpendingCategory(&models.SpendingCategory{AgentID: agent.ID, SpendingCategoryName: "Groceries"})
	if err != nil {
		t.Fatalf("CreateSpendingCategory: %s", err)
	}
	transactions := financeTransactionsByProviderId(t, tx, agent.ID)
	world.groceryTransactionID = transactions["transaction-grocer"].ID
	if _, err := tx.SetTransactionCategorization(agent.ID, world.groceryTransactionID, groceries.ID, models.CategorizedByPerson, nil); err != nil {
		t.Fatalf("SetTransactionCategorization: %s", err)
	}
	// What the owner wrote on a charge is not part of any share.
	if _, err := tx.GetFinanceTransaction(agent.ID, world.groceryTransactionID); err != nil {
		t.Fatalf("GetFinanceTransaction: %s", err)
	}
	return world
}

func shareFinance(t *testing.T, tx db.Transaction, world *financeShareWorld, account *models.FinanceAccount, granteeUserId, granteeCircleId string, level models.FinanceShareLevel) *models.FinanceShare {
	t.Helper()
	share, err := tx.ShareFinanceAccount(world.ada.ID, &models.FinanceShare{
		FinanceAccountID: account.ID, GranteeUserID: granteeUserId, GranteeCircleID: granteeCircleId, FinanceShareLevel: level,
	})
	if err != nil {
		t.Fatalf("ShareFinanceAccount: %s", err)
	}
	return share
}

func reachedLevel(t *testing.T, tx db.Transaction, viewerUserId string, account *models.FinanceAccount) models.FinanceShareLevel {
	t.Helper()
	reached, err := tx.ReachFinanceAccount(viewerUserId, account.ID)
	if err != nil {
		t.Fatalf("ReachFinanceAccount: %s", err)
	}
	if reached == nil {
		return ""
	}
	return reached.FinanceShareLevel
}

// Sharing a finance account needs it to be the sharer's own, and a circle in
// common with a person or membership of a circle, and says not found rather
// than why. Sharing again changes the level of the same share.
func TestFinanceShareNeedsAnOwnAccountAndACircleInCommon(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newFinanceShareWorld(t, tx)

		share := shareFinance(t, tx, world, world.checking, world.ben.ID, "", models.FinanceShareLevelSpending)
		if share.ID == "" || share.CreatedByUserID != world.ada.ID || share.FinanceShareLevel != models.FinanceShareLevelSpending {
			t.Fatalf("a share with someone in a shared circle: %+v", share)
		}
		again := shareFinance(t, tx, world, world.checking, world.ben.ID, "", models.FinanceShareLevelBalance)
		if again.ID != share.ID || again.FinanceShareLevel != models.FinanceShareLevelBalance {
			t.Fatalf("sharing again changes the level of the same share: %+v", again)
		}
		for _, stranger := range []*models.User{world.cora, world.dave} {
			_, err := tx.ShareFinanceAccount(world.ada.ID, &models.FinanceShare{
				FinanceAccountID: world.checking.ID, GranteeUserID: stranger.ID, FinanceShareLevel: models.FinanceShareLevelBalance,
			})
			if !errors.Is(err, db.ErrNotFound) {
				t.Fatalf("a share with %s needs a circle in common: %v", stranger.Username, err)
			}
		}
		shareFinance(t, tx, world, world.checking, "", world.home.ID, models.FinanceShareLevelBalance)
		if _, err := tx.ShareFinanceAccount(world.ada.ID, &models.FinanceShare{
			FinanceAccountID: world.checking.ID, GranteeCircleID: world.club.ID, FinanceShareLevel: models.FinanceShareLevelBalance,
		}); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("a share with a circle she is not in: %v", err)
		}
		// Only the person whose agent holds the account shares it, and a
		// person who does not exist is the same answer.
		if _, err := tx.ShareFinanceAccount(world.ben.ID, &models.FinanceShare{
			FinanceAccountID: world.checking.ID, GranteeUserID: world.ada.ID, FinanceShareLevel: models.FinanceShareLevelBalance,
		}); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("ben shares an account of ada's: %v", err)
		}
		if _, err := tx.ShareFinanceAccount(world.ada.ID, &models.FinanceShare{
			FinanceAccountID: world.checking.ID, GranteeUserID: "nobody", FinanceShareLevel: models.FinanceShareLevelBalance,
		}); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("a person who does not exist: %v", err)
		}
		if _, err := tx.ShareFinanceAccount(world.ada.ID, &models.FinanceShare{
			FinanceAccountID: "nothing", GranteeUserID: world.ben.ID, FinanceShareLevel: models.FinanceShareLevelBalance,
		}); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("an account that does not exist: %v", err)
		}
		for name, bad := range map[string]*models.FinanceShare{
			"nolevel":   {FinanceAccountID: world.checking.ID, GranteeUserID: world.ben.ID},
			"owner":     {FinanceAccountID: world.checking.ID, GranteeUserID: world.ben.ID, FinanceShareLevel: "owner"},
			"wrongcase": {FinanceAccountID: world.checking.ID, GranteeUserID: world.ben.ID, FinanceShareLevel: "Balance"},
			"both":      {FinanceAccountID: world.checking.ID, GranteeUserID: world.ben.ID, GranteeCircleID: world.home.ID, FinanceShareLevel: models.FinanceShareLevelBalance},
			"neither":   {FinanceAccountID: world.checking.ID, FinanceShareLevel: models.FinanceShareLevelBalance},
			"herself":   {FinanceAccountID: world.checking.ID, GranteeUserID: world.ada.ID, FinanceShareLevel: models.FinanceShareLevelBalance},
		} {
			if _, err := tx.ShareFinanceAccount(world.ada.ID, bad); !errors.Is(err, db.ErrInvalidArguments) {
				t.Errorf("a share with %s is refused: %v", name, err)
			}
		}

		listed, err := tx.ListFinanceSharesBy(world.ada.ID)
		if err != nil || len(listed) != 2 {
			t.Fatalf("ListFinanceSharesBy: %+v %v", listed, err)
		}
		if listed[0].GranteeUsername != "ben" || listed[0].GranteeName == "" || listed[0].OwnerUsername != "ada" ||
			listed[0].AccountName != "Everyday Checking" || listed[0].AccountMask != "0001" || listed[1].GranteeCircleName != "Home" {
			t.Errorf("the listing says which account, whose, and who each is for: %+v %+v", listed[0], listed[1])
		}
		if others, _ := tx.ListFinanceSharesBy(world.ben.ID); len(others) != 0 {
			t.Errorf("ben shares nothing: %+v", others)
		}

		// Taking a share back is the owner's alone.
		if err := tx.UnshareFinanceAccount(world.ben.ID, share.ID); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("ben takes back ada's share: %v", err)
		}
		if err := tx.UnshareFinanceAccount(world.ada.ID, share.ID); err != nil {
			t.Fatalf("UnshareFinanceAccount: %s", err)
		}
		if err := tx.UnshareFinanceAccount(world.ada.ID, share.ID); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("taking back what is not there: %v", err)
		}
	})
}

// A level shows what it adds and nothing of what a higher one adds: a balance
// has no spending, spending has no transactions, and a transaction has no
// description, annotation or provider data at any level.
func TestAFinanceShareShowsOnlyWhatItsLevelAdds(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newFinanceShareWorld(t, tx)
		if _, err := tx.ListSharedFinanceSpending(world.ben.ID, world.checking.ID, "", ""); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("spending before any share: %v", err)
		}

		shareFinance(t, tx, world, world.checking, world.ben.ID, "", models.FinanceShareLevelBalance)
		if got := reachedLevel(t, tx, world.ben.ID, world.checking); got != models.FinanceShareLevelBalance {
			t.Fatalf("the level that reaches ben: %q", got)
		}
		if _, err := tx.ListSharedFinanceSpending(world.ben.ID, world.checking.ID, "", ""); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("spending at the balance level: %v", err)
		}
		if _, err := tx.ListSharedFinanceTransactions(world.ben.ID, world.checking.ID, nil); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("transactions at the balance level: %v", err)
		}
		// The card is not shared, so it is not reached at all.
		if got := reachedLevel(t, tx, world.ben.ID, world.card); got != "" {
			t.Errorf("an account that is not shared: %q", got)
		}

		shareFinance(t, tx, world, world.checking, world.ben.ID, "", models.FinanceShareLevelSpending)
		rows, err := tx.ListSharedFinanceSpending(world.ben.ID, world.checking.ID, "", "")
		if err != nil {
			t.Fatalf("spending at the spending level: %s", err)
		}
		byCategory := map[string]*models.SharedFinanceSpendingRow{}
		for _, row := range rows {
			byCategory[row.SpendingCategoryName] = row
		}
		if len(rows) != 2 || byCategory["Groceries"] == nil || byCategory["Groceries"].SpendingMonth != "2026-09" ||
			byCategory["Groceries"].MoneyOut != "42.1700" || byCategory["Groceries"].MoneyIn != "0.0000" || byCategory["Groceries"].FinanceTransactionCount != 1 ||
			byCategory[""] == nil || byCategory[""].MoneyIn != "2500.0000" {
			t.Errorf("monthly totals by spending category, the card's diner left out: %+v", rows)
		}
		if encoded, _ := json.Marshal(rows); strings.Contains(string(encoded), "Corner Grocer") || strings.Contains(string(encoded), "CORNER GROCER") {
			t.Errorf("a merchant or a description in the totals: %s", encoded)
		}
		if _, err := tx.ListSharedFinanceTransactions(world.ben.ID, world.checking.ID, nil); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("transactions at the spending level: %v", err)
		}
		// A range narrows the months.
		if rows, err := tx.ListSharedFinanceSpending(world.ben.ID, world.checking.ID, "2026-09-05", "2026-09-30"); err != nil || len(rows) != 1 || rows[0].SpendingCategoryName != "Groceries" {
			t.Errorf("a range: %+v %v", rows, err)
		}

		shareFinance(t, tx, world, world.checking, world.ben.ID, "", models.FinanceShareLevelTransactions)
		page, err := tx.ListSharedFinanceTransactions(world.ben.ID, world.checking.ID, nil)
		if err != nil || len(page.FinanceTransactions) != 2 {
			t.Fatalf("transactions at the transactions level: %+v %v", page, err)
		}
		newest := page.FinanceTransactions[0]
		if newest.ID != world.groceryTransactionID || newest.PostedOn != "2026-09-10" || newest.Amount != "-42.1700" ||
			newest.CurrencyCode != "USD" || newest.MerchantName != "Corner Grocer" || newest.SpendingCategoryName != "Groceries" {
			t.Errorf("day, amount, merchant and category: %+v", newest)
		}
		encoded, _ := json.Marshal(page)
		for _, secret := range []string{"CORNER GROCER 0412", "PAYROLL EXAMPLE CO", "payee", "Lakeside"} {
			if strings.Contains(string(encoded), secret) {
				t.Errorf("%q reaches a reader of transactions: %s", secret, encoded)
			}
		}
		// Paging.
		first, err := tx.ListSharedFinanceTransactions(world.ben.ID, world.checking.ID, &db.SharedFinanceTransactionFilter{Limit: 1})
		if err != nil || len(first.FinanceTransactions) != 1 || first.NextCursor == "" || first.TotalCount != 2 {
			t.Fatalf("the first page: %+v %v", first, err)
		}
		second, err := tx.ListSharedFinanceTransactions(world.ben.ID, world.checking.ID, &db.SharedFinanceTransactionFilter{Limit: 1, After: first.NextCursor})
		if err != nil || len(second.FinanceTransactions) != 1 || second.FinanceTransactions[0].ID == first.FinanceTransactions[0].ID || second.NextCursor != "" {
			t.Fatalf("the second page: %+v %v", second, err)
		}
		byOffset, err := tx.ListSharedFinanceTransactions(world.ben.ID, world.checking.ID, &db.SharedFinanceTransactionFilter{Limit: 1, Offset: 1})
		if err != nil || len(byOffset.FinanceTransactions) != 1 || byOffset.FinanceTransactions[0].ID != second.FinanceTransactions[0].ID || byOffset.TotalCount != 2 {
			t.Fatalf("a page by its offset: %+v %v", byOffset, err)
		}
		// The card's diner is not on the checking account's page, and the
		// account of another owner is never read through this one.
		if _, err := tx.ListSharedFinanceTransactions(world.ben.ID, world.card.ID, nil); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("an account that is not shared: %v", err)
		}
	})
}

// Whoever reaches an account through a direct share and through a circle gets
// the higher level, and an account reaches nobody the owner has not shared it
// with.
func TestTheHighestFinanceShareLevelWins(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newFinanceShareWorld(t, tx)
		shareFinance(t, tx, world, world.checking, "", world.home.ID, models.FinanceShareLevelTransactions)
		shareFinance(t, tx, world, world.checking, world.ben.ID, "", models.FinanceShareLevelBalance)
		if got := reachedLevel(t, tx, world.ben.ID, world.checking); got != models.FinanceShareLevelTransactions {
			t.Fatalf("a direct balance share and a circle's transactions share: %q", got)
		}
		reaching, err := tx.ListFinanceSharesReaching(world.ben.ID)
		if err != nil || len(reaching) != 1 || reaching[0].FinanceShareLevel != models.FinanceShareLevelTransactions ||
			reaching[0].OwnerUsername != "ada" || reaching[0].AccountName != "Everyday Checking" || reaching[0].OwnerUserID != world.ada.ID {
			t.Fatalf("one share per account, the highest, saying whose it is: %+v %v", reaching, err)
		}
		reached, err := tx.ListReachedFinanceAccounts(world.ben.ID, "")
		if err != nil || len(reached) != 1 || reached[0].Account.ID != world.checking.ID || reached[0].OwnerUsername != "ada" {
			t.Fatalf("ListReachedFinanceAccounts: %+v %v", reached, err)
		}
		if onlyAda, _ := tx.ListReachedFinanceAccounts(world.ben.ID, world.ada.ID); len(onlyAda) != 1 {
			t.Errorf("of one owner: %+v", onlyAda)
		}
		if onlyDave, _ := tx.ListReachedFinanceAccounts(world.ben.ID, world.dave.ID); len(onlyDave) != 0 {
			t.Errorf("of an owner who shares nothing: %+v", onlyDave)
		}
		// A share is one way, and the owner's own account is not "shared".
		if got := reachedLevel(t, tx, world.ada.ID, world.checking); got != "" {
			t.Errorf("the owner reads their own without a share: %q", got)
		}
		for _, other := range []*models.User{world.cora, world.dave} {
			if got := reachedLevel(t, tx, other.ID, world.checking); got != "" {
				t.Errorf("%s: %q", other.Username, got)
			}
		}
		if none, _ := tx.ListFinanceSharesReaching(world.ada.ID); len(none) != 0 {
			t.Errorf("nothing reaches ada: %+v", none)
		}
	})
}

// A finance share reaches only people who may use finance, from an owner who
// may, whose account and agent are on, and who is in the circle.
func TestAFinanceShareReachesOnlyPeopleWhoMayUseFinance(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newFinanceShareWorld(t, tx)
		shareFinance(t, tx, world, world.checking, "", world.home.ID, models.FinanceShareLevelTransactions)

		// A viewer in the circle without agent:use gets nothing: cora is not
		// in "home", so put her in.
		if _, err := tx.AddCircleMember(world.home.ID, world.cora.ID, models.MemberRoleMember, world.ada.ID); err != nil {
			t.Fatal(err)
		}
		if got := reachedLevel(t, tx, world.cora.ID, world.checking); got != "" {
			t.Fatalf("a viewer without agent:use: %q", got)
		}
		dbtest.GrantPermission(t, tx, world.cora.ID, models.PermissionAgentUse)
		if got := reachedLevel(t, tx, world.cora.ID, world.checking); got != models.FinanceShareLevelTransactions {
			t.Fatalf("with agent:use: %q", got)
		}

		// A viewer who is disabled.
		if _, err := tx.UpdateUser(world.cora.ID, func(person *models.User) error {
			now := time.Now()
			person.DisabledAt = &now
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if got := reachedLevel(t, tx, world.cora.ID, world.checking); got != "" {
			t.Fatalf("a disabled viewer: %q", got)
		}

		// An agent that is switched off, or that the operator disabled.
		if _, err := tx.UpdateAgent(world.agentId, func(agent *models.Agent) error { agent.Enabled = false; return nil }); err != nil {
			t.Fatal(err)
		}
		if got := reachedLevel(t, tx, world.ben.ID, world.checking); got != "" {
			t.Fatalf("an agent that is off: %q", got)
		}
		if _, err := tx.UpdateAgent(world.agentId, func(agent *models.Agent) error {
			now := time.Now()
			agent.Enabled, agent.OperatorDisabledAt = true, &now
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if got := reachedLevel(t, tx, world.ben.ID, world.checking); got != "" {
			t.Fatalf("an agent the operator disabled: %q", got)
		}
		if _, err := tx.UpdateAgent(world.agentId, func(agent *models.Agent) error { agent.OperatorDisabledAt = nil; return nil }); err != nil {
			t.Fatal(err)
		}
		if got := reachedLevel(t, tx, world.ben.ID, world.checking); got != models.FinanceShareLevelTransactions {
			t.Fatalf("with the agent back on: %q", got)
		}

		// A disabled owner's accounts reach nobody.
		if _, err := tx.UpdateUser(world.ada.ID, func(owner *models.User) error {
			now := time.Now()
			owner.DisabledAt = &now
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if got := reachedLevel(t, tx, world.ben.ID, world.checking); got != "" {
			t.Fatalf("a disabled owner: %q", got)
		}
	})
}

// A share to a person ends when the two no longer share a circle, and a share
// to a circle ends when the sharer leaves it; a circle's member who leaves
// stops reading it.
func TestLeavingACircleEndsTheFinanceSharesItMadePossible(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newFinanceShareWorld(t, tx)
		shareFinance(t, tx, world, world.checking, world.ben.ID, "", models.FinanceShareLevelBalance)
		shareFinance(t, tx, world, world.card, "", world.home.ID, models.FinanceShareLevelSpending)

		// Ben leaves "home": the direct share to him is gone, not just silent,
		// and the circle's share no longer reaches him.
		if _, err := tx.RemoveCircleMember(world.home.ID, world.ben.ID); err != nil {
			t.Fatalf("RemoveCircleMember: %s", err)
		}
		if got := reachedLevel(t, tx, world.ben.ID, world.checking); got != "" {
			t.Errorf("ada's share to ben after they no longer meet: %q", got)
		}
		if got := reachedLevel(t, tx, world.ben.ID, world.card); got != "" {
			t.Errorf("the circle's share after ben left it: %q", got)
		}
		left, err := tx.ListFinanceSharesBy(world.ada.ID)
		if err != nil || len(left) != 1 || left[0].GranteeCircleName != "Home" {
			t.Errorf("only the share to the circle is left, and it is still ada's: %+v %v", left, err)
		}

		// Ben comes back and the circle's share reaches him again, with no
		// new share made.
		if _, err := tx.AddCircleMember(world.home.ID, world.ben.ID, models.MemberRoleMember, world.ada.ID); err != nil {
			t.Fatal(err)
		}
		if got := reachedLevel(t, tx, world.ben.ID, world.card); got != models.FinanceShareLevelSpending {
			t.Errorf("a member who joins reads what the circle was shared: %q", got)
		}

		// Ada leaves her own circle: her shares to it end.
		if _, err := tx.AddCircleMember(world.home.ID, world.dave.ID, models.MemberRoleAdmin, world.ada.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.RemoveCircleMember(world.home.ID, world.ada.ID); err != nil {
			t.Fatalf("RemoveCircleMember: %s", err)
		}
		if left, _ := tx.ListFinanceSharesBy(world.ada.ID); len(left) != 0 {
			t.Errorf("a share to a circle its sharer left: %+v", left)
		}
		if got := reachedLevel(t, tx, world.ben.ID, world.card); got != "" {
			t.Errorf("after the sharer left: %q", got)
		}
	})
}

// The circle's joining guard asks whether anything sensitive is shared with
// it, and a finance account counts as Health does.
func TestACircleWithAFinanceShareIsSensitive(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newFinanceShareWorld(t, tx)
		isSensitive, err := tx.CircleHasSensitiveShare(world.home.ID)
		if err != nil || isSensitive {
			t.Fatalf("a circle with nothing shared: %v %v", isSensitive, err)
		}
		shareFinance(t, tx, world, world.checking, world.ben.ID, "", models.FinanceShareLevelBalance)
		if isSensitive, _ := tx.CircleHasSensitiveShare(world.home.ID); isSensitive {
			t.Errorf("a share to a person is not a share to the circle")
		}
		shareFinance(t, tx, world, world.checking, "", world.home.ID, models.FinanceShareLevelBalance)
		if isSensitive, err := tx.CircleHasSensitiveShare(world.home.ID); err != nil || !isSensitive {
			t.Errorf("a finance account shared with the circle: %v %v", isSensitive, err)
		}
		if isSensitive, _ := tx.CircleHasSensitiveShare(world.club.ID); isSensitive {
			t.Errorf("another circle")
		}
		if _, err := tx.SetHealthShare(world.ben.ID, &models.HealthShare{GranteeCircleID: world.club.ID, HealthCategories: []string{"sleep"}}); err != nil {
			t.Fatal(err)
		}
		if isSensitive, err := tx.CircleHasSensitiveShare(world.club.ID); err != nil || !isSensitive {
			t.Errorf("Health shared with the circle: %v %v", isSensitive, err)
		}
	})
}

// Deleting a finance account takes its shares with it.
func TestDeletingAFinanceAccountDeletesItsShares(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newFinanceShareWorld(t, tx)
		shareFinance(t, tx, world, world.card, world.ben.ID, "", models.FinanceShareLevelBalance)
		if _, err := tx.DeleteFinanceAccount(world.agentId, world.card.ID); err != nil {
			t.Fatalf("DeleteFinanceAccount: %s", err)
		}
		if left, _ := tx.ListFinanceSharesBy(world.ada.ID); len(left) != 0 {
			t.Errorf("shares of a deleted account: %+v", left)
		}
		if got := reachedLevel(t, tx, world.ben.ID, world.card); got != "" {
			t.Errorf("a deleted account: %q", got)
		}
	})
}
