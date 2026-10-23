package apigraph

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

// financeShareWorld is healthWorld with finance: ada has her agent on and a
// checking account with two transactions and a credit card; ada, ben and dave
// may use the agent, and cora may not.
type financeShareWorld struct {
	*healthWorld
	checkingID string
	cardID     string
}

func newFinanceShareWorld(t *testing.T) *financeShareWorld {
	t.Helper()
	world := &financeShareWorld{healthWorld: newHealthWorld(t)}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		for _, person := range []*models.User{world.ada, world.ben, world.dave} {
			role, err := tx.CreateRole(&models.Role{Name: person.Username + " agent role", Permissions: []models.Permission{models.PermissionAgentUse}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tx.CreateGroup(&models.Group{Name: person.Username + " agent group", UserIDs: []string{person.ID}, RoleIDs: []string{role.ID}}); err != nil {
				t.Fatal(err)
			}
		}
	})
	world.checkingID, world.cardID = seedFinance(t, world.database, world.ada)
	return world
}

// seedFinance gives a person an agent that is on, a checking account with two
// transactions and a credit card, and says the two accounts' identifiers.
func seedFinance(t *testing.T, database db.Database, owner *models.User) (checkingID, cardID string) {
	t.Helper()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agent, err := tx.EnsureAgent(owner.ID)
		if err != nil {
			t.Fatalf("EnsureAgent: %s", err)
		}
		if _, err := tx.UpdateAgent(agent.ID, func(found *models.Agent) error { found.Enabled = true; return nil }); err != nil {
			t.Fatal(err)
		}
		source, err := tx.PutAgentSource(&models.AgentKnowledgeSource{
			AgentID: agent.ID, Kind: models.SourceWeb, Name: "institution",
			Specification: models.AgentKnowledgeSpecification{Start: "https://example.test/"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ApplyFinanceSync(agent.ID, source.ID, &finance.SyncResult{
			Accounts: []finance.Account{
				{
					ProviderAccountID: "account-checking", AccountName: "Everyday Checking", AccountMask: "0001", AccountKind: finance.AccountKindDepository,
					CurrencyCode: "USD", CurrentBalance: "1200.50", AvailableBalance: "1100.50", ProviderMetadata: []byte(`{"secretField":"provider-only-text"}`),
				},
				{
					ProviderAccountID: "account-card", AccountName: "Travel Card", AccountKind: finance.AccountKindCredit, CurrencyCode: "USD",
					CurrentBalance: "-340.25", AvailableBalance: "659.75",
				},
			},
			Added: []finance.Transaction{
				{
					ProviderTransactionID: "t1", ProviderAccountID: "account-checking", PostedOn: "2026-09-10", Amount: "-42.17", CurrencyCode: "USD",
					Description: "CORNER GROCER 0412 private memo", MerchantName: "Corner Grocer",
				},
				{
					ProviderTransactionID: "t2", ProviderAccountID: "account-checking", PostedOn: "2026-09-01", Amount: "2500", CurrencyCode: "USD",
					Description: "PAYROLL EXAMPLE CO",
				},
			},
		}, "2026-09-15"); err != nil {
			t.Fatal(err)
		}
		accounts, err := tx.ListFinanceAccounts(agent.ID, "")
		if err != nil || len(accounts) != 2 {
			t.Fatalf("ListFinanceAccounts: %+v %v", accounts, err)
		}
		for _, account := range accounts {
			if account.AccountName == "Everyday Checking" {
				checkingID = account.ID
			} else {
				cardID = account.ID
			}
		}
	})
	return checkingID, cardID
}

const (
	shareFinanceQuery = `mutation ($financeAccountId: String!, $granteeUserId: String, $granteeCircleId: String, $financeShareLevel: String!) {
  ShareFinanceAccount(financeAccountId: $financeAccountId, granteeUserId: $granteeUserId, granteeCircleId: $granteeCircleId, financeShareLevel: $financeShareLevel) {
    id financeAccountId accountName accountMask granteeUserId granteeUsername granteeCircleId granteeCircleName financeShareLevel createdAt
  }
}`
	unshareFinanceQuery = `mutation ($shareId: String!) { UnshareFinanceAccount(shareId: $shareId) }`
	financeSharesQuery  = `query ($financeAccountId: String) { ListFinanceAccountShares(financeAccountId: $financeAccountId) { id financeAccountId accountName granteeUsername granteeCircleName financeShareLevel } }`
	sharedAccountsQuery = `query ($ownerUserId: String) { ListSharedFinanceAccounts(ownerUserId: $ownerUserId) {
  financeAccountId ownerUserId ownerUsername ownerName financeShareLevel accountName accountMask accountKind currencyCode currentBalance balanceAt
  creditUsage { owedAmount creditLimitAmount creditLimitSource usageShare }
} }`
	sharedSpendingQuery = `query ($financeAccountId: String!, $from: String, $to: String) {
  ListSharedFinanceSpending(financeAccountId: $financeAccountId, from: $from, to: $to) { spendingMonth spendingCategoryName currencyCode moneyOut moneyIn financeTransactionCount }
}`
	sharedTransactionsQuery = `query ($financeAccountId: String!, $limit: Int, $after: String, $offset: Int) {
  ListSharedFinanceTransactions(financeAccountId: $financeAccountId, limit: $limit, after: $after, offset: $offset) { nextCursor totalCount financeTransactions { id postedOn amount currencyCode merchantName spendingCategoryName } }
}`
)

func (self *financeShareWorld) share(t *testing.T, credentialKind models.CredentialKind, accountId string, granteeUserId, granteeCircleId any, level string) (map[string]any, []string) {
	t.Helper()
	return self.callWithCredential(t, self.ada, credentialKind, shareFinanceQuery, map[string]any{
		"financeAccountId": accountId, "granteeUserId": granteeUserId, "granteeCircleId": granteeCircleId, "financeShareLevel": level,
	})
}

func (self *financeShareWorld) shareAsTap(t *testing.T, accountId string, granteeUserId, granteeCircleId any, level string) map[string]any {
	t.Helper()
	data, failures := self.share(t, models.CredentialSession, accountId, granteeUserId, granteeCircleId, level)
	if len(failures) > 0 {
		t.Fatalf("sharing %s at %s: %v", accountId, level, failures)
	}
	return data["ShareFinanceAccount"].(map[string]any)
}

// Each level shows what it adds and nothing of what a higher one adds, over
// the API: the balance with a card's usage, then the months, then the
// transactions, and never a field the shared view does not have.
func TestFinanceShareLevelsOverTheAPI(t *testing.T) {
	world := newFinanceShareWorld(t)

	// Before any share, ben sees nothing and the ids are not found.
	if listed := world.run(t, world.ben, sharedAccountsQuery, nil)["ListSharedFinanceAccounts"].([]any); len(listed) != 0 {
		t.Fatalf("before any share: %v", listed)
	}
	for name, query := range map[string]string{"spending": sharedSpendingQuery, "transactions": sharedTransactionsQuery} {
		if message := world.refusal(t, world.ben, query, map[string]any{"financeAccountId": world.checkingID}); !strings.Contains(strings.ToLower(message), "not found") {
			t.Errorf("%s before any share: %q", name, message)
		}
	}

	shared := world.shareAsTap(t, world.checkingID, world.ben.ID, nil, "balance")
	if shared["accountName"] != "Everyday Checking" || shared["accountMask"] != "0001" || shared["granteeUsername"] != world.ben.Username || shared["financeShareLevel"] != "balance" {
		t.Fatalf("the share says which account and who: %v", shared)
	}
	world.shareAsTap(t, world.cardID, nil, world.home, "balance")

	listed := world.run(t, world.ben, sharedAccountsQuery, nil)["ListSharedFinanceAccounts"].([]any)
	if len(listed) != 2 {
		t.Fatalf("both accounts reach ben: %v", listed)
	}
	checking, card := listed[0].(map[string]any), listed[1].(map[string]any)
	if checking["accountName"] != "Everyday Checking" || checking["accountKind"] != "depository" || checking["currentBalance"] != "1200.5000" ||
		checking["ownerUsername"] != world.ada.Username || checking["ownerUserId"] != world.ada.ID || checking["financeShareLevel"] != "balance" || checking["creditUsage"] != nil {
		t.Errorf("the checking account at the balance level: %v", checking)
	}
	usage, _ := card["creditUsage"].(map[string]any)
	if card["accountKind"] != "credit" || usage == nil || usage["owedAmount"] != "340.2500" || usage["creditLimitAmount"] != "1000.0000" || usage["creditLimitSource"] != "derived" || usage["usageShare"] != 0.3403 {
		t.Errorf("a card shows what it owes against its limit: %v", card)
	}
	if only := world.run(t, world.ben, sharedAccountsQuery, map[string]any{"ownerUserId": world.dave.ID})["ListSharedFinanceAccounts"].([]any); len(only) != 0 {
		t.Errorf("of another person: %v", only)
	}

	// The shared view has no field for the rest of the account.
	for _, field := range []string{"availableBalance", "providerMetadata", "sourceId", "institutionName", "isSignInRequired", "creditLimitAmount"} {
		query := fmt.Sprintf(`query { ListSharedFinanceAccounts { %s } }`, field)
		if message := world.refusal(t, world.ben, query, nil); !strings.Contains(message, "Cannot query field") {
			t.Errorf("asking for %s: %q", field, message)
		}
	}

	// Spending and transactions are not at the balance level.
	for name, query := range map[string]string{"spending": sharedSpendingQuery, "transactions": sharedTransactionsQuery} {
		if message := world.refusal(t, world.ben, query, map[string]any{"financeAccountId": world.checkingID}); !strings.Contains(strings.ToLower(message), "not found") {
			t.Errorf("%s at the balance level: %q", name, message)
		}
	}

	world.shareAsTap(t, world.checkingID, world.ben.ID, nil, "spending")
	spending := world.run(t, world.ben, sharedSpendingQuery, map[string]any{"financeAccountId": world.checkingID})["ListSharedFinanceSpending"].([]any)
	if len(spending) != 1 {
		t.Fatalf("the months: %v", spending)
	}
	if row := spending[0].(map[string]any); row["spendingMonth"] != "2026-09" || row["currencyCode"] != "USD" || row["moneyOut"] != "42.1700" ||
		row["moneyIn"] != "2500.0000" || row["spendingCategoryName"] != "" || row["financeTransactionCount"] != float64(2) {
		t.Errorf("a month of uncategorized transactions: %v", row)
	}
	if message := world.refusal(t, world.ben, sharedTransactionsQuery, map[string]any{"financeAccountId": world.checkingID}); !strings.Contains(strings.ToLower(message), "not found") {
		t.Errorf("transactions at the spending level: %q", message)
	}
	// A card, shared at balance, is not read for spending.
	if message := world.refusal(t, world.ben, sharedSpendingQuery, map[string]any{"financeAccountId": world.cardID}); !strings.Contains(strings.ToLower(message), "not found") {
		t.Errorf("spending of the card at the balance level: %q", message)
	}

	world.shareAsTap(t, world.checkingID, world.ben.ID, nil, "transactions")
	page := world.run(t, world.ben, sharedTransactionsQuery, map[string]any{"financeAccountId": world.checkingID})["ListSharedFinanceTransactions"].(map[string]any)
	transactions := page["financeTransactions"].([]any)
	if len(transactions) != 2 || page["totalCount"] != float64(2) {
		t.Fatalf("the transactions: %v", page)
	}
	if second := world.run(t, world.ben, sharedTransactionsQuery, map[string]any{"financeAccountId": world.checkingID, "limit": 1, "offset": 1})["ListSharedFinanceTransactions"].(map[string]any); len(second["financeTransactions"].([]any)) != 1 || second["nextCursor"] != "" {
		t.Errorf("the second page by its offset: %v", second)
	}
	newest := transactions[0].(map[string]any)
	if newest["postedOn"] != "2026-09-10" || newest["amount"] != "-42.1700" || newest["merchantName"] != "Corner Grocer" || newest["currencyCode"] != "USD" {
		t.Errorf("day, amount and merchant: %v", newest)
	}
	for _, field := range []string{"description", "annotation", "providerMetadata", "receiptCount", "providerCategoryPrimary", "isPending"} {
		query := fmt.Sprintf(`query ($financeAccountId: String!) { ListSharedFinanceTransactions(financeAccountId: $financeAccountId) { financeTransactions { %s } } }`, field)
		if message := world.refusal(t, world.ben, query, map[string]any{"financeAccountId": world.checkingID}); !strings.Contains(message, "Cannot query field") {
			t.Errorf("asking a transaction for %s: %q", field, message)
		}
	}
	// What is private to the account is nowhere in what ben can read.
	everything := fmt.Sprint(listed, spending, page)
	for _, secret := range []string{"CORNER GROCER", "private memo", "PAYROLL", "provider-only-text", "1100.50"} {
		if strings.Contains(everything, secret) {
			t.Errorf("%q reaches a reader: %s", secret, everything)
		}
	}

	// Cora is in no circle with ada and reads nothing, however the account is
	// named.
	for name, query := range map[string]string{"spending": sharedSpendingQuery, "transactions": sharedTransactionsQuery} {
		world.refusal(t, world.cora, query, map[string]any{"financeAccountId": world.checkingID})
		_ = name
	}
	if listed := world.run(t, world.dave, sharedAccountsQuery, nil)["ListSharedFinanceAccounts"].([]any); len(listed) != 0 {
		t.Errorf("dave: %v", listed)
	}
}

// Without the permission finance needs, nothing is available and each answer
// is not found, and nobody but the account's owner shares or unshares it.
func TestFinanceShareNeedsThePermissionAndTheOwner(t *testing.T) {
	world := newFinanceShareWorld(t)
	share := world.shareAsTap(t, world.checkingID, world.ben.ID, nil, "transactions")
	for name, call := range map[string]struct {
		query     string
		variables map[string]any
	}{
		"shares":       {financeSharesQuery, nil},
		"accounts":     {sharedAccountsQuery, nil},
		"spending":     {sharedSpendingQuery, map[string]any{"financeAccountId": world.checkingID}},
		"transactions": {sharedTransactionsQuery, map[string]any{"financeAccountId": world.checkingID}},
		"share":        {shareFinanceQuery, map[string]any{"financeAccountId": world.checkingID, "granteeUserId": world.ada.ID, "financeShareLevel": "balance"}},
		"unshare":      {unshareFinanceQuery, map[string]any{"shareId": share["id"]}},
	} {
		if message := world.refusal(t, world.cora, call.query, call.variables); !strings.Contains(strings.ToLower(message), "not found") {
			t.Errorf("%s: a person without agent:use is told %q", name, message)
		}
	}

	// Ben has the permission and the share, and no account of his own to
	// share. He cannot share ada's, nor take her share back.
	_, failures := world.callWith(t, world.ben, shareFinanceQuery, map[string]any{
		"financeAccountId": world.checkingID, "granteeUserId": world.dave.ID, "financeShareLevel": "balance"})
	if len(failures) == 0 {
		t.Error("ben shared an account of ada's")
	}
	_, failures = world.callWith(t, world.ben, unshareFinanceQuery, map[string]any{"shareId": share["id"]})
	if len(failures) == 0 {
		t.Error("ben took back a share of ada's")
	}
	if shares := world.run(t, world.ada, financeSharesQuery, nil)["ListFinanceAccountShares"].([]any); len(shares) != 1 {
		t.Fatalf("ada's share is still there: %v", shares)
	}
	if one := world.run(t, world.ada, financeSharesQuery, map[string]any{"financeAccountId": world.cardID})["ListFinanceAccountShares"].([]any); len(one) != 0 {
		t.Errorf("of one account: %v", one)
	}

	// Taking it back is the owner's, and the person reads nothing after.
	if data := world.run(t, world.ada, unshareFinanceQuery, map[string]any{"shareId": share["id"]}); data["UnshareFinanceAccount"] != true {
		t.Fatalf("unshare: %v", data)
	}
	if listed := world.run(t, world.ben, sharedAccountsQuery, nil)["ListSharedFinanceAccounts"].([]any); len(listed) != 0 {
		t.Errorf("after the share was taken back: %v", listed)
	}
	// An argument that is wrong is refused.
	for name, variables := range map[string]map[string]any{
		"level":    {"financeAccountId": world.checkingID, "granteeUserId": world.ben.ID, "financeShareLevel": "everything"},
		"nobody":   {"financeAccountId": world.checkingID, "financeShareLevel": "balance"},
		"everyone": {"financeAccountId": world.checkingID, "granteeUserId": world.ben.ID, "granteeCircleId": world.home, "financeShareLevel": "balance"},
		"stranger": {"financeAccountId": world.checkingID, "granteeUserId": world.cora.ID, "financeShareLevel": "balance"},
	} {
		if _, failures := world.callWith(t, world.ada, shareFinanceQuery, variables); len(failures) == 0 {
			t.Errorf("a share with %s was not refused", name)
		}
	}
}

// Commands run on the person's computer, by them or by an agent there, act
// with the person's own login, so a command line token may keep, narrow and
// end a share but never start one or widen it.
func TestFinanceSharesNeedATapToStartOrWiden(t *testing.T) {
	world := newFinanceShareWorld(t)
	isTapRequired := func(failures []string) bool {
		return strings.Contains(strings.Join(failures, "; "), api.TapRequiredPhrase)
	}
	levelOf := func() string {
		shares := world.run(t, world.ada, financeSharesQuery, nil)["ListFinanceAccountShares"].([]any)
		if len(shares) == 0 {
			return ""
		}
		return shares[0].(map[string]any)["financeShareLevel"].(string)
	}

	for _, credentialKind := range []models.CredentialKind{models.CredentialToken, ""} {
		if data, failures := world.share(t, credentialKind, world.checkingID, world.ben.ID, nil, "balance"); !isTapRequired(failures) {
			t.Errorf("a new share with credential %q: %v %v", credentialKind, data, failures)
		}
	}
	if levelOf() != "" {
		t.Fatalf("a refused request changed something: %q", levelOf())
	}

	for _, credentialKind := range []models.CredentialKind{models.CredentialSession, models.CredentialApp} {
		world.run(t, world.ada, unshareFinanceQuery, map[string]any{"shareId": world.shareAsTap(t, world.checkingID, world.ben.ID, nil, "spending")["id"]})
		if data, failures := world.share(t, credentialKind, world.checkingID, world.ben.ID, nil, "spending"); len(failures) > 0 {
			t.Fatalf("a share by credential %q: %v %v", credentialKind, data, failures)
		}
	}
	if levelOf() != "spending" {
		t.Fatalf("shared at %q", levelOf())
	}
	// Wider, never; the same or narrower, yes; and a share to someone new is
	// a start.
	if data, failures := world.share(t, models.CredentialToken, world.checkingID, world.ben.ID, nil, "transactions"); !isTapRequired(failures) {
		t.Errorf("widening with a token: %v %v", data, failures)
	}
	if data, failures := world.share(t, models.CredentialToken, world.checkingID, world.dave.ID, nil, "balance"); !isTapRequired(failures) {
		t.Errorf("a new person with a token: %v %v", data, failures)
	}
	if data, failures := world.share(t, models.CredentialToken, world.cardID, world.ben.ID, nil, "balance"); !isTapRequired(failures) {
		t.Errorf("another account with a token: %v %v", data, failures)
	}
	if levelOf() != "spending" {
		t.Fatalf("refused requests changed the share: %q", levelOf())
	}
	if data, failures := world.share(t, models.CredentialToken, world.checkingID, world.ben.ID, nil, "spending"); len(failures) > 0 {
		t.Errorf("the same again with a token: %v %v", data, failures)
	}
	if data, failures := world.share(t, models.CredentialToken, world.checkingID, world.ben.ID, nil, "balance"); len(failures) > 0 {
		t.Fatalf("narrowing with a token: %v %v", data, failures)
	}
	if levelOf() != "balance" {
		t.Fatalf("narrowed to %q", levelOf())
	}
	shares := world.run(t, world.ada, financeSharesQuery, nil)["ListFinanceAccountShares"].([]any)
	if data, failures := world.callWithCredential(t, world.ada, models.CredentialToken, unshareFinanceQuery,
		map[string]any{"shareId": shares[0].(map[string]any)["id"]}); len(failures) > 0 {
		t.Fatalf("ending a share with a token: %v %v", data, failures)
	}
	if levelOf() != "" {
		t.Fatalf("still shared at %q", levelOf())
	}
}

// The person's agent has no tool for sharing finance or for reading what
// others share, and the API refuses it the same way if it reaches the
// operations some other way.
func TestTheAgentCannotShareOrReadSharedFinance(t *testing.T) {
	world := newFinanceShareWorld(t)
	world.shareAsTap(t, world.checkingID, world.ben.ID, nil, "transactions")

	for owner, user := range map[string]*models.User{"the owner": world.ada, "the reader": world.ben} {
		operations := &agentOperations{graph: world.graph, user: user, permissions: &models.EffectivePermissions{Everywhere: []models.Permission{models.PermissionAgentUse}}}
		for name, call := range map[string]struct {
			query     string
			variables map[string]any
		}{
			"share":        {shareFinanceQuery, map[string]any{"financeAccountId": world.checkingID, "granteeUserId": world.dave.ID, "financeShareLevel": "balance"}},
			"unshare":      {unshareFinanceQuery, map[string]any{"shareId": "anything"}},
			"shares":       {financeSharesQuery, nil},
			"accounts":     {sharedAccountsQuery, nil},
			"spending":     {sharedSpendingQuery, map[string]any{"financeAccountId": world.checkingID}},
			"transactions": {sharedTransactionsQuery, map[string]any{"financeAccountId": world.checkingID}},
		} {
			var result map[string]any
			err := operations.Execute(t.Context(), call.query, call.variables, &result)
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), "not found") {
				t.Errorf("the agent of %s, %s: %v %v", owner, name, result, err)
			}
		}
	}
	// The person's own reads still work as the person.
	if listed := world.run(t, world.ben, sharedAccountsQuery, nil)["ListSharedFinanceAccounts"].([]any); len(listed) != 1 {
		t.Errorf("ben reads what ada shares with him: %v", listed)
	}
}

