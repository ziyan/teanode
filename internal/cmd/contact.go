package cmd

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/models"
)

// The address book: the people somebody keeps, which their phone
// synchronizes over CardDAV.
//
// Not "mailbox contact", which is the addresses a mailbox has learned from
// traffic. The address book belongs to the account rather than to a mailbox,
// which is why it is here and not under mailbox. A circle has an address book
// of its own too, which everyone in it reads and edits: 'contact book' makes
// and lists them, and --book on the commands that keep contacts chooses one.

// bookFlag chooses the address book to work in: your own when left out.
func bookFlag() cli.Flag {
	return &cli.StringFlag{Name: "book", Usage: "the address book, from 'teanode contact book list'; your own by default. A circle's is chosen with its id"}
}

func NewContactCommand() *cli.Command {
	return &cli.Command{
		Name:  "contact",
		Usage: "your address book: the people you keep, synchronized to your devices over CardDAV",
		Commands: []*cli.Command{
			{
				Name:  "list",
				Usage: "list your contacts",
				Flags: []cli.Flag{
					JSONFlag(), bookFlag(),
					&cli.StringFlag{Name: "search", Usage: "only those whose name, organization, address or number matches"},
					&cli.IntFlag{Name: "first", Usage: "how many, at most"},
				},
				Action: runContactBookList,
			},
			{
				Name:      "show",
				Usage:     "one contact, with the card as it is stored",
				ArgsUsage: "<id>",
				Flags:     []cli.Flag{JSONFlag(), bookFlag()},
				Action:    runContactBookShow,
			},
			{
				Name:  "add",
				Usage: "keep a contact; to promote one the mailbox has learned, give its address here",
				Flags: []cli.Flag{
					JSONFlag(), bookFlag(),
					&cli.StringFlag{Name: "name", Usage: "what to call them"},
					&cli.StringFlag{Name: "organization", Usage: "where they work"},
					&cli.StringFlag{Name: "title", Usage: "what they do there"},
					&cli.StringSliceFlag{Name: "email", Usage: "an email address; repeat for more"},
					&cli.StringSliceFlag{Name: "phone", Usage: "a telephone number; repeat for more"},
					&cli.StringFlag{Name: "note", Usage: "anything else"},
					&cli.StringFlag{Name: "card", Usage: "a whole vCard, or - to read one from standard input; the other flags are then ignored"},
				},
				Action: runContactBookAdd,
			},
			{
				Name:      "edit",
				Usage:     "change a contact; what you do not give is left alone",
				ArgsUsage: "<id>",
				Flags: []cli.Flag{
					JSONFlag(), bookFlag(),
					&cli.StringFlag{Name: "name", Usage: "what to call them; give it empty to clear it"},
					&cli.StringFlag{Name: "organization", Usage: "where they work"},
					&cli.StringFlag{Name: "title", Usage: "what they do there"},
					&cli.StringSliceFlag{Name: "email", Usage: "the email addresses, replacing what is there"},
					&cli.StringSliceFlag{Name: "phone", Usage: "the telephone numbers, replacing what is there"},
					&cli.StringFlag{Name: "note", Usage: "anything else"},
					&cli.StringFlag{Name: "card", Usage: "a whole vCard, or - to read one from standard input"},
				},
				Action: runContactBookEdit,
			},
			{
				Name:      "remove",
				Aliases:   []string{"delete"},
				Usage:     "forget a contact",
				ArgsUsage: "<id>",
				Flags:     []cli.Flag{ForceFlag(), bookFlag()},
				Action:    runContactBookRemove,
			},
			{
				Name:      "remove-picture",
				Usage:     "take the picture off a contact and leave the rest of it",
				ArgsUsage: "<id>",
				Flags:     []cli.Flag{JSONFlag(), ForceFlag(), bookFlag()},
				Action:    runContactBookRemovePicture,
			},
			{
				Name:      "link",
				Usage:     "link a contact to somebody you share a circle with; only you know, they are not told",
				ArgsUsage: "<contact id> <username>",
				Flags:     []cli.Flag{JSONFlag(), &cli.StringFlag{Name: "address-book", Usage: "the address book the contact is in, when its id is in more than one of yours"}},
				Action:    runContactBookLink,
			},
			{
				Name:      "unlink",
				Usage:     "unlink a contact from somebody in your circles; only you knew, so nobody is told",
				ArgsUsage: "<contact id>",
				Flags:     []cli.Flag{JSONFlag(), &cli.StringFlag{Name: "address-book", Usage: "the address book the contact is in, when its id is in more than one of yours"}},
				Action:    runContactBookUnlink,
			},
			{
				Name:   "suggestions",
				Usage:  "contacts that carry a mailbox address of somebody you share a circle with; 'contact link' confirms one",
				Flags:  []cli.Flag{JSONFlag()},
				Action: runContactBookSuggestions,
			},
			{
				Name:  "book",
				Usage: "your address books, and the one each circle keeps for itself",
				Commands: []*cli.Command{
					{
						Name:   "list",
						Usage:  "list the address books you can use: your own, and your circles'",
						Flags:  []cli.Flag{JSONFlag()},
						Action: runContactBookBooks,
					},
					{
						Name:   "create",
						Usage:  "make the address book a circle keeps for itself; anyone in the circle may",
						Flags:  []cli.Flag{JSONFlag(), &cli.StringFlag{Name: "circle", Usage: "the circle, by id or name", Required: true}, &cli.StringFlag{Name: "name", Usage: "what to call it; the circle's name by default"}},
						Action: runContactBookCreate,
					},
					{
						Name:      "rename",
						Usage:     "rename an address book; a circle's only by an admin of the circle",
						ArgsUsage: "<book id> <name>",
						Flags:     []cli.Flag{JSONFlag()},
						Action:    runContactBookRename,
					},
					{
						Name:      "delete",
						Usage:     "delete a circle's address book with every contact in it; only an admin of the circle may",
						ArgsUsage: "<book id>",
						Flags:     []cli.Flag{ForceFlag()},
						Action:    runContactBookDelete,
					},
				},
			},
			{
				Name:      "me",
				Usage:     "say which contact is you, so your agent knows your own addresses; with no id, say who it thinks you are",
				ArgsUsage: "[id]",
				Action:    runContactBookMe,
			},
		},
	}
}

