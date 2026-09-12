package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// A device is something on the person's side that keeps a websocket open
// to the server so that the agent can ask it to do things: their own
// browser tab through the extension, their computer through `teanode
// computer`. The device does the acting; the server only carries a
// numbered request across and waits for the numbered answer. Only a turn
// with the person present may use one, and the refusals that matter are
// the device's own.

// DeviceConnection is what the relay needs of a device's websocket.
type DeviceConnection interface {
	Send(message []byte) error
}

// deviceMessage is what goes over the relay, either way.
type deviceMessage struct {
	Type   string          `json:"type"`
	ID     int64           `json:"id,omitempty"`
	Action string          `json:"action,omitempty"`
	Args   json.RawMessage `json:"args,omitempty"`
	Title  string          `json:"title,omitempty"`
	URL    string          `json:"url,omitempty"`
	Name   string          `json:"name,omitempty"`
	System string          `json:"system,omitempty"`
	Home   string          `json:"home,omitempty"`
	OK     bool            `json:"ok,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
	Error  string          `json:"error,omitempty"`
}

type deviceAnswer struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data,omitempty"`
	Error string          `json:"error,omitempty"`
}

// deviceAnswerWait is how long a request waits for the device.
const deviceAnswerWait = 60 * time.Second

// deviceLink is one attached device: its connection and the requests
// waiting on it.
type deviceLink struct {
	// what the device is called in an error: "the attached tab", "the
	// computer".
	what       string
	connection DeviceConnection
	attachedAt time.Time

	mutex   sync.Mutex
	next    int64
	pending map[int64]chan deviceAnswer
}

func newDeviceLink(what string, connection DeviceConnection) *deviceLink {
	return &deviceLink{what: what, connection: connection, attachedAt: time.Now(), pending: map[int64]chan deviceAnswer{}}
}

// Ask sends an action to the device and waits a minute for its answer.
func (self *deviceLink) Ask(ctx context.Context, action string, args any) (json.RawMessage, error) {
	return self.AskFor(ctx, action, args, deviceAnswerWait)
}

// AskFor sends an action to the device and waits up to wait for its
// answer: as long as the request itself may take, plus a margin, so that
// a slow command is not given up on while it still runs.
func (self *deviceLink) AskFor(ctx context.Context, action string, args any, wait time.Duration) (json.RawMessage, error) {
	if wait < deviceAnswerWait {
		wait = deviceAnswerWait
	}
	encoded, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	self.mutex.Lock()
	self.next++
	id := self.next
	channel := make(chan deviceAnswer, 1)
	self.pending[id] = channel
	self.mutex.Unlock()
	forget := func() {
		self.mutex.Lock()
		delete(self.pending, id)
		self.mutex.Unlock()
	}
	message, _ := json.Marshal(deviceMessage{Type: "act", ID: id, Action: action, Args: encoded})
	if err := self.connection.Send(message); err != nil {
		forget()
		return nil, fmt.Errorf("%s is gone: %w", self.what, err)
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case answer, ok := <-channel:
		if !ok {
			return nil, fmt.Errorf("%s was detached", self.what)
		}
		if !answer.OK {
			return nil, fmt.Errorf("%s refused: %s", self.what, answer.Error)
		}
		return answer.Data, nil
	case <-timer.C:
		forget()
		return nil, fmt.Errorf("%s did not answer within %s", self.what, wait.Round(time.Second))
	case <-ctx.Done():
		forget()
		return nil, ctx.Err()
	}
}

// answered is the device answering a request.
func (self *deviceLink) answered(id int64, ok bool, data json.RawMessage, failure string) {
	self.mutex.Lock()
	channel, found := self.pending[id]
	if found {
		delete(self.pending, id)
	}
	self.mutex.Unlock()
	if found {
		channel <- deviceAnswer{OK: ok, Data: data, Error: failure}
	}
}

// drop tells whoever is waiting that the device went.
func (self *deviceLink) drop() {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	for id, channel := range self.pending {
		close(channel)
		delete(self.pending, id)
	}
}

// AttachedAt is when the device was attached.
func (self *deviceLink) AttachedAt() time.Time { return self.attachedAt }
