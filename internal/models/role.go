package models

import "time"

// Role is a named set of permissions. Attached to groups, never to users: a
// user in a group holds the group's roles over the group's domains.
//
// Three are seeded and are ordinary rows from then on; there is no built-in
// flag and nothing a seeded role may not do.
type Role struct {
	ID          string       `json:"id"`
	CreatedAt   time.Time    `json:"createdAt"`
	ModifiedAt  time.Time    `json:"modifiedAt"`
	Name        string       `json:"name"`
	Description string       `json:"description,omitempty"`
	Permissions []Permission `json:"permissions"`
}

// The seeded roles, by name.
const (
	RoleNameAdministrator = "Administrator"
	RoleNameOperator      = "Operator"
	RoleNameMember        = "Member"
)

// SeededRolePermissions is what each seeded role holds on the day it is made.
func SeededRolePermissions(name string) []Permission {
	switch name {
	case RoleNameAdministrator:
		return Permissions()
	case RoleNameOperator:
		// Everything except managing who may do what, which is what every
		// user was before there were roles.
		var permissions []Permission
		for _, permission := range Permissions() {
			switch permission {
			case PermissionUserManage, PermissionGroupManage, PermissionRoleManage:
				continue
			}
			permissions = append(permissions, permission)
		}
		return permissions
	case RoleNameMember:
		// A person with an inbox and nothing else.
		return []Permission{PermissionMailRead, PermissionMailWrite, PermissionMailSend, PermissionMailboxManage}
	}
	return nil
}

// Validate reports everything wrong with the role.
func (self *Role) Validate() error {
	var errors ValidationErrors
	if self.Name == "" {
		errors.add("name", "required")
	} else if len(self.Name) > 128 {
		errors.add("name", "must be under 128 characters")
	}
	for index, permission := range self.Permissions {
		if !permission.IsValid() {
			errors.add("permissions", "%q is not a permission (entry %d)", permission, index)
		}
	}
	return errors.ErrOrNil()
}