// theAddressBook is the caller's, made by the server if they have never had
// one. A person has one of their own; the circles' are chosen with --book.
func theAddressBook(ctx context.Context, connection *client.Client) (*client.AddressBook, error) {
	books, err := client.ListAddressBooks(ctx, connection)
	if err != nil {
		return nil, err
	}
	if len(books) == 0 {
		return nil, fmt.Errorf("this account has no address book")
	}
	return books[0], nil
}

// chosenAddressBook is the book --book names, one the caller can use, or their
// own when it is left out.
func chosenAddressBook(ctx context.Context, command *cli.Command, connection *client.Client) (*client.AddressBook, error) {
	chosen := strings.TrimSpace(command.String("book"))
	if chosen == "" {
		return theAddressBook(ctx, connection)
	}
	books, err := client.ListReachableAddressBooks(ctx, connection)
	if err != nil {
		return nil, err
	}
	for _, book := range books {
		if book.ID == chosen {
			return book, nil
		}
	}
	return nil, fmt.Errorf("no address book %s; 'teanode contact book list' lists yours and your circles'", chosen)
}

// chosenAddressBookId is the id of the book --book names, or empty when it is
// left out: a command that names a contact then lets the server look in the
// caller's own books first. A contact in a circle's book is reached only by
// naming the book.
func chosenAddressBookId(ctx context.Context, command *cli.Command, connection *client.Client) (string, error) {
	if strings.TrimSpace(command.String("book")) == "" {
		return "", nil
	}
	book, err := chosenAddressBook(ctx, command, connection)
	if err != nil {
		return "", err
	}
	return book.ID, nil
}

