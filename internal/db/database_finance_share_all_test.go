package db_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

func shareAllFinance(t *testing.T, tx db.Transaction, world *financeShareWorld, granteeUserId, granteeCircleId string, level models.FinanceShareLevel) *models.AllFinanceShare {
	t.Helper()
	share, err := tx.ShareAllFinance(world.ada.ID, &models.AllFinanceShare{
		GranteeUserID: granteeUserId, GranteeCircleID: granteeCircleId, FinanceShareLevel: level,
	})
	if err != nil {
		t.Fatalf("ShareAllFinance: %s", err)
	}
	return share
}

func reachedAllLevel(t *testing.T, tx db.Transaction, viewerUserId string) models.FinanceShareLevel {
	t.Helper()
	reached, err := tx.ReachAllFinance(viewerUserId, mustOwner(t, tx))
	if err != nil {
		t.Fatalf("ReachAllFinance: %s", err)
	}
	if reached == nil {
		return ""
	}
	return reached.FinanceShareLevel
}

// mustOwner is the identifier of ada, the owner in every world here.
func mustOwner(t *testing.T, tx db.Transaction) string {
	t.Helper()
	ada, err := tx.GetUserByUsername("ada")
	if err != nil || ada == nil {
		t.Fatalf("GetUserByUsername: %+v %v", ada, err)
	}
	return ada.ID
}

// amountOf is a decimal amount of a row as a number, enough to add up and
// compare to the cent.
func amountOf(t *testing.T, amount string) float64 {
	t.Helper()
	value, err := strconv.ParseFloat(amount, 64)
	if err != nil {
		t.Fatalf("%q is not an amount: %s", amount, err)
	}
	return value
}

// spendingLines are the rows of the months as sorted lines, to compare.
func spendingLines(rows []*models.SharedFinanceSpendingRow) []string {
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		lines = append(lines, fmt.Sprintf("%s|%s|%s|out %s|in %s|%d", row.SpendingMonth, row.SpendingCategoryName, row.CurrencyCode,
			row.MoneyOut, row.MoneyIn, row.FinanceTransactionCount))
	}
	sort.Strings(lines)
	return lines
}

// linkSavingsAndTransfer links a third account after the share was made, with a
// salary in another currency, and a transfer between checking and the card
// that the owner's own spending leaves out: -300 on the checking account and
// +300 on the card, both in the transfer category.
func linkSavingsAndTransfer(t *testing.T, tx db.Transaction, world *financeShareWorld) {
	t.Helper()
	result := sampleFinanceSync()
	result.Accounts = append(result.Accounts, finance.Account{
		ProviderAccountID: "account-savings", AccountName: "Rainy Day Savings", AccountMask: "0777",
		AccountKind: finance.AccountKindDepository, CurrencyCode: "EUR", CurrentBalance: "900.00",
	})
	result.Added = []finance.Transaction{
		{ProviderTransactionID: "transaction-bonus", ProviderAccountID: "account-savings", PostedOn: "2026-09-20", Amount: "120", CurrencyCode: "EUR", Description: "BONUS EXAMPLE CO"},
		{ProviderTransactionID: "transaction-out", ProviderAccountID: "account-checking", PostedOn: "2026-09-21", Amount: "-300", CurrencyCode: "USD", Description: "CARD PAYMENT OUT"},
		{ProviderTransactionID: "transaction-in", ProviderAccountID: "account-card", PostedOn: "2026-09-21", Amount: "300", CurrencyCode: "USD", Description: "CARD PAYMENT IN"},
	}
	source, err := tx.PutAgentSource(&models.AgentKnowledgeSource{
		AgentID: world.agentId, Kind: models.SourceWeb, Name: "second institution",
		Specification: models.AgentKnowledgeSpecification{Start: "https://example.test/second"},
	})
	if err != nil {
		t.Fatalf("PutAgentSource: %s", err)
	}
	if _, err := tx.ApplyFinanceSync(world.agentId, source.ID, result, "2026-09-25"); err != nil {
		t.Fatalf("ApplyFinanceSync: %s", err)
	}
	transferId := transferCategoryId(t, tx, world.agentId)
	found := financeTransactionsByProviderId(t, tx, world.agentId)
	for _, providerId := range []string{"transaction-out", "transaction-in"} {
		if _, err := tx.SetTransactionCategorization(world.agentId, found[providerId].ID, transferId, models.CategorizedByPerson, nil); err != nil {
			t.Fatalf("SetTransactionCategorization: %s", err)
		}
	}
}

