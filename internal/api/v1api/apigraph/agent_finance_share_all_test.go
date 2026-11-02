package apigraph

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

const (
	shareAllFinanceQuery = `mutation ($granteeUserId: String, $granteeCircleId: String, $financeShareLevel: String!) {
  ShareFinance(granteeUserId: $granteeUserId, granteeCircleId: $granteeCircleId, financeShareLevel: $financeShareLevel) {
    id granteeUserId granteeUsername granteeName granteeCircleId granteeCircleName financeShareLevel createdAt
  }
}`
	unshareAllFinanceQuery = `mutation ($financeShareId: String!) { UnshareFinance(financeShareId: $financeShareId) }`
	allFinanceSharesQuery  = `query { ListFinanceShares { id granteeUsername granteeCircleName financeShareLevel } }`
	sharedAllFinanceQuery  = `query ($ownerUserId: String) { ListSharedFinance(ownerUserId: $ownerUserId) { ownerUserId ownerUsername ownerName financeShareLevel } }`

	allSpendingQuery = `query ($financeAccountId: String, $ownerUserId: String, $from: String, $to: String) {
  ListSharedFinanceSpending(financeAccountId: $financeAccountId, ownerUserId: $ownerUserId, from: $from, to: $to) {
    spendingMonth spendingCategoryName currencyCode moneyOut moneyIn financeTransactionCount
  }
}`
	allTransactionsQuery = `query ($financeAccountId: String, $ownerUserId: String, $limit: Int, $after: String, $offset: Int) {
  ListSharedFinanceTransactions(financeAccountId: $financeAccountId, ownerUserId: $ownerUserId, limit: $limit, after: $after, offset: $offset) {
    nextCursor totalCount financeTransactions { id postedOn amount currencyCode merchantName spendingCategoryName }
  }
}`
)

func (self *financeShareWorld) shareAll(t *testing.T, credentialKind models.CredentialKind, granteeUserId, granteeCircleId any, level string) (map[string]any, []string) {
	t.Helper()
	return self.callWithCredential(t, self.ada, credentialKind, shareAllFinanceQuery, map[string]any{
		"granteeUserId": granteeUserId, "granteeCircleId": granteeCircleId, "financeShareLevel": level,
	})
}

func (self *financeShareWorld) shareAllAsTap(t *testing.T, granteeUserId, granteeCircleId any, level string) map[string]any {
	t.Helper()
	data, failures := self.shareAll(t, models.CredentialSession, granteeUserId, granteeCircleId, level)
	if len(failures) > 0 {
		t.Fatalf("sharing all finance at %s: %v", level, failures)
	}
	return data["ShareFinance"].(map[string]any)
}

