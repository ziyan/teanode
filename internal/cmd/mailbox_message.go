package cmd

import (
	"context"
	"fmt"
	"io"
	"net/mail"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v3"
	"golang.org/x/term"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/util/security"
)

// The person's own messages, from the shell: the same operations as the
// dashboard's list, reader and composer, so a message is found, read, filed,
// flagged, reported, written and sent with the same names and the same
// refusals. 'teanode mail' is the operator's log of mail the server handled,
// not this.

func newMessageCommand() *cli.Command {
	return &cli.Command{
		Name:    "message",
		Aliases: []string{"messages"},
		Usage:   "the messages in this mailbox: find, read, file, flag, report, write and send",
		Commands: []*cli.Command{
			{
				Name:  "list",
				Usage: "list messages, newest first; the Inbox unless --folder or --everywhere says otherwise",
				Flags: []cli.Flag{
					JSONFlag(), mailboxFlag(),
					&cli.StringFlag{Name: "folder", Usage: "which folder, by name or identifier; the Inbox by default"},
					&cli.BoolFlag{Name: "everywhere", Usage: "every folder of the mailbox at once"},
					&cli.BoolFlag{Name: "unread", Usage: "only unread messages"},
					&cli.BoolFlag{Name: "flagged", Usage: "only flagged messages"},
					&cli.BoolFlag{Name: "has-attachment", Usage: "only messages with an attachment"},
					&cli.StringFlag{Name: "from", Usage: "part of the sender, case-insensitively"},
					&cli.StringFlag{Name: "to", Usage: "part of a recipient"},
					&cli.StringFlag{Name: "subject", Usage: "part of the subject"},
					&cli.StringFlag{Name: "search", Usage: "words to look for in the subject, sender, recipients and text"},
					&cli.StringFlag{Name: "since", Usage: "received on or after a date like 2026-09-01 or a duration like 72h"},
					&cli.StringFlag{Name: "before", Usage: "received before a date like 2026-09-01 or a duration back from now like 72h"},
					&cli.IntFlag{Name: "first", Value: 50, Usage: "how many at most, up to 200"},
					&cli.IntFlag{Name: "offset", Usage: "skip this many, for the next page of a search across folders"},
					&cli.StringFlag{Name: "after", Usage: "continue after this message identifier, for the next page of a folder"},
				},
				Action: runMessageList,
			},
			{
				Name:      "show",
				Usage:     "print a message's headers and text, or a whole conversation with --thread",
				ArgsUsage: "<id>",
				Flags: []cli.Flag{
					JSONFlag(), mailboxFlag(),
					&cli.BoolFlag{Name: "thread", Usage: "the whole conversation this message belongs to, oldest first"},
					&cli.BoolFlag{Name: "html", Usage: "print the HTML part instead of the text"},
					&cli.StringFlag{Name: "save-attachments", Usage: "write the message's attachments into this directory"},
				},
				Action: runMessageShow,
			},
			{
				Name:      "move",
				Usage:     "file messages in another folder; a moved message gets a new identifier, which is printed",
				ArgsUsage: "<id>...",
				Flags: []cli.Flag{
					JSONFlag(), mailboxFlag(),
					&cli.StringFlag{Name: "folder", Usage: "the folder to file them in, by name or identifier", Required: true},
				},
				Action: runMessageMove,
			},
			{
				Name:      "flag",
				Usage:     "mark messages read or unread, flagged or not",
				ArgsUsage: "<id>...",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.BoolFlag{Name: "read", Usage: "mark read"},
					&cli.BoolFlag{Name: "unread", Usage: "mark unread"},
					&cli.BoolFlag{Name: "flag", Usage: "flag"},
					&cli.BoolFlag{Name: "unflag", Usage: "take the flag off"},
				},
				Action: runMessageFlag,
			},
			{
				Name:      "junk",
				Usage:     "move messages to Junk and tell the spam filter, or back to the Inbox with --not-junk",
				ArgsUsage: "<id>...",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.BoolFlag{Name: "not-junk", Usage: "the filter was wrong: back to the Inbox, and tell it so"},
				},
				Action: runMessageJunk,
			},
			{
				Name:  "send",
				Usage: "send a message from this mailbox",
				Description: "The text is a file; \"-\" reads it from standard input.\n\n" +
					"  teanode mailbox message send --to ann@example.org --subject 'Lunch' --text -\n" +
					"  teanode mailbox message send --reply-to <id> --to ann@example.org --subject 'Re: Lunch' --text reply.txt\n\n" +
					"--reply-to keeps the conversation together and marks that message answered; --forward carries a message's attachments along.",
				Flags:  messageFlags(true),
				Action: runMessageSend,
			},
			{
				Name:        "draft",
				Usage:       "save a message in Drafts without sending it",
				Description: "The same flags as send. --draft replaces a draft saved before, so saving again does not leave two.",
				Flags:       messageFlags(false),
				Action:      runMessageDraft,
			},
		},
	}
}

