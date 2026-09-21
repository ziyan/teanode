package client

import (
	"context"
	"encoding/json"
	"time"
)

// Group is a set of people, what they may do, and where. Permissions come
// from the group's roles; the domain-kind ones apply to the group's domains.
type Group struct {
	ID          string    `json:"id"`
	CreatedAt   time.Time `json:"createdAt"`
	ModifiedAt  time.Time `json:"modifiedAt"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	IDPGroup    string    `json:"idpGroup"`
	UserIDs     []string  `json:"userIds"`
	RoleIDs     []string  `json:"roleIds"`
	DomainIDs   []string  `json:"domainIds"`
}

// Role is a named set of permissions.
type Role struct {
	ID          string    `json:"id"`
	CreatedAt   time.Time `json:"createdAt"`
	ModifiedAt  time.Time `json:"modifiedAt"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Permissions []string  `json:"permissions"`
}

// PermissionDescription is one permission a role may hold: its key, whether
// it applies to the server or to a group's domains, and, for one that covers
// every domain, the domain permission it stands in for.
type PermissionDescription struct {
	Key    string `json:"key"`
	Kind   string `json:"kind"`
	Widens string `json:"widens"`
}

// AuditEvent is one administrative change: who made it, to what, and the row
// before and after.
type AuditEvent struct {
	ID           string          `json:"id"`
	CreatedAt    time.Time       `json:"createdAt"`
	ActorKind    string          `json:"actorKind"`
	ActorUserID  string          `json:"actorUserId"`
	ActorLabel   string          `json:"actorLabel"`
	TokenID      string          `json:"tokenId"`
	SourceIP     string          `json:"sourceIp"`
	ResourceType string          `json:"resourceType"`
	ResourceID   string          `json:"resourceId"`
	Action       string          `json:"action"`
	Before       json.RawMessage `json:"before"`
	After        json.RawMessage `json:"after"`
}

// AuditEventPage is a page of the audit log, and how many there are in all.
type AuditEventPage struct {
	Events []*AuditEvent `json:"events"`
	Total  int           `json:"total"`
}

const groupFields = `{ id createdAt modifiedAt name description idpGroup userIds roleIds domainIds }`

const roleFields = `{ id createdAt modifiedAt name description permissions }`

// The documents this file sends, named so that a test can validate each one
// against the schema the server builds.
const (
	DocumentListGroups  = `query { ListGroups ` + groupFields + ` }`
	DocumentCreateGroup = `mutation ($name: String!, $description: String, $idpGroup: String,
		$userIds: [String!], $roleIds: [String!], $domainIds: [String!]) {
		CreateGroup(name: $name, description: $description, idpGroup: $idpGroup,
			userIds: $userIds, roleIds: $roleIds, domainIds: $domainIds) ` + groupFields + `
	}`
	DocumentUpdateGroup = `mutation ($groupId: String!, $name: String, $description: String, $idpGroup: String,
		$userIds: [String!], $roleIds: [String!], $domainIds: [String!]) {
		UpdateGroup(groupId: $groupId, name: $name, description: $description, idpGroup: $idpGroup,
			userIds: $userIds, roleIds: $roleIds, domainIds: $domainIds) ` + groupFields + `
	}`
	DocumentDeleteGroup = `mutation ($groupId: String!) { DeleteGroup(groupId: $groupId) }`

	DocumentListRoles       = `query { ListRoles ` + roleFields + ` }`
	DocumentListPermissions = `query { ListPermissions { key kind widens } }`
	DocumentCreateRole      = `mutation ($name: String!, $description: String, $permissions: [String!]!) {
		CreateRole(name: $name, description: $description, permissions: $permissions) ` + roleFields + `
	}`
	DocumentUpdateRole = `mutation ($roleId: String!, $name: String, $description: String, $permissions: [String!]) {
		UpdateRole(roleId: $roleId, name: $name, description: $description, permissions: $permissions) ` + roleFields + `
	}`
	DocumentDeleteRole = `mutation ($roleId: String!) { DeleteRole(roleId: $roleId) }`

	DocumentListAuditEvents = `query ($resourceType: String, $resourceId: String, $actorUserId: String,
		$since: DateTime, $until: DateTime, $first: Int, $offset: Int) {
		ListAuditEvents(resourceType: $resourceType, resourceId: $resourceId, actorUserId: $actorUserId,
			since: $since, until: $until, first: $first, offset: $offset) {
			total
			events { id createdAt actorKind actorUserId actorLabel tokenId sourceIp resourceType resourceId action before after }
		}
	}`
)

// ListGroups returns the groups on this server.
func ListGroups(ctx context.Context, connection *Client) ([]*Group, error) {
	var result struct {
		ListGroups []*Group `json:"ListGroups"`
	}
	if err := connection.Execute(ctx, DocumentListGroups, nil, &result); err != nil {
		return nil, err
	}
	return result.ListGroups, nil
}

// GroupParameters is what a group is made or changed with. A nil field is
// left alone; a list replaces the one stored.
type GroupParameters struct {
	Name        *string
	Description *string
	IDPGroup    *string
	UserIDs     *[]string
	RoleIDs     *[]string
	DomainIDs   *[]string
}