// The combined view at each level over the API: ben is told that ada shares
// all her finance and at which level, reads one Spending view of her accounts
// together, and at the transactions level the transactions of all of them,
// and nothing in either names an account.
func TestAllFinanceShareLevelsOverTheAPI(t *testing.T) {
	world := newFinanceShareWorld(t)
	byOwner := map[string]any{"ownerUserId": world.ada.ID}

	if listed := world.run(t, world.ben, sharedAllFinanceQuery, nil)["ListSharedFinance"].([]any); len(listed) != 0 {
		t.Fatalf("before any share: %v", listed)
	}
	for name, query := range map[string]string{"spending": allSpendingQuery, "transactions": allTransactionsQuery} {
		if message := world.refusal(t, world.ben, query, byOwner); !strings.Contains(strings.ToLower(message), "not found") {
			t.Errorf("%s before any share: %q", name, message)
		}
	}

	shared := world.shareAllAsTap(t, world.ben.ID, nil, "spending")
	if shared["id"] == "" || shared["granteeUsername"] != world.ben.Username || shared["financeShareLevel"] != "spending" || shared["granteeCircleId"] != nil {
		t.Fatalf("the share says who and at what level: %v", shared)
	}
	listed := world.run(t, world.ben, sharedAllFinanceQuery, nil)["ListSharedFinance"].([]any)
	if len(listed) != 1 {
		t.Fatalf("ben is told ada shares all her finance: %v", listed)
	}
	if row := listed[0].(map[string]any); row["ownerUserId"] != world.ada.ID || row["ownerUsername"] != world.ada.Username || row["financeShareLevel"] != "spending" {
		t.Errorf("whose and at which level: %v", row)
	}
	if only := world.run(t, world.ben, sharedAllFinanceQuery, map[string]any{"ownerUserId": world.dave.ID})["ListSharedFinance"].([]any); len(only) != 0 {
		t.Errorf("of another person: %v", only)
	}
	// It names no account, so no account is listed and none can be asked for.
	if accounts := world.run(t, world.ben, sharedAccountsQuery, nil)["ListSharedFinanceAccounts"].([]any); len(accounts) != 0 {
		t.Errorf("a share of all finance lists no account: %v", accounts)
	}
	if message := world.refusal(t, world.ben, sharedSpendingQuery, map[string]any{"financeAccountId": world.checkingID}); !strings.Contains(strings.ToLower(message), "not found") {
		t.Errorf("one account of a person who shares all their finance: %q", message)
	}

	spending := world.run(t, world.ben, allSpendingQuery, byOwner)["ListSharedFinanceSpending"].([]any)
	if len(spending) != 1 {
		t.Fatalf("the months: %v", spending)
	}
	if row := spending[0].(map[string]any); row["spendingMonth"] != "2026-09" || row["currencyCode"] != "USD" || row["moneyOut"] != "42.1700" ||
		row["moneyIn"] != "2500.0000" || row["financeTransactionCount"] != float64(2) {
		t.Errorf("a month across the accounts: %v", row)
	}
	if message := world.refusal(t, world.ben, allTransactionsQuery, byOwner); !strings.Contains(strings.ToLower(message), "not found") {
		t.Errorf("transactions at the spending level: %q", message)
	}

	world.shareAllAsTap(t, world.ben.ID, nil, "transactions")
	page := world.run(t, world.ben, allTransactionsQuery, byOwner)["ListSharedFinanceTransactions"].(map[string]any)
	transactions := page["financeTransactions"].([]any)
	if len(transactions) != 2 || page["totalCount"] != float64(2) {
		t.Fatalf("the transactions: %v", page)
	}
	if newest := transactions[0].(map[string]any); newest["postedOn"] != "2026-09-10" || newest["amount"] != "-42.1700" || newest["merchantName"] != "Corner Grocer" || newest["currencyCode"] != "USD" {
		t.Errorf("day, amount and merchant: %v", newest)
	}
	if second := world.run(t, world.ben, allTransactionsQuery, map[string]any{"ownerUserId": world.ada.ID, "limit": 1, "offset": 1})["ListSharedFinanceTransactions"].(map[string]any); len(second["financeTransactions"].([]any)) != 1 || second["nextCursor"] != "" {
		t.Errorf("the second page by its offset: %v", second)
	}

	// Neither answer has a field for an account, a mask, a balance or what the
	// owner wrote on a charge.
	for _, field := range []string{"financeAccountId", "accountName", "accountMask", "currentBalance", "description", "annotation", "providerMetadata"} {
		for name, query := range map[string]string{
			"spending":     fmt.Sprintf(`query ($ownerUserId: String) { ListSharedFinanceSpending(ownerUserId: $ownerUserId) { %s } }`, field),
			"transactions": fmt.Sprintf(`query ($ownerUserId: String) { ListSharedFinanceTransactions(ownerUserId: $ownerUserId) { financeTransactions { %s } } }`, field),
			"shared":       fmt.Sprintf(`query { ListSharedFinance { %s } }`, field),
		} {
			if message := world.refusal(t, world.ben, query, byOwner); !strings.Contains(message, "Cannot query field") {
				t.Errorf("asking %s for %s: %q", name, field, message)
			}
		}
	}
	everything := fmt.Sprint(listed, spending, page)
	for _, secret := range []string{"Everyday Checking", "Travel Card", "0001", "CORNER GROCER", "private memo", "PAYROLL", "provider-only-text", "1200", "1100"} {
		if strings.Contains(everything, secret) {
			t.Errorf("%q reaches a reader: %s", secret, everything)
		}
	}
}

// A read is of one account or of all of a person's finance, never both and
// never neither.
func TestAllFinanceReadsAskForOneSubject(t *testing.T) {
	world := newFinanceShareWorld(t)
	world.shareAllAsTap(t, world.ben.ID, nil, "transactions")
	for name, variables := range map[string]map[string]any{
		"both":    {"financeAccountId": world.checkingID, "ownerUserId": world.ada.ID},
		"neither": {},
	} {
		for kind, query := range map[string]string{"spending": allSpendingQuery, "transactions": allTransactionsQuery} {
			if message := world.refusal(t, world.ben, query, variables); !strings.Contains(message, "financeAccountId") {
				t.Errorf("%s asking for %s: %q", kind, name, message)
			}
		}
	}
}

