// Package people is the operator's people and access: accounts, groups,
// roles, and the audit log.
package people

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/agent/tools/operator"
	"github.com/ziyan/teanode/internal/models"
)

func init() {
	tools.Register(func() []*tools.Tool {
		return []*tools.Tool{
			{
				Name: "user_list", Family: tools.FamilyPeople, Risk: tools.RiskRead, Permissions: []models.Permission{models.PermissionUserManage},
				Description: "The accounts on this server, with their groups and whether they may sign in.",
				Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
					result, err := operator.Execute(ctx, `query { ListUsers { id username name email disabledAt groupIds locale timezone } }`, nil)
					if err != nil {
						return nil, err
					}
					return tools.JSONResult(map[string]any{"users": result["ListUsers"]})
				},
			},
			{
				Name: "user_add", Family: tools.FamilyPeople, Risk: tools.RiskWrite, Permissions: []models.Permission{models.PermissionUserManage},
				Description: "Make an account. Without a password the person signs in another way — a passkey, single sign-on, or a password set later.",
				Parameters: tools.Object(map[string]any{
					"username":  tools.StringProperty("the username"),
					"name":      tools.StringProperty("what to call them"),
					"email":     tools.StringProperty("where notifications go"),
					"group_ids": tools.ArrayProperty("the groups to put them in", tools.StringProperty("a group id, from group_list")),
				}, "username"),
				Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
					arguments, err := tools.DecodeArguments[struct {
						Username string   `json:"username"`
						Name     *string  `json:"name"`
						Email    *string  `json:"email"`
						GroupIDs []string `json:"group_ids"`
					}](call)
					if err != nil {
						return nil, err
					}
					variables := map[string]any{"username": strings.TrimSpace(arguments.Username)}
					if arguments.Name != nil {
						variables["name"] = *arguments.Name
					}
					if arguments.Email != nil {
						variables["email"] = *arguments.Email
					}
					if arguments.GroupIDs != nil {
						variables["groupIds"] = arguments.GroupIDs
					}
					result, err := operator.Execute(ctx, `mutation ($username: String!, $name: String, $email: String, $groupIds: [String!]) { CreateUser(username: $username, name: $name, email: $email, groupIds: $groupIds) { id username } }`, variables)
					if err != nil {
						return nil, err
					}
					answer, err := tools.JSONResult(result["CreateUser"])
					if err != nil {
						return nil, err
					}
					answer.Note = "made the account " + arguments.Username
					return answer, nil
				},
			},
			{
				Name: "user_update", Family: tools.FamilyPeople, Risk: tools.RiskWrite, Permissions: []models.Permission{models.PermissionUserManage},
				Description: "Change an account: name, notification address, groups, or whether it may sign in.",
				Parameters: tools.Object(map[string]any{
					"user_id":   tools.StringProperty("the account, from user_list"),
					"name":      tools.StringProperty("what to call them"),
					"email":     tools.StringProperty("where notifications go"),
					"group_ids": tools.ArrayProperty("the groups, replacing the current ones", tools.StringProperty("a group id")),
					"disabled":  tools.BooleanProperty("whether they may not sign in"),
				}, "user_id"),
				Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
					arguments, err := tools.DecodeArguments[struct {
						UserID   string   `json:"user_id"`
						Name     *string  `json:"name"`
						Email    *string  `json:"email"`
						GroupIDs []string `json:"group_ids"`
						Disabled *bool    `json:"disabled"`
					}](call)
					if err != nil {
						return nil, err
					}
					variables := map[string]any{"userId": arguments.UserID}
					if arguments.Name != nil {
						variables["name"] = *arguments.Name
					}
					if arguments.Email != nil {
						variables["email"] = *arguments.Email
					}
					if arguments.GroupIDs != nil {
						variables["groupIds"] = arguments.GroupIDs
					}
					if arguments.Disabled != nil {
						variables["disabled"] = *arguments.Disabled
					}
					if _, err := operator.Execute(ctx, `mutation ($userId: String!, $name: String, $email: String, $groupIds: [String!], $disabled: Boolean) { UpdateUser(userId: $userId, name: $name, email: $email, groupIds: $groupIds, disabled: $disabled) { id } }`, variables); err != nil {
						return nil, err
					}
					return tools.TextResult("changed the account"), nil
				},
			},
			{
				Name: "user_remove", Family: tools.FamilyPeople, Risk: tools.RiskDestructive, Permissions: []models.Permission{models.PermissionUserManage},
				Description: "Delete an account and what only it held. Cannot be undone; disabling is the reversible choice.",
				Parameters:  tools.Object(map[string]any{"user_id": tools.StringProperty("the account, from user_list")}, "user_id"),
				Preview: func(arguments json.RawMessage) string {
					return "Delete the account " + strings.TrimSpace(string(arguments))
				},
				Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
					arguments, err := tools.DecodeArguments[struct {
						UserID string `json:"user_id"`
					}](call)
					if err != nil {
						return nil, err
					}
					if _, err := operator.Execute(ctx, `mutation ($userId: String!) { DeleteUser(userId: $userId) }`, map[string]any{"userId": arguments.UserID}); err != nil {
						return nil, err
					}
					return tools.TextResult("deleted the account"), nil
				},
			},
			{
				Name: "group_list", Family: tools.FamilyPeople, Risk: tools.RiskRead, Permissions: []models.Permission{models.PermissionGroupManage, models.PermissionUserManage},
				Description: "The groups: who is in each, which roles it carries, which domains it reaches.",
				Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
					result, err := operator.Execute(ctx, `query { ListGroups { id name description idpGroup userIds roleIds domainIds } }`, nil)
					if err != nil {
						return nil, err
					}
					return tools.JSONResult(map[string]any{"groups": result["ListGroups"]})
				},
			},
			{
				Name: "group_manage", Family: tools.FamilyPeople, Risk: tools.RiskWrite, Permissions: []models.Permission{models.PermissionGroupManage},
				Description: "Make, change or delete a group: its members, roles and domains. Deleting asks first.",
				Parameters: tools.Object(map[string]any{
					"action":      tools.EnumProperty("what to do", "create", "update", "delete"),
					"group_id":    tools.StringProperty("for update and delete: the group"),
					"name":        tools.StringProperty("the name"),
					"description": tools.StringProperty("what it is for"),
					"user_ids":    tools.ArrayProperty("the members, replacing the current ones", tools.StringProperty("a user id")),
					"role_ids":    tools.ArrayProperty("the roles, replacing the current ones", tools.StringProperty("a role id")),
					"domain_ids":  tools.ArrayProperty("the domains, replacing the current ones", tools.StringProperty("a domain id")),
				}, "action"),
				RiskOf: func(arguments json.RawMessage) tools.Risk {
					if strings.Contains(string(arguments), `"delete"`) {
						return tools.RiskDestructive
					}
					return tools.RiskWrite
				},
				Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
					arguments, err := tools.DecodeArguments[struct {
						Action      string   `json:"action"`
						GroupID     string   `json:"group_id"`
						Name        *string  `json:"name"`
						Description *string  `json:"description"`
						UserIDs     []string `json:"user_ids"`
						RoleIDs     []string `json:"role_ids"`
						DomainIDs   []string `json:"domain_ids"`
					}](call)
					if err != nil {
						return nil, err
					}
					variables := map[string]any{}
					if arguments.Name != nil {
						variables["name"] = *arguments.Name
					}
					if arguments.Description != nil {
						variables["description"] = *arguments.Description
					}
					if arguments.UserIDs != nil {
						variables["userIds"] = arguments.UserIDs
					}
					if arguments.RoleIDs != nil {
						variables["roleIds"] = arguments.RoleIDs
					}
					if arguments.DomainIDs != nil {
						variables["domainIds"] = arguments.DomainIDs
					}
					switch arguments.Action {
					case "create":
						if arguments.Name == nil {
							return nil, fmt.Errorf("a group needs a name")
						}
						result, err := operator.Execute(ctx, `mutation ($name: String!, $description: String, $userIds: [String!], $roleIds: [String!], $domainIds: [String!]) { CreateGroup(name: $name, description: $description, userIds: $userIds, roleIds: $roleIds, domainIds: $domainIds) { id name } }`, variables)
						if err != nil {
							return nil, err
						}
						return tools.JSONResult(result["CreateGroup"])
					case "update":
						variables["groupId"] = arguments.GroupID
						if _, err := operator.Execute(ctx, `mutation ($groupId: String!, $name: String, $description: String, $userIds: [String!], $roleIds: [String!], $domainIds: [String!]) { UpdateGroup(groupId: $groupId, name: $name, description: $description, userIds: $userIds, roleIds: $roleIds, domainIds: $domainIds) { id } }`, variables); err != nil {
							return nil, err
						}
						return tools.TextResult("changed the group"), nil
					case "delete":
						if _, err := operator.Execute(ctx, `mutation ($groupId: String!) { DeleteGroup(groupId: $groupId) }`, map[string]any{"groupId": arguments.GroupID}); err != nil {
							return nil, err
						}
						return tools.TextResult("deleted the group"), nil
					}
					return nil, fmt.Errorf("%q is not an action of group_manage", arguments.Action)
				},
			},
			{
				Name: "role_list", Family: tools.FamilyPeople, Risk: tools.RiskRead, Permissions: []models.Permission{models.PermissionRoleManage, models.PermissionGroupManage, models.PermissionUserManage},
				Description: "The roles and the permissions each carries, and every permission that exists with what it means.",
				Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
					result, err := operator.Execute(ctx, `query { ListRoles { id name description permissions } ListPermissions { key kind widens } }`, nil)
					if err != nil {
						return nil, err
					}
					return tools.JSONResult(map[string]any{"roles": result["ListRoles"], "permissions": result["ListPermissions"]})
				},
			},
			{
				Name: "role_manage", Family: tools.FamilyPeople, Risk: tools.RiskWrite, Permissions: []models.Permission{models.PermissionRoleManage},
				Description: "Make, change or delete a role and its permissions. Deleting asks first.",
				Parameters: tools.Object(map[string]any{
					"action":      tools.EnumProperty("what to do", "create", "update", "delete"),
					"role_id":     tools.StringProperty("for update and delete: the role"),
					"name":        tools.StringProperty("the name"),
					"description": tools.StringProperty("what it is for"),
					"permissions": tools.ArrayProperty("the permissions, replacing the current ones", tools.StringProperty("a permission such as mail:read")),
				}, "action"),
				RiskOf: func(arguments json.RawMessage) tools.Risk {
					if strings.Contains(string(arguments), `"delete"`) {
						return tools.RiskDestructive
					}
					return tools.RiskWrite
				},
				Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
					arguments, err := tools.DecodeArguments[struct {
						Action      string   `json:"action"`
						RoleID      string   `json:"role_id"`
						Name        *string  `json:"name"`
						Description *string  `json:"description"`
						Permissions []string `json:"permissions"`
					}](call)
					if err != nil {
						return nil, err
					}
					variables := map[string]any{}
					if arguments.Name != nil {
						variables["name"] = *arguments.Name
					}
					if arguments.Description != nil {
						variables["description"] = *arguments.Description
					}
					if arguments.Permissions != nil {
						variables["permissions"] = arguments.Permissions
					}
					switch arguments.Action {
					case "create":
						if arguments.Name == nil {
							return nil, fmt.Errorf("a role needs a name")
						}
						if variables["permissions"] == nil {
							variables["permissions"] = []string{}
						}
						result, err := operator.Execute(ctx, `mutation ($name: String!, $description: String, $permissions: [String!]!) { CreateRole(name: $name, description: $description, permissions: $permissions) { id name } }`, variables)
						if err != nil {
							return nil, err
						}
						return tools.JSONResult(result["CreateRole"])
					case "update":
						variables["roleId"] = arguments.RoleID
						if _, err := operator.Execute(ctx, `mutation ($roleId: String!, $name: String, $description: String, $permissions: [String!]) { UpdateRole(roleId: $roleId, name: $name, description: $description, permissions: $permissions) { id } }`, variables); err != nil {
							return nil, err
						}
						return tools.TextResult("changed the role"), nil
					case "delete":
						if _, err := operator.Execute(ctx, `mutation ($roleId: String!) { DeleteRole(roleId: $roleId) }`, map[string]any{"roleId": arguments.RoleID}); err != nil {
							return nil, err
						}
						return tools.TextResult("deleted the role"), nil
					}
					return nil, fmt.Errorf("%q is not an action of role_manage", arguments.Action)
				},
			},
			{
				Name: "audit_log", Family: tools.FamilyPeople, Risk: tools.RiskRead, Permissions: []models.Permission{models.PermissionAuditRead},
				Description: "Who changed what, and when: the audit trail, newest first, narrowed by resource, actor or time.",
				Parameters: tools.Object(map[string]any{
					"resource_type": tools.StringProperty("domain, alias, credential, user, group, role, settings, mailbox, agent…"),
					"resource_id":   tools.StringProperty("one resource"),
					"actor_user_id": tools.StringProperty("one person's doings"),
					"since":         tools.StringProperty("an ISO date"),
					"limit":         tools.IntegerProperty("how many, 30 by default"),
				}),
				Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
					arguments, err := tools.DecodeArguments[struct {
						ResourceType string `json:"resource_type"`
						ResourceID   string `json:"resource_id"`
						ActorUserID  string `json:"actor_user_id"`
						Since        string `json:"since"`
						Limit        int    `json:"limit"`
					}](call)
					if err != nil {
						return nil, err
					}
					variables := map[string]any{"first": 30}
					if arguments.Limit > 0 {
						variables["first"] = arguments.Limit
					}
					for key, value := range map[string]string{"resourceType": arguments.ResourceType, "resourceId": arguments.ResourceID, "actorUserId": arguments.ActorUserID} {
						if value != "" {
							variables[key] = value
						}
					}
					if arguments.Since != "" {
						since, err := tools.ParseTime(arguments.Since, tools.Location(tools.MustRun(ctx).Owner()), time.Now())
						if err != nil {
							return nil, err
						}
						variables["since"] = since.UTC().Format(time.RFC3339)
					}
					result, err := operator.Execute(ctx, `query ($resourceType: String, $resourceId: String, $actorUserId: String, $since: DateTime, $first: Int) { ListAuditEvents(resourceType: $resourceType, resourceId: $resourceId, actorUserId: $actorUserId, since: $since, first: $first) { total events { id createdAt actorKind actorLabel resourceType resourceId resourceLabel action } } }`, variables)
					if err != nil {
						return nil, err
					}
					return tools.JSONResult(result["ListAuditEvents"])
				},
			},
		}
	})
}
