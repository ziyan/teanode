// Package rule is the mailbox's rules as tools: listed, added, changed,
// removed, tried and applied.
package rule

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/agent/tools/mailbox"
	"github.com/ziyan/teanode/internal/models"

	"github.com/op/go-logging"
)

var log = logging.MustGetLogger("agent")

func init() {
	tools.Register(func() []*tools.Tool {
		ruleFields := map[string]any{
			"mailbox": mailbox.MailboxProperty,
			"name":    tools.StringProperty("the rule's name"),
			"conditions": tools.ArrayProperty("every one must hold", tools.Object(map[string]any{
				"field":    tools.EnumProperty("what to test", "from", "to", "subject", "header", "score", "sender-known", "category", "priority", "needs-reply", "any"),
				"header":   tools.StringProperty("for header: the header's name"),
				"operator": tools.EnumProperty("how to compare", "contains", "equals", "matches", "above", "below"),
				"value":    tools.StringProperty("what to compare with"),
			})),
			"actions": tools.ArrayProperty("what to do, in order", tools.Object(map[string]any{
				"kind":    tools.EnumProperty("the action", "move", "markRead", "flag", "forward", "delete"),
				"folder":  tools.StringProperty("for move: the folder, by name"),
				"address": tools.StringProperty("for forward: the address"),
			})),
			"stop":     tools.BooleanProperty("stop at this rule when it matches"),
			"enabled":  tools.BooleanProperty("on; true by default"),
			"position": tools.IntegerProperty("where in the list, 1 first; the end by default"),
		}
		return []*tools.Tool{
			{
				Name: "rule_list", Family: tools.FamilyMailbox, Risk: tools.RiskRead,
				Permissions: []models.Permission{models.PermissionMailboxManage},
				Description: "The mailbox's rules in order, each with its conditions and actions.",
				Parameters:  tools.Object(map[string]any{"mailbox": mailbox.MailboxProperty}),
				Run:         runRuleList,
			},
			{
				Name: "rule_add", Family: tools.FamilyMailbox, Risk: tools.RiskWrite,
				RiskOf:      ruleRisk,
				Permissions: []models.Permission{models.PermissionMailboxManage},
				Description: "Add a rule. Rules run on every message that arrives; the ones reading category, priority or needs-reply run once the agent has sorted it. The answer says what the rule would have matched among the newest fifty in the Inbox.",
				Parameters:  tools.Object(ruleFields, "name", "conditions", "actions"),
				Guidance:    "A rule is the right shape for \"always file X in Y\": it is the person's, shown in their settings, and runs without you. Prefer it over remembering to do it yourself.",
				Run:         runRuleAdd,
			},
			{
				Name: "rule_update", Family: tools.FamilyMailbox, Risk: tools.RiskWrite,
				RiskOf:      ruleRisk,
				Permissions: []models.Permission{models.PermissionMailboxManage},
				Description: "Change a rule: give its current name as rule, and the fields to change.",
				Parameters:  tools.Object(mailbox.MergeProperties(ruleFields, map[string]any{"rule": tools.StringProperty("the rule to change, by name or position")}), "rule"),
				Run:         runRuleUpdate,
			},
			{
				Name: "rule_remove", Family: tools.FamilyMailbox, Risk: tools.RiskWrite,
				Permissions: []models.Permission{models.PermissionMailboxManage},
				Description: "Remove a rule.",
				Parameters:  tools.Object(map[string]any{"mailbox": mailbox.MailboxProperty, "rule": tools.StringProperty("the rule, by name or position")}, "rule"),
				Run:         runRuleRemove,
			},
			{
				Name: "rule_test", Family: tools.FamilyMailbox, Risk: tools.RiskRead,
				Permissions: []models.Permission{models.PermissionMailboxManage},
				Description: "A dry run: which of the newest messages each rule matches. Give rules to test unsaved candidates, or none to test the stored ones.",
				Parameters: tools.Object(map[string]any{
					"mailbox": mailbox.MailboxProperty,
					"folder":  tools.StringProperty("the folder to test against; Inbox by default"),
					"rules":   tools.ArrayProperty("candidates; the stored rules when absent", tools.Object(ruleFields)),
					"limit":   tools.IntegerProperty("how many newest messages, 50 by default"),
				}),
				Run: runRuleTest,
			},
			{
				Name: "rule_apply", Family: tools.FamilyMailbox, Risk: tools.RiskWrite,
				Permissions: []models.Permission{models.PermissionMailboxManage},
				Description: "Run the stored rules over the messages already in a folder. Moves and marks; never forwards.",
				Parameters: tools.Object(map[string]any{
					"mailbox": mailbox.MailboxProperty,
					"folder":  tools.StringProperty("the folder; Inbox by default"),
					"limit":   tools.IntegerProperty("how many newest messages, 200 by default"),
				}),
				Preview: func(arguments json.RawMessage) string {
					return "Apply the rules to existing mail: " + strings.TrimSpace(string(arguments))
				},
				Run: runRuleApply,
			},
		}
	})
}