// An account linked after the share is covered with no new share, and the
// reader's numbers add up across currencies and accounts.
func TestAnAccountLinkedAfterAnAllFinanceShareIsCovered(t *testing.T) {
	world := newFinanceShareWorld(t)
	world.shareAllAsTap(t, world.ben.ID, nil, "transactions")
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		agent, err := tx.GetAgentByUser(world.ada.ID)
		if err != nil || agent == nil {
			t.Fatalf("GetAgentByUser: %+v %v", agent, err)
		}
		source, err := tx.PutAgentSource(&models.AgentKnowledgeSource{
			AgentID: agent.ID, Kind: models.SourceWeb, Name: "second institution",
			Specification: models.AgentKnowledgeSpecification{Start: "https://example.test/second"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ApplyFinanceSync(agent.ID, source.ID, &finance.SyncResult{
			Accounts: []finance.Account{{ProviderAccountID: "account-savings", AccountName: "Rainy Day Savings", AccountMask: "0777", AccountKind: finance.AccountKindDepository, CurrencyCode: "EUR", CurrentBalance: "900"}},
			Added: []finance.Transaction{{
				ProviderTransactionID: "t-bonus", ProviderAccountID: "account-savings", PostedOn: "2026-09-20", Amount: "120", CurrencyCode: "EUR", Description: "BONUS EXAMPLE CO",
			}},
		}, "2026-09-25"); err != nil {
			t.Fatal(err)
		}
	})
	byOwner := map[string]any{"ownerUserId": world.ada.ID}
	spending := world.run(t, world.ben, allSpendingQuery, byOwner)["ListSharedFinanceSpending"].([]any)
	currencies := map[string]bool{}
	for _, row := range spending {
		currencies[row.(map[string]any)["currencyCode"].(string)] = true
	}
	if len(spending) != 2 || !currencies["USD"] || !currencies["EUR"] {
		t.Errorf("a month in each currency: %v", spending)
	}
	page := world.run(t, world.ben, allTransactionsQuery, byOwner)["ListSharedFinanceTransactions"].(map[string]any)
	if page["totalCount"] != float64(3) {
		t.Errorf("the new account's transaction is in the page: %v", page)
	}
	if strings.Contains(fmt.Sprint(spending, page), "Rainy Day") {
		t.Errorf("the new account is named: %v %v", spending, page)
	}
}

// Nobody outside the circle reads anything, however the owner is named, and a
// person without agent:use is told there is nothing.
func TestAllFinanceReachesOnlyTheCircle(t *testing.T) {
	world := newFinanceShareWorld(t)
	world.shareAllAsTap(t, nil, world.home, "transactions")
	byOwner := map[string]any{"ownerUserId": world.ada.ID}

	if listed := world.run(t, world.ben, sharedAllFinanceQuery, nil)["ListSharedFinance"].([]any); len(listed) != 1 {
		t.Fatalf("a member of the circle: %v", listed)
	}
	for name, user := range map[string]*models.User{"dave, who is in no circle with her": world.dave, "cora, who may not use finance": world.cora} {
		for kind, query := range map[string]string{"spending": allSpendingQuery, "transactions": allTransactionsQuery} {
			if message := world.refusal(t, user, query, byOwner); !strings.Contains(strings.ToLower(message), "not found") {
				t.Errorf("%s reads %s: %q", name, kind, message)
			}
		}
		if user == world.dave {
			if listed := world.run(t, user, sharedAllFinanceQuery, nil)["ListSharedFinance"].([]any); len(listed) != 0 {
				t.Errorf("%s: %v", name, listed)
			}
		} else if message := world.refusal(t, user, sharedAllFinanceQuery, nil); !strings.Contains(strings.ToLower(message), "not found") {
			t.Errorf("%s lists: %q", name, message)
		}
	}

	// Ben leaves the circle and reads nothing; the share is still ada's.
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		if _, err := tx.RemoveCircleMember(world.home, world.ben.ID); err != nil {
			t.Fatal(err)
		}
	})
	if message := world.refusal(t, world.ben, allSpendingQuery, byOwner); !strings.Contains(strings.ToLower(message), "not found") {
		t.Errorf("after leaving the circle: %q", message)
	}
	if shares := world.run(t, world.ada, allFinanceSharesQuery, nil)["ListFinanceShares"].([]any); len(shares) != 1 {
		t.Errorf("the circle's share is still ada's: %v", shares)
	}
}

