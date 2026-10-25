package cmd

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/models"
)

// Chats from a terminal: conversations with other people in them. A chat is
// listed, shown, written to and reacted to with the commands for any
// conversation (list, show, ask, chat and react in agent_chat.go); these are
// the ones only a chat needs. People are named by username, the way
// "teanode circle" names them, and chats by the id of your own conversation
// for them, which "conversation list" prints.

// chatCommands are the subcommands of "agent conversation" for chats.
func chatCommands() []*cli.Command {
	return []*cli.Command{
		{
			Name:      "direct",
			Usage:     "open your chat with somebody you share a circle with; it prints the conversation to use",
			ArgsUsage: "<username>",
			Flags:     []cli.Flag{JSONFlag()},
			Action:    runChatDirect,
		},
		{
			Name:      "group",
			Usage:     "start a group chat with people you share a circle with, you as its admin",
			ArgsUsage: "<title>",
			Flags: []cli.Flag{
				&cli.StringSliceFlag{Name: "with", Usage: "who is in it besides you, by username; repeatable, or comma-separated", Required: true},
				JSONFlag(),
			},
			Action: runChatGroup,
		},
		{
			Name:      "members",
			Usage:     "who is in a chat, who has left, and how far each has read",
			ArgsUsage: "<conversation-id>",
			Flags:     []cli.Flag{JSONFlag()},
			Action:    runChatMembers,
		},
		{
			Name:      "add",
			Usage:     "put people in a group chat, who read it from then on; needs an admin of the chat",
			ArgsUsage: "<conversation-id> <username>...",
			Flags:     []cli.Flag{JSONFlag()},
			Action:    runChatAdd,
		},
		{
			Name:      "remove",
			Usage:     "take a person out of a group chat; needs an admin of the chat",
			ArgsUsage: "<conversation-id> <username>",
			Flags:     []cli.Flag{ForceFlag()},
			Action:    runChatRemove,
		},
		{
			Name:      "leave",
			Usage:     "leave a group chat; you can still read it up to now",
			ArgsUsage: "<conversation-id>",
			Flags:     []cli.Flag{ForceFlag()},
			Action:    runChatLeave,
		},
		{
			Name:      "notifications",
			Usage:     "choose what your phone is told of in a chat: all of it, only mentions of you or everybody, or off; it stays until you change it",
			ArgsUsage: "<conversation-id> <all|mentions|off>",
			Flags:     []cli.Flag{JSONFlag()},
			Action:    runChatNotifications,
		},
		{
			Name:      "read",
			Usage:     "say you have read a chat through its newest message",
			ArgsUsage: "<conversation-id>",
			Action:    runChatRead,
		},
		{
			Name:      "remember",
			Usage:     "let your agent remember a chat from now on, as a knowledge source it reads with the rest, or stop; what it learned stays",
			ArgsUsage: "<conversation-id> on|off",
			Flags:     []cli.Flag{JSONFlag()},
			Action:    runChatRemember,
		},
		{
			Name:      "others-may-ask",
			Usage:     "others in this chat may ask your agent there (on), who then answers them limited to the chat and the web, or may not (off)",
			ArgsUsage: "<conversation-id> on|off",
			Flags:     []cli.Flag{JSONFlag()},
			Action:    runChatOthersMayAsk,
		},
		{
			Name:      "edit",
			Usage:     "change what one of your messages says; the others see it was edited",
			ArgsUsage: "<message-id> <text>",
			Flags:     []cli.Flag{JSONFlag()},
			Action:    runChatEdit,
		},
		{
			Name:      "delete-message",
			Usage:     "delete one of your messages for everybody in the chat; in a group chat an admin may delete any",
			ArgsUsage: "<message-id>",
			Flags:     []cli.Flag{ForceFlag()},
			Action:    runChatDeleteMessage,
		},
	}
}

// chatPeople maps usernames, lower-cased, to the account ids of everybody
// you share a circle with.
func chatPeople(ctx context.Context, command *cli.Command, connection *client.Client) (map[string]*client.CircleMember, error) {
	people, err := client.ListCirclePeople(ctx, connection)
	if err != nil {
		return nil, describeError(command, err)
	}
	byUsername := make(map[string]*client.CircleMember, len(people))
	for _, person := range people {
		byUsername[strings.ToLower(person.Username)] = person
	}
	return byUsername, nil
}

