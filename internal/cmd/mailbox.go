package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// NewMailboxCommand builds "teanode mailbox": a person's own mail, as the
// dashboard shows it. The folders it is filed into, the rules that file it,
// the contacts it has learned, the app passwords a mail program signs in
// with, and the out-of-office reply.
//
// Every subcommand acts on one mailbox. Most people have one, so --mailbox
// is needed only when there are several; it takes the name or the identifier.
func NewMailboxCommand() *cli.Command {
	return &cli.Command{
		Name:  "mailbox",
		Usage: "mailboxes: folders, rules, contacts, subscriptions, devices, out of office",
		Commands: []*cli.Command{
			{
				Name:   "list",
				Usage:  "list the mailboxes you can open",
				Flags:  []cli.Flag{JSONFlag(), &cli.BoolFlag{Name: "all", Usage: "every mailbox on the server, with its owner; needs domain:manage or user:manage"}},
				Action: runMailboxList,
			},
			{
				Name:   "show",
				Usage:  "a mailbox with its addresses, folders and rules",
				Flags:  []cli.Flag{JSONFlag(), mailboxFlag()},
				Action: runMailboxShow,
			},
			{
				Name:  "update",
				Usage: "change a mailbox's name or signature",
				Flags: []cli.Flag{
					JSONFlag(), mailboxFlag(),
					&cli.StringFlag{Name: "name", Usage: "what this mailbox is called in the switcher"},
					&cli.StringFlag{Name: "signature", Usage: "the plain text signature; \"-\" reads it from standard input"},
					&cli.StringFlag{Name: "signature-html", Usage: "the HTML signature; \"-\" reads it from standard input"},
				},
				Action: runMailboxUpdate,
			},
			newFolderCommand(),
			newRuleCommand(),
			newContactCommand(),
			newDeviceCommand(),
			newAutoReplyCommand(),
			newSubscriptionCommand(),
			{
				Name:   "programs",
				Usage:  "the hosts and ports a mail program connects to",
				Flags:  []cli.Flag{JSONFlag()},
				Action: runMailboxPrograms,
			},
		},
	}
}

func mailboxFlag() cli.Flag {
	return &cli.StringFlag{
		Name:  "mailbox",
		Usage: "which mailbox, by name or identifier; needed only when you have more than one",
	}
}

// requireMailbox is the mailbox a command acts on: the one named, or the
// only one there is. Somebody with several and no --mailbox is shown the
// names rather than a guess.
func requireMailbox(ctx context.Context, command *cli.Command, connection *client.Client) (*client.MailboxView, error) {
	views, err := client.ListMailboxes(ctx, connection)
	if err != nil {
		return nil, describeError(command, err)
	}
	if len(views) == 0 {
		// The console is not an account, so it owns no mailbox; saying "this
		// account has no mailbox" there reads as though something is broken.
		if connection.URL() == "" || strings.HasPrefix(connection.URL(), "http://127.0.0.1") {
			return nil, fmt.Errorf("a mailbox belongs to an account, and the console is not one; " +
				"sign in with 'teanode auth login --url https://…' to reach yours")
		}
		return nil, fmt.Errorf("this account has no mailbox")
	}
	wanted := strings.TrimSpace(command.String("mailbox"))
	if wanted == "" {
		if len(views) == 1 {
			return views[0], nil
		}
		names := make([]string, 0, len(views))
		for _, view := range views {
			names = append(names, view.Mailbox.Name)
		}
		return nil, usage(fmt.Sprintf("which mailbox? this account has %s; name one with --mailbox", strings.Join(names, ", ")))
	}
	for _, view := range views {
		if view.Mailbox.ID == wanted || strings.EqualFold(view.Mailbox.Name, wanted) {
			return view, nil
		}
	}
	return nil, fmt.Errorf("no mailbox called %q", wanted)
}

// requireFolder is the folder a command acts on, by identifier or by name.
// Two folders may share a name under different parents, and then both are
// shown rather than one picked.
func requireFolder(view *client.MailboxView, wanted string) (*client.MailboxFolder, error) {
	var matches []*client.MailboxFolder
	for _, folder := range view.Folders {
		if folder.ID == wanted {
			return folder, nil
		}
		if strings.EqualFold(folder.Name, wanted) {
			matches = append(matches, folder)
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("no folder called %q in %s", wanted, view.Mailbox.Name)
	case 1:
		return matches[0], nil
	default:
		ids := make([]string, 0, len(matches))
		for _, folder := range matches {
			ids = append(ids, folder.ID)
		}
		return nil, fmt.Errorf("%s names %d folders; use one of these identifiers: %s", wanted, len(matches), strings.Join(ids, ", "))
	}
}

// folderPath is a folder's name with its parents' names before it, so a list
// of folders reads as a tree without drawing one.
func folderPath(view *client.MailboxView, folder *client.MailboxFolder) string {
	byId := map[string]*client.MailboxFolder{}
	for _, candidate := range view.Folders {
		byId[candidate.ID] = candidate
	}
	parts := []string{folder.Name}
	for parent := folder.ParentID; parent != ""; {
		found, ok := byId[parent]
		if !ok || len(parts) > 16 {
			break
		}
		parts = append([]string{found.Name}, parts...)
		parent = found.ParentID
	}
	return strings.Join(parts, "/")
}

func runMailboxList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if command.Bool("all") {
		mailboxes, err := client.ListAllMailboxes(ctx, connection)
		if err != nil {
			return describeError(command, err)
		}
		if command.Bool("json") {
			return PrintJSON(mailboxes)
		}
		if len(mailboxes) == 0 {
			fmt.Println("no mailboxes on this server")
			return nil
		}
		rows := make([][]string, 0, len(mailboxes))
		for _, mailbox := range mailboxes {
			rows = append(rows, []string{mailbox.Name, mailbox.Username, mailbox.ID})
		}
		return printTable([]string{"NAME", "OWNER", "ID"}, rows)
	}

	views, err := client.ListMailboxes(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(views)
	}
	if len(views) == 0 {
		fmt.Println("this account has no mailbox")
		return nil
	}
	rows := make([][]string, 0, len(views))
	for _, view := range views {
		addresses := make([]string, 0, len(view.Mailbox.Addresses))
		for _, address := range view.Mailbox.Addresses {
			addresses = append(addresses, address.Address)
		}
		rows = append(rows, []string{
			view.Mailbox.Name,
			strings.Join(addresses, ", "),
			itoa(view.Unread),
			itoa(len(view.Folders)),
			view.Mailbox.ID,
		})
	}
	return printTable([]string{"NAME", "ADDRESSES", "UNREAD", "FOLDERS", "ID"}, rows)
}

