// Package folder lists and manages a mailbox's folders.
package folder

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
				Name: "folder_list", Family: tools.FamilyMailbox, Risk: tools.RiskRead,
				Permissions: []models.Permission{models.PermissionMailRead},
				Description: "The folders of a mailbox, with unread and total counts.",
				Parameters:  tools.Object(map[string]any{"mailbox": mailbox.MailboxProperty}),
				Run:         runFolderList,
			},
			{
				Name: "folder_manage", Family: tools.FamilyMailbox, Risk: tools.RiskWrite,
				Permissions: []models.Permission{models.PermissionMailboxManage},
				Description: "Make, rename, move, pin, unpin or delete a folder. Deleting a folder deletes what is in it, so it asks the person first.",
				Parameters: tools.Object(map[string]any{
					"action":  tools.EnumProperty("what to do", "create", "rename", "move", "pin", "unpin", "delete"),
					"mailbox": mailbox.MailboxProperty,
					"folder":  tools.StringProperty("the folder, by name; for create, the new name"),
					"name":    tools.StringProperty("for rename: the new name"),
					"parent":  tools.StringProperty("for create and move: the parent folder, or empty for the top"),
				}, "action", "folder"),
				Preview: func(arguments json.RawMessage) string {
					var call folderManageArguments
					_ = json.Unmarshal(arguments, &call)
					return fmt.Sprintf("%s the folder %q", call.Action, call.Folder)
				},
				RiskOf: func(arguments json.RawMessage) tools.Risk {
					var call folderManageArguments
					_ = json.Unmarshal(arguments, &call)
					if call.Action == "delete" {
						return tools.RiskDestructive
					}
					return tools.RiskWrite
				},
				Run: runFolderManage,
			},
		}
	})
}

func runFolderList(ctx context.Context, call *tools.Call) (*tools.Result, error) {
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
	targets := views
	if arguments.Mailbox != "" {
		view, err := mailbox.FindMailbox(views, arguments.Mailbox)
		if err != nil {
			return nil, err
		}
		targets = []*mailbox.MailboxView{view}
	}
	type folderRow struct {
		Mailbox string `json:"mailbox"`
		Folder  string `json:"folder"`
		Kind    string `json:"kind,omitempty"`
		Unread  int64  `json:"unread"`
		Total   int64  `json:"total"`
		Pinned  bool   `json:"pinned,omitempty"`
	}
	var rows []folderRow
	for _, view := range targets {
		for _, folder := range view.Folders {
			rows = append(rows, folderRow{Mailbox: view.Mailbox.Name, Folder: mailbox.FolderPath(view, folder), Kind: string(folder.Kind), Unread: folder.Unread, Total: folder.Total, Pinned: folder.PinnedAt != nil})
		}
	}
	return tools.JSONResult(map[string]any{"folders": rows})
}

type folderManageArguments struct {
	Action  string `json:"action"`
	Mailbox string `json:"mailbox"`
	Folder  string `json:"folder"`
	Name    string `json:"name"`
	Parent  string `json:"parent"`
}

func runFolderManage(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	arguments, err := tools.DecodeArguments[folderManageArguments](call)
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
	execute := func(document string, variables map[string]any) error {
		var discard map[string]any
		return operations.Execute(ctx, document, variables, &discard)
	}
	parentId := func() (any, error) {
		if strings.TrimSpace(arguments.Parent) == "" {
			return nil, nil
		}
		parent, err := mailbox.FindFolder(view, arguments.Parent)
		if err != nil {
			return nil, err
		}
		return parent.ID, nil
	}
	switch arguments.Action {
	case "create":
		parent, err := parentId()
		if err != nil {
			return nil, err
		}
		variables := map[string]any{"mailboxId": view.Mailbox.ID, "name": strings.TrimSpace(arguments.Folder)}
		if parent != nil {
			variables["parentId"] = parent
		}
		if err := execute(`mutation ($mailboxId: String!, $name: String!, $parentId: String) { CreateMailboxFolder(mailboxId: $mailboxId, name: $name, parentId: $parentId) { id } }`, variables); err != nil {
			return nil, err
		}
		return tools.TextResult("made the folder %q in %q", arguments.Folder, view.Mailbox.Name), nil
	case "rename", "move":
		folder, err := mailbox.FindFolder(view, arguments.Folder)
		if err != nil {
			return nil, err
		}
		variables := map[string]any{"folderId": folder.ID}
		if arguments.Action == "rename" {
			if strings.TrimSpace(arguments.Name) == "" {
				return nil, fmt.Errorf("rename needs name")
			}
			variables["name"] = strings.TrimSpace(arguments.Name)
		} else {
			parent, err := parentId()
			if err != nil {
				return nil, err
			}
			variables["parentId"] = parent
		}
		if err := execute(`mutation ($folderId: String!, $name: String, $parentId: String) { UpdateMailboxFolder(folderId: $folderId, name: $name, parentId: $parentId) { id } }`, variables); err != nil {
			return nil, err
		}
		return tools.TextResult("%s done for %q", arguments.Action, arguments.Folder), nil
	case "pin", "unpin":
		folder, err := mailbox.FindFolder(view, arguments.Folder)
		if err != nil {
			return nil, err
		}
		if err := execute(`mutation ($folderId: String!, $pinned: Boolean!) { SetMailboxFolderPinned(folderId: $folderId, pinned: $pinned) { id } }`, map[string]any{"folderId": folder.ID, "pinned": arguments.Action == "pin"}); err != nil {
			return nil, err
		}
		return tools.TextResult("%s done for %q", arguments.Action, arguments.Folder), nil
	case "delete":
		if !call.Confirmed {
			return nil, fmt.Errorf("deleting a folder needs the person's confirmation")
		}
		folder, err := mailbox.FindFolder(view, arguments.Folder)
		if err != nil {
			return nil, err
		}
		if err := execute(`mutation ($folderId: String!) { DeleteMailboxFolder(folderId: $folderId) }`, map[string]any{"folderId": folder.ID}); err != nil {
			return nil, err
		}
		return tools.TextResult("deleted the folder %q and what was in it", arguments.Folder), nil
	}
	return nil, fmt.Errorf("%q is not an action of folder_manage", arguments.Action)
}

// --- rules --------------------------------------------------------------
