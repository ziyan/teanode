package conversation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/agent/tools/operator"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// What a person does with their chats, as the dashboard, the command line and
// the iPhone app let them, and so what the agent does for them: each action
// here runs the same GraphQL operation as the person, so the server holds it
// to the same rules (only an admin changes who is in a group chat, only the
// author edits a message) and refuses in the same words. Nothing is checked
// twice here; what is looked up before a call is only a name, to turn the
// words of the person ("take Sam out") into the identifiers the operation
// takes, and to say who and what on the card.
//
// The actions are named as the command line names them
// (teanode agent conversation direct, group, add, remove, leave, mute, read,
// remember, others-may-ask, edit, delete-message and rename), except that
// "add" and "remove" say what they take because this tool also has other
// things to add to, and "read" is mark_read because read already reads.

// chatActions are the actions that change a chat, in the order the tool
// lists them.
var chatActions = []string{
	"direct", "group", "rename", "add_members", "remove_member", "leave",
	"mute", "mark_read", "edit", "delete_message", "remember", "others_may_ask",
}

// isChatAction says whether an action changes a chat.
func isChatAction(action string) bool {
	for _, name := range chatActions {
		if name == action {
			return true
		}
	}
	return false
}

// chatRiskOf is what a call to a chat action costs.
//
// Granting is for what gives somebody a way in: adding people, who then read
// everything said in the chat from then on, and letting the others in a chat
// address the person's agent, which answers them in front of everybody and is
// counted as the person's. Turning that last one off only takes a way in
// back, so it is an ordinary write; a call that does not say which way is
// judged as the one that gives.
//
// Destructive is for what the person cannot do again alone: taking somebody
// out, leaving (an admin has to add the person back) and deleting a message
// for everybody. The command line asks first for exactly these.
//
// A group chat is a write although it puts people in a chat with the person:
// they each share a circle with the person already, and could be written to
// in a direct chat, and nothing is said to them until the person says
// something, which is outward on its own. The command line and the dashboard
// start one without asking, as they do a direct chat.
//
// Editing a message is outward, as sending one is: everybody in the chat
// reads the new words as the person's, and what it said before is not kept.
// Everything else changes the person's own view of a chat, which they can
// change back.
func chatRiskOf(asked conversationArguments) tools.Risk {
	switch strings.TrimSpace(asked.Action) {
	case "add_members":
		return tools.RiskGranting
	case "remove_member", "leave", "delete_message":
		return tools.RiskDestructive
	case "edit":
		return tools.RiskOutward
	case "others_may_ask":
		if asked.IsOn != nil && !*asked.IsOn {
			return tools.RiskWrite
		}
		return tools.RiskGranting
	case "direct", "group", "rename", "mute", "mark_read", "remember":
		return tools.RiskWrite
	}
	return tools.RiskRead
}

// The documents are the command line's (internal/client/agent_chat.go), with
// the parts of the answer a model reads.
const (
	chatAnswerFields = `{ id title chat { chatId chatKind isMuted mutedUntil members { userId username name memberRole isRemoved } } }`

	documentOpenDirectChat      = `mutation ($userId: String!) { OpenDirectChat(userId: $userId) ` + chatAnswerFields + ` }`
	documentCreateGroupChat     = `mutation ($title: String, $userIds: [String!]!) { CreateGroupChat(title: $title, userIds: $userIds) ` + chatAnswerFields + ` }`
	documentRenameChat          = `mutation ($conversationId: String!, $title: String!) { RenameChat(conversationId: $conversationId, title: $title) ` + chatAnswerFields + ` }`
	documentAddChatMembers      = `mutation ($conversationId: String!, $userIds: [String!]!) { AddChatMembers(conversationId: $conversationId, userIds: $userIds) ` + chatAnswerFields + ` }`
	documentRemoveChatMember    = `mutation ($conversationId: String!, $userId: String!) { RemoveChatMember(conversationId: $conversationId, userId: $userId) ` + chatAnswerFields + ` }`
	documentLeaveChat           = `mutation ($conversationId: String!) { LeaveChat(conversationId: $conversationId) }`
	documentMuteChat            = `mutation ($conversationId: String!, $mutedUntil: DateTime) { MuteChat(conversationId: $conversationId, mutedUntil: $mutedUntil) ` + chatAnswerFields + ` }`
	documentMarkChatRead        = `mutation ($conversationId: String!, $throughMessageId: String!) { MarkChatRead(conversationId: $conversationId, throughMessageId: $throughMessageId) }`
	documentEditChatMessage     = `mutation ($messageId: String!, $message: String!) { EditChatMessage(messageId: $messageId, message: $message) { id editedAt } }`
	documentDeleteChatMessage   = `mutation ($messageId: String!) { DeleteChatMessage(messageId: $messageId) }`
	documentSetChatAgentReading = `mutation ($conversationId: String!, $isAgentReading: Boolean!) { SetChatAgentReading(conversationId: $conversationId, isAgentReading: $isAgentReading) ` + chatAnswerFields + ` }`
	documentSetChatAgentAskable = `mutation ($conversationId: String!, $isAgentAskableByOthers: Boolean!) { SetChatAgentAskableByOthers(conversationId: $conversationId, isAgentAskableByOthers: $isAgentAskableByOthers) ` + chatAnswerFields + ` }`

	documentListCirclePeople = `query { ListCirclePeople { userId username name } }`
)

