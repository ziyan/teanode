package apigraph

import (
	"context"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
)

type RoleQuery interface {
	// List every Role. Needs role:manage or group:manage: a group's page
	// shows which roles it carries.
	ListRoles(ctx context.Context) ([]*models.Role, error)

	// List the whole permission vocabulary, with what each one reaches
	ListPermissions(ctx context.Context) ([]*PermissionDescription, error)
}

type RoleMutation interface {
	// Add a Role
	CreateRole(ctx context.Context, arguments CreateRoleArguments) (*models.Role, error)

	// Change a Role: its name, description, or the permissions it carries,
	// replacing the current set
	UpdateRole(ctx context.Context, arguments UpdateRoleArguments) (*models.Role, error)

	// Remove a Role. Every group that carried it loses it.
	DeleteRole(ctx context.Context, arguments DeleteRoleArguments) error
}

// PermissionDescription is one permission and how far it reaches.
type PermissionDescription struct {
	Key models.Permission `json:"key"`

	// One of server, domain or all-domains
	Kind models.PermissionKind `json:"kind"`

	// For an all-domains permission, the domain permission it stands in for
	// on every domain
	Widens models.Permission `json:"widens,omitempty"`
}

func (self *graph) ListPermissions(ctx context.Context) ([]*PermissionDescription, error) {
	if _, err := self.requireRoleReader(ctx); err != nil {
		return nil, err
	}
	permissions := models.Permissions()
	described := make([]*PermissionDescription, 0, len(permissions))
	for _, permission := range permissions {
		described = append(described, &PermissionDescription{Key: permission, Kind: permission.Kind(), Widens: permission.Widens()})
	}
	return described, nil
}

// requireRoleReader is whoever may see the roles: those who edit them, and
// those who attach them to groups.
// mayWrite refuses to let somebody put a permission into a role that they do
// not hold themselves.
//
// A role is the permissions, and whoever may write one decides what everybody
// carrying it may do. Without this, the permission to manage roles was the
// permission to hold every other one: write them into a role, put the role on
// a group you are in, and the next request is answered with all of them.
func mayWrite(principal *api.Principal, permissions []models.Permission) error {
	for _, permission := range permissions {
		switch permission.Kind() {
		case models.PermissionKindServer, models.PermissionKindAllDomains:
			if !principal.Permissions.Has(permission) {
				return fmt.Errorf("%w: you do not hold %s, so it is not yours to give",
					api.ErrPermissionDenied, permission)
			}
		case models.PermissionKindDomain:
			// A role says what its holders may do over whichever domains
			// the group carrying it names, so the question is whether this
			// person holds it anywhere at all.
			if !principal.Permissions.HasAnywhere(permission) {
				return fmt.Errorf("%w: you do not hold %s over any domain, so it is not yours to give",
					api.ErrPermissionDenied, permission)
			}
		}
	}
	return nil
}

func (self *graph) requireRoleReader(ctx context.Context) (*api.Principal, error) {
	principal, err := self.requireSignedIn(ctx)
	if err != nil {
		return nil, err
	}
	if !principal.Permissions.Has(models.PermissionRoleManage) && !principal.Permissions.Has(models.PermissionGroupManage) {
		return nil, api.ErrNotFound
	}
	return principal, nil
}

func (self *graph) ListRoles(ctx context.Context) ([]*models.Role, error) {
	if _, err := self.requireRoleReader(ctx); err != nil {
		return nil, err
	}
	return self.transaction(ctx).ListRoles()
}

type CreateRoleArguments struct {
	Name        string              `json:"name"`
	Description *string             `json:"description"`
	Permissions []models.Permission `json:"permissions"`
}

func (self *graph) CreateRole(ctx context.Context, arguments CreateRoleArguments) (*models.Role, error) {
	principal, err := self.requirePermission(ctx, models.PermissionRoleManage)
	if err != nil {
		return nil, err
	}
	if err := mayWrite(principal, arguments.Permissions); err != nil {
		return nil, err
	}
	role := &models.Role{Name: strings.TrimSpace(arguments.Name), Permissions: arguments.Permissions}
	if arguments.Description != nil {
		role.Description = strings.TrimSpace(*arguments.Description)
	}
	created, err := self.transaction(ctx).CreateRole(role)
	if err != nil {
		return nil, translateError(err)
	}
	log.Noticef("%s created the role %q", operatorName(ctx), created.Name)
	return created, nil
}

type UpdateRoleArguments struct {
	RoleID      string               `json:"roleId"`
	Name        *string              `json:"name"`
	Description *string              `json:"description"`
	Permissions *[]models.Permission `json:"permissions"`
}

func (self *graph) UpdateRole(ctx context.Context, arguments UpdateRoleArguments) (*models.Role, error) {
	principal, err := self.requirePermission(ctx, models.PermissionRoleManage)
	if err != nil {
		return nil, err
	}
	if arguments.Permissions != nil {
		if err := mayWrite(principal, *arguments.Permissions); err != nil {
			return nil, err
		}
	}
	updated, err := self.transaction(ctx).UpdateRole(arguments.RoleID, func(role *models.Role) error {
		if arguments.Name != nil {
			role.Name = strings.TrimSpace(*arguments.Name)
		}
		if arguments.Description != nil {
			role.Description = strings.TrimSpace(*arguments.Description)
		}
		if arguments.Permissions != nil {
			role.Permissions = *arguments.Permissions
		}
		return nil
	})
	if err != nil {
		return nil, translateError(err)
	}
	log.Noticef("%s changed the role %q", operatorName(ctx), updated.Name)
	return updated, nil
}

type DeleteRoleArguments struct {
	RoleID string `json:"roleId"`
}

func (self *graph) DeleteRole(ctx context.Context, arguments DeleteRoleArguments) error {
	if _, err := self.requirePermission(ctx, models.PermissionRoleManage); err != nil {
		return err
	}
	if err := self.transaction(ctx).DeleteRole(arguments.RoleID); err != nil {
		return translateError(err)
	}
	log.Noticef("%s removed the role %s", operatorName(ctx), arguments.RoleID)
	return nil
}
