package apigraph

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/call"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/util/deferutil"
)

const (
	// chatCallEventSubscriptionsMost is how many tabs and phones of one
	// account may follow its calls on this instance at once.
	chatCallEventSubscriptionsMost = 20

	// chatCallEventBacklog is how many changes a follower that has stopped
	// reading holds before it starts losing them; it reads the call again
	// with the next.
	chatCallEventBacklog = 16

	// chatCallSignalBacklog is how many offers wait for a device that has
	// not opened its signals yet, or has stopped reading them. Only the
	// latest matters, but they are answered in order.
	chatCallSignalBacklog = 8
)

// chatCallEventHub is the one LISTEN this process holds for calls, fanned
// out in memory to the tabs and phones of the people with a part in each, as
// locationEventHub does for maps. The zero value is ready to use.
type chatCallEventHub struct {
	mutex            sync.Mutex
	subscribers      map[int]*chatCallEventSubscriber
	nextSubscriberId int
	cancel           context.CancelFunc
}

type chatCallEventSubscriber struct {
	userId  string
	callIds chan string
}

// subscribe adds a follower for the person, and starts the listener if it is
// the first. The returned function removes it.
func (self *chatCallEventHub) subscribe(database db.Database, userId string) (<-chan string, func(), error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	openCount := 0
	for _, each := range self.subscribers {
		if each.userId == userId {
			openCount++
		}
	}
	if openCount >= chatCallEventSubscriptionsMost {
		return nil, nil, fmt.Errorf("%w: this account already follows its calls in %d places; close one first", api.ErrInvalidArguments, chatCallEventSubscriptionsMost)
	}
	if self.cancel == nil {
		listenContext, cancel := context.WithCancel(context.Background())
		payloads, err := database.ListenChatCallEvents(listenContext)
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
		self.subscribers = map[int]*chatCallEventSubscriber{}
	}
	subscriber := &chatCallEventSubscriber{userId: userId, callIds: make(chan string, chatCallEventBacklog)}
	id := self.nextSubscriberId
	self.nextSubscriberId++
	self.subscribers[id] = subscriber
	return subscriber.callIds, func() {
		self.mutex.Lock()
		defer self.mutex.Unlock()
		delete(self.subscribers, id)
		if len(self.subscribers) == 0 && self.cancel != nil {
			self.cancel()
			self.cancel = nil
		}
	}, nil
}

// deliver hands the call's id to the followers of the people the event names.
func (self *chatCallEventHub) deliver(payload string) {
	var event db.ChatCallEvent
	if json.Unmarshal([]byte(payload), &event) != nil || event.CallID == "" {
		return
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	for _, each := range self.subscribers {
		if !slices.Contains(event.UserIDs, each.userId) {
			continue
		}
		select {
		case each.callIds <- event.CallID:
		default:
		}
	}
}

// chatCallSignals holds what the media server tells each device on a call
// (an offer to answer), until the device's ChatCallSignals subscription reads
// it: an offer can come before the device has opened the subscription. Only
// the instance whose media server holds the call has its signals.
type chatCallSignals struct {
	mutex sync.Mutex
	boxes map[string]*chatCallSignalBox
}

type chatCallSignalBox struct {
	signals chan call.Signal
}

func chatCallSignalKey(callId, userId string) string { return callId + "/" + userId }

// box is the person's box on a call, made the first time it is wanted, by
// the device's signals or by its join, whichever reaches the server first:
// they travel apart, so either may.
func (self *chatCallSignals) box(callId, userId string) *chatCallSignalBox {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.boxes == nil {
		self.boxes = map[string]*chatCallSignalBox{}
	}
	key := chatCallSignalKey(callId, userId)
	found := self.boxes[key]
	if found == nil {
		found = &chatCallSignalBox{signals: make(chan call.Signal, chatCallSignalBacklog)}
		self.boxes[key] = found
	}
	return found
}

// send is the media server's signal function for one device.
func (self *chatCallSignalBox) send(signal call.Signal) {
	select {
	case self.signals <- signal:
	default:
		log.Warningf("a device on a call is not reading the offers it is sent")
	}
}

// drain drops the offers made to the device before this one joined: a
// device that joins again does not answer them.
func (self *chatCallSignalBox) drain() {
	for {
		select {
		case <-self.signals:
		default:
			return
		}
	}
}

// forget drops the person's box when they leave the call.
func (self *chatCallSignals) forget(callId, userId string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	delete(self.boxes, chatCallSignalKey(callId, userId))
}
