package models

import (
	"encoding/json"
	"time"
)

// AuditEvent is one administrative change: who made it, from where, what row
// it touched, and the row before and after with secrets removed. Written in
// the same transaction as the change.
//
// Per-user private things — folders, items, contacts, flags — are never
// audited. "Who gave this group that role" must be answerable a year later;
// "who read this message" must not be recorded.
type AuditEvent struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"createdAt"`

	ActorKind   AuditActorKind `json:"actorKind"`
	ActorUserID string         `json:"actorUserId,omitempty"`

	// ActorLabel is the actor's username, or "system" or "rescue": resolved
	// when read, never stored, so a renamed user reads by their current name.
	ActorLabel string `json:"actorLabel,omitempty"`

	// TokenID is the session or API token that authorised the request; never
	// its secret.
	TokenID  string `json:"tokenId,omitempty"`
	SourceIP string `json:"sourceIp,omitempty"`

	// Instance is which server instance served the request.
	Instance string `json:"instance,omitempty"`

	ResourceType AuditResourceType `json:"resourceType"`
	ResourceID   string            `json:"resourceId"`
	Action       AuditAction       `json:"action"`

	// Before is the row before the change, redacted; nil on create.
	Before json.RawMessage `json:"before,omitempty"`

	// After is the row after the change, redacted; nil on delete.
	After json.RawMessage `json:"after,omitempty"`
}

// AuditActorKind says what kind of principal made a change.
type AuditActorKind string

const (
	// AuditActorUser is a signed-in person, through the web UI or the API.
	AuditActorUser AuditActorKind = "user"

	// AuditActorSystem is the server itself: a sweep, a certificate renewal,
	// single sign-on reconciling a group's users.
	AuditActorSystem AuditActorKind = "system"

	// AuditActorRescue is teanode-server run on the host against the
	// database, the one path that bypasses permissions.
	AuditActorRescue AuditActorKind = "rescue"
)

// AuditAction is what happened to the row.
type AuditAction string

const (
	AuditActionCreate AuditAction = "create"
	AuditActionUpdate AuditAction = "update"
	AuditActionDelete AuditAction = "delete"
)

// AuditResourceType names what was changed. Adding one here is what makes a
// table audited.
type AuditResourceType string

const (
	AuditResourceUser               AuditResourceType = "user"
	AuditResourceGroup              AuditResourceType = "group" // and its users, roles, domains
	AuditResourceRole               AuditResourceType = "role"  // and its permissions
	AuditResourceDomain             AuditResourceType = "domain"
	AuditResourceAlias              AuditResourceType = "alias"
	AuditResourceCredential         AuditResourceType = "credential"
	AuditResourceMailbox            AuditResourceType = "mailbox" // and its rules, signature, out-of-office
	AuditResourceMailboxAddress     AuditResourceType = "mailbox_address"
	AuditResourceMailboxAppPassword AuditResourceType = "mailbox_app_password"
	AuditResourceToken              AuditResourceType = "token"
	AuditResourcePasskey            AuditResourceType = "passkey"
	AuditResourceConfiguration      AuditResourceType = "configuration"
)

// AuditRedactor is implemented by a model that carries a secret, so the secret
// never reaches the audit row.
type AuditRedactor interface {
	RedactForAudit() any
}