// Whoever joins a circle reads what was shared with it, so bringing somebody
// into a circle a finance account is shared with is sharing that account with
// them, and a command line token cannot do it.
func TestJoiningACircleAFinanceAccountIsSharedWithNeedsATap(t *testing.T) {
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
	invite := func(credentialKind models.CredentialKind) []string {
		_, failures := world.callWithCredential(t, world.ada, credentialKind,
			fmt.Sprintf(`mutation { InviteToCircle(circleId: %q, username: %q, memberRole: "member") }`, world.home, world.cora.Username), nil)
		return failures
	}
	add := func(credentialKind models.CredentialKind) []string {
		_, failures := world.callWithCredential(t, world.ada, credentialKind,
			fmt.Sprintf(`mutation { AddCircleMember(circleId: %q, username: %q, memberRole: "member") { id } }`, world.home, world.cora.Username), nil)
		return failures
	}
	isTapRequired := func(failures []string) bool {
		return strings.Contains(strings.Join(failures, "; "), api.TapRequiredPhrase)
	}

	// Nothing is shared with the circle yet, so a token may bring cora in.
	if failures := invite(models.CredentialToken); len(failures) > 0 {
		t.Fatalf("an invitation by a token to a circle with nothing shared: %v", failures)
	}
	world.shareAsTap(t, world.checkingID, nil, world.home, "balance")
	for _, credentialKind := range []models.CredentialKind{models.CredentialToken, ""} {
		if failures := invite(credentialKind); !isTapRequired(failures) {
			t.Errorf("an invitation with credential %q: %v", credentialKind, failures)
		}
		if failures := add(credentialKind); !isTapRequired(failures) {
			t.Errorf("putting an account in with credential %q: %v", credentialKind, failures)
		}
	}
	world.refusal(t, world.cora, sharedSpendingQuery, map[string]any{"financeAccountId": world.checkingID})

	if failures := add(models.CredentialSession); len(failures) > 0 {
		t.Fatalf("putting an account in by a tap: %v", failures)
	}
	if listed := world.run(t, world.cora, sharedAccountsQuery, nil)["ListSharedFinanceAccounts"].([]any); len(listed) != 1 {
		t.Errorf("cora, who a tap brought in, reads what the circle was shared: %v", listed)
	}
}

