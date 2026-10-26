package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/push"
)

// The two notifications shopping lists send: what the person's lists need at
// a store they just arrived at, and somebody starting to shop a list the
// person shares.

const (
	// shoppingArrivalInterval is how long after reminding somebody at a place
	// they are not reminded there again, so a visit split by a walk to the car
	// is one reminder.
	shoppingArrivalInterval = 3 * time.Hour

	// shoppingStartedInterval is how long after telling somebody that a
	// person started shopping they are not told it again about that person.
	shoppingStartedInterval = 2 * time.Hour

	// shoppingArrivalNamesMost is how many items a reminder names for each
	// list before it says how many more there are.
	shoppingArrivalNamesMost = 5
)

// shoppingNotices remembers whom this instance told what, and when, so the
// same reminder is not sent twice in a row. A restart forgets it, which costs
// at most one repeated notice.
type shoppingNotices struct {
	mutex  sync.Mutex
	toldAt map[string]time.Time
}

// claim says whether the notice named by key may be sent now, and marks it
// sent when it may.
func (self *shoppingNotices) claim(key string, interval time.Duration, now time.Time) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.toldAt == nil {
		self.toldAt = map[string]time.Time{}
	}
	for each, toldAt := range self.toldAt {
		if now.Sub(toldAt) >= shoppingArrivalInterval {
			delete(self.toldAt, each)
		}
	}
	if toldAt, isTold := self.toldAt[key]; isTold && now.Sub(toldAt) < interval {
		return false
	}
	self.toldAt[key] = now
	return true
}

// groceryCategories are the OpenStreetMap categories of a place where things
// for any store are bought too: tomatoes at a supermarket, not at a pharmacy.
var groceryCategories = map[string]bool{
	"shop=supermarket": true, "shop=grocery": true, "shop=greengrocer": true, "shop=convenience": true,
	"shop=wholesale": true, "shop=farm": true, "shop=health_food": true, "amenity=marketplace": true,
}

// ShoppingStoreOfPlace is the store a place is, by the name the person gave it
// or, when they gave none, its name on the map; and whether things for any
// store are bought there.
func ShoppingStoreOfPlace(place *models.Place) (string, bool) {
	if place == nil {
		return "", false
	}
	storeName := place.OSMName
	if place.IsNamedByPerson && place.PlaceName != "" || storeName == "" {
		storeName = place.PlaceName
	}
	return strings.TrimSpace(storeName), groceryCategories[place.OSMCategory]
}

// What the notifications say, around the names.
var (
	phraseShoppingAt        = push.Phrase{English: "At %s", Chinese: "在%s", Japanese: "%sにいます"}
	phraseShoppingOneList   = push.Phrase{English: "%s needs %s", Chinese: "%s需要%s", Japanese: "%sに必要なもの：%s"}
	phraseShoppingMore      = push.Phrase{English: "%s and %d more", Chinese: "%s等%d样", Japanese: "%sなど、ほか%d点"}
	phraseShoppingStarted   = push.Phrase{English: "%s started shopping %s", Chinese: "%s开始采购%s", Japanese: "%sが%sの買い物を始めました"}
	phraseShoppingStartedAt = push.Phrase{English: "At %s.", Chinese: "在%s。", Japanese: "%sにいます。"}
)

// shoppingNamesLine names the first items of a list, and how many more.
func shoppingNamesLine(names []string, language string) string {
	if len(names) <= shoppingArrivalNamesMost {
		return strings.Join(names, ", ")
	}
	return phraseShoppingMore.In(language, strings.Join(names[:shoppingArrivalNamesMost], ", "), len(names)-shoppingArrivalNamesMost)
}

// shoppingArrivalBody says what each list needs, one sentence per list, in
// the order the lists were first met.
func shoppingArrivalBody(listOrder []string, names map[string][]string, language string) string {
	sentences := make([]string, 0, len(listOrder))
	for _, listName := range listOrder {
		sentences = append(sentences, phraseShoppingOneList.In(language, listName, shoppingNamesLine(names[listName], language))+".")
	}
	return PhoneBody(strings.Join(sentences, " "))
}

