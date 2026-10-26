package apigraph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/mux"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
	"github.com/ziyan/teanode/internal/util/deferutil"
)

const (
	// shoppingEventSubscriptionsMost is how many screens one account may have
	// following its lists at once on this instance.
	shoppingEventSubscriptionsMost = 20

	// shoppingEventBacklog is how many events a subscriber that has stopped
	// reading holds before it starts losing them; a screen that missed some
	// reads its list again.
	shoppingEventBacklog = 32

	// shoppingEventDecisionWait bounds the reading of who may see an event.
	shoppingEventDecisionWait = 10 * time.Second
)

// shoppingEventHub is the one LISTEN this process holds for shopping list
// changes, fanned out in memory to the screens open on this instance, as the
// location hub does for maps. The listener is held only while somebody
// follows a list. The zero value is ready to use.
type shoppingEventHub struct {
	mutex            sync.Mutex
	subscribers      map[int]*shoppingEventSubscriber
	nextSubscriberId int
	cancel           context.CancelFunc
}

// shoppingEventSubscriber is one open screen.
type shoppingEventSubscriber struct {
	viewerUserId string
	events       chan db.ShoppingEvent
}

// subscribe adds a screen for the viewer, and starts the listener if this is
// the first one. The returned function removes it, and stops the listener if
// it was the last.
func (self *shoppingEventHub) subscribe(database db.Database, viewerUserId string) (<-chan db.ShoppingEvent, func(), error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	openCount := 0
	for _, each := range self.subscribers {
		if each.viewerUserId == viewerUserId {
			openCount++
		}
	}
	if openCount >= shoppingEventSubscriptionsMost {
		return nil, nil, fmt.Errorf("%w: this account already follows its shopping lists in %d places; close one first", api.ErrInvalidArguments, shoppingEventSubscriptionsMost)
	}
	if self.cancel == nil {
		listenContext, cancel := context.WithCancel(context.Background())
		payloads, err := database.ListenShoppingEvents(listenContext)
		if err != nil {
			cancel()
			return nil, nil, err
		}
		self.cancel = cancel
		go func() {
			defer deferutil.Recover()
			for payload := range payloads {
				self.deliver(database, payload)
			}
		}()
	}
	if self.subscribers == nil {
		self.subscribers = map[int]*shoppingEventSubscriber{}
	}
	subscriber := &shoppingEventSubscriber{viewerUserId: viewerUserId, events: make(chan db.ShoppingEvent, shoppingEventBacklog)}
	id := self.nextSubscriberId
	self.nextSubscriberId++
	self.subscribers[id] = subscriber
	return subscriber.events, func() {
		self.mutex.Lock()
		defer self.mutex.Unlock()
		delete(self.subscribers, id)
		if len(self.subscribers) == 0 && self.cancel != nil {
			self.cancel()
			self.cancel = nil
		}
	}, nil
}

// deliver hands one event to the screens that may see it: those of the people
// the list reaches now, decided for all of them by one query, and those the
// event says have just lost it, so they read again and drop it.
func (self *shoppingEventHub) deliver(database db.Database, payload string) {
	var event db.ShoppingEvent
	if json.Unmarshal([]byte(payload), &event) != nil || event.ShoppingListID == "" {
		return
	}
	self.mutex.Lock()
	subscribers := make([]*shoppingEventSubscriber, 0, len(self.subscribers))
	for _, each := range self.subscribers {
		subscribers = append(subscribers, each)
	}
	self.mutex.Unlock()
	if len(subscribers) == 0 {
		return
	}
	isAllowed := map[string]bool{}
	for _, userId := range event.EndedForUserIDs {
		isAllowed[userId] = true
	}
	ctx, cancel := context.WithTimeout(context.Background(), shoppingEventDecisionWait)
	defer cancel()
	if err := database.TransactionContext(ctx, func(tx db.Transaction) error {
		readers, err := tx.ListShoppingListReaderUserIDs(event.ShoppingListID)
		if err != nil {
			return err
		}
		for _, userId := range readers {
			isAllowed[userId] = true
		}
		return nil
	}); err != nil {
		log.Warningf("cannot work out who may see a shopping list change: %s", err)
		return
	}
	for _, each := range subscribers {
		if !isAllowed[each.viewerUserId] {
			continue
		}
		select {
		case each.events <- event:
		default:
		}
	}
}

// ShoppingEvents tells the viewer each time a list they reach changes. Whether
// the viewer reaches the list is decided again for every event, so a list
// whose share ended is not followed from that moment.
func (self *graph) ShoppingEvents(ctx context.Context) (<-chan *ShoppingEventView, error) {
	var viewer *models.User
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		viewer, err = self.shoppingPerson(api.ContextWithTransaction(ctx, tx))
		return err
	}); err != nil {
		return nil, err
	}
	received, release, err := self.shoppingEvents.subscribe(self.database, viewer.ID)
	if err != nil {
		return nil, err
	}
	events := make(chan *ShoppingEventView)
	go func() {
		defer deferutil.Recover()
		defer close(events)
		defer release()
		for {
			select {
			case <-ctx.Done():
				return
			case event := <-received:
				select {
				case events <- &ShoppingEventView{ShoppingListID: event.ShoppingListID}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return events, nil
}

// shoppingPictureView serves an item's picture to a signed-in person the
// item's list reaches.
func (self *graph) shoppingPictureView(response http.ResponseWriter, request *http.Request) {
	username := api.UsernameFromRequest(request)
	if username == "" || username == config.LocalUsername {
		http.Error(response, "not signed in", http.StatusUnauthorized)
		return
	}
	var item *models.ShoppingItem
	if err := self.database.TransactionContext(request.Context(), func(tx db.Transaction) error {
		user, err := tx.GetUserByUsername(username)
		if err != nil {
			return err
		}
		if user == nil || user.Disabled() {
			return db.ErrNotFound
		}
		item, err = tx.GetShoppingItem(user.ID, mux.Vars(request)["shoppingItemId"])
		return err
	}); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			http.Error(response, "no such picture", http.StatusNotFound)
			return
		}
		log.Errorf("cannot read a shopping item's picture: %s", err)
		http.Error(response, "cannot read that just now", http.StatusInternalServerError)
		return
	}
	if item.PictureFileID == "" {
		http.Error(response, "no such picture", http.StatusNotFound)
		return
	}
	tag := `"` + item.PictureFileID + `"`
	response.Header().Set("ETag", tag)
	// Private because only the people the list reaches are let in, and a day
	// because a changed picture has a new address.
	response.Header().Set("Cache-Control", "private, max-age=86400")
	if matchesTag(request.Header.Get("If-None-Match"), tag) {
		response.WriteHeader(http.StatusNotModified)
		return
	}
	content, err := self.storage.GetFile(request.Context(), item.PictureFileID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			http.Error(response, "no such picture", http.StatusNotFound)
			return
		}
		log.Errorf("cannot read the file of a shopping item's picture: %s", err)
		http.Error(response, "cannot read that just now", http.StatusInternalServerError)
		return
	}
	response.Header().Set("Content-Type", "image/jpeg")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	if request.Method == http.MethodHead {
		return
	}
	_, _ = response.Write(content)
}