// messageFlags are what the composer holds. A send alone takes a submission
// identifier, because only a send can be repeated by accident.
func messageFlags(isSend bool) []cli.Flag {
	flags := []cli.Flag{
		JSONFlag(), mailboxFlag(),
		&cli.StringFlag{Name: "from", Usage: "the address to send as; the mailbox's only address by default"},
		&cli.StringFlag{Name: "from-name", Usage: "display name beside the address"},
		&cli.StringSliceFlag{Name: "to", Usage: "recipient, an address or \"Name <address>\"; repeatable"},
		&cli.StringSliceFlag{Name: "cc", Usage: "carbon copy; repeatable"},
		&cli.StringSliceFlag{Name: "bcc", Usage: "blind carbon copy; repeatable"},
		&cli.StringFlag{Name: "subject", Usage: "subject line"},
		&cli.StringFlag{Name: "text", Usage: "file holding the text body; \"-\" reads standard input"},
		&cli.StringFlag{Name: "html", Usage: "file holding the HTML body; \"-\" reads standard input"},
		&cli.StringFlag{Name: "reply-to", Usage: "the identifier of the message this answers"},
		&cli.StringFlag{Name: "forward", Usage: "the identifier of the message this forwards"},
		&cli.StringFlag{Name: "draft", Usage: "the identifier of the draft this is written from; it is replaced"},
	}
	if isSend {
		flags = append(flags, &cli.StringFlag{Name: "submission-id", Usage: "reuse this identifier and the unchanged message for a retry"})
	}
	return flags
}

// requireItemIDs is the messages a command acts on, from its arguments.
func requireItemIDs(command *cli.Command, usageLine string) ([]string, error) {
	itemIds := command.Args().Slice()
	if len(itemIds) == 0 {
		return nil, usage("which messages? usage: " + usageLine)
	}
	return itemIds, nil
}

func describeMessageState(item *client.MailboxItem) string {
	var states []string
	if !item.Seen {
		states = append(states, "unread")
	}
	if item.Flagged {
		states = append(states, "flagged")
	}
	if item.Draft {
		states = append(states, "draft")
	}
	if item.Answered {
		states = append(states, "answered")
	}
	return strings.Join(states, ",")
}

func describeSender(mailItem *client.MailboxMail) string {
	if mailItem.FromName != "" {
		return mailItem.FromName + " <" + mailItem.From + ">"
	}
	return mailItem.From
}