// Sharing all finance needs the sharer's own agent and a circle in common with
// a person or membership of a circle, says not found rather than why, offers
// spending and transactions only (a balance belongs to one account), and
// sharing again changes the level of the same share.
func TestShareAllFinanceNeedsACircleInCommonAndHasTwoLevels(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newFinanceShareWorld(t, tx)

		share := shareAllFinance(t, tx, world, world.ben.ID, "", models.FinanceShareLevelSpending)
		if share.ID == "" || share.OwnerUserID != world.ada.ID || share.CreatedByUserID != world.ada.ID || share.FinanceShareLevel != models.FinanceShareLevelSpending {
			t.Fatalf("a share with someone in a shared circle: %+v", share)
		}
		again := shareAllFinance(t, tx, world, world.ben.ID, "", models.FinanceShareLevelTransactions)
		if again.ID != share.ID || again.FinanceShareLevel != models.FinanceShareLevelTransactions || !again.CreatedAt.Equal(share.CreatedAt) {
			t.Fatalf("sharing again changes the level of the same share: %+v", again)
		}
		shareAllFinance(t, tx, world, "", world.home.ID, models.FinanceShareLevelSpending)

		for _, stranger := range []*models.User{world.cora, world.dave} {
			_, err := tx.ShareAllFinance(world.ada.ID, &models.AllFinanceShare{GranteeUserID: stranger.ID, FinanceShareLevel: models.FinanceShareLevelSpending})
			if !errors.Is(err, db.ErrNotFound) {
				t.Fatalf("a share with %s needs a circle in common: %v", stranger.Username, err)
			}
		}
		if _, err := tx.ShareAllFinance(world.ada.ID, &models.AllFinanceShare{GranteeCircleID: world.club.ID, FinanceShareLevel: models.FinanceShareLevelSpending}); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("a share with a circle she is not in: %v", err)
		}
		if _, err := tx.ShareAllFinance(world.ada.ID, &models.AllFinanceShare{GranteeUserID: "nobody", FinanceShareLevel: models.FinanceShareLevelSpending}); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("a person who does not exist: %v", err)
		}
		for name, bad := range map[string]*models.AllFinanceShare{
			"nolevel":   {GranteeUserID: world.ben.ID},
			"balance":   {GranteeUserID: world.ben.ID, FinanceShareLevel: models.FinanceShareLevelBalance},
			"wrongcase": {GranteeUserID: world.ben.ID, FinanceShareLevel: "Spending"},
			"both":      {GranteeUserID: world.ben.ID, GranteeCircleID: world.home.ID, FinanceShareLevel: models.FinanceShareLevelSpending},
			"neither":   {FinanceShareLevel: models.FinanceShareLevelSpending},
			"herself":   {GranteeUserID: world.ada.ID, FinanceShareLevel: models.FinanceShareLevelSpending},
		} {
			if _, err := tx.ShareAllFinance(world.ada.ID, bad); !errors.Is(err, db.ErrInvalidArguments) {
				t.Errorf("a share that is %s: %v", name, err)
			}
		}

		made, err := tx.ListAllFinanceSharesBy(world.ada.ID)
		if err != nil || len(made) != 2 || made[0].GranteeUsername != "ben" || made[1].GranteeCircleName != "Home" || made[0].OwnerUsername != "ada" {
			t.Fatalf("ListAllFinanceSharesBy: %+v %v", made, err)
		}
		if others, _ := tx.ListAllFinanceSharesBy(world.ben.ID); len(others) != 0 {
			t.Errorf("ben shares nothing: %+v", others)
		}

		// Only the owner takes it back.
		if err := tx.UnshareAllFinance(world.ben.ID, made[0].ID); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("ben takes back ada's share: %v", err)
		}
		if err := tx.UnshareAllFinance(world.ada.ID, made[0].ID); err != nil {
			t.Fatalf("UnshareAllFinance: %s", err)
		}
		if err := tx.UnshareAllFinance(world.ada.ID, made[0].ID); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("twice: %v", err)
		}
		// Ben still reads through the circle's share.
		if got := reachedAllLevel(t, tx, world.ben.ID); got != models.FinanceShareLevelSpending {
			t.Errorf("after the share by name was taken back: %q", got)
		}
	})
}