func runContactBookList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	book, err := chosenAddressBook(ctx, command, connection)
	if err != nil {
		return describeError(command, err)
	}
	contacts, err := client.ListContacts(ctx, connection, book.ID, command.String("search"), command.Int("first"))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(contacts)
	}
	if len(contacts) == 0 {
		fmt.Println("no contacts yet; 'teanode contact add --name \"Ada Lovelace\" --email ada@example.com' keeps one")
		return nil
	}
	rows := make([][]string, 0, len(contacts))
	for _, contact := range contacts {
		written := ""
		if len(contact.Addresses) > 0 && contact.Addresses[0] != nil {
			written = contact.Addresses[0].Written
		}
		// The username of the person in your circles the contact is linked to,
		// as 'contact show' says it.
		linkedUsername := ""
		if contact.Account != nil {
			linkedUsername = contact.Account.Username
		}
		rows = append(rows, []string{
			contact.Name, strings.Join(contact.Emails, ", "),
			strings.Join(contact.Phones, ", "), written, contact.Organization, linkedUsername, contact.ID,
		})
	}
	return printTable([]string{"name", "email", "numbers", "address", "organization", "in your circles as", "id"}, rows)
}

func runContactBookShow(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 1 {
		return fmt.Errorf("which contact: 'teanode contact list' says their ids")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	addressBookId, err := chosenAddressBookId(ctx, command, connection)
	if err != nil {
		return describeError(command, err)
	}
	contact, err := client.GetContact(ctx, connection, addressBookId, command.Args().First())
	if err != nil {
		return describeError(command, err)
	}
	if contact == nil {
		return fmt.Errorf("no such contact")
	}
	if command.Bool("json") {
		return PrintJSON(contact)
	}
	fmt.Printf("%s\n", contact.Name)
	if contact.Organization != "" {
		fmt.Printf("  %s\n", contact.Organization)
	}
	for _, address := range contact.Emails {
		fmt.Printf("  %s\n", address)
	}
	for _, number := range contact.Phones {
		fmt.Printf("  %s\n", number)
	}
	for _, address := range contact.Addresses {
		if address == nil {
			continue
		}
		if address.Label != "" {
			fmt.Printf("  %s (%s)\n", address.Written, address.Label)
			continue
		}
		fmt.Printf("  %s\n", address.Written)
	}
	if contact.HasPhoto {
		fmt.Printf("  has a picture\n")
	}
	if contact.Account != nil {
		fmt.Printf("  In your circles as %s\n", forTerminal(contact.Account.Username))
	}
	if contact.Note != "" {
		fmt.Printf("  %s\n", contact.Note)
	}
	fmt.Printf("\n%s", contact.Card)
	return nil
}

// fieldsFrom reads the flags that were actually given. A flag left out means
// "leave it alone"; a flag given empty means "clear it".
func fieldsFrom(command *cli.Command) (*client.SaveContactFields, error) {
	fields := &client.SaveContactFields{}
	if card := command.String("card"); card != "" {
		text, err := readValueOrStandardInput(card)
		if err != nil {
			return nil, err
		}
		fields.Card = text
		return fields, nil
	}
	if command.IsSet("name") {
		value := command.String("name")
		fields.Name = &value
	}
	if command.IsSet("organization") {
		value := command.String("organization")
		fields.Organization = &value
	}
	if command.IsSet("title") {
		value := command.String("title")
		fields.Title = &value
	}
	if command.IsSet("note") {
		value := command.String("note")
		fields.Note = &value
	}
	if command.IsSet("email") {
		value := command.StringSlice("email")
		fields.Emails = &value
	}
	if command.IsSet("phone") {
		value := command.StringSlice("phone")
		fields.Phones = &value
	}
	return fields, nil
}

func runContactBookAdd(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	book, err := chosenAddressBook(ctx, command, connection)
	if err != nil {
		return describeError(command, err)
	}
	fields, err := fieldsFrom(command)
	if err != nil {
		return err
	}
	if fields.Card == "" && fields.Name == nil && fields.Emails == nil {
		return fmt.Errorf("give at least a name or an email address")
	}
	fields.AddressBookID = book.ID
	contact, err := client.SaveContact(ctx, connection, fields)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(contact)
	}
	fmt.Printf("kept %s as %s\n", displayed(contact), contact.ID)
	return nil
}