// mutedForGood is the time a mute that never ends is set to, the one the
// dashboard sets and recognizes as "until I unmute". A far year of the
// command line's would show there as a date.
const mutedForGood = "2999-01-01T00:00:00Z"

// chatAnswer is the conversation an operation answers with.
type chatAnswer struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Chat  struct {
		ChatKind   string     `json:"chatKind"`
		IsMuted    bool       `json:"isMuted"`
		MutedUntil *time.Time `json:"mutedUntil"`
		Members    []struct {
			UserID     string `json:"userId"`
			Username   string `json:"username"`
			Name       string `json:"name"`
			MemberRole string `json:"memberRole"`
			IsRemoved  bool   `json:"isRemoved"`
		} `json:"members"`
	} `json:"chat"`
}

// chatResult is what the model reads after a change to a chat: which
// conversation, what it is called now and who is in it. The names in it were
// chosen by other people, so it is data.
func chatResult(operationResult map[string]any, operation, note string) (*tools.Result, error) {
	encoded, err := json.Marshal(operationResult[operation])
	if err != nil {
		return nil, err
	}
	var answered chatAnswer
	if err := json.Unmarshal(encoded, &answered); err != nil {
		return nil, fmt.Errorf("the answer to %s is not a conversation: %w", operation, err)
	}
	answer := map[string]any{"conversation_id": answered.ID, "kind": "chat", "title": answered.Title}
	// The same shape as read's: by username too, which remove_member and
	// add_members take.
	people := make([]map[string]string, 0, len(answered.Chat.Members))
	for _, member := range answered.Chat.Members {
		if !member.IsRemoved {
			people = append(people, map[string]string{"name": member.Name, "username": member.Username, "role": member.MemberRole})
		}
	}
	answer["people_in_it"] = people
	if answered.Chat.IsMuted && answered.Chat.MutedUntil != nil {
		answer["muted_until"] = answered.Chat.MutedUntil.Format(time.RFC3339)
	}
	result, err := tools.JSONResult(answer)
	if err != nil {
		return nil, err
	}
	result.Untrusted = true
	result.Note = note
	return result, nil
}

// person is somebody the person shares a circle with.
type person struct {
	UserID   string `json:"userId"`
	Username string `json:"username"`
	Name     string `json:"name"`
}

// circlePeople are everybody the person shares a circle with, by lower-cased
// username: the only people a chat can be started with or added to.
func circlePeople(ctx context.Context) (map[string]person, error) {
	result, err := operator.Execute(ctx, documentListCirclePeople, nil)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(result["ListCirclePeople"])
	if err != nil {
		return nil, err
	}
	var listed []person
	if err := json.Unmarshal(encoded, &listed); err != nil {
		return nil, err
	}
	byUsername := make(map[string]person, len(listed))
	for _, listedPerson := range listed {
		byUsername[strings.ToLower(listedPerson.Username)] = listedPerson
	}
	return byUsername, nil
}

// usernameOf is a username as the person said it: without the @ a model
// puts before a name, and in the case accounts are looked up by.
func usernameOf(text string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(text), "@"))
}