// The combined spending is the sum over the owner's accounts, now and any
// linked after the share, in each currency, with the owner's category names;
// a transfer between the owner's own accounts is left out exactly as the
// owner's own spending summary leaves it out, so nothing is counted twice; and
// the reader is told no account name, mask or balance.
func TestSharedAllFinanceSpendingIsTheSumOfEveryAccount(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newFinanceShareWorld(t, tx)
		if _, err := tx.ListSharedAllFinanceSpending(world.ben.ID, world.ada.ID, "", ""); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("before any share: %v", err)
		}
		shareAllFinance(t, tx, world, world.ben.ID, "", models.FinanceShareLevelSpending)

		rows, err := tx.ListSharedAllFinanceSpending(world.ben.ID, world.ada.ID, "", "")
		if err != nil {
			t.Fatalf("ListSharedAllFinanceSpending: %s", err)
		}
		// Groceries on checking, the salary on checking and the diner on the card
		// are three lines of one month: two uncategorized charges share a line.
		want := []string{
			"2026-09|Groceries|USD|out 42.1700|in 0.0000|1",
			"2026-09||USD|out 18.4000|in 2500.0000|2",
		}
		sort.Strings(want)
		if got := spendingLines(rows); strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Fatalf("both accounts together:\n got %v\nwant %v", got, want)
		}

		// An account linked after the share is covered with no new share, and a
		// transfer between two of the owner's accounts counts as neither.
		linkSavingsAndTransfer(t, tx, world)
		rows, err = tx.ListSharedAllFinanceSpending(world.ben.ID, world.ada.ID, "", "")
		if err != nil {
			t.Fatalf("after linking an account: %s", err)
		}
		want = []string{
			"2026-09|Groceries|USD|out 42.1700|in 0.0000|1",
			"2026-09||USD|out 18.4000|in 2500.0000|2",
			"2026-09||EUR|out 0.0000|in 120.0000|1",
		}
		sort.Strings(want)
		if got := spendingLines(rows); strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Fatalf("a later account, the transfer left out:\n got %v\nwant %v", got, want)
		}

		// The same numbers as the owner's own Spending page, month by month.
		own, err := tx.FinanceSpendingSummary(world.agentId, &db.FinanceSpendingSummaryFilter{GroupBy: models.FinanceSpendingSummaryGroupByMonth})
		if err != nil {
			t.Fatalf("FinanceSpendingSummary: %s", err)
		}
		ownOut, ownIn := map[string]string{}, map[string]string{}
		for _, row := range own {
			ownOut[row.CurrencyCode], ownIn[row.CurrencyCode] = row.MoneyOut, row.MoneyIn
		}
		if ownOut["USD"] != "60.5700" || ownIn["USD"] != "2500.0000" || ownIn["EUR"] != "120.0000" {
			t.Fatalf("the owner's own summary: %+v", own)
		}
		sharedOut, sharedIn := map[string]float64{}, map[string]float64{}
		for _, row := range rows {
			sharedOut[row.CurrencyCode] += amountOf(t, row.MoneyOut)
			sharedIn[row.CurrencyCode] += amountOf(t, row.MoneyIn)
		}
		if fmt.Sprintf("%.2f", sharedOut["USD"]) != "60.57" || fmt.Sprintf("%.2f", sharedIn["USD"]) != "2500.00" || fmt.Sprintf("%.2f", sharedIn["EUR"]) != "120.00" {
			t.Errorf("the combined totals differ from the owner's own: out %v in %v", sharedOut, sharedIn)
		}

		// It is the sum of the accounts read one at a time, which a person who
		// also holds a share of each account reads as well.
		for _, account := range []*models.FinanceAccount{world.checking, world.card} {
			shareFinance(t, tx, world, account, world.ben.ID, "", models.FinanceShareLevelSpending)
		}
		byAccount := map[string]float64{}
		for _, account := range []*models.FinanceAccount{world.checking, world.card} {
			accountRows, err := tx.ListSharedFinanceSpending(world.ben.ID, account.ID, "", "")
			if err != nil {
				t.Fatalf("ListSharedFinanceSpending: %s", err)
			}
			for _, row := range accountRows {
				byAccount[row.CurrencyCode] += amountOf(t, row.MoneyOut)
			}
		}
		if fmt.Sprintf("%.2f", byAccount["USD"]) != "60.57" {
			t.Errorf("the accounts one at a time add up to %v, the combined read says 60.57", byAccount)
		}

		// No account name, mask or balance is anywhere in what the reader gets.
		encoded, _ := json.Marshal(rows)
		for _, secret := range []string{"Everyday Checking", "Travel Card", "Rainy Day", "0001", "0777", "1200", "900", "Corner Grocer", "CORNER GROCER", "PAYROLL", "BONUS"} {
			if strings.Contains(string(encoded), secret) {
				t.Errorf("%q reaches a reader of the combined spending: %s", secret, encoded)
			}
		}
	})
}

