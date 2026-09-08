package apigraph

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

type AuditQuery interface {
	// List the audit log, newest first: every administrative change, who
	// made it, and the row before and after. Needs audit:read.
	ListAuditEvents(ctx context.Context, arguments ListAuditEventsArguments) (*AuditEventPage, error)
}

type ListAuditEventsArguments struct {
	// Only changes to this kind of thing: user, group, role, domain, ...
	ResourceType *string `json:"resourceType"`

	// Only changes to this row
	ResourceID *string `json:"resourceId"`

	// Only changes made by this user
	ActorUserID *string `json:"actorUserId"`

	// Only changes at or after this time
	Since *time.Time `json:"since"`

	// Only changes before this time
	Until *time.Time `json:"until"`

	// How many to return, at most 200; and how many to skip
	First  *int `json:"first"`
	Offset *int `json:"offset"`
}

// AuditEventPage is one page of the log, with how many there are in all.
type AuditEventPage struct {
	Events []*models.AuditEvent `json:"events"`
	Total  int64                `json:"total"`
}

// describeAuditResources names the thing each event is about, and says where
// it is.
//
// An audit row stores an id, because that is what does not change. An id is
// also the one thing nobody can read: a page of "ziyan removed alias
// 01m1z7m3ktjds2sajygpwwhn71" says who and what happened and nothing about
// which alias. The name is resolved here rather than stored, so a renamed
// group reads by the name it has now.
//
// Everything is read in a handful of queries rather than one per row: a
// hundred events over twenty aliases is twenty lookups if it is done per row
// and one if it is done per kind.
func (self *graph) describeAuditResources(tx db.Transaction, events []*models.AuditEvent) error {
	wanted := map[models.AuditResourceType]map[string]bool{}
	for _, event := range events {
		if event.ResourceID == "" {
			continue
		}
		if wanted[event.ResourceType] == nil {
			wanted[event.ResourceType] = map[string]bool{}
		}
		wanted[event.ResourceType][event.ResourceID] = true
	}
	if len(wanted) == 0 {
		return nil
	}

	type described struct {
		label string
		link  string
	}
	found := map[string]described{}
	key := func(resourceType models.AuditResourceType, id string) string {
		return string(resourceType) + "/" + id
	}

	if wanted[models.AuditResourceUser] != nil {
		users, err := tx.ListUsers()
		if err != nil {
			return err
		}
		for _, user := range users {
			found[key(models.AuditResourceUser, user.ID)] = described{label: user.Username, link: "/access/people"}
		}
	}
	if wanted[models.AuditResourceGroup] != nil {
		groups, err := tx.ListGroups()
		if err != nil {
			return err
		}
		for _, group := range groups {
			found[key(models.AuditResourceGroup, group.ID)] = described{label: group.Name, link: "/access/people"}
		}
	}
	if wanted[models.AuditResourceRole] != nil {
		roles, err := tx.ListRoles()
		if err != nil {
			return err
		}
		for _, role := range roles {
			found[key(models.AuditResourceRole, role.ID)] = described{label: role.Name, link: "/access/roles"}
		}
	}

	// Domains carry their own aliases and credentials, so one listing names
	// all three kinds. An address of a mailbox is an alias too, recorded
	// under its own type, and reads by the same name.
	if wanted[models.AuditResourceDomain] != nil || wanted[models.AuditResourceAlias] != nil ||
		wanted[models.AuditResourceCredential] != nil || wanted[models.AuditResourceMailboxAddress] != nil {
		domains, err := tx.ListDomains()
		if err != nil {
			return err
		}
		for _, domain := range domains {
			found[key(models.AuditResourceDomain, domain.ID)] = described{
				label: domain.Domain,
				link:  "/domains/" + domain.ID,
			}
			for _, entry := range domain.Aliases {
				if entry == nil {
					continue
				}
				// An alias is known by what it matches, and a catch-all
				// matches everything, which is worth saying rather than
				// showing as an empty string.
				id := entry.ID
				pattern := entry.Pattern
				if pattern == "" {
					pattern = "(catch-all)"
				}
				alias := described{
					label: pattern + "@" + domain.Domain,
					link:  "/domains/" + domain.ID + "/aliases",
				}
				found[key(models.AuditResourceAlias, id)] = alias
				found[key(models.AuditResourceMailboxAddress, id)] = alias
			}
			for _, credential := range domain.Credentials {
				if credential == nil {
					continue
				}
				label := credential.Comment
				if label == "" {
					label = domain.Domain
				}
				found[key(models.AuditResourceCredential, credential.ID)] = described{
					label: label,
					link:  "/domains/" + domain.ID + "/credentials",
				}
			}
		}
	}

	// A mailbox has no page of its own that another person may open, so these
	// are named and not linked. The name is still the whole point: "created
	// mailbox_app_password Thunderbird" is a sentence, and the same line with
	// an identifier in it is not.
	if wanted[models.AuditResourceMailbox] != nil || wanted[models.AuditResourceMailboxAppPassword] != nil {
		mailboxes, err := tx.ListMailboxes("")
		if err != nil {
			return err
		}
		for _, mailbox := range mailboxes {
			found[key(models.AuditResourceMailbox, mailbox.ID)] = described{label: mailbox.Name}
			if wanted[models.AuditResourceMailboxAppPassword] == nil {
				continue
			}
			// One query per mailbox rather than per event: app passwords
			// hang off a mailbox and there is no listing across all of them.
			appPasswords, err := tx.ListAppPasswords(mailbox.ID)
			if err != nil {
				return err
			}
			for _, appPassword := range appPasswords {
				label := appPassword.Name
				if label == "" {
					label = mailbox.Name
				}
				found[key(models.AuditResourceMailboxAppPassword, appPassword.ID)] = described{label: label}
			}
		}
	}

	for _, event := range events {
		if event.ResourceID == "" {
			continue
		}
		if entry, ok := found[key(event.ResourceType, event.ResourceID)]; ok {
			event.ResourceLabel = entry.label
			event.ResourceLink = entry.link
			continue
		}
		// Nothing to look up: the thing is gone, which is exactly the row
		// that most needs a name. "console removed alias 01m1z675zz7kpe" is
		// the line that sends somebody digging through the database. The row
		// carries the answer — what was deleted is in its own snapshot — so
		// read the name out of that instead. No link: there is nowhere to go.
		event.ResourceLabel = labelFromSnapshot(event.ResourceType, event.Before, event.After)
	}
	return nil
}