type ruleArguments struct {
	Mailbox    string `json:"mailbox"`
	Rule       string `json:"rule"`
	Name       string `json:"name"`
	Conditions []struct {
		Field    string `json:"field"`
		Header   string `json:"header"`
		Operator string `json:"operator"`
		Value    string `json:"value"`
	} `json:"conditions"`
	Actions []struct {
		Kind    string `json:"kind"`
		Folder  string `json:"folder"`
		Address string `json:"address"`
	} `json:"actions"`
	Stop     *bool `json:"stop"`
	Enabled  *bool `json:"enabled"`
	Position int   `json:"position"`
}

// ruleInput is a rule as UpdateMailbox takes it.
type ruleInput struct {
	Name       string           `json:"name"`
	Enabled    bool             `json:"enabled"`
	Stop       bool             `json:"stop"`
	Conditions []conditionInput `json:"conditions"`
	Actions    []actionInput    `json:"actions"`
}

type conditionInput struct {
	Field    string `json:"field"`
	Header   string `json:"header"`
	Operator string `json:"operator"`
	Value    string `json:"value"`
}

type actionInput struct {
	Kind     string `json:"kind"`
	FolderID string `json:"folderId"`
	Address  string `json:"address"`
}

func storedRules(view *mailbox.MailboxView) []ruleInput {
	rules := make([]ruleInput, 0, len(view.Mailbox.Rules))
	for _, rule := range view.Mailbox.Rules {
		input := ruleInput{Name: rule.Name, Enabled: rule.Enabled, Stop: rule.Stop, Conditions: []conditionInput{}, Actions: []actionInput{}}
		for _, condition := range rule.Conditions {
			input.Conditions = append(input.Conditions, conditionInput{Field: condition.Field, Header: condition.Header, Operator: condition.Operator, Value: condition.Value})
		}
		for _, action := range rule.Actions {
			input.Actions = append(input.Actions, actionInput{Kind: action.Kind, FolderID: action.FolderID, Address: action.Address})
		}
		rules = append(rules, input)
	}
	return rules
}

// ruleFromArguments builds a rule from what the model gave, resolving
// folder names.
func ruleFromArguments(view *mailbox.MailboxView, arguments *ruleArguments, base *ruleInput) (*ruleInput, error) {
	rule := ruleInput{Enabled: true, Conditions: []conditionInput{}, Actions: []actionInput{}}
	if base != nil {
		rule = *base
	}
	if strings.TrimSpace(arguments.Name) != "" {
		rule.Name = strings.TrimSpace(arguments.Name)
	}
	if arguments.Stop != nil {
		rule.Stop = *arguments.Stop
	}
	if arguments.Enabled != nil {
		rule.Enabled = *arguments.Enabled
	}
	if len(arguments.Conditions) > 0 {
		rule.Conditions = []conditionInput{}
		for _, condition := range arguments.Conditions {
			operator := condition.Operator
			switch condition.Field {
			case "sender-known", "needs-reply", "any":
				operator = ""
			case "":
				return nil, fmt.Errorf("a condition needs a field")
			default:
				if operator == "" {
					operator = "contains"
				}
			}
			rule.Conditions = append(rule.Conditions, conditionInput{Field: condition.Field, Header: condition.Header, Operator: operator, Value: condition.Value})
		}
	}
	if len(arguments.Actions) > 0 {
		rule.Actions = []actionInput{}
		for _, action := range arguments.Actions {
			input := actionInput{Kind: action.Kind, Address: action.Address}
			if action.Kind == "move" {
				folder, err := mailbox.FindFolder(view, action.Folder)
				if err != nil {
					return nil, err
				}
				input.FolderID = folder.ID
			}
			rule.Actions = append(rule.Actions, input)
		}
	}
	if rule.Name == "" {
		return nil, fmt.Errorf("a rule needs a name")
	}
	return &rule, nil
}

