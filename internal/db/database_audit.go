package db

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/security"
)

// AuditPrincipal is who is making the changes in a transaction: derived once
// from the request by the authentication layer and carried on the context the
// transaction was opened with, so that every audited write in it names the
// same actor without each caller passing one.
type AuditPrincipal struct {
	ActorKind models.AuditActorKind
	UserID    string
	TokenID   string
	SourceIP  string

	// ImpersonatorUserID is the operator signed in as UserID, when the
	// request comes from an impersonation.
	ImpersonatorUserID string
}

type auditPrincipalKey struct{}

// ContextWithAuditPrincipal records who is acting, for TransactionContext.
func ContextWithAuditPrincipal(ctx context.Context, principal AuditPrincipal) context.Context {
	return context.WithValue(ctx, auditPrincipalKey{}, principal)
}

// PrincipalFromContext is the actor a context carries. A context that carries
// none is the server acting on its own behalf.
func PrincipalFromContext(ctx context.Context) AuditPrincipal {
	if ctx != nil {
		if principal, ok := ctx.Value(auditPrincipalKey{}).(AuditPrincipal); ok {
			return principal
		}
	}
	return AuditPrincipal{ActorKind: models.AuditActorSystem}
}

// AuditOperation reads the audit log. Writing it is not an operation anybody
// calls: every audited write in this package goes through applyMutation.
type AuditOperation interface {
	ListAuditEvents(options *AuditOptions) ([]*models.AuditEvent, error)
	CountAuditEvents(options *AuditOptions) (int64, error)

	// ScavengeAuditEvents removes rows older than the retention.
	ScavengeAuditEvents(before time.Time) (int64, error)
}

// AuditOptions narrows a listing. Every filter is optional.
type AuditOptions struct {
	ResourceType string
	ResourceID   string
	ActorUserID  string
	Since        *time.Time
	Until        *time.Time
	Limit        int
	Offset       int
}

type auditEventModel struct {
	ID          string    `gorm:"column:id;primaryKey"`
	CreatedAt   time.Time `gorm:"column:created_at"`
	ActorKind   string    `gorm:"column:actor_kind"`
	ActorUserID string    `gorm:"column:actor_user_id"`
	TokenID     string    `gorm:"column:token_id"`

	ImpersonatorUserID *string `gorm:"column:impersonator_user_id"`
	SourceIP           string  `gorm:"column:source_ip"`
	Instance           string  `gorm:"column:instance"`

	ResourceType string `gorm:"column:resource_type"`
	ResourceID   string `gorm:"column:resource_id"`
	Action       string `gorm:"column:action"`
	Before       []byte `gorm:"column:before;type:jsonb"`
	After        []byte `gorm:"column:after;type:jsonb"`
}

func (auditEventModel) TableName() string { return "audit_event" }

func auditEventFromModel(model *auditEventModel) *models.AuditEvent {
	return &models.AuditEvent{
		ID:          model.ID,
		CreatedAt:   model.CreatedAt.In(time.Local),
		ActorKind:   models.AuditActorKind(model.ActorKind),
		ActorUserID: model.ActorUserID,
		TokenID:     model.TokenID,

		ImpersonatorUserID: stringOrEmpty(model.ImpersonatorUserID),
		SourceIP:           model.SourceIP,
		Instance:           model.Instance,
		ResourceType:       models.AuditResourceType(model.ResourceType),
		ResourceID:         model.ResourceID,
		Action:             models.AuditAction(model.Action),
		Before:             json.RawMessage(model.Before),
		After:              json.RawMessage(model.After),
	}
}