// peopleNamed are the people with these usernames, once each, who must each
// share a circle with the person: the operation refuses anybody else, and
// this says which in the words of the circle tool.
func peopleNamed(ctx context.Context, usernames []string) ([]person, error) {
	people, err := circlePeople(ctx)
	if err != nil {
		return nil, err
	}
	found := make([]person, 0, len(usernames))
	seen := map[string]bool{}
	for _, username := range usernames {
		key := usernameOf(username)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		listedPerson, isKnown := people[key]
		if !isKnown {
			return nil, fmt.Errorf("the person shares no circle with %q; the circle tool's people action lists who they do", strings.TrimSpace(username))
		}
		found = append(found, listedPerson)
	}
	return found, nil
}

func userIDsOf(people []person) []string {
	userIds := make([]string, 0, len(people))
	for _, listedPerson := range people {
		userIds = append(userIds, listedPerson.UserID)
	}
	return userIds
}

// chatConversation is one of the agent's conversations that is a chat, with
// who is in it, or an error that reads the same whether it is somebody
// else's, not a chat, or not there. It is only for finding names and the
// newest message: whether the person may change the chat is the operation's
// to say.
func chatConversation(ctx context.Context, run tools.Run, conversationId string) (*models.AgentConversation, error) {
	conversationId = strings.TrimSpace(conversationId)
	if conversationId == "" {
		return nil, fmt.Errorf("this needs the conversation_id of a chat, which list gives")
	}
	var conversation *models.AgentConversation
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if conversation, err = tx.GetAgentConversation(conversationId); err != nil || conversation == nil {
			return err
		}
		if conversation.AgentID != run.Agent().ID || !conversation.IsChat() {
			conversation = nil
			return nil
		}
		return tx.ChatViewsFor(run.Agent().UserID, []*models.AgentConversation{conversation})
	}); err != nil {
		return nil, err
	}
	if conversation == nil || conversation.Chat == nil {
		return nil, fmt.Errorf("there is no chat %q", conversationId)
	}
	return conversation, nil
}