// An owner who has turned the agent off can still see what they share and end
// it, though what they share reaches nobody meanwhile and they cannot start a
// new share.
func TestAnOwnerWithTheAgentOffCanListAndEndFinanceShares(t *testing.T) {
	world := newFinanceShareWorld(t)
	share := world.shareAsTap(t, world.checkingID, world.ben.ID, nil, "spending")
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		agent, err := tx.GetAgentByUser(world.ada.ID)
		if err != nil || agent == nil {
			t.Fatalf("GetAgentByUser: %+v %v", agent, err)
		}
		if _, err := tx.UpdateAgent(agent.ID, func(found *models.Agent) error { found.Enabled = false; return nil }); err != nil {
			t.Fatal(err)
		}
	})

	if listed := world.run(t, world.ben, sharedAccountsQuery, nil)["ListSharedFinanceAccounts"].([]any); len(listed) != 0 {
		t.Errorf("an account of an agent that is off reaches nobody: %v", listed)
	}
	if shares := world.run(t, world.ada, financeSharesQuery, nil)["ListFinanceAccountShares"].([]any); len(shares) != 1 {
		t.Fatalf("the owner lists what they share: %v", shares)
	}
	if _, failures := world.share(t, models.CredentialSession, world.checkingID, world.dave.ID, nil, "balance"); len(failures) == 0 {
		t.Error("a new share was started with the agent off")
	}
	if data := world.run(t, world.ada, unshareFinanceQuery, map[string]any{"shareId": share["id"]}); data["UnshareFinanceAccount"] != true {
		t.Fatalf("the owner ends the share: %v", data)
	}
	if shares := world.run(t, world.ada, financeSharesQuery, nil)["ListFinanceAccountShares"].([]any); len(shares) != 0 {
		t.Errorf("after ending it: %v", shares)
	}
}