// Leaving the circle that made a share to a person possible ends it, so the
// two do not meet it again in another circle.
func TestLeavingACircleEndsAnAllFinanceShareToAPerson(t *testing.T) {
	world := newFinanceShareWorld(t)
	world.shareAllAsTap(t, world.ben.ID, nil, "spending")
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		if _, err := tx.RemoveCircleMember(world.home, world.ben.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.AddCircleMember(world.home, world.ben.ID, models.MemberRoleMember, world.ada.ID); err != nil {
			t.Fatal(err)
		}
	})
	if shares := world.run(t, world.ada, allFinanceSharesQuery, nil)["ListFinanceShares"].([]any); len(shares) != 0 {
		t.Errorf("the share to ben outlived the circle: %v", shares)
	}
	if message := world.refusal(t, world.ben, allSpendingQuery, map[string]any{"ownerUserId": world.ada.ID}); !strings.Contains(strings.ToLower(message), "not found") {
		t.Errorf("ben, back in the circle: %q", message)
	}
}

// Commands run on the person's computer, by them or by an agent there, act
// with the person's own login, so a command line token may keep, narrow and
// end a share of all finance but never start one or widen it.
func TestAllFinanceSharesNeedATapToStartOrWiden(t *testing.T) {
	world := newFinanceShareWorld(t)
	isTapRequired := func(failures []string) bool {
		return strings.Contains(strings.Join(failures, "; "), api.TapRequiredPhrase)
	}
	levelOf := func() string {
		shares := world.run(t, world.ada, allFinanceSharesQuery, nil)["ListFinanceShares"].([]any)
		if len(shares) == 0 {
			return ""
		}
		return shares[0].(map[string]any)["financeShareLevel"].(string)
	}

	for _, credentialKind := range []models.CredentialKind{models.CredentialToken, ""} {
		if data, failures := world.shareAll(t, credentialKind, world.ben.ID, nil, "spending"); !isTapRequired(failures) {
			t.Errorf("a new share with credential %q: %v %v", credentialKind, data, failures)
		}
	}
	if levelOf() != "" {
		t.Fatalf("a refused request changed something: %q", levelOf())
	}
	for _, credentialKind := range []models.CredentialKind{models.CredentialSession, models.CredentialApp} {
		if data, failures := world.shareAll(t, credentialKind, world.ben.ID, nil, "spending"); len(failures) > 0 {
			t.Fatalf("a share by credential %q: %v %v", credentialKind, data, failures)
		}
	}
	if levelOf() != "spending" {
		t.Fatalf("shared at %q", levelOf())
	}
	if data, failures := world.shareAll(t, models.CredentialToken, world.ben.ID, nil, "transactions"); !isTapRequired(failures) {
		t.Errorf("widening with a token: %v %v", data, failures)
	}
	if data, failures := world.shareAll(t, models.CredentialToken, nil, world.home, "spending"); !isTapRequired(failures) {
		t.Errorf("a new grantee with a token: %v %v", data, failures)
	}
	if levelOf() != "spending" {
		t.Fatalf("refused requests changed the share: %q", levelOf())
	}
	if data, failures := world.shareAll(t, models.CredentialToken, world.ben.ID, nil, "spending"); len(failures) > 0 {
		t.Errorf("the same again with a token: %v %v", data, failures)
	}
	world.shareAllAsTap(t, world.ben.ID, nil, "transactions")
	if data, failures := world.shareAll(t, models.CredentialToken, world.ben.ID, nil, "spending"); len(failures) > 0 {
		t.Fatalf("narrowing with a token: %v %v", data, failures)
	}
	if levelOf() != "spending" {
		t.Fatalf("narrowed to %q", levelOf())
	}
	shares := world.run(t, world.ada, allFinanceSharesQuery, nil)["ListFinanceShares"].([]any)
	if data, failures := world.callWithCredential(t, world.ada, models.CredentialToken, unshareAllFinanceQuery,
		map[string]any{"financeShareId": shares[0].(map[string]any)["id"]}); len(failures) > 0 {
		t.Fatalf("ending a share with a token: %v %v", data, failures)
	}
	if levelOf() != "" {
		t.Fatalf("still shared at %q", levelOf())
	}
}

