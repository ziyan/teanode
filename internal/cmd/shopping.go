package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/models"
)

// Shopping lists: the same operations as the dashboard's Shopping page, the
// iPhone app and the agent's shopping tool, under the same names. The live
// feed (ShoppingEvents) is the dashboard's and the app's; a command reads once.

const (
	// maximumShoppingImportBytes is the most text of a list that import-preview
	// and add --from-file send for the model to read.
	maximumShoppingImportBytes = 256 << 10

	// maximumShoppingPictureBytes is the largest picture of a list, or of an
	// item, that is uploaded: more than a model takes as a picture.
	maximumShoppingPictureBytes = 10 << 20
)

// NewShoppingCommand is `teanode shopping`.
func NewShoppingCommand() *cli.Command {
	itemFlags := func() []cli.Flag {
		return []cli.Flag{
			&cli.StringFlag{Name: "store", Usage: "the store to get it from; leave out for any store"},
			&cli.StringFlag{Name: "aisle", Usage: "the aisle or section of the store, as the list's heading says it"},
			&cli.StringFlag{Name: "quantity", Usage: "how many are needed, a number above zero (1 by default)"},
			&cli.StringFlag{Name: "unit", Usage: "what the quantity counts: kg, pack, bottle"},
			&cli.StringFlag{Name: "details", Usage: "preferences, in the list's own words: Fuji preferred, the large bag"},
		}
	}
	importFlags := func() []cli.Flag {
		return []cli.Flag{
			&cli.StringFlag{Name: "from-file", Usage: "a file with the text of a list, or - for standard input; the server's model reads it into items"},
			&cli.StringSliceFlag{Name: "picture", Usage: "a picture or screenshot of a list to read into items; repeat it for more than one"},
		}
	}
	return &cli.Command{
		Name:  "shopping",
		Usage: "shopping lists: shared with your circles, checked off at the store, matched to receipts",
		Description: "A list has items; a bought item stays on it as not needed, so a list is reused.\n" +
			"A list is named by its name or its id. Everybody a list is shared with can do\n" +
			"everything but share it further or delete it. Store names are compared without\n" +
			"case or punctuation. Every Finance receipt is matched to your lists on its own;\n" +
			"'teanode shopping receipt' shows what it did and 'undo-receipt' reverts it.\n\n" +
			"  teanode shopping create Groceries\n" +
			"  teanode shopping add Groceries Apples Bread --store \"Example Market\"\n" +
			"  teanode shopping add Groceries --from-file list.txt\n" +
			"  teanode shopping store \"Example Market\"\n" +
			"  teanode shopping check --store \"Example Market\" <item id>\n" +
			"  teanode shopping done",
		Commands: []*cli.Command{
			{Name: "lists", Usage: "the lists you reach, yours first, with how many items are needed", Flags: []cli.Flag{JSONFlag()}, Action: runShoppingLists},
			{
				Name: "show", Usage: "one list and its items", ArgsUsage: "<list>",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringSliceFlag{Name: "state", Usage: "only items in this state: needed, in_cart or not_needed; repeat it for more than one (every state by default)"},
					&cli.IntFlag{Name: "limit", Usage: "items to show, at most 500 (200 by default)"},
					&cli.IntFlag{Name: "offset", Usage: "how many items to skip, for the next page"},
				},
				Action: runShoppingShow,
			},
			{Name: "item", Usage: "one item", ArgsUsage: "<item id>", Flags: []cli.Flag{JSONFlag()}, Action: runShoppingItem},
			{Name: "create", Usage: "make a list", ArgsUsage: "<name>", Flags: []cli.Flag{JSONFlag()}, Action: runShoppingCreate},
			{Name: "rename", Usage: "give a list another name", ArgsUsage: "<list> <name>", Flags: []cli.Flag{JSONFlag()}, Action: runShoppingRename},
			{
				Name: "delete", Usage: "delete one of your lists with its items and prices; this cannot be undone", ArgsUsage: "<list>",
				Flags: []cli.Flag{JSONFlag(), ForceFlag()}, Action: runShoppingDelete,
			},
			{
				Name: "share", Usage: "share one of your lists with a person you share a circle with, or with a whole circle",
				ArgsUsage: "<list> <username|circle>",
				Description: "They can add, change and check off items, start shopping and upload receipts;\n" +
					"they cannot share the list further or delete it.",
				Flags: []cli.Flag{JSONFlag(), ForceFlag()}, Action: runShoppingShare,
			},
			{Name: "unshare", Usage: "stop sharing a list with somebody", ArgsUsage: "<share id>", Flags: []cli.Flag{JSONFlag(), ForceFlag()}, Action: runShoppingUnshare},
			{Name: "shares", Usage: "who one of your lists is shared with", ArgsUsage: "<list>", Flags: []cli.Flag{JSONFlag()}, Action: runShoppingShares},
			{
				Name: "remind", Usage: "be told what a list needs when you arrive at a store (on by default)", ArgsUsage: "<list>",
				Flags: []cli.Flag{JSONFlag(), &cli.BoolFlag{Name: "off", Usage: "stop the reminders for this list"}}, Action: runShoppingRemind,
			},
			{
				Name: "store-remind", Usage: "be told what your lists need when you arrive at one store (on by default)", ArgsUsage: "<store>",
				Flags: []cli.Flag{JSONFlag(), &cli.BoolFlag{Name: "off", Usage: "never remind me at this store"}}, Action: runShoppingStoreRemind,
			},
			{
				Name: "add", Usage: "add items to a list: named here, or read from a file or pictures of a list", ArgsUsage: "<list> [item]...",
				Description: "Items named here take the flags' store, aisle, quantity, unit and details. With\n" +
					"--from-file or --picture the server's model reads a list into items first, shows\n" +
					"them with what each merges into on the list, and asks before adding. An item\n" +
					"already on the list is merged into: its count becomes the larger of the two.",
				Flags: append(append(append([]cli.Flag{JSONFlag(), ForceFlag()}, itemFlags()...), importFlags()...),
					&cli.BoolFlag{Name: "store-brand", Usage: "the store's own product"},
					&cli.StringFlag{Name: "price", Usage: "the price of one, when you know it"},
					&cli.StringFlag{Name: "currency", Usage: "the currency of the price, such as USD"}),
				Action: runShoppingAdd,
			},
			{
				Name: "edit", Usage: "change an item; only what you name changes", ArgsUsage: "<item id>",
				Flags: append([]cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "description", Usage: "what the item is, without the count"},
					&cli.BoolFlag{Name: "store-brand", Usage: "the store's own product"},
					&cli.BoolFlag{Name: "not-store-brand", Usage: "not the store's own product"},
				}, itemFlags()...),
				Action: runShoppingEdit,
			},
			{Name: "remove", Usage: "take items off their lists for good; to keep one in the catalog use not-needed", ArgsUsage: "<item id>...", Flags: []cli.Flag{JSONFlag(), ForceFlag()}, Action: runShoppingRemove},
			{
				Name: "check", Usage: "put items in the cart; the first one starts your trip and tells the others", ArgsUsage: "<item id>...",
				Flags:  []cli.Flag{JSONFlag(), &cli.StringFlag{Name: "store", Usage: "the store you are shopping, for the trip the first check starts; the item's store by default"}},
				Action: runShoppingCheck,
			},
			{Name: "uncheck", Usage: "take items out of the cart, back to needed", ArgsUsage: "<item id>...", Flags: []cli.Flag{JSONFlag()}, Action: runShoppingUncheck},
			{
				Name: "need", Usage: "need items again, with a count", ArgsUsage: "<item id>...",
				Flags: []cli.Flag{JSONFlag(), &cli.StringFlag{Name: "quantity", Usage: "how many are needed, a number above zero (1 by default)"}}, Action: runShoppingNeed,
			},
			{Name: "not-needed", Usage: "mark items bought or not wanted now; they stay on the list", ArgsUsage: "<item id>...", Flags: []cli.Flag{JSONFlag()}, Action: runShoppingNotNeeded},
			{
				Name: "out-of-stock", Usage: "say a store had none of an item", ArgsUsage: "<item id>",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "store", Usage: "the store that had none; the item's store by default"},
					&cli.BoolFlag{Name: "in-stock", Usage: "it is in stock again"},
				},
				Action: runShoppingOutOfStock,
			},
			{
				Name: "price", Usage: "remember a price you saw for an item", ArgsUsage: "<item id> <price of one>",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "store", Usage: "the store it was seen at; the item's store by default"},
					&cli.StringFlag{Name: "on", Usage: "the day it was seen, as 2026-09-30; today by default"},
					&cli.StringFlag{Name: "currency", Usage: "the currency, such as USD"},
				},
				Action: runShoppingPrice,
			},
			{Name: "prices", Usage: "the prices remembered for an item, newest first", ArgsUsage: "<item id>", Flags: []cli.Flag{JSONFlag()}, Action: runShoppingPrices},
			{Name: "picture", Usage: "search the web for another picture of an item", ArgsUsage: "<item id>", Flags: []cli.Flag{JSONFlag()}, Action: runShoppingPicture},
			{
				Name: "set-picture", Usage: "give an item a picture of your own, or take its picture away", ArgsUsage: "<item id> [file]",
				Flags:  []cli.Flag{JSONFlag(), &cli.BoolFlag{Name: "remove", Usage: "take the picture away"}},
				Action: runShoppingSetPicture,
			},
			{Name: "stores", Usage: "the stores your items are for, with how many are needed at each", Flags: []cli.Flag{JSONFlag()}, Action: runShoppingStores},
			{
				Name: "store", Usage: "what to get at one store across your lists, by aisle", ArgsUsage: "<store>",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringSliceFlag{Name: "list", Usage: "only this list; repeat it for more than one (every list with items there by default)"},
					&cli.BoolFlag{Name: "this-store-only", Usage: "leave out the items for any store"},
				},
				Action: runShoppingStore,
			},
			{Name: "trips", Usage: "who is shopping now, on the lists you reach", Flags: []cli.Flag{JSONFlag()}, Action: runShoppingTrips},
			{
				Name: "shop", Usage: "start shopping a store for some lists; the others the lists reach are told", ArgsUsage: "<list>...",
				Flags:  []cli.Flag{JSONFlag(), &cli.StringFlag{Name: "store", Usage: "the store; leave out for none in particular"}},
				Action: runShoppingShop,
			},
			{Name: "done", Usage: "finish shopping: what you put in the cart is bought, no receipt needed", ArgsUsage: "[trip id]", Flags: []cli.Flag{JSONFlag()}, Action: runShoppingDone},
			{Name: "receipt", Usage: "what a Finance receipt did to your lists", ArgsUsage: "<finance receipt id>", Flags: []cli.Flag{JSONFlag()}, Action: runShoppingReceipt},
			{Name: "match-receipt", Usage: "match a receipt to your lists again, for one read before the list had its items", ArgsUsage: "<finance receipt id>", Flags: []cli.Flag{JSONFlag()}, Action: runShoppingMatchReceipt},
			{
				Name: "decide", Usage: "apply a match the matcher was unsure of, or dismiss one (undoing it when it was applied)", ArgsUsage: "<match id> apply|dismiss",
				Flags: []cli.Flag{JSONFlag()}, Action: runShoppingDecide,
			},
			{Name: "match-line", Usage: "tie a receipt line to an item by hand", ArgsUsage: "<item id> <finance receipt line id>", Flags: []cli.Flag{JSONFlag()}, Action: runShoppingMatchLine},
			{Name: "undo-receipt", Usage: "put everything a receipt changed back as it was", ArgsUsage: "<finance receipt id>", Flags: []cli.Flag{JSONFlag()}, Action: runShoppingUndoReceipt},
			{Name: "add-from-receipt", Usage: "put a receipt line on a list as an item", ArgsUsage: "<finance receipt line id> <list>", Flags: []cli.Flag{JSONFlag()}, Action: runShoppingAddFromReceipt},
			{
				Name: "import-preview", Usage: "read a list from a file or pictures into items and show what would be added; nothing is added", ArgsUsage: "[list]",
				Description: "Give the list the items would go on to see which ones merge into items already there.",
				Flags:       append([]cli.Flag{JSONFlag()}, importFlags()...),
				Action:      runShoppingImportPreview,
			},
		},
	}
}