func findRule(rules []ruleInput, nameOrPosition string) (int, error) {
	nameOrPosition = strings.TrimSpace(nameOrPosition)
	for index, rule := range rules {
		if strings.EqualFold(rule.Name, nameOrPosition) || fmt.Sprint(index+1) == nameOrPosition {
			return index, nil
		}
	}
	return -1, fmt.Errorf("there is no rule %q", nameOrPosition)
}

func saveRules(ctx context.Context, operations tools.Operations, view *mailbox.MailboxView, rules []ruleInput) error {
	var discard map[string]any
	return operations.Execute(ctx, `mutation ($mailboxId: String!, $rules: [MailboxRuleInput!]) { UpdateMailbox(mailboxId: $mailboxId, rules: $rules) { mailbox { id } } }`, map[string]any{"mailboxId": view.Mailbox.ID, "rules": rules}, &discard)
}

func describeRules(view *mailbox.MailboxView, rules []ruleInput) []map[string]any {
	byId := map[string]*models.MailboxFolder{}
	for _, folder := range view.Folders {
		byId[folder.ID] = folder
	}
	described := make([]map[string]any, 0, len(rules))
	for index, rule := range rules {
		conditions := make([]string, 0, len(rule.Conditions))
		for _, condition := range rule.Conditions {
			switch condition.Field {
			case "sender-known", "needs-reply", "any":
				conditions = append(conditions, condition.Field)
			case "header":
				conditions = append(conditions, fmt.Sprintf("header %s %s %q", condition.Header, condition.Operator, condition.Value))
			default:
				conditions = append(conditions, fmt.Sprintf("%s %s %q", condition.Field, condition.Operator, condition.Value))
			}
		}
		actions := make([]string, 0, len(rule.Actions))
		for _, action := range rule.Actions {
			switch action.Kind {
			case "move":
				name := action.FolderID
				if folder := byId[action.FolderID]; folder != nil {
					name = mailbox.FolderPath(view, folder)
				}
				actions = append(actions, "move to "+name)
			case "forward":
				actions = append(actions, "forward to "+action.Address)
			default:
				actions = append(actions, action.Kind)
			}
		}
		described = append(described, map[string]any{"position": index + 1, "name": rule.Name, "enabled": rule.Enabled, "stop": rule.Stop, "when": strings.Join(conditions, " and "), "then": strings.Join(actions, ", ")})
	}
	return described
}

func runRuleList(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	arguments, err := tools.DecodeArguments[mailbox.MailboxArguments](call)
	if err != nil {
		return nil, err
	}
	views, err := mailbox.GrantedMailboxes(ctx, run.Operations())
	if err != nil {
		return nil, err
	}
	view, err := mailbox.FindMailbox(views, arguments.Mailbox)
	if err != nil {
		return nil, err
	}
	return tools.JSONResult(map[string]any{"mailbox": view.Mailbox.Name, "rules": describeRules(view, storedRules(view))})
}

