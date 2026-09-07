package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// NewGroupCommand builds "teanode group": who may do what, and where. A
// group holds people, the roles that say what they may do, and the domains
// the domain-kind permissions apply to. An account with no group may read
// its own mailbox and nothing else.
func NewGroupCommand() *cli.Command {
	return &cli.Command{
		Name:  "group",
		Usage: "groups: who may do what, and over which domains",
		Commands: []*cli.Command{
			{
				Name:   "list",
				Usage:  "list the groups on this server",
				Flags:  []cli.Flag{JSONFlag()},
				Action: runGroupList,
			},
			{
				Name:      "show",
				Usage:     "one group with its members, roles and domains",
				ArgsUsage: "<group>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runGroupShow,
			},
			{
				Name: "create",
				// No "add" alias here: this group has an add of its own,
				// which puts people into a group that exists, and an alias
				// would shadow it — the library takes the first name that
				// matches.
				Usage:     "add a group",
				ArgsUsage: "<name>",
				Description: "  teanode group create Support --role Operator --domain example.com --user ada\n\n" +
					"An identity provider group name makes single sign-on keep the membership\n" +
					"in step: --idp-group support.",
				Flags:  append(groupFlags(), JSONFlag()),
				Action: runGroupCreate,
			},
			{
				Name:      "update",
				Usage:     "change a group; each list given replaces the one stored",
				ArgsUsage: "<group>",
				Flags:     append(groupFlags(), JSONFlag(), &cli.StringFlag{Name: "rename", Usage: "a new name for the group"}),
				Action:    runGroupUpdate,
			},
			{
				Name:      "add",
				Usage:     "add users, roles or domains to a group, keeping what it has",
				ArgsUsage: "<group>",
				Flags:     append(groupMembershipFlags(), JSONFlag()),
				Action:    runGroupAddMembers,
			},
			{
				Name:      "remove",
				Usage:     "take users, roles or domains out of a group",
				ArgsUsage: "<group>",
				Flags:     append(groupMembershipFlags(), JSONFlag()),
				Action:    runGroupRemoveMembers,
			},
			{
				Name:      "delete",
				Usage:     "remove a group; its members keep their accounts",
				ArgsUsage: "<group>",
				Flags:     []cli.Flag{ForceFlag()},
				Action:    runGroupDelete,
			},
		},
	}
}

func groupMembershipFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringSliceFlag{Name: "user", Usage: "a member, by username; repeatable"},
		&cli.StringSliceFlag{Name: "role", Usage: "a role, by name or identifier; repeatable"},
		&cli.StringSliceFlag{Name: "domain", Usage: "a domain the domain-kind permissions apply to; repeatable"},
	}
}

func groupFlags() []cli.Flag {
	return append(groupMembershipFlags(),
		&cli.StringFlag{Name: "description", Usage: "a note for the operator"},
		&cli.StringFlag{Name: "idp-group", Usage: "the group's name at the identity provider, for single sign-on"},
	)
}