func runMessageList(ctx context.Context, command *cli.Command) error {
	if command.IsSet("folder") && command.Bool("everywhere") {
		return usage("--folder and --everywhere ask for different things; pass one")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	view, err := requireMailbox(ctx, command, connection)
	if err != nil {
		return err
	}
	options := client.ListMailboxItemsOptions{
		From: command.String("from"), To: command.String("to"), Subject: command.String("subject"),
		Search: command.String("search"), First: int(command.Int("first")),
		Offset: int(command.Int("offset")), After: command.String("after"),
	}
	switch {
	case command.Bool("everywhere"):
		options.MailboxID = view.Mailbox.ID
	case command.IsSet("folder"):
		folder, err := requireFolder(view, command.String("folder"))
		if err != nil {
			return err
		}
		options.FolderID = folder.ID
	default:
		// The Inbox is where a person looks first, and a list with no
		// folder would otherwise be every folder at once, Trash and Junk
		// included.
		for _, folder := range view.Folders {
			if folder.Kind == "inbox" {
				options.FolderID = folder.ID
			}
		}
		if options.FolderID == "" {
			return fmt.Errorf("%s has no Inbox; name a folder with --folder", view.Mailbox.Name)
		}
	}
	for name, target := range map[string]**bool{"unread": &options.Unread, "flagged": &options.Flagged, "has-attachment": &options.HasAttachment} {
		if command.Bool(name) {
			isTrue := true
			*target = &isTrue
		}
	}
	if options.Since, err = parseSince(command.String("since")); err != nil {
		return usage("--since: " + err.Error())
	}
	if options.Before, err = parseSince(command.String("before")); err != nil {
		return usage("--before: " + err.Error())
	}
	page, err := client.ListMailboxItems(ctx, connection, options)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(page)
	}
	if page == nil || len(page.Items) == 0 {
		fmt.Println("no messages")
		return nil
	}
	rows := make([][]string, 0, len(page.Items))
	for _, item := range page.Items {
		if item.Mail == nil {
			continue
		}
		rows = append(rows, []string{
			formatTime(item.Mail.ReceivedAt),
			truncate(describeSender(item.Mail), 36),
			truncate(item.Mail.Subject, 50),
			describeMessageState(item),
			item.ID,
		})
	}
	if err := printTable([]string{"RECEIVED", "FROM", "SUBJECT", "", "ID"}, rows); err != nil {
		return err
	}
	if shown := int64(options.Offset + len(page.Items)); shown < page.Total {
		fmt.Printf("\n%d of %d; --offset %d is the next page", shown, page.Total, shown)
		if options.FolderID != "" {
			fmt.Printf(", or --after %s", page.Items[len(page.Items)-1].ID)
		}
		fmt.Println()
	}
	return nil
}

// messageText is a message's own words, which are written as they are to a
// file or a pipe and with control characters escaped to a terminal, which
// would otherwise obey whatever the sender put in them.
func messageText(text string) string {
	if term.IsTerminal(int(os.Stdout.Fd())) {
		return forTerminal(text)
	}
	return text
}

// shownHeaders are the header lines worth printing above a message; the
// rest are in --json.
var shownHeaders = []string{"Date", "From", "Reply-To", "To", "Cc", "Subject"}