func testRules(ctx context.Context, operations tools.Operations, view *mailbox.MailboxView, folderId string, rules []ruleInput, limit int) ([]map[string]any, error) {
	if limit <= 0 {
		limit = 50
	}
	var result struct {
		TestMailboxRules []struct {
			Matched []int `json:"matched"`
			Item    struct {
				ID   string `json:"id"`
				Mail *struct {
					From    string `json:"from"`
					Subject string `json:"subject"`
				} `json:"mail"`
			} `json:"item"`
		} `json:"TestMailboxRules"`
	}
	variables := map[string]any{"mailboxId": view.Mailbox.ID, "first": limit, "rules": rules}
	if folderId != "" {
		variables["folderId"] = folderId
	}
	if err := operations.Execute(ctx, `query ($mailboxId: String!, $folderId: String, $first: Int, $rules: [MailboxRuleInput!]!) { TestMailboxRules(mailboxId: $mailboxId, folderId: $folderId, first: $first, rules: $rules) { matched item { id mail { from subject } } } }`, variables, &result); err != nil {
		return nil, err
	}
	perRule := make([]map[string]any, len(rules))
	for index, rule := range rules {
		perRule[index] = map[string]any{"rule": rule.Name, "matches": []string{}, "count": 0}
	}
	for _, entry := range result.TestMailboxRules {
		for _, index := range entry.Matched {
			if index < 0 || index >= len(perRule) {
				continue
			}
			line := entry.Item.ID
			if entry.Item.Mail != nil {
				line = fmt.Sprintf("%s — %s (%s)", entry.Item.Mail.From, entry.Item.Mail.Subject, entry.Item.ID)
			}
			matches := perRule[index]["matches"].([]string)
			if len(matches) < 10 {
				perRule[index]["matches"] = append(matches, line)
			}
			perRule[index]["count"] = perRule[index]["count"].(int) + 1
		}
	}
	return perRule, nil
}

func runRuleAdd(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	arguments, err := tools.DecodeArguments[ruleArguments](call)
	if err != nil {
		return nil, err
	}
	operations := run.Operations()
	views, err := mailbox.GrantedMailboxes(ctx, operations)
	if err != nil {
		return nil, err
	}
	view, err := mailbox.FindMailbox(views, arguments.Mailbox)
	if err != nil {
		return nil, err
	}
	rule, err := ruleFromArguments(view, &arguments, nil)
	if err != nil {
		return nil, err
	}
	rules := storedRules(view)
	position := arguments.Position - 1
	if position < 0 || position > len(rules) {
		position = len(rules)
	}
	rules = append(rules[:position], append([]ruleInput{*rule}, rules[position:]...)...)
	if err := saveRules(ctx, operations, view, rules); err != nil {
		return nil, err
	}
	effect, err := testRules(ctx, operations, view, "", []ruleInput{*rule}, 50)
	if err != nil {
		log.Warningf("cannot test the new rule %q: %s", rule.Name, err)
	}
	result, err := tools.JSONResult(map[string]any{"rules": describeRules(view, rules), "would_have_matched": effect})
	if err != nil {
		return nil, err
	}
	result.Note = fmt.Sprintf("added the rule %q", rule.Name)
	return result, nil
}

func runRuleUpdate(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	arguments, err := tools.DecodeArguments[ruleArguments](call)
	if err != nil {
		return nil, err
	}
	operations := run.Operations()
	views, err := mailbox.GrantedMailboxes(ctx, operations)
	if err != nil {
		return nil, err
	}
	view, err := mailbox.FindMailbox(views, arguments.Mailbox)
	if err != nil {
		return nil, err
	}
	rules := storedRules(view)
	index, err := findRule(rules, arguments.Rule)
	if err != nil {
		return nil, err
	}
	updated, err := ruleFromArguments(view, &arguments, &rules[index])
	if err != nil {
		return nil, err
	}
	rules[index] = *updated
	if arguments.Position > 0 && arguments.Position-1 != index && arguments.Position-1 < len(rules) {
		moved := rules[index]
		rules = append(rules[:index], rules[index+1:]...)
		target := arguments.Position - 1
		rules = append(rules[:target], append([]ruleInput{moved}, rules[target:]...)...)
	}
	if err := saveRules(ctx, operations, view, rules); err != nil {
		return nil, err
	}
	result, err := tools.JSONResult(map[string]any{"rules": describeRules(view, rules)})
	if err != nil {
		return nil, err
	}
	result.Note = fmt.Sprintf("changed the rule %q", updated.Name)
	return result, nil
}

