package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

// SkillShareOperation is who may call which tools of whose skill: a person's
// installed skill, some of its tools, shared with one person in a circle they
// share, with limits. And the calls made through it, each of which is a row
// whatever came of it.
//
// This is only the tables and the questions about them. What a call is worth
// and whether it goes ahead is internal/skills/share.go, and carrying it out
// on the owner's computer is internal/agent/skill_share.go.
type SkillShareOperation interface {
	// ShareSkill shares the owner's skill tools with one person, or changes
	// the tools and limits when it is shared with them already. ErrNotFound
	// unless the owner has an agent, the grantee exists and is not the owner,
	// and the two share a circle. ErrInvalidArguments for no tools or a limit
	// with no currency. Whether the skill is installed and declares the tools
	// is the caller's to check.
	ShareSkill(ownerUserId string, share *models.SkillShare) (*models.SkillShare, error)

	// GetSkillShare is one share, or nil.
	GetSkillShare(shareId string) (*models.SkillShare, error)

	// LockSkillShare is a share with its row held until the transaction ends,
	// so that two calls deciding at once against the month's limit take turns.
	// ErrNotFound when there is none.
	LockSkillShare(shareId string) (*models.SkillShare, error)

	// ListSkillSharesBy are the shares the person made, by skill and then by
	// who they are for.
	ListSkillSharesBy(ownerUserId string) ([]*models.SkillShare, error)

	// ListSkillSharesReaching are the shares made to the person by somebody
	// who still shares a circle with them, with the owner named.
	ListSkillSharesReaching(granteeUserId string) ([]*models.SkillShare, error)

	// DeleteSkillShare takes one back, with its calls. ErrNotFound when there
	// is none.
	DeleteSkillShare(shareId string) error

	// CreateSkillShareCall records one call. The caller sets its state; a
	// call that waits has an ExpiresAt.
	CreateSkillShareCall(call *models.SkillShareCall) (*models.SkillShareCall, error)

	// GetSkillShareCall is one call, or nil.
	GetSkillShareCall(callId string) (*models.SkillShareCall, error)

	// ListSkillShareCalls are a share's calls, waiting ones first and then
	// newest first. A state narrows them; empty is every state.
	ListSkillShareCalls(shareId string, callState models.SkillShareCallState, limit int) ([]*models.SkillShareCall, error)

	// SkillShareMonthTotal is what the share's ran calls come to, in the
	// currency, for those that ran from the moment to the moment (a call that
	// waited ran when it was approved), and no other calls: one that failed,
	// was refused, declined, expired or still waits has paid nothing.
	SkillShareMonthTotal(shareId, currencyCode string, from, until time.Time) (int64, error)

	// HasSkillShareOutputWith says whether an earlier call of the share that
	// ran answered with this identifier as a whole word in any text of its
	// output: the identifier of a request it made. One shorter than
	// SkillShareIdentifierShortest never is.
	HasSkillShareOutputWith(shareId, identifier string) (bool, error)

	// AnswerSkillShareCall moves a call out of waiting_for_approval to the
	// state given, once: it changes the row only if the call is waiting and,
	// for ran, has not expired, and says whether it did. Two answers at the
	// same moment change the row once. The call is returned as it now stands.
	AnswerSkillShareCall(callId string, callState models.SkillShareCallState, decisionNote string, now time.Time) (*models.SkillShareCall, bool, error)

	// FinishSkillShareCall records how a call that was started came out: its
	// state and what it answered.
	FinishSkillShareCall(callId string, callState models.SkillShareCallState, toolOutput map[string]any) error

	// ExpireSkillShareCalls moves the calls whose time to wait is over to
	// expired, and returns them.
	ExpireSkillShareCalls(now time.Time) ([]*models.SkillShareCall, error)

	// ListSkillShareCallsToWake are the calls the owner or the clock decided
	// between the two moments whose conversation has not been told, and whose
	// claim, if any, is older than the third.
	ListSkillShareCallsToWake(decidedAfter, decidedBefore, claimedBefore time.Time, limit int) ([]*models.SkillShareCall, error)

	// ClaimSkillShareCallWake takes the telling of one call, and says whether
	// the caller has it: only one caller does, until the claim is older than
	// claimedBefore.
	ClaimSkillShareCallWake(callId string, now, claimedBefore time.Time) (bool, error)

	// ReleaseSkillShareCallWakes lets go of claims whose telling was given
	// up, for the next sweep to take.
	ReleaseSkillShareCallWakes(callIds []string) error

	// MarkSkillShareCallsWoken records that the conversation was told.
	MarkSkillShareCallsWoken(callIds []string, now time.Time) error
}

