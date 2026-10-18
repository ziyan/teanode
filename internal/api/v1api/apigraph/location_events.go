package apigraph

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/util/deferutil"
)

const (
	// locationEventSubscriptionsMost is how many maps one account may follow
	// at once on this instance: a few browser tabs, not a script that holds
	// the server's subscriptions open.
	locationEventSubscriptionsMost = 20

	// locationEventBacklog is how many events a subscriber that has stopped
	// reading holds before it starts losing them. A map that missed some
	// reads the location again.
	locationEventBacklog = 16

	// locationEventDecisionWait bounds the reading of who may see an event.
	locationEventDecisionWait = 10 * time.Second
)

// locationEventHub is the one LISTEN this process holds for location events,
// fanned out in memory to the maps open on this instance, as the chat hub
// does for chats. A listener of its own for each open map would use up the
// database's connections, and one transaction for each map for each event
// would make a report cost as many queries as there are dashboards open.
//
// The listener is held only while somebody follows a map. The zero value is
// ready to use.
type locationEventHub struct {
	mutex            sync.Mutex
	subscribers      map[int]*locationEventSubscriber
	nextSubscriberId int
	cancel           context.CancelFunc
}

// locationEventSubscriber is one open map.
type locationEventSubscriber struct {
	viewerUserId string
	events       chan db.LocationEvent
}

// subscribe adds a map for the viewer, and starts the listener if this is the
// first one. The returned function removes it, and stops the listener if it
// was the last.
func (self *locationEventHub) subscribe(database db.Database, viewerUserId string) (<-chan db.LocationEvent, func(), error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	openCount := 0
	for _, each := range self.subscribers {
		if each.viewerUserId == viewerUserId {
			openCount++
		}
	}
	if openCount >= locationEventSubscriptionsMost {
		return nil, nil, fmt.Errorf("%w: this account already follows the map in %d places; close one first", api.ErrInvalidArguments, locationEventSubscriptionsMost)
	}
	if self.cancel == nil {
		listenContext, cancel := context.WithCancel(context.Background())
		payloads, err := database.ListenLocationEvents(listenContext)
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
		self.subscribers = map[int]*locationEventSubscriber{}
	}
	subscriber := &locationEventSubscriber{viewerUserId: viewerUserId, events: make(chan db.LocationEvent, locationEventBacklog)}
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

// deliver hands one event to the maps that may see it: the person's own, the
// ones the event says have just lost the right to (so that they read again and
// drop the person), and those of the people the location reaches now, decided
// for all of them by one query.
func (self *locationEventHub) deliver(database db.Database, payload string) {
	var event db.LocationEvent
	if json.Unmarshal([]byte(payload), &event) != nil || event.UserID == "" {
		return
	}
	self.mutex.Lock()
	subscribers := make([]*locationEventSubscriber, 0, len(self.subscribers))
	for _, each := range self.subscribers {
		subscribers = append(subscribers, each)
	}
	self.mutex.Unlock()

	endedFor := map[string]bool{}
	for _, userId := range event.EndedForUserIDs {
		endedFor[userId] = true
	}
	var toDecide []string
	seen := map[string]bool{}
	for _, each := range subscribers {
		if each.viewerUserId != event.UserID && !endedFor[each.viewerUserId] && !seen[each.viewerUserId] {
			seen[each.viewerUserId] = true
			toDecide = append(toDecide, each.viewerUserId)
		}
	}
	isAllowed := map[string]bool{}
	if len(toDecide) > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), locationEventDecisionWait)
		defer cancel()
		if err := database.TransactionContext(ctx, func(tx db.Transaction) error {
			// One query for all of them: who among the open maps the person's
			// location reaches. A viewer who shares no circle with the person
			// costs nothing.
			reached, err := tx.FilterLocationGrantees(event.UserID, toDecide)
			if err != nil {
				return err
			}
			for _, viewerUserId := range reached {
				isAllowed[viewerUserId] = true
			}
			return nil
		}); err != nil {
			log.Warningf("cannot work out who may see a location event: %s", err)
			isAllowed = map[string]bool{}
		}
	}
	for _, each := range subscribers {
		if each.viewerUserId != event.UserID && !endedFor[each.viewerUserId] && !isAllowed[each.viewerUserId] {
			continue
		}
		select {
		case each.events <- event:
		default:
		}
	}
}