// shoppingListNamed is the list the person typed: by id, else by name among
// the lists they reach. A name more than one list has is refused with the ids.
func shoppingListNamed(ctx context.Context, connection *client.Client, typed string) (*models.ShoppingList, error) {
	typed = strings.TrimSpace(typed)
	lists, err := client.ListShoppingLists(ctx, connection)
	if err != nil {
		return nil, err
	}
	var named []*models.ShoppingList
	for _, list := range lists {
		if list.ID == typed {
			return list, nil
		}
		if strings.EqualFold(list.ListName, typed) {
			named = append(named, list)
		}
	}
	switch len(named) {
	case 0:
		return nil, fmt.Errorf("%q is not a list you reach; 'teanode shopping lists' lists them", typed)
	case 1:
		return named[0], nil
	}
	ids := make([]string, 0, len(named))
	for _, list := range named {
		ids = append(ids, fmt.Sprintf("%s (%s)", list.ID, shoppingOwnerText(list)))
	}
	return nil, fmt.Errorf("more than one list is called %q; give the id of one of %s", typed, strings.Join(ids, ", "))
}

// shoppingListsNamed is shoppingListNamed for each of several, as list ids.
func shoppingListsNamed(ctx context.Context, connection *client.Client, typed []string) ([]string, error) {
	shoppingListIds := make([]string, 0, len(typed))
	for _, each := range typed {
		list, err := shoppingListNamed(ctx, connection, each)
		if err != nil {
			return nil, err
		}
		shoppingListIds = append(shoppingListIds, list.ID)
	}
	return shoppingListIds, nil
}