func runMailboxShow(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	view, err := requireMailbox(ctx, command, connection)
	if err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(view)
	}
	addresses := make([]string, 0, len(view.Mailbox.Addresses))
	for _, address := range view.Mailbox.Addresses {
		addresses = append(addresses, address.Address)
	}
	fields := [][2]string{
		{"name", view.Mailbox.Name},
		{"id", view.Mailbox.ID},
		{"addresses", strings.Join(addresses, ", ")},
		{"unread", itoa(view.Unread)},
		{"folders", itoa(len(view.Folders))},
		{"rules", itoa(len(view.Mailbox.Rules))},
	}
	if view.Mailbox.AutoReply != nil && view.Mailbox.AutoReply.Enabled {
		fields = append(fields, [2]string{"out of office", "on"})
	}
	if len(addresses) == 0 {
		fields = append(fields, [2]string{"", "no address yet: add an alias of kind mailbox pointing at this mailbox"})
	}
	return printFields(fields)
}

func runMailboxUpdate(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	view, err := requireMailbox(ctx, command, connection)
	if err != nil {
		return err
	}
	parameters := &client.MailboxParameters{}
	if command.IsSet("name") {
		value := command.String("name")
		parameters.Name = &value
	}
	if command.IsSet("signature") {
		value, err := readValueOrStandardInput(command.String("signature"))
		if err != nil {
			return err
		}
		parameters.SignatureText = &value
	}
	if command.IsSet("signature-html") {
		value, err := readValueOrStandardInput(command.String("signature-html"))
		if err != nil {
			return err
		}
		parameters.SignatureHTML = &value
	}
	if parameters.Name == nil && parameters.SignatureText == nil && parameters.SignatureHTML == nil {
		return usage("nothing to change; pass --name, --signature or --signature-html")
	}
	updated, err := client.UpdateMailbox(ctx, connection, view.Mailbox.ID, parameters)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(updated)
	}
	fmt.Printf("updated %s\n", updated.Mailbox.Name)
	return nil
}

func runMailboxPrograms(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	settings, err := client.GetMailProgramSettings(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(settings)
	}
	fields := [][2]string{{"host", settings.IMAPHost}}
	if settings.IMAPSPort > 0 {
		fields = append(fields, [2]string{"imap (tls)", itoa(settings.IMAPSPort)})
	}
	if settings.IMAPPort > 0 {
		fields = append(fields, [2]string{"imap (starttls)", itoa(settings.IMAPPort)})
	}
	if settings.IMAPSPort == 0 && settings.IMAPPort == 0 {
		fields = append(fields, [2]string{"imap", "not listening; set listen.imaps to turn it on"})
	}
	fields = append(fields,
		[2]string{"submission host", settings.SubmissionHost},
		[2]string{"submission port", itoa(settings.SubmissionPort)},
		[2]string{"", "the username is one of the mailbox's addresses, and the password is an app password"},
	)
	return printFields(fields)
}

// --- folders ----------------------------------------------------------------

func newFolderCommand() *cli.Command {
	return &cli.Command{
		Name:  "folder",
		Usage: "the folders of a mailbox",
		Commands: []*cli.Command{
			{
				Name:   "list",
				Usage:  "list a mailbox's folders, in the order the rail shows them",
				Flags:  []cli.Flag{JSONFlag(), mailboxFlag()},
				Action: runFolderList,
			},
			{
				Name:      "create",
				Aliases:   []string{"add"},
				Usage:     "add a folder, at the top level or inside another",
				ArgsUsage: "<name>",
				Flags: []cli.Flag{
					JSONFlag(), mailboxFlag(),
					&cli.StringFlag{Name: "inside", Usage: "the folder to put it in, by name or identifier"},
				},
				Action: runFolderCreate,
			},
			{
				Name:      "rename",
				Usage:     "rename a folder you made",
				ArgsUsage: "<folder> <new-name>",
				Flags:     []cli.Flag{JSONFlag(), mailboxFlag()},
				Action:    runFolderRename,
			},
			{
				Name:      "move",
				Usage:     "move a folder you made inside another, or to the top level",
				ArgsUsage: "<folder>",
				Flags: []cli.Flag{
					JSONFlag(), mailboxFlag(),
					&cli.StringFlag{Name: "inside", Usage: "the folder to put it in; leave it out for the top level"},
				},
				Action: runFolderMove,
			},
			{
				Name:      "pin",
				Usage:     "pin a folder to the top of the rail, beside the Inbox and Starred",
				ArgsUsage: "<folder>",
				Flags:     []cli.Flag{JSONFlag(), mailboxFlag()},
				Action:    runFolderPin,
			},
			{
				Name:      "unpin",
				Usage:     "take a folder down from the top of the rail",
				ArgsUsage: "<folder>",
				Flags:     []cli.Flag{JSONFlag(), mailboxFlag()},
				Action:    runFolderUnpin,
			},
			{
				Name:      "delete",
				Aliases:   []string{"remove"},
				Usage:     "remove a folder you made, and everything in it",
				ArgsUsage: "<folder>",
				Flags:     []cli.Flag{ForceFlag(), mailboxFlag()},
				Action:    runFolderDelete,
			},
		},
	}
}

func runFolderList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	view, err := requireMailbox(ctx, command, connection)
	if err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(view.Folders)
	}
	rows := make([][]string, 0, len(view.Folders))
	for _, folder := range view.Folders {
		kind := folder.Kind
		if kind == "" {
			kind = "custom"
		}
		pinned := ""
		if folder.PinnedAt != nil {
			pinned = "pinned"
		}
		rows = append(rows, []string{folderPath(view, folder), kind, itoa(folder.Total), itoa(folder.Unread), pinned, folder.ID})
	}
	return printTable([]string{"FOLDER", "KIND", "TOTAL", "UNREAD", "", "ID"}, rows)
}

func runFolderCreate(ctx context.Context, command *cli.Command) error {
	name := command.Args().First()
	if name == "" {
		return usage("what is it called? usage: teanode mailbox folder create <name> [--inside <folder>]")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	view, err := requireMailbox(ctx, command, connection)
	if err != nil {
		return err
	}
	parentId := ""
	if inside := command.String("inside"); inside != "" {
		parent, err := requireFolder(view, inside)
		if err != nil {
			return err
		}
		parentId = parent.ID
	}
	folder, err := client.CreateMailboxFolder(ctx, connection, view.Mailbox.ID, name, parentId)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(folder)
	}
	fmt.Printf("added %s\n", folder.Name)
	return nil
}

func runFolderRename(ctx context.Context, command *cli.Command) error {
	wanted, newName := command.Args().First(), command.Args().Get(1)
	if wanted == "" || newName == "" {
		return usage("usage: teanode mailbox folder rename <folder> <new-name>")
	}
	connection, _, folder, err := openFolder(ctx, command, wanted)
	if err != nil {
		return err
	}
	updated, err := client.UpdateMailboxFolder(ctx, connection, folder.ID, &newName, nil)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(updated)
	}
	fmt.Printf("renamed %s to %s\n", folder.Name, updated.Name)
	return nil
}