// At the spending level a range of days is widened to whole months, and at the
// transactions level it is kept as given.
func TestSharedAllFinanceSpendingWidensARangeBelowTransactions(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newFinanceShareWorld(t, tx)
		shareAllFinance(t, tx, world, world.ben.ID, "", models.FinanceShareLevelSpending)
		if rows, err := tx.ListSharedAllFinanceSpending(world.ben.ID, world.ada.ID, "2026-09-10", "2026-09-10"); err != nil || len(rows) != 2 {
			t.Errorf("a one-day range at the spending level is the whole month: %+v %v", rows, err)
		}
		if rows, err := tx.ListSharedAllFinanceSpending(world.ben.ID, world.ada.ID, "2026-08-31", "2026-08-31"); err != nil || len(rows) != 0 {
			t.Errorf("a range in the month before: %+v %v", rows, err)
		}
		if _, err := tx.ListSharedAllFinanceSpending(world.ben.ID, world.ada.ID, "yesterday", ""); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("a day that is not a day: %v", err)
		}
		shareAllFinance(t, tx, world, world.ben.ID, "", models.FinanceShareLevelTransactions)
		if rows, err := tx.ListSharedAllFinanceSpending(world.ben.ID, world.ada.ID, "2026-09-05", "2026-09-30"); err != nil || len(rows) != 2 {
			t.Errorf("a range at the transactions level: %+v %v", rows, err)
		}
		if rows, err := tx.ListSharedAllFinanceSpending(world.ben.ID, world.ada.ID, "2026-09-11", "2026-09-30"); err != nil || len(rows) != 1 || rows[0].FinanceTransactionCount != 1 {
			t.Errorf("a range at the transactions level is kept as given, only the diner: %+v %v", rows, err)
		}
	})
}

