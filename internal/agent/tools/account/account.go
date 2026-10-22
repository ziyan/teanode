// Package account is the person's own account: profile, tokens,
// sessions, app passwords, and what their permissions let them do.
package account

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/agent/tools/mailbox"
	"github.com/ziyan/teanode/internal/agent/tools/operator"
	"github.com/ziyan/teanode/internal/agent/tools/phone"
	"github.com/ziyan/teanode/internal/avatar"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

func init() {
	tools.Register(func() []*tools.Tool {
		return tools.Grouped(append([]*tools.Tool{
			{
				Name: "account_get", Family: tools.FamilyAccount, Risk: tools.RiskRead,
				Description: "The person's own account: name, username, notification address, language, zone, how they are described (pronoun), and their sessions, API tokens and passkeys.",
				Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
					result, err := operator.Execute(ctx, `query { GetCurrentUser { id username name email locale timezone timezoneMode pronoun avatarUrl } ListTokens { id name created expires lastUsed revoked } ListSessions { id current created expires lastUsed ip userAgent } ListPasskeys { id name createdAt } }`, nil)
					if err != nil {
						return nil, err
					}
					return tools.JSONResult(map[string]any{"account": result["GetCurrentUser"], "tokens": result["ListTokens"], "sessions": result["ListSessions"], "passkeys": result["ListPasskeys"]})
				},
			},
			{
				Name: "account_update", Family: tools.FamilyAccount, Risk: tools.RiskWrite,
				// The notification address is where the agent's mailed
				// briefings and notices go, so pointing it somewhere else
				// asks first; the other fields are ordinary writes.
				RiskOf: func(arguments json.RawMessage) tools.Risk {
					var asked struct {
						Email *string `json:"email"`
					}
					if err := json.Unmarshal(arguments, &asked); err != nil || asked.Email != nil {
						return tools.RiskOutward
					}
					return tools.RiskWrite
				},
				Description: "Change the person's own name, notification address, language, time zone or pronoun. The pronoun (he, she or they, empty to clear it) is how they are described: it words what the people in their circles are to them (a son rather than a child) and how you speak of them. Set it only on their own word, never from their name.",
				Parameters: tools.Object(map[string]any{
					"name":          tools.StringProperty("what to call them"),
					"email":         tools.StringProperty("where notifications go"),
					"locale":        tools.StringProperty("the language of the dashboard: en, ja, zh"),
					"timezone":      tools.StringProperty("an IANA zone"),
					"timezone_mode": tools.EnumProperty("follow the browser, or keep the zone", "auto", "fixed"),
					"pronoun":       tools.StringProperty("how they are described, as they said it: he, she or they; empty to clear it"),
				}),
				Preview: tools.PreviewOf(func(call struct {
					Name     *string `json:"name"`
					Email    *string `json:"email"`
					Locale   *string `json:"locale"`
					Timezone *string `json:"timezone"`
					Pronoun  *string `json:"pronoun"`
				}) string {
					changing := []string{}
					for label, value := range map[string]*string{"name": call.Name, "notification address": call.Email, "language": call.Locale, "time zone": call.Timezone, "pronoun": call.Pronoun} {
						if value != nil {
							changing = append(changing, label)
						}
					}
					sort.Strings(changing)
					if len(changing) == 0 {
						return "Change your account"
					}
					return "Change your " + tools.Some(changing, 4)
				}),
				Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
					arguments, err := tools.DecodeArguments[struct {
						Name         *string `json:"name"`
						Email        *string `json:"email"`
						Locale       *string `json:"locale"`
						Timezone     *string `json:"timezone"`
						TimezoneMode *string `json:"timezone_mode"`
						Pronoun      *string `json:"pronoun"`
					}](call)
					if err != nil {
						return nil, err
					}
					variables := map[string]any{"userId": tools.MustRun(ctx).Owner().ID}
					for key, value := range map[string]*string{"name": arguments.Name, "email": arguments.Email, "locale": arguments.Locale, "timezone": arguments.Timezone, "timezoneMode": arguments.TimezoneMode, "pronoun": arguments.Pronoun} {
						if value != nil {
							variables[key] = *value
						}
					}
					if len(variables) == 1 {
						return nil, fmt.Errorf("nothing to change")
					}
					if _, err := operator.Execute(ctx, `mutation ($userId: String!, $name: String, $email: String, $locale: String, $timezone: String, $timezoneMode: String, $pronoun: String) { UpdateUser(userId: $userId, name: $name, email: $email, locale: $locale, timezone: $timezone, timezoneMode: $timezoneMode, pronoun: $pronoun) { id } }`, variables); err != nil {
						return nil, err
					}
					return tools.TextResult("changed the account"), nil
				},
			},
			{
				Name: "account_set_avatar", Family: tools.FamilyAccount, Risk: tools.RiskWrite,
				Description: "Use a picture from this conversation as the person's own picture, the one other people and their own screens show beside their name. A PNG or JPEG; it is cropped to a square. It replaces the picture they have.",
				Parameters:  tools.Object(map[string]any{"attachment_id": tools.StringProperty("the picture's attachment id in this conversation")}, "attachment_id"),
				Preview: tools.PreviewOf(func(call struct {
					AttachmentID string `json:"attachment_id"`
				}) string {
					return "Use a picture from this conversation as your picture"
				}),
				Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
					arguments, err := tools.DecodeArguments[struct {
						AttachmentID string `json:"attachment_id"`
					}](call)
					if err != nil {
						return nil, err
					}
					run, err := tools.RunFrom(ctx)
					if err != nil {
						return nil, err
					}
					attachmentId := strings.TrimSpace(arguments.AttachmentID)
					if attachmentId == "" {
						return nil, fmt.Errorf("the attachment_id is needed")
					}
					var attachment *models.AgentAttachment
					if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
						attachment, err = tx.GetAgentAttachment(attachmentId)
						return err
					}); err != nil {
						return nil, err
					}
					// This conversation's own file, as share_file requires: the
					// ids of files in other conversations are readable.
					if attachment == nil || attachment.AgentID != run.Agent().ID ||
						(attachment.ConversationID != "" && attachment.ConversationID != tools.ConversationIDOf(run)) {
						return nil, fmt.Errorf("there is no attachment %q in this conversation", attachmentId)
					}
					if attachment.Size > avatar.MaxInputBytes {
						return nil, fmt.Errorf("the picture is %d bytes; a picture may be at most %d", attachment.Size, avatar.MaxInputBytes)
					}
					content, err := run.Storage().GetFile(ctx, attachment.ID)
					if err != nil {
						return nil, err
					}
					if _, err := operator.Execute(ctx, `mutation ($userId: String, $content: String!) { SetUserAvatar(userId: $userId, content: $content) { id } }`,
						map[string]any{"userId": run.Owner().ID, "content": base64.StdEncoding.EncodeToString(content)}); err != nil {
						return nil, err
					}
					return tools.TextResult("changed the picture"), nil
				},
			},
			{
				Name: "account_remove_avatar", Family: tools.FamilyAccount, Risk: tools.RiskWrite,
				Description: "Take the person's own picture away, so they are shown as their initial on a color.",
				Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
					if _, err := operator.Execute(ctx, `mutation ($userId: String) { RemoveUserAvatar(userId: $userId) { id } }`,
						map[string]any{"userId": tools.MustRun(ctx).Owner().ID}); err != nil {
						return nil, err
					}
					return tools.TextResult("removed the picture"), nil
				},
			},
			{
				Name: "token_manage", Family: tools.FamilyAccount, Risk: tools.RiskWrite,
				Description: "The person's API tokens: make one (shown once, to them, exactly), rename one or give it a new lifetime from now, or revoke one.",
				Parameters:  tools.Object(map[string]any{"action": tools.EnumProperty("create, update or revoke", "create", "update", "revoke"), "name": tools.StringProperty("for create: what the token is for; for update: its new name"), "token_id": tools.StringProperty("for update and revoke: the token"), "lifetime": tools.StringProperty("for create and update: how long it lives from now, such as 30d, or never")}, "action"),
				Preview: tools.PreviewOf(func(call struct {
					Action   string `json:"action"`
					Name     string `json:"name"`
					Lifetime string `json:"lifetime"`
				}) string {
					if call.Action == "revoke" {
						return "Revoke one of your API tokens"
					}
					if call.Action == "update" {
						if call.Lifetime != "" {
							return "Change one of your API tokens to last " + call.Lifetime + " from now"
						}
						return "Rename one of your API tokens to " + tools.Named(call.Name, "something else")
					}
					lasting := ""
					if call.Lifetime != "" {
						lasting = ", lasting " + call.Lifetime
					}
					// A token is a way into the whole account for as long
					// as it lives, so the card says both.
					return "Make an API token called " + tools.Named(call.Name, "a token") + lasting
				}),
				// Making one hands out a way into the whole account, for
				// as long as it lives, to whoever ends up holding it --
				// so the person is asked. It was an ordinary write, and
				// the answer carries the token itself, so an agent
				// following instructions it read in a message could mint
				// one and nobody was ever asked anything.
				//
				// Giving a token a longer life is the same grant again, so
				// it asks the same way; a new name alone is an ordinary
				// write.
				RiskOf: func(arguments json.RawMessage) tools.Risk {
					switch tools.ActionOf(arguments) {
					case "revoke":
						return tools.RiskDestructive
					case "update":
						var call struct {
							Lifetime string `json:"lifetime"`
						}
						if json.Unmarshal(arguments, &call) == nil && call.Lifetime == "" {
							return tools.RiskWrite
						}
					}
					return tools.RiskGranting
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
					case "update":
						variables := map[string]any{"tokenId": arguments.TokenID}
						if arguments.Name != "" {
							variables["name"] = arguments.Name
						}
						if arguments.Lifetime != "" {
							variables["lifetime"] = arguments.Lifetime
						}
						result, err := operator.Execute(ctx, `mutation ($tokenId: String!, $name: String, $lifetime: String) { UpdateToken(tokenId: $tokenId, name: $name, lifetime: $lifetime) { id name expires } }`, variables)
						if err != nil {
							return nil, err
						}
						return tools.JSONResult(result["UpdateToken"])
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
				Preview: tools.PreviewOf(func(call struct {
					SessionID string `json:"session_id"`
				}) string {
					// Which matters: one is a device, none is every device
					// the person is signed in on but this one.
					if strings.TrimSpace(call.SessionID) == "" {
						return "Sign you out everywhere but here"
					}
					return "Sign you out of one other browser"
				}),
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
				Preview: tools.PreviewOf(func(call struct {
					Action  string `json:"action"`
					Mailbox string `json:"mailbox"`
					Name    string `json:"name"`
				}) string {
					if call.Action == "remove" {
						return "Remove an app password" + tools.In(call.Mailbox) + ", so whatever signs in with it stops"
					}
					return "Make an app password for " + tools.Named(call.Name, "a mail program") + tools.In(call.Mailbox)
				}),
				RiskOf: func(arguments json.RawMessage) tools.Risk {
					if tools.ActionOf(arguments) == "remove" {
						return tools.RiskDestructive
					}
					if tools.ActionOf(arguments) == "list" {
						return tools.RiskRead
					}
					// Making one is a password for the mailbox, which
					// reads and sends mail from any program that has it.
					return tools.RiskGranting
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
		}, phone.Tools()...),
			// Reading the account and changing it are one tool, and the
			// phones signed in to it are part of it.
			tools.Group{
				Name: "account", Family: tools.FamilyAccount,
				Description: "The person's own account on this server: who they are, how they are reached, what they have chosen, their picture, and their phones running the TeaNode app.",
				Members: []tools.Member{
					{Action: "get", Tool: "account_get"},
					{Action: "update", Tool: "account_update"},
					{Action: "set_avatar", Tool: "account_set_avatar"},
					{Action: "remove_avatar", Tool: "account_remove_avatar"},
					{Action: "phones", Tool: "phone_list"},
					{Action: "pair_phone", Tool: "phone_pair"},
					{Action: "set_up_phone_mail", Tool: "phone_setup_profile"},
					{Action: "set_up_device", Tool: "phone_setup_profile"},
					{Action: "rename_phone", Tool: "phone_rename"},
					{Action: "phone_notifications", Tool: "phone_notify"},
					{Action: "test_phone", Tool: "phone_test"},
					{Action: "remove_phone", Tool: "phone_remove"},
				},
			},
		)
	})
}