// shoppingOwnerText is whose a list is, in words.
func shoppingOwnerText(list *models.ShoppingList) string {
	switch {
	case list.IsOwner:
		return "you"
	case list.OwnerName != "":
		return forTerminal(list.OwnerName)
	}
	return forTerminal(list.OwnerUsername)
}

// shoppingQuantityText is an item's count with its unit: 2 kg, 6.
func shoppingQuantityText(item *models.ShoppingItem) string {
	return strings.TrimSpace(item.NeededQuantity + " " + item.QuantityUnit)
}

// shoppingItemWords is an item as one cell: what it is, and whether it is the
// store's own.
func shoppingItemWords(item *models.ShoppingItem) string {
	words := item.ItemDescription
	if item.IsStoreBrand {
		words += " (store brand)"
	}
	return words
}

// shoppingStateWords is where an item is, in words.
func shoppingStateWords(item *models.ShoppingItem) string {
	words := strings.ReplaceAll(string(item.ShoppingItemState), "_", " ")
	if item.OutOfStockAt != nil {
		words += ", out of stock"
		if item.OutOfStockStoreName != "" {
			words += " at " + item.OutOfStockStoreName
		}
	}
	return words
}

// shoppingMomentText is a moment as the day and time it falls on here.
func shoppingMomentText(moment *time.Time) string {
	if moment == nil {
		return ""
	}
	return moment.Local().Format("2006-01-02 15:04")
}

// printShoppingItems writes items as a table. The list's name is a column
// when the items come from more than one list.
func printShoppingItems(items []*models.ShoppingItem, isListShown bool) error {
	headers := []string{"id", "item", "quantity", "store", "aisle", "state", "details"}
	if isListShown {
		headers = append(headers, "list")
	}
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		row := []string{item.ID, shoppingItemWords(item), shoppingQuantityText(item), item.StoreName, item.AisleName, shoppingStateWords(item), item.ItemDetails}
		if isListShown {
			row = append(row, item.ListName)
		}
		rows = append(rows, row)
	}
	return printTable(headers, rows)
}

// shoppingTripWords is a trip in a line: who is shopping where since when.
func shoppingTripWords(trip *models.ShoppingTrip) string {
	shopper := trip.ShopperName
	if shopper == "" {
		shopper = trip.ShopperUsername
	}
	where := "a store"
	if trip.StoreName != "" {
		where = trip.StoreName
	}
	return fmt.Sprintf("%s is shopping %s since %s (trip %s)", forTerminal(shopper), forTerminal(where), shoppingMomentText(&trip.StartedAt), trip.ID)
}

// shoppingArguments is the arguments after the command, at least minimum of
// them, trimmed; fewer is the usage line.
func shoppingArguments(command *cli.Command, minimum int, usageLine string) ([]string, error) {
	arguments := make([]string, 0, command.Args().Len())
	for _, argument := range command.Args().Slice() {
		if argument = strings.TrimSpace(argument); argument != "" {
			arguments = append(arguments, argument)
		}
	}
	if len(arguments) < minimum {
		return nil, usage(usageLine)
	}
	return arguments, nil
}

func runShoppingLists(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	lists, err := client.ListShoppingLists(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		if lists == nil {
			lists = []*models.ShoppingList{}
		}
		return PrintJSON(lists)
	}
	if len(lists) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "no shopping lists yet; 'teanode shopping create <name>' makes one")
		return nil
	}
	rows := make([][]string, 0, len(lists))
	for _, list := range lists {
		isShared := ""
		if list.IsShared {
			isShared = "yes"
		}
		reminder := "on"
		if !list.IsReminded {
			reminder = "off"
		}
		rows = append(rows, []string{list.ID, list.ListName, fmt.Sprint(list.NeededCount), fmt.Sprint(list.InCartCount), fmt.Sprint(list.NotNeededCount), shoppingOwnerText(list), isShared, reminder})
	}
	return printTable([]string{"id", "name", "needed", "in cart", "not needed", "owner", "shared", "reminder"}, rows)
}

func runShoppingShow(ctx context.Context, command *cli.Command) error {
	arguments, err := shoppingArguments(command, 1, "usage: teanode shopping show <list>")
	if err != nil {
		return err
	}
	shoppingItemStates := command.StringSlice("state")
	for _, state := range shoppingItemStates {
		if !models.ShoppingItemState(state).IsValid() {
			return usage(fmt.Sprintf("--state is needed, in_cart or not_needed, not %q", state))
		}
	}
	if command.Int("limit") < 0 || command.Int("limit") > 500 || command.Int("offset") < 0 {
		return usage("--limit is from 1 to 500 and --offset is not negative")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	list, err := shoppingListNamed(ctx, connection, arguments[0])
	if err != nil {
		return describeError(command, err)
	}
	page, err := client.GetShoppingList(ctx, connection, client.ShoppingListQuery{
		ShoppingListID: list.ID, ShoppingItemStates: shoppingItemStates, Offset: int(command.Int("offset")), PageSize: int(command.Int("limit")),
	})
	if err != nil {
		return describeNotFound(command, err, "the list "+arguments[0])
	}
	if command.Bool("json") {
		return PrintJSON(page)
	}
	if err := printFields([][2]string{
		{"id", page.ShoppingList.ID}, {"name", page.ShoppingList.ListName}, {"owner", shoppingOwnerText(page.ShoppingList)},
		{"needed", fmt.Sprint(page.ShoppingList.NeededCount)}, {"in cart", fmt.Sprint(page.ShoppingList.InCartCount)}, {"not needed", fmt.Sprint(page.ShoppingList.NotNeededCount)},
	}); err != nil {
		return err
	}
	for _, trip := range page.ShoppingTrips {
		_, _ = fmt.Fprintln(command.Writer, shoppingTripWords(trip))
	}
	if len(page.ShoppingItems) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "\nno items")
		return nil
	}
	_, _ = fmt.Fprintln(command.Writer)
	if err := printShoppingItems(page.ShoppingItems, false); err != nil {
		return err
	}
	if shown := int(command.Int("offset")) + len(page.ShoppingItems); shown < page.TotalCount {
		fmt.Fprintf(os.Stderr, "note: %d items shown of %d; add --offset %d for the next page\n", len(page.ShoppingItems), page.TotalCount, shown)
	}
	return nil
}