// resolveUsers turns usernames into identifiers, since a group holds
// identifiers and a person types names.
func resolveUsers(ctx context.Context, connection *client.Client, names []string) ([]string, error) {
	if len(names) == 0 {
		return nil, nil
	}
	users, err := client.ListUsers(ctx, connection)
	if err != nil {
		return nil, err
	}
	byName := map[string]string{}
	for _, user := range users {
		byName[strings.ToLower(user.Username)] = user.ID
	}
	ids := make([]string, 0, len(names))
	for _, name := range names {
		id, ok := byName[strings.ToLower(strings.TrimSpace(name))]
		if !ok {
			return nil, fmt.Errorf("no account called %q", name)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// resolveRoles turns role names into identifiers.
func resolveRoles(ctx context.Context, connection *client.Client, names []string) ([]string, error) {
	if len(names) == 0 {
		return nil, nil
	}
	roles, err := client.ListRoles(ctx, connection)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(names))
	for _, name := range names {
		wanted := strings.TrimSpace(name)
		found := ""
		for _, role := range roles {
			if role.ID == wanted || strings.EqualFold(role.Name, wanted) {
				found = role.ID
				break
			}
		}
		if found == "" {
			return nil, fmt.Errorf("no role called %q", name)
		}
		ids = append(ids, found)
	}
	return ids, nil
}

// resolveDomains turns domain names into identifiers.
func resolveDomains(ctx context.Context, connection *client.Client, names []string) ([]string, error) {
	if len(names) == 0 {
		return nil, nil
	}
	ids := make([]string, 0, len(names))
	for _, name := range names {
		domain, err := client.FindDomain(ctx, connection, strings.TrimSpace(name))
		if err != nil {
			return nil, err
		}
		if domain == nil {
			return nil, fmt.Errorf("no domain called %q", name)
		}
		ids = append(ids, domain.ID)
	}
	return ids, nil
}

// requireGroup is the group a command names, by name or identifier.
func requireGroup(ctx context.Context, command *cli.Command, connection *client.Client, wanted string) (*client.Group, error) {
	groups, err := client.ListGroups(ctx, connection)
	if err != nil {
		return nil, describeError(command, err)
	}
	for _, group := range groups {
		if group.ID == wanted || strings.EqualFold(group.Name, wanted) {
			return group, nil
		}
	}
	return nil, fmt.Errorf("no group called %q", wanted)
}

// names turns identifiers into the names a person reads, leaving an
// identifier alone when whatever it named is gone.
func names(lookup map[string]string, ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if name, ok := lookup[id]; ok {
			out = append(out, name)
			continue
		}
		out = append(out, id)
	}
	return out
}

// groupLookups is the three name tables a group's identifiers are printed
// through, read once for a whole command.
func groupLookups(ctx context.Context, connection *client.Client) (users, roles, domains map[string]string) {
	users, roles, domains = map[string]string{}, map[string]string{}, map[string]string{}
	if list, err := client.ListUsers(ctx, connection); err == nil {
		for _, user := range list {
			users[user.ID] = user.Username
		}
	}
	if list, err := client.ListRoles(ctx, connection); err == nil {
		for _, role := range list {
			roles[role.ID] = role.Name
		}
	}
	for id, name := range domainNames(ctx, connection) {
		domains[id] = name
	}
	return users, roles, domains
}

func runGroupList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	groups, err := client.ListGroups(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(groups)
	}
	if len(groups) == 0 {
		fmt.Println("no groups; every account may read its own mailbox and nothing else")
		return nil
	}
	users, roles, domains := groupLookups(ctx, connection)
	rows := make([][]string, 0, len(groups))
	for _, group := range groups {
		rows = append(rows, []string{
			group.Name,
			itoa(len(group.UserIDs)),
			strings.Join(names(roles, group.RoleIDs), ", "),
			strings.Join(names(domains, group.DomainIDs), ", "),
			group.IDPGroup,
		})
	}
	_ = users
	return printTable([]string{"NAME", "MEMBERS", "ROLES", "DOMAINS", "IDP GROUP"}, rows)
}

