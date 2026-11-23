package apigraph

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/deferutil"
)

const (
	// agentFeedSubscriptionsMost is how many tabs and phones of one account
	// may follow its feed on this instance at once.
	agentFeedSubscriptionsMost = 20

	// agentFeedBacklog is how many changes a follower that has stopped
	// reading holds before it starts losing them; a screen that missed some
	// reads its list again.
	agentFeedBacklog = 32
)

// agentFeedEventHub is the one LISTEN this process holds for feed changes,
// fanned out in memory to the tabs and phones of the person each is for, as
// chatCallEventHub does for calls. The zero value is ready to use.
type agentFeedEventHub struct {
	mutex            sync.Mutex
	subscribers      map[int]*agentFeedSubscriber
	nextSubscriberId int
	cancel           context.CancelFunc
}

type agentFeedSubscriber struct {
	userId string
	events chan models.FeedEvent
}

// subscribe adds a follower for the person, and starts the listener if it is
// the first. The returned function removes it.
func (self *agentFeedEventHub) subscribe(database db.Database, userId string) (<-chan models.FeedEvent, func(), error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	openCount := 0
	for _, each := range self.subscribers {
		if each.userId == userId {
			openCount++
		}
	}
	if openCount >= agentFeedSubscriptionsMost {
		return nil, nil, fmt.Errorf("%w: this account already follows its feed in %d places; close one first", api.ErrInvalidArguments, agentFeedSubscriptionsMost)
	}
	if self.cancel == nil {
		listenContext, cancel := context.WithCancel(context.Background())
		payloads, err := database.ListenFeedEvents(listenContext)
		if err != nil {
			cancel()
			return nil, nil, err
		}
		self.cancel = cancel
		go func() {
			defer deferutil.Recover()
			for payload := range payloads {
				self.deliver(payload)
			}
		}()
	}
	if self.subscribers == nil {
		self.subscribers = map[int]*agentFeedSubscriber{}
	}
	subscriber := &agentFeedSubscriber{userId: userId, events: make(chan models.FeedEvent, agentFeedBacklog)}
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

// deliver hands a change to the followers of the person it is for.
func (self *agentFeedEventHub) deliver(payload string) {
	var event models.FeedEvent
	if json.Unmarshal([]byte(payload), &event) != nil || event.FeedItemID == "" {
		return
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	for _, each := range self.subscribers {
		if each.userId != event.UserID {
			continue
		}
		select {
		case each.events <- event:
		default:
		}
	}
}