func (self *GroupParameters) variables() map[string]any {
	variables := map[string]any{}
	if self.Name != nil {
		variables["name"] = *self.Name
	}
	if self.Description != nil {
		variables["description"] = *self.Description
	}
	if self.IDPGroup != nil {
		variables["idpGroup"] = *self.IDPGroup
	}
	if self.UserIDs != nil {
		variables["userIds"] = *self.UserIDs
	}
	if self.RoleIDs != nil {
		variables["roleIds"] = *self.RoleIDs
	}
	if self.DomainIDs != nil {
		variables["domainIds"] = *self.DomainIDs
	}
	return variables
}

// CreateGroup adds a group.
func CreateGroup(ctx context.Context, connection *Client, name string, parameters *GroupParameters) (*Group, error) {
	var result struct {
		CreateGroup *Group `json:"CreateGroup"`
	}
	variables := parameters.variables()
	variables["name"] = name
	if err := connection.Execute(ctx, DocumentCreateGroup, variables, &result); err != nil {
		return nil, err
	}
	return result.CreateGroup, nil
}

// UpdateGroup changes a group.
func UpdateGroup(ctx context.Context, connection *Client, groupId string, parameters *GroupParameters) (*Group, error) {
	var result struct {
		UpdateGroup *Group `json:"UpdateGroup"`
	}
	variables := parameters.variables()
	variables["groupId"] = groupId
	if err := connection.Execute(ctx, DocumentUpdateGroup, variables, &result); err != nil {
		return nil, err
	}
	return result.UpdateGroup, nil
}

// DeleteGroup removes a group. Its members keep their accounts and lose what
// the group gave them.
func DeleteGroup(ctx context.Context, connection *Client, groupId string) error {
	return connection.Execute(ctx, DocumentDeleteGroup, map[string]any{"groupId": groupId}, nil)
}

// ListRoles returns the roles on this server.
func ListRoles(ctx context.Context, connection *Client) ([]*Role, error) {
	var result struct {
		ListRoles []*Role `json:"ListRoles"`
	}
	if err := connection.Execute(ctx, DocumentListRoles, nil, &result); err != nil {
		return nil, err
	}
	return result.ListRoles, nil
}

// ListPermissions returns every permission a role may hold.
func ListPermissions(ctx context.Context, connection *Client) ([]*PermissionDescription, error) {
	var result struct {
		ListPermissions []*PermissionDescription `json:"ListPermissions"`
	}
	if err := connection.Execute(ctx, DocumentListPermissions, nil, &result); err != nil {
		return nil, err
	}
	return result.ListPermissions, nil
}

// RoleParameters is what a role is made or changed with.
type RoleParameters struct {
	Name        *string
	Description *string
	Permissions *[]string
}

// CreateRole adds a role.
func CreateRole(ctx context.Context, connection *Client, name string, permissions []string, description *string) (*Role, error) {
	var result struct {
		CreateRole *Role `json:"CreateRole"`
	}
	variables := map[string]any{"name": name, "permissions": permissions}
	if description != nil {
		variables["description"] = *description
	}
	if err := connection.Execute(ctx, DocumentCreateRole, variables, &result); err != nil {
		return nil, err
	}
	return result.CreateRole, nil
}

// UpdateRole changes a role.
func UpdateRole(ctx context.Context, connection *Client, roleId string, parameters *RoleParameters) (*Role, error) {
	var result struct {
		UpdateRole *Role `json:"UpdateRole"`
	}
	variables := map[string]any{"roleId": roleId}
	if parameters.Name != nil {
		variables["name"] = *parameters.Name
	}
	if parameters.Description != nil {
		variables["description"] = *parameters.Description
	}
	if parameters.Permissions != nil {
		variables["permissions"] = *parameters.Permissions
	}
	if err := connection.Execute(ctx, DocumentUpdateRole, variables, &result); err != nil {
		return nil, err
	}
	return result.UpdateRole, nil
}

// DeleteRole removes a role. A seeded role cannot be removed.
func DeleteRole(ctx context.Context, connection *Client, roleId string) error {
	return connection.Execute(ctx, DocumentDeleteRole, map[string]any{"roleId": roleId}, nil)
}

// AuditFilter narrows the audit log.
type AuditFilter struct {
	ResourceType string
	ResourceID   string
	ActorUserID  string
	Since        *time.Time
	Until        *time.Time
	First        int
	Offset       int
}

// ListAuditEvents returns a page of the audit log, newest first.
func ListAuditEvents(ctx context.Context, connection *Client, filter *AuditFilter) (*AuditEventPage, error) {
	var result struct {
		ListAuditEvents *AuditEventPage `json:"ListAuditEvents"`
	}
	variables := map[string]any{}
	if filter != nil {
		if filter.ResourceType != "" {
			variables["resourceType"] = filter.ResourceType
		}
		if filter.ResourceID != "" {
			variables["resourceId"] = filter.ResourceID
		}
		if filter.ActorUserID != "" {
			variables["actorUserId"] = filter.ActorUserID
		}
		if filter.Since != nil {
			variables["since"] = filter.Since.Format(time.RFC3339)
		}
		if filter.Until != nil {
			variables["until"] = filter.Until.Format(time.RFC3339)
		}
		if filter.First > 0 {
			variables["first"] = filter.First
		}
		if filter.Offset > 0 {
			variables["offset"] = filter.Offset
		}
	}
	if err := connection.Execute(ctx, DocumentListAuditEvents, variables, &result); err != nil {
		return nil, err
	}
	return result.ListAuditEvents, nil
}