func runGroupShow(ctx context.Context, command *cli.Command) error {
	wanted := command.Args().First()
	if wanted == "" {
		return usage("which group? usage: teanode group show <group>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	group, err := requireGroup(ctx, command, connection, wanted)
	if err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(group)
	}
	users, roles, domains := groupLookups(ctx, connection)
	fields := [][2]string{
		{"name", group.Name},
		{"id", group.ID},
	}
	if group.Description != "" {
		fields = append(fields, [2]string{"description", group.Description})
	}
	if group.IDPGroup != "" {
		fields = append(fields, [2]string{"idp group", group.IDPGroup})
	}
	fields = append(fields,
		[2]string{"members", strings.Join(names(users, group.UserIDs), ", ")},
		[2]string{"roles", strings.Join(names(roles, group.RoleIDs), ", ")},
		[2]string{"domains", strings.Join(names(domains, group.DomainIDs), ", ")},
	)
	return printFields(fields)
}

// groupParameters reads the flags a create or an update was given, resolving
// every name to an identifier.
func groupParameters(ctx context.Context, command *cli.Command, connection *client.Client) (*client.GroupParameters, error) {
	parameters := &client.GroupParameters{}
	if command.IsSet("description") {
		value := command.String("description")
		parameters.Description = &value
	}
	if command.IsSet("idp-group") {
		value := command.String("idp-group")
		parameters.IDPGroup = &value
	}
	if command.IsSet("user") {
		ids, err := resolveUsers(ctx, connection, command.StringSlice("user"))
		if err != nil {
			return nil, err
		}
		parameters.UserIDs = &ids
	}
	if command.IsSet("role") {
		ids, err := resolveRoles(ctx, connection, command.StringSlice("role"))
		if err != nil {
			return nil, err
		}
		parameters.RoleIDs = &ids
	}
	if command.IsSet("domain") {
		ids, err := resolveDomains(ctx, connection, command.StringSlice("domain"))
		if err != nil {
			return nil, err
		}
		parameters.DomainIDs = &ids
	}
	return parameters, nil
}

func runGroupCreate(ctx context.Context, command *cli.Command) error {
	name := command.Args().First()
	if name == "" {
		return usage("what is the group called? usage: teanode group create <name> [--role ...] [--domain ...]")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	parameters, err := groupParameters(ctx, command, connection)
	if err != nil {
		return err
	}
	group, err := client.CreateGroup(ctx, connection, name, parameters)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(group)
	}
	fmt.Printf("added %s\n", group.Name)
	return nil
}

func runGroupUpdate(ctx context.Context, command *cli.Command) error {
	wanted := command.Args().First()
	if wanted == "" {
		return usage("which group? usage: teanode group update <group> [--role ...] [--rename ...]")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	group, err := requireGroup(ctx, command, connection, wanted)
	if err != nil {
		return err
	}
	parameters, err := groupParameters(ctx, command, connection)
	if err != nil {
		return err
	}
	if command.IsSet("rename") {
		value := command.String("rename")
		parameters.Name = &value
	}
	updated, err := client.UpdateGroup(ctx, connection, group.ID, parameters)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(updated)
	}
	fmt.Printf("updated %s\n", updated.Name)
	return nil
}

func runGroupAddMembers(ctx context.Context, command *cli.Command) error {
	return changeGroupMembers(ctx, command, true)
}

func runGroupRemoveMembers(ctx context.Context, command *cli.Command) error {
	return changeGroupMembers(ctx, command, false)
}

// changeGroupMembers adds to or takes from a group's lists, leaving the rest
// as it is: the API replaces a list whole, so the list is read, changed and
// written back.
func changeGroupMembers(ctx context.Context, command *cli.Command, adding bool) error {
	wanted := command.Args().First()
	if wanted == "" {
		return usage("which group? usage: teanode group add <group> --user ada --role Operator")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	group, err := requireGroup(ctx, command, connection, wanted)
	if err != nil {
		return err
	}
	userIds, err := resolveUsers(ctx, connection, command.StringSlice("user"))
	if err != nil {
		return err
	}
	roleIds, err := resolveRoles(ctx, connection, command.StringSlice("role"))
	if err != nil {
		return err
	}
	domainIds, err := resolveDomains(ctx, connection, command.StringSlice("domain"))
	if err != nil {
		return err
	}
	if len(userIds)+len(roleIds)+len(domainIds) == 0 {
		return usage("nothing to change; pass --user, --role or --domain")
	}
	parameters := &client.GroupParameters{}
	if len(userIds) > 0 {
		changed := changeList(group.UserIDs, userIds, adding)
		parameters.UserIDs = &changed
	}
	if len(roleIds) > 0 {
		changed := changeList(group.RoleIDs, roleIds, adding)
		parameters.RoleIDs = &changed
	}
	if len(domainIds) > 0 {
		changed := changeList(group.DomainIDs, domainIds, adding)
		parameters.DomainIDs = &changed
	}
	updated, err := client.UpdateGroup(ctx, connection, group.ID, parameters)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(updated)
	}
	if adding {
		fmt.Printf("added to %s\n", updated.Name)
	} else {
		fmt.Printf("removed from %s\n", updated.Name)
	}
	return nil
}

// changeList adds identifiers to a list without repeating them, or takes
// them out.
func changeList(current, change []string, adding bool) []string {
	has := map[string]bool{}
	for _, id := range current {
		has[id] = true
	}
	if adding {
		result := append([]string{}, current...)
		for _, id := range change {
			if !has[id] {
				result = append(result, id)
				has[id] = true
			}
		}
		return result
	}
	remove := map[string]bool{}
	for _, id := range change {
		remove[id] = true
	}
	result := make([]string, 0, len(current))
	for _, id := range current {
		if !remove[id] {
			result = append(result, id)
		}
	}
	return result
}

func runGroupDelete(ctx context.Context, command *cli.Command) error {
	wanted := command.Args().First()
	if wanted == "" {
		return usage("which group? usage: teanode group delete <group>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	group, err := requireGroup(ctx, command, connection, wanted)
	if err != nil {
		return err
	}
	if err := confirm(command, fmt.Sprintf("This removes the group %s. Its %d member(s) keep their accounts and lose what it gave them.",
		group.Name, len(group.UserIDs))); err != nil {
		return err
	}
	if err := client.DeleteGroup(ctx, connection, group.ID); err != nil {
		return describeError(command, err)
	}
	fmt.Printf("removed %s\n", group.Name)
	return nil
}

// NewRoleCommand builds "teanode role": the named sets of permissions a
// group holds.
func NewRoleCommand() *cli.Command {
	return &cli.Command{
		Name:  "role",
		Usage: "roles: the named sets of permissions a group holds",
		Commands: []*cli.Command{
			{
				Name:   "list",
				Usage:  "list the roles on this server",
				Flags:  []cli.Flag{JSONFlag()},
				Action: runRoleList,
			},
			{
				Name:   "permissions",
				Usage:  "list every permission a role may hold",
				Flags:  []cli.Flag{JSONFlag()},
				Action: runRolePermissions,
			},
			{
				Name:      "create",
				Aliases:   []string{"add"},
				Usage:     "add a role",
				ArgsUsage: "<name>",
				Description: "  teanode role create Postmaster --permission mail:audit --permission queue:manage\n\n" +
					"'teanode role permissions' lists what may be given.",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringSliceFlag{Name: "permission", Usage: "a permission, by key; repeatable"},
					&cli.StringFlag{Name: "description", Usage: "a note for the operator"},
				},
				Action: runRoleCreate,
			},
			{
				Name:      "update",
				Usage:     "change a role; the permissions given replace the ones stored",
				ArgsUsage: "<role>",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringSliceFlag{Name: "permission", Usage: "a permission, by key; repeatable"},
					&cli.StringFlag{Name: "description", Usage: "a note for the operator"},
					&cli.StringFlag{Name: "rename", Usage: "a new name for the role"},
				},
				Action: runRoleUpdate,
			},
			{
				Name:      "delete",
				Usage:     "remove a role that is not one of the seeded ones",
				ArgsUsage: "<role>",
				Flags:     []cli.Flag{ForceFlag()},
				Action:    runRoleDelete,
			},
		},
	}
}

func requireRole(ctx context.Context, command *cli.Command, connection *client.Client, wanted string) (*client.Role, error) {
	roles, err := client.ListRoles(ctx, connection)
	if err != nil {
		return nil, describeError(command, err)
	}
	for _, role := range roles {
		if role.ID == wanted || strings.EqualFold(role.Name, wanted) {
			return role, nil
		}
	}
	return nil, fmt.Errorf("no role called %q", wanted)
}

func runRoleList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	roles, err := client.ListRoles(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(roles)
	}
	if len(roles) == 0 {
		fmt.Println("no roles")
		return nil
	}
	rows := make([][]string, 0, len(roles))
	for _, role := range roles {
		rows = append(rows, []string{role.Name, itoa(len(role.Permissions)), strings.Join(role.Permissions, " ")})
	}
	return printTable([]string{"NAME", "COUNT", "PERMISSIONS"}, rows)
}