func runShoppingItem(ctx context.Context, command *cli.Command) error {
	arguments, err := shoppingArguments(command, 1, "usage: teanode shopping item <item id>")
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	item, err := client.GetShoppingItem(ctx, connection, arguments[0])
	if err != nil {
		return describeNotFound(command, err, "the item "+arguments[0])
	}
	if command.Bool("json") {
		return PrintJSON(item)
	}
	fields := [][2]string{
		{"id", item.ID}, {"item", shoppingItemWords(item)}, {"list", item.ListName}, {"quantity", shoppingQuantityText(item)},
		{"state", shoppingStateWords(item)}, {"store", item.StoreName}, {"aisle", item.AisleName}, {"details", item.ItemDetails},
	}
	if item.UnitPriceAmount != "" {
		fields = append(fields, [2]string{"latest price", strings.TrimSpace(item.UnitPriceAmount + " " + item.CurrencyCode)})
	}
	if item.CartedByName != "" {
		fields = append(fields, [2]string{"in the cart of", item.CartedByName})
	}
	if item.LastBoughtAt != nil {
		fields = append(fields, [2]string{"last bought", shoppingMomentText(item.LastBoughtAt)})
	}
	if item.PicturePageURL != "" {
		fields = append(fields, [2]string{"picture from", item.PicturePageURL})
	}
	return printFields(fields)
}

func runShoppingCreate(ctx context.Context, command *cli.Command) error {
	arguments, err := shoppingArguments(command, 1, "usage: teanode shopping create <name>")
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	list, err := client.CreateShoppingList(ctx, connection, strings.Join(arguments, " "))
	if err != nil {
		return describeError(command, err)
	}
	return printShoppingList(command, list, "created")
}

func printShoppingList(command *cli.Command, list *models.ShoppingList, verb string) error {
	if command.Bool("json") {
		return PrintJSON(list)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s %s (%s)\n", verb, forTerminal(list.ListName), list.ID)
	return nil
}

func runShoppingRename(ctx context.Context, command *cli.Command) error {
	arguments, err := shoppingArguments(command, 2, "usage: teanode shopping rename <list> <name>")
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	list, err := shoppingListNamed(ctx, connection, arguments[0])
	if err != nil {
		return describeError(command, err)
	}
	renamed, err := client.RenameShoppingList(ctx, connection, list.ID, strings.Join(arguments[1:], " "))
	if err != nil {
		return describeError(command, err)
	}
	return printShoppingList(command, renamed, "renamed to")
}

func runShoppingDelete(ctx context.Context, command *cli.Command) error {
	arguments, err := shoppingArguments(command, 1, "usage: teanode shopping delete <list>")
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	list, err := shoppingListNamed(ctx, connection, arguments[0])
	if err != nil {
		return describeError(command, err)
	}
	if err := confirm(command, fmt.Sprintf("This deletes the list %s with its %d items and their prices, for everybody it is shared with, and cannot be undone.", forTerminal(list.ListName), list.NeededCount+list.InCartCount+list.NotNeededCount)); err != nil {
		return err
	}
	if err := client.DeleteShoppingList(ctx, connection, list.ID); err != nil {
		return describeError(command, err)
	}
	return printShoppingDone(command, map[string]any{"deletedShoppingListId": list.ID}, "deleted the list "+forTerminal(list.ListName))
}

// printShoppingDone prints what a command did: the value as JSON with --json,
// otherwise the sentence.
func printShoppingDone(command *cli.Command, value any, sentence string) error {
	if command.Bool("json") {
		return PrintJSON(value)
	}
	_, _ = fmt.Fprintln(command.Writer, sentence)
	return nil
}

func runShoppingShare(ctx context.Context, command *cli.Command) error {
	arguments, err := shoppingArguments(command, 2, "usage: teanode shopping share <list> <username|circle>")
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	list, err := shoppingListNamed(ctx, connection, arguments[0])
	if err != nil {
		return describeError(command, err)
	}
	granteeUserId, granteeCircleId, described, err := shareTarget(ctx, connection, arguments[1])
	if err != nil {
		return describeError(command, err)
	}
	if err := confirm(command, fmt.Sprintf("Sharing the list %s with %s lets them see, add, change and check off every item on it.", forTerminal(list.ListName), forTerminal(described))); err != nil {
		return err
	}
	share, err := client.ShareShoppingList(ctx, connection, list.ID, granteeUserId, granteeCircleId)
	if err != nil {
		return describeNotFound(command, err, "that list or "+arguments[1])
	}
	return printShoppingDone(command, share, fmt.Sprintf("shared %s with %s (share %s)", forTerminal(list.ListName), forTerminal(described), share.ID))
}

func runShoppingUnshare(ctx context.Context, command *cli.Command) error {
	arguments, err := shoppingArguments(command, 1, "usage: teanode shopping unshare <share id>; 'teanode shopping shares <list>' lists them")
	if err != nil {
		return err
	}
	if err := confirm(command, "This takes the list away from the person or circle it was shared with."); err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := client.UnshareShoppingList(ctx, connection, arguments[0]); err != nil {
		return describeNotFound(command, err, "the share "+arguments[0])
	}
	return printShoppingDone(command, map[string]any{"unsharedShoppingListShareId": arguments[0]}, "no longer shared")
}

func runShoppingShares(ctx context.Context, command *cli.Command) error {
	arguments, err := shoppingArguments(command, 1, "usage: teanode shopping shares <list>")
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	list, err := shoppingListNamed(ctx, connection, arguments[0])
	if err != nil {
		return describeError(command, err)
	}
	shares, err := client.ListShoppingListShares(ctx, connection, list.ID)
	if err != nil {
		return describeNotFound(command, err, "the list "+arguments[0])
	}
	if command.Bool("json") {
		if shares == nil {
			shares = []*models.ShoppingListShare{}
		}
		return PrintJSON(shares)
	}
	if len(shares) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "the list is not shared with anybody")
		return nil
	}
	rows := make([][]string, 0, len(shares))
	for _, share := range shares {
		with := share.GranteeUsername
		if share.GranteeCircleID != "" {
			with = models.DescribeCircleByName(share.GranteeCircleName)
		}
		rows = append(rows, []string{share.ID, with, share.CreatedAt.Local().Format("2006-01-02")})
	}
	return printTable([]string{"id", "with", "since"}, rows)
}

