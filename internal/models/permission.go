package models

import (
	"slices"
	"sort"
)

// Permission is one named thing somebody may do, written resource:verb.
//
// The vocabulary is Go, not a table: a permission the code does not know
// cannot be granted, and a role_permission row naming one the code has
// forgotten is ignored rather than fatal.
type Permission string

const (
	PermissionMailRead        Permission = "mail:read"         // read one's own mailboxes
	PermissionMailWrite       Permission = "mail:write"        // flag, move, delete in them
	PermissionMailSend        Permission = "mail:send"         // send as an address of them
	PermissionMailboxManage   Permission = "mailbox:manage"    // folders, rules, addresses of them
	PermissionMailAudit       Permission = "mail:audit"        // every message of the group's domains, as the operator sees it today
	PermissionMailAuditAll    Permission = "mail:audit-all"    // the same, over every domain
	PermissionDomainManage    Permission = "domain:manage"     // the group's domains: aliases, credentials, templates
	PermissionDomainManageAll Permission = "domain:manage-all" // every domain, present and future, and creating new ones
	PermissionQueueManage     Permission = "queue:manage"      // the delivery queue
	PermissionReportRead      Permission = "report:read"       // DMARC reports
	PermissionUserManage      Permission = "user:manage"
	PermissionGroupManage     Permission = "group:manage"
	PermissionRoleManage      Permission = "role:manage"
	PermissionServerManage    Permission = "server:manage" // settings, upgrades, certificates
	PermissionAuditRead       Permission = "audit:read"    // the audit log
)

// PermissionKind is where a permission applies, declared with the vocabulary:
// there is no flag on a group, the permission itself says how far it reaches.
type PermissionKind string

const (
	// PermissionKindServer applies everywhere, whatever domains the group has.
	PermissionKindServer PermissionKind = "server"

	// PermissionKindDomain applies over the group's domains only.
	PermissionKindDomain PermissionKind = "domain"

	// PermissionKindAllDomains applies over every domain, present and future;
	// the group's domains are not consulted.
	PermissionKindAllDomains PermissionKind = "all-domains"
)

// permissionKinds is the vocabulary with its reach. The order is the order the
// role editor lists them in.
var permissionKinds = []struct {
	permission Permission
	kind       PermissionKind
	widens     Permission
}{
	{PermissionMailRead, PermissionKindServer, ""},
	{PermissionMailWrite, PermissionKindServer, ""},
	{PermissionMailSend, PermissionKindServer, ""},
	{PermissionMailboxManage, PermissionKindServer, ""},
	{PermissionMailAudit, PermissionKindDomain, ""},
	{PermissionMailAuditAll, PermissionKindAllDomains, PermissionMailAudit},
	{PermissionDomainManage, PermissionKindDomain, ""},
	{PermissionDomainManageAll, PermissionKindAllDomains, PermissionDomainManage},
	{PermissionQueueManage, PermissionKindServer, ""},
	{PermissionReportRead, PermissionKindServer, ""},
	{PermissionUserManage, PermissionKindServer, ""},
	{PermissionGroupManage, PermissionKindServer, ""},
	{PermissionRoleManage, PermissionKindServer, ""},
	{PermissionServerManage, PermissionKindServer, ""},
	{PermissionAuditRead, PermissionKindServer, ""},
}

// Permissions lists the whole vocabulary, in the order the role editor shows
// it.
func Permissions() []Permission {
	permissions := make([]Permission, 0, len(permissionKinds))
	for _, entry := range permissionKinds {
		permissions = append(permissions, entry.permission)
	}
	return permissions
}

// IsValid reports whether the code knows this permission.
func (self Permission) IsValid() bool {
	for _, entry := range permissionKinds {
		if entry.permission == self {
			return true
		}
	}
	return false
}

// Kind is the permission's reach. A key the code does not know has no kind
// and grants nothing.
func (self Permission) Kind() PermissionKind {
	for _, entry := range permissionKinds {
		if entry.permission == self {
			return entry.kind
		}
	}
	return ""
}

// Widens is, for an all-domains permission, the domain permission it stands
// in for on every domain: "domain:manage-all" widens "domain:manage". Empty
// for every other permission.
func (self Permission) Widens() Permission {
	for _, entry := range permissionKinds {
		if entry.permission == self {
			return entry.widens
		}
	}
	return ""
}

