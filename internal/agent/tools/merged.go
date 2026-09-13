package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ziyan/teanode/internal/models"
)

// Merging: one tool per thing, not one per verb.
//
// The catalog grew a name for every verb of every noun -- domain_list,
// domain_get, domain_add, domain_update, domain_remove, domain_dns_check,
// and the same again for aliases and credentials -- until it was eighty-two
// tools, of which seventeen were the operator's domains. Every one of them is
// a paragraph in every request, and a model choosing between eighty-two
// near-identical names chooses worse than one choosing between fifty.
//
// This is the shape the catalog already used in the places that were written
// later: contact_book, folder_manage, schedule, connected_server, skill and
// mail_act each take an action and do several related things. Merged makes
// that shape out of definitions that are already written, so the code under
// each action does not move and its tests go on testing it.

// Action is one verb of a merged tool: the word the model passes as
// "action", and the tool it stands for.
type Action struct {
	Name string
	Tool *Tool
}

// MergedTool is what to build: the merged tool's own name and family, the
// sentence that introduces it, and its actions in the order they should be
// read.
type MergedTool struct {
	Name        string
	Family      Family
	Description string
	Guidance    string
	Core        bool
	Actions     []Action
}

// Merge builds the merged tool.
//
// Three things are carried over from the actions rather than written again.
//
// The parameters are an "action" enumeration and the union of what the
// actions take; where two actions use the same field for different things,
// both descriptions are kept, so "for save: ..." and "for remove: ..." read
// as they do in the tools that were written this way by hand.
//
// The risk is the gentlest of the actions, and RiskOf raises it for the call
// in hand. That is what lets a read-only run offer a tool whose other actions
// write: the loop keeps a tool with a RiskOf and refuses the call itself if
// the action turns out not to be a read. Arguments that cannot be read at all
// take the strictest risk any action carries, never the gentlest -- a call
// nobody can parse is not a call anybody should wave through.
//
// The permissions are the union, because they decide only whether the tool is
// offered; each action checks its own before it runs, and says so in words the
// model can relay when it does not hold.
func Merge(merged MergedTool) *Tool {
	if len(merged.Actions) == 0 {
		panic("tools: a merged tool with no actions")
	}
	byAction := make(map[string]*Tool, len(merged.Actions))
	verbs := make([]string, 0, len(merged.Actions))
	lines := []string{strings.TrimSpace(merged.Description)}
	properties := map[string]any{}
	descriptions := map[string][]string{}
	permissions := mergedPermissions(merged.Actions)
	headless := true
	for _, action := range merged.Actions {
		if action.Tool == nil || action.Name == "" {
			panic("tools: an action with no name or no tool")
		}
		// A tool that already takes an "action" cannot be an action of
		// another: the merged tool's own field would sit on top of it and
		// every call would be dispatched twice to the wrong half. It is
		// how folder_manage, which is already action-shaped, was found to
		// belong beside a merged tool rather than inside one.
		if takesAnAction(action.Tool) {
			panic(fmt.Sprintf("tools: %s takes an action of its own and cannot be an action of %s", action.Tool.Name, merged.Name))
		}
		byAction[action.Name] = action.Tool
		verbs = append(verbs, action.Name)
		lines = append(lines, action.Name+" — "+strings.TrimSpace(action.Tool.Description))
		mergeProperties(properties, descriptions, action.Tool.Parameters)
		if !action.Tool.Headless {
			headless = false
		}
	}
	for name, parts := range descriptions {
		if len(parts) < 2 {
			continue
		}
		if property, ok := properties[name].(map[string]any); ok {
			copied := map[string]any{}
			for key, value := range property {
				copied[key] = value
			}
			copied["description"] = strings.Join(parts, "; ")
			properties[name] = copied
		}
	}
	properties["action"] = EnumProperty("what to do", verbs...)

	return &Tool{
		Name:        merged.Name,
		Family:      merged.Family,
		Description: strings.Join(lines, "\n"),
		Risk:        gentlest(merged.Actions),
		Parameters:  Object(properties, "action"),
		Permissions: permissions,
		Core:        merged.Core,
		Headless:    headless,
		Guidance:    strings.TrimSpace(merged.Guidance),
		Preview: func(arguments json.RawMessage) string {
			name, err := actionOf(arguments)
			tool := byAction[name]
			if err != nil || tool == nil {
				return merged.Name
			}
			if tool.Preview != nil {
				return tool.Preview(arguments)
			}
			return merged.Name + " " + name
		},
		RiskOf: func(arguments json.RawMessage) Risk {
			name, err := actionOf(arguments)
			if err != nil {
				return strictest(merged.Actions)
			}
			tool := byAction[name]
			if tool == nil {
				return strictest(merged.Actions)
			}
			return tool.RiskFor(arguments)
		},
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			name, err := actionOf(call.Arguments)
			if err != nil {
				return nil, err
			}
			tool := byAction[name]
			if tool == nil {
				return nil, fmt.Errorf("%q is not something %s does; it does %s", name, merged.Name, strings.Join(verbs, ", "))
			}
			// The catalog admitted the tool on the union of what its
			// actions ask for. This action asks for its own.
			if run, err := RunFrom(ctx); err == nil && run.Operations() != nil {
				if !AllowedByPermissions(tool, run.Operations().Permissions()) {
					return nil, fmt.Errorf("%s %s is not something they may do", merged.Name, name)
				}
			}
			return tool.Run(ctx, call)
		},
	}
}