func runShoppingRemind(ctx context.Context, command *cli.Command) error {
	arguments, err := shoppingArguments(command, 1, "usage: teanode shopping remind <list> [--off]")
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	list, err := shoppingListNamed(ctx, connection, arguments[0])
	if err != nil {
		return describeError(command, err)
	}
	isReminded := !command.Bool("off")
	if err := client.SetShoppingListReminder(ctx, connection, list.ID, isReminded); err != nil {
		return describeError(command, err)
	}
	sentence := "you are reminded of " + forTerminal(list.ListName) + " on arriving at a store"
	if !isReminded {
		sentence = "you are no longer reminded of " + forTerminal(list.ListName)
	}
	return printShoppingDone(command, map[string]any{"shoppingListId": list.ID, "isReminded": isReminded}, sentence)
}

func runShoppingStoreRemind(ctx context.Context, command *cli.Command) error {
	arguments, err := shoppingArguments(command, 1, "usage: teanode shopping store-remind <store> [--off]")
	if err != nil {
		return err
	}
	storeName := strings.Join(arguments, " ")
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	isReminded := !command.Bool("off")
	if err := client.SetShoppingStoreReminder(ctx, connection, storeName, isReminded); err != nil {
		return describeError(command, err)
	}
	sentence := "you are reminded of what your lists need on arriving at " + forTerminal(storeName)
	if !isReminded {
		sentence = "you are never reminded at " + forTerminal(storeName)
	}
	return printShoppingDone(command, map[string]any{"storeName": storeName, "isReminded": isReminded}, sentence)
}

// readShoppingImportText reads the text of a list from a file, or from
// standard input for -.
func readShoppingImportText(path string) (string, error) {
	var content []byte
	var err error
	if path == "-" {
		content, err = io.ReadAll(io.LimitReader(os.Stdin, maximumShoppingImportBytes+1))
	} else {
		content, err = readLimitedFile(path, maximumShoppingImportBytes+1)
	}
	if err != nil {
		return "", err
	}
	if len(content) > maximumShoppingImportBytes {
		return "", usage(fmt.Sprintf("%s is larger than %d KB; split the list", path, maximumShoppingImportBytes>>10))
	}
	return string(content), nil
}

// uploadShoppingPicture hands the server a picture as the caller's agent
// attachment and returns its id, which is how bytes reach an operation.
func uploadShoppingPicture(ctx context.Context, command *cli.Command, connection *client.Client, path string) (string, error) {
	content, err := readLimitedFile(path, maximumShoppingPictureBytes+1)
	if err != nil {
		return "", err
	}
	if len(content) > maximumShoppingPictureBytes {
		return "", usage(fmt.Sprintf("%s is larger than %d MB, more than a model takes as a picture", path, maximumShoppingPictureBytes>>20))
	}
	attachment, err := client.UploadAgentAttachment(ctx, connection, filepath.Base(path), content)
	if err != nil {
		return "", describeError(command, err)
	}
	return attachment.ID, nil
}

// previewShoppingImport reads the file and pictures the flags name into
// items, against the list when one is given.
func previewShoppingImport(ctx context.Context, command *cli.Command, connection *client.Client, shoppingListId string) (*client.ShoppingImportView, error) {
	query := client.ShoppingImportQuery{ShoppingListID: shoppingListId}
	if path := strings.TrimSpace(command.String("from-file")); path != "" {
		text, err := readShoppingImportText(path)
		if err != nil {
			return nil, err
		}
		query.ImportText = text
	}
	for _, path := range command.StringSlice("picture") {
		agentAttachmentId, err := uploadShoppingPicture(ctx, command, connection, path)
		if err != nil {
			return nil, err
		}
		query.AgentAttachmentIDs = append(query.AgentAttachmentIDs, agentAttachmentId)
	}
	if query.ImportText == "" && len(query.AgentAttachmentIDs) == 0 {
		return nil, usage("give --from-file or --picture with the list to read")
	}
	preview, err := client.PreviewShoppingImport(ctx, connection, query)
	if err != nil {
		return nil, describeError(command, err)
	}
	return preview, nil
}

// printShoppingImport writes the items a list was read into, and which of them
// merge into an item already on the list.
func printShoppingImport(preview *client.ShoppingImportView) error {
	rows := make([][]string, 0, len(preview.ShoppingItems))
	for _, item := range preview.ShoppingItems {
		words := item.ItemDescription
		if item.IsStoreBrand {
			words += " (store brand)"
		}
		merges := ""
		if item.MergedShoppingItemID != "" {
			merges = fmt.Sprintf("merges into the item already there (%s, %s)", strings.ReplaceAll(item.MergedShoppingItemState, "_", " "), item.MergedNeededQuantity)
		}
		rows = append(rows, []string{words, strings.TrimSpace(item.NeededQuantity + " " + item.QuantityUnit), item.StoreName, item.AisleName, item.ItemDetails, merges})
	}
	return printTable([]string{"item", "quantity", "store", "aisle", "details", "merges"}, rows)
}

func runShoppingImportPreview(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	shoppingListId := ""
	if command.Args().Len() > 0 {
		list, err := shoppingListNamed(ctx, connection, command.Args().First())
		if err != nil {
			return describeError(command, err)
		}
		shoppingListId = list.ID
	}
	preview, err := previewShoppingImport(ctx, command, connection, shoppingListId)
	if err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(preview)
	}
	if len(preview.ShoppingItems) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "the model found no items in it")
		return nil
	}
	return printShoppingImport(preview)
}

