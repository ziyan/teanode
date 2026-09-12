// Package mailsearch finds messages by words, by meaning where it exists,
// and by what the agent decided about them.
package mailsearch

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/agent/tools/mailbox"
	"github.com/ziyan/teanode/internal/models"

	"github.com/op/go-logging"
)

var log = logging.MustGetLogger("agent")

func init() {
	tools.Register(func() []*tools.Tool {
		return []*tools.Tool{
			{
				Name: "mail_search", Family: tools.FamilyMailbox, Core: true, Risk: tools.RiskRead,
				Permissions: []models.Permission{models.PermissionMailRead},
				Description: "Find messages in the person's mailboxes: by words (or by meaning where it exists), sender, recipient, subject, date, flags, folder, or what the agent decided about them. Rows carry item_id (for mail_read and mail_act) and thread_id.",
				Parameters: tools.Object(map[string]any{
					"query":          tools.StringProperty("words to look for; by meaning when search by meaning exists"),
					"mailbox":        mailbox.MailboxProperty,
					"folder":         tools.StringProperty("a folder name, a kind (inbox, sent, drafts, archive, junk, trash), or all; all by default"),
					"from":           tools.StringProperty("part of the sender"),
					"to":             tools.StringProperty("part of a recipient"),
					"subject":        tools.StringProperty("part of the subject"),
					"since":          tools.StringProperty("an ISO date: on or after"),
					"before":         tools.StringProperty("an ISO date: before"),
					"unread":         tools.BooleanProperty("only unread"),
					"flagged":        tools.BooleanProperty("only starred"),
					"has_attachment": tools.BooleanProperty("only with an attachment"),
					"category":       tools.StringProperty("what the agent sorted it as: personal, work, newsletter, notification, receipt, promotion, social, invitation, other, or one of the person's own"),
					"priority":       tools.EnumProperty("what the agent said", "high", "normal", "low"),
					"needs_reply":    tools.BooleanProperty("only what the agent said needs an answer"),
					"limit":          tools.IntegerProperty("how many rows, 20 by default, 100 at most"),
					"offset":         tools.IntegerProperty("skip this many rows, for the next page"),
				}),
				Guidance: "mail_search rows carry item_id and thread_id; mail_read and mail_act take item_id. Leave folder out unless the person names one: rules file messages into other folders, so a search of the inbox alone misses them.",
				Run:      runMailSearch,
			},
		}
	})
}

type mailSearchArguments struct {
	Query         string `json:"query"`
	Mailbox       string `json:"mailbox"`
	Folder        string `json:"folder"`
	From          string `json:"from"`
	To            string `json:"to"`
	Subject       string `json:"subject"`
	Since         string `json:"since"`
	Before        string `json:"before"`
	Unread        *bool  `json:"unread"`
	Flagged       *bool  `json:"flagged"`
	HasAttachment *bool  `json:"has_attachment"`
	Category      string `json:"category"`
	Priority      string `json:"priority"`
	NeedsReply    *bool  `json:"needs_reply"`
	Limit         int    `json:"limit"`
	Offset        int    `json:"offset"`
}

// threadRow is a thread as ListMailboxThreads returns it, in the parts a
// tool row is made of.
type threadRow struct {
	ThreadID     string   `json:"threadId"`
	Count        int      `json:"count"`
	Unread       int      `json:"unread"`
	Flagged      bool     `json:"flagged"`
	Participants []string `json:"participants"`
	ItemIDs      []string `json:"itemIds"`
	Item         struct {
		ID       string `json:"id"`
		FolderID string `json:"folderId"`
		MailID   string `json:"mailId"`
		Seen     bool   `json:"seen"`
		Flagged  bool   `json:"flagged"`
		Insight  *struct {
			Category   string `json:"category"`
			Priority   string `json:"priority"`
			NeedsReply bool   `json:"needsReply"`
			Summary    string `json:"summary"`
		} `json:"insight"`
		Mail *struct {
			ID         string    `json:"id"`
			From       string    `json:"from"`
			FromName   string    `json:"fromName"`
			Subject    string    `json:"subject"`
			ReceivedAt time.Time `json:"receivedAt"`
		} `json:"mail"`
	} `json:"item"`
}