func runFolderMove(ctx context.Context, command *cli.Command) error {
	wanted := command.Args().First()
	if wanted == "" {
		return usage("which folder? usage: teanode mailbox folder move <folder> [--inside <folder>]")
	}
	connection, view, folder, err := openFolder(ctx, command, wanted)
	if err != nil {
		return err
	}
	parentId := ""
	if inside := command.String("inside"); inside != "" {
		parent, err := requireFolder(view, inside)
		if err != nil {
			return err
		}
		if parent.ID == folder.ID {
			return fmt.Errorf("a folder cannot go inside itself")
		}
		parentId = parent.ID
	}
	updated, err := client.UpdateMailboxFolder(ctx, connection, folder.ID, nil, &parentId)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(updated)
	}
	if parentId == "" {
		fmt.Printf("moved %s to the top level\n", updated.Name)
	} else {
		fmt.Printf("moved %s inside %s\n", updated.Name, command.String("inside"))
	}
	return nil
}

func runFolderPin(ctx context.Context, command *cli.Command) error {
	return setFolderPinned(ctx, command, true)
}

func runFolderUnpin(ctx context.Context, command *cli.Command) error {
	return setFolderPinned(ctx, command, false)
}

func setFolderPinned(ctx context.Context, command *cli.Command, pinned bool) error {
	wanted := command.Args().First()
	if wanted == "" {
		return usage("which folder? usage: teanode mailbox folder pin <folder>")
	}
	connection, _, folder, err := openFolder(ctx, command, wanted)
	if err != nil {
		return err
	}
	updated, err := client.SetMailboxFolderPinned(ctx, connection, folder.ID, pinned)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(updated)
	}
	if pinned {
		fmt.Printf("pinned %s to the top of the rail\n", updated.Name)
	} else {
		fmt.Printf("unpinned %s\n", updated.Name)
	}
	return nil
}

func runFolderDelete(ctx context.Context, command *cli.Command) error {
	wanted := command.Args().First()
	if wanted == "" {
		return usage("which folder? usage: teanode mailbox folder delete <folder>")
	}
	connection, view, folder, err := openFolder(ctx, command, wanted)
	if err != nil {
		return err
	}
	if folder.Kind != "" {
		return fmt.Errorf("%s is one of the folders every mailbox has and cannot be removed", folder.Name)
	}
	// What goes with it: the folders inside it, however deep, and their
	// mail. The row's own count is only its own messages, and a folder that
	// holds nothing directly can still hold thousands.
	inside, messages := insideFolder(view, folder)
	what := fmt.Sprintf("the folder %s of %s and the %d message(s) in it",
		folderPath(view, folder), view.Mailbox.Name, messages)
	if inside > 0 {
		what = fmt.Sprintf("the folder %s of %s, the %d folder(s) inside it, and the %d message(s) they hold",
			folderPath(view, folder), view.Mailbox.Name, inside, messages)
	}
	// Said even when nothing asks, so that a script's log records what went.
	fmt.Printf("removing %s\n", what)
	if err := confirm(command, fmt.Sprintf("This removes %s. They do not go to Trash.", what)); err != nil {
		return err
	}
	if err := client.DeleteMailboxFolder(ctx, connection, folder.ID); err != nil {
		return describeError(command, err)
	}
	fmt.Printf("removed %s\n", folder.Name)
	return nil
}

// insideFolder is how many folders are inside one, however deep, and how
// many messages they and it hold between them.
func insideFolder(view *client.MailboxView, folder *client.MailboxFolder) (folders, messages int) {
	messages = folder.Total
	within := map[string]bool{folder.ID: true}
	// The tree is small and the list is in parent-before-child order, but a
	// pass per level costs nothing and does not rely on that.
	for range view.Folders {
		for _, candidate := range view.Folders {
			if candidate.ParentID != "" && within[candidate.ParentID] && !within[candidate.ID] {
				within[candidate.ID] = true
				folders++
				messages += candidate.Total
			}
		}
	}
	return folders, messages
}

// openFolder is the connection, the mailbox and the folder a folder command
// names: the three things every one of them needs.
func openFolder(ctx context.Context, command *cli.Command, wanted string) (*client.Client, *client.MailboxView, *client.MailboxFolder, error) {
	connection, err := openClient(command)
	if err != nil {
		return nil, nil, nil, err
	}
	view, err := requireMailbox(ctx, command, connection)
	if err != nil {
		return nil, nil, nil, err
	}
	folder, err := requireFolder(view, wanted)
	if err != nil {
		return nil, nil, nil, err
	}
	return connection, view, folder, nil
}

// --- rules ------------------------------------------------------------------

func newRuleCommand() *cli.Command {
	return &cli.Command{
		Name:  "rule",
		Usage: "the rules that file arriving mail",
		Commands: []*cli.Command{
			{
				Name:  "list",
				Usage: "list a mailbox's rules, in the order they run",
				Description: "A mailbox's rules are stored as one list, so adding, removing or\n" +
					"turning one on reads the list, changes it, and writes it back. Two of\n" +
					"these at the same moment, or one of them beside somebody editing the\n" +
					"rules in the dashboard, keeps only the last to be written.",
				Flags:  []cli.Flag{JSONFlag(), mailboxFlag()},
				Action: runRuleList,
			},
			{
				Name:      "add",
				Aliases:   []string{"create"},
				Usage:     "add a rule",
				ArgsUsage: "<name>",
				Description: "Conditions are --when field:operator:value, repeated; every one must match.\n" +
					"The fields are from, to, subject, header, score, sender-known, category, priority,\n" +
					"needs-reply and any; the operators are contains, equals, matches (a regular\n" +
					"expression), above and below. A header condition names the header:\n" +
					"--when header:List-Id:contains:golang. sender-known, needs-reply and any take no\n" +
					"operator. category, priority and needs-reply read what the agent decided about a\n" +
					"message, so such a rule runs once the agent has sorted it rather than at delivery.\n\n" +
					"  teanode mailbox rule add GitHub --when from:contains:@github.com --move GitHub --stop\n" +
					"  teanode mailbox rule add Loud --when score:above:5 --move Junk\n" +
					"  teanode mailbox rule add Receipts --when subject:contains:receipt --move Receipts --mark-read\n" +
					"  teanode mailbox rule add Reading --when category:equals:newsletter --move Reading --mark-read",
				Flags:  append(ruleFlags(), JSONFlag(), mailboxFlag()),
				Action: runRuleAdd,
			},
			{
				Name:      "remove",
				Aliases:   []string{"delete"},
				Usage:     "remove a rule by name",
				ArgsUsage: "<name>",
				Flags:     []cli.Flag{ForceFlag(), mailboxFlag()},
				Action:    runRuleRemove,
			},
			{
				Name:      "enable",
				Usage:     "turn a rule on",
				ArgsUsage: "<name>",
				Flags:     []cli.Flag{JSONFlag(), mailboxFlag()},
				Action:    runRuleEnable,
			},
			{
				Name:      "disable",
				Usage:     "turn a rule off without removing it",
				ArgsUsage: "<name>",
				Flags:     []cli.Flag{JSONFlag(), mailboxFlag()},
				Action:    runRuleDisable,
			},
			{
				Name:  "test",
				Usage: "say which of the newest messages the rules would match, without changing anything",
				Flags: []cli.Flag{
					JSONFlag(), mailboxFlag(),
					&cli.StringFlag{Name: "folder", Usage: "which folder to try them against; the Inbox by default"},
					&cli.IntFlag{Name: "first", Value: 20, Usage: "how many of the newest messages, at most 100"},
				},
				Action: runRuleTest,
			},
			{
				Name:  "apply",
				Usage: "run the rules over the mail already in a folder",
				Description: "A rule written today does not reach into yesterday's Inbox by itself.\n" +
					"This runs the stored rules over what is already there, moving, marking,\n" +
					"flagging and deleting as arrival would have. Forwarding is not repeated:\n" +
					"old mail is not sent again.",
				Flags: []cli.Flag{
					JSONFlag(), mailboxFlag(), ForceFlag(),
					&cli.StringFlag{Name: "folder", Usage: "which folder to run over; the Inbox by default"},
					&cli.IntFlag{Name: "first", Value: 200, Usage: "how many of the newest messages, at most 500"},
				},
				Action: runRuleApply,
			},
		},
	}
}

func ruleFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringSliceFlag{Name: "when", Usage: "a condition, field:operator:value; repeatable, and every one must match"},
		&cli.StringFlag{Name: "move", Usage: "move the message to this folder"},
		&cli.BoolFlag{Name: "mark-read", Usage: "mark the message read"},
		&cli.BoolFlag{Name: "flag", Usage: "flag the message"},
		&cli.StringFlag{Name: "forward", Usage: "forward the message to this address"},
		&cli.BoolFlag{Name: "delete", Usage: "move the message to Trash"},
		&cli.BoolFlag{Name: "stop", Usage: "run no later rule once this one matches"},
		&cli.BoolFlag{Name: "disabled", Usage: "add the rule turned off"},
	}
}

// parseCondition reads one --when. A condition is field:operator:value, and
// a header condition names the header between the two:
// header:List-Id:contains:golang. The fields that ask nothing of a value —
// sender-known, needs-reply, any — are written alone.
func parseCondition(specification string) (client.MailboxRuleCondition, error) {
	parts := strings.Split(specification, ":")
	field := strings.TrimSpace(parts[0])
	switch field {
	case "sender-known", "needs-reply", "any":
		if len(parts) != 1 {
			return client.MailboxRuleCondition{}, fmt.Errorf("%q takes nothing after it: write --when %s", field, field)
		}
		return client.MailboxRuleCondition{Field: field}, nil
	case "from", "to", "subject", "score", "category", "priority":
		if len(parts) < 3 {
			return client.MailboxRuleCondition{}, fmt.Errorf("%q is not a condition; write field:operator:value, as in from:contains:@github.com", specification)
		}
		condition := client.MailboxRuleCondition{
			Field:    field,
			Operator: strings.TrimSpace(parts[1]),
			Value:    strings.Join(parts[2:], ":"),
		}
		return condition, validateOperator(condition)
	case "header":
		if len(parts) < 4 {
			return client.MailboxRuleCondition{}, fmt.Errorf("%q is not a header condition; write header:Name:operator:value, as in header:List-Id:contains:golang", specification)
		}
		condition := client.MailboxRuleCondition{
			Field:    field,
			Header:   strings.TrimSpace(parts[1]),
			Operator: strings.TrimSpace(parts[2]),
			Value:    strings.Join(parts[3:], ":"),
		}
		return condition, validateOperator(condition)
	default:
		return client.MailboxRuleCondition{}, fmt.Errorf("%q is not a field; use from, to, subject, header, score, sender-known, category, priority, needs-reply or any", field)
	}
}

func validateOperator(condition client.MailboxRuleCondition) error {
	switch condition.Operator {
	case "contains", "equals", "matches":
		// A score is a number, and text operators never match one.
		if condition.Field == "score" {
			return fmt.Errorf("a score is compared with above or below, not %q", condition.Operator)
		}
		return nil
	case "above", "below":
		if condition.Field != "score" {
			return fmt.Errorf("%q compares numbers, and %s is text: use contains, equals or matches",
				condition.Operator, condition.Field)
		}
		return nil
	default:
		return fmt.Errorf("%q is not an operator; use contains, equals, matches, above or below", condition.Operator)
	}
}

// ruleFromFlags reads a rule out of the flags given.
func ruleFromFlags(command *cli.Command, name string, view *client.MailboxView) (*client.MailboxRule, error) {
	rule := &client.MailboxRule{
		Name:       name,
		Enabled:    !command.Bool("disabled"),
		Stop:       command.Bool("stop"),
		Conditions: []client.MailboxRuleCondition{},
		Actions:    []client.MailboxRuleAction{},
	}
	for _, specification := range command.StringSlice("when") {
		condition, err := parseCondition(specification)
		if err != nil {
			return nil, err
		}
		rule.Conditions = append(rule.Conditions, condition)
	}
	if len(rule.Conditions) == 0 {
		return nil, usage("a rule needs at least one condition: --when from:contains:@github.com")
	}
	if folder := command.String("move"); folder != "" {
		target, err := requireFolder(view, folder)
		if err != nil {
			return nil, err
		}
		rule.Actions = append(rule.Actions, client.MailboxRuleAction{Kind: "move", FolderID: target.ID})
	}
	if command.Bool("mark-read") {
		rule.Actions = append(rule.Actions, client.MailboxRuleAction{Kind: "markRead"})
	}
	if command.Bool("flag") {
		rule.Actions = append(rule.Actions, client.MailboxRuleAction{Kind: "flag"})
	}
	if address := command.String("forward"); address != "" {
		rule.Actions = append(rule.Actions, client.MailboxRuleAction{Kind: "forward", Address: address})
	}
	if command.Bool("delete") {
		rule.Actions = append(rule.Actions, client.MailboxRuleAction{Kind: "delete"})
	}
	if len(rule.Actions) == 0 {
		return nil, usage("a rule needs at least one action: --move, --mark-read, --flag, --forward or --delete")
	}
	return rule, nil
}

// describeRule is a rule as one line: what it looks for and what it does.
func describeRule(view *client.MailboxView, rule client.MailboxRule) (string, string) {
	folders := map[string]string{}
	for _, folder := range view.Folders {
		folders[folder.ID] = folder.Name
	}
	conditions := make([]string, 0, len(rule.Conditions))
	for _, condition := range rule.Conditions {
		switch condition.Field {
		case "sender-known", "needs-reply", "any":
			conditions = append(conditions, condition.Field)
		case "header":
			conditions = append(conditions, fmt.Sprintf("header %s %s %s", condition.Header, condition.Operator, condition.Value))
		default:
			conditions = append(conditions, fmt.Sprintf("%s %s %s", condition.Field, condition.Operator, condition.Value))
		}
	}
	actions := make([]string, 0, len(rule.Actions))
	for _, action := range rule.Actions {
		switch action.Kind {
		case "move":
			name, ok := folders[action.FolderID]
			if !ok {
				name = "a folder that is gone"
			}
			actions = append(actions, "move to "+name)
		case "markRead":
			actions = append(actions, "mark read")
		case "flag":
			actions = append(actions, "flag")
		case "forward":
			actions = append(actions, "forward to "+action.Address)
		case "delete":
			actions = append(actions, "delete")
		default:
			actions = append(actions, action.Kind)
		}
	}
	return strings.Join(conditions, " and "), strings.Join(actions, ", ")
}

