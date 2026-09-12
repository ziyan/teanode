package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// The address book: the people somebody keeps, which their phone
// synchronizes over CardDAV.
//
// Not "mailbox contact", which is the addresses a mailbox has learned from
// traffic. The address book belongs to the account rather than to a mailbox,
// which is why it is here and not under mailbox.

func NewContactCommand() *cli.Command {
	return &cli.Command{
		Name:  "contact",
		Usage: "your address book: the people you keep, synchronized to your devices over CardDAV",
		Commands: []*cli.Command{
			{
				Name:  "list",
				Usage: "list your contacts",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "search", Usage: "only those whose name, organization, address or number matches"},
					&cli.IntFlag{Name: "first", Usage: "how many, at most"},
				},
				Action: runContactBookList,
			},
			{
				Name:      "show",
				Usage:     "one contact, with the card as it is stored",
				ArgsUsage: "<id>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runContactBookShow,
			},
			{
				Name:  "add",
				Usage: "keep a contact",
				Flags: []cli.Flag{
					JSONFlag(),
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
					JSONFlag(),
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
				Flags:     []cli.Flag{ForceFlag()},
				Action:    runContactBookRemove,
			},
		},
	}
}

// theAddressBook is the caller's, made by the server if they have never had
// one. Everything here works on one book, because a person has one.
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

func runContactBookList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	book, err := theAddressBook(ctx, connection)
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
		rows = append(rows, []string{
			contact.Name, strings.Join(contact.Emails, ", "),
			strings.Join(contact.Phones, ", "), contact.Organization, contact.ID,
		})
	}
	return printTable([]string{"name", "addresses", "numbers", "organization", "id"}, rows)
}

func runContactBookShow(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 1 {
		return fmt.Errorf("which contact: 'teanode contact list' says their ids")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	contact, err := client.GetContact(ctx, connection, command.Args().First())
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
	book, err := theAddressBook(ctx, connection)
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
	book, err := theAddressBook(ctx, connection)
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
	contact, err := client.GetContact(ctx, connection, id)
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
	if err := client.DeleteContact(ctx, connection, id); err != nil {
		return describeError(command, err)
	}
	fmt.Printf("forgot %s\n", displayed(contact))
	return nil
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