// takesAnAction says whether a tool already has a field of that name.
func takesAnAction(tool *Tool) bool {
	fields, ok := tool.Parameters["properties"].(map[string]any)
	if !ok {
		return false
	}
	_, taken := fields["action"]
	return taken
}

// actionOf reads the action out of a call's arguments, as the tool will read
// them: repaired first, so that the risk decision and the act agree about the
// same bytes.
func actionOf(arguments json.RawMessage) (string, error) {
	var asked struct {
		Action string `json:"action"`
	}
	settled := SettledArguments(arguments)
	if len(settled) == 0 {
		return "", fmt.Errorf("say which action to take")
	}
	if err := json.Unmarshal(settled, &asked); err != nil {
		return "", fmt.Errorf("the arguments are not what the tool takes: %w", err)
	}
	name := strings.ToLower(strings.TrimSpace(asked.Action))
	if name == "" {
		return "", fmt.Errorf("say which action to take")
	}
	return name, nil
}

// mergeProperties adds one action's properties to the union, keeping every
// description given for a field several actions share.
func mergeProperties(properties map[string]any, descriptions map[string][]string, schema map[string]any) {
	if schema == nil {
		return
	}
	fields, ok := schema["properties"].(map[string]any)
	if !ok {
		return
	}
	for name, property := range fields {
		if _, already := properties[name]; !already {
			properties[name] = property
		}
		if described, ok := property.(map[string]any); ok {
			if text, ok := described["description"].(string); ok && text != "" {
				if !contains(descriptions[name], text) {
					descriptions[name] = append(descriptions[name], text)
				}
			}
		}
	}
}

func contains(values []string, value string) bool {
	for _, each := range values {
		if each == value {
			return true
		}
	}
	return false
}

// severity orders the classes for the two questions a merged tool asks of
// them: which is the gentlest, for the floor, and which the strictest, for a
// call that cannot be read.
var severity = map[Risk]int{RiskRead: 0, RiskWrite: 1, RiskGranting: 2, RiskOutward: 3, RiskDestructive: 4}

func gentlest(actions []Action) Risk {
	found := RiskDestructive
	for _, action := range actions {
		if severity[action.Tool.Risk] < severity[found] {
			found = action.Tool.Risk
		}
	}
	return found
}

func strictest(actions []Action) Risk {
	found := RiskRead
	for _, action := range actions {
		risk := action.Tool.Risk
		// A tool that tells its own calls apart may cost more than its
		// declared class says; the strictest it could be is what counts.
		if action.Tool.RiskOf != nil && severity[RiskDestructive] > severity[risk] {
			risk = RiskDestructive
		}
		if severity[risk] > severity[found] {
			found = risk
		}
	}
	return found
}

