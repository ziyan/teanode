package models

import "time"

// Group is a named set of users, and the only thing a role or a domain is
// attached to: everyone in the group holds the group's roles, over the group's
// domains for the domain-kind permissions and everywhere for the rest.
type Group struct {
	ID          string    `json:"id"`
	CreatedAt   time.Time `json:"createdAt"`
	ModifiedAt  time.Time `json:"modifiedAt"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`

	// IDPGroup is the group's name at the identity provider; single sign-on
	// adds and removes users of a group that has one and never touches one
	// that does not.
	IDPGroup string `json:"idpGroup,omitempty"`

	// UserIDs is the user_group table: who is in the group.
	UserIDs []string `json:"userIds"`

	// RoleIDs is the group_role table: what every user in the group may do.
	RoleIDs []string `json:"roleIds"`

	// DomainIDs is the group_domain table: where the domain-kind permissions
	// of those roles apply.
	DomainIDs []string `json:"domainIds"`
}

// The seeded groups, by name.
const (
	GroupNameAdministrators = "Administrators"
	GroupNameMembers        = "Members"
)

// Validate reports everything wrong with the group.
func (self *Group) Validate() error {
	var errors ValidationErrors
	if self.Name == "" {
		errors.add("name", "required")
	} else if len(self.Name) > 128 {
		errors.add("name", "must be under 128 characters")
	}
	if len(self.IDPGroup) > 256 {
		errors.add("idpGroup", "must be under 256 characters")
	}
	return errors.ErrOrNil()
}