// runChatAction does one of the chat actions.
func runChatAction(ctx context.Context, run tools.Run, arguments conversationArguments) (*tools.Result, error) {
	conversationId := strings.TrimSpace(arguments.ConversationID)
	switch arguments.Action {
	case "direct":
		people, err := peopleNamed(ctx, []string{arguments.Username})
		if err != nil {
			return nil, err
		}
		if len(people) != 1 {
			return nil, fmt.Errorf("a direct chat needs the username of the one person to chat with")
		}
		result, err := operator.Execute(ctx, documentOpenDirectChat, map[string]any{"userId": people[0].UserID})
		if err != nil {
			return nil, err
		}
		return chatResult(result, "OpenDirectChat", "opened the direct chat")

	case "group":
		people, err := peopleNamed(ctx, arguments.Usernames)
		if err != nil {
			return nil, err
		}
		if len(people) == 0 {
			return nil, fmt.Errorf("a group chat needs the usernames of the people to put in it, besides the person")
		}
		result, err := operator.Execute(ctx, documentCreateGroupChat, map[string]any{"title": strings.TrimSpace(arguments.Title), "userIds": userIDsOf(people)})
		if err != nil {
			return nil, err
		}
		return chatResult(result, "CreateGroupChat", "started the group chat")

	case "rename":
		title := strings.TrimSpace(arguments.Title)
		if conversationId == "" || title == "" {
			return nil, fmt.Errorf("renaming needs the conversation_id of a group chat and the new title")
		}
		result, err := operator.Execute(ctx, documentRenameChat, map[string]any{"conversationId": conversationId, "title": title})
		if err != nil {
			return nil, err
		}
		return chatResult(result, "RenameChat", "renamed the group chat")

	case "add_members":
		people, err := peopleNamed(ctx, arguments.Usernames)
		if err != nil {
			return nil, err
		}
		if len(people) == 0 {
			return nil, fmt.Errorf("adding needs the usernames of the people to add")
		}
		conversation, err := chatConversation(ctx, run, conversationId)
		if err != nil {
			return nil, err
		}
		// Somebody already in the chat is not added again, as the command
		// line does; somebody who left is.
		newPeople := make([]person, 0, len(people))
		alreadyIn := []string{}
		for _, listedPerson := range people {
			if member := memberOf(conversation.Chat, listedPerson.UserID); member != nil && !member.IsRemoved {
				alreadyIn = append(alreadyIn, listedPerson.Name)
			} else {
				newPeople = append(newPeople, listedPerson)
			}
		}
		if len(newPeople) == 0 {
			// The names are the ones those people chose.
			result := tools.TextResult("%s already in this chat; added nobody", strings.Join(alreadyIn, ", "))
			result.Untrusted = true
			return result, nil
		}
		result, err := operator.Execute(ctx, documentAddChatMembers, map[string]any{"conversationId": conversationId, "userIds": userIDsOf(newPeople)})
		if err != nil {
			return nil, err
		}
		note := fmt.Sprintf("added %d to the chat", len(newPeople))
		if len(alreadyIn) > 0 {
			note += fmt.Sprintf("; %s already in it", strings.Join(alreadyIn, ", "))
		}
		return chatResult(result, "AddChatMembers", note)

	case "remove_member":
		conversation, err := chatConversation(ctx, run, conversationId)
		if err != nil {
			return nil, err
		}
		member, err := memberNamed(conversation, arguments.Username)
		if err != nil {
			return nil, err
		}
		result, err := operator.Execute(ctx, documentRemoveChatMember, map[string]any{"conversationId": conversationId, "userId": member.UserID})
		if err != nil {
			return nil, err
		}
		return chatResult(result, "RemoveChatMember", "took "+member.Name+" out of the chat")

	case "leave":
		if conversationId == "" {
			return nil, fmt.Errorf("leaving needs the conversation_id of a group chat")
		}
		if _, err := operator.Execute(ctx, documentLeaveChat, map[string]any{"conversationId": conversationId}); err != nil {
			return nil, err
		}
		return tools.TextResult("left the chat; the person can still read it up to now"), nil

	case "mute":
		if conversationId == "" {
			return nil, fmt.Errorf("muting needs the conversation_id of a chat")
		}
		mutedUntil, note, err := mutedUntilOf(ctx, arguments)
		if err != nil {
			return nil, err
		}
		result, err := operator.Execute(ctx, documentMuteChat, map[string]any{"conversationId": conversationId, "mutedUntil": mutedUntil})
		if err != nil {
			return nil, err
		}
		return chatResult(result, "MuteChat", note)

	case "mark_read":
		conversation, err := chatConversation(ctx, run, conversationId)
		if err != nil {
			return nil, err
		}
		throughMessageId := strings.TrimSpace(arguments.ChatMessageID)
		if throughMessageId == "" {
			if throughMessageId, err = newestChatMessageID(ctx, run, conversation); err != nil {
				return nil, err
			}
			if throughMessageId == "" {
				return tools.TextResult("nothing to read in this chat"), nil
			}
		}
		result, err := operator.Execute(ctx, documentMarkChatRead, map[string]any{"conversationId": conversationId, "throughMessageId": throughMessageId})
		if err != nil {
			return nil, err
		}
		if isMoved, _ := result["MarkChatRead"].(bool); !isMoved {
			return tools.TextResult("the chat was already marked read through there"), nil
		}
		return tools.TextResult("marked the chat read"), nil

	case "edit":
		chatMessageId, text := strings.TrimSpace(arguments.ChatMessageID), strings.TrimSpace(arguments.Text)
		if chatMessageId == "" || text == "" {
			return nil, fmt.Errorf("editing needs the chat_message_id of the person's own message, which read gives, and the text it should say now")
		}
		if _, err := operator.Execute(ctx, documentEditChatMessage, map[string]any{"messageId": chatMessageId, "message": text}); err != nil {
			return nil, err
		}
		return tools.TextResult("edited the message; the others see that it was edited"), nil

	case "delete_message":
		chatMessageId := strings.TrimSpace(arguments.ChatMessageID)
		if chatMessageId == "" {
			return nil, fmt.Errorf("deleting needs the chat_message_id of the message, which read gives")
		}
		if _, err := operator.Execute(ctx, documentDeleteChatMessage, map[string]any{"messageId": chatMessageId}); err != nil {
			return nil, err
		}
		return tools.TextResult("deleted the message for everybody in the chat"), nil

	case "remember", "others_may_ask":
		if conversationId == "" {
			return nil, fmt.Errorf("this needs the conversation_id of a chat")
		}
		if arguments.IsOn == nil {
			return nil, fmt.Errorf("say is_on: true to turn it on, false to turn it off")
		}
		if arguments.Action == "remember" {
			result, err := operator.Execute(ctx, documentSetChatAgentReading, map[string]any{"conversationId": conversationId, "isAgentReading": *arguments.IsOn})
			if err != nil {
				return nil, err
			}
			note := "you no longer read this chat; what you learned from it stays"
			if *arguments.IsOn {
				note = "you read this chat from now on, along with the rest of what you know"
			}
			return chatResult(result, "SetChatAgentReading", note)
		}
		result, err := operator.Execute(ctx, documentSetChatAgentAskable, map[string]any{"conversationId": conversationId, "isAgentAskableByOthers": *arguments.IsOn})
		if err != nil {
			return nil, err
		}
		note := "only the person can ask you in this chat"
		if *arguments.IsOn {
			note = "the others in this chat may ask you there; you answer them with the chat and the web only"
		}
		return chatResult(result, "SetChatAgentAskableByOthers", note)
	}
	return nil, fmt.Errorf("%q is not an action on a chat", arguments.Action)
}