func runRuleList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	view, err := requireMailbox(ctx, command, connection)
	if err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(view.Mailbox.Rules)
	}
	if len(view.Mailbox.Rules) == 0 {
		fmt.Printf("no rules; add one with 'teanode mailbox rule add <name> --when from:contains:@github.com --move <folder>'\n")
		return nil
	}
	rows := make([][]string, 0, len(view.Mailbox.Rules))
	for _, rule := range view.Mailbox.Rules {
		when, then := describeRule(view, rule)
		state := "on"
		if !rule.Enabled {
			state = "off"
		}
		if rule.Stop {
			state += ", stops"
		}
		rows = append(rows, []string{rule.Name, state, when, then})
	}
	return printTable([]string{"NAME", "STATE", "WHEN", "THEN"}, rows)
}

func runRuleAdd(ctx context.Context, command *cli.Command) error {
	name := command.Args().First()
	if name == "" {
		return usage("what is the rule called? usage: teanode mailbox rule add <name> --when ... --move ...")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	view, err := requireMailbox(ctx, command, connection)
	if err != nil {
		return err
	}
	for _, existing := range view.Mailbox.Rules {
		if strings.EqualFold(existing.Name, name) {
			return fmt.Errorf("there is already a rule called %q; remove it first, or give this one another name", existing.Name)
		}
	}
	rule, err := ruleFromFlags(command, name, view)
	if err != nil {
		return err
	}
	rules := append(append([]client.MailboxRule{}, view.Mailbox.Rules...), *rule)
	updated, err := client.UpdateMailbox(ctx, connection, view.Mailbox.ID, &client.MailboxParameters{Rules: &rules})
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(updated.Mailbox.Rules)
	}
	when, then := describeRule(updated, *rule)
	fmt.Printf("added %s: when %s, %s\n", rule.Name, when, then)
	fmt.Printf("\nIt files mail that arrives from now on. For mail already in the mailbox:\n")
	fmt.Printf("  teanode mailbox rule apply\n")
	return nil
}

// changeRules reads the mailbox, changes the one rule named, and writes the
// list back: the server keeps rules as one list, so there is nothing finer
// to send.
func changeRules(ctx context.Context, command *cli.Command, name string, change func(*client.MailboxRule) error) (*client.MailboxView, error) {
	connection, err := openClient(command)
	if err != nil {
		return nil, err
	}
	view, err := requireMailbox(ctx, command, connection)
	if err != nil {
		return nil, err
	}
	rules := append([]client.MailboxRule{}, view.Mailbox.Rules...)
	found, matches := -1, 0
	for index := range rules {
		if strings.EqualFold(rules[index].Name, name) {
			matches++
			if found < 0 {
				found = index
			}
		}
	}
	if found < 0 {
		return nil, fmt.Errorf("no rule called %q in %s", name, view.Mailbox.Name)
	}
	if matches > 1 {
		// The command line refuses to add a second rule of one name, but the
		// dashboard does not, and changing whichever came first is not an
		// answer to "which one did you mean".
		return nil, fmt.Errorf("%s has %d rules called %q; rename one in the dashboard first", view.Mailbox.Name, matches, name)
	}
	if err := change(&rules[found]); err != nil {
		return nil, err
	}
	updated, err := client.UpdateMailbox(ctx, connection, view.Mailbox.ID, &client.MailboxParameters{Rules: &rules})
	if err != nil {
		return nil, describeError(command, err)
	}
	return updated, nil
}

func runRuleRemove(ctx context.Context, command *cli.Command) error {
	name := command.Args().First()
	if name == "" {
		return usage("which rule? usage: teanode mailbox rule remove <name>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	view, err := requireMailbox(ctx, command, connection)
	if err != nil {
		return err
	}
	rules := make([]client.MailboxRule, 0, len(view.Mailbox.Rules))
	removed := false
	for _, rule := range view.Mailbox.Rules {
		if strings.EqualFold(rule.Name, name) {
			removed = true
			continue
		}
		rules = append(rules, rule)
	}
	if !removed {
		return fmt.Errorf("no rule called %q in %s", name, view.Mailbox.Name)
	}
	if err := confirm(command, fmt.Sprintf("This removes the rule %s. Mail it has already filed stays where it is.", name)); err != nil {
		return err
	}
	if _, err := client.UpdateMailbox(ctx, connection, view.Mailbox.ID, &client.MailboxParameters{Rules: &rules}); err != nil {
		return describeError(command, err)
	}
	fmt.Printf("removed %s\n", name)
	return nil
}

func runRuleEnable(ctx context.Context, command *cli.Command) error {
	return setRuleEnabled(ctx, command, true)
}

func runRuleDisable(ctx context.Context, command *cli.Command) error {
	return setRuleEnabled(ctx, command, false)
}

func setRuleEnabled(ctx context.Context, command *cli.Command, enabled bool) error {
	name := command.Args().First()
	if name == "" {
		return usage("which rule? usage: teanode mailbox rule enable <name>")
	}
	updated, err := changeRules(ctx, command, name, func(rule *client.MailboxRule) error {
		rule.Enabled = enabled
		return nil
	})
	if err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(updated.Mailbox.Rules)
	}
	if enabled {
		fmt.Printf("turned %s on\n", name)
	} else {
		fmt.Printf("turned %s off\n", name)
	}
	return nil
}

func runRuleTest(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	view, err := requireMailbox(ctx, command, connection)
	if err != nil {
		return err
	}
	if len(view.Mailbox.Rules) == 0 {
		fmt.Println("no rules to try")
		return nil
	}
	folderId := ""
	if wanted := command.String("folder"); wanted != "" {
		folder, err := requireFolder(view, wanted)
		if err != nil {
			return err
		}
		folderId = folder.ID
	}
	trials, err := client.TestMailboxRules(ctx, connection, view.Mailbox.ID, folderId, int(command.Int("first")), view.Mailbox.Rules)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(trials)
	}
	rows := make([][]string, 0, len(trials))
	matched := 0
	for _, trial := range trials {
		names := make([]string, 0, len(trial.Matched))
		for _, index := range trial.Matched {
			if index >= 0 && index < len(view.Mailbox.Rules) {
				names = append(names, view.Mailbox.Rules[index].Name)
			}
		}
		if len(names) > 0 {
			matched++
		}
		sender, subject := "", ""
		if trial.Item != nil && trial.Item.Mail != nil {
			sender = trial.Item.Mail.From
			subject = trial.Item.Mail.Subject
		}
		rows = append(rows, []string{truncate(sender, 32), truncate(subject, 40), strings.Join(names, ", ")})
	}
	if err := printTable([]string{"FROM", "SUBJECT", "RULES"}, rows); err != nil {
		return err
	}
	fmt.Printf("\n%d of %d message(s) match. Nothing was changed; 'teanode mailbox rule apply' files them.\n", matched, len(trials))
	return nil
}