const documentListThreads = `query ($folderId: String, $mailboxId: String, $unread: Boolean, $flagged: Boolean, $priority: String, $category: String, $needsReply: Boolean, $search: String, $from: String, $to: String, $subject: String, $since: DateTime, $before: DateTime, $hasAttachment: Boolean, $mailIds: [String!], $first: Int, $offset: Int) {
	ListMailboxThreads(folderId: $folderId, mailboxId: $mailboxId, unread: $unread, flagged: $flagged, priority: $priority, category: $category, needsReply: $needsReply, search: $search, from: $from, to: $to, subject: $subject, since: $since, before: $before, hasAttachment: $hasAttachment, mailIds: $mailIds, first: $first, offset: $offset) {
		total
		threads { threadId count unread flagged participants itemIds
			item { id folderId mailId seen flagged insight { category priority needsReply summary } mail { id from fromName subject receivedAt } } }
	}
}`

func runMailSearch(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	arguments, err := tools.DecodeArguments[mailSearchArguments](call)
	if err != nil {
		return nil, err
	}
	operations := run.Operations()
	views, err := mailbox.GrantedMailboxes(ctx, operations)
	if err != nil {
		return nil, err
	}
	if len(views) == 0 {
		return nil, fmt.Errorf("the person has not given you any mailbox")
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
	if limit > 100 {
		limit = 100
	}
	variables := map[string]any{"first": limit, "offset": max(arguments.Offset, 0)}
	if query := strings.TrimSpace(arguments.Query); query != "" {
		variables["search"] = query
	}
	for key, value := range map[string]string{"from": arguments.From, "to": arguments.To, "subject": arguments.Subject, "category": arguments.Category, "priority": arguments.Priority} {
		if strings.TrimSpace(value) != "" {
			variables[key] = strings.TrimSpace(value)
		}
	}
	for key, value := range map[string]*bool{"unread": arguments.Unread, "flagged": arguments.Flagged, "hasAttachment": arguments.HasAttachment, "needsReply": arguments.NeedsReply} {
		if value != nil {
			variables[key] = *value
		}
	}
	location := tools.Location(run.Owner())
	if arguments.Since != "" {
		since, err := tools.ParseTime(arguments.Since, location, time.Now())
		if err != nil {
			return nil, fmt.Errorf("since: %w", err)
		}
		variables["since"] = since.UTC().Format(time.RFC3339)
	}
	if arguments.Before != "" {
		before, err := tools.ParseTime(arguments.Before, location, time.Now())
		if err != nil {
			return nil, fmt.Errorf("before: %w", err)
		}
		variables["before"] = before.UTC().Format(time.RFC3339)
	}

	type row struct {
		ItemID     string `json:"item_id"`
		ThreadID   string `json:"thread_id"`
		Mailbox    string `json:"mailbox"`
		Folder     string `json:"folder"`
		Date       string `json:"date"`
		From       string `json:"from"`
		Subject    string `json:"subject"`
		Messages   int    `json:"messages"`
		Unread     bool   `json:"unread"`
		Flagged    bool   `json:"flagged"`
		Category   string `json:"category,omitempty"`
		Priority   string `json:"priority,omitempty"`
		NeedsReply bool   `json:"needs_reply,omitempty"`
		Summary    string `json:"summary,omitempty"`
		received   time.Time
	}
	rows := []row{}
	total := 0
	for _, view := range targets {
		perMailbox := map[string]any{}
		for key, value := range variables {
			perMailbox[key] = value
		}
		folder := strings.TrimSpace(arguments.Folder)
		if folder == "" || strings.EqualFold(folder, "all") {
			perMailbox["mailboxId"] = view.Mailbox.ID
		} else {
			found, err := mailbox.FindFolder(view, folder)
			if err != nil {
				if len(targets) > 1 {
					continue
				}
				return nil, err
			}
			perMailbox["folderId"] = found.ID
		}
		var result struct {
			ListMailboxThreads struct {
				Total   int         `json:"total"`
				Threads []threadRow `json:"threads"`
			} `json:"ListMailboxThreads"`
		}
		if err := operations.Execute(ctx, documentListThreads, perMailbox, &result); err != nil {
			return nil, err
		}
		total += result.ListMailboxThreads.Total
		threads := result.ListMailboxThreads.Threads
		// By meaning as well, where the mailbox has vectors: what the words
		// missed, after what they hit.
		if query := strings.TrimSpace(arguments.Query); query != "" && tools.SearchMode(run.Configuration(), view.Mailbox.Agent) == "meaning" {
			ids, err := run.MeaningSearch(ctx, view.Mailbox.ID, query, limit)
			if err != nil {
				log.Warningf("search by meaning failed in mailbox %q: %s", view.Mailbox.ID, err)
			} else if len(ids) > 0 {
				seen := map[string]bool{}
				for _, thread := range threads {
					seen[thread.Item.MailID] = true
				}
				byMeaning := map[string]any{"mailboxId": view.Mailbox.ID, "mailIds": ids, "first": limit}
				var more struct {
					ListMailboxThreads struct {
						Threads []threadRow `json:"threads"`
					} `json:"ListMailboxThreads"`
				}
				if err := operations.Execute(ctx, documentListThreads, byMeaning, &more); err != nil {
					return nil, err
				}
				// In the order the ranking gave, not the date.
				position := map[string]int{}
				for index, id := range ids {
					position[id] = index
				}
				found := more.ListMailboxThreads.Threads
				sort.SliceStable(found, func(left, right int) bool {
					return position[found[left].Item.MailID] < position[found[right].Item.MailID]
				})
				for _, thread := range found {
					if !seen[thread.Item.MailID] {
						threads = append(threads, thread)
						total++
					}
				}
			}
		}
		for _, thread := range threads {
			entry := row{ItemID: thread.Item.ID, ThreadID: thread.ThreadID, Mailbox: view.Mailbox.Name, Messages: thread.Count, Unread: thread.Unread > 0, Flagged: thread.Flagged}
			if _, folder := mailbox.FolderById([]*mailbox.MailboxView{view}, thread.Item.FolderID); folder != nil {
				entry.Folder = mailbox.FolderPath(view, folder)
			}
			if mail := thread.Item.Mail; mail != nil {
				entry.Date = mail.ReceivedAt.In(location).Format("2006-01-02 15:04")
				entry.From = mail.From
				if mail.FromName != "" {
					entry.From = mail.FromName + " <" + mail.From + ">"
				}
				entry.Subject = mail.Subject
				entry.received = mail.ReceivedAt
			}
			if len(thread.Participants) > 1 {
				entry.From = strings.Join(thread.Participants, ", ")
			}
			if insight := thread.Item.Insight; insight != nil {
				entry.Category, entry.Priority, entry.NeedsReply, entry.Summary = insight.Category, insight.Priority, insight.NeedsReply, insight.Summary
			}
			rows = append(rows, entry)
		}
	}
	if strings.TrimSpace(arguments.Query) == "" {
		sort.SliceStable(rows, func(left, right int) bool { return rows[left].received.After(rows[right].received) })
	}
	if len(rows) > limit {
		rows = rows[:limit]
	}
	next := 0
	if total > arguments.Offset+len(rows) && len(rows) == limit {
		next = arguments.Offset + limit
	}
	result, err := tools.JSONResult(map[string]any{"rows": rows, "total": total, "next_offset": next})
	if err != nil {
		return nil, err
	}
	result.Untrusted = true
	result.Note = fmt.Sprintf("found %d conversation(s)", total)
	return result, nil
}