// Only the owner shares and ends, with the arguments right; nobody else takes
// a share back, and a person who was not told a share exists is told nothing
// about it.
func TestAllFinanceShareArgumentsAndOwnership(t *testing.T) {
	world := newFinanceShareWorld(t)
	share := world.shareAllAsTap(t, world.ben.ID, nil, "spending")

	for name, variables := range map[string]map[string]any{
		"balance":  {"granteeUserId": world.ben.ID, "financeShareLevel": "balance"},
		"level":    {"granteeUserId": world.ben.ID, "financeShareLevel": "everything"},
		"nobody":   {"financeShareLevel": "spending"},
		"everyone": {"granteeUserId": world.ben.ID, "granteeCircleId": world.home, "financeShareLevel": "spending"},
		"stranger": {"granteeUserId": world.cora.ID, "financeShareLevel": "spending"},
		"herself":  {"granteeUserId": world.ada.ID, "financeShareLevel": "spending"},
	} {
		if _, failures := world.callWith(t, world.ada, shareAllFinanceQuery, variables); len(failures) == 0 {
			t.Errorf("a share with %s was not refused", name)
		}
	}
	if _, failures := world.callWith(t, world.ben, unshareAllFinanceQuery, map[string]any{"financeShareId": share["id"]}); len(failures) == 0 {
		t.Error("ben took back a share of ada's")
	}
	// A person who may not use finance lists nothing of their own.
	if message := world.refusal(t, world.cora, allFinanceSharesQuery, nil); !strings.Contains(strings.ToLower(message), "not found") {
		t.Errorf("a person without agent:use lists shares: %q", message)
	}
	if data := world.run(t, world.ada, unshareAllFinanceQuery, map[string]any{"financeShareId": share["id"]}); data["UnshareFinance"] != true {
		t.Fatalf("unshare: %v", data)
	}
	if _, failures := world.callWith(t, world.ada, unshareAllFinanceQuery, map[string]any{"financeShareId": share["id"]}); len(failures) == 0 {
		t.Error("a share was ended twice")
	}
	if message := world.refusal(t, world.ben, allSpendingQuery, map[string]any{"ownerUserId": world.ada.ID}); !strings.Contains(strings.ToLower(message), "not found") {
		t.Errorf("after the share was taken back: %q", message)
	}
}

// The person's agent has no tool for sharing finance or for reading what
// others share, and the API refuses it the same way if it reaches the
// operations some other way, as a command the agent ran on the computer does.
func TestTheAgentCannotShareOrReadSharedAllFinance(t *testing.T) {
	world := newFinanceShareWorld(t)
	share := world.shareAllAsTap(t, world.ben.ID, nil, "transactions")
	byOwner := map[string]any{"ownerUserId": world.ada.ID}

	for owner, user := range map[string]*models.User{"the owner": world.ada, "the reader": world.ben} {
		operations := &agentOperations{graph: world.graph, user: user, permissions: &models.EffectivePermissions{Everywhere: []models.Permission{models.PermissionAgentUse}}}
		for name, call := range map[string]struct {
			query     string
			variables map[string]any
		}{
			"share":        {shareAllFinanceQuery, map[string]any{"granteeUserId": world.dave.ID, "financeShareLevel": "spending"}},
			"unshare":      {unshareAllFinanceQuery, map[string]any{"financeShareId": share["id"]}},
			"shares":       {allFinanceSharesQuery, nil},
			"shared":       {sharedAllFinanceQuery, nil},
			"spending":     {allSpendingQuery, byOwner},
			"transactions": {allTransactionsQuery, byOwner},
		} {
			var result map[string]any
			err := operations.Execute(t.Context(), call.query, call.variables, &result)
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), "not found") {
				t.Errorf("the agent of %s, %s: %v %v", owner, name, result, err)
			}
		}
	}
	marked := map[string]string{api.AgentCommandHeader: "1"}
	for name, call := range map[string]struct {
		user      *models.User
		query     string
		variables map[string]any
	}{
		"shared":       {world.ben, sharedAllFinanceQuery, nil},
		"spending":     {world.ben, allSpendingQuery, byOwner},
		"transactions": {world.ben, allTransactionsQuery, byOwner},
		"shares":       {world.ada, allFinanceSharesQuery, nil},
	} {
		_, failures := world.callWithHeaders(t, call.user, models.CredentialToken, marked, call.query, call.variables)
		if !strings.Contains(strings.ToLower(strings.Join(failures, "; ")), "not found") {
			t.Errorf("%s, marked as the agent's: %v", name, failures)
		}
		if _, failures := world.callWithHeaders(t, call.user, models.CredentialToken, nil, call.query, call.variables); len(failures) > 0 {
			t.Errorf("%s, not marked: %v", name, failures)
		}
	}
	// The person's own reads still work as the person.
	if listed := world.run(t, world.ben, sharedAllFinanceQuery, nil)["ListSharedFinance"].([]any); len(listed) != 1 {
		t.Errorf("ben reads what ada shares with him: %v", listed)
	}
}