func runRuleApply(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	view, err := requireMailbox(ctx, command, connection)
	if err != nil {
		return err
	}
	if len(view.Mailbox.Rules) == 0 {
		fmt.Println("no rules to apply")
		return nil
	}
	folderId, folderName := "", "the Inbox"
	if wanted := command.String("folder"); wanted != "" {
		folder, err := requireFolder(view, wanted)
		if err != nil {
			return err
		}
		folderId, folderName = folder.ID, folder.Name
	}
	// What the rules actually do, rather than a general "moving and
	// marking": one of them may delete.
	kinds := map[string]bool{}
	enabled := 0
	for _, rule := range view.Mailbox.Rules {
		if !rule.Enabled {
			continue
		}
		enabled++
		for _, action := range rule.Actions {
			kinds[action.Kind] = true
		}
	}
	does := []string{}
	for _, kind := range []string{"move", "markRead", "flag", "delete"} {
		if !kinds[kind] {
			continue
		}
		does = append(does, map[string]string{
			"move": "moving it", "markRead": "marking it read", "flag": "flagging it", "delete": "moving it to Trash",
		}[kind])
	}
	what := strings.Join(does, ", ")
	if what == "" {
		what = "changing nothing but what the rules say"
	}
	if err := confirm(command, fmt.Sprintf("This runs %d rule(s) over the mail already in %s of %s: %s.",
		enabled, folderName, view.Mailbox.Name, what)); err != nil {
		return err
	}
	applied, err := client.ApplyMailboxRules(ctx, connection, view.Mailbox.ID, folderId, int(command.Int("first")))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(applied)
	}
	fields := [][2]string{
		{"considered", itoa(applied.Considered)},
		{"matched", itoa(applied.Matched)},
		{"moved", itoa(applied.Moved)},
		{"marked read", itoa(applied.Marked)},
		{"flagged", itoa(applied.Flagged)},
		{"deleted", itoa(applied.Deleted)},
	}
	if applied.Skipped > 0 {
		fields = append(fields, [2]string{"not forwarded", itoa(applied.Skipped)},
			[2]string{"", "old mail is not sent again, so forward actions were left alone"})
	}
	if applied.Failed > 0 {
		fields = append(fields, [2]string{"could not be filed", itoa(applied.Failed)},
			[2]string{"", "the rest were filed; the server's log says what went wrong"})
	}
	return printFields(fields)
}

// --- contacts ---------------------------------------------------------------

func newContactCommand() *cli.Command {
	return &cli.Command{
		Name:  "contact",
		Usage: "the addresses a mailbox has corresponded with",
		Commands: []*cli.Command{
			{
				Name:  "list",
				Usage: "list a mailbox's contacts, most recent first",
				Flags: []cli.Flag{
					JSONFlag(), mailboxFlag(),
					&cli.StringFlag{Name: "prefix", Usage: "only those whose address or name begins with this"},
					&cli.IntFlag{Name: "first", Value: 50, Usage: "how many, at most 500"},
				},
				Action: runContactList,
			},
			{
				Name:      "add",
				Aliases:   []string{"create", "update"},
				Usage:     "add a contact, or rename one",
				ArgsUsage: "<address>",
				Flags: []cli.Flag{
					JSONFlag(), mailboxFlag(),
					&cli.StringFlag{Name: "name", Usage: "what to call them; leave it out to clear the name"},
				},
				Action: runContactAdd,
			},
			{
				Name:      "remove",
				Aliases:   []string{"delete"},
				Usage:     "remove a contact; it comes back if that address writes again",
				ArgsUsage: "<address>",
				Flags:     []cli.Flag{ForceFlag(), mailboxFlag()},
				Action:    runContactRemove,
			},
		},
	}
}

func runContactList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	view, err := requireMailbox(ctx, command, connection)
	if err != nil {
		return err
	}
	contacts, err := client.ListMailboxContacts(ctx, connection, view.Mailbox.ID, command.String("prefix"), int(command.Int("first")))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(contacts)
	}
	if len(contacts) == 0 {
		fmt.Println("no contacts yet; they are learned from the mail this mailbox exchanges")
		return nil
	}
	rows := make([][]string, 0, len(contacts))
	for _, contact := range contacts {
		rows = append(rows, []string{contact.Name, contact.Address, itoa(contact.Count), formatTime(&contact.LastSeenAt)})
	}
	return printTable([]string{"NAME", "ADDRESS", "MESSAGES", "LAST SEEN"}, rows)
}

func runContactAdd(ctx context.Context, command *cli.Command) error {
	address := command.Args().First()
	if address == "" {
		return usage("which address? usage: teanode mailbox contact add <address> [--name ...]")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	view, err := requireMailbox(ctx, command, connection)
	if err != nil {
		return err
	}
	contact, err := client.SaveMailboxContact(ctx, connection, view.Mailbox.ID, address, command.String("name"))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(contact)
	}
	fmt.Printf("saved %s\n", contact.Address)
	return nil
}

func runContactRemove(ctx context.Context, command *cli.Command) error {
	address := command.Args().First()
	if address == "" {
		return usage("which address? usage: teanode mailbox contact remove <address>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	view, err := requireMailbox(ctx, command, connection)
	if err != nil {
		return err
	}
	if err := confirm(command, fmt.Sprintf("This removes the contact %s. It comes back if that address writes again.", address)); err != nil {
		return err
	}
	if err := client.DeleteMailboxContact(ctx, connection, view.Mailbox.ID, address); err != nil {
		return describeError(command, err)
	}
	fmt.Printf("removed %s\n", address)
	return nil
}

// --- devices ----------------------------------------------------------------

func newDeviceCommand() *cli.Command {
	return &cli.Command{
		Name:  "device",
		Usage: "the app passwords a mail program signs in with, one per device",
		Commands: []*cli.Command{
			{
				Name:   "list",
				Usage:  "list a mailbox's app passwords",
				Flags:  []cli.Flag{JSONFlag(), mailboxFlag()},
				Action: runDeviceList,
			},
			{
				Name:      "add",
				Aliases:   []string{"create"},
				Usage:     "make an app password for a device; it is shown once and never again",
				ArgsUsage: "<name>",
				Flags:     []cli.Flag{JSONFlag(), mailboxFlag()},
				Action:    runDeviceAdd,
			},
			{
				Name:      "remove",
				Aliases:   []string{"delete", "revoke"},
				Usage:     "revoke one device's app password",
				ArgsUsage: "<name-or-id>",
				Flags:     []cli.Flag{ForceFlag(), mailboxFlag()},
				Action:    runDeviceRemove,
			},
		},
	}
}

func runDeviceList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	view, err := requireMailbox(ctx, command, connection)
	if err != nil {
		return err
	}
	passwords, err := client.ListMailboxAppPasswords(ctx, connection, view.Mailbox.ID)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(passwords)
	}
	if len(passwords) == 0 {
		fmt.Println("no app passwords; add one with 'teanode mailbox device add <name>'")
		return nil
	}
	rows := make([][]string, 0, len(passwords))
	for _, password := range passwords {
		used := "never"
		if password.LastUsedAt != nil {
			used = formatTime(password.LastUsedAt)
		}
		rows = append(rows, []string{password.Name, formatTime(&password.CreatedAt), used, password.ID})
	}
	return printTable([]string{"NAME", "CREATED", "LAST USED", "ID"}, rows)
}

