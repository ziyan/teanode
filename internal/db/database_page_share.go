package db

import (
	"database/sql"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lib/pq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

// PageShareOperation is who may read which of a person's pages: a page of the
// agent's graph, and everything under it, shared with another person or with
// a circle.
//
// This is only the table and the questions about it. Reading what a share
// reaches is internal/agent/shared_memory.go, which is the one place another
// agent's graph is read, and which asks ListPageSharesReaching what it may
// read before it does.
type PageShareOperation interface {
	// SharePage shares the sharer's own page, and everything under it, with a
	// person or a circle, or changes whether the quotes are shared when it is
	// shared with them already. ErrNotFound unless there is a page at the
	// path in the sharer's graph and, for a person, the two share a circle,
	// or, for a circle, the sharer is in it. ErrInvalidArguments for a path
	// that is not one or a grantee that is not exactly one.
	SharePage(sharerUserId string, share *models.AgentPageShare) (*models.AgentPageShare, error)

	// GetPageShare is one share, or nil.
	GetPageShare(shareId string) (*models.AgentPageShare, error)

	// ListPageSharesBy are the shares the person made of their own pages,
	// by path, with who each is for.
	ListPageSharesBy(ownerUserId string) ([]*models.AgentPageShare, error)

	// DeletePageShare takes one back. ErrNotFound when there is none.
	DeletePageShare(shareId string) error

	// ListPageSharesReaching are the shares that reach the person as a
	// reader: made to them, or to a circle they are in, by somebody who
	// still shares that circle or a circle with them, with the owner named.
	// By owner name and then path.
	ListPageSharesReaching(readerUserId string) ([]*models.AgentPageShare, error)

	// SearchAgentGraphWithin finds pages and facts by words in the graph of
	// one agent, only on the pages at these roots or under them. A page is
	// found by its path, name, aliases and summary, except that the pages
	// named in hiddenSummaryNodeIds are found by their path, name and
	// aliases alone: their summary is not shown to the reader, so a word on
	// it must not find them. The shared-memory reader is the only caller.
	SearchAgentGraphWithin(agentId string, roots, hiddenSummaryNodeIds []string, query string, limit int) ([]*models.AgentNode, []*models.AgentFact, error)

	// ListAgentNodesWithQuoteMarksWithin are the pages at these roots or
	// under them whose summary has a quotation mark in it, which are the only
	// ones whose summary may be left out of what a reader is shown.
	ListAgentNodesWithQuoteMarksWithin(agentId string, roots []string) ([]*models.AgentNode, error)

	// ListAgentFactsWithin are the live facts on the pages at these roots or
	// under them, by identifier, at most limit of those after afterFactId
	// (empty to start). The shared-memory reader counts what a share would
	// keep from its reader with it, a batch at a time.
	ListAgentFactsWithin(agentId string, roots []string, afterFactId string, limit int) ([]*models.AgentFact, error)

	// ListOwnCalendarObjectsByID are the objects with these identifiers in
	// the calendars the person owns, in one read. An object of a circle's
	// calendar, or one that is gone, is not among them. The shared-memory
	// reader classifies the events a fact cites with it.
	ListOwnCalendarObjectsByID(userId string, objectIds []string) ([]*models.CalendarObject, error)
}

type agentPageShareModel struct {
	ID              string    `gorm:"column:id;primaryKey"`
	AgentID         string    `gorm:"column:agent_id"`
	Path            string    `gorm:"column:path"`
	GranteeUserID   *string   `gorm:"column:grantee_user_id"`
	GranteeCircleID *string   `gorm:"column:grantee_circle_id"`
	IsQuoteShared   bool      `gorm:"column:is_quote_shared"`
	CreatedAt       time.Time `gorm:"column:created_at"`
	CreatedByUserID *string   `gorm:"column:created_by_user_id"`
}

func (agentPageShareModel) TableName() string { return "agent_page_share" }

func (self *agentPageShareModel) toModel() *models.AgentPageShare {
	share := &models.AgentPageShare{
		ID:            self.ID,
		AgentID:       self.AgentID,
		Path:          self.Path,
		IsQuoteShared: self.IsQuoteShared,
		CreatedAt:     self.CreatedAt.In(time.Local),
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

// describePageShares names who owns each share and who it is for. One read
// of the owners' agents, one of the accounts and one of the circles, however
// many shares there are.
func (self *transaction) describePageShares(rows []agentPageShareModel) ([]*models.AgentPageShare, error) {
	agentIds, userIds, circleIds := []string{}, []string{}, []string{}
	for _, row := range rows {
		agentIds = append(agentIds, row.AgentID)
		if row.GranteeUserID != nil {
			userIds = append(userIds, *row.GranteeUserID)
		}
		if row.GranteeCircleID != nil {
			circleIds = append(circleIds, *row.GranteeCircleID)
		}
	}
	ownerOfAgent := map[string]string{}
	if len(agentIds) > 0 {
		var owners []struct {
			ID     string `gorm:"column:id"`
			UserID string `gorm:"column:user_id"`
		}
		if err := self.tx.Table("agent").Select("id, user_id").Where("id IN ?", uniqueStrings(agentIds)).Scan(&owners).Error; err != nil {
			return nil, err
		}
		for _, owner := range owners {
			ownerOfAgent[owner.ID] = owner.UserID
			userIds = append(userIds, owner.UserID)
		}
	}
	users, err := loadUsersById(self.tx, userIds)
	if err != nil {
		return nil, err
	}
	circleNames := map[string]string{}
	if len(circleIds) > 0 {
		var circles []circleModel
		if err := self.tx.Where("\"id\" IN ?", uniqueStrings(circleIds)).Find(&circles).Error; err != nil {
			return nil, err
		}
		for _, circle := range circles {
			circleNames[circle.ID] = circle.Name
		}
	}
	shares := make([]*models.AgentPageShare, 0, len(rows))
	for index := range rows {
		share := rows[index].toModel()
		share.OwnerUserID = ownerOfAgent[share.AgentID]
		if owner := users[share.OwnerUserID]; owner != nil {
			share.OwnerUsername, share.OwnerName, share.OwnerUser = owner.Username, owner.DisplayName(), owner
		}
		if grantee := users[share.GranteeUserID]; grantee != nil {
			share.GranteeUsername, share.GranteeName, share.GranteeUser = grantee.Username, grantee.DisplayName(), grantee
		}
		share.GranteeCircleName = circleNames[share.GranteeCircleID]
		shares = append(shares, share)
	}
	return shares, nil
}

func (self *transaction) SharePage(sharerUserId string, share *models.AgentPageShare) (*models.AgentPageShare, error) {
	if share == nil || sharerUserId == "" || (share.GranteeUserID == "") == (share.GranteeCircleID == "") {
		return nil, ErrInvalidArguments
	}
	path := models.NormalizePath(share.Path)
	if models.ValidPath(path) != nil {
		return nil, ErrInvalidArguments
	}
	owner, err := self.GetAgentByUser(sharerUserId)
	if err != nil {
		return nil, err
	}
	if owner == nil {
		return nil, ErrNotFound
	}
	// A page the sharer has, because a share of nothing would reach whatever
	// is filed there later without the owner having looked at it again.
	node, err := self.GetAgentNode(owner.ID, path)
	if err != nil {
		return nil, err
	}
	if node == nil {
		return nil, ErrNotFound
	}
	if share.GranteeUserID != "" {
		if share.GranteeUserID == sharerUserId {
			return nil, ErrInvalidArguments
		}
		var count int64
		if err := self.tx.Model(&userModel{}).Where("\"id\" = ?", share.GranteeUserID).Count(&count).Error; err != nil {
			return nil, err
		}
		if count == 0 {
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
		var rows []circleMemberModel
		if err := self.tx.Clauses(clause.Locking{Strength: "SHARE"}).
			Where("\"circle_id\" = ? AND \"user_id\" = ?", share.GranteeCircleID, sharerUserId).Find(&rows).Error; err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			return nil, ErrNotFound
		}
	}

	row := &agentPageShareModel{
		ID:              newID(),
		AgentID:         owner.ID,
		Path:            node.Path,
		GranteeUserID:   nullableText(share.GranteeUserID),
		GranteeCircleID: nullableText(share.GranteeCircleID),
		IsQuoteShared:   share.IsQuoteShared,
		CreatedAt:       time.Now(),
		CreatedByUserID: &sharerUserId,
	}
	conflict := clause.OnConflict{DoUpdates: clause.AssignmentColumns([]string{"is_quote_shared"})}
	if share.GranteeUserID != "" {
		conflict.Columns = []clause.Column{{Name: "agent_id"}, {Name: "path"}, {Name: "grantee_user_id"}}
	} else {
		conflict.Columns = []clause.Column{{Name: "agent_id"}, {Name: "path"}, {Name: "grantee_circle_id"}}
	}
	if err := self.tx.Clauses(conflict).Create(row).Error; err != nil {
		return nil, err
	}
	// Read back, because an upsert that changed an existing share keeps its
	// identifier and its date.
	query := self.tx.Where("\"agent_id\" = ? AND \"path\" = ?", owner.ID, node.Path)
	if share.GranteeUserID != "" {
		query = query.Where("\"grantee_user_id\" = ?", share.GranteeUserID)
	} else {
		query = query.Where("\"grantee_circle_id\" = ?", share.GranteeCircleID)
	}
	var kept agentPageShareModel
	if err := query.Take(&kept).Error; err != nil {
		return nil, err
	}
	described, err := self.describePageShares([]agentPageShareModel{kept})
	if err != nil {
		return nil, err
	}
	return described[0], nil
}

func (self *transaction) GetPageShare(shareId string) (*models.AgentPageShare, error) {
	if shareId == "" {
		return nil, nil
	}
	var found []agentPageShareModel
	if err := self.tx.Where("\"id\" = ?", shareId).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	described, err := self.describePageShares(found)
	if err != nil {
		return nil, err
	}
	return described[0], nil
}

func (self *transaction) ListPageSharesBy(ownerUserId string) ([]*models.AgentPageShare, error) {
	var rows []agentPageShareModel
	if err := self.tx.Table("agent_page_share AS share").
		Select("share.*").
		Joins("JOIN agent owner ON owner.id = share.agent_id").
		Where("owner.user_id = ?", ownerUserId).
		Order("share.path ASC, share.created_at ASC, share.id ASC").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	return self.describePageShares(rows)
}

func (self *transaction) DeletePageShare(shareId string) error {
	result := self.tx.Where("\"id\" = ?", shareId).Delete(&agentPageShareModel{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// reachingPageSharesQuery selects the shares that reach one reader. The
// joins on circle_member are what make a share stop reaching someone the
// moment the circle it depended on is gone: a share to a person needs the two
// to share a circle, and a share to a circle needs both the reader and the
// owner to be in it. The join on the owner's account is what stops a disabled
// account's pages reaching anybody: it cannot sign in, and what it shared is
// not read in its name while it is off. It is reachingLocationSharesQuery for
// pages in the rest: the reader's account must not be disabled either, and
// the owner and the reader both need agent:use (@agentPermission) and
// circle:use (@circlePermission), so that an operator who takes either from a
// role has switched sharing pages off for those people and what they shared
// is no longer read by others.
var reachingPageSharesQuery = `
SELECT share.*
FROM "agent_page_share" share
JOIN "agent" owner ON owner.id = share.agent_id
JOIN "user" owner_user ON owner_user.id = owner.user_id AND owner_user.disabled_at IS NULL
WHERE owner.user_id <> @reader
  AND ` + userHasNamedPermissionSQL("owner.user_id", "agentPermission") + `
  AND ` + userHasNamedPermissionSQL("owner.user_id", "circlePermission") + `
  AND EXISTS (SELECT 1 FROM "user" reader_user WHERE reader_user.id = @reader AND reader_user.disabled_at IS NULL)
  AND ` + userHasNamedPermissionSQL("@reader", "agentPermission") + `
  AND ` + userHasNamedPermissionSQL("@reader", "circlePermission") + `
  AND (
    (share.grantee_user_id = @reader AND EXISTS (
        SELECT 1 FROM "circle_member" mine
        JOIN "circle_member" theirs ON theirs.circle_id = mine.circle_id
        WHERE mine.user_id = @reader AND theirs.user_id = owner.user_id))
    OR
    (share.grantee_circle_id IS NOT NULL
     AND EXISTS (SELECT 1 FROM "circle_member" WHERE circle_id = share.grantee_circle_id AND user_id = @reader)
     AND EXISTS (SELECT 1 FROM "circle_member" WHERE circle_id = share.grantee_circle_id AND user_id = owner.user_id))
  )`

func (self *transaction) ListPageSharesReaching(readerUserId string) ([]*models.AgentPageShare, error) {
	if readerUserId == "" {
		return []*models.AgentPageShare{}, nil
	}
	var rows []agentPageShareModel
	if err := self.tx.Raw(reachingPageSharesQuery, sql.Named("reader", readerUserId),
		sql.Named("agentPermission", string(models.PermissionAgentUse)),
		sql.Named("circlePermission", string(models.PermissionCircleUse))).Scan(&rows).Error; err != nil {
		return nil, err
	}
	shares, err := self.describePageShares(rows)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(shares, func(first, second int) bool {
		left, right := shares[first], shares[second]
		if leftName, rightName := strings.ToLower(left.OwnerName), strings.ToLower(right.OwnerName); leftName != rightName {
			return leftName < rightName
		}
		if left.OwnerUserID != right.OwnerUserID {
			return left.OwnerUserID < right.OwnerUserID
		}
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		return left.ID < right.ID
	})
	return shares, nil
}

// endPageSharesOutsideCircles deletes the shares that only a circle made
// possible once the person no longer has it, in both directions; the circle
// code calls it (endSharesOutsideCircles) in the transaction that takes a
// person out of a circle.
//
// Two kinds of share are left over when somebody leaves a circle. A direct
// share between two people made possible by the circle they shared, which
// would reach them again, unchosen, the day they met in another circle. And a
// share the person made to a circle they are no longer in. A share to a circle
// that somebody else left stops reaching that person by the join in
// reachingPageSharesQuery, and needs no deleting.
func endPageSharesOutsideCircles(tx *gorm.DB, userId string) error {
	return tx.Exec(`DELETE FROM "agent_page_share" WHERE "id" IN (
		SELECT share.id FROM "agent_page_share" share
		JOIN "agent" owner ON owner.id = share.agent_id
		WHERE share.grantee_user_id IS NOT NULL
		  AND (owner.user_id = @user OR share.grantee_user_id = @user)
		  AND NOT EXISTS (
		      SELECT 1 FROM "circle_member" mine
		      JOIN "circle_member" theirs ON theirs.circle_id = mine.circle_id
		      WHERE mine.user_id = owner.user_id AND theirs.user_id = share.grantee_user_id)
		UNION
		SELECT share.id FROM "agent_page_share" share
		JOIN "agent" owner ON owner.id = share.agent_id
		WHERE share.grantee_circle_id IS NOT NULL
		  AND owner.user_id = @user
		  AND NOT EXISTS (
		      SELECT 1 FROM "circle_member" member
		      WHERE member.circle_id = share.grantee_circle_id AND member.user_id = @user))`,
		sql.Named("user", userId)).Error
}

// withinRootsCondition is the SQL that keeps a row whose path is one of the
// roots or lies under one, at a slash boundary: "people/a" keeps people/a and
// people/a/x and not people/ab.
const withinRootsCondition = `EXISTS (SELECT 1 FROM unnest(?::text[]) AS root
	WHERE node."path" = root OR starts_with(node."path", root || '/'))`

func (self *transaction) SearchAgentGraphWithin(agentId string, roots, hiddenSummaryNodeIds []string, query string, limit int) ([]*models.AgentNode, []*models.AgentFact, error) {
	if strings.TrimSpace(query) == "" || len(roots) == 0 {
		return nil, nil, nil
	}
	if limit <= 0 {
		limit = 20
	}
	if hiddenSummaryNodeIds == nil {
		hiddenSummaryNodeIds = []string{}
	}
	query = SearchText(query)
	var nodeIds []string
	// What a page is found by is the text of its row less the summary when
	// the summary is not shown, and it is ranked by that text too: the order
	// of the answers would otherwise say how often a word is on a summary the
	// reader cannot read.
	if err := self.tx.Raw(
		`SELECT node."id" FROM "agent_node" node,
		   LATERAL (SELECT CASE WHEN node."id" = ANY(?::text[])
		     THEN to_tsvector('simple', node."path" || ' ' || node."name" || ' ' ||
		       COALESCE((SELECT string_agg("alias", ' ') FROM jsonb_array_elements_text(node."aliases") AS "alias"), ''))
		     ELSE node."search" END AS "shown") visible
		 WHERE node."agent_id" = ? AND visible."shown" @@ `+AnyWord+` AND `+withinRootsCondition+`
		 ORDER BY ts_rank(visible."shown", `+AnyWord+`) DESC, node."importance" DESC, node."id" ASC LIMIT ?`,
		pq.Array(hiddenSummaryNodeIds), agentId, query, pq.Array(roots), query, limit).Scan(&nodeIds).Error; err != nil {
		return nil, nil, err
	}
	var factIds []string
	if err := self.tx.Raw(
		`SELECT fact."id" FROM "agent_fact" fact
		 JOIN "agent_node" node ON node."id" = fact."node_id"
		 WHERE fact."agent_id" = ? AND NOT fact."dormant" AND fact."superseded_by" IS NULL
		   AND fact."search" @@ `+AnyWord+` AND `+withinRootsCondition+`
		 ORDER BY ts_rank(fact."search", `+AnyWord+`) DESC, fact."modified_at" DESC, fact."id" ASC LIMIT ?`,
		agentId, query, pq.Array(roots), query, limit).Scan(&factIds).Error; err != nil {
		return nil, nil, err
	}
	nodes, err := self.GetAgentNodes(agentId, nodeIds)
	if err != nil {
		return nil, nil, err
	}
	facts, err := self.GetAgentFacts(agentId, factIds)
	if err != nil {
		return nil, nil, err
	}
	return orderNodesBy(nodes, nodeIds), orderFactsBy(facts, factIds), nil
}

func (self *transaction) ListAgentNodesWithQuoteMarksWithin(agentId string, roots []string) ([]*models.AgentNode, error) {
	if len(roots) == 0 {
		return nil, nil
	}
	var nodeIds []string
	if err := self.tx.Raw(
		`SELECT node."id" FROM "agent_node" node
		 WHERE node."agent_id" = ? AND node."summary" ~ ? AND `+withinRootsCondition+`
		 ORDER BY node."id" ASC`,
		agentId, "[\"'“”‘’«»「」『』]", pq.Array(roots)).Scan(&nodeIds).Error; err != nil {
		return nil, err
	}
	nodes, err := self.GetAgentNodes(agentId, nodeIds)
	if err != nil {
		return nil, err
	}
	return orderNodesBy(nodes, nodeIds), nil
}

func (self *transaction) ListAgentFactsWithin(agentId string, roots []string, afterFactId string, limit int) ([]*models.AgentFact, error) {
	if len(roots) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 500
	}
	var factIds []string
	if err := self.tx.Raw(
		`SELECT fact."id" FROM "agent_fact" fact
		 JOIN "agent_node" node ON node."id" = fact."node_id"
		 WHERE fact."agent_id" = ? AND NOT fact."dormant" AND fact."superseded_by" IS NULL
		   AND fact."id" > ? AND `+withinRootsCondition+`
		 ORDER BY fact."id" ASC LIMIT ?`,
		agentId, afterFactId, pq.Array(roots), limit).Scan(&factIds).Error; err != nil {
		return nil, err
	}
	facts, err := self.GetAgentFacts(agentId, factIds)
	if err != nil {
		return nil, err
	}
	return orderFactsBy(facts, factIds), nil
}

func (self *transaction) ListOwnCalendarObjectsByID(userId string, objectIds []string) ([]*models.CalendarObject, error) {
	if len(objectIds) == 0 {
		return nil, nil
	}
	var found []calendarObjectModel
	if err := self.tx.Where(`"calendar_id" IN (SELECT "id" FROM "calendar" WHERE "user_id" = ?) AND "id" IN ?`, userId, objectIds).
		Order(`"calendar_id" ASC, "id" ASC`).Find(&found).Error; err != nil {
		return nil, err
	}
	objects := make([]*models.CalendarObject, 0, len(found))
	for index := range found {
		objects = append(objects, found[index].toModel())
	}
	return objects, nil
}

// endPageSharesUnder ends the shares of the agent's pages at this path and
// under it. A share is a path and not a reference to a page, so the code that
// removes pages calls this: a share left behind would reach whatever page is
// made at that path next.
func (self *transaction) endPageSharesUnder(agentId, path string) error {
	return self.tx.Exec(`DELETE FROM "agent_page_share" WHERE "agent_id" = ? AND ("path" = ? OR "path" LIKE ?)`,
		agentId, path, likeEscaped(path)+"/%").Error
}

// movePageShares moves the shares of the page at path, and of the pages under
// it, to where the page and they have been moved to, so that a share follows
// the page it was made of. Whatever was shared at the target before is ended
// first: it belonged to a page that is gone.
func (self *transaction) movePageShares(agentId, path, target string) error {
	if err := self.endPageSharesUnder(agentId, target); err != nil {
		return err
	}
	return self.tx.Exec(
		`UPDATE "agent_page_share" SET "path" = ? || substring("path" from ?::int)
		 WHERE "agent_id" = ? AND ("path" = ? OR "path" LIKE ?)`,
		target, utf8.RuneCountInString(path)+1, agentId, path, likeEscaped(path)+"/%").Error
}
