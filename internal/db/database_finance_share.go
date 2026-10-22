package db

import (
	"database/sql"
	"sort"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

// FinanceShareOperation is who may see which finance account, and how much: a
// person's finance account shared with another person or with a circle at a
// level (balance, spending or transactions).
//
// "May this person see this finance account, and at what level" has one
// answer, ReachFinanceAccount, and every read of a shared account asks it. A
// caller that compares an account's owner with the person itself is wrong for
// somebody who left the circle that made the share possible. The reads here
// return types that have no field for what a level does not show, so that the
// level is not a thing a caller has to remember to apply.
type FinanceShareOperation interface {
	// ShareFinanceAccount gives a person or a circle a level of access to a
	// finance account of the sharer, or changes the level they have.
	// ErrNotFound unless the account is held by the sharer's agent and, for a
	// person, the two share a circle, or, for a circle, the sharer is in it.
	// ErrInvalidArguments for a level that cannot be given, a grantee that is
	// not exactly one, or the sharer themselves.
	ShareFinanceAccount(sharerUserId string, share *models.FinanceShare) (*models.FinanceShare, error)

	// UnshareFinanceAccount takes one of the owner's shares back. ErrNotFound
	// when the owner has no such share.
	UnshareFinanceAccount(ownerUserId, shareId string) error

	// ListFinanceSharesBy are the shares the person has made, oldest first,
	// with which account each is of and who it is for.
	ListFinanceSharesBy(ownerUserId string) ([]*models.FinanceShare, error)

	// ListFinanceSharesReaching are the shares that reach the person now,
	// oldest first, one per finance account: at the highest level that reaches
	// them, with whose account it is. When a share to the person by name
	// reaches them too, the row is that share, so they can decline it. A share
	// to a person reaches them only while the two still share a circle, and a
	// share to a circle only while both are in it. Not the person's own.
	ListFinanceSharesReaching(userId string) ([]*models.FinanceShare, error)

	// ListReachedFinanceAccounts are the finance accounts of other people that
	// reach the viewer, oldest first, each at the highest level that reaches
	// them; of one owner, or, with an empty identifier, of all of them.
	ListReachedFinanceAccounts(viewerUserId, ownerUserId string) ([]*models.ReachedFinanceAccount, error)

	// ReachFinanceAccount is one finance account of another person as the
	// viewer reaches it, or nil when it is not shared with them (or the
	// person who owns it, or they, can no longer use finance).
	ReachFinanceAccount(viewerUserId, financeAccountId string) (*models.ReachedFinanceAccount, error)

	// ListSharedFinanceSpending is a shared finance account's money out and in
	// by month and spending category, newest month first, for the days from
	// and to ("2006-01-02", either may be empty). ErrNotFound unless the
	// account reaches the viewer at the spending level or better.
	ListSharedFinanceSpending(viewerUserId, financeAccountId, from, to string) ([]*models.SharedFinanceSpendingRow, error)

	// ListSharedFinanceTransactions is a page of a shared finance account's
	// transactions, newest first. ErrNotFound unless the account reaches the
	// viewer at the transactions level.
	ListSharedFinanceTransactions(viewerUserId, financeAccountId string, filter *SharedFinanceTransactionFilter) (*SharedFinanceTransactionPage, error)
}

// SharedFinanceTransactionFilter narrows a page of a shared account's
// transactions. Every field is optional.
type SharedFinanceTransactionFilter struct {
	// From and To bound the posted day, both included, "2006-01-02".
	From string
	To   string

	// Limit is at most FinanceTransactionLimitMost; zero is
	// FinanceTransactionLimitDefault. After is the NextCursor of the page
	// before, and Offset how many to pass over first, counted from the cursor
	// when both are given.
	Limit  int
	After  string
	Offset int
}

// SharedFinanceTransactionPage is one page of a shared account's
// transactions, the cursor for the next, empty on the last, and how many
// transactions the account has in the days asked for, on every page.
type SharedFinanceTransactionPage struct {
	FinanceTransactions []*models.SharedFinanceTransaction
	NextCursor          string
	TotalCount          int
}

type financeAccountShareModel struct {
	ID                string    `gorm:"column:id;primaryKey"`
	FinanceAccountID  string    `gorm:"column:finance_account_id"`
	GranteeUserID     *string   `gorm:"column:grantee_user_id"`
	GranteeCircleID   *string   `gorm:"column:grantee_circle_id"`
	FinanceShareLevel string    `gorm:"column:finance_share_level"`
	CreatedAt         time.Time `gorm:"column:created_at"`
	CreatedByUserID   *string   `gorm:"column:created_by_user_id"`
}

func (financeAccountShareModel) TableName() string { return "finance_account_share" }

func (self *financeAccountShareModel) toModel() *models.FinanceShare {
	share := &models.FinanceShare{
		ID:                self.ID,
		FinanceAccountID:  self.FinanceAccountID,
		FinanceShareLevel: models.FinanceShareLevel(self.FinanceShareLevel),
		CreatedAt:         self.CreatedAt.In(time.Local),
	}
	if self.GranteeUserID != nil {
		share.GranteeUserID = *self.GranteeUserID
	}
	if self.GranteeCircleID != nil {
		share.GranteeCircleID = *self.GranteeCircleID
	}
	if self.CreatedByUserID != nil {
		share.CreatedByUserID = *self.CreatedByUserID
	}
	return share
}

func (self *transaction) ShareFinanceAccount(sharerUserId string, share *models.FinanceShare) (*models.FinanceShare, error) {
	if share == nil || sharerUserId == "" || !share.FinanceShareLevel.IsGrantable() ||
		(share.GranteeUserID == "") == (share.GranteeCircleID == "") || share.GranteeUserID == sharerUserId {
		return nil, ErrInvalidArguments
	}
	// Only an account held by the sharer's own agent is shared. The row is
	// held for the rest of the transaction, so that deleting the account
	// waits for this and its share goes with it.
	var accountIds []string
	if err := self.tx.Raw(`SELECT account.id FROM "agent_finance_account" account
		JOIN "agent" owner_agent ON owner_agent.id = account.agent_id
		WHERE account.id = ? AND owner_agent.user_id = ? FOR SHARE OF account`, share.FinanceAccountID, sharerUserId).Scan(&accountIds).Error; err != nil {
		return nil, err
	}
	if len(accountIds) == 0 {
		return nil, ErrNotFound
	}
	if share.GranteeUserID != "" {
		var userCount int64
		if err := self.tx.Model(&userModel{}).Where("\"id\" = ?", share.GranteeUserID).Count(&userCount).Error; err != nil {
			return nil, err
		}
		if userCount == 0 {
			return nil, ErrNotFound
		}
		isMutual, err := self.holdMutualCircle(sharerUserId, share.GranteeUserID)
		if err != nil {
			return nil, err
		}
		if !isMutual {
			return nil, ErrNotFound
		}
	} else {
		var memberRows []circleMemberModel
		if err := self.tx.Clauses(clause.Locking{Strength: "SHARE"}).
			Where("\"circle_id\" = ? AND \"user_id\" = ?", share.GranteeCircleID, sharerUserId).Find(&memberRows).Error; err != nil {
			return nil, err
		}
		if len(memberRows) == 0 {
			return nil, ErrNotFound
		}
	}

	row := &financeAccountShareModel{
		ID:                newID(),
		FinanceAccountID:  share.FinanceAccountID,
		GranteeUserID:     nullableText(share.GranteeUserID),
		GranteeCircleID:   nullableText(share.GranteeCircleID),
		FinanceShareLevel: string(share.FinanceShareLevel),
		CreatedAt:         time.Now(),
		CreatedByUserID:   &sharerUserId,
	}
	conflict := clause.OnConflict{DoUpdates: clause.AssignmentColumns([]string{"finance_share_level"})}
	query := self.tx.Where("\"finance_account_id\" = ?", share.FinanceAccountID)
	if share.GranteeUserID != "" {
		conflict.Columns = []clause.Column{{Name: "finance_account_id"}, {Name: "grantee_user_id"}}
		query = query.Where("\"grantee_user_id\" = ?", share.GranteeUserID)
	} else {
		conflict.Columns = []clause.Column{{Name: "finance_account_id"}, {Name: "grantee_circle_id"}}
		query = query.Where("\"grantee_circle_id\" = ?", share.GranteeCircleID)
	}
	if err := self.tx.Clauses(conflict).Create(row).Error; err != nil {
		return nil, err
	}
	// Read back, because an upsert that changed an existing share keeps its
	// identifier and its date.
	var kept financeAccountShareModel
	if err := query.Take(&kept).Error; err != nil {
		return nil, err
	}
	return kept.toModel(), nil
}

func (self *transaction) UnshareFinanceAccount(ownerUserId, shareId string) error {
	if ownerUserId == "" || shareId == "" {
		return ErrNotFound
	}
	result := self.tx.Exec(`DELETE FROM "finance_account_share" WHERE "id" = @share AND "finance_account_id" IN (
		SELECT account.id FROM "agent_finance_account" account
		JOIN "agent" owner_agent ON owner_agent.id = account.agent_id
		WHERE owner_agent.user_id = @owner)`, sql.Named("share", shareId), sql.Named("owner", ownerUserId))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// nameFinanceShares fills in the names the listings show: which account each
// share is of, whose it is, and who it is for.
func nameFinanceShares(tx *gorm.DB, shares []*models.FinanceShare) error {
	accountIds, circleIds := []string{}, []string{}
	for _, share := range shares {
		accountIds = append(accountIds, share.FinanceAccountID)
		if share.GranteeCircleID != "" {
			circleIds = append(circleIds, share.GranteeCircleID)
		}
	}
	type accountRow struct {
		ID          string `gorm:"column:id"`
		AccountName string `gorm:"column:account_name"`
		AccountMask string `gorm:"column:account_mask"`
		OwnerUserID string `gorm:"column:owner_user_id"`
	}
	accounts := map[string]accountRow{}
	if len(accountIds) > 0 {
		var rows []accountRow
		if err := tx.Raw(`SELECT account.id AS id, account.account_name AS account_name, account.account_mask AS account_mask,
			owner_agent.user_id AS owner_user_id
			FROM "agent_finance_account" account JOIN "agent" owner_agent ON owner_agent.id = account.agent_id
			WHERE account.id IN ?`, uniqueStrings(accountIds)).Scan(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			accounts[row.ID] = row
		}
	}
	userIds := []string{}
	for _, share := range shares {
		userIds = append(userIds, accounts[share.FinanceAccountID].OwnerUserID)
		if share.GranteeUserID != "" {
			userIds = append(userIds, share.GranteeUserID)
		}
	}
	users, err := loadUsersById(tx, userIds)
	if err != nil {
		return err
	}
	circleNames := map[string]string{}
	if len(circleIds) > 0 {
		var circles []circleModel
		if err := tx.Where("\"id\" IN ?", circleIds).Find(&circles).Error; err != nil {
			return err
		}
		for _, circle := range circles {
			circleNames[circle.ID] = circle.Name
		}
	}
	for _, share := range shares {
		account := accounts[share.FinanceAccountID]
		share.AccountName, share.AccountMask, share.OwnerUserID = account.AccountName, account.AccountMask, account.OwnerUserID
		if user := users[account.OwnerUserID]; user != nil {
			share.OwnerUsername, share.OwnerName = user.Username, user.DisplayName()
		}
		if user := users[share.GranteeUserID]; user != nil {
			share.GranteeUsername, share.GranteeName = user.Username, user.DisplayName()
		}
		share.GranteeCircleName = circleNames[share.GranteeCircleID]
	}
	return nil
}

func (self *transaction) ListFinanceSharesBy(ownerUserId string) ([]*models.FinanceShare, error) {
	if ownerUserId == "" {
		return []*models.FinanceShare{}, nil
	}
	var rows []financeAccountShareModel
	if err := self.tx.Raw(`SELECT share.* FROM "finance_account_share" share
		JOIN "agent_finance_account" account ON account.id = share.finance_account_id
		JOIN "agent" owner_agent ON owner_agent.id = account.agent_id
		WHERE owner_agent.user_id = ?
		ORDER BY share.created_at ASC, share.id ASC`, ownerUserId).Scan(&rows).Error; err != nil {
		return nil, err
	}
	shares := make([]*models.FinanceShare, 0, len(rows))
	for index := range rows {
		shares = append(shares, rows[index].toModel())
	}
	if err := nameFinanceShares(self.tx, shares); err != nil {
		return nil, err
	}
	return shares, nil
}

// reachingFinanceSharesQuery selects the shares that reach one viewer, with
// the finance account each is of. It is reachingHealthSharesQuery for finance
// accounts: the joins on circle_member are what make a share stop reaching
// someone the moment the circle it depended on is gone, the joins on the
// owner's agent and account stop a disabled account's, or a switched off
// agent's, finance reaching anybody, and the owner and the viewer both need
// agent:use (@permission), the permission the finance operations themselves
// need, so that an operator who takes it from a role has switched finance off
// for those people and what they hold is no longer read by others.
var reachingFinanceSharesQuery = `
SELECT share.id AS share_id, share.finance_account_id AS finance_account_id,
       share.finance_share_level AS finance_share_level, owner_agent.user_id AS owner_user_id
FROM "finance_account_share" share
JOIN "agent_finance_account" account ON account.id = share.finance_account_id
JOIN "agent" owner_agent ON owner_agent.id = account.agent_id AND owner_agent.enabled AND owner_agent.operator_disabled_at IS NULL
JOIN "user" owner_user ON owner_user.id = owner_agent.user_id AND owner_user.disabled_at IS NULL
WHERE owner_agent.user_id <> @viewer
  AND ` + userHasPermissionSQL("owner_agent.user_id") + `
  AND (@owner = '' OR owner_agent.user_id = @owner)
  AND (@account = '' OR share.finance_account_id = @account)
  AND EXISTS (SELECT 1 FROM "user" viewer_user WHERE viewer_user.id = @viewer AND viewer_user.disabled_at IS NULL)
  AND ` + userHasPermissionSQL("@viewer") + `
  AND (
    (share.grantee_user_id = @viewer AND EXISTS (
        SELECT 1 FROM "circle_member" mine
        JOIN "circle_member" theirs ON theirs.circle_id = mine.circle_id
        WHERE mine.user_id = @viewer AND theirs.user_id = owner_agent.user_id))
    OR
    (share.grantee_circle_id IS NOT NULL
     AND EXISTS (SELECT 1 FROM "circle_member" WHERE circle_id = share.grantee_circle_id AND user_id = @viewer)
     AND EXISTS (SELECT 1 FROM "circle_member" WHERE circle_id = share.grantee_circle_id AND user_id = owner_agent.user_id))
  )`

type reachingFinanceShareRow struct {
	ShareID           string `gorm:"column:share_id"`
	FinanceAccountID  string `gorm:"column:finance_account_id"`
	FinanceShareLevel string `gorm:"column:finance_share_level"`
	OwnerUserID       string `gorm:"column:owner_user_id"`
}

// allReachingFinanceShares are every share that reaches the viewer, for one
// owner and one account or, with empty identifiers, for all of them.
func (self *transaction) allReachingFinanceShares(viewerUserId, ownerUserId, financeAccountId string) ([]reachingFinanceShareRow, error) {
	if viewerUserId == "" {
		return nil, nil
	}
	var rows []reachingFinanceShareRow
	if err := self.tx.Raw(reachingFinanceSharesQuery,
		sql.Named("viewer", viewerUserId), sql.Named("owner", ownerUserId), sql.Named("account", financeAccountId),
		sql.Named("permission", string(models.PermissionAgentUse))).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// highestReachingFinanceShares keeps, for each finance account, the share that
// gives the highest level.
func highestReachingFinanceShares(rows []reachingFinanceShareRow) []reachingFinanceShareRow {
	best := map[string]reachingFinanceShareRow{}
	for _, row := range rows {
		kept, isKept := best[row.FinanceAccountID]
		level, keptLevel := models.FinanceShareLevel(row.FinanceShareLevel), models.FinanceShareLevel(kept.FinanceShareLevel)
		if !isKept || level.Rank() > keptLevel.Rank() || (level.Rank() == keptLevel.Rank() && row.ShareID < kept.ShareID) {
			best[row.FinanceAccountID] = row
		}
	}
	reaching := make([]reachingFinanceShareRow, 0, len(best))
	for _, row := range best {
		reaching = append(reaching, row)
	}
	return reaching
}

// reachingFinanceShares are the shares that reach the viewer, the one of the
// highest level for each finance account, for one owner and one account or,
// with empty identifiers, for all of them.
func (self *transaction) reachingFinanceShares(viewerUserId, ownerUserId, financeAccountId string) ([]reachingFinanceShareRow, error) {
	rows, err := self.allReachingFinanceShares(viewerUserId, ownerUserId, financeAccountId)
	if err != nil {
		return nil, err
	}
	return highestReachingFinanceShares(rows), nil
}

func (self *transaction) ListFinanceSharesReaching(userId string) ([]*models.FinanceShare, error) {
	allReaching, err := self.allReachingFinanceShares(userId, "", "")
	if err != nil {
		return nil, err
	}
	if len(allReaching) == 0 {
		return []*models.FinanceShare{}, nil
	}
	shareIds := make([]string, 0, len(allReaching))
	for _, row := range allReaching {
		shareIds = append(shareIds, row.ShareID)
	}
	var rows []financeAccountShareModel
	if err := self.tx.Where("\"id\" IN ?", shareIds).Order("\"created_at\" ASC, \"id\" ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	shareById := make(map[string]*models.FinanceShare, len(rows))
	directShareByAccount := map[string]*models.FinanceShare{}
	for index := range rows {
		share := rows[index].toModel()
		shareById[share.ID] = share
		// The rows are oldest first, so the oldest share by name of an account
		// is the one kept.
		if share.GranteeUserID == userId && directShareByAccount[share.FinanceAccountID] == nil {
			directShareByAccount[share.FinanceAccountID] = share
		}
	}
	// One row for each account, at the highest level that reaches the person.
	// A share made to the person by name can be declined and one through a
	// circle cannot, so when both reach, the row is the share by name with the
	// highest level: a share by name hidden under a higher circle share would
	// otherwise be out of the person's reach to end.
	shares := make([]*models.FinanceShare, 0, len(allReaching))
	for _, best := range highestReachingFinanceShares(allReaching) {
		share := shareById[best.ShareID]
		if share == nil {
			continue
		}
		if direct := directShareByAccount[best.FinanceAccountID]; direct != nil && direct.ID != share.ID {
			withHighestLevel := *direct
			withHighestLevel.FinanceShareLevel = share.FinanceShareLevel
			share = &withHighestLevel
		}
		shares = append(shares, share)
	}
	sort.SliceStable(shares, func(first, second int) bool {
		if !shares[first].CreatedAt.Equal(shares[second].CreatedAt) {
			return shares[first].CreatedAt.Before(shares[second].CreatedAt)
		}
		return shares[first].ID < shares[second].ID
	})
	if err := nameFinanceShares(self.tx, shares); err != nil {
		return nil, err
	}
	return shares, nil
}

func (self *transaction) ListReachedFinanceAccounts(viewerUserId, ownerUserId string) ([]*models.ReachedFinanceAccount, error) {
	reaching, err := self.reachingFinanceShares(viewerUserId, ownerUserId, "")
	if err != nil {
		return nil, err
	}
	return self.reachedFinanceAccounts(reaching)
}

func (self *transaction) ReachFinanceAccount(viewerUserId, financeAccountId string) (*models.ReachedFinanceAccount, error) {
	if financeAccountId == "" {
		return nil, nil
	}
	reaching, err := self.reachingFinanceShares(viewerUserId, "", financeAccountId)
	if err != nil {
		return nil, err
	}
	reached, err := self.reachedFinanceAccounts(reaching)
	if err != nil || len(reached) == 0 {
		return nil, err
	}
	return reached[0], nil
}

// reachedFinanceAccounts reads the accounts the reaching shares are of, oldest
// first, with the owner and the finance source's provider.
func (self *transaction) reachedFinanceAccounts(reaching []reachingFinanceShareRow) ([]*models.ReachedFinanceAccount, error) {
	if len(reaching) == 0 {
		return []*models.ReachedFinanceAccount{}, nil
	}
	levels := make(map[string]models.FinanceShareLevel, len(reaching))
	accountIds, ownerIds := []string{}, []string{}
	for _, row := range reaching {
		levels[row.FinanceAccountID] = models.FinanceShareLevel(row.FinanceShareLevel)
		accountIds = append(accountIds, row.FinanceAccountID)
		ownerIds = append(ownerIds, row.OwnerUserID)
	}
	var accounts []agentFinanceAccountModel
	if err := self.tx.Where("\"id\" IN ?", accountIds).Order("\"created_at\" ASC, \"id\" ASC").Find(&accounts).Error; err != nil {
		return nil, err
	}
	owners, err := loadUsersById(self.tx, ownerIds)
	if err != nil {
		return nil, err
	}
	ownerByAccount := make(map[string]string, len(reaching))
	for _, row := range reaching {
		ownerByAccount[row.FinanceAccountID] = row.OwnerUserID
	}
	sourceIds := make([]string, 0, len(accounts))
	for _, account := range accounts {
		sourceIds = append(sourceIds, account.SourceID)
	}
	var sources []struct {
		ID           string `gorm:"column:id"`
		ProviderKind string `gorm:"column:provider_kind"`
	}
	if err := self.tx.Raw(`SELECT "id", COALESCE("specification"->>'type', '') AS "provider_kind" FROM "agent_source" WHERE "id" IN ?`,
		uniqueStrings(sourceIds)).Scan(&sources).Error; err != nil {
		return nil, err
	}
	providerKinds := make(map[string]string, len(sources))
	for _, source := range sources {
		providerKinds[source.ID] = source.ProviderKind
	}
	reached := make([]*models.ReachedFinanceAccount, 0, len(accounts))
	for index := range accounts {
		account := accounts[index].toModel()
		entry := &models.ReachedFinanceAccount{
			Account: account, ProviderKind: providerKinds[account.SourceID],
			FinanceShareLevel: levels[account.ID], OwnerUserID: ownerByAccount[account.ID],
		}
		if owner := owners[entry.OwnerUserID]; owner != nil {
			entry.OwnerUsername, entry.OwnerName = owner.Username, owner.DisplayName()
		}
		reached = append(reached, entry)
	}
	return reached, nil
}

// reachFinanceAccountAt is the finance account if it reaches the viewer at
// the level or better, and ErrNotFound otherwise, which is also what an
// account at a lower level answers: whether a level above exists is not the
// reader's business.
func (self *transaction) reachFinanceAccountAt(viewerUserId, financeAccountId string, level models.FinanceShareLevel) (*models.ReachedFinanceAccount, error) {
	reached, err := self.ReachFinanceAccount(viewerUserId, financeAccountId)
	if err != nil {
		return nil, err
	}
	if reached == nil || !reached.FinanceShareLevel.AtLeast(level) {
		return nil, ErrNotFound
	}
	return reached, nil
}

func (self *transaction) ListSharedFinanceSpending(viewerUserId, financeAccountId, from, to string) ([]*models.SharedFinanceSpendingRow, error) {
	reached, err := self.reachFinanceAccountAt(viewerUserId, financeAccountId, models.FinanceShareLevelSpending)
	if err != nil {
		return nil, err
	}
	conditions := `"summarized"."finance_account_id" = @finance_account_id AND "summarized"."agent_id" = @agent_id
		AND NOT COALESCE("spending_category"."is_transfer", false) AND "summarized"."duplicate_of_transaction_id" IS NULL`
	arguments := map[string]any{"finance_account_id": reached.Account.ID, "agent_id": reached.Account.AgentID}
	fromDay, err := parseOptionalDay(from)
	if err != nil {
		return nil, err
	}
	toDay, err := parseOptionalDay(to)
	if err != nil {
		return nil, err
	}
	// Months are all the spending level shows, so a range of single days
	// would let a reader tell which day and how much each charge was by
	// moving the ends one day at a time. Below the transactions level the
	// range is widened to whole months.
	if !reached.FinanceShareLevel.AtLeast(models.FinanceShareLevelTransactions) {
		if fromDay != "" {
			fromDay = fromDay[:len("2006-01")] + "-01"
		}
		if toDay != "" {
			parsed, err := time.Parse(time.DateOnly, toDay)
			if err != nil {
				return nil, err
			}
			toDay = parsed.AddDate(0, 1, -parsed.Day()).Format(time.DateOnly)
		}
	}
	if fromDay != "" {
		conditions += ` AND "summarized"."posted_on" >= CAST(@from AS date)`
		arguments["from"] = fromDay
	}
	if toDay != "" {
		conditions += ` AND "summarized"."posted_on" <= CAST(@to AS date)`
		arguments["to"] = toDay
	}
	var rows []struct {
		SpendingMonth           string `gorm:"column:spending_month"`
		SpendingCategoryName    string `gorm:"column:spending_category_name"`
		CurrencyCode            string `gorm:"column:currency_code"`
		MoneyOut                string `gorm:"column:money_out"`
		MoneyIn                 string `gorm:"column:money_in"`
		FinanceTransactionCount int    `gorm:"column:finance_transaction_count"`
	}
	moneyOut := `COALESCE(SUM(-"summarized"."amount") FILTER (WHERE "summarized"."amount" < 0), 0::numeric(19,4))`
	moneyIn := `COALESCE(SUM("summarized"."amount") FILTER (WHERE "summarized"."amount" > 0), 0::numeric(19,4))`
	if err := self.tx.Raw(`SELECT to_char("summarized"."posted_on", 'YYYY-MM') AS "spending_month",
			COALESCE(MAX("spending_category"."spending_category_name"), '') AS "spending_category_name",
			"summarized"."currency_code",
			`+moneyOut+`::text AS "money_out", `+moneyIn+`::text AS "money_in", COUNT(*) AS "finance_transaction_count"
		FROM "agent_finance_transaction" AS "summarized"
		LEFT JOIN "agent_spending_category" AS "spending_category"
		  ON "spending_category"."id" = "summarized"."spending_category_id" AND "spending_category"."agent_id" = "summarized"."agent_id"
		WHERE `+conditions+`
		GROUP BY to_char("summarized"."posted_on", 'YYYY-MM'), COALESCE("summarized"."spending_category_id", ''), "summarized"."currency_code"
		ORDER BY 1 DESC, `+moneyOut+` DESC, 2 ASC, 3 ASC`, arguments).Scan(&rows).Error; err != nil {
		return nil, err
	}
	spending := make([]*models.SharedFinanceSpendingRow, 0, len(rows))
	for _, row := range rows {
		spending = append(spending, &models.SharedFinanceSpendingRow{
			SpendingMonth: row.SpendingMonth, SpendingCategoryName: row.SpendingCategoryName, CurrencyCode: row.CurrencyCode,
			MoneyOut: row.MoneyOut, MoneyIn: row.MoneyIn, FinanceTransactionCount: row.FinanceTransactionCount,
		})
	}
	return spending, nil
}

func (self *transaction) ListSharedFinanceTransactions(viewerUserId, financeAccountId string, filter *SharedFinanceTransactionFilter) (*SharedFinanceTransactionPage, error) {
	reached, err := self.reachFinanceAccountAt(viewerUserId, financeAccountId, models.FinanceShareLevelTransactions)
	if err != nil {
		return nil, err
	}
	if filter == nil {
		filter = &SharedFinanceTransactionFilter{}
	}
	// A mirrored copy is the same charge reported again, left out of every
	// total of the owner's, and so out of this too.
	page, err := self.ListFinanceTransactions(reached.Account.AgentID, &FinanceTransactionFilter{
		FinanceAccountID: reached.Account.ID, From: filter.From, To: filter.To, Limit: filter.Limit, After: filter.After,
		Offset: filter.Offset, IsDuplicateExcluded: true, ShouldCountTotal: true,
	})
	if err != nil {
		return nil, err
	}
	categoryNames := map[string]string{}
	if len(page.FinanceTransactions) > 0 {
		var categories []struct {
			ID                   string `gorm:"column:id"`
			SpendingCategoryName string `gorm:"column:spending_category_name"`
		}
		if err := self.tx.Raw(`SELECT "id", "spending_category_name" FROM "agent_spending_category" WHERE "agent_id" = ?`,
			reached.Account.AgentID).Scan(&categories).Error; err != nil {
			return nil, err
		}
		for _, category := range categories {
			categoryNames[category.ID] = category.SpendingCategoryName
		}
	}
	shared := &SharedFinanceTransactionPage{
		FinanceTransactions: make([]*models.SharedFinanceTransaction, 0, len(page.FinanceTransactions)),
		NextCursor:          page.NextCursor, TotalCount: page.TotalCount,
	}
	for _, found := range page.FinanceTransactions {
		shared.FinanceTransactions = append(shared.FinanceTransactions, &models.SharedFinanceTransaction{
			ID: found.ID, PostedOn: found.PostedOn, Amount: found.Amount, CurrencyCode: found.CurrencyCode,
			MerchantName: found.MerchantName, SpendingCategoryName: categoryNames[found.SpendingCategoryID],
		})
	}
	return shared, nil
}

// endFinanceSharesOutsideCircles deletes the finance shares that only a circle
// made possible once the person no longer has it, in both directions; the
// circle code calls it (endSharesOutsideCircles) in the transaction that takes
// a person out of a circle. The reasoning is that of
// endHealthSharesOutsideCircles: a direct share would reach two people again,
// unchosen, the day they met in another circle, and a share to a circle the
// sharer has left is meaningless. A share to a circle that somebody else left
// stops reaching that person by the join in reachingFinanceSharesQuery and
// needs no deleting.
func endFinanceSharesOutsideCircles(tx *gorm.DB, userId string) error {
	return tx.Exec(`DELETE FROM "finance_account_share" WHERE "id" IN (
		SELECT share.id FROM "finance_account_share" share
		JOIN "agent_finance_account" account ON account.id = share.finance_account_id
		JOIN "agent" owner_agent ON owner_agent.id = account.agent_id
		WHERE share.grantee_user_id IS NOT NULL
		  AND (owner_agent.user_id = @user OR share.grantee_user_id = @user)
		  AND NOT EXISTS (
		      SELECT 1 FROM "circle_member" owner
		      JOIN "circle_member" grantee ON grantee.circle_id = owner.circle_id
		      WHERE owner.user_id = owner_agent.user_id AND grantee.user_id = share.grantee_user_id)
		UNION
		SELECT share.id FROM "finance_account_share" share
		JOIN "agent_finance_account" account ON account.id = share.finance_account_id
		JOIN "agent" owner_agent ON owner_agent.id = account.agent_id
		WHERE share.grantee_circle_id IS NOT NULL
		  AND owner_agent.user_id = @user
		  AND NOT EXISTS (
		      SELECT 1 FROM "circle_member" member
		      WHERE member.circle_id = share.grantee_circle_id AND member.user_id = @user))`,
		sql.Named("user", userId)).Error
}