// NotifyShoppingArrival reminds the person, on their own phones, of what their
// lists need at the store they just arrived at: the items for that store, and
// the items for any store when the place is a grocery shop. Nothing is sent
// for a place that is not a store any item names, a store the person asked
// never to be reminded at, a list they muted, an arrival more than
// placeVisitNoticeWindow ago, or a second time within shoppingArrivalInterval.
// The sending is in the background, as NotifyPhones is.
func (self *PhoneNotifier) NotifyShoppingArrival(ctx context.Context, userId string, visit *models.PlaceVisit, happenedAt time.Time) {
	if visit == nil || userId == "" || time.Since(happenedAt) > placeVisitNoticeWindow {
		return
	}
	var storeName string
	var items []*models.ShoppingItem
	var agentId string
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		place := visit.Place
		if place == nil {
			var err error
			if place, err = tx.GetPlace(userId, visit.PlaceID); err != nil || place == nil {
				return err
			}
		}
		var isGrocery bool
		storeName, isGrocery = ShoppingStoreOfPlace(place)
		storeKey := models.ShoppingStoreKey(storeName)
		if storeKey == "" {
			return nil
		}
		muted, err := tx.ListShoppingStoreMuteKeys(userId)
		if err != nil {
			return err
		}
		for _, key := range muted {
			if key == storeKey {
				return nil
			}
		}
		if items, err = tx.ListShoppingItemsNeededAt(userId, storeKey, isGrocery); err != nil || len(items) == 0 {
			return err
		}
		agent, err := tx.GetAgentByUser(userId)
		if err != nil || agent == nil {
			items = nil
			return err
		}
		agentId = agent.ID
		return nil
	}); err != nil {
		log.Warningf("cannot work out what the shopping lists need at a store: %s", err)
		return
	}
	if len(items) == 0 || agentId == "" {
		return
	}
	if !self.shoppingNotices.claim("arrival "+userId+" "+visit.PlaceID, shoppingArrivalInterval, time.Now()) {
		return
	}
	listOrder := []string{}
	names := map[string][]string{}
	for _, item := range items {
		if _, isMet := names[item.ListName]; !isMet {
			listOrder = append(listOrder, item.ListName)
		}
		name := item.ItemDescription
		if item.NeededQuantity != "" && item.NeededQuantity != "1" {
			name = item.NeededQuantity + " " + name
		}
		names[item.ListName] = append(names[item.ListName], name)
	}
	self.NotifyPhones(agentId, models.NotificationShopping, &push.Content{
		NotificationKind:  models.NotificationShopping,
		Title:             PhoneBody(phraseShoppingAt.In(push.LanguageEnglish, storeName)),
		Body:              shoppingArrivalBody(listOrder, names, push.LanguageEnglish),
		ShoppingStoreName: storeName,
		ThreadID:          "shopping-arrival",
		Wording: func(content *push.Content, language string) {
			content.Title = PhoneBody(phraseShoppingAt.In(language, storeName))
			content.Body = shoppingArrivalBody(listOrder, names, language)
		},
	})
}

// NotifyShoppingStarted tells everybody the trip's lists reach, but the
// shopper, that the shopper started shopping, naming the lists each of them
// shares and the store, so they can add what they forgot. Somebody told
// within shoppingStartedInterval about the same shopper is not told again.
func (self *PhoneNotifier) NotifyShoppingStarted(ctx context.Context, shopper *models.User, trip *models.ShoppingTrip) {
	if shopper == nil || trip == nil || len(trip.ShoppingListIDs) == 0 {
		return
	}
	listNames := map[string][]string{}
	firstListIds := map[string]string{}
	agentIds := map[string]string{}
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		for _, listId := range trip.ShoppingListIDs {
			list, err := tx.GetShoppingList(shopper.ID, listId)
			if err != nil {
				return err
			}
			readers, err := tx.ListShoppingListReaderUserIDs(listId)
			if err != nil {
				return err
			}
			for _, readerId := range readers {
				if readerId == shopper.ID {
					continue
				}
				listNames[readerId] = append(listNames[readerId], list.ListName)
				if firstListIds[readerId] == "" {
					firstListIds[readerId] = listId
				}
			}
		}
		for readerId := range listNames {
			agent, err := tx.GetAgentByUser(readerId)
			if err != nil {
				return err
			}
			if agent != nil {
				agentIds[readerId] = agent.ID
			}
		}
		return nil
	}); err != nil {
		log.Warningf("cannot work out who to tell about somebody shopping: %s", err)
		return
	}
	now := time.Now()
	for readerId, names := range listNames {
		agentId := agentIds[readerId]
		if agentId == "" || !self.shoppingNotices.claim(fmt.Sprintf("started %s %s", shopper.ID, readerId), shoppingStartedInterval, now) {
			continue
		}
		listWords := strings.Join(names, ", ")
		shopperName, storeName := shopper.DisplayName(), trip.StoreName
		body := ""
		if storeName != "" {
			body = phraseShoppingStartedAt.In(push.LanguageEnglish, storeName)
		}
		self.NotifyPhones(agentId, models.NotificationShopping, &push.Content{
			NotificationKind: models.NotificationShopping,
			Title:            PhoneBody(phraseShoppingStarted.In(push.LanguageEnglish, shopperName, listWords)),
			Body:             PhoneBody(body),
			ShoppingListID:   firstListIds[readerId],
			ThreadID:         "shopping-" + firstListIds[readerId],
			Wording: func(content *push.Content, language string) {
				content.Title = PhoneBody(phraseShoppingStarted.In(language, shopperName, listWords))
				if storeName != "" {
					content.Body = PhoneBody(phraseShoppingStartedAt.In(language, storeName))
				}
			},
		})
	}
}