// mergedPermissions is the union, in a stable order so that two builds of the
// same catalog are the same catalog.
func mergedPermissions(actions []Action) []models.Permission {
	seen := map[models.Permission]bool{}
	for _, action := range actions {
		// An action anybody may take opens the tool to anybody: a union
		// with "everyone" in it is everyone.
		if len(action.Tool.Permissions) == 0 {
			return nil
		}
		for _, permission := range action.Tool.Permissions {
			seen[permission] = true
		}
	}
	union := make([]models.Permission, 0, len(seen))
	for permission := range seen {
		union = append(union, permission)
	}
	sort.Slice(union, func(first, second int) bool { return union[first] < union[second] })
	return union
}

// Member names one tool a group is made of: the word the model will pass as
// the action, and the name the tool is registered under today.
type Member struct {
	Action string
	Tool   string
}

// Group is a merged tool described by the names of its parts, so that a
// package can go on declaring its tools exactly as it does and say, in one
// line at the end, which of them are one tool.
type Group struct {
	Name        string
	Family      Family
	Description string
	Guidance    string
	Core        bool
	Members     []Member
}

// Grouped merges what the groups name and keeps everything else as it is.
//
// A group naming a tool that is not there is a wiring mistake, and it panics:
// this runs while the catalog is being built, and a capability that quietly
// disappears because somebody renamed a tool is worse than a server that will
// not start. Every package that registers tools is imported by
// internal/agent/tools/all, so any test that builds the catalog finds it.
func Grouped(all []*Tool, groups ...Group) []*Tool {
	byName := make(map[string]*Tool, len(all))
	for _, tool := range all {
		byName[tool.Name] = tool
	}
	merged := make([]*Tool, 0, len(groups))
	taken := map[string]bool{}
	for _, group := range groups {
		actions := make([]Action, 0, len(group.Members))
		for _, member := range group.Members {
			tool := byName[member.Tool]
			if tool == nil {
				panic(fmt.Sprintf("tools: %s names %q, which is not registered", group.Name, member.Tool))
			}
			taken[member.Tool] = true
			actions = append(actions, Action{Name: member.Action, Tool: tool})
		}
		merged = append(merged, Merge(MergedTool{
			Name:        group.Name,
			Family:      group.Family,
			Description: group.Description,
			Guidance:    group.Guidance,
			Core:        group.Core,
			Actions:     actions,
		}))
	}
	for _, tool := range all {
		if !taken[tool.Name] {
			merged = append(merged, tool)
		}
	}
	return merged
}

// Renamed maps the names the catalog used before its tools were merged to
// what they are called now.
//
// An operator's policy and a person's own "always ask me" list are written
// down by name (agent.tools.disabled, agent.tools.confirm, and the agent's
// Confirm), and those lists were written when every verb had a name of its
// own. A list naming domain_remove means what it meant: do not let this
// happen. Because the verb is an action now, the whole tool is what the name
// resolves to -- so a policy that switched one verb off switches its tool
// off. That is broader than what was asked for and never narrower, which is
// the direction a policy may safely be wrong in, and the release notes say
// so.
var Renamed = map[string]string{
	"domain_list": "domain", "domain_get": "domain", "domain_add": "domain",
	"domain_update": "domain", "domain_remove": "domain", "domain_dns_check": "domain",
	"alias_list": "alias", "alias_match": "alias", "alias_add": "alias",
	"alias_update": "alias", "alias_remove": "alias",
	"credential_list": "credential", "credential_create": "credential",
	"credential_update": "credential", "credential_remove": "credential",
	"queue_list": "queue", "queue_retry": "queue",
	"rule_list": "rule", "rule_add": "rule", "rule_update": "rule",
	"rule_remove": "rule", "rule_test": "rule", "rule_apply": "rule",
	"user_list": "user", "user_add": "user", "user_update": "user", "user_remove": "user",
	"calendar_agenda": "calendar", "calendar_free": "calendar", "calendar_add": "calendar",
	"calendar_edit": "calendar", "calendar_remove": "calendar",
	"mail_audit_search": "mail_audit", "mail_audit_get": "mail_audit",
	"mail_audit_content": "mail_audit", "mail_audit_mark": "mail_audit",
	"account_get": "account", "account_update": "account",
	"settings_get": "settings", "settings_update": "settings",
}
