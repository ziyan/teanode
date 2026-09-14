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
		return tools.Grouped([]*tools.Tool{
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
				// Granting: an account in a group holds whatever that
				// group holds, from its first sign-in.
				Name: "user_add", Family: tools.FamilyPeople, Risk: tools.RiskGranting, Permissions: []models.Permission{models.PermissionUserManage},
				Description: "Make an account. Without a password the person signs in another way — a passkey, single sign-on, or a password set later.",
				Parameters: tools.Object(map[string]any{
					"username":  tools.StringProperty("the username"),
					"name":      tools.StringProperty("what to call them"),
					"email":     tools.StringProperty("where notifications go"),
					"group_ids": tools.ArrayProperty("the groups to put them in", tools.StringProperty("a group id, from group_list")),
				}, "username"),
				Preview: tools.PreviewOf(func(call struct {
					Username string   `json:"username"`
					GroupIDs []string `json:"group_ids"`
				}) string {
					// An account on this server, and the groups are what
					// it can do -- so the count is said rather than left
					// to the ids nobody can read.
					said := "Make an account for " + tools.Named(call.Username, "somebody")
					if len(call.GroupIDs) > 0 {
						said += ", in " + counted(len(call.GroupIDs), "group", "groups")
					}
					return said
				}),
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
				// Changing the groups an account is in is changing what it
				// may do, and moving one into the administrators is the
				// shortest way to everything. Asked about whenever the
				// call names groups; renaming somebody is an ordinary
				// write.
				Name: "user_update", Family: tools.FamilyPeople, Risk: tools.RiskWrite, Permissions: []models.Permission{models.PermissionUserManage},
				RiskOf: func(arguments json.RawMessage) tools.Risk {
					var asked struct {
						GroupIDs []string `json:"group_ids"`
						Disabled *bool    `json:"disabled"`
					}
					if err := json.Unmarshal(arguments, &asked); err == nil {
						if asked.GroupIDs != nil {
							return tools.RiskGranting
						}
						if asked.Disabled != nil {
							return tools.RiskDestructive
						}
					}
					return tools.RiskWrite
				},
				Description: "Change an account: name, notification address, groups, or whether it may sign in.",
				Parameters: tools.Object(map[string]any{
					"user_id":   tools.StringProperty("the account, from user_list"),
					"name":      tools.StringProperty("what to call them"),
					"email":     tools.StringProperty("where notifications go"),
					"group_ids": tools.ArrayProperty("the groups, replacing the current ones", tools.StringProperty("a group id")),
					"disabled":  tools.BooleanProperty("whether they may not sign in"),
				}, "user_id"),
				Preview: tools.PreviewOf(func(call struct {
					GroupIDs []string `json:"group_ids"`
					Disabled *bool    `json:"disabled"`
					Name     *string  `json:"name"`
					Email    *string  `json:"email"`
				}) string {
					// The two that matter are said first: locking somebody
					// out, and setting what they may do.
					if call.Disabled != nil {
						if *call.Disabled {
							return "Stop somebody signing in to this server"
						}
						return "Let somebody sign in again"
					}
					if call.GroupIDs != nil {
						if len(call.GroupIDs) == 0 {
							return "Take somebody out of every group, leaving them nothing they may do here"
						}
						return "Set what somebody may do here: " + counted(len(call.GroupIDs), "group", "groups")
					}
					changing := []string{}
					if call.Name != nil {
						changing = append(changing, "their name")
					}
					if call.Email != nil {
						changing = append(changing, "their notification address")
					}
					if len(changing) == 0 {
						return "Change an account"
					}
					return "Change " + tools.Some(changing, 2)
				}),
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
				Preview: tools.PreviewOf(func(struct{}) string {
					return "Delete an account and what only it held; this cannot be undone"
				}),
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
				// A group is a bundle of permissions and the people who
				// hold them, so every change to one is a change to who may
				// do what -- including adding oneself to a group that
				// already holds everything, which needs no permission
				// beyond the one to manage groups.
				Name: "group_manage", Family: tools.FamilyPeople, Risk: tools.RiskGranting, Permissions: []models.Permission{models.PermissionGroupManage},
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
				Preview: tools.PreviewOf(func(call struct {
					Action  string   `json:"action"`
					Name    string   `json:"name"`
					UserIDs []string `json:"user_ids"`
					RoleIDs []string `json:"role_ids"`
				}) string {
					named := tools.Named(call.Name, "a group")
					switch call.Action {
					case "create":
						return "Make the group " + named
					case "delete":
						return "Delete the group " + named + ", taking away what it gave its members"
					}
					// Replacing rather than adding: the lists are the
					// whole membership, so this can take access away as
					// easily as it gives it.
					changing := []string{}
					if call.UserIDs != nil {
						changing = append(changing, counted(len(call.UserIDs), "member", "members"))
					}
					if call.RoleIDs != nil {
						changing = append(changing, counted(len(call.RoleIDs), "role", "roles"))
					}
					if len(changing) == 0 {
						return "Change the group " + named
					}
					return "Set the group " + named + " to " + tools.Some(changing, 2)
				}),
				RiskOf: func(arguments json.RawMessage) tools.Risk {
					if tools.ActionOf(arguments) == "delete" {
						return tools.RiskDestructive
					}
					return tools.RiskGranting
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
				// A role is the permissions themselves.
				Name: "role_manage", Family: tools.FamilyPeople, Risk: tools.RiskGranting, Permissions: []models.Permission{models.PermissionRoleManage},
				Description: "Make, change or delete a role and its permissions. Deleting asks first.",
				Parameters: tools.Object(map[string]any{
					"action":      tools.EnumProperty("what to do", "create", "update", "delete"),
					"role_id":     tools.StringProperty("for update and delete: the role"),
					"name":        tools.StringProperty("the name"),
					"description": tools.StringProperty("what it is for"),
					"permissions": tools.ArrayProperty("the permissions, replacing the current ones", tools.StringProperty("a permission such as mail:read")),
				}, "action"),
				Preview: tools.PreviewOf(func(call struct {
					Action      string   `json:"action"`
					Name        string   `json:"name"`
					Permissions []string `json:"permissions"`
				}) string {
					named := tools.Named(call.Name, "a role")
					switch call.Action {
					case "create":
						if said := tools.Some(call.Permissions, 4); said != "" {
							return "Make the role " + named + ", which may " + said
						}
						return "Make the role " + named
					case "delete":
						return "Delete the role " + named + ", taking it away from every group that holds it"
					}
					// The permissions replace what is there, so the card
					// names them: this is where access is decided.
					if call.Permissions != nil {
						if said := tools.Some(call.Permissions, 4); said != "" {
							return "Set what the role " + named + " may do: " + said
						}
						return "Take every permission away from the role " + named
					}
					return "Change the role " + named
				}),
				RiskOf: func(arguments json.RawMessage) tools.Risk {
					if tools.ActionOf(arguments) == "delete" {
						return tools.RiskDestructive
					}
					return tools.RiskGranting
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
		},
			// One tool for the accounts. The groups and the roles keep a
			// name each: group_manage and role_manage already take an
			// action of their own -- adding somebody to a group, writing a
			// permission into a role -- and a tool whose action field is
			// already spoken for cannot be an action of another.
			tools.Group{
				Name: "user", Family: tools.FamilyPeople,
				Description: "The accounts on this server.",
				Members: []tools.Member{
					{Action: "list", Tool: "user_list"},
					{Action: "add", Tool: "user_add"},
					{Action: "update", Tool: "user_update"},
					{Action: "remove", Tool: "user_remove"},
				},
			},
		)
	})
}

// counted is "one group" or "three groups": a card is read, and "3 group(s)"
// is a form somebody filled in.
func counted(many int, one, more string) string {
	if many == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", many, more)
}
