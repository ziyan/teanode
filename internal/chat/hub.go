package chat

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/deferutil"
)

// Settings is what a hub needs.
type Settings struct {
	// Database carries the events to the other instances. Nil in a test that
	// has only one, which then has neither the relay nor the listener.
	Database db.Database

	// Configuration says how to reach the phones' relay, as the agent's does.
	Configuration func() *config.Configuration

	// Instance names this server instance on what it tells the others, so
	// that it can tell its own words when they come back.
	Instance string

	// Presence is who has a dashboard or the app open. The server hands the
	// agent the same one.
	Presence *agent.Presence

	// Phones sends a notification to a person's phones. The server hands the
	// agent the same one; nil makes the hub its own.
	Phones *agent.PhoneNotifier
}

// Hub is the feed of every chat conversation on this server: the events of
// what the people in a chat do, on this instance and the others.
//
// The events are those of the agent's feed, and travel the way they do:
// across instances as PostgreSQL notifications, one payload for each event,
// bounded in size, so that a long message is cut for the trip and the
// client reads the conversation again. What a listener misses while it is
// reconnecting is gone the same way, and found the same way.
//
// A chat has one row of what was said and a conversation for each member, so
// an event is addressed to the conversation of one member, with the message
// as that member reads it. Whoever publishes names only the members who are
// in the chat, which is how a person taken out of it stops hearing it at
// once; EventsForMembers builds them so.
type Hub struct {
	settings *Settings
	phones   *agent.PhoneNotifier

	ctx       context.Context
	cancel    context.CancelFunc
	waitGroup sync.WaitGroup

	// feeds are the subscribers to each conversation's events, by
	// conversation; the relay queue is what this instance published and the
	// others have not heard yet.
	feedsMutex sync.Mutex
	feeds      map[string]map[int]chan agent.Event
	nextFeed   int

	relayMutex sync.Mutex
	relaying   bool
	relayQueue []agent.Event
	relayWake  chan struct{}
}

// feedBacklog is how many events a feed holds for a subscriber that has
// stopped reading before it starts losing them. A drawer that missed some
// reads the chat again.
const feedBacklog = 128

// relayed is an event as it travels between instances.
type relayed struct {
	Instance string      `json:"instance"`
	Event    agent.Event `json:"event"`
}

// New is a hub nobody is subscribed to. Start makes it hear the other
// instances and tell them.
func New(settings *Settings) *Hub {
	self := &Hub{settings: settings, phones: settings.Phones}
	self.ctx, self.cancel = context.WithCancel(context.Background())
	if self.phones == nil {
		self.phones = agent.NewPhoneNotifier(&agent.PhoneNotifierSettings{
			Database:      settings.Database,
			Configuration: settings.Configuration,
			Context:       self.ctx,
			WaitGroup:     &self.waitGroup,
		})
	}
	if settings.Presence == nil {
		settings.Presence = agent.NewPresence()
	}
	return self
}

// Presence is who has a dashboard or the app open.
func (self *Hub) Presence() *agent.Presence {
	return self.settings.Presence
}

// Phones sends a notification to a person's phones.
func (self *Hub) Phones() *agent.PhoneNotifier {
	return self.phones
}

// ReportPresence records one tab's or one phone's report for a person; see
// agent.Presence.Report. A chat is only notified to a phone while nobody is
// looking at that conversation, so the dashboard and the app report whether
// they are shown and which conversation they show.
func (self *Hub) ReportPresence(userId, reporterId string, isVisible bool, idleSeconds int, conversationId string, now time.Time) {
	self.settings.Presence.Report(userId, reporterId, isVisible, idleSeconds, conversationId, now)
}

// Start starts the relay to the other instances and the listener for theirs.
// Without a database, as in some tests, there is neither.
func (self *Hub) Start() {
	if self.settings.Database == nil {
		return
	}
	self.relayMutex.Lock()
	self.relaying = true
	self.relayWake = make(chan struct{}, 1)
	self.relayMutex.Unlock()
	self.waitGroup.Add(1)
	go func() {
		defer deferutil.Recover()
		defer self.waitGroup.Done()
		self.runRelay()
	}()
	payloads, err := self.settings.Database.ListenChatEvents(self.ctx)
	if err != nil {
		log.Warningf("chats cannot hear the other instances: %s", err)
		return
	}
	self.waitGroup.Add(1)
	go func() {
		defer deferutil.Recover()
		defer self.waitGroup.Done()
		for payload := range payloads {
			var received relayed
			if err := json.Unmarshal([]byte(payload), &received); err != nil || received.Instance == self.settings.Instance {
				continue
			}
			self.deliver(received.Event)
		}
	}()
}