func runDeviceAdd(ctx context.Context, command *cli.Command) error {
	name := command.Args().First()
	if name == "" {
		return usage("what is the device called? usage: teanode mailbox device add <name>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	view, err := requireMailbox(ctx, command, connection)
	if err != nil {
		return err
	}
	created, err := client.CreateMailboxAppPassword(ctx, connection, view.Mailbox.ID, name)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(created)
	}
	return printFields([][2]string{
		{"device", created.AppPassword.Name},
		{"username", created.Username},
		{"password", created.Password},
		{"", "this is the only time the password is shown; 'teanode mailbox programs' says where to type it"},
	})
}

func runDeviceRemove(ctx context.Context, command *cli.Command) error {
	wanted := command.Args().First()
	if wanted == "" {
		return usage("which device? usage: teanode mailbox device remove <name-or-id>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	view, err := requireMailbox(ctx, command, connection)
	if err != nil {
		return err
	}
	passwords, err := client.ListMailboxAppPasswords(ctx, connection, view.Mailbox.ID)
	if err != nil {
		return describeError(command, err)
	}
	var found *client.MailboxAppPassword
	for _, password := range passwords {
		if password.ID == wanted || strings.EqualFold(password.Name, wanted) {
			found = password
			break
		}
	}
	if found == nil {
		return fmt.Errorf("no device called %q in %s", wanted, view.Mailbox.Name)
	}
	if err := confirm(command, fmt.Sprintf("This revokes the app password %s. That device stops receiving mail until it is given a new one.", found.Name)); err != nil {
		return err
	}
	if err := client.DeleteMailboxAppPassword(ctx, connection, found.ID); err != nil {
		return describeError(command, err)
	}
	fmt.Printf("revoked %s\n", found.Name)
	return nil
}

// --- out of office ----------------------------------------------------------

func newAutoReplyCommand() *cli.Command {
	return &cli.Command{
		Name:  "autoreply",
		Usage: "the out-of-office reply",
		Commands: []*cli.Command{
			{
				Name:   "show",
				Usage:  "what the out-of-office reply is set to",
				Flags:  []cli.Flag{JSONFlag(), mailboxFlag()},
				Action: runAutoReplyShow,
			},
			{
				Name:  "set",
				Usage: "turn the out-of-office reply on, and say what it says",
				Description: "It answers once per sender, and never a mailing list, an automatic\n" +
					"message, or another mailbox that is also away.\n\n" +
					"  teanode mailbox autoreply set --text 'Away until Monday.'\n" +
					"  teanode mailbox autoreply set --text - --subject 'Out of office'",
				Flags: []cli.Flag{
					JSONFlag(), mailboxFlag(),
					&cli.StringFlag{Name: "text", Usage: "what it says; \"-\" reads it from standard input"},
					&cli.StringFlag{Name: "html", Usage: "an HTML version; \"-\" reads it from standard input"},
					&cli.StringFlag{Name: "subject", Usage: "the subject; the original subject with \"Auto: \" before it by default"},
				},
				Action: runAutoReplySet,
			},
			{
				Name:   "off",
				Usage:  "turn the out-of-office reply off, keeping what it says",
				Flags:  []cli.Flag{JSONFlag(), mailboxFlag()},
				Action: runAutoReplyOff,
			},
		},
	}
}

func runAutoReplyShow(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	view, err := requireMailbox(ctx, command, connection)
	if err != nil {
		return err
	}
	reply := view.Mailbox.AutoReply
	if command.Bool("json") {
		return PrintJSON(reply)
	}
	if reply == nil {
		fmt.Println("no out-of-office reply set")
		return nil
	}
	fields := [][2]string{{"state", yesNo(reply.Enabled)}}
	if reply.Subject != "" {
		fields = append(fields, [2]string{"subject", reply.Subject})
	}
	if reply.From != nil {
		fields = append(fields, [2]string{"from", formatTime(reply.From)})
	}
	if reply.Until != nil {
		fields = append(fields, [2]string{"until", formatTime(reply.Until)})
	}
	fields = append(fields, [2]string{"text", reply.Text})
	return printFields(fields)
}

func runAutoReplySet(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	view, err := requireMailbox(ctx, command, connection)
	if err != nil {
		return err
	}
	reply := &client.MailboxAutoReply{Enabled: true}
	if view.Mailbox.AutoReply != nil {
		copied := *view.Mailbox.AutoReply
		reply = &copied
		reply.Enabled = true
	}
	if command.IsSet("text") {
		value, err := readValueOrStandardInput(command.String("text"))
		if err != nil {
			return err
		}
		reply.Text = value
	}
	if command.IsSet("html") {
		value, err := readValueOrStandardInput(command.String("html"))
		if err != nil {
			return err
		}
		reply.HTML = value
	}
	if command.IsSet("subject") {
		reply.Subject = command.String("subject")
	}
	if strings.TrimSpace(reply.Text) == "" && strings.TrimSpace(reply.HTML) == "" {
		return usage("what should it say? pass --text, or --text - to read it from standard input")
	}
	updated, err := client.UpdateMailbox(ctx, connection, view.Mailbox.ID, &client.MailboxParameters{AutoReply: reply})
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(updated.Mailbox.AutoReply)
	}
	fmt.Println("the out-of-office reply is on")
	return nil
}

func runAutoReplyOff(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	view, err := requireMailbox(ctx, command, connection)
	if err != nil {
		return err
	}
	if view.Mailbox.AutoReply == nil {
		fmt.Println("no out-of-office reply set")
		return nil
	}
	reply := *view.Mailbox.AutoReply
	reply.Enabled = false
	if _, err := client.UpdateMailbox(ctx, connection, view.Mailbox.ID, &client.MailboxParameters{AutoReply: &reply}); err != nil {
		return describeError(command, err)
	}
	fmt.Println("the out-of-office reply is off")
	return nil
}

// readValueOrStandardInput is a flag's value, or everything on standard
// input when the value is "-": a signature or an out-of-office message is
// usually a paragraph, and a paragraph belongs in a file rather than in
// shell quoting.
func readValueOrStandardInput(value string) (string, error) {
	if value != "-" {
		return value, nil
	}
	content, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", fmt.Errorf("cannot read standard input: %w", err)
	}
	return strings.TrimRight(string(content), "\n"), nil
}

// itoa is strconv.Itoa under a shorter name, for table cells.
func itoa(value int) string {
	return strconv.Itoa(value)
}

// newSubscriptionCommand is the mailing lists a mailbox receives, and leaving
// them.
//
// A subscription is not a stored thing but a grouping of stored things: every
// message that named the same list. So there is nothing to create or rename
// here — only what arrived, and the one thing a person wants to do about it.
func newSubscriptionCommand() *cli.Command {
	return &cli.Command{
		Name:    "subscription",
		Aliases: []string{"subscriptions"},
		Usage:   "the mailing lists this mailbox receives, and leaving them",
		Commands: []*cli.Command{
			{
				Name:  "list",
				Usage: "list the subscriptions, the busiest first",
				Flags: []cli.Flag{
					JSONFlag(), mailboxFlag(),
					&cli.IntFlag{Name: "first", Value: 50, Usage: "how many at most"},
					&cli.IntFlag{Name: "offset", Usage: "skip this many"},
				},
				Action: runSubscriptionList,
			},
			{
				Name:      "show",
				Usage:     "one subscription: how much of it there is, and how it can be left",
				ArgsUsage: "<key>",
				Flags:     []cli.Flag{JSONFlag(), mailboxFlag()},
				Action:    runSubscriptionShow,
			},
			{
				Name:      "mail",
				Usage:     "its messages, newest first, the way the dashboard groups them",
				ArgsUsage: "<key>",
				Flags:     []cli.Flag{JSONFlag(), mailboxFlag()},
				Action:    runSubscriptionMail,
			},
			{
				Name:      "unsubscribe",
				Usage:     "ask to leave, by whichever way the sender offered",
				ArgsUsage: "<key>",
				Flags:     []cli.Flag{JSONFlag(), mailboxFlag()},
				Action:    runSubscriptionUnsubscribe,
			},
		},
	}
}

func runSubscriptionList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	view, err := requireMailbox(ctx, command, connection)
	if err != nil {
		return err
	}
	page, err := client.ListMailboxSubscriptions(ctx, connection, view.Mailbox.ID,
		int(command.Int("first")), int(command.Int("offset")))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(page)
	}
	if page == nil || len(page.Subscriptions) == 0 {
		fmt.Println("no subscriptions; mail that names a list is what makes one")
		return nil
	}
	rows := make([][]string, 0, len(page.Subscriptions))
	for _, subscription := range page.Subscriptions {
		rows = append(rows, []string{
			subscription.Name,
			subscription.From,
			itoa(subscription.Count),
			itoa(subscription.Unread),
			formatTime(&subscription.LastAt),
			describeUnsubscribeState(subscription),
			subscription.Key,
		})
	}
	return printTable([]string{"NAME", "FROM", "MAIL", "UNREAD", "NEWEST", "STATE", "KEY"}, rows)
}

