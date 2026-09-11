// Package subscription is the mailing lists a mailbox receives and the
// ways out of each.
package subscription

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/agent/tools/mailbox"
	"github.com/ziyan/teanode/internal/models"
)

func init() {
	tools.Register(func() []*tools.Tool {
		return []*tools.Tool{
			{
				Name: "subscription", Family: tools.FamilyMailbox, Core: true, Risk: tools.RiskWrite,
				Permissions: []models.Permission{models.PermissionMailWrite},
				Description: "The mailing lists a mailbox receives and the ways out of each: list them (how much arrives, whether the sender offers a one-click way to leave, whether it is muted), mute one (it keeps arriving, straight to Archive), unmute, or leave one — the server asks the sender to stop the way the sender said, by its own unsubscribe address or link, which is what works. A hand-written unsubscribe mail is not what a list honours; use leave.",
				Parameters: tools.Object(map[string]any{
					"action":   tools.EnumProperty("what to do", "list", "mute", "unmute", "leave"),
					"mailbox":  mailbox.MailboxProperty,
					"key":      tools.StringProperty("for mute, unmute and leave: the list's key, from a list row"),
					"matching": tools.StringProperty("for list: words in the list's name, address or key"),
					"limit":    tools.IntegerProperty("for list: how many rows, 30 by default"),
				}, "action"),
				Guidance: "subscription: to stop a newsletter, leave it with the subscription tool; never draft an unsubscribe mail by hand.",
				RiskOf: func(arguments json.RawMessage) tools.Risk {
					var call subscriptionArguments
					_ = json.Unmarshal(arguments, &call)
					if call.Action == "leave" {
						return tools.RiskOutward
					}
					return tools.RiskWrite
				},
				Run: runSubscription,
			},
		}
	})
}

type subscriptionArguments struct {
	Action   string `json:"action"`
	Mailbox  string `json:"mailbox"`
	Key      string `json:"key"`
	Matching string `json:"matching"`
	Limit    int    `json:"limit"`
}

const subscriptionFields = `{ id key name from count unread lastAt lastItemId oneClick unsubscribe stripped mutedAt }`

func runSubscription(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	arguments, err := tools.DecodeArguments[subscriptionArguments](call)
	if err != nil {
		return nil, err
	}
	operations := run.Operations()
	views, err := mailbox.GrantedMailboxes(ctx, operations)
	if err != nil {
		return nil, err
	}
	targets := views
	if arguments.Mailbox != "" {
		view, err := mailbox.FindMailbox(views, arguments.Mailbox)
		if err != nil {
			return nil, err
		}
		targets = []*mailbox.MailboxView{view}
	}
	if arguments.Action == "list" {
		limit := arguments.Limit
		if limit <= 0 {
			limit = 30
		}
		var rows []map[string]any
		for _, view := range targets {
			var result struct {
				ListMailboxSubscriptions struct {
					Subscriptions []models.MailboxSubscription `json:"subscriptions"`
					Total         int64                        `json:"total"`
				} `json:"ListMailboxSubscriptions"`
			}
			variables := map[string]any{"mailboxId": view.Mailbox.ID, "first": limit}
			if arguments.Matching != "" {
				variables["matching"] = arguments.Matching
			}
			if err := operations.Execute(ctx, `query ($mailboxId: String!, $first: Int, $matching: String) { ListMailboxSubscriptions(mailboxId: $mailboxId, first: $first, matching: $matching) { subscriptions `+subscriptionFields+` total } }`, variables, &result); err != nil {
				return nil, err
			}
			for _, subscription := range result.ListMailboxSubscriptions.Subscriptions {
				rows = append(rows, map[string]any{
					"mailbox": view.Mailbox.Name, "key": subscription.Key, "name": subscription.Name, "from": subscription.From,
					"messages": subscription.Count, "unread": subscription.Unread, "last": subscription.LastAt.Format("2006-01-02"),
					"one_click": subscription.OneClick, "can_leave": len(subscription.Unsubscribe) > 0, "muted": subscription.MutedAt != nil,
				})
			}
		}
		if rows == nil {
			rows = []map[string]any{}
		}
		result, err := tools.JSONResult(map[string]any{"rows": rows})
		if err != nil {
			return nil, err
		}
		result.Untrusted = true
		result.Note = fmt.Sprintf("%d list(s)", len(rows))
		return result, nil
	}
	if strings.TrimSpace(arguments.Key) == "" {
		return nil, fmt.Errorf("%s needs the list's key", arguments.Action)
	}
	if len(targets) != 1 {
		return nil, fmt.Errorf("say which mailbox")
	}
	view := targets[0]
	var discard map[string]any
	switch arguments.Action {
	case "mute", "unmute":
		if err := operations.Execute(ctx, `mutation ($mailboxId: String!, $key: String!, $muted: Boolean!) { MuteMailboxSubscription(mailboxId: $mailboxId, key: $key, muted: $muted) { id } }`, map[string]any{"mailboxId": view.Mailbox.ID, "key": arguments.Key, "muted": arguments.Action == "mute"}, &discard); err != nil {
			return nil, err
		}
		result := tools.TextResult("%s: %s", arguments.Action, arguments.Key)
		result.Note = arguments.Action + " " + arguments.Key
		return result, nil
	case "leave":
		if !call.Confirmed {
			return nil, fmt.Errorf("leaving a list asks the sender to stop and needs the person's confirmation")
		}
		var result struct {
			UnsubscribeMailboxSubscription models.MailboxSubscription `json:"UnsubscribeMailboxSubscription"`
		}
		if err := operations.Execute(ctx, `mutation ($mailboxId: String!, $key: String!) { UnsubscribeMailboxSubscription(mailboxId: $mailboxId, key: $key) `+subscriptionFields+` }`, map[string]any{"mailboxId": view.Mailbox.ID, "key": arguments.Key}, &result); err != nil {
			return nil, err
		}
		answer := tools.TextResult("asked %s to stop; the request went the way the sender said", arguments.Key)
		answer.Note = "left " + result.UnsubscribeMailboxSubscription.Name
		return answer, nil
	default:
		return nil, fmt.Errorf("%q is not list, mute, unmute or leave", arguments.Action)
	}
}
