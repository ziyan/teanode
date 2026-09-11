// Package contact finds the people the person writes to.
package contact

import (
	"context"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/agent/tools/mailbox"
	"github.com/ziyan/teanode/internal/models"
)

func init() {
	tools.Register(func() []*tools.Tool {
		return []*tools.Tool{
			{
				Name: "contact_search", Family: tools.FamilyMailbox, Risk: tools.RiskRead,
				Permissions: []models.Permission{models.PermissionMailRead},
				Description: "People who have written to the mailbox, by the start of their address or name.",
				Parameters: tools.Object(map[string]any{
					"mailbox": mailbox.MailboxProperty,
					"prefix":  tools.StringProperty("the start of an address or a name"),
					"limit":   tools.IntegerProperty("how many, 20 by default"),
				}),
				Run: runContactSearch,
			},
		}
	})
}

type contactSearchArguments struct {
	Mailbox string `json:"mailbox"`
	Prefix  string `json:"prefix"`
	Limit   int    `json:"limit"`
}

func runContactSearch(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	arguments, err := tools.DecodeArguments[contactSearchArguments](call)
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
	limit := arguments.Limit
	if limit <= 0 {
		limit = 20
	}
	var rows []map[string]any
	for _, view := range targets {
		var result struct {
			ListMailboxContacts []struct {
				Address    string     `json:"address"`
				Name       string     `json:"name"`
				LastSeenAt *time.Time `json:"lastSeenAt"`
				Count      int        `json:"count"`
			} `json:"ListMailboxContacts"`
		}
		if err := operations.Execute(ctx, `query ($mailboxId: String!, $prefix: String, $first: Int) { ListMailboxContacts(mailboxId: $mailboxId, prefix: $prefix, first: $first) { address name lastSeenAt count } }`, map[string]any{"mailboxId": view.Mailbox.ID, "prefix": arguments.Prefix, "first": limit}, &result); err != nil {
			return nil, err
		}
		for _, contact := range result.ListMailboxContacts {
			row := map[string]any{"mailbox": view.Mailbox.Name, "address": contact.Address, "name": contact.Name, "messages": contact.Count}
			if contact.LastSeenAt != nil {
				row["last_seen"] = contact.LastSeenAt.Format("2006-01-02")
			}
			rows = append(rows, row)
		}
	}
	result, err := tools.JSONResult(map[string]any{"contacts": rows})
	if err != nil {
		return nil, err
	}
	result.Untrusted = true
	return result, nil
}

// --- the reply queue -----------------------------------------------------