func runMessageShow(ctx context.Context, command *cli.Command) error {
	itemId := command.Args().First()
	if itemId == "" {
		return usage("which message? usage: teanode mailbox message show <id>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if command.Bool("thread") {
		return showThread(ctx, command, connection, itemId)
	}
	item, err := client.GetMailboxItem(ctx, connection, itemId)
	if err != nil {
		return describeNotFound(command, err, "message "+itemId)
	}
	content, err := client.GetMailContent(ctx, connection, item.MailID)
	if err != nil {
		return describeNotFound(command, err, "the text of message "+itemId)
	}
	if command.Bool("json") {
		return PrintJSON(map[string]any{"item": item, "content": content})
	}
	if err := printMessage(command, item, content); err != nil {
		return err
	}
	if directory := command.String("save-attachments"); directory != "" {
		return saveAttachments(ctx, connection, item.MailID, content, directory)
	}
	return nil
}

func printMessage(command *cli.Command, item *client.MailboxItem, content *client.MailContent) error {
	if !content.Available {
		return fmt.Errorf("the message is no longer stored; retention removed it, or it arrived before storage was configured")
	}
	fields := [][2]string{}
	for _, wanted := range shownHeaders {
		for _, header := range content.Headers {
			if strings.EqualFold(header.Key, wanted) {
				fields = append(fields, [2]string{header.Key, header.Value})
			}
		}
	}
	fields = append(fields, [2]string{"id", item.ID})
	if state := describeMessageState(item); state != "" {
		fields = append(fields, [2]string{"state", state})
	}
	if err := printFields(fields); err != nil {
		return err
	}
	body := content.Text
	if command.Bool("html") {
		if content.HTML == "" {
			return fmt.Errorf("the message has no HTML part; drop --html for the text")
		}
		body = content.HTML
	} else if body == "" && content.HTML != "" {
		return fmt.Errorf("the message has no text part; pass --html for the HTML")
	}
	fmt.Println()
	fmt.Println(messageText(body))
	if len(content.Attachments) > 0 {
		fmt.Fprintf(os.Stderr, "\n%d attachment(s):\n", len(content.Attachments))
		for _, attachment := range content.Attachments {
			fmt.Fprintf(os.Stderr, "  %d: %s (%s, %d bytes)\n", attachment.Index, forTerminal(attachment.Filename), forTerminal(attachment.ContentType), attachment.Size)
		}
		if !command.IsSet("save-attachments") {
			fmt.Fprintln(os.Stderr, "--save-attachments <directory> writes them out")
		}
	}
	return nil
}

// showThread prints every message of a conversation, oldest first, the order
// it was written in.
func showThread(ctx context.Context, command *cli.Command, connection *client.Client, itemId string) error {
	thread, err := client.GetMailboxThread(ctx, connection, itemId)
	if err != nil {
		return describeNotFound(command, err, "message "+itemId)
	}
	if thread == nil {
		return describeNotFound(command, client.ErrNotFound, "message "+itemId)
	}
	if command.Bool("json") {
		return PrintJSON(thread)
	}
	if thread.Truncated {
		fmt.Fprintln(os.Stderr, "note: this conversation is longer than can be read at once; the oldest messages are not shown")
	}
	for index := len(thread.Items) - 1; index >= 0; index-- {
		entry := thread.Items[index]
		if entry.Item == nil {
			continue
		}
		content, err := client.GetMailContent(ctx, connection, entry.Item.MailID)
		if err != nil {
			return describeNotFound(command, err, "the text of message "+entry.Item.ID)
		}
		if index != len(thread.Items)-1 {
			fmt.Printf("\n%s\n\n", strings.Repeat("-", 72))
		}
		if err := printMessage(command, entry.Item, content); err != nil {
			return err
		}
	}
	return nil
}

// saveAttachments writes a message's attachments into a directory. A name
// comes from the sender, so only its last element is used: a filename of
// "../../.profile" must not leave the directory. Nothing is overwritten.
func saveAttachments(ctx context.Context, connection *client.Client, mailId string, content *client.MailContent, directory string) error {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("cannot make %s: %w", directory, err)
	}
	for _, attachment := range content.Attachments {
		name := filepath.Base(strings.ReplaceAll(attachment.Filename, "\\", "/"))
		if name == "." || name == ".." || name == string(filepath.Separator) || name == "" {
			name = fmt.Sprintf("attachment-%d", attachment.Index)
		}
		response, err := connection.Download(ctx, api.MailAttachmentPath(mailId, attachment.Index))
		if err != nil {
			return fmt.Errorf("cannot fetch attachment %d: %w", attachment.Index, err)
		}
		path := filepath.Join(directory, name)
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			_ = response.Body.Close()
			return fmt.Errorf("cannot write %s: %w", path, err)
		}
		written, err := io.Copy(file, response.Body)
		_ = response.Body.Close()
		if closeError := file.Close(); err == nil {
			err = closeError
		}
		if err != nil {
			return fmt.Errorf("cannot write %s: %w", path, err)
		}
		fmt.Printf("wrote %s (%d bytes)\n", path, written)
	}
	return nil
}

func runMessageMove(ctx context.Context, command *cli.Command) error {
	itemIds, err := requireItemIDs(command, "teanode mailbox message move <id>... --folder <name>")
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	view, err := requireMailbox(ctx, command, connection)
	if err != nil {
		return err
	}
	folder, err := requireFolder(view, command.String("folder"))
	if err != nil {
		return err
	}
	moved, err := client.MoveMailboxItems(ctx, connection, itemIds, folder.ID)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(moved)
	}
	fmt.Printf("moved %d message(s) to %s\n", len(moved), folderPath(view, folder))
	for _, item := range moved {
		fmt.Printf("  new id %s\n", item.ID)
	}
	return nil
}

func runMessageFlag(ctx context.Context, command *cli.Command) error {
	itemIds, err := requireItemIDs(command, "teanode mailbox message flag <id>... [--read|--unread] [--flag|--unflag]")
	if err != nil {
		return err
	}
	if command.Bool("read") && command.Bool("unread") {
		return usage("--read and --unread are opposites; pass one")
	}
	if command.Bool("flag") && command.Bool("unflag") {
		return usage("--flag and --unflag are opposites; pass one")
	}
	var isSeen, isFlagged *bool
	yes, no := true, false
	switch {
	case command.Bool("read"):
		isSeen = &yes
	case command.Bool("unread"):
		isSeen = &no
	}
	switch {
	case command.Bool("flag"):
		isFlagged = &yes
	case command.Bool("unflag"):
		isFlagged = &no
	}
	if isSeen == nil && isFlagged == nil {
		return usage("what should change? pass --read, --unread, --flag or --unflag")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	changed, err := client.SetMailboxItemFlags(ctx, connection, itemIds, isSeen, isFlagged)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(map[string]int{"changedCount": changed})
	}
	fmt.Printf("changed %d message(s)\n", changed)
	return nil
}