func runRolePermissions(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	permissions, err := client.ListPermissions(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(permissions)
	}
	rows := make([][]string, 0, len(permissions))
	for _, permission := range permissions {
		rows = append(rows, []string{permission.Key, permission.Kind, permission.Widens})
	}
	return printTable([]string{"PERMISSION", "APPLIES TO", "WIDENS"}, rows)
}

func runRoleCreate(ctx context.Context, command *cli.Command) error {
	name := command.Args().First()
	if name == "" {
		return usage("what is the role called? usage: teanode role create <name> --permission <key>")
	}
	permissions := command.StringSlice("permission")
	if len(permissions) == 0 {
		return usage("a role needs at least one permission; 'teanode role permissions' lists them")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	var description *string
	if command.IsSet("description") {
		value := command.String("description")
		description = &value
	}
	role, err := client.CreateRole(ctx, connection, name, permissions, description)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(role)
	}
	fmt.Printf("added %s\n", role.Name)
	return nil
}

func runRoleUpdate(ctx context.Context, command *cli.Command) error {
	wanted := command.Args().First()
	if wanted == "" {
		return usage("which role? usage: teanode role update <role> [--permission ...] [--rename ...]")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	role, err := requireRole(ctx, command, connection, wanted)
	if err != nil {
		return err
	}
	parameters := &client.RoleParameters{}
	if command.IsSet("permission") {
		values := command.StringSlice("permission")
		parameters.Permissions = &values
	}
	if command.IsSet("description") {
		value := command.String("description")
		parameters.Description = &value
	}
	if command.IsSet("rename") {
		value := command.String("rename")
		parameters.Name = &value
	}
	updated, err := client.UpdateRole(ctx, connection, role.ID, parameters)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(updated)
	}
	fmt.Printf("updated %s\n", updated.Name)
	return nil
}