// The transactions of all accounts come as one page with their day, amount,
// currency, merchant and category and nothing that names an account, only at
// the transactions level, and are paged.
func TestSharedAllFinanceTransactionsAreOnePageAcrossAccounts(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newFinanceShareWorld(t, tx)
		shareAllFinance(t, tx, world, world.ben.ID, "", models.FinanceShareLevelSpending)
		if _, err := tx.ListSharedAllFinanceTransactions(world.ben.ID, world.ada.ID, nil); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("transactions at the spending level: %v", err)
		}
		shareAllFinance(t, tx, world, world.ben.ID, "", models.FinanceShareLevelTransactions)
		linkSavingsAndTransfer(t, tx, world)

		page, err := tx.ListSharedAllFinanceTransactions(world.ben.ID, world.ada.ID, nil)
		if err != nil {
			t.Fatalf("ListSharedAllFinanceTransactions: %s", err)
		}
		// Three from the first sync, the bonus, and both legs of the transfer:
		// the owner's own Transactions page lists the transfer too.
		if len(page.FinanceTransactions) != 6 || page.TotalCount != 6 {
			t.Fatalf("every account's transactions: %d of %d", len(page.FinanceTransactions), page.TotalCount)
		}
		newest := page.FinanceTransactions[0]
		if newest.PostedOn != "2026-09-21" {
			t.Errorf("newest first: %+v", newest)
		}
		byMerchant := map[string]*models.SharedFinanceTransaction{}
		for _, each := range page.FinanceTransactions {
			byMerchant[each.MerchantName] = each
		}
		grocer := byMerchant["Corner Grocer"]
		if grocer == nil || grocer.Amount != "-42.1700" || grocer.CurrencyCode != "USD" || grocer.SpendingCategoryName != "Groceries" || grocer.PostedOn != "2026-09-10" {
			t.Errorf("day, amount, currency, merchant and category: %+v", grocer)
		}
		if diner := byMerchant["Lakeside Diner"]; diner == nil || diner.Amount != "-18.4000" {
			t.Errorf("a transaction of the second account: %+v", diner)
		}
		encoded, _ := json.Marshal(page)
		for _, secret := range []string{"Everyday Checking", "Travel Card", "Rainy Day", "0001", "0777", "1200", "CORNER GROCER 0412", "PAYROLL EXAMPLE CO", "payee", "accountName", "financeAccountId"} {
			if strings.Contains(string(encoded), secret) {
				t.Errorf("%q reaches a reader of the combined transactions: %s", secret, encoded)
			}
		}

		first, err := tx.ListSharedAllFinanceTransactions(world.ben.ID, world.ada.ID, &db.SharedFinanceTransactionFilter{Limit: 4})
		if err != nil || len(first.FinanceTransactions) != 4 || first.NextCursor == "" || first.TotalCount != 6 {
			t.Fatalf("the first page: %+v %v", first, err)
		}
		second, err := tx.ListSharedAllFinanceTransactions(world.ben.ID, world.ada.ID, &db.SharedFinanceTransactionFilter{Limit: 4, After: first.NextCursor})
		if err != nil || len(second.FinanceTransactions) != 2 || second.NextCursor != "" {
			t.Fatalf("the second page: %+v %v", second, err)
		}
		if rangeOnly, err := tx.ListSharedAllFinanceTransactions(world.ben.ID, world.ada.ID, &db.SharedFinanceTransactionFilter{From: "2026-09-12", To: "2026-09-20"}); err != nil || len(rangeOnly.FinanceTransactions) != 2 {
			t.Errorf("a range: %+v %v", rangeOnly, err)
		}
	})
}