func runShoppingAdd(ctx context.Context, command *cli.Command) error {
	arguments, err := shoppingArguments(command, 1, "usage: teanode shopping add <list> [item]... [--from-file <file>] [--picture <file>]")
	if err != nil {
		return err
	}
	isImport := command.String("from-file") != "" || len(command.StringSlice("picture")) > 0
	if len(arguments) == 1 && !isImport {
		return usage("name the items to add, or give --from-file or --picture with a list to read")
	}
	if len(arguments) > 1 && isImport {
		return usage("name the items, or give --from-file or --picture, not both")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	list, err := shoppingListNamed(ctx, connection, arguments[0])
	if err != nil {
		return describeError(command, err)
	}
	var entries []client.ShoppingItemEntry
	if isImport {
		preview, err := previewShoppingImport(ctx, command, connection, list.ID)
		if err != nil {
			return err
		}
		if len(preview.ShoppingItems) == 0 {
			return fmt.Errorf("the model found no items to add")
		}
		mergeCount := 0
		for _, item := range preview.ShoppingItems {
			if item.MergedShoppingItemID != "" {
				mergeCount++
			}
			entries = append(entries, client.ShoppingItemEntry{
				ItemDescription: item.ItemDescription, ItemDetails: item.ItemDetails, StoreName: item.StoreName, IsStoreBrand: item.IsStoreBrand,
				AisleName: item.AisleName, NeededQuantity: item.NeededQuantity, QuantityUnit: item.QuantityUnit,
			})
		}
		if !command.Bool("json") {
			if err := printShoppingImport(preview); err != nil {
				return err
			}
		}
		if err := confirm(command, fmt.Sprintf("This adds %s to %s, of which %d merge into items already there.", countWords(len(entries), "item"), forTerminal(list.ListName), mergeCount)); err != nil {
			return err
		}
	} else {
		for _, description := range arguments[1:] {
			entries = append(entries, client.ShoppingItemEntry{
				ItemDescription: description, ItemDetails: command.String("details"), StoreName: command.String("store"), IsStoreBrand: command.Bool("store-brand"),
				AisleName: command.String("aisle"), NeededQuantity: command.String("quantity"), QuantityUnit: command.String("unit"),
				UnitPriceAmount: command.String("price"), CurrencyCode: command.String("currency"),
			})
		}
	}
	added, err := client.AddShoppingItems(ctx, connection, list.ID, entries)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(added)
	}
	mergedCount := 0
	items := make([]*models.ShoppingItem, 0, len(added))
	for _, each := range added {
		if each.IsMerged {
			mergedCount++
		}
		items = append(items, each.ShoppingItem)
	}
	_, _ = fmt.Fprintf(command.Writer, "added %s to %s, %d merged into items already there\n", countWords(len(added), "item"), forTerminal(list.ListName), mergedCount)
	return printShoppingItems(items, false)
}

func runShoppingEdit(ctx context.Context, command *cli.Command) error {
	arguments, err := shoppingArguments(command, 1, "usage: teanode shopping edit <item id> [--description ...] [--details ...] [--store ...] [--aisle ...] [--quantity ...] [--unit ...] [--store-brand | --not-store-brand]")
	if err != nil {
		return err
	}
	if command.Bool("store-brand") && command.Bool("not-store-brand") {
		return usage("--store-brand and --not-store-brand say opposite things; give one")
	}
	var change models.ShoppingItemChange
	for flag, target := range map[string]**string{
		"description": &change.ItemDescription, "details": &change.ItemDetails, "store": &change.StoreName,
		"aisle": &change.AisleName, "quantity": &change.NeededQuantity, "unit": &change.QuantityUnit,
	} {
		if command.IsSet(flag) {
			value := command.String(flag)
			*target = &value
		}
	}
	if command.IsSet("store-brand") || command.IsSet("not-store-brand") {
		isStoreBrand := command.Bool("store-brand")
		change.IsStoreBrand = &isStoreBrand
	}
	if change == (models.ShoppingItemChange{}) {
		return usage("name what to change: --description, --details, --store, --aisle, --quantity, --unit, --store-brand or --not-store-brand")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	item, err := client.UpdateShoppingItem(ctx, connection, arguments[0], change)
	if err != nil {
		return describeNotFound(command, err, "the item "+arguments[0])
	}
	return printShoppingItem(command, item, "changed")
}

// printShoppingItem prints one item that a command changed.
func printShoppingItem(command *cli.Command, item *models.ShoppingItem, verb string) error {
	if command.Bool("json") {
		return PrintJSON(item)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s %s (%s), %s, %s\n", verb, forTerminal(shoppingItemWords(item)), item.ID, shoppingQuantityText(item), shoppingStateWords(item))
	return nil
}

// eachShoppingItem runs a change on every item id given and prints the items
// as they are after it: one object, or a list for several with --json.
func eachShoppingItem(ctx context.Context, command *cli.Command, usageLine, verb string, change func(connection *client.Client, shoppingItemId string) (*models.ShoppingItem, error)) error {
	arguments, err := shoppingArguments(command, 1, usageLine)
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	items := make([]*models.ShoppingItem, 0, len(arguments))
	for _, shoppingItemId := range arguments {
		item, err := change(connection, shoppingItemId)
		if err != nil {
			return describeNotFound(command, err, "the item "+shoppingItemId)
		}
		items = append(items, item)
	}
	if command.Bool("json") {
		return PrintJSON(items)
	}
	for _, item := range items {
		if err := printShoppingItem(command, item, verb); err != nil {
			return err
		}
	}
	return nil
}

func runShoppingRemove(ctx context.Context, command *cli.Command) error {
	arguments, err := shoppingArguments(command, 1, "usage: teanode shopping remove <item id>...")
	if err != nil {
		return err
	}
	if err := confirm(command, fmt.Sprintf("This takes %s off their lists for good, with their prices and pictures.", countWords(len(arguments), "item"))); err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	removedCount, err := client.RemoveShoppingItems(ctx, connection, arguments)
	if err != nil {
		return describeError(command, err)
	}
	return printShoppingDone(command, map[string]any{"removedCount": removedCount}, "removed "+countWords(removedCount, "item"))
}

func runShoppingCheck(ctx context.Context, command *cli.Command) error {
	return eachShoppingItem(ctx, command, "usage: teanode shopping check <item id>...", "in the cart:", func(connection *client.Client, shoppingItemId string) (*models.ShoppingItem, error) {
		return client.CheckShoppingItem(ctx, connection, shoppingItemId, true, strings.TrimSpace(command.String("store")))
	})
}

func runShoppingUncheck(ctx context.Context, command *cli.Command) error {
	return eachShoppingItem(ctx, command, "usage: teanode shopping uncheck <item id>...", "out of the cart:", func(connection *client.Client, shoppingItemId string) (*models.ShoppingItem, error) {
		return client.CheckShoppingItem(ctx, connection, shoppingItemId, false, "")
	})
}

func runShoppingNeed(ctx context.Context, command *cli.Command) error {
	return eachShoppingItem(ctx, command, "usage: teanode shopping need <item id>... [--quantity <count>]", "needed:", func(connection *client.Client, shoppingItemId string) (*models.ShoppingItem, error) {
		return client.SetShoppingItemNeed(ctx, connection, shoppingItemId, true, command.String("quantity"))
	})
}

func runShoppingNotNeeded(ctx context.Context, command *cli.Command) error {
	return eachShoppingItem(ctx, command, "usage: teanode shopping not-needed <item id>...", "not needed:", func(connection *client.Client, shoppingItemId string) (*models.ShoppingItem, error) {
		return client.SetShoppingItemNeed(ctx, connection, shoppingItemId, false, "")
	})
}

func runShoppingOutOfStock(ctx context.Context, command *cli.Command) error {
	verb := "out of stock:"
	if command.Bool("in-stock") {
		verb = "in stock:"
	}
	return eachShoppingItem(ctx, command, "usage: teanode shopping out-of-stock <item id> [--store <name>] [--in-stock]", verb, func(connection *client.Client, shoppingItemId string) (*models.ShoppingItem, error) {
		return client.SetShoppingItemOutOfStock(ctx, connection, shoppingItemId, !command.Bool("in-stock"), command.String("store"))
	})
}

func runShoppingPrice(ctx context.Context, command *cli.Command) error {
	arguments, err := shoppingArguments(command, 2, "usage: teanode shopping price <item id> <price of one> [--store <name>] [--on <day>] [--currency <code>]")
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	price, err := client.RecordShoppingItemPrice(ctx, connection, client.ShoppingPriceEntry{
		ShoppingItemID: arguments[0], UnitPriceAmount: arguments[1], StoreName: command.String("store"),
		ObservedOn: command.String("on"), CurrencyCode: command.String("currency"),
	})
	if err != nil {
		return describeNotFound(command, err, "the item "+arguments[0])
	}
	return printShoppingDone(command, price, fmt.Sprintf("remembered %s", shoppingPriceWords(price)))
}

// shoppingPriceWords is a price as a phrase: 3.50 USD at a store on a day.
func shoppingPriceWords(price *models.ShoppingItemPrice) string {
	words := strings.TrimSpace(price.UnitPriceAmount + " " + price.CurrencyCode)
	if price.StoreName != "" {
		words += " at " + forTerminal(price.StoreName)
	}
	if price.ObservedOn != "" {
		words += " on " + price.ObservedOn
	}
	return words
}

func runShoppingPrices(ctx context.Context, command *cli.Command) error {
	arguments, err := shoppingArguments(command, 1, "usage: teanode shopping prices <item id>")
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	prices, err := client.ListShoppingItemPrices(ctx, connection, arguments[0])
	if err != nil {
		return describeNotFound(command, err, "the item "+arguments[0])
	}
	if command.Bool("json") {
		if prices == nil {
			prices = []*models.ShoppingItemPrice{}
		}
		return PrintJSON(prices)
	}
	if len(prices) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "no prices remembered for this item")
		return nil
	}
	rows := make([][]string, 0, len(prices))
	for _, price := range prices {
		rows = append(rows, []string{price.ObservedOn, price.StoreName, price.UnitPriceAmount, price.CurrencyCode, price.Quantity, price.QuantityUnit, string(price.PriceSource)})
	}
	return printTable([]string{"day", "store", "price of one", "currency", "quantity", "unit", "from"}, rows)
}

