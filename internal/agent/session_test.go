package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// A fake device that answers session requests the way a computer does, and
// says things about a session without being asked.
type fakeDevice struct {
	link *deviceLink

	mutex   sync.Mutex
	started []string
	ids     []string
	written []string
	closed  []string
}

func (self *fakeDevice) startedIds() []string {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return append([]string{}, self.ids...)
}

func (self *fakeDevice) Send(message []byte) error {
	var sent struct {
		ID     int64           `json:"id"`
		Action string          `json:"action"`
		Args   json.RawMessage `json:"args"`
	}
	if err := json.Unmarshal(message, &sent); err != nil {
		return err
	}
	var arguments struct {
		Session string `json:"session"`
		Command string `json:"command"`
		Data    string `json:"data"`
	}
	_ = json.Unmarshal(sent.Args, &arguments)

	self.mutex.Lock()
	switch sent.Action {
	case "session_start":
		self.started = append(self.started, arguments.Command)
		self.ids = append(self.ids, arguments.Session)
	case "session_write":
		decoded, _ := base64.StdEncoding.DecodeString(arguments.Data)
		self.written = append(self.written, string(decoded))
	case "session_close":
		self.closed = append(self.closed, arguments.Session)
	}
	self.mutex.Unlock()

	// Answered on another goroutine, as a real device would: the caller is
	// waiting on the answer while this returns.
	go self.link.answered(sent.ID, true, json.RawMessage(`{"ok":true}`), "")
	return nil
}

// A program that stays open: what it says arrives unasked, is kept, and is
// read by whoever is driving it.
func TestASessionKeepsWhatArrivesUntilItIsRead(t *testing.T) {
	t.Parallel()

	device := &fakeDevice{}
	device.link = newDeviceLink("the computer", device)

	held, err := device.link.startSession(context.Background(), "stdio", "cat", nil, "", nil, 0, 0)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if len(device.started) != 1 || device.started[0] != "cat" {
		t.Fatalf("the device was asked to start it: %v", device.started)
	}

	if err := device.link.WriteSession(context.Background(), held.id, []byte("hello\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if len(device.written) != 1 || device.written[0] != "hello\n" {
		t.Fatalf("and written to: %v", device.written)
	}

	// Output arrives without anybody asking.
	device.link.sessionSaid(held.id, "output", "stdout",
		base64.StdEncoding.EncodeToString([]byte("hello\n")), 0)

	taken, dropped, ended, _ := held.take()
	if string(taken) != "hello\n" || dropped != 0 || ended {
		t.Fatalf("what arrived is there to be read: %q, %d dropped, ended %v", taken, dropped, ended)
	}
	// And taking it empties it, so the same output is not read twice.
	if again, _, _, _ := held.take(); len(again) != 0 {
		t.Fatalf("read once: %q", again)
	}

	device.link.sessionSaid(held.id, "ended", "", "", 3)
	if _, _, ended, code := held.take(); !ended || code != 3 {
		t.Fatalf("and the end is reported with its code: %v %d", ended, code)
	}
}

// A program that prints forever is not a way to fill this server's memory.
func TestASessionsOutputIsBounded(t *testing.T) {
	t.Parallel()

	device := &fakeDevice{}
	device.link = newDeviceLink("the computer", device)
	held, err := device.link.startSession(context.Background(), "stdio", "yes", nil, "", nil, 0, 0)
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	chunk := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 64<<10)))
	for round := 0; round < 8; round++ {
		device.link.sessionSaid(held.id, "output", "stdout", chunk, 0)
	}

	taken, dropped, _, _ := held.take()
	if len(taken) > sessionBuffer {
		t.Fatalf("kept %d bytes, which is past the bound", len(taken))
	}
	if dropped == 0 {
		t.Fatalf("and it says what it dropped rather than pretending it had it all")
	}
	// What is kept is the most recent, which is what somebody driving a
	// program wants to see.
	if len(taken) != sessionBuffer {
		t.Fatalf("the buffer is full: %d", len(taken))
	}
}

// A device that goes away does not leave a reader waiting for a program that
// is no longer running.
func TestADetachedDeviceEndsItsSessions(t *testing.T) {
	t.Parallel()

	device := &fakeDevice{}
	device.link = newDeviceLink("the computer", device)
	held, err := device.link.startSession(context.Background(), "stdio", "cat", nil, "", nil, 0, 0)
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	waited := make(chan struct{})
	go func() {
		held.settled(context.Background(), 5*time.Second)
		close(waited)
	}()

	device.link.drop()

	select {
	case <-waited:
	case <-time.After(2 * time.Second):
		t.Fatalf("a reader waiting on it was left waiting")
	}
	if _, _, ended, _ := held.take(); !ended {
		t.Fatalf("and the session reads as ended")
	}
	if device.link.Session(held.id) != nil {
		t.Fatalf("the device holds it no longer")
	}
}

// A session as a pair of pipes: what a protocol written for a subprocess
// wants, over a program that is on somebody else's machine. The reader
// waits for something to arrive, hands over what has, and reports the end
// of the stream once the program has ended and everything is read.
func TestASessionReadsAndWritesLikePipes(t *testing.T) {
	t.Parallel()

	device := &fakeDevice{}
	device.link = newDeviceLink("the computer", device)
	writer, reader, closer, err := device.link.SessionPipes(context.Background(), "some-server", []string{"--stdio"}, "", nil)
	if err != nil {
		t.Fatalf("pipes: %v", err)
	}
	if len(device.started) != 1 || device.started[0] != "some-server" {
		t.Fatalf("the program was started on the device: %v", device.started)
	}
	held := device.link.Session(device.startedIds()[0])

	// Writing goes to the device as the session's input.
	if _, err := writer.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if len(device.written) != 1 || !strings.Contains(device.written[0], `"method":"ping"`) {
		t.Fatalf("written across as it was: %v", device.written)
	}

	// Reading blocks until the device says something, then hands it over.
	got := make(chan string, 1)
	go func() {
		buffer := make([]byte, 256)
		read, err := reader.Read(buffer)
		if err != nil {
			got <- "error: " + err.Error()
			return
		}
		got <- string(buffer[:read])
	}()
	select {
	case early := <-got:
		t.Fatalf("read before anything arrived: %q", early)
	case <-time.After(100 * time.Millisecond):
	}
	device.link.sessionSaid(held.id, "output", "stdout",
		base64.StdEncoding.EncodeToString([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`+"\n")), 0)
	select {
	case line := <-got:
		if !strings.Contains(line, `"id":1`) {
			t.Fatalf("what the device said: %q", line)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("the reader did not wake when output arrived")
	}

	// Once the program has ended and there is nothing left, the stream
	// ends -- which is how the protocol above learns the server went.
	device.link.sessionSaid(held.id, "ended", "", "", 0)
	if _, err := reader.Read(make([]byte, 16)); err != io.EOF {
		t.Fatalf("end of stream after the end: %v", err)
	}

	// Closing the pipes closes the session on the device.
	if err := closer(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if len(device.closed) != 1 {
		t.Fatalf("the device was told to close it: %v", device.closed)
	}
}
