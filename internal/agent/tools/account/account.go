// Package account is the person's own account: profile, tokens,
// sessions, app passwords, and what their permissions let them do.
package account

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/agent/tools/mailbox"
	"github.com/ziyan/teanode/internal/agent/tools/operator"
	"github.com/ziyan/teanode/internal/models"
)

func init() {
	tools.Register(func() []*tools.Tool {
		return []*tools.Tool{
			{
				Name: "account_get", Family: tools.FamilyAccount, Risk: tools.RiskRead,
				Description: "The person's own account: name, username, notification address, language, zone, and their sessions, API tokens and passkeys.",
				Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
					result, err := operator.Execute(ctx, `query { GetCurrentUser { id username name email locale timezone timezoneMode } ListTokens { id name created expires lastUsed revoked } ListSessions { id current created expires lastUsed ip userAgent } ListPasskeys { id name createdAt } }`, nil)
					if err != nil {
						return nil, err
					}
					return tools.JSONResult(map[string]any{"account": result["GetCurrentUser"], "tokens": result["ListTokens"], "sessions": result["ListSessions"], "passkeys": result["ListPasskeys"]})
				},
			},
			{
				Name: "account_update", Family: tools.FamilyAccount, Risk: tools.RiskWrite,
				Description: "Change the person's own name, notification address, language or time zone.",
				Parameters: tools.Object(map[string]any{
					"name":          tools.StringProperty("what to call them"),
					"email":         tools.StringProperty("where notifications go"),
					"locale":        tools.StringProperty("the language of the dashboard: en, ja, zh"),
					"timezone":      tools.StringProperty("an IANA zone"),
					"timezone_mode": tools.EnumProperty("follow the browser, or keep the zone", "auto", "fixed"),
				}),
				Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
					arguments, err := tools.DecodeArguments[struct {
						Name         *string `json:"name"`
						Email        *string `json:"email"`
						Locale       *string `json:"locale"`
						Timezone     *string `json:"timezone"`
						TimezoneMode *string `json:"timezone_mode"`
					}](call)
					if err != nil {
						return nil, err
					}
					variables := map[string]any{"userId": tools.MustRun(ctx).Owner().ID}
					for key, value := range map[string]*string{"name": arguments.Name, "email": arguments.Email, "locale": arguments.Locale, "timezone": arguments.Timezone, "timezoneMode": arguments.TimezoneMode} {
						if value != nil {
							variables[key] = *value
						}
					}
					if len(variables) == 1 {
						return nil, fmt.Errorf("nothing to change")
					}
					if _, err := operator.Execute(ctx, `mutation ($userId: String!, $name: String, $email: String, $locale: String, $timezone: String, $timezoneMode: String) { UpdateUser(userId: $userId, name: $name, email: $email, locale: $locale, timezone: $timezone, timezoneMode: $timezoneMode) { id } }`, variables); err != nil {
						return nil, err
					}
					return tools.TextResult("changed the account"), nil
				},
			},
			{
				Name: "token_manage", Family: tools.FamilyAccount, Risk: tools.RiskWrite,
				Description: "The person's API tokens: make one (shown once, to them, exactly) or revoke one.",
				Parameters:  tools.Object(map[string]any{"action": tools.EnumProperty("create or revoke", "create", "revoke"), "name": tools.StringProperty("for create: what the token is for"), "token_id": tools.StringProperty("for revoke: the token"), "lifetime": tools.StringProperty("for create: how long it lives, such as 30d")}, "action"),
				RiskOf: func(arguments json.RawMessage) tools.Risk {
					if strings.Contains(string(arguments), `"revoke"`) {
						return tools.RiskDestructive
					}
					return tools.RiskWrite
				},
				Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
					arguments, err := tools.DecodeArguments[struct {
						Action   string `json:"action"`
						Name     string `json:"name"`
						TokenID  string `json:"token_id"`
						Lifetime string `json:"lifetime"`
					}](call)
					if err != nil {
						return nil, err
					}
					switch arguments.Action {
					case "create":
						variables := map[string]any{"name": arguments.Name}
						if arguments.Lifetime != "" {
							variables["lifetime"] = arguments.Lifetime
						}
						result, err := operator.Execute(ctx, `mutation ($name: String!, $lifetime: String) { CreateToken(name: $name, lifetime: $lifetime) { token { id name expires } secret } }`, variables)
						if err != nil {
							return nil, err
						}
						answer, err := tools.JSONResult(result["CreateToken"])
						if err != nil {
							return nil, err
						}
						answer.ShowVerbatim = true
						return answer, nil
					case "revoke":
						if _, err := operator.Execute(ctx, `mutation ($tokenId: String!) { DeleteToken(tokenId: $tokenId) }`, map[string]any{"tokenId": arguments.TokenID}); err != nil {
							return nil, err
						}
						return tools.TextResult("revoked the token"), nil
					}
					return nil, fmt.Errorf("%q is not an action of token_manage", arguments.Action)
				},
			},
			{
				Name: "session_revoke", Family: tools.FamilyAccount, Risk: tools.RiskWrite,
				Description: "Sign the person out elsewhere: one session, or every session but this one.",
				Parameters:  tools.Object(map[string]any{"session_id": tools.StringProperty("one session, from account_get; all of them when absent")}),
				Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
					arguments, err := tools.DecodeArguments[struct {
						SessionID string `json:"session_id"`
					}](call)
					if err != nil {
						return nil, err
					}
					if arguments.SessionID != "" {
						if _, err := operator.Execute(ctx, `mutation ($sessionId: String!) { RevokeSession(sessionId: $sessionId) }`, map[string]any{"sessionId": arguments.SessionID}); err != nil {
							return nil, err
						}
						return tools.TextResult("revoked the session"), nil
					}
					if _, err := operator.Execute(ctx, `mutation { RevokeAllSessions { authenticated username } }`, nil); err != nil {
						return nil, err
					}
					return tools.TextResult("revoked every other session"), nil
				},
			},
			{
				Name: "app_password_manage", Family: tools.FamilyAccount, Risk: tools.RiskWrite, Permissions: []models.Permission{models.PermissionMailboxManage},
				Description: "App passwords for a mailbox, which mail programs sign in with: list, make (shown once, exactly) or remove.",
				Parameters:  tools.Object(map[string]any{"action": tools.EnumProperty("what to do", "list", "create", "remove"), "mailbox": tools.StringProperty("the mailbox, by name or id"), "name": tools.StringProperty("for create: which program"), "app_password_id": tools.StringProperty("for remove: the app password")}, "action"),
				RiskOf: func(arguments json.RawMessage) tools.Risk {
					if strings.Contains(string(arguments), `"remove"`) {
						return tools.RiskDestructive
					}
					if strings.Contains(string(arguments), `"list"`) {
						return tools.RiskRead
					}
					return tools.RiskWrite
				},
				Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
					arguments, err := tools.DecodeArguments[struct {
						Action        string `json:"action"`
						Mailbox       string `json:"mailbox"`
						Name          string `json:"name"`
						AppPasswordID string `json:"app_password_id"`
					}](call)
					if err != nil {
						return nil, err
					}
					views, err := mailbox.GrantedMailboxes(ctx, tools.MustRun(ctx).Operations())
					if err != nil {
						return nil, err
					}
					switch arguments.Action {
					case "list":
						view, err := mailbox.FindMailbox(views, arguments.Mailbox)
						if err != nil {
							return nil, err
						}
						result, err := operator.Execute(ctx, `query ($mailboxId: String!) { ListMailboxAppPasswords(mailboxId: $mailboxId) { id name createdAt lastUsedAt } }`, map[string]any{"mailboxId": view.Mailbox.ID})
						if err != nil {
							return nil, err
						}
						return tools.JSONResult(map[string]any{"mailbox": view.Mailbox.Name, "app_passwords": result["ListMailboxAppPasswords"]})
					case "create":
						view, err := mailbox.FindMailbox(views, arguments.Mailbox)
						if err != nil {
							return nil, err
						}
						if strings.TrimSpace(arguments.Name) == "" {
							return nil, fmt.Errorf("an app password needs a name: the program it is for")
						}
						result, err := operator.Execute(ctx, `mutation ($mailboxId: String!, $name: String!) { CreateMailboxAppPassword(mailboxId: $mailboxId, name: $name) { password username appPassword { id name } } }`, map[string]any{"mailboxId": view.Mailbox.ID, "name": arguments.Name})
						if err != nil {
							return nil, err
						}
						answer, err := tools.JSONResult(result["CreateMailboxAppPassword"])
						if err != nil {
							return nil, err
						}
						answer.ShowVerbatim = true
						return answer, nil
					case "remove":
						if _, err := operator.Execute(ctx, `mutation ($appPasswordId: String!) { DeleteMailboxAppPassword(appPasswordId: $appPasswordId) }`, map[string]any{"appPasswordId": arguments.AppPasswordID}); err != nil {
							return nil, err
						}
						return tools.TextResult("removed the app password"), nil
					}
					return nil, fmt.Errorf("%q is not an action of app_password_manage", arguments.Action)
				},
			},
			{
				Name: "access_explain", Family: tools.FamilyAccount, Core: true, Risk: tools.RiskRead,
				Description: "What the person may do on this server, in words, and why a tool is or is not available to them. Use it before saying that something cannot be done.",
				Parameters:  tools.Object(map[string]any{"tool": tools.StringProperty("a tool to explain; every permission when absent")}),
				Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
					arguments, err := tools.DecodeArguments[struct {
						Tool string `json:"tool"`
					}](call)
					if err != nil {
						return nil, err
					}
					run := tools.MustRun(ctx)
					permissions := run.Operations().Permissions()
					configuration := run.Configuration()
					if name := strings.TrimSpace(arguments.Tool); name != "" {
						tool := tools.Build().Get(name)
						if tool == nil {
							return tools.TextResult("there is no tool named %q", name), nil
						}
						reasons := []string{}
						if !tools.AllowedByPermissions(tool, permissions) {
							needs := make([]string, 0, len(tool.Permissions))
							for _, permission := range tool.Permissions {
								needs = append(needs, string(permission))
							}
							reasons = append(reasons, "the person lacks a permission it needs: one of "+strings.Join(needs, ", "))
						}
						if tools.Listed(configuration.Agent.Tools.Disabled, tool) {
							reasons = append(reasons, "the operator switched it off for agents on this server")
						}
						if run.ReadOnly() && tool.Risk != tools.RiskRead {
							reasons = append(reasons, "this conversation is read-only")
						}
						if len(reasons) == 0 {
							return tools.JSONResult(map[string]any{"tool": name, "available": true, "risk": tool.Risk, "asks_first": tools.NeedsConfirmation(tool, nil, &configuration.Agent.Tools, run.Agent())})
						}
						return tools.JSONResult(map[string]any{"tool": name, "available": false, "because": reasons})
					}
					return tools.JSONResult(map[string]any{"may": tools.PermissionWords(permissions), "offered_tools": len(run.Offered()), "switched_off_by_operator": configuration.Agent.Tools.Disabled})
				},
			},
		}
	})
}