func runShoppingPicture(ctx context.Context, command *cli.Command) error {
	arguments, err := shoppingArguments(command, 1, "usage: teanode shopping picture <item id>")
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := client.FindShoppingItemPicture(ctx, connection, arguments[0]); err != nil {
		return describeNotFound(command, err, "the item "+arguments[0])
	}
	return printShoppingDone(command, map[string]any{"shoppingItemId": arguments[0], "isSearchQueued": true}, "searching for another picture; it arrives in a minute or so")
}

func runShoppingSetPicture(ctx context.Context, command *cli.Command) error {
	arguments, err := shoppingArguments(command, 1, "usage: teanode shopping set-picture <item id> <file>, or <item id> --remove")
	if err != nil {
		return err
	}
	isRemoved := command.Bool("remove")
	if isRemoved == (len(arguments) > 1) {
		return usage("give a picture file, or --remove to take the picture away")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	agentAttachmentId := ""
	if !isRemoved {
		if agentAttachmentId, err = uploadShoppingPicture(ctx, command, connection, arguments[1]); err != nil {
			return err
		}
	}
	item, err := client.SetShoppingItemPicture(ctx, connection, arguments[0], agentAttachmentId)
	if err != nil {
		return describeNotFound(command, err, "the item "+arguments[0])
	}
	if isRemoved {
		return printShoppingItem(command, item, "no picture:")
	}
	return printShoppingItem(command, item, "picture set:")
}

func runShoppingStores(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	stores, err := client.ListShoppingStores(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		if stores == nil {
			stores = []*models.ShoppingStore{}
		}
		return PrintJSON(stores)
	}
	if len(stores) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "none of your items is for a particular store yet")
		return nil
	}
	rows := make([][]string, 0, len(stores))
	for _, store := range stores {
		reminder := "on"
		if !store.IsReminded {
			reminder = "off"
		}
		rows = append(rows, []string{store.StoreName, fmt.Sprint(store.NeededCount), reminder})
	}
	return printTable([]string{"store", "needed", "reminder"}, rows)
}

func runShoppingStore(ctx context.Context, command *cli.Command) error {
	arguments, err := shoppingArguments(command, 1, "usage: teanode shopping store <store> [--list <list>]... [--this-store-only]")
	if err != nil {
		return err
	}
	storeName := strings.Join(arguments, " ")
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	shoppingListIds, err := shoppingListsNamed(ctx, connection, command.StringSlice("list"))
	if err != nil {
		return describeError(command, err)
	}
	query := client.ShoppingStoreQuery{StoreName: storeName, ShoppingListIDs: shoppingListIds}
	if command.Bool("this-store-only") {
		isAnyStoreIncluded := false
		query.IsAnyStoreIncluded = &isAnyStoreIncluded
	}
	view, err := client.ListShoppingStoreItems(ctx, connection, query)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(view)
	}
	reminder := "on"
	if !view.IsReminded {
		reminder = "off"
	}
	_, _ = fmt.Fprintf(command.Writer, "%s, reminder on arriving: %s\n", forTerminal(view.StoreName), reminder)
	for _, trip := range view.ShoppingTrips {
		_, _ = fmt.Fprintln(command.Writer, shoppingTripWords(trip))
	}
	for _, list := range view.ShoppingLists {
		included := "not included"
		if list.IsIncluded {
			included = "included"
		}
		_, _ = fmt.Fprintf(command.Writer, "list %s (%s): %d needed, %s\n", forTerminal(list.ListName), list.ShoppingListID, list.NeededCount, included)
	}
	if len(view.ShoppingItems) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "\nnothing is needed here")
		return nil
	}
	_, _ = fmt.Fprintln(command.Writer)
	return printShoppingItems(view.ShoppingItems, len(view.ShoppingLists) > 1)
}