// SkillShareIdentifierShortest is the shortest identifier that counts as one
// an earlier call made. A short one would be found in any output by chance, and
// the identifiers a payment service gives are long.
const SkillShareIdentifierShortest = 6

type skillShareModel struct {
	ID                 string         `gorm:"column:id;primaryKey"`
	CreatedAt          time.Time      `gorm:"column:created_at"`
	ModifiedAt         time.Time      `gorm:"column:modified_at"`
	OwnerAgentID       string         `gorm:"column:owner_agent_id"`
	SkillName          string         `gorm:"column:skill_name"`
	ToolNames          pq.StringArray `gorm:"column:tool_names;type:text[]"`
	GranteeUserID      string         `gorm:"column:grantee_user_id"`
	PaymentLimitMinor  *int64         `gorm:"column:payment_limit_minor"`
	MonthlyLimitMinor  *int64         `gorm:"column:monthly_limit_minor"`
	CurrencyCode       *string        `gorm:"column:currency_code"`
	IsApprovalRequired bool           `gorm:"column:is_approval_required"`
}

func (skillShareModel) TableName() string { return "skill_share" }

func (self *skillShareModel) toModel() *models.SkillShare {
	share := &models.SkillShare{
		ID:                 self.ID,
		CreatedAt:          self.CreatedAt.In(time.Local),
		ModifiedAt:         self.ModifiedAt.In(time.Local),
		OwnerAgentID:       self.OwnerAgentID,
		SkillName:          self.SkillName,
		ToolNames:          append([]string{}, self.ToolNames...),
		GranteeUserID:      self.GranteeUserID,
		PaymentLimitMinor:  self.PaymentLimitMinor,
		MonthlyLimitMinor:  self.MonthlyLimitMinor,
		IsApprovalRequired: self.IsApprovalRequired,
	}
	if self.CurrencyCode != nil {
		share.CurrencyCode = *self.CurrencyCode
	}
	return share
}

type skillShareCallModel struct {
	ID                    string     `gorm:"column:id;primaryKey"`
	CreatedAt             time.Time  `gorm:"column:created_at"`
	ModifiedAt            time.Time  `gorm:"column:modified_at"`
	SkillShareID          string     `gorm:"column:skill_share_id"`
	GranteeRunID          *string    `gorm:"column:grantee_run_id"`
	GranteeConversationID *string    `gorm:"column:grantee_conversation_id"`
	ToolName              string     `gorm:"column:tool_name"`
	ToolArguments         []byte     `gorm:"column:tool_arguments"`
	RequestReason         string     `gorm:"column:request_reason"`
	AmountMinor           *int64     `gorm:"column:amount_minor"`
	CurrencyCode          *string    `gorm:"column:currency_code"`
	CallState             string     `gorm:"column:call_state"`
	ToolOutput            []byte     `gorm:"column:tool_output"`
	DecidedAt             *time.Time `gorm:"column:decided_at"`
	DecisionNote          string     `gorm:"column:decision_note"`
	ExpiresAt             *time.Time `gorm:"column:expires_at"`
	WakeClaimedAt         *time.Time `gorm:"column:wake_claimed_at"`
	WokenAt               *time.Time `gorm:"column:woken_at"`
}

func (skillShareCallModel) TableName() string { return "skill_share_call" }

func (self *skillShareCallModel) toModel() *models.SkillShareCall {
	call := &models.SkillShareCall{
		ID:            self.ID,
		CreatedAt:     self.CreatedAt.In(time.Local),
		ModifiedAt:    self.ModifiedAt.In(time.Local),
		SkillShareID:  self.SkillShareID,
		ToolName:      self.ToolName,
		RequestReason: self.RequestReason,
		AmountMinor:   self.AmountMinor,
		CallState:     models.SkillShareCallState(self.CallState),
		DecisionNote:  self.DecisionNote,
		ToolArguments: map[string]any{},
	}
	if self.GranteeRunID != nil {
		call.GranteeRunID = *self.GranteeRunID
	}
	if self.GranteeConversationID != nil {
		call.GranteeConversationID = *self.GranteeConversationID
	}
	if self.CurrencyCode != nil {
		call.CurrencyCode = *self.CurrencyCode
	}
	if len(self.ToolArguments) > 0 {
		_ = json.Unmarshal(self.ToolArguments, &call.ToolArguments)
	}
	if call.ToolArguments == nil {
		call.ToolArguments = map[string]any{}
	}
	if len(self.ToolOutput) > 0 {
		_ = json.Unmarshal(self.ToolOutput, &call.ToolOutput)
	}
	call.DecidedAt = localTime(self.DecidedAt)
	call.ExpiresAt = localTime(self.ExpiresAt)
	call.WokenAt = localTime(self.WokenAt)
	return call
}