func runSubscriptionShow(ctx context.Context, command *cli.Command) error {
	key := command.Args().First()
	if key == "" {
		return usage("which subscription? usage: teanode mailbox subscription show <key>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	view, err := requireMailbox(ctx, command, connection)
	if err != nil {
		return err
	}
	subscription, err := client.GetMailboxSubscription(ctx, connection, view.Mailbox.ID, key)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(subscription)
	}
	if subscription == nil {
		return describeNotFound(command, client.ErrNotFound, "subscription "+key)
	}
	fields := [][2]string{
		{"name", subscription.Name},
		{"from", subscription.From},
		{"mail", itoa(subscription.Count)},
		{"unread", itoa(subscription.Unread)},
		{"newest", formatTime(&subscription.LastAt)},
		{"state", describeUnsubscribeState(subscription)},
		{"key", subscription.Key},
	}
	if subscription.OneClick {
		fields = append(fields, [2]string{"one click", "yes; one request is enough, and the sender undertook to honour it"})
	}
	for index, address := range subscription.Unsubscribe {
		name := "leave by"
		if index > 0 {
			// A continuation of the line above, which is what an empty name
			// means here.
			name = ""
		}
		fields = append(fields, [2]string{name, address})
	}
	if subscription.Error != "" {
		fields = append(fields, [2]string{"error", subscription.Error})
	}
	return printFields(fields)
}

func runSubscriptionMail(ctx context.Context, command *cli.Command) error {
	key := command.Args().First()
	if key == "" {
		return usage("which subscription? usage: teanode mailbox subscription mail <key>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	view, err := requireMailbox(ctx, command, connection)
	if err != nil {
		return err
	}
	thread, err := client.ReadMailboxSubscription(ctx, connection, view.Mailbox.ID, key)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(thread)
	}
	if thread == nil || len(thread.Items) == 0 {
		fmt.Println("no mail from this subscription is in the mailbox")
		return nil
	}
	rows := make([][]string, 0, len(thread.Items))
	for _, entry := range thread.Items {
		if entry.Item == nil || entry.Item.Mail == nil {
			continue
		}
		state := "read"
		if !entry.Item.Seen {
			state = "unread"
		}
		rows = append(rows, []string{
			formatTime(entry.Item.Mail.ReceivedAt),
			entry.Item.Mail.Subject,
			entry.FolderName,
			state,
			entry.Item.ID,
		})
	}
	if err := printTable([]string{"RECEIVED", "SUBJECT", "FOLDER", "", "ID"}, rows); err != nil {
		return err
	}
	if thread.Truncated {
		fmt.Println("\nthere is more than this; the oldest are not listed")
	}
	return nil
}

func runSubscriptionUnsubscribe(ctx context.Context, command *cli.Command) error {
	key := command.Args().First()
	if key == "" {
		return usage("which subscription? usage: teanode mailbox subscription unsubscribe <key>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	view, err := requireMailbox(ctx, command, connection)
	if err != nil {
		return err
	}
	subscription, err := client.UnsubscribeMailboxSubscription(ctx, connection, view.Mailbox.ID, key)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(subscription)
	}
	if subscription == nil {
		return describeNotFound(command, client.ErrNotFound, "subscription "+key)
	}

	// What happened, in the sender's terms rather than ours: leaving is a
	// request made to somebody else, and only one of the three ways finishes
	// here.
	switch {
	case subscription.Failed:
		fmt.Printf("could not leave %s: %s\n", subscription.Name, subscription.Error)
	case subscription.Method == "link":
		fmt.Printf("%s asks a person to leave through a page, so nothing was sent:\n", subscription.Name)
		for _, address := range subscription.Unsubscribe {
			fmt.Printf("  %s\n", address)
		}
	case subscription.Method == "oneClick":
		fmt.Printf("asked %s to stop, with the one-click request it offered\n", subscription.Name)
	case subscription.Method == "mail":
		fmt.Printf("sent %s the message it named to leave by\n", subscription.Name)
	default:
		fmt.Printf("asked %s to stop\n", subscription.Name)
	}
	fmt.Println("mail already in the mailbox stays; what stops is what has not been sent yet")
	return nil
}

// describeUnsubscribeState says where leaving got to, for a column that has
// to hold it in one word or two.
func describeUnsubscribeState(subscription *client.MailboxSubscription) string {
	switch {
	case subscription.Failed:
		return "failed"
	case subscription.RequestedAt != nil && subscription.Method == "link":
		return "opened"
	case subscription.RequestedAt != nil:
		return "asked to leave"
	case subscription.OneClick:
		return "one click"
	case len(subscription.Unsubscribe) > 0:
		return "can leave"
	}
	return "no way offered"
}