// A command the agent ran on the person's computer reaches the API with the
// person's own token and says so in a header. It is then the agent's request,
// refused what the agent's tools are refused, and the same command without
// the mark is the person's.
func TestAMarkedCommandLineRequestIsTheAgents(t *testing.T) {
	world := newFinanceShareWorld(t)
	world.shareAsTap(t, world.checkingID, world.ben.ID, nil, "transactions")
	marked := map[string]string{api.AgentCommandHeader: "1"}

	for name, call := range map[string]struct {
		user      *models.User
		query     string
		variables map[string]any
	}{
		"accounts":     {world.ben, sharedAccountsQuery, nil},
		"spending":     {world.ben, sharedSpendingQuery, map[string]any{"financeAccountId": world.checkingID}},
		"transactions": {world.ben, sharedTransactionsQuery, map[string]any{"financeAccountId": world.checkingID}},
		"shares":       {world.ada, financeSharesQuery, nil},
	} {
		_, failures := world.callWithHeaders(t, call.user, models.CredentialToken, marked, call.query, call.variables)
		if !strings.Contains(strings.ToLower(strings.Join(failures, "; ")), "not found") {
			t.Errorf("%s, marked as the agent's: %v", name, failures)
		}
		if _, failures := world.callWithHeaders(t, call.user, models.CredentialToken, nil, call.query, call.variables); len(failures) > 0 {
			t.Errorf("%s, not marked: %v", name, failures)
		}
	}
	// The mark names the agent in the audit trail, and only for a person.
	request := httptest.NewRequest(http.MethodPost, api.PathGraphQL, nil)
	request.Header.Set(api.AgentCommandHeader, "1")
	if kind := world.graph.auditPrincipal(request, world.ada, nil).ActorKind; kind != models.AuditActorAgent {
		t.Errorf("a marked request is audited as %q", kind)
	}
	if kind := world.graph.auditPrincipal(request, nil, nil).ActorKind; kind != models.AuditActorUser {
		t.Errorf("a marked request with no person is audited as %q", kind)
	}
	if kind := world.graph.auditPrincipal(httptest.NewRequest(http.MethodPost, api.PathGraphQL, nil), world.ada, nil).ActorKind; kind != models.AuditActorUser {
		t.Errorf("an unmarked request is audited as %q", kind)
	}
}