// userIdsOf are the account ids of the usernames, which must each be
// somebody you share a circle with.
func userIdsOf(ctx context.Context, command *cli.Command, connection *client.Client, usernames []string) ([]string, error) {
	people, err := chatPeople(ctx, command, connection)
	if err != nil {
		return nil, err
	}
	userIds := make([]string, 0, len(usernames))
	for _, username := range usernames {
		person, ok := people[strings.ToLower(strings.TrimSpace(username))]
		if !ok {
			return nil, fmt.Errorf("you share no circle with %q; teanode circle people lists who you do", username)
		}
		userIds = append(userIds, person.UserID)
	}
	return userIds, nil
}

func requireConversationArgument(command *cli.Command) (string, error) {
	conversationId := strings.TrimSpace(command.Args().First())
	if conversationId == "" {
		return "", fmt.Errorf("which chat? give your conversation for it, as conversation list shows")
	}
	return conversationId, nil
}

func printChatConversation(command *cli.Command, conversation *client.AgentConversation, isJson bool, said string) error {
	if isJson {
		return PrintJSON(conversation)
	}
	_, _ = fmt.Fprintf(command.Writer, "conversation %s: %s%s\n", conversation.ID, forTerminal(conversation.Title), said)
	return nil
}

// splitNewMembers says which of the people to add are not in the chat yet and
// which already are. Somebody who left is added again; a deleted account is
// not in the chat either.
func splitNewMembers(members []*client.AgentChatMember, userIds []string) (newUserIds []string, alreadyMembers []*client.AgentChatMember) {
	for _, userId := range userIds {
		var present *client.AgentChatMember
		for _, member := range members {
			if member.UserID == userId && !member.IsRemoved {
				present = member
			}
		}
		if present != nil {
			alreadyMembers = append(alreadyMembers, present)
		} else {
			newUserIds = append(newUserIds, userId)
		}
	}
	return newUserIds, alreadyMembers
}

func runChatDirect(ctx context.Context, command *cli.Command) error {
	username := strings.TrimSpace(command.Args().First())
	if username == "" {
		return fmt.Errorf("with whom? give their username")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	userIds, err := userIdsOf(ctx, command, connection, []string{username})
	if err != nil {
		return err
	}
	conversation, err := client.OpenDirectChat(ctx, connection, userIds[0])
	if err != nil {
		return describeError(command, err)
	}
	return printChatConversation(command, conversation, command.Bool("json"), "")
}

func runChatGroup(ctx context.Context, command *cli.Command) error {
	title := strings.TrimSpace(strings.Join(command.Args().Slice(), " "))
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	userIds, err := userIdsOf(ctx, command, connection, flagList(command, "with"))
	if err != nil {
		return err
	}
	conversation, err := client.CreateGroupChat(ctx, connection, title, userIds)
	if err != nil {
		return describeError(command, err)
	}
	return printChatConversation(command, conversation, command.Bool("json"), "")
}

// chatOf reads the newest page of a conversation and returns it, refusing
// one that is not a chat.
func chatOf(ctx context.Context, command *cli.Command, connection *client.Client, conversationId string, first int) (*client.AgentConversationView, error) {
	view, err := client.ReadAgentConversation(ctx, connection, conversationId, first, 0)
	if err != nil {
		return nil, describeError(command, err)
	}
	if view.Conversation == nil || view.Conversation.Chat == nil {
		return nil, fmt.Errorf("%s is not a chat", conversationId)
	}
	return view, nil
}

// memberWords is a member's name, and where they stand if not in the chat.
func memberWords(member *client.AgentChatMember) string {
	words := forTerminal(member.Name)
	switch {
	case member.IsDeletedAccount:
		words += " (deleted account)"
	case member.IsRemoved:
		words += " (left)"
	}
	return words
}

func runChatMembers(ctx context.Context, command *cli.Command) error {
	conversationId, err := requireConversationArgument(command)
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	view, err := chatOf(ctx, command, connection, conversationId, 100)
	if err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(view.Conversation.Chat.Members)
	}
	readAt := map[string]time.Time{}
	for _, message := range view.Messages {
		readAt[message.ID] = message.CreatedAt
	}
	rows := make([][]string, 0, len(view.Conversation.Chat.Members))
	for _, member := range view.Conversation.Chat.Members {
		read := ""
		if at, isKnown := readAt[member.ReadThroughMessageID]; isKnown {
			read = at.Local().Format("2006-01-02 15:04")
		}
		rows = append(rows, []string{memberWords(member), member.Username, member.MemberRole, read, agentWords(member)})
	}
	return printTable([]string{"name", "username", "role", "read through", "agent"}, rows)
}