// applyMutation is the one seam every audited write goes through: it runs the
// write, and when it succeeds writes the audit row in the same transaction,
// naming the actor the transaction's context carries.
//
// before is the row before the change, nil on create; after is the row after,
// nil on delete. A model that carries a secret implements
// models.AuditRedactor and its redacted form is what is recorded. An update
// whose redacted before and after are the same changed nothing worth a row.
func (self *transaction) applyMutation(resourceType models.AuditResourceType, resourceId string, action models.AuditAction, before, after any, write func(*gorm.DB) error) error {
	if err := write(self.tx); err != nil {
		return err
	}

	principal := PrincipalFromContext(self.ctx)
	if principal.ActorKind == "" {
		principal.ActorKind = models.AuditActorSystem
	}

	encodedBefore, err := encodeForAudit(before)
	if err != nil {
		return fmt.Errorf("db: cannot record the audit event for %s %s: %w", resourceType, resourceId, err)
	}
	encodedAfter, err := encodeForAudit(after)
	if err != nil {
		return fmt.Errorf("db: cannot record the audit event for %s %s: %w", resourceType, resourceId, err)
	}
	if action == models.AuditActionUpdate {
		var changed bool
		encodedBefore, encodedAfter, changed, err = changedAuditFields(resourceType, principal.ActorKind, encodedBefore, encodedAfter)
		if err != nil {
			return fmt.Errorf("db: cannot record the audit event for %s %s: %w", resourceType, resourceId, err)
		}
		if !changed {
			return nil
		}
	}

	event := &auditEventModel{
		ID:          security.NewULID(),
		CreatedAt:   time.Now(),
		ActorKind:   string(principal.ActorKind),
		ActorUserID: principal.UserID,
		TokenID:     principal.TokenID,

		ImpersonatorUserID: stringOrNil(principal.ImpersonatorUserID),
		SourceIP:           principal.SourceIP,
		Instance:           self.database.settings.BackendID,
		ResourceType:       string(resourceType),
		ResourceID:         resourceId,
		Action:             string(action),
		Before:             encodedBefore,
		After:              encodedAfter,
	}
	return self.tx.Create(event).Error
}

// encodeForAudit is the row as the audit log keeps it. Timestamps are left
// out of the comparison an update makes by being left out of the record: the
// row's own modified_at says when, the event says what.
func encodeForAudit(value any) ([]byte, error) {
	if value == nil {
		return nil, nil
	}
	if redactor, ok := value.(models.AuditRedactor); ok {
		value = redactor.RedactForAudit()
		if value == nil {
			return nil, nil
		}
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		// Not an object; recorded as it is.
		return encoded, nil
	}
	delete(fields, "createdAt")
	delete(fields, "modifiedAt")
	return json.Marshal(fields)
}

func (self *transaction) auditQuery(options *AuditOptions) *gorm.DB {
	query := self.tx.Model(&auditEventModel{})
	if options == nil {
		return query
	}
	if options.ResourceType != "" {
		query = query.Where("\"resource_type\" = ?", options.ResourceType)
	}
	if options.ResourceID != "" {
		query = query.Where("\"resource_id\" = ?", options.ResourceID)
	}
	if options.ActorUserID != "" {
		query = query.Where("\"actor_user_id\" = ?", options.ActorUserID)
	}
	if options.Since != nil {
		query = query.Where("\"created_at\" >= ?", *options.Since)
	}
	if options.Until != nil {
		query = query.Where("\"created_at\" < ?", *options.Until)
	}
	return query
}

func (self *transaction) ListAuditEvents(options *AuditOptions) ([]*models.AuditEvent, error) {
	query := self.auditQuery(options).Order("\"created_at\" DESC, \"id\" DESC")
	if options != nil && options.Limit > 0 {
		query = query.Limit(options.Limit)
	}
	if options != nil && options.Offset > 0 {
		query = query.Offset(options.Offset)
	}
	var rows []auditEventModel
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	events := make([]*models.AuditEvent, 0, len(rows))
	for index := range rows {
		events = append(events, auditEventFromModel(&rows[index]))
	}
	return events, nil
}

