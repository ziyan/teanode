package db

import (
	"database/sql"
	"sort"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

// AllFinanceShareOperation is a person's whole finance shared with another
// person or with a circle at a level (spending or transactions): every finance
// account the owner holds now and every one they link later, read together.
// It sits beside FinanceShareOperation, which is of one account at a time, and
// follows its rules: the same conditions decide who is reached
// (financeShareReachesViewerSQL), the same circles make a share possible, and
// the reads return types that have no field for an account's name, mask or
// balance, so that what a whole-finance share leaves out is not something a
// caller has to remember to apply.
type AllFinanceShareOperation interface {
	// ShareAllFinance gives a person or a circle a level of access to all of
	// the sharer's finance, or changes the level they have. ErrNotFound unless
	// the sharer has an agent and, for a person, the two share a circle, or,
	// for a circle, the sharer is in it. ErrInvalidArguments for a level that
	// cannot be given, a grantee that is not exactly one, or the sharer
	// themselves.
	ShareAllFinance(ownerUserId string, share *models.AllFinanceShare) (*models.AllFinanceShare, error)

	// UnshareAllFinance takes one of the owner's whole-finance shares back.
	// ErrNotFound when the owner has no such share.
	UnshareAllFinance(ownerUserId, shareId string) error

	// ListAllFinanceSharesBy are the whole-finance shares the person has made,
	// oldest first, with who each is for.
	ListAllFinanceSharesBy(ownerUserId string) ([]*models.AllFinanceShare, error)

	// ListAllFinanceSharesReaching are the whole-finance shares that reach the
	// person now, oldest first, each with whose finance it is. A share to a
	// person reaches them only while the two still share a circle, and a share
	// to a circle only while both are in it. Not the person's own.
	ListAllFinanceSharesReaching(viewerUserId string) ([]*models.AllFinanceShare, error)

	// ReachAllFinance is the finance of another person as the viewer reaches
	// it as a whole, at the highest level of the shares that reach them, or nil
	// when none does (or the person who owns it, or they, can no longer use
	// finance).
	ReachAllFinance(viewerUserId, ownerUserId string) (*models.ReachedAllFinance, error)

	// ListReachedAllFinance are the people whose whole finance reaches the
	// viewer, each once at the highest level, by username.
	ListReachedAllFinance(viewerUserId string) ([]*models.ReachedAllFinance, error)

	// ListSharedAllFinanceSpending is the owner's money out and in by month
	// and spending category across all their finance accounts, newest month
	// first, in each currency, for the days from and to ("2006-01-02", either
	// may be empty). ErrNotFound unless the owner's whole finance reaches the
	// viewer at the spending level or better. A finance account shared on its
	// own at a higher level is not read here: this is the level of the share
	// of the whole.
	ListSharedAllFinanceSpending(viewerUserId, ownerUserId, from, to string) ([]*models.SharedFinanceSpendingRow, error)

	// ListSharedAllFinanceTransactions is a page of the transactions of all
	// the owner's finance accounts together, newest first. ErrNotFound unless
	// the owner's whole finance reaches the viewer at the transactions level.
	ListSharedAllFinanceTransactions(viewerUserId, ownerUserId string, filter *SharedFinanceTransactionFilter) (*SharedFinanceTransactionPage, error)
}

type financeShareModel struct {
	ID                string    `gorm:"column:id;primaryKey"`
	OwnerUserID       string    `gorm:"column:owner_user_id"`
	GranteeUserID     *string   `gorm:"column:grantee_user_id"`
	GranteeCircleID   *string   `gorm:"column:grantee_circle_id"`
	FinanceShareLevel string    `gorm:"column:finance_share_level"`
	CreatedAt         time.Time `gorm:"column:created_at"`
	UpdatedAt         time.Time `gorm:"column:updated_at"`
	CreatedByUserID   *string   `gorm:"column:created_by_user_id"`
}

func (financeShareModel) TableName() string { return "finance_share" }

func (self *financeShareModel) toModel() *models.AllFinanceShare {
	share := &models.AllFinanceShare{
		ID:                self.ID,
		OwnerUserID:       self.OwnerUserID,
		FinanceShareLevel: models.FinanceShareLevel(self.FinanceShareLevel),
		CreatedAt:         self.CreatedAt.In(time.Local),
		UpdatedAt:         self.UpdatedAt.In(time.Local),
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

func (self *transaction) ShareAllFinance(ownerUserId string, share *models.AllFinanceShare) (*models.AllFinanceShare, error) {
	if share == nil || ownerUserId == "" || !share.FinanceShareLevel.IsGrantableForAllAccounts() ||
		(share.GranteeUserID == "") == (share.GranteeCircleID == "") || share.GranteeUserID == ownerUserId {
		return nil, ErrInvalidArguments
	}
	// Only a person whose agent holds finance shares it. The agent row is held
	// for the rest of the transaction, so that deleting the person waits for
	// this and their share goes with them.
	var agentIds []string
	if err := self.tx.Raw(`SELECT "id" FROM "agent" WHERE "user_id" = ? FOR SHARE`, ownerUserId).Scan(&agentIds).Error; err != nil {
		return nil, err
	}
	if len(agentIds) == 0 {
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
		isMutual, err := self.holdMutualCircle(ownerUserId, share.GranteeUserID)
		if err != nil {
			return nil, err
		}
		if !isMutual {
			return nil, ErrNotFound
		}
	} else {
		var memberRows []circleMemberModel
		if err := self.tx.Clauses(clause.Locking{Strength: "SHARE"}).
			Where("\"circle_id\" = ? AND \"user_id\" = ?", share.GranteeCircleID, ownerUserId).Find(&memberRows).Error; err != nil {
			return nil, err
		}
		if len(memberRows) == 0 {
			return nil, ErrNotFound
		}
	}

	now := time.Now()
	row := &financeShareModel{
		ID:                newID(),
		OwnerUserID:       ownerUserId,
		GranteeUserID:     nullableText(share.GranteeUserID),
		GranteeCircleID:   nullableText(share.GranteeCircleID),
		FinanceShareLevel: string(share.FinanceShareLevel),
		CreatedAt:         now,
		UpdatedAt:         now,
		CreatedByUserID:   &ownerUserId,
	}
	conflict := clause.OnConflict{DoUpdates: clause.AssignmentColumns([]string{"finance_share_level", "updated_at"})}
	query := self.tx.Where("\"owner_user_id\" = ?", ownerUserId)
	if share.GranteeUserID != "" {
		conflict.Columns = []clause.Column{{Name: "owner_user_id"}, {Name: "grantee_user_id"}}
		query = query.Where("\"grantee_user_id\" = ?", share.GranteeUserID)
	} else {
		conflict.Columns = []clause.Column{{Name: "owner_user_id"}, {Name: "grantee_circle_id"}}
		query = query.Where("\"grantee_circle_id\" = ?", share.GranteeCircleID)
	}
	if err := self.tx.Clauses(conflict).Create(row).Error; err != nil {
		return nil, err
	}
	// Read back, because an upsert that changed an existing share keeps its
	// identifier and its date.
	var kept financeShareModel
	if err := query.Take(&kept).Error; err != nil {
		return nil, err
	}
	return kept.toModel(), nil
}

func (self *transaction) UnshareAllFinance(ownerUserId, shareId string) error {
	if ownerUserId == "" || shareId == "" {
		return ErrNotFound
	}
	result := self.tx.Where("\"id\" = ? AND \"owner_user_id\" = ?", shareId, ownerUserId).Delete(&financeShareModel{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// nameAllFinanceShares fills in the names the listings show: whose finance it
// is and who it is for.
func nameAllFinanceShares(tx *gorm.DB, shares []*models.AllFinanceShare) error {
	userIds, circleIds := []string{}, []string{}
	for _, share := range shares {
		userIds = append(userIds, share.OwnerUserID)
		if share.GranteeUserID != "" {
			userIds = append(userIds, share.GranteeUserID)
		}
		if share.GranteeCircleID != "" {
			circleIds = append(circleIds, share.GranteeCircleID)
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
		if user := users[share.OwnerUserID]; user != nil {
			share.OwnerUsername, share.OwnerName, share.OwnerUser = user.Username, user.DisplayName(), user
		}
		if user := users[share.GranteeUserID]; user != nil {
			share.GranteeUsername, share.GranteeName = user.Username, user.DisplayName()
		}
		share.GranteeCircleName = circleNames[share.GranteeCircleID]
	}
	return nil
}

func (self *transaction) ListAllFinanceSharesBy(ownerUserId string) ([]*models.AllFinanceShare, error) {
	if ownerUserId == "" {
		return []*models.AllFinanceShare{}, nil
	}
	var rows []financeShareModel
	if err := self.tx.Where("\"owner_user_id\" = ?", ownerUserId).Order("\"created_at\" ASC, \"id\" ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	shares := make([]*models.AllFinanceShare, 0, len(rows))
	for index := range rows {
		shares = append(shares, rows[index].toModel())
	}
	if err := nameAllFinanceShares(self.tx, shares); err != nil {
		return nil, err
	}
	return shares, nil
}

// reachingAllFinanceSharesQuery selects the whole-finance shares that reach one
// viewer. It is reachingFinanceSharesQuery for shares with no account: the same
// conditions on the owner and the viewer, and on the agent, which must be on and
// not disabled by the operator, because what an agent that is off holds is not
// read by anybody.
var reachingAllFinanceSharesQuery = `
SELECT share.id AS share_id, share.owner_user_id AS owner_user_id, owner_agent.id AS agent_id,
       share.finance_share_level AS finance_share_level
FROM "finance_share" share
JOIN "agent" owner_agent ON owner_agent.user_id = share.owner_user_id AND owner_agent.enabled AND owner_agent.operator_disabled_at IS NULL
JOIN "user" owner_user ON owner_user.id = share.owner_user_id AND owner_user.disabled_at IS NULL
WHERE ` + financeShareReachesViewerSQL("share.owner_user_id")

type reachingAllFinanceShareRow struct {
	ShareID           string `gorm:"column:share_id"`
	OwnerUserID       string `gorm:"column:owner_user_id"`
	AgentID           string `gorm:"column:agent_id"`
	FinanceShareLevel string `gorm:"column:finance_share_level"`
}

// allReachingAllFinanceShares are every whole-finance share that reaches the
// viewer, of one owner or, with an empty identifier, of all of them.
func (self *transaction) allReachingAllFinanceShares(viewerUserId, ownerUserId string) ([]reachingAllFinanceShareRow, error) {
	if viewerUserId == "" {
		return nil, nil
	}
	var rows []reachingAllFinanceShareRow
	if err := self.tx.Raw(reachingAllFinanceSharesQuery,
		sql.Named("viewer", viewerUserId), sql.Named("owner", ownerUserId),
		sql.Named("permission", string(models.PermissionAgentUse))).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// highestAllFinanceLevels is the highest level that reaches the viewer from
// each owner.
func highestAllFinanceLevels(rows []reachingAllFinanceShareRow) map[string]models.FinanceShareLevel {
	levels := map[string]models.FinanceShareLevel{}
	for _, row := range rows {
		levels[row.OwnerUserID] = models.HighestFinanceShareLevel(levels[row.OwnerUserID], models.FinanceShareLevel(row.FinanceShareLevel))
	}
	return levels
}

func (self *transaction) ListAllFinanceSharesReaching(viewerUserId string) ([]*models.AllFinanceShare, error) {
	reaching, err := self.allReachingAllFinanceShares(viewerUserId, "")
	if err != nil {
		return nil, err
	}
	if len(reaching) == 0 {
		return []*models.AllFinanceShare{}, nil
	}
	shareIds := make([]string, 0, len(reaching))
	for _, row := range reaching {
		shareIds = append(shareIds, row.ShareID)
	}
	var rows []financeShareModel
	if err := self.tx.Where("\"id\" IN ?", shareIds).Order("\"created_at\" ASC, \"id\" ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	shares := make([]*models.AllFinanceShare, 0, len(rows))
	for index := range rows {
		shares = append(shares, rows[index].toModel())
	}
	if err := nameAllFinanceShares(self.tx, shares); err != nil {
		return nil, err
	}
	return shares, nil
}

func (self *transaction) ListReachedAllFinance(viewerUserId string) ([]*models.ReachedAllFinance, error) {
	reaching, err := self.allReachingAllFinanceShares(viewerUserId, "")
	if err != nil {
		return nil, err
	}
	return self.reachedAllFinance(reaching)
}

// reachedAllFinance makes one entry for each owner of the reaching shares, at
// the highest level of theirs, by the owner's username.
func (self *transaction) reachedAllFinance(reaching []reachingAllFinanceShareRow) ([]*models.ReachedAllFinance, error) {
	if len(reaching) == 0 {
		return []*models.ReachedAllFinance{}, nil
	}
	levels := highestAllFinanceLevels(reaching)
	ownerIds := make([]string, 0, len(levels))
	for ownerId := range levels {
		ownerIds = append(ownerIds, ownerId)
	}
	owners, err := loadUsersById(self.tx, ownerIds)
	if err != nil {
		return nil, err
	}
	reached := make([]*models.ReachedAllFinance, 0, len(levels))
	for _, ownerId := range ownerIds {
		entry := &models.ReachedAllFinance{OwnerUserID: ownerId, FinanceShareLevel: levels[ownerId]}
		if owner := owners[ownerId]; owner != nil {
			entry.OwnerUsername, entry.OwnerName = owner.Username, owner.DisplayName()
		}
		reached = append(reached, entry)
	}
	sort.Slice(reached, func(first, second int) bool {
		if reached[first].OwnerUsername != reached[second].OwnerUsername {
			return reached[first].OwnerUsername < reached[second].OwnerUsername
		}
		return reached[first].OwnerUserID < reached[second].OwnerUserID
	})
	return reached, nil
}

func (self *transaction) ReachAllFinance(viewerUserId, ownerUserId string) (*models.ReachedAllFinance, error) {
	if ownerUserId == "" {
		return nil, nil
	}
	reaching, err := self.allReachingAllFinanceShares(viewerUserId, ownerUserId)
	if err != nil {
		return nil, err
	}
	reached, err := self.reachedAllFinance(reaching)
	if err != nil || len(reached) == 0 {
		return nil, err
	}
	return reached[0], nil
}

// reachAllFinanceAt is the owner's whole finance if it reaches the viewer at the
// level or better, with the identifier of the owner's agent, and ErrNotFound
// otherwise, which is also what a lower level answers: whether a level above
// exists is not the reader's business.
func (self *transaction) reachAllFinanceAt(viewerUserId, ownerUserId string, level models.FinanceShareLevel) (*models.ReachedAllFinance, string, error) {
	if ownerUserId == "" {
		return nil, "", ErrNotFound
	}
	reaching, err := self.allReachingAllFinanceShares(viewerUserId, ownerUserId)
	if err != nil {
		return nil, "", err
	}
	reached, err := self.reachedAllFinance(reaching)
	if err != nil {
		return nil, "", err
	}
	if len(reached) == 0 || !reached[0].FinanceShareLevel.AtLeast(level) {
		return nil, "", ErrNotFound
	}
	return reached[0], reaching[0].AgentID, nil
}

func (self *transaction) ListSharedAllFinanceSpending(viewerUserId, ownerUserId, from, to string) ([]*models.SharedFinanceSpendingRow, error) {
	reached, agentId, err := self.reachAllFinanceAt(viewerUserId, ownerUserId, models.FinanceShareLevelSpending)
	if err != nil {
		return nil, err
	}
	return self.sharedFinanceSpending(agentId, "", reached.FinanceShareLevel, from, to)
}

func (self *transaction) ListSharedAllFinanceTransactions(viewerUserId, ownerUserId string, filter *SharedFinanceTransactionFilter) (*SharedFinanceTransactionPage, error) {
	_, agentId, err := self.reachAllFinanceAt(viewerUserId, ownerUserId, models.FinanceShareLevelTransactions)
	if err != nil {
		return nil, err
	}
	return self.sharedFinanceTransactions(agentId, "", filter)
}

// endAllFinanceSharesOutsideCircles is endFinanceSharesOutsideCircles for the
// shares of all of a person's finance, for the same reasons.
func endAllFinanceSharesOutsideCircles(tx *gorm.DB, userId string) error {
	return tx.Exec(`DELETE FROM "finance_share" WHERE "id" IN (
		SELECT share.id FROM "finance_share" share
		WHERE share.grantee_user_id IS NOT NULL
		  AND (share.owner_user_id = @user OR share.grantee_user_id = @user)
		  AND NOT EXISTS (
		      SELECT 1 FROM "circle_member" owner
		      JOIN "circle_member" grantee ON grantee.circle_id = owner.circle_id
		      WHERE owner.user_id = share.owner_user_id AND grantee.user_id = share.grantee_user_id)
		UNION
		SELECT share.id FROM "finance_share" share
		WHERE share.grantee_circle_id IS NOT NULL
		  AND share.owner_user_id = @user
		  AND NOT EXISTS (
		      SELECT 1 FROM "circle_member" member
		      WHERE member.circle_id = share.grantee_circle_id AND member.user_id = @user))`,
		sql.Named("user", userId)).Error
}
