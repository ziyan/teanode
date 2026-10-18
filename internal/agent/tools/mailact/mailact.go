// Package mailact acts on messages: marks, stars, files, junks, trashes.
package mailact

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

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
				// The answer is mailed to the organizer and written to the
				// person's calendar, which the API checks as mail:send and
				// calendar:use. Without both the action is not offered.
				ActionRequires: map[string][]models.Permission{"answer_invitation": answerInvitationPermissions},
				Description:    "Act on messages: mark_read, mark_unread, star, unstar, archive, move (to a folder), junk, not_junk, trash, delete_forever, answer_invitation (say accepted, tentative or declined to the calendar invitation one message carried; it writes to the organizer, so it always asks the person first). Give item_ids, or thread_item_id for every message of a conversation. A message moved to another folder gets a new item_id, given back in the result. A draft sent to trash is discarded for good, as the Discard button does; a message it was a reply to stays where it is.",
				Parameters: tools.Object(map[string]any{
					"action":         tools.EnumProperty("what to do", "mark_read", "mark_unread", "star", "unstar", "archive", "move", "junk", "not_junk", "trash", "delete_forever", "answer_invitation"),
					"item_ids":       tools.ArrayProperty("the messages", tools.StringProperty("item id")),
					"thread_item_id": tools.StringProperty("any message of the conversation, to act on all of it"),
					"folder":         tools.StringProperty("for move: the folder, by name"),
					"answer":         tools.EnumProperty("for answer_invitation: whether the person is coming", "accepted", "tentative", "declined"),
				}, "action"),
				// What is about to happen to the mail, in the words the
				// reader uses for it. "delete_forever 3 message(s)" is the
				// call, and the one that cannot be undone is exactly the
				// one somebody has to be able to read at a glance.
				Preview: func(arguments json.RawMessage) string {
					var call mailActArguments
					_ = json.Unmarshal(arguments, &call)
					said := map[string]string{
						"mark_read": "Mark %s read", "mark_unread": "Mark %s unread",
						"star": "Star %s", "unstar": "Take the star off %s",
						"archive": "Archive %s", "move": "Move %s",
						"junk": "Move %s to Junk", "not_junk": "Take %s out of Junk",
						"trash": "Move %s to Trash", "delete_forever": "Delete %s for good, which cannot be undone",
						"answer_invitation": "Answer the invitation in %s",
					}[call.Action]
					if said == "" {
						said = "Change %s"
					}
					what := "a whole conversation"
					if call.ThreadItemID == "" {
						switch len(call.ItemIDs) {
						case 0:
							what = "some mail"
						case 1:
							what = "a message"
						default:
							what = fmt.Sprintf("%d messages", len(call.ItemIDs))
						}
					}
					if call.Action == "move" && strings.TrimSpace(call.Folder) != "" {
						return fmt.Sprintf(said, what) + " to " + call.Folder
					}
					if call.Action == "answer_invitation" && call.Answer != "" {
						return fmt.Sprintf(said, what) + ": " + call.Answer
					}
					return fmt.Sprintf(said, what)
				},
				PreviewIn: previewAnswerInvitation,
				RiskOf: func(arguments json.RawMessage) tools.Risk {
					var call mailActArguments
					_ = json.Unmarshal(arguments, &call)
					if call.Action == "delete_forever" {
						return tools.RiskDestructive
					}
					// The answer is mailed to the organizer in the person's
					// name, so it is held to the standard of a send.
					if call.Action == "answer_invitation" {
						return tools.RiskOutward
					}
					return tools.RiskWrite
				},
				Run: runMailAct,
			},
		}
	})
}

// answerInvitationPermissions are what answering needs beyond mail:write.
var answerInvitationPermissions = []models.Permission{models.PermissionMailSend, models.PermissionCalendarUse}

// previewAnswerInvitation names the event and who will be told, which the
// static line cannot: the call carries only a message id and an answer. It
// says nothing for any other action, so the plain line is used.
func previewAnswerInvitation(ctx context.Context, arguments json.RawMessage) string {
	var call mailActArguments
	if err := json.Unmarshal(arguments, &call); err != nil || call.Action != "answer_invitation" || len(call.ItemIDs) != 1 {
		return ""
	}
	run, err := tools.RunFrom(ctx)
	if err != nil || run.Operations() == nil {
		return ""
	}
	invitation, err := mailbox.GetInvitation(ctx, run.Operations(), call.ItemIDs[0])
	if err != nil || invitation == nil {
		return ""
	}
	event := "an invitation with no title"
	if summary := strings.TrimSpace(invitation.Summary); summary != "" {
		event = strconv.Quote(summary)
	}
	if invitation.StartsAt != "" {
		event += " at " + invitation.StartsAt
	}
	line := "Answer " + event + ": " + call.Answer
	if invitation.Organizer != "" {
		return line + ", mailed to " + invitation.Organizer
	}
	return line + " (it names no organizer, so nobody is mailed)"
}

