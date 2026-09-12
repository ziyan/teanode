package connected

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/agent/tools"
)

// Skills are tools that arrive without a release. The agent can look at
// what is installed without any particular permission; installing one
// changes what every person on this server is offered, and the API asks
// for the same permission as changing the settings.

func init() {
	tools.Register(func() []*tools.Tool {
		return []*tools.Tool{
			{
				Name: "skill", Family: tools.FamilyServers, Risk: tools.RiskWrite,
				Description: "Skills: files of declarations from a signed registry whose tools become yours. List what this server has installed and what each brings, search the registry for one, install or update it, or take one away. Secrets says which values the installed skills are waiting on from this person, which is why one of their tools may be refusing to work. Scope settles who fills a skill's values in here: one set for the whole server, or each person's own -- which is the difference between one camera system in a household and twenty people each with their own. Installing changes what everybody on this server is offered, so it needs the person to manage this server; listing does not. A skill that runs commands runs them on a computer the person attached, never on this server.",
				Parameters: tools.Object(map[string]any{
					"action": tools.EnumProperty("what to do", "list", "search", "secrets", "install", "update", "remove", "enable", "disable", "scope"),
					"name":   tools.StringProperty("the skill, for install, update, remove, enable, disable and scope"),
					"scope":  tools.EnumProperty("for scope: who fills this skill's secrets in here -- operator for one set of values for the whole server, person for each person's own, skill to leave it to what the skill declares", "operator", "person", "skill"),
					"query":  tools.StringProperty("for search: words to narrow what the registry offers; leave it out to see everything, which is a short list"),
				}, "action"),
				Guidance: "skill: a tool of a skill that refuses because a value is missing is answered with secrets, which says what the person has not filled in -- tell them where to set it and never ask them to type a secret to you, because what they type is kept in the conversation. Look before you install -- what is installed may already do it, and the registry is small enough to read. Installing one is the person's decision as much as yours: say what it brings and what it would let you do before you ask for it. A skill is checked against the registry's signature before anything is kept, so a refusal means the file is not what was signed for, not that the network failed. When somebody wants a skill pointed at equipment of their own rather than the deployment's, scope person is the answer: they then fill in the address and the credential themselves, and neither is shared with anybody else here.",
				Preview: func(arguments json.RawMessage) string {
					var call skillRequest
					if err := json.Unmarshal(arguments, &call); err != nil {
						return "Skills: " + strings.TrimSpace(string(arguments))
					}
					switch call.Action {
					case "install", "update":
						return fmt.Sprintf("Install the %s skill on this server, offering its tools to everybody here", call.Name)
					case "remove":
						return fmt.Sprintf("Take the %s skill away from this server, with the tools it brought", call.Name)
					case "disable":
						return fmt.Sprintf("Stop offering the %s skill's tools", call.Name)
					case "scope":
						if call.Scope == "person" {
							return fmt.Sprintf("Have the %s skill ask each person here for their own values", call.Name)
						}
						if call.Scope == "operator" {
							return fmt.Sprintf("Have the %s skill use one set of values for everybody here", call.Name)
						}
						return fmt.Sprintf("Leave who fills the %s skill's values in to what the skill declares", call.Name)
					}
					return "Skills: " + strings.TrimSpace(string(arguments))
				},
				RiskOf: func(arguments json.RawMessage) tools.Risk {
					var call skillRequest
					if err := json.Unmarshal(arguments, &call); err != nil {
						return tools.RiskWrite
					}
					switch call.Action {
					case "list", "search", "secrets":
						return tools.RiskRead
					case "install", "update", "remove", "scope":
						// It changes what everybody here is offered, or who
						// they are offered it as, so it asks first, as
						// declaring a connected server does.
						return tools.RiskDestructive
					}
					return tools.RiskWrite
				},
				Run: runSkill,
			},
		}
	})
}

type skillRequest struct {
	Action string `json:"action"`
	Name   string `json:"name"`
	Query  string `json:"query"`
	Scope  string `json:"scope"`
}