// agentWords is what a member's agent does in the chat, as far as you are told:
// its name when it is yours or may be asked, and whether it remembers the chat.
func agentWords(member *client.AgentChatMember) string {
	if member.IsRemoved {
		return ""
	}
	parts := []string{}
	if member.AgentName != "" {
		name := forTerminal(member.AgentName)
		if !member.IsAgentActive {
			name += " (off)"
		}
		parts = append(parts, name)
	}
	if member.IsAgentAskableByOthers {
		parts = append(parts, "others may ask it")
	}
	if member.IsAgentReading {
		parts = append(parts, "remembers this chat")
	}
	return strings.Join(parts, ", ")
}

// onOrOff reads the second argument of a switch.
func onOrOff(command *cli.Command) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(command.Args().Get(1))) {
	case "on":
		return true, nil
	case "off":
		return false, nil
	}
	return false, fmt.Errorf("say on or off: %s <conversation-id> on|off", command.Name)
}

func runChatRemember(ctx context.Context, command *cli.Command) error {
	conversationId, err := requireConversationArgument(command)
	if err != nil {
		return err
	}
	isOn, err := onOrOff(command)
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	conversation, err := client.SetChatAgentReading(ctx, connection, conversationId, isOn)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(conversation)
	}
	if isOn {
		_, _ = fmt.Fprintf(command.Writer, "%s: your agent remembers this chat from now on\n", conversationId)
	} else {
		_, _ = fmt.Fprintf(command.Writer, "%s: your agent no longer reads this chat; what it learned stays\n", conversationId)
	}
	return nil
}

func runChatOthersMayAsk(ctx context.Context, command *cli.Command) error {
	conversationId, err := requireConversationArgument(command)
	if err != nil {
		return err
	}
	isOn, err := onOrOff(command)
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	conversation, err := client.SetChatAgentAskableByOthers(ctx, connection, conversationId, isOn)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(conversation)
	}
	if isOn {
		_, _ = fmt.Fprintf(command.Writer, "%s: the others in this chat may ask your agent there; it answers them with the chat and the web only\n", conversationId)
	} else {
		_, _ = fmt.Fprintf(command.Writer, "%s: only you can ask your agent in this chat\n", conversationId)
	}
	return nil
}