// Whole-finance shares reach the people a share of one account reaches and no
// more: a person outside the circle reads nothing, a person without agent:use
// reads nothing, and nothing is read from an owner whose agent is off.
func TestAnAllFinanceShareReachesOnlyPeopleWhoMayUseFinance(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newFinanceShareWorld(t, tx)
		shareAllFinance(t, tx, world, "", world.home.ID, models.FinanceShareLevelTransactions)

		for _, outsider := range []*models.User{world.cora, world.dave} {
			if _, err := tx.ListSharedAllFinanceSpending(outsider.ID, world.ada.ID, "", ""); !errors.Is(err, db.ErrNotFound) {
				t.Errorf("%s outside the circle reads spending: %v", outsider.Username, err)
			}
			if _, err := tx.ListSharedAllFinanceTransactions(outsider.ID, world.ada.ID, nil); !errors.Is(err, db.ErrNotFound) {
				t.Errorf("%s outside the circle reads transactions: %v", outsider.Username, err)
			}
			if reached, _ := tx.ListReachedAllFinance(outsider.ID); len(reached) != 0 {
				t.Errorf("%s: %+v", outsider.Username, reached)
			}
		}
		// The owner does not read their own through a share, and a share is one way.
		if got := reachedAllLevel(t, tx, world.ada.ID); got != "" {
			t.Errorf("the owner: %q", got)
		}
		if reached, _ := tx.ReachAllFinance(world.ada.ID, world.ben.ID); reached != nil {
			t.Errorf("ben shares nothing with ada: %+v", reached)
		}
		reached, err := tx.ListReachedAllFinance(world.ben.ID)
		if err != nil || len(reached) != 1 || reached[0].OwnerUsername != "ada" || reached[0].FinanceShareLevel != models.FinanceShareLevelTransactions {
			t.Fatalf("ListReachedAllFinance: %+v %v", reached, err)
		}

		// Cora joins the circle but may not use finance until she holds agent:use.
		if _, err := tx.AddCircleMember(world.home.ID, world.cora.ID, models.MemberRoleMember, world.ada.ID); err != nil {
			t.Fatal(err)
		}
		if got := reachedAllLevel(t, tx, world.cora.ID); got != "" {
			t.Fatalf("a viewer without agent:use: %q", got)
		}
		dbtest.GrantPermission(t, tx, world.cora.ID, models.PermissionAgentUse)
		if got := reachedAllLevel(t, tx, world.cora.ID); got != models.FinanceShareLevelTransactions {
			t.Fatalf("with agent:use: %q", got)
		}

		if _, err := tx.UpdateAgent(world.agentId, func(agent *models.Agent) error { agent.Enabled = false; return nil }); err != nil {
			t.Fatal(err)
		}
		if got := reachedAllLevel(t, tx, world.ben.ID); got != "" {
			t.Fatalf("an agent that is off: %q", got)
		}
		if _, err := tx.UpdateAgent(world.agentId, func(agent *models.Agent) error {
			now := time.Now()
			agent.Enabled, agent.OperatorDisabledAt = true, &now
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if got := reachedAllLevel(t, tx, world.ben.ID); got != "" {
			t.Fatalf("an agent the operator disabled: %q", got)
		}
		if _, err := tx.UpdateAgent(world.agentId, func(agent *models.Agent) error { agent.OperatorDisabledAt = nil; return nil }); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.UpdateUser(world.cora.ID, func(person *models.User) error {
			now := time.Now()
			person.DisabledAt = &now
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if got := reachedAllLevel(t, tx, world.cora.ID); got != "" {
			t.Fatalf("a disabled viewer: %q", got)
		}
		if _, err := tx.UpdateUser(world.ada.ID, func(owner *models.User) error {
			now := time.Now()
			owner.DisabledAt = &now
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if got := reachedAllLevel(t, tx, world.ben.ID); got != "" {
			t.Fatalf("a disabled owner: %q", got)
		}
	})
}

// The highest level that reaches a person wins, between a share by name and a
// circle's, and for an account that a share of that account also reaches: the
// level of the account is the higher of the two. A share of all finance names
// no account, so it adds none to the list of accounts.
func TestTheHighestLevelWinsBetweenAllFinanceAndOneAccount(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newFinanceShareWorld(t, tx)
		shareAllFinance(t, tx, world, "", world.home.ID, models.FinanceShareLevelTransactions)
		shareAllFinance(t, tx, world, world.ben.ID, "", models.FinanceShareLevelSpending)
		if got := reachedAllLevel(t, tx, world.ben.ID); got != models.FinanceShareLevelTransactions {
			t.Fatalf("a share by name at spending and a circle's at transactions: %q", got)
		}
		reaching, err := tx.ListAllFinanceSharesReaching(world.ben.ID)
		if err != nil || len(reaching) != 2 || reaching[0].OwnerUsername != "ada" {
			t.Fatalf("every share that reaches ben: %+v %v", reaching, err)
		}

		// No account is listed for a person who shares all their finance only.
		if accounts, err := tx.ListReachedFinanceAccounts(world.ben.ID, ""); err != nil || len(accounts) != 0 {
			t.Fatalf("a share of all finance names no account: %+v %v", accounts, err)
		}
		if got := reachedLevel(t, tx, world.ben.ID, world.checking); got != "" {
			t.Fatalf("an account reached only through all of the finance is not reached on its own: %q", got)
		}

		// An account shared on its own at the balance level is read at the level
		// of the whole, the higher of the two.
		shareFinance(t, tx, world, world.checking, world.ben.ID, "", models.FinanceShareLevelBalance)
		if got := reachedLevel(t, tx, world.ben.ID, world.checking); got != models.FinanceShareLevelTransactions {
			t.Fatalf("balance on its own, transactions for all: %q", got)
		}
		accounts, err := tx.ListReachedFinanceAccounts(world.ben.ID, "")
		if err != nil || len(accounts) != 1 || accounts[0].FinanceShareLevel != models.FinanceShareLevelTransactions {
			t.Fatalf("the account lists at the higher level: %+v %v", accounts, err)
		}
		page, err := tx.ListSharedFinanceTransactions(world.ben.ID, world.checking.ID, nil)
		if err != nil || len(page.FinanceTransactions) != 2 {
			t.Fatalf("one account's transactions: %+v %v", page, err)
		}
		// A share of one account on its own that is higher than the whole is read
		// per account and does not make the combined read wider. The circle's
		// share goes, leaving the whole at spending.
		for _, share := range mustAllShares(t, tx, world) {
			if share.GranteeCircleID != "" {
				if err := tx.UnshareAllFinance(world.ada.ID, share.ID); err != nil {
					t.Fatal(err)
				}
			}
		}
		shareFinance(t, tx, world, world.checking, world.ben.ID, "", models.FinanceShareLevelTransactions)
		if _, err := tx.ListSharedAllFinanceTransactions(world.ben.ID, world.ada.ID, nil); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("the whole at spending and one account at transactions: the combined page is %v", err)
		}
		if page, err := tx.ListSharedFinanceTransactions(world.ben.ID, world.checking.ID, nil); err != nil || len(page.FinanceTransactions) != 2 {
			t.Errorf("that account on its own: %+v %v", page, err)
		}
	})
}

func mustAllShares(t *testing.T, tx db.Transaction, world *financeShareWorld) []*models.AllFinanceShare {
	t.Helper()
	shares, err := tx.ListAllFinanceSharesBy(world.ada.ID)
	if err != nil {
		t.Fatalf("ListAllFinanceSharesBy: %s", err)
	}
	return shares
}

// Leaving a circle ends the shares of all finance that only the circle made
// possible, as it does the shares of one account, and a member who leaves
// simply stops being reached.
func TestLeavingACircleEndsTheAllFinanceSharesItMadePossible(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newFinanceShareWorld(t, tx)
		shareAllFinance(t, tx, world, world.ben.ID, "", models.FinanceShareLevelSpending)
		shareAllFinance(t, tx, world, "", world.home.ID, models.FinanceShareLevelSpending)

		if _, err := tx.RemoveCircleMember(world.home.ID, world.ben.ID); err != nil {
			t.Fatalf("RemoveCircleMember: %s", err)
		}
		if got := reachedAllLevel(t, tx, world.ben.ID); got != "" {
			t.Errorf("ben after leaving: %q", got)
		}
		left := mustAllShares(t, tx, world)
		if len(left) != 1 || left[0].GranteeCircleName != "Home" {
			t.Errorf("only the share to the circle is left, still ada's: %+v", left)
		}

		// Back in the circle, the circle's share reaches him with no new share.
		if _, err := tx.AddCircleMember(world.home.ID, world.ben.ID, models.MemberRoleMember, world.ada.ID); err != nil {
			t.Fatal(err)
		}
		if got := reachedAllLevel(t, tx, world.ben.ID); got != models.FinanceShareLevelSpending {
			t.Errorf("a member who joins reads what the circle was shared: %q", got)
		}

		// Ada leaves her own circle: her share to it ends.
		if _, err := tx.AddCircleMember(world.home.ID, world.dave.ID, models.MemberRoleAdmin, world.ada.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.RemoveCircleMember(world.home.ID, world.ada.ID); err != nil {
			t.Fatalf("RemoveCircleMember: %s", err)
		}
		if left := mustAllShares(t, tx, world); len(left) != 0 {
			t.Errorf("a share to a circle its sharer left: %+v", left)
		}
		if got := reachedAllLevel(t, tx, world.ben.ID); got != "" {
			t.Errorf("after the sharer left: %q", got)
		}
	})
}