// skillView is an installed skill as the API describes it.
type skillView struct {
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	Version         string   `json:"version"`
	Publisher       string   `json:"publisher"`
	Enabled         bool     `json:"enabled"`
	Readable        bool     `json:"readable"`
	Problem         string   `json:"problem,omitempty"`
	Secrets         []string `json:"secrets"`
	PersonalSecrets []string `json:"personalSecrets"`
	Scope           string   `json:"scope"`
	Tools           []*struct {
		Name          string `json:"name"`
		Description   string `json:"description"`
		Kind          string `json:"kind"`
		NeedsComputer bool   `json:"needsComputer"`
	} `json:"tools"`
}

// skillSecretView is one value an installed skill asks this person for.
// Whether it is set comes back; the value never does.
type skillSecretView struct {
	Skill       string `json:"skill"`
	Key         string `json:"key"`
	Description string `json:"description"`
	Set         bool   `json:"set"`
}

// skillOffer is one skill the registry publishes.
type skillOffer struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Version     string   `json:"version"`
	Tags        []string `json:"tags"`
	Installed   string   `json:"installed,omitempty"`
	Newer       bool     `json:"newer"`
}

const (
	documentInstalledSkills = `query { ListAgentSkills { name description version publisher enabled readable problem scope secrets personalSecrets
		tools { name description kind needsComputer } } }`

	documentOfferedSkills = `query ($query: String) { SearchAgentSkills(query: $query) { name description version tags installed newer } }`

	documentInstallSkill = `mutation ($name: String!) { InstallAgentSkill(name: $name) { name description version publisher enabled readable problem scope secrets personalSecrets
		tools { name description kind needsComputer } } }`

	documentRemoveSkill = `mutation ($name: String!) { RemoveAgentSkill(name: $name) }`

	documentSkillSecrets = `query { ListAgentSkillSecrets { skill key description set } }`

	documentSetSkillEnabled = `mutation ($name: String!, $enabled: Boolean!) { SetAgentSkillEnabled(name: $name, enabled: $enabled) { name enabled } }`

	documentSetSkillScope = `mutation ($name: String!, $scope: String!) { SetAgentSkillScope(name: $name, scope: $scope) { name scope secrets personalSecrets } }`
)