func runRoleDelete(ctx context.Context, command *cli.Command) error {
	wanted := command.Args().First()
	if wanted == "" {
		return usage("which role? usage: teanode role delete <role>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	role, err := requireRole(ctx, command, connection, wanted)
	if err != nil {
		return err
	}
	if err := confirm(command, fmt.Sprintf("This removes the role %s. Every group that holds it loses what it gave.", role.Name)); err != nil {
		return err
	}
	if err := client.DeleteRole(ctx, connection, role.ID); err != nil {
		return describeError(command, err)
	}
	fmt.Printf("removed %s\n", role.Name)
	return nil
}

// NewAuditCommand builds "teanode audit": the log of administrative
// changes, who made them, and what the row looked like before and after.
func NewAuditCommand() *cli.Command {
	return &cli.Command{
		Name:  "audit",
		Usage: "the log of administrative changes",
		Commands: []*cli.Command{
			{
				Name:  "list",
				Usage: "list the audit log, newest first",
				Description: "  teanode audit list --type mailbox_folder\n" +
					"  teanode audit list --actor ada --since 2026-09-01\n" +
					"  teanode audit list --id 01m1... --json",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "type", Usage: "only changes to this kind of thing: user, group, role, domain, mailbox, ..."},
					&cli.StringFlag{Name: "id", Usage: "only changes to this row"},
					&cli.StringFlag{Name: "actor", Usage: "only changes made by this account, by username"},
					&cli.StringFlag{Name: "since", Usage: "only changes at or after this time, as 2026-09-01 or a full timestamp"},
					&cli.StringFlag{Name: "until", Usage: "only changes before this time"},
					&cli.IntFlag{Name: "first", Value: 20, Usage: "how many, at most 200"},
					&cli.IntFlag{Name: "offset", Usage: "how many to skip, for reading further back"},
				},
				Action: runAuditList,
			},
		},
	}
}

// parseWhen reads a time flag: a date, or a full timestamp.
func parseWhen(flag, value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02"} {
		if when, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return &when, nil
		}
	}
	return nil, fmt.Errorf("--%s: %q is not a time; write it as 2026-09-01 or 2026-09-01T09:00:00", flag, value)
}

func runAuditList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	since, err := parseWhen("since", command.String("since"))
	if err != nil {
		return usage(err.Error())
	}
	until, err := parseWhen("until", command.String("until"))
	if err != nil {
		return usage(err.Error())
	}
	filter := &client.AuditFilter{
		ResourceType: command.String("type"),
		ResourceID:   command.String("id"),
		Since:        since,
		Until:        until,
		First:        int(command.Int("first")),
		Offset:       int(command.Int("offset")),
	}
	if actor := command.String("actor"); actor != "" {
		ids, err := resolveUsers(ctx, connection, []string{actor})
		if err != nil {
			return err
		}
		filter.ActorUserID = ids[0]
	}
	page, err := client.ListAuditEvents(ctx, connection, filter)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(page)
	}
	if page == nil || len(page.Events) == 0 {
		fmt.Println("no audit events")
		return nil
	}
	rows := make([][]string, 0, len(page.Events))
	for _, event := range page.Events {
		actor := event.ActorLabel
		if actor == "" {
			actor = event.ActorKind
		}
		rows = append(rows, []string{
			formatTime(&event.CreatedAt),
			actor,
			event.Action,
			event.ResourceType,
			truncate(describeAuditRow(event), 44),
		})
	}
	if err := printTable([]string{"WHEN", "WHO", "WHAT", "TO", "WHICH"}, rows); err != nil {
		return err
	}
	if page.Total > len(page.Events) {
		fmt.Printf("\n%d of %d; read further back with --offset %d\n", len(page.Events), page.Total, len(page.Events)+filter.Offset)
	}
	return nil
}

// describeAuditRow names the row a change was made to: its name where it has
// one, since an identifier says nothing to a reader.
func describeAuditRow(event *client.AuditEvent) string {
	for _, raw := range []json.RawMessage{event.After, event.Before} {
		if len(raw) == 0 {
			continue
		}
		var fields map[string]any
		if err := json.Unmarshal(raw, &fields); err != nil {
			continue
		}
		for _, key := range []string{"name", "username", "domain", "pattern", "address", "email"} {
			if value, ok := fields[key].(string); ok && value != "" {
				return value
			}
		}
	}
	return event.ResourceID
}