// A circle shared with all of somebody's finance is sensitive, lists the owner
// among those who share, and a share made to a person by name can be declined.
func TestAnAllFinanceShareCountsAmongTheSharesOfACircleAndCanBeDeclined(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newFinanceShareWorld(t, tx)
		shareAllFinance(t, tx, world, world.ben.ID, "", models.FinanceShareLevelSpending)
		if isSensitive, _ := tx.CircleHasSensitiveShare(world.home.ID); isSensitive {
			t.Errorf("a share to a person is not a share to the circle")
		}
		circleShare := shareAllFinance(t, tx, world, "", world.home.ID, models.FinanceShareLevelSpending)
		if isSensitive, err := tx.CircleHasSensitiveShare(world.home.ID); err != nil || !isSensitive {
			t.Errorf("all finance shared with the circle: %v %v", isSensitive, err)
		}
		owners, err := tx.ListCircleShareOwners(world.home.ID)
		if err != nil || len(owners) != 1 || owners[0].OwnerUserID != world.ada.ID || len(owners[0].ShareKinds) != 1 || owners[0].ShareKinds[0] != models.ShareKindFinance {
			t.Errorf("ListCircleShareOwners: %+v %v", owners, err)
		}
		// A share to a circle cannot be declined, one by name can.
		if err := tx.DeclineFinanceShare(world.ben.ID, circleShare.ID); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("declining a circle's share: %v", err)
		}
		if err := tx.DeclineFinanceShare(world.dave.ID, mustAllShares(t, tx, world)[0].ID); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("declining somebody else's share: %v", err)
		}
		if err := tx.DeclineFinanceShare(world.ben.ID, mustAllShares(t, tx, world)[0].ID); err != nil {
			t.Fatalf("DeclineFinanceShare: %s", err)
		}
		if left := mustAllShares(t, tx, world); len(left) != 1 || left[0].ID != circleShare.ID {
			t.Errorf("only the circle's share is left: %+v", left)
		}
	})
}

// The shares go with the person who owns them.
func TestDeletingTheOwnerDeletesTheirAllFinanceShares(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newFinanceShareWorld(t, tx)
		shareAllFinance(t, tx, world, world.ben.ID, "", models.FinanceShareLevelSpending)
		if err := tx.DeleteUser(world.ada.ID); err != nil {
			t.Fatalf("DeleteUser: %s", err)
		}
		if reached, err := tx.ListReachedAllFinance(world.ben.ID); err != nil || len(reached) != 0 {
			t.Errorf("a share outlived its owner: %+v %v", reached, err)
		}
		if left, err := tx.ListAllFinanceSharesBy(world.ada.ID); err != nil || len(left) != 0 {
			t.Errorf("shares left of a person who is gone: %+v %v", left, err)
		}
	})
}