// Stop ends the relay and the listener, and waits for the notifications
// still on their way to phones.
func (self *Hub) Stop() {
	self.cancel()
	self.waitGroup.Wait()
}

// Subscribe is the feed of a chat conversation: what happens in it from now
// until the returned function is called, and the channel closes then. Nothing
// is replayed, as no event of a chat belongs to a run: a client that opens
// later reads what it missed with the messages.
func (self *Hub) Subscribe(conversationId string) (<-chan agent.Event, func()) {
	channel := make(chan agent.Event, feedBacklog)
	self.feedsMutex.Lock()
	if self.feeds == nil {
		self.feeds = map[string]map[int]chan agent.Event{}
	}
	if self.feeds[conversationId] == nil {
		self.feeds[conversationId] = map[int]chan agent.Event{}
	}
	id := self.nextFeed
	self.nextFeed++
	self.feeds[conversationId][id] = channel
	self.feedsMutex.Unlock()
	return channel, func() {
		self.feedsMutex.Lock()
		defer self.feedsMutex.Unlock()
		subscribers := self.feeds[conversationId]
		if _, ok := subscribers[id]; ok {
			delete(subscribers, id)
			close(channel)
		}
		if len(subscribers) == 0 {
			delete(self.feeds, conversationId)
		}
	}
}

// PublishChatEvents tells everybody watching the conversations the events are
// for, on this instance and the others. Called once the change is committed.
// Each event is addressed to the conversation of one member and names only
// members who are in the chat. None of them belongs to a run, so none is
// replayed.
//
// It is what an agent calls, through agent.Settings.ChatEvents, to say
// something in a chat or to show that it is typing there.
func (self *Hub) PublishChatEvents(events []agent.Event) {
	now := time.Now()
	for _, event := range events {
		if event.ConversationID == "" {
			continue
		}
		event.At = now
		self.deliver(event)
		self.relay(event)
	}
}

// EventsForMembers builds an event for each member who is in the chat and
// has a conversation, addressed to that conversation. A member who has left or
// been taken out is not told, nor is one the build function answers an event
// of no kind for. The result is for PublishChatEvents.
func EventsForMembers(members []*models.ChatMember, build func(member *models.ChatMember) agent.Event) []agent.Event {
	events := []agent.Event{}
	for _, member := range members {
		if member.IsRemoved || member.ConversationID == "" {
			continue
		}
		event := build(member)
		if event.Kind == "" {
			continue
		}
		event.ConversationID = member.ConversationID
		events = append(events, event)
	}
	return events
}

// deliver hands an event to the subscribers of its conversation on this
// instance.
func (self *Hub) deliver(event agent.Event) {
	self.feedsMutex.Lock()
	defer self.feedsMutex.Unlock()
	for _, channel := range self.feeds[event.ConversationID] {
		select {
		case channel <- event:
		default:
			// A drawer that stopped reading is not a reason to stall the
			// person who is writing; it will miss this one.
		}
	}
}

// relay queues an event for the other instances.
func (self *Hub) relay(event agent.Event) {
	self.relayMutex.Lock()
	defer self.relayMutex.Unlock()
	if !self.relaying {
		return
	}
	self.relayQueue = append(self.relayQueue, event)
	select {
	case self.relayWake <- struct{}{}:
	default:
	}
}

// runRelay drains the queue into notifications until the hub stops.
func (self *Hub) runRelay() {
	var lastWarning time.Time
	for {
		select {
		case <-self.ctx.Done():
			return
		case <-self.relayWake:
		}
		self.relayMutex.Lock()
		queued := self.relayQueue
		self.relayQueue = nil
		self.relayMutex.Unlock()
		for _, event := range queued {
			payload, err := json.Marshal(relayed{Instance: self.settings.Instance, Event: agent.FitEventForRelay(event)})
			if err != nil {
				continue
			}
			if err := self.settings.Database.NotifyChatEvent(string(payload)); err != nil && time.Since(lastWarning) > time.Minute {
				lastWarning = time.Now()
				log.Warningf("chats cannot tell the other instances: %s", err)
			}
		}
	}
}