// describeSkillShares names who owns each share and who it is for. One read
// of the owners' agents and one of the accounts however many shares there are.
func (self *transaction) describeSkillShares(rows []skillShareModel) ([]*models.SkillShare, error) {
	agentIds, userIds := []string{}, []string{}
	for _, row := range rows {
		agentIds = append(agentIds, row.OwnerAgentID)
		userIds = append(userIds, row.GranteeUserID)
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
	users, err := loadUsersByID(self.tx, userIds)
	if err != nil {
		return nil, err
	}
	shares := make([]*models.SkillShare, 0, len(rows))
	for index := range rows {
		share := rows[index].toModel()
		share.OwnerUserID = ownerOfAgent[share.OwnerAgentID]
		if owner := users[share.OwnerUserID]; owner != nil {
			share.OwnerUsername, share.OwnerName, share.OwnerUser = owner.Username, owner.DisplayName(), owner
		}
		if grantee := users[share.GranteeUserID]; grantee != nil {
			share.GranteeUsername, share.GranteeName, share.GranteeUser = grantee.Username, grantee.DisplayName(), grantee
		}
		shares = append(shares, share)
	}
	return shares, nil
}

func (self *transaction) ShareSkill(ownerUserId string, share *models.SkillShare) (*models.SkillShare, error) {
	if share == nil || ownerUserId == "" || strings.TrimSpace(share.SkillName) == "" || share.GranteeUserID == "" {
		return nil, ErrInvalidArguments
	}
	toolNames := uniqueStrings(share.ToolNames)
	if len(toolNames) == 0 {
		return nil, fmt.Errorf("%w: share at least one tool", ErrInvalidArguments)
	}
	currencyCode, hasCurrency := models.NormalizeCurrencyCode(share.CurrencyCode)
	if share.CurrencyCode != "" && !hasCurrency {
		return nil, fmt.Errorf("%w: a currency is three letters", ErrInvalidArguments)
	}
	if (share.PaymentLimitMinor != nil || share.MonthlyLimitMinor != nil) && !hasCurrency {
		return nil, fmt.Errorf("%w: a limit is in a currency", ErrInvalidArguments)
	}
	for _, limit := range []*int64{share.PaymentLimitMinor, share.MonthlyLimitMinor} {
		if limit != nil && *limit <= 0 {
			return nil, fmt.Errorf("%w: a limit is above zero", ErrInvalidArguments)
		}
	}
	if share.GranteeUserID == ownerUserId {
		return nil, ErrInvalidArguments
	}
	owner, err := self.GetAgentByUser(ownerUserId)
	if err != nil {
		return nil, err
	}
	if owner == nil {
		return nil, ErrNotFound
	}
	var granteeCount int64
	if err := self.tx.Model(&userModel{}).Where("\"id\" = ?", share.GranteeUserID).Count(&granteeCount).Error; err != nil {
		return nil, err
	}
	if granteeCount == 0 {
		return nil, ErrNotFound
	}
	isMutual, err := self.holdMutualCircle(ownerUserId, share.GranteeUserID)
	if err != nil {
		return nil, err
	}
	if !isMutual {
		return nil, ErrNotFound
	}

	var storedCurrency *string
	if hasCurrency {
		storedCurrency = &currencyCode
	}
	now := time.Now()
	row := &skillShareModel{
		ID:                 newID(),
		CreatedAt:          now,
		ModifiedAt:         now,
		OwnerAgentID:       owner.ID,
		SkillName:          strings.TrimSpace(share.SkillName),
		ToolNames:          toolNames,
		GranteeUserID:      share.GranteeUserID,
		PaymentLimitMinor:  share.PaymentLimitMinor,
		MonthlyLimitMinor:  share.MonthlyLimitMinor,
		CurrencyCode:       storedCurrency,
		IsApprovalRequired: share.IsApprovalRequired,
	}
	conflict := clause.OnConflict{
		Columns: []clause.Column{{Name: "owner_agent_id"}, {Name: "skill_name"}, {Name: "grantee_user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"modified_at", "tool_names", "payment_limit_minor", "monthly_limit_minor", "currency_code", "is_approval_required",
		}),
	}
	if err := self.tx.Clauses(conflict).Create(row).Error; err != nil {
		return nil, err
	}
	// Read back, because an upsert that changed an existing share keeps its
	// identifier and its date.
	var kept skillShareModel
	if err := self.tx.Where("\"owner_agent_id\" = ? AND \"skill_name\" = ? AND \"grantee_user_id\" = ?",
		owner.ID, row.SkillName, row.GranteeUserID).Take(&kept).Error; err != nil {
		return nil, err
	}
	described, err := self.describeSkillShares([]skillShareModel{kept})
	if err != nil {
		return nil, err
	}
	return described[0], nil
}

func (self *transaction) GetSkillShare(shareId string) (*models.SkillShare, error) {
	if shareId == "" {
		return nil, nil
	}
	var found []skillShareModel
	if err := self.tx.Where("\"id\" = ?", shareId).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	described, err := self.describeSkillShares(found)
	if err != nil {
		return nil, err
	}
	return described[0], nil
}

func (self *transaction) LockSkillShare(shareId string) (*models.SkillShare, error) {
	var locked skillShareModel
	if err := lockRow(self.tx, &locked, shareId); err != nil {
		return nil, err
	}
	described, err := self.describeSkillShares([]skillShareModel{locked})
	if err != nil {
		return nil, err
	}
	return described[0], nil
}

func (self *transaction) ListSkillSharesBy(ownerUserId string) ([]*models.SkillShare, error) {
	var rows []skillShareModel
	if err := self.tx.Table("skill_share AS share").
		Select("share.*").
		Joins("JOIN agent owner ON owner.id = share.owner_agent_id").
		Where("owner.user_id = ?", ownerUserId).
		Order("share.skill_name ASC, share.created_at ASC, share.id ASC").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	return self.describeSkillShares(rows)
}

func (self *transaction) ListSkillSharesReaching(granteeUserId string) ([]*models.SkillShare, error) {
	if granteeUserId == "" {
		return []*models.SkillShare{}, nil
	}
	var rows []skillShareModel
	if err := self.tx.Raw(`
SELECT share.*
FROM "skill_share" share
JOIN "agent" owner ON owner.id = share.owner_agent_id
WHERE share.grantee_user_id = @grantee
  AND owner.user_id <> @grantee
  AND EXISTS (
      SELECT 1 FROM "circle_member" mine
      JOIN "circle_member" theirs ON theirs.circle_id = mine.circle_id
      WHERE mine.user_id = @grantee AND theirs.user_id = owner.user_id)`, sql.Named("grantee", granteeUserId)).Scan(&rows).Error; err != nil {
		return nil, err
	}
	shares, err := self.describeSkillShares(rows)
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
		if left.SkillName != right.SkillName {
			return left.SkillName < right.SkillName
		}
		return left.ID < right.ID
	})
	return shares, nil
}

