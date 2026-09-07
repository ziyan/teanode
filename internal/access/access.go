// Package access is who may do what: the seeded roles and groups, the checks
// a resolver makes against a caller's effective permissions, and the one
// rescue path that bypasses them.
//
// The model is groups, roles, permissions and users. Roles and domains attach
// only to groups; a user in a group holds the group's roles over the group's
// domains, additively across groups. Nothing attaches to a user directly.
package access

import (
	"fmt"

	"github.com/op/go-logging"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

var log = logging.MustGetLogger("access")

// EnsureSeeded creates the three roles and the two groups a fresh server
// starts with, when they are missing, and reports whether it made anything.
//
// Only when missing by name: a role somebody renamed or deleted is theirs to
// have renamed or deleted, and this must not put it back. The exception is a
// server with no groups at all, which is a server that has never had access
// control — every existing user goes into Administrators then, so that the
// day this lands nobody can do less than they could the day before.
func EnsureSeeded(tx db.Transaction) (bool, error) {
	roles, err := tx.ListRoles()
	if err != nil {
		return false, err
	}
	groups, err := tx.ListGroups()
	if err != nil {
		return false, err
	}
	if len(roles) > 0 || len(groups) > 0 {
		return false, nil
	}

	byName := map[string]*models.Role{}
	for _, name := range []string{models.RoleNameAdministrator, models.RoleNameOperator, models.RoleNameMember} {
		role, err := tx.CreateRole(&models.Role{
			Name:        name,
			Description: seededRoleDescription(name),
			Permissions: models.SeededRolePermissions(name),
		})
		if err != nil {
			return false, fmt.Errorf("access: cannot seed the %s role: %w", name, err)
		}
		byName[name] = role
	}

	users, err := tx.ListUsers()
	if err != nil {
		return false, err
	}
	userIds := make([]string, 0, len(users))
	for _, user := range users {
		userIds = append(userIds, user.ID)
	}

	if _, err := tx.CreateGroup(&models.Group{
		Name:        models.GroupNameAdministrators,
		Description: "Everyone here may do everything, over every domain.",
		UserIDs:     userIds,
		RoleIDs:     []string{byName[models.RoleNameAdministrator].ID},
	}); err != nil {
		return false, fmt.Errorf("access: cannot seed the %s group: %w", models.GroupNameAdministrators, err)
	}
	if _, err := tx.CreateGroup(&models.Group{
		Name:        models.GroupNameMembers,
		Description: "Everyone with a mailbox. New users join here.",
		UserIDs:     userIds,
		RoleIDs:     []string{byName[models.RoleNameMember].ID},
	}); err != nil {
		return false, fmt.Errorf("access: cannot seed the %s group: %w", models.GroupNameMembers, err)
	}
	log.Noticef("seeded the roles and groups; %d existing users are administrators", len(users))
	return true, nil
}

func seededRoleDescription(name string) string {
	switch name {
	case models.RoleNameAdministrator:
		return "Every permission, including deciding who may do what."
	case models.RoleNameOperator:
		return "Runs the server and its domains, but does not manage users, groups or roles."
	case models.RoleNameMember:
		return "Reads and sends from their own mailboxes, and nothing else."
	}
	return ""
}

// AddUserToGroups puts a user into the groups named, creating none: a group
// somebody deleted stays deleted.
func AddUserToGroups(tx db.Transaction, userId string, groupNames ...string) error {
	for _, name := range groupNames {
		group, err := tx.GetGroupByName(name)
		if err != nil {
			return err
		}
		if group == nil {
			continue
		}
		if _, err := tx.UpdateGroup(group.ID, func(group *models.Group) error {
			for _, existing := range group.UserIDs {
				if existing == userId {
					return nil
				}
			}
			group.UserIDs = append(group.UserIDs, userId)
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

// Rescue puts a user into a group that holds every permission, recreating
// the Administrator role and the Administrators group if they were deleted.
// The way back for an administrator who edited themselves out; run on the
// host, against the database, with no permission check, and recorded as such.
func Rescue(tx db.Transaction, username string) error {
	user, err := tx.GetUserByUsername(username)
	if err != nil {
		return err
	}
	if user == nil {
		return fmt.Errorf("access: there is no user %q", username)
	}
	role, err := tx.GetRoleByName(models.RoleNameAdministrator)
	if err != nil {
		return err
	}
	if role == nil {
		role, err = tx.CreateRole(&models.Role{
			Name:        models.RoleNameAdministrator,
			Description: seededRoleDescription(models.RoleNameAdministrator),
			Permissions: models.SeededRolePermissions(models.RoleNameAdministrator),
		})
		if err != nil {
			return err
		}
	} else if _, err := tx.UpdateRole(role.ID, func(role *models.Role) error {
		role.Permissions = models.Permissions()
		return nil
	}); err != nil {
		return err
	}
	group, err := tx.GetGroupByName(models.GroupNameAdministrators)
	if err != nil {
		return err
	}
	if group == nil {
		_, err = tx.CreateGroup(&models.Group{
			Name:        models.GroupNameAdministrators,
			Description: "Everyone here may do everything, over every domain.",
			UserIDs:     []string{user.ID},
			RoleIDs:     []string{role.ID},
		})
		return err
	}
	_, err = tx.UpdateGroup(group.ID, func(group *models.Group) error {
		if !contains(group.UserIDs, user.ID) {
			group.UserIDs = append(group.UserIDs, user.ID)
		}
		if !contains(group.RoleIDs, role.ID) {
			group.RoleIDs = append(group.RoleIDs, role.ID)
		}
		return nil
	})
	return err
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

// EnsureMailbox gives a user their mailbox when they have none: "Personal",
// with the folders every mail program expects. Reports whether it made one.
func EnsureMailbox(tx db.Transaction, user *models.User) (bool, error) {
	if user == nil {
		return false, nil
	}
	mailboxes, err := tx.ListMailboxes(user.ID)
	if err != nil {
		return false, err
	}
	if len(mailboxes) > 0 {
		return false, nil
	}
	if _, err := tx.CreateMailbox(&models.Mailbox{UserID: user.ID, Name: "Personal"}); err != nil {
		return false, fmt.Errorf("access: cannot create a mailbox for %q: %w", user.Username, err)
	}
	return true, nil
}

// EnsureMailboxes gives every user who has none a mailbox: what a server
// that predates mailboxes runs on the first start with them.
func EnsureMailboxes(tx db.Transaction) (int, error) {
	users, err := tx.ListUsers()
	if err != nil {
		return 0, err
	}
	made := 0
	for _, user := range users {
		created, err := EnsureMailbox(tx, user)
		if err != nil {
			return made, err
		}
		if created {
			made++
		}
	}
	return made, nil
}

// CanReadMail says whether a person may see a message: as the operator of
// its domain, holding mail:audit over it, or as the owner of a mailbox
// holding it, with mail:read.
func CanReadMail(tx db.Transaction, user *models.User, permissions *models.EffectivePermissions, mail *models.Mail) (bool, error) {
	if mail == nil || permissions == nil {
		return false, nil
	}
	if mail.DomainID != "" && permissions.HasOverDomain(models.PermissionMailAudit, mail.DomainID) {
		return true, nil
	}
	if user == nil || !permissions.Has(models.PermissionMailRead) {
		return false, nil
	}
	items, err := tx.ListItemsByMail(mail.ID)
	if err != nil || len(items) == 0 {
		return false, err
	}
	mailboxes, err := tx.ListMailboxes(user.ID)
	if err != nil {
		return false, err
	}
	owned := map[string]bool{}
	for _, mailbox := range mailboxes {
		owned[mailbox.ID] = true
	}
	for _, item := range items {
		folder, err := tx.GetFolder(item.FolderID)
		if err != nil {
			return false, err
		}
		if folder != nil && owned[folder.MailboxID] {
			return true, nil
		}
	}
	return false, nil
}
