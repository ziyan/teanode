package cmd

import (
	"reflect"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/all"
	"github.com/ziyan/teanode/internal/api/v1api/apigraph"
)

// Shopping lists are on the API, the command line and the agent's shopping
// tool under the same names. The live feed (ShoppingEvents) is a subscription
// that the dashboard and the iPhone app follow; a command or a tool call reads
// once, so it has neither. The tool has no action for SetShoppingItemPicture:
// the picture is a file on the person's own device, which the dashboard, the
// app and `teanode shopping set-picture` upload, and an agent has no file of
// the person's to give it.
func TestShoppingParity(test *testing.T) {
	test.Parallel()

	operations := map[string]bool{}
	for _, kind := range []reflect.Type{reflect.TypeFor[apigraph.ShoppingQuery](), reflect.TypeFor[apigraph.ShoppingMutation]()} {
		for index := 0; index < kind.NumMethod(); index++ {
			operations[kind.Method(index).Name] = true
		}
	}

	table := map[string]struct {
		subcommands []string
		actions     []string
	}{
		"GetShoppingItem":                {[]string{"item"}, []string{"item"}},
		"GetShoppingList":                {[]string{"show"}, []string{"list"}},
		"ListOpenShoppingTrips":          {[]string{"trips"}, []string{"trips"}},
		"ListShoppingItemPrices":         {[]string{"prices"}, []string{"prices"}},
		"ListShoppingListShares":         {[]string{"shares"}, []string{"shares"}},
		"ListShoppingLists":              {[]string{"lists"}, []string{"lists"}},
		"ListShoppingReceiptMatches":     {[]string{"receipt"}, []string{"receipt"}},
		"ListShoppingStoreItems":         {[]string{"store"}, []string{"store"}},
		"ListShoppingStores":             {[]string{"stores"}, []string{"stores"}},
		"PreviewShoppingImport":          {[]string{"import-preview"}, []string{"import_preview"}},
		"AddShoppingItemFromReceiptLine": {[]string{"add-from-receipt"}, []string{"add_from_receipt"}},
		"AddShoppingItems":               {[]string{"add"}, []string{"add_items"}},
		"CheckShoppingItem":              {[]string{"check", "uncheck"}, []string{"check", "uncheck"}},
		"CreateShoppingList":             {[]string{"create"}, []string{"create_list"}},
		"DecideShoppingReceiptMatch":     {[]string{"decide"}, []string{"decide_match"}},
		"DeleteShoppingList":             {[]string{"delete"}, []string{"delete_list"}},
		"FindShoppingItemPicture":        {[]string{"picture"}, []string{"find_picture"}},
		"FinishShopping":                 {[]string{"done"}, []string{"finish_shopping"}},
		"MatchShoppingReceipt":           {[]string{"match-receipt"}, []string{"match_receipt"}},
		"MatchShoppingReceiptLine":       {[]string{"match-line"}, []string{"match_line"}},
		"RecordShoppingItemPrice":        {[]string{"price"}, []string{"price"}},
		"RemoveShoppingItems":            {[]string{"remove"}, []string{"remove_items"}},
		"RenameShoppingList":             {[]string{"rename"}, []string{"rename_list"}},
		"SetShoppingItemNeed":            {[]string{"need", "not-needed"}, []string{"need", "not_needed"}},
		"SetShoppingItemOutOfStock":      {[]string{"out-of-stock"}, []string{"out_of_stock"}},
		"SetShoppingItemPicture":         {[]string{"set-picture"}, nil},
		"SetShoppingListReminder":        {[]string{"remind"}, []string{"remind"}},
		"SetShoppingStoreReminder":       {[]string{"store-remind"}, []string{"store_remind"}},
		"ShareShoppingList":              {[]string{"share"}, []string{"share"}},
		"StartShopping":                  {[]string{"shop"}, []string{"start_shopping"}},
		"UndoShoppingReceipt":            {[]string{"undo-receipt"}, []string{"undo_receipt"}},
		"UnshareShoppingList":            {[]string{"unshare"}, []string{"unshare"}},
		"UpdateShoppingItem":             {[]string{"edit"}, []string{"update_item"}},
	}
	for operation := range operations {
		if _, isListed := table[operation]; !isListed {
			test.Errorf("the table does not list the operation %s", operation)
		}
	}

	commands := map[string]*cli.Command{}
	for _, subcommand := range NewShoppingCommand().Commands {
		commands[subcommand.Name] = subcommand
	}
	properties := map[string]any{}
	actions := map[string]bool{}
	for _, tool := range tools.Build().All() {
		if tool.Name != "shopping" {
			continue
		}
		properties, _ = tool.Parameters["properties"].(map[string]any)
		action, _ := properties["action"].(map[string]any)
		switch enum := action["enum"].(type) {
		case []string:
			for _, name := range enum {
				actions[name] = true
			}
		case []any:
			for _, name := range enum {
				actions[name.(string)] = true
			}
		}
	}
	if len(actions) == 0 {
		test.Fatal("the agent has no shopping tool")
	}
	knownSubcommands := map[string]bool{}
	for operation, names := range table {
		if !operations[operation] {
			test.Errorf("the API has no operation %s", operation)
		}
		for _, subcommand := range names.subcommands {
			knownSubcommands[subcommand] = true
			if commands[subcommand] == nil {
				test.Errorf("teanode shopping has no %s", subcommand)
			}
		}
		for _, action := range names.actions {
			if !actions[action] {
				test.Errorf("the shopping tool has no action %s", action)
			}
		}
	}
	for action := range actions {
		isKnown := false
		for _, names := range table {
			for _, each := range names.actions {
				isKnown = isKnown || each == action
			}
		}
		if !isKnown {
			test.Errorf("the shopping tool action %s is not an operation of the API", action)
		}
	}
	for name := range commands {
		if !knownSubcommands[name] {
			test.Errorf("teanode shopping %s is not an operation of the API", name)
		}
	}

	// Every argument of an operation is a flag of one of its subcommands and a
	// property of its tool, under the name that surface gives it, or is exempt
	// with the reason. The tool's properties are one set for all its actions,
	// as the model is given them.
	flagNames := map[string]string{
		"shoppingItemStates": "state", "offset": "offset", "pageSize": "limit", "storeName": "store", "shoppingListIds": "list",
		"isAnyStoreIncluded": "this-store-only", "importText": "from-file", "agentAttachmentIds": "picture", "isReminded": "off",
		"itemDescription": "description", "itemDetails": "details", "isStoreBrand": "store-brand", "aisleName": "aisle",
		"neededQuantity": "quantity", "quantityUnit": "unit", "isOutOfStock": "in-stock", "observedOn": "on", "currencyCode": "currency",
	}
	propertyNames := map[string]string{
		"shoppingListId": "list_id", "shoppingListIds": "list_ids", "shoppingItemId": "item_id", "shoppingItemIds": "item_ids",
		"shoppingItemStates": "shopping_item_states", "offset": "offset", "pageSize": "page_size", "storeName": "store_name",
		"isAnyStoreIncluded": "is_any_store_included", "importText": "import_text", "agentAttachmentIds": "attachment_ids",
		"listName": "list_name", "granteeUserId": "with", "granteeCircleId": "with", "shoppingListShareId": "share_id",
		"isReminded": "is_reminded", "shoppingItems": "items", "itemDescription": "item_description", "itemDetails": "item_details",
		"isStoreBrand": "is_store_brand", "aisleName": "aisle_name", "neededQuantity": "needed_quantity", "quantityUnit": "quantity_unit",
		"isOutOfStock": "is_out_of_stock", "unitPriceAmount": "unit_price_amount", "observedOn": "observed_on", "currencyCode": "currency_code",
		"shoppingTripId": "trip_id", "financeReceiptId": "receipt_id", "financeReceiptLineId": "receipt_line_id",
		"shoppingReceiptMatchId": "match_id", "isApplied": "is_applied",
	}
	// "<Operation>.<argument>.<surface>" or "<argument>.<surface>" is exempt
	// for the reason given.
	exempt := map[string]string{
		"shoppingListId.command":       "the list is an argument of the subcommand, by name or id",
		"shoppingItemId.command":       "the item is an argument of the subcommand",
		"shoppingItemIds.command":      "the items are the arguments of the subcommand",
		"shoppingListShareId.command":  "the share is the argument of the subcommand",
		"shoppingTripId.command":       "the trip is the argument of the subcommand",
		"financeReceiptId.command":     "the receipt is the argument of the subcommand",
		"financeReceiptLineId.command": "the receipt line is an argument of the subcommand",
		"granteeUserId.command":        "who to share with is the argument of the subcommand, a username or a circle",
		"granteeCircleId.command":      "who to share with is the argument of the subcommand, a username or a circle",
		"unitPriceAmount.command":      "the price is an argument of the subcommand",
		"isChecked.command":            "the subcommands are check and uncheck",
		"isChecked.tool":               "the actions are check and uncheck",
		"isNeeded.command":             "the subcommands are need and not-needed",
		"isNeeded.tool":                "the actions are need and not_needed",

		"ListShoppingStoreItems.storeName.command":                  "the store is the argument of the subcommand",
		"SetShoppingStoreReminder.storeName.command":                "the store is the argument of the subcommand",
		"StartShopping.shoppingListIds.command":                     "the lists are the arguments of the subcommand",
		"CreateShoppingList.listName.command":                       "the name is the argument of the subcommand",
		"RenameShoppingList.listName.command":                       "the name is an argument of the subcommand",
		"AddShoppingItems.shoppingItems.command":                    "the items are arguments of the subcommand, with flags for their fields",
		"DecideShoppingReceiptMatch.shoppingReceiptMatchId.command": "the match is an argument of the subcommand",
		"DecideShoppingReceiptMatch.isApplied.command":              "apply or dismiss is an argument of the subcommand",
		"SetShoppingItemPicture.agentAttachmentId.command":          "the picture is a file argument of the subcommand, uploaded by it",
		"SetShoppingItemPicture.agentAttachmentId.tool":             "the tool has no set-picture action: an agent has no file of the person's to give",
	}
	for operation, names := range table {
		var argumentsType reflect.Type
		for _, kind := range []reflect.Type{reflect.TypeFor[apigraph.ShoppingQuery](), reflect.TypeFor[apigraph.ShoppingMutation]()} {
			if method, isMethod := kind.MethodByName(operation); isMethod && method.Type.NumIn() >= 2 {
				argumentsType = method.Type.In(1)
			}
		}
		if argumentsType == nil {
			continue
		}
		flags := map[string]bool{}
		for _, subcommand := range names.subcommands {
			if commands[subcommand] == nil {
				continue
			}
			for _, flag := range commands[subcommand].Flags {
				for _, name := range flag.Names() {
					flags[name] = true
				}
			}
		}
		for index := 0; index < argumentsType.NumField(); index++ {
			argument, _, _ := strings.Cut(argumentsType.Field(index).Tag.Get("json"), ",")
			_, isCommandExemptHere := exempt[operation+"."+argument+".command"]
			_, isCommandExempt := exempt[argument+".command"]
			if !isCommandExemptHere && !isCommandExempt && !flags[flagNames[argument]] {
				test.Errorf("teanode shopping %s has no flag for the argument %s of %s", strings.Join(names.subcommands, " or "), argument, operation)
			}
			_, isToolExemptHere := exempt[operation+"."+argument+".tool"]
			_, isToolExempt := exempt[argument+".tool"]
			if len(names.actions) > 0 && !isToolExemptHere && !isToolExempt && properties[propertyNames[argument]] == nil {
				test.Errorf("the shopping tool action %s has no property for the argument %s of %s", strings.Join(names.actions, " or "), argument, operation)
			}
		}
	}
	for key, reason := range exempt {
		if strings.TrimSpace(reason) == "" {
			test.Errorf("the exemption %s needs a reason", key)
		}
	}
}