func runMessageJunk(ctx context.Context, command *cli.Command) error {
	itemIds, err := requireItemIDs(command, "teanode mailbox message junk <id>... [--not-junk]")
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	isNotJunk := command.Bool("not-junk")
	moved, err := client.ReportMailboxJunk(ctx, connection, itemIds, isNotJunk)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(map[string]int{"movedCount": moved})
	}
	if isNotJunk {
		fmt.Printf("moved %d message(s) back to the Inbox and told the filter it was wrong\n", moved)
		return nil
	}
	fmt.Printf("moved %d message(s) to Junk and told the filter\n", moved)
	return nil
}

// messageFromFlags is the message the composer flags describe, and the
// mailbox it is written in.
func messageFromFlags(ctx context.Context, command *cli.Command, connection *client.Client) (*client.MailboxView, *client.MailboxMessageParameters, error) {
	view, err := requireMailbox(ctx, command, connection)
	if err != nil {
		return nil, nil, err
	}
	message := &client.MailboxMessageParameters{
		From:          strings.TrimSpace(command.String("from")),
		FromName:      command.String("from-name"),
		To:            command.StringSlice("to"),
		Cc:            command.StringSlice("cc"),
		Bcc:           command.StringSlice("bcc"),
		Subject:       command.String("subject"),
		ReplyToItemID: command.String("reply-to"),
		ForwardItemID: command.String("forward"),
		DraftItemID:   command.String("draft"),
	}
	for _, address := range append(append(append([]string{}, message.To...), message.Cc...), message.Bcc...) {
		if _, err := mail.ParseAddress(address); err != nil {
			return nil, nil, fmt.Errorf("%q is not an address", address)
		}
	}
	if message.From == "" {
		switch len(view.Mailbox.Addresses) {
		case 0:
			return nil, nil, fmt.Errorf("%s has no address to send as", view.Mailbox.Name)
		case 1:
			message.From = view.Mailbox.Addresses[0].Address
		default:
			addresses := make([]string, 0, len(view.Mailbox.Addresses))
			for _, address := range view.Mailbox.Addresses {
				addresses = append(addresses, address.Address)
			}
			return nil, nil, usage(fmt.Sprintf("which address? %s has %s; name one with --from", view.Mailbox.Name, strings.Join(addresses, ", ")))
		}
	}
	if file := command.String("text"); file != "" {
		content, err := readFileOrStdin(file)
		if err != nil {
			return nil, nil, err
		}
		message.TextContent = string(content)
	}
	if file := command.String("html"); file != "" {
		content, err := readFileOrStdin(file)
		if err != nil {
			return nil, nil, err
		}
		message.HTMLContent = string(content)
	}
	return view, message, nil
}

func runMessageSend(ctx context.Context, command *cli.Command) error {
	if len(command.StringSlice("to"))+len(command.StringSlice("cc"))+len(command.StringSlice("bcc")) == 0 {
		return usage("who is it for? pass --to")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	view, message, err := messageFromFlags(ctx, command, connection)
	if err != nil {
		return err
	}
	submissionId := command.String("submission-id")
	if submissionId == "" {
		submissionId = security.NewULID()
	}
	fmt.Fprintf(os.Stderr, "submission %s\n", submissionId)
	sent, err := client.SendMailboxMessage(ctx, connection, view.Mailbox.ID, submissionId, message)
	if err != nil {
		return fmt.Errorf("send failed; retry unchanged content with --submission-id %s: %w", submissionId, describeError(command, err))
	}
	if command.Bool("json") {
		return PrintJSON(sent)
	}
	recipients := strings.Join(append(append(append([]string{}, message.To...), message.Cc...), message.Bcc...), ", ")
	if sent.Item != nil {
		fmt.Printf("sent to %s; the copy in Sent is %s\n", recipients, sent.Item.ID)
		return nil
	}
	fmt.Printf("sent to %s\n", recipients)
	return nil
}

func runMessageDraft(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	view, message, err := messageFromFlags(ctx, command, connection)
	if err != nil {
		return err
	}
	saved, err := client.SaveMailboxDraft(ctx, connection, view.Mailbox.ID, message)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(saved)
	}
	fmt.Printf("saved in Drafts as %s; pass --draft %s to change it\n", saved.ID, saved.ID)
	return nil
}