func runShoppingTrips(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	trips, err := client.ListOpenShoppingTrips(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		if trips == nil {
			trips = []*models.ShoppingTrip{}
		}
		return PrintJSON(trips)
	}
	if len(trips) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "nobody is shopping now")
		return nil
	}
	for _, trip := range trips {
		_, _ = fmt.Fprintln(command.Writer, shoppingTripWords(trip))
	}
	return nil
}

func runShoppingShop(ctx context.Context, command *cli.Command) error {
	arguments, err := shoppingArguments(command, 1, "usage: teanode shopping shop <list>... [--store <name>]")
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	shoppingListIds, err := shoppingListsNamed(ctx, connection, arguments)
	if err != nil {
		return describeError(command, err)
	}
	trip, err := client.StartShopping(ctx, connection, command.String("store"), shoppingListIds)
	if err != nil {
		return describeError(command, err)
	}
	return printShoppingDone(command, trip, fmt.Sprintf("started shopping (trip %s); 'teanode shopping done %s' finishes it", trip.ID, trip.ID))
}

func runShoppingDone(ctx context.Context, command *cli.Command) error {
	arguments, err := shoppingArguments(command, 0, "usage: teanode shopping done [trip id]")
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	// No trip named ends the person's own, or buys their cart without one.
	tripId := ""
	if len(arguments) > 0 {
		tripId = arguments[0]
	}
	trip, err := client.FinishShopping(ctx, connection, tripId)
	if err != nil {
		return describeNotFound(command, err, "the trip "+tripId)
	}
	return printShoppingDone(command, trip, "finished shopping; what you put in the cart is bought")
}

func runShoppingReceipt(ctx context.Context, command *cli.Command) error {
	arguments, err := shoppingArguments(command, 1, "usage: teanode shopping receipt <finance receipt id>")
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	view, err := client.ListShoppingReceiptMatches(ctx, connection, arguments[0])
	if err != nil {
		return describeNotFound(command, err, "the receipt "+arguments[0])
	}
	if command.Bool("json") {
		return PrintJSON(view)
	}
	if err := printFields([][2]string{
		{"receipt", view.FinanceReceiptID}, {"merchant", view.MerchantName}, {"day", view.PurchasedOn}, {"total", strings.TrimSpace(view.TotalAmount + " " + view.CurrencyCode)},
	}); err != nil {
		return err
	}
	if len(view.ShoppingReceiptLines) > 0 {
		_, _ = fmt.Fprintln(command.Writer)
		rows := make([][]string, 0, len(view.ShoppingReceiptLines))
		for _, line := range view.ShoppingReceiptLines {
			matches := make([]string, 0, len(line.ShoppingReceiptMatches))
			for _, match := range line.ShoppingReceiptMatches {
				if match.ShoppingReceiptMatch == nil || match.ShoppingItem == nil {
					continue
				}
				matches = append(matches, fmt.Sprintf("%s %s [match %s]", match.ShoppingReceiptMatch.ReceiptMatchState, match.ShoppingItem.ItemDescription, match.ShoppingReceiptMatch.ID))
			}
			rows = append(rows, []string{line.FinanceReceiptLineID, line.Description, line.Quantity, line.LineAmount, strings.Join(matches, "; ")})
		}
		if err := printTable([]string{"line id", "description", "quantity", "amount", "matches"}, rows); err != nil {
			return err
		}
	}
	if len(view.StillNeededItems) > 0 {
		_, _ = fmt.Fprintln(command.Writer, "\nstill needed at that store:")
		return printShoppingItems(view.StillNeededItems, true)
	}
	return nil
}

func runShoppingMatchReceipt(ctx context.Context, command *cli.Command) error {
	arguments, err := shoppingArguments(command, 1, "usage: teanode shopping match-receipt <finance receipt id>")
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := client.MatchShoppingReceipt(ctx, connection, arguments[0]); err != nil {
		return describeNotFound(command, err, "the receipt "+arguments[0])
	}
	return printShoppingDone(command, map[string]any{"financeReceiptId": arguments[0], "isMatchQueued": true},
		"queued; 'teanode shopping receipt "+arguments[0]+"' shows what it did in a minute or so")
}

func runShoppingDecide(ctx context.Context, command *cli.Command) error {
	arguments, err := shoppingArguments(command, 2, "usage: teanode shopping decide <match id> apply|dismiss")
	if err != nil {
		return err
	}
	if arguments[1] != "apply" && arguments[1] != "dismiss" {
		return usage(fmt.Sprintf("say apply or dismiss, not %q", arguments[1]))
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	match, err := client.DecideShoppingReceiptMatch(ctx, connection, arguments[0], arguments[1] == "apply")
	if err != nil {
		return describeNotFound(command, err, "the match "+arguments[0])
	}
	return printShoppingDone(command, match, fmt.Sprintf("match %s is %s", match.ID, match.ReceiptMatchState))
}

func runShoppingMatchLine(ctx context.Context, command *cli.Command) error {
	arguments, err := shoppingArguments(command, 2, "usage: teanode shopping match-line <item id> <finance receipt line id>")
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	match, err := client.MatchShoppingReceiptLine(ctx, connection, arguments[0], arguments[1])
	if err != nil {
		return describeNotFound(command, err, "that item or receipt line")
	}
	return printShoppingDone(command, match, fmt.Sprintf("tied the receipt line to the item (match %s, %s)", match.ID, match.ReceiptMatchState))
}

func runShoppingUndoReceipt(ctx context.Context, command *cli.Command) error {
	arguments, err := shoppingArguments(command, 1, "usage: teanode shopping undo-receipt <finance receipt id>")
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := client.UndoShoppingReceipt(ctx, connection, arguments[0]); err != nil {
		return describeNotFound(command, err, "the receipt "+arguments[0])
	}
	return printShoppingDone(command, map[string]any{"financeReceiptId": arguments[0], "isUndone": true}, "put everything the receipt changed back as it was")
}

func runShoppingAddFromReceipt(ctx context.Context, command *cli.Command) error {
	arguments, err := shoppingArguments(command, 2, "usage: teanode shopping add-from-receipt <finance receipt line id> <list>")
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	list, err := shoppingListNamed(ctx, connection, arguments[1])
	if err != nil {
		return describeError(command, err)
	}
	item, err := client.AddShoppingItemFromReceiptLine(ctx, connection, arguments[0], list.ID)
	if err != nil {
		return describeNotFound(command, err, "the receipt line "+arguments[0])
	}
	return printShoppingItem(command, item, "added")
}