// memberOf is the member of a chat with this account, or nil.
func memberOf(chat *models.ChatView, userId string) *models.ChatMember {
	for _, member := range chat.Members {
		if member.UserID == userId {
			return member
		}
	}
	return nil
}

// memberNamed is the person in a chat that a username, or failing that a
// name, points at. The command line takes the username; a model has read the
// name, so an exact name that only one member has is taken too.
func memberNamed(conversation *models.AgentConversation, text string) (*models.ChatMember, error) {
	key := usernameOf(text)
	if key == "" {
		return nil, fmt.Errorf("whom to take out? give their username, which read shows for each person in the chat")
	}
	var byName []*models.ChatMember
	for _, member := range conversation.Chat.Members {
		if member.IsRemoved || member.UserID == "" {
			continue
		}
		if strings.ToLower(member.Username) == key {
			return member, nil
		}
		if strings.ToLower(strings.TrimSpace(member.Name)) == key {
			byName = append(byName, member)
		}
	}
	switch len(byName) {
	case 1:
		return byName[0], nil
	case 0:
		return nil, fmt.Errorf("%q is not in this chat; read shows who is", strings.TrimSpace(text))
	}
	return nil, fmt.Errorf("more than one person in this chat is called %q; give the username, which read shows", strings.TrimSpace(text))
}

// mutedUntilOf is when a mute ends, as the operation takes it, and what to
// say about it: nothing to stop silencing, a time or a length of time
// from now, or no end at all.
func mutedUntilOf(ctx context.Context, arguments conversationArguments) (any, string, error) {
	if arguments.IsMuted != nil && !*arguments.IsMuted {
		if strings.TrimSpace(arguments.Until) != "" {
			return nil, "", fmt.Errorf("give until to mute until a time, or is_muted false to stop muting, not both")
		}
		return nil, "notifications are on again", nil
	}
	until := strings.TrimSpace(arguments.Until)
	if until == "" {
		return mutedForGood, "muted until the person unmutes it", nil
	}
	now := time.Now()
	var ends time.Time
	if duration, err := tools.ParseDuration(until); err == nil {
		ends = now.Add(duration)
	} else {
		var parseErr error
		if ends, parseErr = tools.ParseTime(until, tools.Location(tools.MustRun(ctx).Owner()), now); parseErr != nil {
			return nil, "", fmt.Errorf("%q is not a time or a length of time; try 2026-11-01 09:00, 8h or 3d", until)
		}
	}
	// The operation takes a time that has passed as "stop silencing it",
	// which is not what was asked.
	if !ends.After(now) {
		return nil, "", fmt.Errorf("%q has already passed; to stop muting a chat, say is_muted false", until)
	}
	return ends.UTC().Format(time.RFC3339), "muted until " + ends.UTC().Format(time.RFC3339), nil
}

// newestChatMessageID is the newest message of a chat that people said, as
// the command line marks a chat read through. The conversation of a chat
// also holds the agent's own rows, which are not messages of the chat.
func newestChatMessageID(ctx context.Context, run tools.Run, conversation *models.AgentConversation) (string, error) {
	var messages []*models.AgentMessage
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		messages, _, err = tx.ListChatConversationMessages(conversation, 50, 0)
		return err
	}); err != nil {
		return "", err
	}
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].AuthorKind != "" {
			return messages[index].ID, nil
		}
	}
	return "", nil
}