// --- invitations ------------------------------------------------------------

func newInvitationCommand() *cli.Command {
	return &cli.Command{
		Name:    "invitation",
		Aliases: []string{"invitations"},
		Usage:   "calendar invitations that arrived as mail: read one and say whether you are coming",
		Commands: []*cli.Command{
			{
				Name:      "show",
				Usage:     "what a message invited you to, who else is asked, and what you have answered",
				ArgsUsage: "<message-id>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runInvitationShow,
			},
			{
				Name:  "answer",
				Usage: "say accept, tentative or decline; the answer is mailed to whoever asked",
				Description: "Needs the right to send mail and to use the calendar, as the dashboard's buttons do.\n\n" +
					"  teanode mailbox invitation answer <message-id> accept",
				ArgsUsage: "<message-id> <accept|tentative|decline>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runInvitationAnswer,
			},
		},
	}
}

// invitationAnswers are the words a person types and what the API takes.
var invitationAnswers = map[string]string{
	"accept": "ACCEPTED", "accepted": "ACCEPTED",
	"tentative": "TENTATIVE",
	"decline":   "DECLINED", "declined": "DECLINED",
}

func runInvitationShow(ctx context.Context, command *cli.Command) error {
	itemId := command.Args().First()
	if itemId == "" {
		return usage("which message? usage: teanode mailbox invitation show <message-id>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	invitation, err := client.GetMailInvitation(ctx, connection, itemId)
	if err != nil {
		return describeNotFound(command, err, "message "+itemId)
	}
	if command.Bool("json") {
		return PrintJSON(invitation)
	}
	if invitation == nil {
		fmt.Println("this message carried no calendar invitation")
		return nil
	}
	return printInvitation(invitation)
}

func printInvitation(invitation *client.MailInvitation) error {
	fields := [][2]string{
		{"what", invitation.Summary},
		{"kind", strings.ToLower(invitation.Method)},
		{"where", invitation.Location},
		{"starts", invitation.StartsAt},
		{"ends", invitation.EndsAt},
		{"organizer", invitation.Organizer},
		{"your answer", strings.ToLower(invitation.Participation)},
	}
	if invitation.IsAllDay {
		fields = append(fields, [2]string{"all day", "yes"})
	}
	if invitation.IsCancelled {
		fields = append(fields, [2]string{"cancelled", "yes"})
	}
	if invitation.Because != "" {
		fields = append(fields, [2]string{"nothing done because", invitation.Because})
	}
	for index, attendee := range invitation.Attendees {
		name := "asked"
		if index > 0 {
			name = ""
		}
		line := attendee.Address
		if attendee.Name != "" {
			line = attendee.Name + " <" + attendee.Address + ">"
		}
		fields = append(fields, [2]string{name, line + " (" + strings.ToLower(attendee.Participation) + ")"})
	}
	kept := fields[:0]
	for _, field := range fields {
		if field[1] != "" {
			kept = append(kept, field)
		}
	}
	return printFields(kept)
}

func runInvitationAnswer(ctx context.Context, command *cli.Command) error {
	itemId, said := command.Args().Get(0), strings.ToLower(command.Args().Get(1))
	if itemId == "" || said == "" {
		return usage("usage: teanode mailbox invitation answer <message-id> <accept|tentative|decline>")
	}
	answer, isKnown := invitationAnswers[said]
	if !isKnown {
		return usage(fmt.Sprintf("%q is not an answer; say accept, tentative or decline", said))
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	invitation, err := client.AnswerMailInvitation(ctx, connection, itemId, answer)
	if err != nil {
		return describeNotFound(command, err, "an invitation on message "+itemId)
	}
	if command.Bool("json") {
		return PrintJSON(invitation)
	}
	fmt.Printf("answered %s; the organizer has been told\n", strings.ToLower(answer))
	return nil
}
