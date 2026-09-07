package models

import (
	"testing"
)

// Every permission has a kind, and an all-domains permission stands in for
// the domain permission it widens. A key the code does not know has no kind
// and grants nothing.
func TestEveryPermissionHasAKind(t *testing.T) {
	t.Parallel()

	for _, permission := range Permissions() {
		if permission.Kind() == "" {
			t.Errorf("%s has no kind", permission)
		}
		if permission.Kind() == PermissionKindAllDomains && permission.Widens().Kind() != PermissionKindDomain {
			t.Errorf("%s widens %q, which is not a domain permission", permission, permission.Widens())
		}
	}
	if Permission("carrier:pigeon").Kind() != "" || Permission("carrier:pigeon").IsValid() {
		t.Error("an unknown key has a kind")
	}
}

// The reach a request ends up with: server permissions everywhere, domain
// permissions over the group's domains, all-domains permissions everywhere
// and standing in for their domain permission on any domain.
func TestEffectivePermissionsReach(t *testing.T) {
	t.Parallel()

	permissions := NewEffectivePermissions([]Grant{
		{Permission: PermissionUserManage},
		{Permission: PermissionMailAudit, DomainID: "one.test"},
		{Permission: PermissionDomainManageAll},
		// A domain permission with no domain grants nothing.
		{Permission: PermissionMailAudit},
		// An unknown key grants nothing either.
		{Permission: "carrier:pigeon", DomainID: "one.test"},
	})

	if !permissions.Has(PermissionUserManage) || permissions.Has(PermissionRoleManage) {
		t.Error("server permissions are not what was granted")
	}
	if !permissions.HasOverDomain(PermissionMailAudit, "one.test") || permissions.HasOverDomain(PermissionMailAudit, "two.test") {
		t.Error("a domain permission reaches the wrong domains")
	}
	if !permissions.HasOverDomain(PermissionDomainManage, "two.test") {
		t.Error("an all-domains permission does not reach a domain nobody listed")
	}
	if !permissions.HasAnywhere(PermissionMailAudit) || permissions.HasAnywhere(PermissionQueueManage) {
		t.Error("HasAnywhere is wrong")
	}
	domains, all := permissions.DomainsWith(PermissionMailAudit)
	if all || len(domains) != 1 || domains[0] != "one.test" {
		t.Errorf("DomainsWith(mail:audit) = %v, %v", domains, all)
	}
	if _, all := permissions.DomainsWith(PermissionDomainManage); !all {
		t.Error("DomainsWith(domain:manage) should be every domain")
	}
	if !permissions.Manages() {
		t.Error("a user manager does not open the management side")
	}
	if NewEffectivePermissions([]Grant{{Permission: PermissionMailRead}}).Manages() {
		t.Error("a member opens the management side")
	}
}

// The seeded roles: an administrator holds everything, an operator
// everything but the access model, a member their own mailbox.
func TestSeededRoles(t *testing.T) {
	t.Parallel()

	if len(SeededRolePermissions(RoleNameAdministrator)) != len(Permissions()) {
		t.Error("the administrator does not hold every permission")
	}
	for _, permission := range SeededRolePermissions(RoleNameOperator) {
		switch permission {
		case PermissionUserManage, PermissionGroupManage, PermissionRoleManage:
			t.Errorf("the operator holds %s", permission)
		}
	}
	member := NewEffectivePermissions([]Grant{{Permission: PermissionMailRead}, {Permission: PermissionMailSend}})
	for _, permission := range SeededRolePermissions(RoleNameMember) {
		if permission.Kind() != PermissionKindServer {
			t.Errorf("the member's %s is not a server permission", permission)
		}
	}
	if member.Manages() {
		t.Error("a member manages")
	}
}