// answerRequestId names one answer for the server, which answers a repeated
// id once. It comes from the call, the message and the answer, so a retried
// confirmation of the same call reuses it and mails the organizer once. The
// message and answer are in it because a provider may reuse call ids: a
// repeat for another message or answer is another answer.
func answerRequestId(callId, itemId, answer string) string {
	encoded, _ := json.Marshal([]string{"mail_act", "answer_invitation", callId, itemId, answer})
	return fmt.Sprintf("answer-%x", sha256.Sum256(encoded))
}

type mailActArguments struct {
	Action       string   `json:"action"`
	ItemIDs      []string `json:"item_ids"`
	ThreadItemID string   `json:"thread_item_id"`
	Folder       string   `json:"folder"`
	Answer       string   `json:"answer"`
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
	if arguments.Action == "answer_invitation" {
		return runAnswerInvitation(ctx, call, operations, arguments)
	}
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
		// A draft has no life in Trash: the dashboard's Discard button
		// deletes it outright, and a draft the agent trashes goes the
		// same way rather than turning up as a message to restore.
		var drafts []string
		for _, entry := range thread.Items {
			if entry.Item.Draft && slices.Contains(itemIds, entry.Item.ID) {
				drafts = append(drafts, entry.Item.ID)
			}
		}
		itemIds = slices.DeleteFunc(itemIds, func(itemId string) bool { return slices.Contains(drafts, itemId) })
		if len(drafts) > 0 {
			err = execute(`mutation ($itemIds: [String!]!) { DeleteMailboxItems(itemIds: $itemIds) }`, map[string]any{"itemIds": drafts})
			where = "gone"
		}
		if err == nil && len(itemIds) > 0 {
			folder := mailbox.FolderOfKind(view, models.MailboxFolderKindTrash)
			err = move(folder)
			where = "Trash"
		}
		itemIds = append(itemIds, drafts...)
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

// runAnswerInvitation answers the invitation one message carried. It takes a
// single message because an answer is about one invitation, and it goes
// through AnswerMailInvitation so the refusals are the dashboard's: a message
// that carried none, one that is only a reply or a cancellation, and a person
// without the right to send.
func runAnswerInvitation(ctx context.Context, call *tools.Call, operations tools.Operations, arguments mailActArguments) (*tools.Result, error) {
	if len(arguments.ItemIDs) != 1 || arguments.ThreadItemID != "" {
		return nil, fmt.Errorf("answer_invitation takes exactly one message in item_ids")
	}
	answer := strings.ToUpper(strings.TrimSpace(arguments.Answer))
	if !slices.Contains([]string{"ACCEPTED", "TENTATIVE", "DECLINED"}, answer) {
		return nil, fmt.Errorf("answer must be accepted, tentative or declined")
	}
	if operations.Permissions() != nil && !tools.HoldsAll(operations.Permissions(), answerInvitationPermissions) {
		return nil, fmt.Errorf("answering an invitation needs the right to send mail and to use the calendar")
	}
	if !call.Confirmed {
		return nil, fmt.Errorf("answering an invitation writes to its organizer and needs the person's confirmation")
	}
	// Only a mailbox the person granted: the item id names any of theirs.
	views, err := mailbox.GrantedMailboxes(ctx, operations)
	if err != nil {
		return nil, err
	}
	if _, _, err := mailbox.MailboxOfItem(ctx, operations, views, arguments.ItemIDs[0]); err != nil {
		return nil, err
	}
	invitation, err := mailbox.AnswerInvitation(ctx, operations, answerRequestId(call.ID, arguments.ItemIDs[0], answer), arguments.ItemIDs[0], answer)
	if err != nil {
		return nil, err
	}
	result, err := tools.JSONResult(map[string]any{"action": arguments.Action, "answer": strings.ToLower(answer), "invitation": invitation})
	if err != nil {
		return nil, err
	}
	result.Untrusted = true
	result.Note = "answered the invitation: " + strings.ToLower(answer)
	return result, nil
}

// --- drafts and sending -------------------------------------------------