// chatPreview is the card for the chat actions that ask. It says what will
// happen in the words the dashboard uses for it, naming the chat and the
// people; what cannot be looked up is said without a name, never guessed.
func chatPreview(ctx context.Context, call conversationArguments) string {
	conversation := previewChat(ctx, call.ConversationID)
	where := "a chat"
	title := ""
	if conversation != nil {
		title = strings.TrimSpace(conversation.Title)
	}
	if title != "" {
		where = "the chat " + tools.Named(title, "")
	}
	switch strings.TrimSpace(call.Action) {
	case "add_members":
		return "Add " + previewPeople(ctx, call.Usernames) + " to " + where + ", who then read everything said there from now on"
	case "remove_member":
		name := tools.Named(call.Username, "somebody")
		if conversation != nil && conversation.Chat != nil {
			if member, err := memberNamed(conversation, call.Username); err == nil {
				name = tools.Named(member.Name, name)
			}
		}
		return "Remove " + name + " from " + where + ". They will not see new messages there; what was said stays"
	case "leave":
		return "Leave " + where + ". The person stops getting its messages and can read it only up to now; an admin of the group has to add them again"
	case "delete_message":
		said, where := messagePreview(ctx, call)
		return "Delete " + said + " in " + where + " for everybody there, who will see \"Message deleted\" in its place"
	case "edit":
		said, where := messagePreview(ctx, call)
		return "Change " + said + " in " + where + " to say " + tools.Named(cutRunes(strings.TrimSpace(call.Text), 100), "nothing") +
			"; everybody there reads it as yours, marked edited, and what it said before is not kept"
	case "others_may_ask":
		if call.IsOn != nil && !*call.IsOn {
			return ""
		}
		return "Let everyone in " + where + " ask the agent there with @. It answers them in front of everybody, from the chat and the web and nothing of the person's"
	}
	return ""
}

// messagePreview names the message a delete or an edit is about, whose it is
// and the chat it is in, when it is one of a chat the person is in. Both name
// only the message, so the chat is found from it.
func messagePreview(ctx context.Context, call conversationArguments) (said string, where string) {
	said, where = "a message", "a chat"
	if run, err := tools.RunFrom(ctx); err == nil && run.Database() != nil {
		_ = run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
			chatMessage, err := tx.GetChatMessage(strings.TrimSpace(call.ChatMessageID))
			if err != nil || chatMessage == nil {
				return err
			}
			// A message is named only in a chat the person has a
			// conversation for: the card is not a way to read others.
			conversations, err := tx.ListChatConversations(run.Agent().ID)
			if err != nil {
				return err
			}
			if err := tx.ChatViewsFor(run.Agent().UserID, conversations); err != nil {
				return err
			}
			for _, conversation := range conversations {
				if conversation.ChatID != chatMessage.ChatID {
					continue
				}
				said = "the message " + tools.Named(cutRunes(strings.TrimSpace(chatMessage.Body), 100), "with no words")
				if author := strings.TrimSpace(chatMessage.AuthorName); author != "" {
					said += " from " + tools.Named(author, "")
				}
				if title := strings.TrimSpace(conversation.Title); title != "" {
					where = "the chat " + tools.Named(title, "")
				}
			}
			return nil
		})
	}
	return said, where
}

// previewChat is the conversation a card is about, found only among the
// agent's own, or nil.
func previewChat(ctx context.Context, conversationId string) *models.AgentConversation {
	conversationId = strings.TrimSpace(conversationId)
	run, err := tools.RunFrom(ctx)
	if conversationId == "" || err != nil || run.Database() == nil {
		return nil
	}
	var conversation *models.AgentConversation
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if conversation, err = tx.GetAgentConversation(conversationId); err != nil || conversation == nil {
			return err
		}
		if conversation.AgentID != run.Agent().ID {
			conversation = nil
			return nil
		}
		return tx.ChatViewsFor(run.Agent().UserID, []*models.AgentConversation{conversation})
	}); err != nil {
		return nil
	}
	return conversation
}

// previewPeople names the people by what the circle calls them, or by the
// username that was given when they cannot be looked up.
func previewPeople(ctx context.Context, usernames []string) string {
	known := map[string]person{}
	if _, err := tools.RunFrom(ctx); err == nil {
		if people, err := circlePeople(ctx); err == nil {
			known = people
		}
	}
	names := make([]string, 0, len(usernames))
	for _, username := range usernames {
		key := usernameOf(username)
		if key == "" {
			continue
		}
		if listedPerson, isKnown := known[key]; isKnown && strings.TrimSpace(listedPerson.Name) != "" {
			names = append(names, tools.Named(listedPerson.Name, ""))
		} else {
			names = append(names, tools.Named(strings.TrimSpace(username), ""))
		}
	}
	if len(names) == 0 {
		return "somebody"
	}
	return strings.Join(names, ", ")
}