func (self *transaction) DeleteSkillShare(shareId string) error {
	deleted := self.tx.Where("\"id\" = ?", shareId).Delete(&skillShareModel{})
	if deleted.Error != nil {
		return deleted.Error
	}
	if deleted.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// endSkillSharesOutsideCircles deletes the skill shares between this person
// and anybody they no longer share any circle with, in both directions; the
// circle code calls it (endSharesOutsideCircles) in the transaction that takes
// a person out of a circle. A skill share is always to one person, so unlike
// the other shares there is no circle's share to leave alone: it ends the
// moment the two stop sharing a circle, and the calls it holds go with it.
func endSkillSharesOutsideCircles(tx *gorm.DB, userId string) error {
	return tx.Exec(`DELETE FROM "skill_share" WHERE "id" IN (
		SELECT share.id FROM "skill_share" share
		JOIN "agent" owner ON owner.id = share.owner_agent_id
		WHERE (owner.user_id = @user OR share.grantee_user_id = @user)
		  AND NOT EXISTS (
		      SELECT 1 FROM "circle_member" mine
		      JOIN "circle_member" theirs ON theirs.circle_id = mine.circle_id
		      WHERE mine.user_id = owner.user_id AND theirs.user_id = share.grantee_user_id))`,
		sql.Named("user", userId)).Error
}

func (self *transaction) CreateSkillShareCall(call *models.SkillShareCall) (*models.SkillShareCall, error) {
	if call == nil || call.SkillShareID == "" || call.ToolName == "" || !call.CallState.IsValid() {
		return nil, ErrInvalidArguments
	}
	if call.CallState == models.SkillShareCallWaitingForApproval && call.ExpiresAt == nil {
		return nil, fmt.Errorf("%w: a call that waits has a time to stop waiting", ErrInvalidArguments)
	}
	arguments := call.ToolArgumentsJSON()
	var output []byte
	if call.ToolOutput != nil {
		encoded, err := json.Marshal(call.ToolOutput)
		if err != nil {
			return nil, err
		}
		output = encoded
	}
	var currencyCode *string
	if call.CurrencyCode != "" {
		currencyCode = &call.CurrencyCode
	}
	now := time.Now()
	id := newID()
	if err := self.tx.Exec(`INSERT INTO "skill_share_call"
		("id", "created_at", "modified_at", "skill_share_id", "grantee_run_id", "grantee_conversation_id", "tool_name",
		 "tool_arguments", "request_reason", "amount_minor", "currency_code", "call_state", "tool_output", "decided_at", "decision_note", "expires_at")
		VALUES (?, ?, ?, ?, ?, ?, ?, ?::jsonb, ?, ?, ?, ?, ?::jsonb, ?, ?, ?)`,
		id, now, now, call.SkillShareID, nullableText(call.GranteeRunID), nullableText(call.GranteeConversationID), call.ToolName,
		string(arguments), call.RequestReason, call.AmountMinor, currencyCode, string(call.CallState), nullableJSON(output),
		call.DecidedAt, call.DecisionNote, call.ExpiresAt).Error; err != nil {
		return nil, err
	}
	return self.GetSkillShareCall(id)
}

// nullableJSON is a value for a nullable jsonb column.
func nullableJSON(encoded []byte) any {
	if len(encoded) == 0 {
		return nil
	}
	return string(encoded)
}

func (self *transaction) GetSkillShareCall(callId string) (*models.SkillShareCall, error) {
	if callId == "" {
		return nil, nil
	}
	var found []skillShareCallModel
	if err := self.tx.Where("\"id\" = ?", callId).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return found[0].toModel(), nil
}

func (self *transaction) ListSkillShareCalls(shareId string, callState models.SkillShareCallState, limit int) ([]*models.SkillShareCall, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := self.tx.Where("\"skill_share_id\" = ?", shareId)
	if callState != "" {
		query = query.Where("\"call_state\" = ?", string(callState))
	}
	var rows []skillShareCallModel
	if err := query.
		Order("(\"call_state\" = 'waiting_for_approval') DESC, \"created_at\" DESC, \"id\" DESC").
		Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	calls := make([]*models.SkillShareCall, 0, len(rows))
	for index := range rows {
		calls = append(calls, rows[index].toModel())
	}
	return calls, nil
}

func (self *transaction) SkillShareMonthTotal(shareId, currencyCode string, from, until time.Time) (int64, error) {
	var total int64
	if err := self.tx.Raw(`SELECT COALESCE(SUM("amount_minor"), 0) FROM "skill_share_call"
		WHERE "skill_share_id" = ? AND "call_state" = 'ran' AND "currency_code" = ?
		  AND "amount_minor" IS NOT NULL AND COALESCE("decided_at", "created_at") >= ? AND COALESCE("decided_at", "created_at") < ?`,
		shareId, currencyCode, from, until).Scan(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

func (self *transaction) HasSkillShareOutputWith(shareId, identifier string) (bool, error) {
	if shareId == "" || len(identifier) < SkillShareIdentifierShortest {
		return false, nil
	}
	// A skill that runs a command answers with what it printed, as one piece
	// of text, so the identifier is looked for as a whole word in any of the
	// output's text values: not as part of a longer word, and not as the name
	// of a field, which is a word followed by a colon.
	pattern := "(^|[^A-Za-z0-9_-])" + regexp.QuoteMeta(identifier) + `(?![A-Za-z0-9_-])(?!["']?\s*:)`
	var matchingCallCount int64
	if err := self.tx.Raw(`SELECT count(*) FROM "skill_share_call" AS call
		WHERE call."skill_share_id" = ? AND call."call_state" = 'ran' AND call."tool_output" IS NOT NULL
		  AND EXISTS (SELECT 1 FROM jsonb_path_query(call."tool_output", ?::jsonpath) AS found
		              WHERE (found #>> '{}') ~ ?)`,
		shareId, `$.** ? (@.type() == "string")`, pattern).Scan(&matchingCallCount).Error; err != nil {
		return false, err
	}
	return matchingCallCount > 0, nil
}

func (self *transaction) AnswerSkillShareCall(callId string, callState models.SkillShareCallState, decisionNote string, now time.Time) (*models.SkillShareCall, bool, error) {
	switch callState {
	case models.SkillShareCallRan, models.SkillShareCallDeclined:
	default:
		return nil, false, ErrInvalidArguments
	}
	// Conditional on still waiting and, to be approved, still within its day:
	// whichever of two answers reaches the row first changes it, and the other
	// finds nothing to change.
	update := self.tx.Exec(`UPDATE "skill_share_call"
		SET "call_state" = ?, "decided_at" = ?, "decision_note" = ?, "modified_at" = ?
		WHERE "id" = ? AND "call_state" = 'waiting_for_approval' AND ("expires_at" IS NULL OR "expires_at" > ? OR ? = 'declined')`,
		string(callState), now, decisionNote, now, callId, now, string(callState))
	if update.Error != nil {
		return nil, false, update.Error
	}
	call, err := self.GetSkillShareCall(callId)
	if err != nil {
		return nil, false, err
	}
	return call, update.RowsAffected > 0, nil
}

func (self *transaction) FinishSkillShareCall(callId string, callState models.SkillShareCallState, toolOutput map[string]any) error {
	if !callState.IsValid() {
		return ErrInvalidArguments
	}
	var output any
	if toolOutput != nil {
		encoded, err := json.Marshal(toolOutput)
		if err != nil {
			return err
		}
		output = string(encoded)
	}
	update := self.tx.Exec(`UPDATE "skill_share_call" SET "call_state" = ?, "tool_output" = ?::jsonb, "modified_at" = ? WHERE "id" = ?`,
		string(callState), output, time.Now(), callId)
	if update.Error != nil {
		return update.Error
	}
	if update.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (self *transaction) ExpireSkillShareCalls(now time.Time) ([]*models.SkillShareCall, error) {
	var rows []skillShareCallModel
	if err := self.tx.Raw(`UPDATE "skill_share_call"
		SET "call_state" = 'expired', "decided_at" = ?, "modified_at" = ?
		WHERE "call_state" = 'waiting_for_approval' AND "expires_at" <= ?
		RETURNING *`, now, now, now).Scan(&rows).Error; err != nil {
		return nil, err
	}
	calls := make([]*models.SkillShareCall, 0, len(rows))
	for index := range rows {
		calls = append(calls, rows[index].toModel())
	}
	return calls, nil
}

func (self *transaction) ListSkillShareCallsToWake(decidedAfter, decidedBefore, claimedBefore time.Time, limit int) ([]*models.SkillShareCall, error) {
	if limit <= 0 {
		limit = 50
	}
	var rows []skillShareCallModel
	if err := self.tx.Raw(`SELECT * FROM "skill_share_call"
		WHERE "decided_at" IS NOT NULL AND "woken_at" IS NULL AND "grantee_conversation_id" IS NOT NULL
		  AND "decided_at" > ? AND "decided_at" <= ?
		  AND ("wake_claimed_at" IS NULL OR "wake_claimed_at" <= ?)
		ORDER BY "decided_at" ASC, "id" ASC LIMIT ?`, decidedAfter, decidedBefore, claimedBefore, limit).Scan(&rows).Error; err != nil {
		return nil, err
	}
	calls := make([]*models.SkillShareCall, 0, len(rows))
	for index := range rows {
		calls = append(calls, rows[index].toModel())
	}
	return calls, nil
}

func (self *transaction) ClaimSkillShareCallWake(callId string, now, claimedBefore time.Time) (bool, error) {
	update := self.tx.Exec(`UPDATE "skill_share_call" SET "wake_claimed_at" = ?
		WHERE "id" = ? AND "woken_at" IS NULL AND ("wake_claimed_at" IS NULL OR "wake_claimed_at" <= ?)`, now, callId, claimedBefore)
	return update.RowsAffected > 0, update.Error
}

func (self *transaction) ReleaseSkillShareCallWakes(callIds []string) error {
	if len(callIds) == 0 {
		return nil
	}
	return self.tx.Exec(`UPDATE "skill_share_call" SET "wake_claimed_at" = NULL WHERE "id" IN ? AND "woken_at" IS NULL`, callIds).Error
}

func (self *transaction) MarkSkillShareCallsWoken(callIds []string, now time.Time) error {
	if len(callIds) == 0 {
		return nil
	}
	return self.tx.Exec(`UPDATE "skill_share_call" SET "woken_at" = ? WHERE "id" IN ?`, now, callIds).Error
}
