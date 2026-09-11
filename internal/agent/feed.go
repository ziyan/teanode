package agent

import (
	"encoding/json"
	"sort"
	"time"
)

// A conversation's feed: every event of every turn of the conversation,
// wherever the turn was started — the drawer, a phone, a terminal, a chat
// app — and whichever instance runs it. A drawer follows the feed of the
// conversation it shows rather than the runs it started itself, so a
// question asked from Telegram is answered in the drawer as it happens.
//
// Across instances the events travel as PostgreSQL notifications: an
// instance says what its runs emit, every other instance hears it and
// hands it to its own subscribers. The payload is bounded, so a long
// tool result is cut for the trip; the drawer reads the whole transcript
// again when the turn is over. What a listener misses while reconnecting
// is gone the same way, and found the same way.

// feedBacklog is how many events a feed holds for a subscriber that has
// stopped reading before it starts losing them: the backlog of a run and
// some, since a subscriber is handed that on arrival.
const feedBacklog = askEventBacklog + 64

// relayPayloadLimit is how long a notification's payload may be; the
// server's own limit is a little under 8000 bytes.
const relayPayloadLimit = 7000

// relayed is an event as it travels between instances.
type relayed struct {
	Instance string `json:"instance"`
	Event    Event  `json:"event"`
}

// SubscribeConversation is the feed of a conversation: the turns in flight
// on this instance are replayed first, then whatever happens until the
// returned function is called. The channel closes then.
func (self *Agent) SubscribeConversation(conversationId string) (<-chan Event, func()) {
	channel := make(chan Event, feedBacklog)
	self.feedsMutex.Lock()
	if self.feeds == nil {
		self.feeds = map[string]map[int]chan Event{}
	}
	if self.feeds[conversationId] == nil {
		self.feeds[conversationId] = map[int]chan Event{}
	}
	id := self.nextFeed
	self.nextFeed++
	self.feeds[conversationId][id] = channel
	// The replay happens with the feeds held, so that nothing emitted
	// meanwhile is delivered before what came before it. An event copied
	// here and published a moment later reaches the subscriber twice;
	// the sequence tells, and whoever forwards the feed drops the second.
	for _, run := range self.runsOf(conversationId) {
		run.mutex.Lock()
		finished := run.finished
		events := append([]Event(nil), run.events...)
		run.mutex.Unlock()
		if finished {
			continue
		}
		for _, event := range events {
			select {
			case channel <- event:
			default:
			}
		}
	}
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

// runsOf is the runs of a conversation on this instance, oldest first.
func (self *Agent) runsOf(conversationId string) []*AskRun {
	self.runsMutex.Lock()
	var runs []*AskRun
	for _, run := range self.runs {
		if run.settings.Conversation.ID == conversationId {
			runs = append(runs, run)
		}
	}
	self.runsMutex.Unlock()
	// A run's id is a ULID: the order they were started in.
	sort.Slice(runs, func(left, right int) bool { return runs[left].ID < runs[right].ID })
	return runs
}

// publish hands an event to the feed's subscribers, and to the other
// instances when it is this one's.
func (self *Agent) publish(event Event, own bool) {
	self.feedsMutex.Lock()
	for _, channel := range self.feeds[event.ConversationID] {
		select {
		case channel <- event:
		default:
			// A drawer that stopped reading is not a reason to stall the
			// run; it will miss this one.
		}
	}
	self.feedsMutex.Unlock()
	if own {
		self.relay(event)
	}
}

// relay queues an event for the other instances. Pieces of the answer
// that follow each other are joined, so that a fast model does not turn
// into a notification per word.
func (self *Agent) relay(event Event) {
	self.relayMutex.Lock()
	defer self.relayMutex.Unlock()
	if !self.relaying {
		return
	}
	if last := len(self.relayQueue) - 1; last >= 0 {
		previous := &self.relayQueue[last]
		if previous.Kind == EventText && event.Kind == EventText && previous.RunID == event.RunID {
			previous.Text += event.Text
			previous.Sequence = event.Sequence
			return
		}
	}
	self.relayQueue = append(self.relayQueue, event)
	select {
	case self.relayWake <- struct{}{}:
	default:
	}
}

// startFeed starts the relay to the other instances and the listener for
// theirs. Without a database — some tests — there is neither.
func (self *Agent) startFeed() {
	if self.settings.Database == nil {
		return
	}
	self.relayMutex.Lock()
	self.relaying = true
	self.relayWake = make(chan struct{}, 1)
	self.relayMutex.Unlock()
	self.waitGroup.Add(1)
	go func() {
		defer self.waitGroup.Done()
		self.runRelay()
	}()
	payloads, err := self.settings.Database.ListenAgentEvents(self.ctx)
	if err != nil {
		log.Warningf("the agent cannot hear the other instances: %s", err)
		return
	}
	self.waitGroup.Add(1)
	go func() {
		defer self.waitGroup.Done()
		for payload := range payloads {
			var received relayed
			if err := json.Unmarshal([]byte(payload), &received); err != nil || received.Instance == self.settings.Instance {
				continue
			}
			self.noteForeignRun(received.Event.RunID, received.Event.ConversationID)
			self.publish(received.Event, false)
		}
	}()
	self.listenCommands()
}

// runRelay drains the queue into notifications until the agent stops.
func (self *Agent) runRelay() {
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
			payload, err := json.Marshal(relayed{Instance: self.settings.Instance, Event: fitForRelay(event)})
			if err != nil {
				continue
			}
			if err := self.settings.Database.NotifyAgentEvent(string(payload)); err != nil && time.Since(lastWarning) > time.Minute {
				lastWarning = time.Now()
				log.Warningf("the agent cannot tell the other instances: %s", err)
			}
		}
	}
}

// fitForRelay cuts what is too long for a notification: the text first,
// then the arguments. The drawer shows the cut piece until the turn is
// over and it reads the transcript.
func fitForRelay(event Event) Event {
	over := func() int {
		encoded, _ := json.Marshal(relayed{Instance: "", Event: event})
		return len(encoded) + 64 - relayPayloadLimit
	}
	if excess := over(); excess > 0 {
		event.Text = cutBytes(event.Text, excess)
	}
	if excess := over(); excess > 0 {
		event.Arguments = cutBytes(event.Arguments, excess)
	}
	return event
}

// cutBytes drops at least excess bytes from the end of the text, on a
// character boundary, with a mark that says so.
func cutBytes(text string, excess int) string {
	const mark = "…"
	keep := len(text) - excess - len(mark)
	if keep <= 0 {
		return ""
	}
	for keep > 0 && !isRuneStart(text[keep]) {
		keep--
	}
	return text[:keep] + mark
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }
