package models

import (
	"strings"
	"time"
	"unicode"
)

// ShoppingItemState is where an item on a shopping list is: to buy, checked
// off at the store, or bought or not wanted now. A bought item stays on its
// list, so a list is reused week after week.
type ShoppingItemState string

const (
	// ShoppingItemStateNeeded is an item to buy, NeededQuantity of it.
	ShoppingItemStateNeeded ShoppingItemState = "needed"

	// ShoppingItemStateInCart is an item checked off at the store. It stays
	// in the cart until the trip is finished, a receipt matches it, or a new
	// trip on its list starts long after; then it is bought.
	ShoppingItemStateInCart ShoppingItemState = "in_cart"

	// ShoppingItemStateNotNeeded is an item that was bought, or is not wanted
	// now. "Need again" makes it needed.
	ShoppingItemStateNotNeeded ShoppingItemState = "not_needed"
)

// ShoppingItemStates is every state, in the order a list shows them.
var ShoppingItemStates = []ShoppingItemState{ShoppingItemStateNeeded, ShoppingItemStateInCart, ShoppingItemStateNotNeeded}

// IsValid reports whether the state is one of the three.
func (self ShoppingItemState) IsValid() bool {
	switch self {
	case ShoppingItemStateNeeded, ShoppingItemStateInCart, ShoppingItemStateNotNeeded:
		return true
	}
	return false
}

// ShoppingPictureSource is where an item's picture came from.
type ShoppingPictureSource string

const (
	// ShoppingPictureSourceWeb is a picture the agent found on a web page.
	ShoppingPictureSourceWeb ShoppingPictureSource = "web"

	// ShoppingPictureSourceUpload is a picture a person gave.
	ShoppingPictureSourceUpload ShoppingPictureSource = "upload"
)

// ShoppingPriceSource is where a price came from.
type ShoppingPriceSource string

const (
	// ShoppingPriceSourceReceipt is a price read from a receipt.
	ShoppingPriceSourceReceipt ShoppingPriceSource = "receipt"

	// ShoppingPriceSourcePerson is a price a person typed.
	ShoppingPriceSourcePerson ShoppingPriceSource = "person"
)

// ReceiptMatchState is what became of a receipt line tied to a shopping item.
type ReceiptMatchState string

const (
	// ReceiptMatchStateApplied checked the item off and wrote its price.
	ReceiptMatchStateApplied ReceiptMatchState = "applied"

	// ReceiptMatchStateProposed is a match the matcher was unsure of, left
	// for a person to apply or dismiss.
	ReceiptMatchStateProposed ReceiptMatchState = "proposed"

	// ReceiptMatchStateDismissed was undone or refused, and is never
	// proposed again.
	ReceiptMatchStateDismissed ReceiptMatchState = "dismissed"
)

// ShoppingStoreKey is the form every store name is compared in: lower case,
// letters and digits only, so "Example Market", "EXAMPLE MARKET'S" and
// "example-market" are one store. A name with no letter or digit has the empty key,
// which is any store.
func ShoppingStoreKey(storeName string) string {
	var builder strings.Builder
	for _, character := range strings.ToLower(storeName) {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			builder.WriteRune(character)
		}
	}
	return builder.String()
}

// ShoppingList is one person's named list of things to buy.
type ShoppingList struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userId"`
	ListName  string    `json:"listName"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	// Whose it is, for a list somebody shared with the reader.
	OwnerUsername string `json:"ownerUsername"`
	OwnerName     string `json:"ownerName"`

	// What the reader may do with it and how they see it.
	IsOwner    bool `json:"isOwner"`
	IsReminded bool `json:"isReminded"`

	// How many of its items are in each state.
	NeededCount    int `json:"neededCount"`
	InCartCount    int `json:"inCartCount"`
	NotNeededCount int `json:"notNeededCount"`

	// Whether anybody besides the owner has it.
	IsShared bool `json:"isShared"`
}

// ShoppingListAccess is how a person reaches a list.
type ShoppingListAccess struct {
	ShoppingListID string
	OwnerUserID    string
	IsOwner        bool
}

// ShoppingListShare gives one person or one circle a list.
type ShoppingListShare struct {
	ID              string    `json:"id"`
	ShoppingListID  string    `json:"shoppingListId"`
	GranteeUserID   string    `json:"granteeUserId"`
	GranteeCircleID string    `json:"granteeCircleId"`
	CreatedByUserID string    `json:"createdByUserId"`
	CreatedAt       time.Time `json:"createdAt"`

	// Names for listings: the list, whose it is, and who it is for.
	ListName          string `json:"listName"`
	OwnerUserID       string `json:"ownerUserId"`
	OwnerUsername     string `json:"ownerUsername"`
	OwnerName         string `json:"ownerName"`
	GranteeUsername   string `json:"granteeUsername"`
	GranteeName       string `json:"granteeName"`
	GranteeCircleName string `json:"granteeCircleName"`
}

// ShoppingItem is one thing on one list.
type ShoppingItem struct {
	ID              string `json:"id"`
	ShoppingListID  string `json:"shoppingListId"`
	ItemDescription string `json:"itemDescription"`
	ItemDetails     string `json:"itemDetails"`

	// StoreName is the store to get it from, empty for any store.
	StoreName    string `json:"storeName"`
	StoreKey     string `json:"storeKey"`
	IsStoreBrand bool   `json:"isStoreBrand"`
	AisleName    string `json:"aisleName"`
	AisleRank    int    `json:"aisleRank"`
	ItemRank     int    `json:"itemRank"`

	// NeededQuantity is a decimal above zero, as text ("2", "1.5").
	NeededQuantity string `json:"neededQuantity"`
	QuantityUnit   string `json:"quantityUnit"`

	// UnitPriceAmount is the latest price seen, a decimal as text, empty
	// when none is known.
	UnitPriceAmount string `json:"unitPriceAmount"`
	CurrencyCode    string `json:"currencyCode"`

	ShoppingItemState ShoppingItemState `json:"shoppingItemState"`
	CartedByUserID    string            `json:"cartedByUserId"`
	CartedAt          *time.Time        `json:"cartedAt"`

	OutOfStockAt        *time.Time `json:"outOfStockAt"`
	OutOfStockByUserID  string     `json:"outOfStockByUserId"`
	OutOfStockStoreName string     `json:"outOfStockStoreName"`
	LastBoughtAt        *time.Time `json:"lastBoughtAt"`

	PictureFileID     string                `json:"pictureFileId"`
	PicturePageURL    string                `json:"picturePageUrl"`
	PictureSource     ShoppingPictureSource `json:"pictureSource"`
	PictureSearchedAt *time.Time            `json:"pictureSearchedAt"`

	// PicturePath is where a signed-in reader of the list gets the picture
	// from this server, empty when there is none. Filled for a reader, not
	// stored.
	PicturePath string `json:"picturePath"`

	CreatedByUserID string    `json:"createdByUserId"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`

	// Names a reader is shown: the list it is on, and who put it in the cart
	// or found it out of stock.
	ListName         string `json:"listName"`
	CartedByName     string `json:"cartedByName"`
	OutOfStockByName string `json:"outOfStockByName"`
}

// ShoppingItemChange is what a change to an item sets; a nil field is left
// as it is.
type ShoppingItemChange struct {
	ItemDescription *string
	ItemDetails     *string
	StoreName       *string
	IsStoreBrand    *bool
	AisleName       *string
	NeededQuantity  *string
	QuantityUnit    *string
}

// ShoppingItemPrice is one price seen for an item at a store.
type ShoppingItemPrice struct {
	ID               string              `json:"id"`
	ShoppingItemID   string              `json:"shoppingItemId"`
	StoreName        string              `json:"storeName"`
	StoreKey         string              `json:"storeKey"`
	UnitPriceAmount  string              `json:"unitPriceAmount"`
	CurrencyCode     string              `json:"currencyCode"`
	Quantity         string              `json:"quantity"`
	QuantityUnit     string              `json:"quantityUnit"`
	ObservedOn       string              `json:"observedOn"`
	PriceSource      ShoppingPriceSource `json:"priceSource"`
	FinanceReceiptID string              `json:"financeReceiptId"`
	CreatedByUserID  string              `json:"createdByUserId"`
	CreatedAt        time.Time           `json:"createdAt"`
}

// ShoppingReceiptMatch ties a line of a receipt to a shopping item.
type ShoppingReceiptMatch struct {
	ID                        string            `json:"id"`
	FinanceReceiptID          string            `json:"financeReceiptId"`
	FinanceReceiptLineID      string            `json:"financeReceiptLineId"`
	ShoppingItemID            string            `json:"shoppingItemId"`
	ReceiptMatchState         ReceiptMatchState `json:"receiptMatchState"`
	MatchedQuantity           string            `json:"matchedQuantity"`
	PreviousShoppingItemState ShoppingItemState `json:"previousShoppingItemState"`
	PreviousNeededQuantity    string            `json:"previousNeededQuantity"`
	ShoppingItemPriceID       string            `json:"shoppingItemPriceId"`
	DecidedByUserID           string            `json:"decidedByUserId"`
	CreatedAt                 time.Time         `json:"createdAt"`
	DecidedAt                 *time.Time        `json:"decidedAt"`
}

// ShoppingReceiptMatchProposal is one tie the matcher would make: a line, an
// item, how many of it the line bought (empty when the line does not say),
// and whether the matcher is sure.
type ShoppingReceiptMatchProposal struct {
	FinanceReceiptLineID string
	ShoppingItemID       string
	MatchedQuantity      string
	IsSure               bool
}

// ShoppingTrip is somebody shopping a store now, for some lists.
type ShoppingTrip struct {
	ID              string     `json:"id"`
	UserID          string     `json:"userId"`
	StoreName       string     `json:"storeName"`
	StoreKey        string     `json:"storeKey"`
	StartedAt       time.Time  `json:"startedAt"`
	FinishedAt      *time.Time `json:"finishedAt"`
	ShoppingListIDs []string   `json:"shoppingListIds"`

	// Who is shopping, for the others.
	ShopperUsername string `json:"shopperUsername"`
	ShopperName     string `json:"shopperName"`
}

// ShoppingStore is a store some of the reader's items are for, with how many
// of them are needed there.
type ShoppingStore struct {
	StoreName   string `json:"storeName"`
	StoreKey    string `json:"storeKey"`
	NeededCount int    `json:"neededCount"`
	IsReminded  bool   `json:"isReminded"`
}

// ShoppingTripStaleAfter is how long after it started an unfinished trip is
// taken as over: a new trip on one of its lists finishes it, and what it had
// in the cart is bought.
const ShoppingTripStaleAfter = 6 * time.Hour