func runChatAdd(ctx context.Context, command *cli.Command) error {
	conversationId, err := requireConversationArgument(command)
	if err != nil {
		return err
	}
	usernames := command.Args().Slice()[1:]
	if len(usernames) == 0 {
		return fmt.Errorf("whom to add? give their usernames")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	userIds, err := userIdsOf(ctx, command, connection, usernames)
	if err != nil {
		return err
	}
	view, err := chatOf(ctx, command, connection, conversationId, 1)
	if err != nil {
		return err
	}
	newUserIds, alreadyMembers := splitNewMembers(view.Conversation.Chat.Members, userIds)
	if len(newUserIds) == 0 {
		// Nothing to send: say who is in already, and add nobody.
		names := make([]string, 0, len(alreadyMembers))
		for _, member := range alreadyMembers {
			names = append(names, memberWords(member))
		}
		if command.Bool("json") {
			return PrintJSON(view.Conversation)
		}
		verb := "is"
		if len(names) > 1 {
			verb = "are"
		}
		_, _ = fmt.Fprintf(command.Writer, "conversation %s: %s already %s in this chat; added nobody\n", conversationId, strings.Join(names, ", "), verb)
		return nil
	}
	conversation, err := client.AddChatMembers(ctx, connection, conversationId, newUserIds)
	if err != nil {
		return describeError(command, err)
	}
	said := fmt.Sprintf(" (%d added)", len(newUserIds))
	if len(alreadyMembers) > 0 {
		said = fmt.Sprintf(" (%d added; %d already in the chat)", len(newUserIds), len(alreadyMembers))
	}
	return printChatConversation(command, conversation, command.Bool("json"), said)
}

func runChatRemove(ctx context.Context, command *cli.Command) error {
	conversationId, err := requireConversationArgument(command)
	if err != nil {
		return err
	}
	username := strings.TrimSpace(command.Args().Get(1))
	if username == "" {
		return fmt.Errorf("whom to take out? give their username")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	view, err := chatOf(ctx, command, connection, conversationId, 1)
	if err != nil {
		return err
	}
	var found *client.AgentChatMember
	for _, member := range view.Conversation.Chat.Members {
		if !member.IsRemoved && strings.EqualFold(member.Username, username) {
			found = member
		}
	}
	if found == nil {
		return fmt.Errorf("%q is not in this chat; conversation members lists who is", username)
	}
	if err := confirm(command, fmt.Sprintf("This takes %s out of %s.", memberWords(found), forTerminal(view.Conversation.Title))); err != nil {
		return err
	}
	if _, err := client.RemoveChatMember(ctx, connection, conversationId, found.UserID); err != nil {
		return describeError(command, err)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s: %s is out\n", conversationId, memberWords(found))
	return nil
}

func runChatLeave(ctx context.Context, command *cli.Command) error {
	conversationId, err := requireConversationArgument(command)
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := confirm(command, fmt.Sprintf("This takes you out of chat %s.", conversationId)); err != nil {
		return err
	}
	if err := client.LeaveChat(ctx, connection, conversationId); err != nil {
		return describeError(command, err)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s: you left\n", conversationId)
	return nil
}

func runChatNotifications(ctx context.Context, command *cli.Command) error {
	conversationId, err := requireConversationArgument(command)
	if err != nil {
		return err
	}
	notificationLevel := strings.ToLower(strings.TrimSpace(command.Args().Get(1)))
	said, isKnown := map[string]string{
		"all":      ": your phone is told of every message",
		"mentions": ": your phone is told only of a message that mentions you or everybody",
		"off":      ": your phone is told of nothing here",
	}[notificationLevel]
	if !isKnown {
		return fmt.Errorf("say all, mentions or off after the conversation id")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	conversation, err := client.SetChatNotificationLevel(ctx, connection, conversationId, notificationLevel)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(conversation)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s%s\n", conversationId, said)
	return nil
}

func runChatRead(ctx context.Context, command *cli.Command) error {
	conversationId, err := requireConversationArgument(command)
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	view, err := chatOf(ctx, command, connection, conversationId, 1)
	if err != nil {
		return err
	}
	if len(view.Messages) == 0 {
		_, _ = fmt.Fprintf(command.Writer, "%s: nothing to read\n", conversationId)
		return nil
	}
	if _, err := client.MarkChatRead(ctx, connection, conversationId, view.Messages[len(view.Messages)-1].ID); err != nil {
		return describeError(command, err)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s: read\n", conversationId)
	return nil
}

func runChatEdit(ctx context.Context, command *cli.Command) error {
	messageId := strings.TrimSpace(command.Args().First())
	text := strings.TrimSpace(strings.Join(command.Args().Slice()[min(1, command.Args().Len()):], " "))
	if messageId == "" || text == "" {
		return fmt.Errorf("give the message id and what it should say now")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	message, err := client.EditChatMessage(ctx, connection, messageId, text)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(message)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s: edited\n", message.ID)
	return nil
}

func runChatDeleteMessage(ctx context.Context, command *cli.Command) error {
	messageId := strings.TrimSpace(command.Args().First())
	if messageId == "" {
		return fmt.Errorf("which message? give its id, as conversation show --json lists them")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := confirm(command, fmt.Sprintf("This deletes message %s for everybody in the chat.", messageId)); err != nil {
		return err
	}
	if err := client.DeleteChatMessage(ctx, connection, messageId); err != nil {
		return describeError(command, err)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s: deleted\n", messageId)
	return nil
}

// authorWords is who a chat message is from, as a terminal says it: you, a
// person by name, or an agent by its name and whose it is, since most are
// called "Agent" and a chat can have several.
func authorWords(message *client.AgentMessage) string {
	name := forTerminal(strings.TrimSpace(message.AuthorName))
	switch {
	case message.AuthorKind == "agent":
		return forTerminal(models.AgentSpeakerLabel(message.AuthorName, message.AuthorOwnerName))
	case message.IsMine:
		return "you"
	case message.AuthorUserID == "":
		return name + " (deleted account)"
	}
	return name
}

// chatMessageLines are one chat message as a terminal shows it, with what it
// replies to and the reactions under it. What others wrote is made safe for
// the terminal.
func chatMessageLines(message *client.AgentMessage) string {
	var builder strings.Builder
	when := message.CreatedAt.Local().Format("15:04")
	for _, reference := range message.References {
		if reference.QuotedText != "" {
			quoted := strings.Join(strings.Fields(reference.QuotedText), " ")
			if runes := []rune(quoted); len(runes) > 80 {
				quoted = string(runes[:80]) + "…"
			}
			_, _ = fmt.Fprintf(&builder, "[%s] ↪ replying to %q\n", when, forTerminal(quoted))
		}
	}
	switch {
	case message.IsDeleted:
		_, _ = fmt.Fprintf(&builder, "[%s] %s: (deleted)  %s\n", when, authorWords(message), message.ID)
	default:
		edited := ""
		if message.EditedAt != nil {
			edited = " (edited)"
		}
		_, _ = fmt.Fprintf(&builder, "[%s] %s: %s%s  %s\n", when, authorWords(message), forTerminal(message.Content), edited, message.ID)
	}
	for _, attachment := range message.Attachments {
		_, _ = fmt.Fprintf(&builder, "        (file %s)\n", forTerminal(attachment.Name))
	}
	if len(message.Reactions) > 0 {
		reacted := make([]string, 0, len(message.Reactions))
		for _, reaction := range message.Reactions {
			name := forTerminal(reaction.ReactorName)
			if name == "" {
				name = "somebody"
			}
			reacted = append(reacted, name+" reacted "+reaction.Emoji)
		}
		_, _ = fmt.Fprintf(&builder, "        (%s)\n", strings.Join(reacted, "; "))
	}
	return builder.String()
}

// ownWorkLine is a row of your own agent's in a chat conversation, as a
// terminal shows it: the tool it used or a note, a line to itself and not
// something said. Empty for a row that has nothing to show, such as the call a
// round made, whose words are the chat's.
func ownWorkLine(message *client.AgentMessage) string {
	when := message.CreatedAt.Local().Format("15:04")
	switch message.Role {
	case "tool":
		return fmt.Sprintf("        [%s] (your agent used %s)\n", when, forTerminal(message.Name))
	case "note":
		return fmt.Sprintf("        [%s] (%s)\n", when, forTerminal(models.NoteText(models.AgentNoteKind(message.Name), message.Content)))
	}
	return ""
}

// isChatLine says a row of a chat conversation is something said, as opposed
// to the work your own agent did to answer.
func isChatLine(message *client.AgentMessage) bool {
	if message.IsAgentWork || (message.Role != "user" && message.Role != "assistant") {
		return false
	}
	return message.IsDeleted || strings.TrimSpace(message.Content) != "" || len(message.Attachments) > 0
}

// printChatTranscript is the newest messages of a chat, who wrote each, and
// who is in it.
func printChatTranscript(command *cli.Command, view *client.AgentConversationView) error {
	chat := view.Conversation.Chat
	names := make([]string, 0, len(chat.Members))
	for _, member := range chat.Members {
		names = append(names, memberWords(member))
	}
	_, _ = fmt.Fprintf(command.Writer, "%s (%s), %d message(s), %d unread\n", forTerminal(view.Conversation.Title), view.Conversation.ID, view.Total, chat.UnreadCount)
	_, _ = fmt.Fprintf(command.Writer, "in it: %s\n", strings.Join(names, ", "))
	if chat.IsRemoved {
		_, _ = fmt.Fprintln(command.Writer, "you are no longer in this chat; this is what was said while you were")
	}
	switch chat.NotificationLevel {
	case "mentions":
		_, _ = fmt.Fprintln(command.Writer, "your phone is told only of mentions")
	case "off":
		_, _ = fmt.Fprintln(command.Writer, "your phone is told of nothing here")
	}
	_, _ = fmt.Fprintln(command.Writer)
	for _, message := range view.Messages {
		if isChatLine(message) {
			_, _ = fmt.Fprint(command.Writer, chatMessageLines(message))
			continue
		}
		// A row of your own agent's in the conversation: the work it did to
		// answer, which nobody else reads.
		if line := ownWorkLine(message); line != "" {
			_, _ = fmt.Fprint(command.Writer, line)
		}
	}
	return nil
}

// chatPollEvery is how often a terminal looks for what the others said.
const chatPollEvery = 2 * time.Second

// runChatFollow is "agent chat" on a chat: it prints what the others say as
// it arrives and sends each line typed, until an empty line or the end of
// the input. A terminal has no way to be pushed to, so it looks every couple
// of seconds.
func runChatFollow(ctx context.Context, command *cli.Command, connection *client.Client, conversationId string) error {
	view, err := chatOf(ctx, command, connection, conversationId, 10)
	if err != nil {
		return err
	}
	var output sync.Mutex
	write := func(text string) {
		output.Lock()
		defer output.Unlock()
		_, _ = fmt.Fprint(command.Writer, text)
	}
	lastId := ""
	for _, message := range view.Messages {
		if isChatLine(message) {
			write(chatMessageLines(message))
		}
		lastId = message.ID
	}
	if lastId != "" {
		_, _ = client.MarkChatRead(ctx, connection, conversationId, lastId)
	}
	write(fmt.Sprintf("In %s; start a line with @agent to ask your agent there, @ and a name mentions that person and @all everybody; an empty line or Ctrl-D leaves.\n", forTerminal(view.Conversation.Title)))
	members := chatMembersOf(view.Conversation.Chat)

	followContext, stopFollowing := context.WithCancel(ctx)
	var waitGroup sync.WaitGroup
	defer func() {
		stopFollowing()
		waitGroup.Wait()
	}()
	waitGroup.Add(1)
	go func() {
		defer waitGroup.Done()
		ticker := time.NewTicker(chatPollEvery)
		defer ticker.Stop()
		for {
			select {
			case <-followContext.Done():
				return
			case <-ticker.C:
			}
			latest, err := client.ReadAgentConversation(followContext, connection, conversationId, 20, 0)
			if err != nil {
				continue
			}
			newest := ""
			for _, message := range latest.Messages {
				if message.ID > lastId {
					if isChatLine(message) {
						write(chatMessageLines(message))
					}
					newest = message.ID
				}
			}
			if newest != "" {
				lastId = newest
				_, _ = client.MarkChatRead(followContext, connection, conversationId, newest)
			}
		}
	}()

	reader := readerOf(command.Reader)
	for {
		line, err := reader.ReadString('\n')
		message := strings.TrimSpace(line)
		if message == "" {
			return nil
		}
		// "@agent" at the start addresses your own agent, which answers in the
		// chat; the words are said as they are.
		addressesAgent := strings.HasPrefix(strings.ToLower(message), "@agent ") || strings.ToLower(message) == "@agent"
		// "@" and a member's name mentions them, and "@all" everybody: a
		// terminal has no menu to choose them from, so the words are read.
		mentionedUserIds, isAllMentioned := models.ChatMentionsIn(message, members)
		if _, sendError := client.AskAgentWith(ctx, connection, &client.AskAgentRequest{
			ConversationID: conversationId, Message: message, Surface: terminalSurface("cli"), ShouldAddressOwnAgent: addressesAgent,
			MentionedUserIDs: mentionedUserIds, IsAllMentioned: isAllMentioned,
		}); sendError != nil {
			return describeError(command, sendError)
		}
		if err != nil {
			return nil
		}
	}
}

// chatMembersOf are the people in a chat as models.ChatMentionsIn reads them.
func chatMembersOf(chat *client.AgentChat) []*models.ChatMember {
	if chat == nil {
		return nil
	}
	members := make([]*models.ChatMember, 0, len(chat.Members))
	for _, member := range chat.Members {
		members = append(members, &models.ChatMember{UserID: member.UserID, Name: member.Name, IsRemoved: member.IsRemoved})
	}
	return members
}