func runContactBookEdit(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 1 {
		return fmt.Errorf("which contact: 'teanode contact list' says their ids")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	book, err := chosenAddressBook(ctx, command, connection)
	if err != nil {
		return describeError(command, err)
	}
	fields, err := fieldsFrom(command)
	if err != nil {
		return err
	}
	fields.AddressBookID = book.ID
	fields.ID = command.Args().First()
	contact, err := client.SaveContact(ctx, connection, fields)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(contact)
	}
	fmt.Printf("changed %s\n", displayed(contact))
	return nil
}

func runContactBookRemove(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 1 {
		return fmt.Errorf("which contact: 'teanode contact list' says their ids")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	id := command.Args().First()
	addressBookId, err := chosenAddressBookId(ctx, command, connection)
	if err != nil {
		return describeError(command, err)
	}
	contact, err := client.GetContact(ctx, connection, addressBookId, id)
	if err != nil {
		return describeError(command, err)
	}
	if contact == nil {
		return fmt.Errorf("no such contact")
	}
	if err := confirm(command, fmt.Sprintf(
		"This forgets %s. Devices synchronizing your address book will lose the contact too.",
		displayed(contact))); err != nil {
		return err
	}
	if err := client.DeleteContact(ctx, connection, addressBookId, id); err != nil {
		return describeError(command, err)
	}
	fmt.Printf("forgot %s\n", displayed(contact))
	return nil
}

func runContactBookRemovePicture(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 1 {
		return fmt.Errorf("which contact: 'teanode contact list' says their ids")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	id := command.Args().First()
	addressBookId, err := chosenAddressBookId(ctx, command, connection)
	if err != nil {
		return describeError(command, err)
	}
	contact, err := client.GetContact(ctx, connection, addressBookId, id)
	if err != nil {
		return describeError(command, err)
	}
	if contact == nil {
		return fmt.Errorf("no such contact")
	}
	if !contact.HasPhoto {
		// The contact as it is, the same shape the removal answers with.
		if command.Bool("json") {
			return PrintJSON(contact)
		}
		fmt.Printf("%s has no picture\n", displayed(contact))
		return nil
	}
	if err := confirm(command, fmt.Sprintf(
		"This takes the picture off %s. Devices synchronizing your address book will lose it too.",
		displayed(contact))); err != nil {
		return err
	}
	kept, err := client.RemoveContactPicture(ctx, connection, addressBookId, id)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(kept)
	}
	fmt.Printf("took the picture off %s\n", displayed(contact))
	return nil
}

// setContactAccountInYourBooks links a contact, or clears the link with an
// empty account, in the address book that holds it. A contact id can be in
// more than one of your books; then the book must be chosen with
// --address-book, since the link is a change to that one contact.
func setContactAccountInYourBooks(ctx context.Context, command *cli.Command, connection *client.Client, contactId, accountUserId string) (*client.Contact, error) {
	books, err := client.ListAddressBooks(ctx, connection)
	if err != nil {
		return nil, err
	}
	chosenAddressBookId := strings.TrimSpace(command.String("address-book"))
	holdingBooks := []*client.AddressBook{}
	for _, book := range books {
		if chosenAddressBookId != "" && book.ID != chosenAddressBookId {
			continue
		}
		contacts, err := client.ListContacts(ctx, connection, book.ID, "", 0)
		if err != nil {
			return nil, err
		}
		for _, contact := range contacts {
			if contact.ID == contactId {
				holdingBooks = append(holdingBooks, book)
				break
			}
		}
	}
	switch len(holdingBooks) {
	case 0:
		if chosenAddressBookId != "" {
			return nil, fmt.Errorf("no such contact in that address book: 'teanode contact list' says their ids")
		}
		return nil, fmt.Errorf("no such contact: 'teanode contact list' says their ids")
	case 1:
		return client.SetContactAccount(ctx, connection, holdingBooks[0].ID, contactId, accountUserId)
	}
	descriptions := make([]string, 0, len(holdingBooks))
	for _, book := range holdingBooks {
		descriptions = append(descriptions, fmt.Sprintf("%s (%s)", forTerminal(book.Name), book.ID))
	}
	return nil, fmt.Errorf("that contact is in more than one of your address books: %s; choose one with --address-book <id>", strings.Join(descriptions, ", "))
}

func runContactBookLink(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 2 {
		return usage("usage: teanode contact link <contact id> <username>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	accountUserId, username, err := photoPersonNamed(ctx, connection, strings.TrimPrefix(strings.TrimSpace(command.Args().Get(1)), "@"))
	if err != nil {
		return describeError(command, err)
	}
	contact, err := setContactAccountInYourBooks(ctx, command, connection, command.Args().First(), accountUserId)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(contact)
	}
	fmt.Printf("%s is in your circles as %s\n", displayed(contact), forTerminal(username))
	return nil
}

func runContactBookUnlink(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 1 {
		return usage("usage: teanode contact unlink <contact id>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	contact, err := setContactAccountInYourBooks(ctx, command, connection, command.Args().First(), "")
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(contact)
	}
	fmt.Printf("%s is no longer linked to somebody in your circles\n", displayed(contact))
	return nil
}

func runContactBookSuggestions(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	suggestions, err := client.ListContactAccountSuggestions(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(suggestions)
	}
	if len(suggestions) == 0 {
		fmt.Println("no contact looks like somebody in your circles")
		return nil
	}
	// The book is named only when there is more than one to tell apart.
	books, err := client.ListAddressBooks(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	bookNames := make(map[string]string, len(books))
	for _, book := range books {
		bookNames[book.ID] = book.Name
	}
	rows := make([][]string, 0, len(suggestions))
	for _, suggestion := range suggestions {
		row := []string{forTerminal(displayed(suggestion.Contact)), forTerminal(suggestion.Account.Username)}
		if len(books) > 1 {
			row = append(row, forTerminal(bookNames[suggestion.AddressBookID]))
		}
		rows = append(rows, append(row, "teanode contact link "+suggestion.Contact.ID+" "+suggestion.Account.Username))
	}
	if len(books) > 1 {
		return printTable([]string{"CONTACT", "IN YOUR CIRCLES AS", "ADDRESS BOOK", "TO CONFIRM"}, rows)
	}
	return printTable([]string{"CONTACT", "IN YOUR CIRCLES AS", "TO CONFIRM"}, rows)
}

// displayed is what to call a contact in a line of output.
func displayed(contact *client.Contact) string {
	if contact == nil {
		return "that contact"
	}
	if contact.Name != "" {
		return contact.Name
	}
	if len(contact.Emails) > 0 {
		return contact.Emails[0]
	}
	return contact.ID
}

// runContactBookMe names the contact that is the caller, or says which
// one is.
//
// It is how the agent knows which commits, messages and mail are the
// person's own rather than somebody else's: without it a checkout of
// their own work looks like a checkout of a stranger's, and the timeline
// it would have built is empty. The dashboard has had a button for this
// since the page was written; this is the same thing from a shell.
func runContactBookMe(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	id := command.Args().First()
	if id == "" {
		// The self page carries it: the contact is read live rather than
		// copied, so one number changed in the address book is right
		// everywhere.
		page, err := client.AgentGraphPageOf(ctx, connection, "self")
		if err != nil {
			return describeError(command, err)
		}
		if page == nil || page.Contact == nil {
			_, _ = fmt.Fprintln(command.Writer, "no contact is marked as you; give an id to say which is")
			return nil
		}
		_, _ = fmt.Fprintf(command.Writer, "%s <%s>\n", page.Contact.Name, strings.Join(page.Contact.Emails, ", "))
		return nil
	}
	if err := client.SetMyContact(ctx, connection, id); err != nil {
		return describeError(command, err)
	}
	_, _ = fmt.Fprintln(command.Writer, "that contact is you now; your agent will read its addresses as your own")
	return nil
}

// circleNamed is the circle the caller means by what they typed: its id, or
// its name when only one circle they are in has it.
func circleNamed(ctx context.Context, connection *client.Client, typed string) (*client.Circle, error) {
	typed = strings.TrimSpace(typed)
	circles, err := client.ListCircles(ctx, connection)
	if err != nil {
		return nil, err
	}
	var named []*client.Circle
	for _, circle := range circles {
		if circle.ID == typed {
			return circle, nil
		}
		if strings.EqualFold(circle.Name, typed) {
			named = append(named, circle)
		}
	}
	switch len(named) {
	case 0:
		return nil, fmt.Errorf("you are in no circle %q; 'teanode circle list' says which", typed)
	case 1:
		return named[0], nil
	}
	choices := make([]models.ShareableCircle, 0, len(named))
	for _, circle := range named {
		choices = append(choices, models.ShareableCircle{ID: circle.ID, Name: circle.Name, MemberCount: len(circle.Members)})
	}
	return nil, models.AmbiguousCircleNameError(typed, choices)
}

// whoseBook says whose an address book is, for a line of output.
func whoseBook(book *client.AddressBook) string {
	if book.IsCircleBook {
		return forTerminal(models.DescribeCircleByName(book.CircleName))
	}
	return "you"
}

func runContactBookBooks(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	books, err := client.ListReachableAddressBooks(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(books)
	}
	rows := make([][]string, 0, len(books))
	for _, book := range books {
		rows = append(rows, []string{forTerminal(book.Name), whoseBook(book), strconv.Itoa(book.Contacts), book.ID})
	}
	return printTable([]string{"name", "whose", "contacts", "id"}, rows)
}

func runContactBookCreate(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	circle, err := circleNamed(ctx, connection, command.String("circle"))
	if err != nil {
		return describeError(command, err)
	}
	made, err := client.CreateCircleAddressBook(ctx, connection, circle.ID, strings.TrimSpace(command.String("name")))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(made)
	}
	fmt.Printf("made %q, which everyone in %s reads and edits; add to it with 'teanode contact add --book %s'\n",
		forTerminal(made.Name), forTerminal(models.DescribeCircleByName(made.CircleName)), made.ID)
	return nil
}

func runContactBookRename(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 2 {
		return usage("usage: teanode contact book rename <book id> <name>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	books, err := client.ListReachableAddressBooks(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	var found *client.AddressBook
	for _, book := range books {
		if book.ID == command.Args().First() {
			found = book
		}
	}
	if found == nil {
		return fmt.Errorf("no address book %s; 'teanode contact book list' lists yours and your circles'", command.Args().First())
	}
	// What the book says about itself is kept: only the name changes.
	renamed, err := client.SaveAddressBook(ctx, connection, found.ID, command.Args().Get(1), found.Description)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(renamed)
	}
	fmt.Printf("renamed to %q\n", forTerminal(renamed.Name))
	return nil
}

func runContactBookDelete(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 1 {
		return usage("usage: teanode contact book delete <book id>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	books, err := client.ListReachableAddressBooks(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	var found *client.AddressBook
	for _, book := range books {
		if book.ID == command.Args().First() {
			found = book
		}
	}
	if found == nil || !found.IsCircleBook {
		return fmt.Errorf("no circle address book %s; 'teanode contact book list' lists them", command.Args().First())
	}
	if err := confirm(command, fmt.Sprintf(
		"This deletes %q and its %d contacts for everyone in %s. Their devices synchronizing it will lose them too.",
		forTerminal(found.Name), found.Contacts, forTerminal(models.DescribeCircleByName(found.CircleName)))); err != nil {
		return err
	}
	if err := client.DeleteCircleAddressBook(ctx, connection, found.ID); err != nil {
		return describeError(command, err)
	}
	fmt.Printf("deleted %q\n", forTerminal(found.Name))
	return nil
}