// Whoever joins a circle reads what was shared with it, so bringing somebody
// into a circle all of somebody's finance is shared with is sharing it with
// them, and a command line token cannot do it.
func TestJoiningACircleAllFinanceIsSharedWithNeedsATap(t *testing.T) {
	world := newFinanceShareWorld(t)
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		dbtest.GrantPermission(t, tx, world.cora.ID, models.PermissionAgentUse)
		role, err := tx.CreateRole(&models.Role{Name: "Account manager", Permissions: []models.Permission{models.PermissionUserManage}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.CreateGroup(&models.Group{Name: "Account managers", RoleIDs: []string{role.ID}, UserIDs: []string{world.ada.ID}}); err != nil {
			t.Fatal(err)
		}
	})
	add := func(credentialKind models.CredentialKind) []string {
		_, failures := world.callWithCredential(t, world.ada, credentialKind,
			fmt.Sprintf(`mutation { AddCircleMember(circleId: %q, username: %q, memberRole: "member") { id } }`, world.home, world.cora.Username), nil)
		return failures
	}
	world.shareAllAsTap(t, nil, world.home, "spending")
	for _, credentialKind := range []models.CredentialKind{models.CredentialToken, ""} {
		if failures := add(credentialKind); !strings.Contains(strings.Join(failures, "; "), api.TapRequiredPhrase) {
			t.Errorf("putting a person in with credential %q: %v", credentialKind, failures)
		}
	}
	world.refusal(t, world.cora, allSpendingQuery, map[string]any{"ownerUserId": world.ada.ID})
	if failures := add(models.CredentialSession); len(failures) > 0 {
		t.Fatalf("putting a person in by a tap: %v", failures)
	}
	if listed := world.run(t, world.cora, sharedAllFinanceQuery, nil)["ListSharedFinance"].([]any); len(listed) != 1 {
		t.Errorf("cora, who a tap brought in, reads what the circle was shared: %v", listed)
	}
}

// An owner who has turned the agent off can still see what they share and end
// it, though it reaches nobody meanwhile and they cannot start a new share.
func TestAnOwnerWithTheAgentOffCanListAndEndAllFinanceShares(t *testing.T) {
	world := newFinanceShareWorld(t)
	share := world.shareAllAsTap(t, world.ben.ID, nil, "spending")
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		agent, err := tx.GetAgentByUser(world.ada.ID)
		if err != nil || agent == nil {
			t.Fatalf("GetAgentByUser: %+v %v", agent, err)
		}
		if _, err := tx.UpdateAgent(agent.ID, func(found *models.Agent) error { found.Enabled = false; return nil }); err != nil {
			t.Fatal(err)
		}
	})
	if listed := world.run(t, world.ben, sharedAllFinanceQuery, nil)["ListSharedFinance"].([]any); len(listed) != 0 {
		t.Errorf("finance of an agent that is off reaches nobody: %v", listed)
	}
	if shares := world.run(t, world.ada, allFinanceSharesQuery, nil)["ListFinanceShares"].([]any); len(shares) != 1 {
		t.Fatalf("the owner lists what they share: %v", shares)
	}
	if _, failures := world.shareAll(t, models.CredentialSession, world.dave.ID, nil, "spending"); len(failures) == 0 {
		t.Error("a new share was started with the agent off")
	}
	if data := world.run(t, world.ada, unshareAllFinanceQuery, map[string]any{"financeShareId": share["id"]}); data["UnshareFinance"] != true {
		t.Fatalf("the owner ends the share: %v", data)
	}
}