func runSkill(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[skillRequest](call)
	if err != nil {
		return nil, err
	}
	run := tools.MustRun(ctx)
	name := strings.ToLower(strings.TrimSpace(arguments.Name))

	switch arguments.Action {
	case "list":
		var answer struct {
			ListAgentSkills []*skillView `json:"ListAgentSkills"`
		}
		if err := run.Operations().Execute(ctx, documentInstalledSkills, nil, &answer); err != nil {
			return nil, err
		}
		return tools.JSONResult(map[string]any{"installed": answer.ListAgentSkills})

	case "search":
		var answer struct {
			SearchAgentSkills []*skillOffer `json:"SearchAgentSkills"`
		}
		variables := map[string]any{}
		if words := strings.TrimSpace(arguments.Query); words != "" {
			variables["query"] = words
		}
		if err := run.Operations().Execute(ctx, documentOfferedSkills, variables, &answer); err != nil {
			return nil, err
		}
		described := map[string]any{"registry": answer.SearchAgentSkills}
		if len(answer.SearchAgentSkills) == 0 && strings.TrimSpace(arguments.Query) != "" {
			// "Nothing matches that word" and "the registry is empty" read
			// the same in an empty list, and the first was being reported
			// as the second.
			described["nothing_matched"] = arguments.Query
			described["do_this_next"] = "Nothing in the registry matches that word. Search again with no query to see everything it offers, which is a short list, before telling the person there is nothing."
		}
		return tools.JSONResult(described)

	case "secrets":
		var answer struct {
			ListAgentSkillSecrets []*skillSecretView `json:"ListAgentSkillSecrets"`
		}
		if err := run.Operations().Execute(ctx, documentSkillSecrets, nil, &answer); err != nil {
			return nil, err
		}
		var waiting []string
		for _, secret := range answer.ListAgentSkillSecrets {
			if !secret.Set {
				waiting = append(waiting, secret.Skill+"."+secret.Key)
			}
		}
		described := map[string]any{"asked_of_this_person": answer.ListAgentSkillSecrets}
		if len(waiting) > 0 {
			// Never offer to take the value in the conversation: it would
			// be written into the transcript and sent to a model.
			described["do_this_next"] = fmt.Sprintf("These are not set yet: %s. Tell the person to set them on their agent page, or with `teanode agent skill secret set <skill> <key> -`. Do not ask them to type a secret to you.", strings.Join(waiting, ", "))
		}
		return tools.JSONResult(described)

	case "install", "update":
		if name == "" {
			return nil, fmt.Errorf("which skill: search says what the registry offers")
		}
		var answer struct {
			InstallAgentSkill *skillView `json:"InstallAgentSkill"`
		}
		if err := run.Operations().Execute(ctx, documentInstallSkill, map[string]any{"name": name}, &answer); err != nil {
			return nil, err
		}
		installed := answer.InstallAgentSkill
		described := map[string]any{"installed": installed}
		if installed != nil {
			var next []string
			if len(installed.Secrets) > 0 {
				next = append(next, fmt.Sprintf("An operator must put %s under agent.skillSecrets, once for the whole server.", strings.Join(installed.Secrets, ", ")))
			}
			if len(installed.PersonalSecrets) > 0 {
				next = append(next, fmt.Sprintf("Each person fills in %s for themselves on their agent page, or with `teanode agent skill secret set %s %s`; an operator cannot set it for them, and you must not ask them to type it to you.",
					strings.Join(installed.PersonalSecrets, ", "), installed.Name, installed.PersonalSecrets[0]))
			}
			if len(next) > 0 {
				described["do_this_next"] = strings.Join(next, " ") + " Tell the person which values it is waiting for."
			}
		}
		result, err := tools.JSONResult(described)
		if err != nil {
			return nil, err
		}
		result.Note = "installed the " + name + " skill"
		return result, nil

	case "remove":
		if name == "" {
			return nil, fmt.Errorf("which skill")
		}
		var answer struct {
			RemoveAgentSkill bool `json:"RemoveAgentSkill"`
		}
		if err := run.Operations().Execute(ctx, documentRemoveSkill, map[string]any{"name": name}, &answer); err != nil {
			return nil, err
		}
		result := tools.TextResult("took the %s skill away, with the tools it brought", name)
		result.Note = "removed the " + name + " skill"
		return result, nil

	case "enable", "disable":
		if name == "" {
			return nil, fmt.Errorf("which skill")
		}
		enabled := arguments.Action == "enable"
		var answer struct {
			SetAgentSkillEnabled *skillView `json:"SetAgentSkillEnabled"`
		}
		if err := run.Operations().Execute(ctx, documentSetSkillEnabled, map[string]any{"name": name, "enabled": enabled}, &answer); err != nil {
			return nil, err
		}
		if enabled {
			return tools.TextResult("%s is offered again", name), nil
		}
		return tools.TextResult("%s is installed but not offered", name), nil

	case "scope":
		if name == "" {
			return nil, fmt.Errorf("which skill")
		}
		// "skill" is how the person says "leave it as the skill declares",
		// which over an argument is easier to say than nothing at all.
		scope := strings.ToLower(strings.TrimSpace(arguments.Scope))
		if scope == "skill" || scope == "declared" {
			scope = ""
		}
		var answer struct {
			SetAgentSkillScope *skillView `json:"SetAgentSkillScope"`
		}
		if err := run.Operations().Execute(ctx, documentSetSkillScope, map[string]any{"name": name, "scope": scope}, &answer); err != nil {
			return nil, err
		}
		settled := answer.SetAgentSkillScope
		if settled == nil {
			return nil, fmt.Errorf("%s answered nothing", name)
		}
		switch settled.Scope {
		case "person":
			result := tools.TextResult("%s now asks each person here for their own %s; nobody's is used for anybody else, and each of them sets it on their agent page or with `teanode agent skill secret set`",
				name, strings.Join(settled.PersonalSecrets, ", "))
			result.Note = name + ": each person's own values"
			return result, nil
		case "operator":
			result := tools.TextResult("%s now takes %s from agent.skillSecrets, one set of values for everybody here",
				name, strings.Join(settled.Secrets, ", "))
			result.Note = name + ": one set of values for everybody"
			return result, nil
		}
		return tools.TextResult("%s fills its values in as the skill declares them: %d for an operator, %d for each person",
			name, len(settled.Secrets), len(settled.PersonalSecrets)), nil
	}
	return nil, fmt.Errorf("%q is not an action of skill", arguments.Action)
}
