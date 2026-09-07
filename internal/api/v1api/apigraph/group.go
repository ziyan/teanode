package apigraph

import (
	"context"
	"strings"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
)

type GroupQuery interface {
	// List every Group, with its users, roles and domains. Needs
	// group:manage or user:manage: a user's page shows their groups.
	ListGroups(ctx context.Context) ([]*models.Group, error)
}

type GroupMutation interface {
	// Add a Group
	CreateGroup(ctx context.Context, arguments CreateGroupArguments) (*models.Group, error)

	// Change a Group: its name, description, identity provider group, and
	// which users, roles and domains it has, each replacing the current list
	UpdateGroup(ctx context.Context, arguments UpdateGroupArguments) (*models.Group, error)

	// Remove a Group. Its users keep their accounts and lose what the group
	// gave them.
	DeleteGroup(ctx context.Context, arguments DeleteGroupArguments) error
}

// requireGroupReader is whoever may see the groups: those who edit them, and
// those who put users in them.
func (self *graph) requireGroupReader(ctx context.Context) (*api.Principal, error) {
	principal, err := self.requireSignedIn(ctx)
	if err != nil {
		return nil, err
	}
	if !principal.Permissions.Has(models.PermissionGroupManage) && !principal.Permissions.Has(models.PermissionUserManage) {
		return nil, api.ErrNotFound
	}
	return principal, nil
}

func (self *graph) ListGroups(ctx context.Context) ([]*models.Group, error) {
	if _, err := self.requireGroupReader(ctx); err != nil {
		return nil, err
	}
	return self.transaction(ctx).ListGroups()
}

type CreateGroupArguments struct {
	Name        string    `json:"name"`
	Description *string   `json:"description"`
	IDPGroup    *string   `json:"idpGroup"`
	UserIDs     *[]string `json:"userIds"`
	RoleIDs     *[]string `json:"roleIds"`
	DomainIDs   *[]string `json:"domainIds"`
}

func (self *graph) CreateGroup(ctx context.Context, arguments CreateGroupArguments) (*models.Group, error) {
	if _, err := self.requirePermission(ctx, models.PermissionGroupManage); err != nil {
		return nil, err
	}
	group := &models.Group{Name: strings.TrimSpace(arguments.Name)}
	if arguments.Description != nil {
		group.Description = strings.TrimSpace(*arguments.Description)
	}
	if arguments.IDPGroup != nil {
		group.IDPGroup = strings.TrimSpace(*arguments.IDPGroup)
	}
	if arguments.UserIDs != nil {
		group.UserIDs = *arguments.UserIDs
	}
	if arguments.RoleIDs != nil {
		group.RoleIDs = *arguments.RoleIDs
	}
	if arguments.DomainIDs != nil {
		group.DomainIDs = *arguments.DomainIDs
	}
	created, err := self.transaction(ctx).CreateGroup(group)
	if err != nil {
		return nil, translateError(err)
	}
	log.Noticef("%s created the group %q", operatorName(ctx), created.Name)
	return created, nil
}

type UpdateGroupArguments struct {
	GroupID     string    `json:"groupId"`
	Name        *string   `json:"name"`
	Description *string   `json:"description"`
	IDPGroup    *string   `json:"idpGroup"`
	UserIDs     *[]string `json:"userIds"`
	RoleIDs     *[]string `json:"roleIds"`
	DomainIDs   *[]string `json:"domainIds"`
}

func (self *graph) UpdateGroup(ctx context.Context, arguments UpdateGroupArguments) (*models.Group, error) {
	principal, err := self.requireSignedIn(ctx)
	if err != nil {
		return nil, err
	}
	// Changing who is in a group needs user:manage; changing what the group
	// is and does needs group:manage. Somebody with only the first may not
	// touch the roles or domains, which is where the reach comes from.
	membershipOnly := arguments.Name == nil && arguments.Description == nil && arguments.IDPGroup == nil &&
		arguments.RoleIDs == nil && arguments.DomainIDs == nil
	if membershipOnly {
		if !principal.Permissions.Has(models.PermissionGroupManage) && !principal.Permissions.Has(models.PermissionUserManage) {
			return nil, api.ErrNotFound
		}
	} else if !principal.Permissions.Has(models.PermissionGroupManage) {
		return nil, api.ErrNotFound
	}
	updated, err := self.transaction(ctx).UpdateGroup(arguments.GroupID, func(group *models.Group) error {
		if arguments.Name != nil {
			group.Name = strings.TrimSpace(*arguments.Name)
		}
		if arguments.Description != nil {
			group.Description = strings.TrimSpace(*arguments.Description)
		}
		if arguments.IDPGroup != nil {
			group.IDPGroup = strings.TrimSpace(*arguments.IDPGroup)
		}
		if arguments.UserIDs != nil {
			group.UserIDs = *arguments.UserIDs
		}
		if arguments.RoleIDs != nil {
			group.RoleIDs = *arguments.RoleIDs
		}
		if arguments.DomainIDs != nil {
			group.DomainIDs = *arguments.DomainIDs
		}
		return nil
	})
	if err != nil {
		return nil, translateError(err)
	}
	log.Noticef("%s changed the group %q", operatorName(ctx), updated.Name)
	return updated, nil
}

type DeleteGroupArguments struct {
	GroupID string `json:"groupId"`
}

func (self *graph) DeleteGroup(ctx context.Context, arguments DeleteGroupArguments) error {
	if _, err := self.requirePermission(ctx, models.PermissionGroupManage); err != nil {
		return err
	}
	if err := self.transaction(ctx).DeleteGroup(arguments.GroupID); err != nil {
		return translateError(err)
	}
	log.Noticef("%s removed the group %s", operatorName(ctx), arguments.GroupID)
	return nil
}