// labelFromSnapshot reads a name out of the row an event recorded, for a
// thing that can no longer be looked up.
func labelFromSnapshot(resourceType models.AuditResourceType, sides ...json.RawMessage) string {
	for _, side := range sides {
		if len(side) == 0 {
			continue
		}
		fields := map[string]any{}
		if err := json.Unmarshal(side, &fields); err != nil {
			continue
		}
		// An alias is known by what it matches, and an empty pattern is a
		// catch-all rather than a missing name.
		if resourceType == models.AuditResourceAlias || resourceType == models.AuditResourceMailboxAddress {
			if pattern, ok := fields["pattern"].(string); ok {
				if pattern == "" {
					return "(catch-all)"
				}
				return pattern
			}
		}
		// In the order a person would name the thing themselves.
		for _, name := range []string{"name", "username", "address", "domain", "email", "comment"} {
			if value, ok := fields[name].(string); ok && value != "" {
				return value
			}
		}
	}
	return ""
}

func (self *graph) ListAuditEvents(ctx context.Context, arguments ListAuditEventsArguments) (*AuditEventPage, error) {
	if _, err := self.requirePermission(ctx, models.PermissionAuditRead); err != nil {
		return nil, err
	}
	options := &db.AuditOptions{Limit: 50}
	if arguments.ResourceType != nil {
		options.ResourceType = *arguments.ResourceType
	}
	if arguments.ResourceID != nil {
		options.ResourceID = *arguments.ResourceID
	}
	if arguments.ActorUserID != nil {
		options.ActorUserID = *arguments.ActorUserID
	}
	options.Since, options.Until = arguments.Since, arguments.Until
	if arguments.First != nil && *arguments.First > 0 {
		options.Limit = min(*arguments.First, 200)
	}
	if arguments.Offset != nil && *arguments.Offset > 0 {
		options.Offset = *arguments.Offset
	}
	tx := self.transaction(ctx)
	events, err := tx.ListAuditEvents(options)
	if err != nil {
		return nil, err
	}
	total, err := tx.CountAuditEvents(options)
	if err != nil {
		return nil, err
	}
	if err := self.describeAuditResources(tx, events); err != nil {
		return nil, err
	}

	// The actor's current name, read once per distinct actor: a renamed
	// user reads by the name they have now.
	labels := map[string]string{}
	for _, event := range events {
		switch event.ActorKind {
		case models.AuditActorUser:
			if event.ActorUserID == "" {
				event.ActorLabel = "console"
				continue
			}
			label, ok := labels[event.ActorUserID]
			if !ok {
				label = "a deleted user"
				if user, err := tx.GetUser(event.ActorUserID); err == nil && user != nil {
					label = user.Username
				}
				labels[event.ActorUserID] = label
			}
			event.ActorLabel = label
		default:
			event.ActorLabel = string(event.ActorKind)
		}
	}
	return &AuditEventPage{Events: events, Total: total}, nil
}