// EffectivePermissions is what one request may do, resolved once from the
// caller's groups: user_group × group_role × role_permission, scoped by
// group_domain. Carried on the request; never cached across requests on an
// instance, so a role change made through one instance applies to another's
// next request.
type EffectivePermissions struct {
	// Everywhere holds the server and all-domains permissions the caller's
	// groups carry.
	Everywhere []Permission `json:"everywhere"`

	// ByDomain holds the domain-kind permissions, from each group's roles
	// crossed with that group's domains. Additive: two groups that each reach
	// a domain contribute both their roles.
	ByDomain []DomainPermissions `json:"byDomain"`
}

// DomainPermissions is what the caller may do over one domain.
type DomainPermissions struct {
	DomainID    string       `json:"domainId"`
	Permissions []Permission `json:"permissions"`
}

// Grant is one (permission, domain) pair a group's roles produce; the domain
// is empty for a server or all-domains permission.
type Grant struct {
	Permission Permission
	DomainID   string
}

// NewEffectivePermissions folds grants into the shape a request carries.
func NewEffectivePermissions(grants []Grant) *EffectivePermissions {
	everywhere := map[Permission]bool{}
	byDomain := map[string]map[Permission]bool{}
	for _, grant := range grants {
		switch grant.Permission.Kind() {
		case PermissionKindServer, PermissionKindAllDomains:
			everywhere[grant.Permission] = true
		case PermissionKindDomain:
			if grant.DomainID == "" {
				continue
			}
			if byDomain[grant.DomainID] == nil {
				byDomain[grant.DomainID] = map[Permission]bool{}
			}
			byDomain[grant.DomainID][grant.Permission] = true
		}
	}
	self := &EffectivePermissions{Everywhere: sortedPermissions(everywhere), ByDomain: []DomainPermissions{}}
	domainIds := make([]string, 0, len(byDomain))
	for domainId := range byDomain {
		domainIds = append(domainIds, domainId)
	}
	sort.Strings(domainIds)
	for _, domainId := range domainIds {
		self.ByDomain = append(self.ByDomain, DomainPermissions{DomainID: domainId, Permissions: sortedPermissions(byDomain[domainId])})
	}
	return self
}

func sortedPermissions(set map[Permission]bool) []Permission {
	permissions := make([]Permission, 0, len(set))
	for permission := range set {
		permissions = append(permissions, permission)
	}
	sort.Slice(permissions, func(first, second int) bool { return permissions[first] < permissions[second] })
	return permissions
}

// Has reports whether a server or all-domains permission is held.
func (self *EffectivePermissions) Has(permission Permission) bool {
	return self != nil && slices.Contains(self.Everywhere, permission)
}

// HasOverDomain reports whether a domain permission is held over this domain:
// either granted over it through a group, or granted everywhere through the
// all-domains permission that widens it.
func (self *EffectivePermissions) HasOverDomain(permission Permission, domainId string) bool {
	if self == nil {
		return false
	}
	for _, candidate := range self.Everywhere {
		if candidate == permission || candidate.Widens() == permission {
			return true
		}
	}
	for _, entry := range self.ByDomain {
		if entry.DomainID == domainId && slices.Contains(entry.Permissions, permission) {
			return true
		}
	}
	return false
}

// HasAnywhere reports whether a domain permission is held over at least one
// domain, or everywhere.
func (self *EffectivePermissions) HasAnywhere(permission Permission) bool {
	if self == nil {
		return false
	}
	for _, candidate := range self.Everywhere {
		if candidate == permission || candidate.Widens() == permission {
			return true
		}
	}
	for _, entry := range self.ByDomain {
		if slices.Contains(entry.Permissions, permission) {
			return true
		}
	}
	return false
}

// DomainsWith lists the domains a domain permission is held over, and whether
// it is held over all of them.
func (self *EffectivePermissions) DomainsWith(permission Permission) (domainIds []string, all bool) {
	if self == nil {
		return nil, false
	}
	for _, candidate := range self.Everywhere {
		if candidate.Widens() == permission {
			return nil, true
		}
	}
	for _, entry := range self.ByDomain {
		if slices.Contains(entry.Permissions, permission) {
			domainIds = append(domainIds, entry.DomainID)
		}
	}
	return domainIds, false
}

// Manages reports whether the caller holds at least one permission that opens
// the management side of the web UI.
func (self *EffectivePermissions) Manages() bool {
	if self == nil {
		return false
	}
	for _, permission := range []Permission{
		PermissionMailAudit, PermissionDomainManage, PermissionQueueManage, PermissionReportRead,
		PermissionUserManage, PermissionGroupManage, PermissionRoleManage, PermissionServerManage, PermissionAuditRead,
	} {
		if self.HasAnywhere(permission) {
			return true
		}
	}
	return false
}
