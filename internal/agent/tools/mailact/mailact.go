// Package mailact acts on messages: marks, stars, files, junks, trashes.
package mailact

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/agent/tools/mailbox"
	"github.com/ziyan/teanode/internal/models"
)

func init() {
	tools.Register(func() []*tools.Tool {
		return []*tools.Tool{
			{
				Name: "mail_act", Family: tools.FamilyMailbox, Core: true, Risk: tools.RiskWrite,
				Permissions: []models.Permission{models.PermissionMailWrite},
				Description: "Act on messages: mark_read, mark_unread, star, unstar, archive, move (to a folder), junk, not_junk, trash, delete_forever. Give item_ids, or thread_item_id for every message of a conversation. A message moved to another folder gets a new item_id, given back in the result.",
				Parameters: tools.Object(map[string]any{
					"action":         tools.EnumProperty("what to do", "mark_read", "mark_unread", "star", "unstar", "archive", "move", "junk", "not_junk", "trash", "delete_forever"),
					"item_ids":       tools.ArrayProperty("the messages", tools.StringProperty("item id")),
					"thread_item_id": tools.StringProperty("any message of the conversation, to act on all of it"),
					"folder":         tools.StringProperty("for move: the folder, by name"),
				}, "action"),
				Preview: func(arguments json.RawMessage) string {
					var call mailActArguments
					_ = json.Unmarshal(arguments, &call)
					if call.ThreadItemID != "" {
						return fmt.Sprintf("%s a whole conversation (via %s)", call.Action, call.ThreadItemID)
					}
					return fmt.Sprintf("%s %d message(s)", call.Action, len(call.ItemIDs))
				},
				RiskOf: func(arguments json.RawMessage) tools.Risk {
					var call mailActArguments
					_ = json.Unmarshal(arguments, &call)
					if call.Action == "delete_forever" {
						return tools.RiskDestructive
					}
					return tools.RiskWrite
				},
				Run: runMailAct,
			},
		}
	})
}

type mailActArguments struct {
	Action       string   `json:"action"`
	ItemIDs      []string `json:"item_ids"`
	ThreadItemID string   `json:"thread_item_id"`
	Folder       string   `json:"folder"`
}

func runMailAct(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	arguments, err := tools.DecodeArguments[mailActArguments](call)
	if err != nil {
		return nil, err
	}
	operations := run.Operations()
	itemIds := append([]string{}, arguments.ItemIDs...)
	var thread *mailbox.ThreadView
	if arguments.ThreadItemID != "" {
		if thread, err = mailbox.GetThread(ctx, operations, arguments.ThreadItemID); err != nil {
			return nil, err
		}
		for _, entry := range thread.Items {
			if !entry.Item.Draft {
				itemIds = append(itemIds, entry.Item.ID)
			}
		}
	}
	if len(itemIds) == 0 {
		return nil, fmt.Errorf("nothing to act on: give item_ids or thread_item_id")
	}
	if arguments.Action == "delete_forever" && !call.Confirmed {
		return nil, fmt.Errorf("delete_forever needs the person's confirmation")
	}
	// The mailbox the items are in, for the folders an action names.
	views, err := mailbox.GrantedMailboxes(ctx, operations)
	if err != nil {
		return nil, err
	}
	var view *mailbox.MailboxView
	if thread == nil {
		if thread, err = mailbox.GetThread(ctx, operations, itemIds[0]); err != nil {
			return nil, err
		}
	}
	for _, entry := range thread.Items {
		if candidate, _ := mailbox.FolderById(views, entry.FolderID); candidate != nil {
			view = candidate
			break
		}
	}
	if view == nil {
		return nil, fmt.Errorf("the message is not in a mailbox the agent may reach")
	}
	yes, no := true, false
	execute := func(document string, variables map[string]any) error {
		var discard map[string]any
		return operations.Execute(ctx, document, variables, &discard)
	}
	// A message moved to another folder is a new item there, with a new
	// id; the result hands the new ids back so the model does not keep
	// the stale ones.
	var movedIds []string
	move := func(folder *models.MailboxFolder) error {
		if folder == nil {
			return fmt.Errorf("the mailbox has no such folder")
		}
		var moved struct {
			MoveMailboxItems []struct {
				ID string `json:"id"`
			} `json:"MoveMailboxItems"`
		}
		if err := operations.Execute(ctx, `mutation ($itemIds: [String!]!, $folderId: String!) { MoveMailboxItems(itemIds: $itemIds, folderId: $folderId) { id } }`, map[string]any{"itemIds": itemIds, "folderId": folder.ID}, &moved); err != nil {
			return err
		}
		for _, item := range moved.MoveMailboxItems {
			movedIds = append(movedIds, item.ID)
		}
		return nil
	}
	flags := func(seen, flagged *bool) error {
		variables := map[string]any{"itemIds": itemIds}
		if seen != nil {
			variables["seen"] = *seen
		}
		if flagged != nil {
			variables["flagged"] = *flagged
		}
		return execute(`mutation ($itemIds: [String!]!, $seen: Boolean, $flagged: Boolean) { SetMailboxItemFlags(itemIds: $itemIds, seen: $seen, flagged: $flagged) }`, variables)
	}
	where := ""
	switch arguments.Action {
	case "mark_read":
		err = flags(&yes, nil)
	case "mark_unread":
		err = flags(&no, nil)
	case "star":
		err = flags(nil, &yes)
	case "unstar":
		err = flags(nil, &no)
	case "archive":
		folder := mailbox.FolderOfKind(view, models.MailboxFolderKindArchive)
		err = move(folder)
		where = "Archive"
	case "trash":
		folder := mailbox.FolderOfKind(view, models.MailboxFolderKindTrash)
		err = move(folder)
		where = "Trash"
	case "move":
		folder, findErr := mailbox.FindFolder(view, arguments.Folder)
		if findErr != nil {
			return nil, findErr
		}
		err = move(folder)
		where = mailbox.FolderPath(view, folder)
	case "junk":
		err = execute(`mutation ($itemIds: [String!]!) { ReportMailboxJunk(itemIds: $itemIds) }`, map[string]any{"itemIds": itemIds})
		where = "Junk"
	case "not_junk":
		err = execute(`mutation ($itemIds: [String!]!) { ReportMailboxJunk(itemIds: $itemIds, notJunk: true) }`, map[string]any{"itemIds": itemIds})
		where = "Inbox"
	case "delete_forever":
		err = execute(`mutation ($itemIds: [String!]!) { DeleteMailboxItems(itemIds: $itemIds) }`, map[string]any{"itemIds": itemIds})
		where = "gone"
	default:
		return nil, fmt.Errorf("%q is not an action of mail_act", arguments.Action)
	}
	if err != nil {
		return nil, err
	}
	answer := map[string]any{"changed": len(itemIds), "action": arguments.Action, "now_in": where}
	if len(movedIds) > 0 {
		answer["item_ids"] = movedIds
		answer["note"] = "moved messages have these new item_ids; the old ones are gone"
	}
	result, err := tools.JSONResult(answer)
	if err != nil {
		return nil, err
	}
	result.Note = fmt.Sprintf("%s: %d message(s)", arguments.Action, len(itemIds))
	return result, nil
}

// --- drafts and sending -------------------------------------------------