// The person card says a person shares all their finance, and at which level.
func TestReadPersonCardSaysAPersonSharesAllTheirFinance(test *testing.T) {
	world := newPersonCardWorld(test)
	seedFinance(test, world.database, world.ada)
	fields := `finance { sectionState allFinanceShareLevel financeAccounts { financeAccountId } }`
	read := func(viewer *models.User) map[string]any {
		data := world.run(test, viewer, fmt.Sprintf(`query { ReadPersonCard(userId: %q) { %s } }`, world.ada.ID, fields), nil)
		return sectionOf(data["ReadPersonCard"].(map[string]any), "finance")
	}
	if section := read(world.ben); section["sectionState"] != "notShared" || section["allFinanceShareLevel"] != "" {
		test.Fatalf("nothing is shared: %v", section)
	}
	dbtest.RunTransactionOn(test, world.database, func(tx db.Transaction) {
		if _, err := tx.ShareAllFinance(world.ada.ID, &models.AllFinanceShare{GranteeUserID: world.ben.ID, FinanceShareLevel: models.FinanceShareLevelSpending}); err != nil {
			test.Fatal(err)
		}
	})
	section := read(world.ben)
	if section["sectionState"] != "shown" || section["allFinanceShareLevel"] != "spending" || len(section["financeAccounts"].([]any)) != 0 {
		test.Errorf("a share of all finance, with no account named: %v", section)
	}
	// The agent of the person is shown none of it.
	operations := &agentOperations{graph: world.graph, user: world.ben, permissions: &models.EffectivePermissions{Everywhere: []models.Permission{
		models.PermissionAgentUse, models.PermissionCircleUse,
	}}}
	var found struct {
		ReadPersonCard map[string]any `json:"ReadPersonCard"`
	}
	if err := operations.Execute(test.Context(), fmt.Sprintf(`query { ReadPersonCard(userId: %q) { %s } }`, world.ada.ID, fields), nil, &found); err != nil {
		test.Fatal(err)
	}
	if agentSection := sectionOf(found.ReadPersonCard, "finance"); agentSection["sectionState"] != "unavailable" || agentSection["allFinanceShareLevel"] != "" {
		test.Errorf("the agent is shown the finance shared with the person: %v", agentSection)
	}
}

// A share of all finance is a row of the Sharing list on both sides with no
// subject and the scope allAccounts, can be declined when it was made by name,
// and its note says all of the finance, at a level that has no balance.
func TestListSharingShowsAShareOfAllFinance(test *testing.T) {
	world := newSharingWorld(test)
	seedFinance(test, world.database, world.ada)
	world.run(test, world.ada, shareAllFinanceQuery, map[string]any{"granteeUserId": world.ben.ID, "granteeCircleId": nil, "financeShareLevel": "spending"})
	world.run(test, world.ada, shareAllFinanceQuery, map[string]any{"granteeUserId": nil, "granteeCircleId": world.home, "financeShareLevel": "transactions"})

	made, _ := world.sharing(test, world.ada)
	if len(made) != 2 || kindsOf(made)[0] != "finance" {
		test.Fatalf("ada shares %v", kindsOf(made))
	}
	for _, row := range made {
		scopes, _ := row["shareScopes"].([]any)
		if row["subjectId"] != "" || row["subjectName"] != "" || len(scopes) != 1 || scopes[0] != "allAccounts" {
			test.Errorf("a share of all finance has no subject and says what it covers: %v", row)
		}
	}
	_, received := world.sharing(test, world.ben)
	if len(received) != 2 {
		test.Fatalf("ben receives %v", kindsOf(received))
	}
	var byName map[string]any
	for _, row := range received {
		if row["isDirect"] == true {
			byName = row
		}
	}
	if byName == nil || byName["shareLevel"] != "spending" || byName["ownerUsername"] != world.ada.Username || byName["canDecline"] != true {
		test.Fatalf("the share made to ben by name: %v", received)
	}
	notes := world.notesOf(test, world.ben, models.NoteShareReceived)
	joined := strings.Join(notes, "\n")
	if !strings.Contains(joined, "shared all their finance with you (spending only)") || strings.Contains(joined, "Everyday Checking") {
		test.Errorf("the note says all finance and names no account: %q", notes)
	}

	world.call(test, world.ben, fmt.Sprintf(`mutation { DeclineShare(shareKind: "finance", shareId: %q) }`, byName["shareId"]))
	if _, received = world.sharing(test, world.ben); len(received) != 1 || received[0]["isDirect"] != false {
		test.Errorf("after declining the share by name, the circle's is left: %v", received)
	}
}