func runRuleRemove(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	arguments, err := tools.DecodeArguments[ruleArguments](call)
	if err != nil {
		return nil, err
	}
	operations := run.Operations()
	views, err := mailbox.GrantedMailboxes(ctx, operations)
	if err != nil {
		return nil, err
	}
	view, err := mailbox.FindMailbox(views, arguments.Mailbox)
	if err != nil {
		return nil, err
	}
	rules := storedRules(view)
	index, err := findRule(rules, arguments.Rule)
	if err != nil {
		return nil, err
	}
	removed := rules[index].Name
	rules = append(rules[:index], rules[index+1:]...)
	if err := saveRules(ctx, operations, view, rules); err != nil {
		return nil, err
	}
	result, err := tools.JSONResult(map[string]any{"rules": describeRules(view, rules)})
	if err != nil {
		return nil, err
	}
	result.Note = fmt.Sprintf("removed the rule %q", removed)
	return result, nil
}

type ruleTestArguments struct {
	Mailbox string          `json:"mailbox"`
	Folder  string          `json:"folder"`
	Rules   []ruleArguments `json:"rules"`
	Limit   int             `json:"limit"`
}

func runRuleTest(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	arguments, err := tools.DecodeArguments[ruleTestArguments](call)
	if err != nil {
		return nil, err
	}
	operations := run.Operations()
	views, err := mailbox.GrantedMailboxes(ctx, operations)
	if err != nil {
		return nil, err
	}
	view, err := mailbox.FindMailbox(views, arguments.Mailbox)
	if err != nil {
		return nil, err
	}
	folderId := ""
	if arguments.Folder != "" {
		folder, err := mailbox.FindFolder(view, arguments.Folder)
		if err != nil {
			return nil, err
		}
		folderId = folder.ID
	}
	rules := storedRules(view)
	if len(arguments.Rules) > 0 {
		rules = []ruleInput{}
		for index := range arguments.Rules {
			rule, err := ruleFromArguments(view, &arguments.Rules[index], nil)
			if err != nil {
				return nil, err
			}
			rules = append(rules, *rule)
		}
	}
	if len(rules) == 0 {
		return tools.TextResult("there are no rules to test"), nil
	}
	effect, err := testRules(ctx, operations, view, folderId, rules, arguments.Limit)
	if err != nil {
		return nil, err
	}
	return tools.JSONResult(map[string]any{"rules": effect})
}

func runRuleApply(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	arguments, err := tools.DecodeArguments[ruleTestArguments](call)
	if err != nil {
		return nil, err
	}
	operations := run.Operations()
	views, err := mailbox.GrantedMailboxes(ctx, operations)
	if err != nil {
		return nil, err
	}
	view, err := mailbox.FindMailbox(views, arguments.Mailbox)
	if err != nil {
		return nil, err
	}
	variables := map[string]any{"mailboxId": view.Mailbox.ID}
	if arguments.Folder != "" {
		folder, err := mailbox.FindFolder(view, arguments.Folder)
		if err != nil {
			return nil, err
		}
		variables["folderId"] = folder.ID
	}
	if arguments.Limit > 0 {
		variables["first"] = arguments.Limit
	}
	var result struct {
		ApplyMailboxRules map[string]any `json:"ApplyMailboxRules"`
	}
	if err := operations.Execute(ctx, `mutation ($mailboxId: String!, $folderId: String, $first: Int) { ApplyMailboxRules(mailboxId: $mailboxId, folderId: $folderId, first: $first) { considered matched moved marked flagged deleted skipped failed } }`, variables, &result); err != nil {
		return nil, err
	}
	answer, err := tools.JSONResult(result.ApplyMailboxRules)
	if err != nil {
		return nil, err
	}
	answer.Note = "applied the rules"
	return answer, nil
}

// ruleRisk is what a rule would do once it runs on its own: a rule that
// forwards sends mail out of the server for as long as it exists, and one
// that deletes throws mail away — neither is a write the person can undo,
// so both ask first, whatever the model was told by a message.
func ruleRisk(arguments json.RawMessage) tools.Risk {
	var call struct {
		Actions []struct {
			Kind string `json:"kind"`
		} `json:"actions"`
	}
	if json.Unmarshal(arguments, &call) != nil {
		return tools.RiskWrite
	}
	risk := tools.RiskWrite
	for _, action := range call.Actions {
		switch strings.ToLower(action.Kind) {
		case "forward":
			return tools.RiskOutward
		case "delete":
			risk = tools.RiskDestructive
		}
	}
	return risk
}