func (self *transaction) CountAuditEvents(options *AuditOptions) (int64, error) {
	var count int64
	if err := self.auditQuery(options).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (self *transaction) ScavengeAuditEvents(before time.Time) (int64, error) {
	result := self.tx.Where("\"created_at\" < ?", before).Delete(&auditEventModel{})
	return result.RowsAffected, result.Error
}

// auditBookkeepingFields are the fields of a resource that the server writes
// on its own account as it works, so an update of only these says nothing a
// person or an operator acts on: an agent's dream finished, a credential was
// used. They are never recorded, alone or beside a real change.
var auditBookkeepingFields = map[models.AuditResourceType][]string{
	models.AuditResourceAgent: {
		"dreamedAt", "decayedAt", "dreamBootstrap", "followUpBackfilledAt", "spokeFirstAt", "onboardedAt",
	},
	models.AuditResourceUser:               {"timezoneSeenAt"},
	models.AuditResourceSyncCredential:     {"lastUsedAt"},
	models.AuditResourceMailboxAppPassword: {"lastUsedAt"},
}

// auditSystemBookkeepingFields are fields that are bookkeeping only when the
// server wrote them: the matcher pairing receipt lines with purchases. The
// same change by a person or by their agent is the person's decision and is
// recorded.
var auditSystemBookkeepingFields = map[models.AuditResourceType][]string{
	models.AuditResourceFinanceReceipt: {"receiptMatches"},
}

// changedAuditFields reduces an update to what changed. Both sides are read
// as objects and a field that is absent, null, an empty list or an empty
// object counts as unset, so a nil slice on one side and an empty slice on
// the other is no change. Bookkeeping fields are dropped. What comes back is
// only the changed fields on each side, and false when there are none.
func changedAuditFields(resourceType models.AuditResourceType, actorKind models.AuditActorKind, encodedBefore, encodedAfter []byte) ([]byte, []byte, bool, error) {
	beforeFields, err := auditFieldsOf(encodedBefore)
	if err != nil {
		return nil, nil, false, err
	}
	afterFields, err := auditFieldsOf(encodedAfter)
	if err != nil {
		return nil, nil, false, err
	}
	ignored := map[string]bool{}
	for _, field := range auditBookkeepingFields[resourceType] {
		ignored[field] = true
	}
	if actorKind == models.AuditActorSystem {
		for _, field := range auditSystemBookkeepingFields[resourceType] {
			ignored[field] = true
		}
	}
	changedBefore := map[string]json.RawMessage{}
	changedAfter := map[string]json.RawMessage{}
	for field, beforeValue := range beforeFields {
		if !ignored[field] && !bytes.Equal(beforeValue, afterFields[field]) {
			changedBefore[field] = beforeValue
		}
	}
	for field, afterValue := range afterFields {
		if !ignored[field] && !bytes.Equal(afterValue, beforeFields[field]) {
			changedAfter[field] = afterValue
		}
	}
	if len(changedBefore) == 0 && len(changedAfter) == 0 {
		return nil, nil, false, nil
	}
	reducedBefore, err := json.Marshal(changedBefore)
	if err != nil {
		return nil, nil, false, err
	}
	reducedAfter, err := json.Marshal(changedAfter)
	if err != nil {
		return nil, nil, false, err
	}
	return reducedBefore, reducedAfter, true, nil
}

// auditFieldsOf is an encoded row as its set fields, each compacted so that
// equal values compare equal as bytes. A value that is not an object has no
// fields.
func auditFieldsOf(encoded []byte) (map[string]json.RawMessage, error) {
	fields := map[string]json.RawMessage{}
	if len(encoded) == 0 {
		return fields, nil
	}
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &parsed); err != nil {
		return fields, nil
	}
	for field, raw := range parsed {
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, err
		}
		switch typed := value.(type) {
		case nil:
			continue
		case []any:
			if len(typed) == 0 {
				continue
			}
		case map[string]any:
			if len(typed) == 0 {
				continue
			}
		}
		compact, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		fields[field] = compact
	}
	return fields, nil
}
