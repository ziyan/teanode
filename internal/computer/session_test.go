package computer

import (
	"context"
	"encoding/base64"
	"strings"
	"sync"
	"testing"
	"time"
)

// A program that stays open: started, written to, answering as it goes, and
// ended when it is closed.
//
// cat is the whole shape of it in one program -- it starts, it says nothing
// until something is written, it answers each write, and it finishes when
// its input is closed. Everything a session layer has to do that a one-shot
// command does not.
func TestASessionRunsUntilItIsClosed(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a process")
	}

	var mutex sync.Mutex
	var said strings.Builder
	finished := make(chan int, 1)

	output := func(_, _, data string) {
		decoded, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			t.Errorf("output is base64: %v", err)
			return
		}
		mutex.Lock()
		said.Write(decoded)
		mutex.Unlock()
	}
	ended := func(_ string, code int) { finished <- code }

	held := newSessions()
	defer held.closeAll()

	options := withDefaults(&Options{})
	started, err := held.start(context.Background(), options, &SessionStartArguments{
		Session: "one", Kind: "stdio", Command: "cat",
	}, output, ended)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if started.PID == 0 {
		t.Fatalf("it is a running process: %+v", started)
	}

	if _, err := held.write(&SessionWriteArguments{
		Session: "one", Data: base64.StdEncoding.EncodeToString([]byte("hello\n")),
	}); err != nil {
		t.Fatalf("write: %v", err)
	}

	// What it says comes back without anybody asking for it.
	deadline := time.Now().Add(5 * time.Second)
	for {
		mutex.Lock()
		heard := said.String()
		mutex.Unlock()
		if strings.Contains(heard, "hello") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("it never answered: %q", heard)
		}
		time.Sleep(20 * time.Millisecond)
	}

	if _, err := held.close(&SessionCloseArguments{Session: "one"}); err != nil {
		t.Fatalf("close: %v", err)
	}
	select {
	case code := <-finished:
		if code != 0 {
			t.Fatalf("cat ends cleanly when its input closes, not %d", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("it was never reported as finished")
	}

	// And it is gone, so writing to it says so rather than pretending.
	if _, err := held.write(&SessionWriteArguments{Session: "one", Data: ""}); err == nil {
		t.Fatalf("writing to a session that ended is refused")
	}
}

// What a session may be asked to do is bounded, and what it is asked to run
// has to be something.
func TestASessionRefusesWhatItCannotDo(t *testing.T) {
	t.Parallel()

	held := newSessions()
	options := withDefaults(&Options{})
	for _, each := range []struct {
		what      string
		arguments SessionStartArguments
		says      string
	}{
		{"no identifier", SessionStartArguments{Command: "cat"}, "needs an identifier"},
		{"no command", SessionStartArguments{Session: "x"}, "needs a command"},
		{"a kind it does not start", SessionStartArguments{Session: "x", Command: "cat", Kind: "socket"}, "not a kind"},
	} {
		t.Run(each.what, func(t *testing.T) {
			_, err := held.start(context.Background(), options, &each.arguments,
				func(string, string, string) {}, func(string, int) {})
			if err == nil || !strings.Contains(err.Error(), each.says) {
				t.Fatalf("refused, saying why: %v", err)
			}
		})
	}
}

// A terminal: the program runs in a pty, and what it shows is read as a
// screen rather than as the bytes that drew it.
func TestATerminalIsReadAsAScreen(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a process in a pty")
	}

	finished := make(chan int, 1)
	held := newSessions()
	defer held.closeAll()
	options := withDefaults(&Options{})

	if _, err := held.start(context.Background(), options, &SessionStartArguments{
		Session: "term", Kind: "pty", Command: "/bin/sh", Columns: 60, Rows: 12,
	}, func(string, string, string) {}, func(_ string, code int) { finished <- code }); err != nil {
		t.Fatalf("start: %v", err)
	}

	// A shell prompt appears; something typed and entered is echoed and
	// answered; all of it lands on the screen.
	if _, err := held.write(&SessionWriteArguments{
		Session: "term", Data: base64.StdEncoding.EncodeToString([]byte("echo terminal-works\r")),
	}); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Generous on purpose. This starts a real shell in a pseudo-terminal
	// and waits for it to echo, which on a loaded machine is not a
	// five-second job: the test failed that way on a CI runner while
	// passing every time by hand. A slow machine should make this test
	// slow, not red.
	deadline := time.Now().Add(30 * time.Second)
	var screen *SessionScreen
	for {
		var err error
		screen, err = held.readScreen(&SessionReadArguments{Session: "term"})
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		// Once as the echo of what was typed, once as the answer.
		if strings.Count(screen.Text, "terminal-works") >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the screen never showed it:\n%s", screen.Text)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if screen.Columns != 60 || screen.Rows != 12 {
		t.Fatalf("the size asked for: %dx%d", screen.Columns, screen.Rows)
	}
	// Read again with nothing new, and it says so.
	//
	// The screen has to have stopped first. A shell that has just answered
	// is often still drawing -- a prompt, the cursor moved back -- and a
	// read taken in the middle of that says, correctly, that something
	// changed. Reading until two in a row agree is what "nothing new"
	// means; asserting it against a shell still settling is what made this
	// fail on a busy machine and nowhere else.
	settled := time.Now().Add(30 * time.Second)
	for {
		again, err := held.readScreen(&SessionReadArguments{Session: "term"})
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if !again.Changed {
			break
		}
		if time.Now().After(settled) {
			t.Fatalf("the screen never stopped changing:\n%s", again.Text)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Resizing is one request too, and the screen follows.
	if _, err := held.resizeTerminal(&SessionResizeArguments{Session: "term", Columns: 80, Rows: 20}); err != nil {
		t.Fatalf("resize: %v", err)
	}
	if after, _ := held.readScreen(&SessionReadArguments{Session: "term"}); after.Columns != 80 || after.Rows != 20 {
		t.Fatalf("resized: %dx%d", after.Columns, after.Rows)
	}

	// Ending the shell ends the session, and the exit code is readable
	// from the last screen until it is closed.
	if _, err := held.write(&SessionWriteArguments{
		Session: "term", Data: base64.StdEncoding.EncodeToString([]byte("exit 7\r")),
	}); err != nil {
		t.Fatalf("write: %v", err)
	}
	select {
	case code := <-finished:
		if code != 7 {
			t.Fatalf("its exit code, not %d", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("never ended")
	}
	last, err := held.readScreen(&SessionReadArguments{Session: "term"})
	if err != nil {
		t.Fatalf("the last screen is still readable: %v", err)
	}
	if !last.Ended || last.Code != 7 {
		t.Fatalf("and says it ended with 7: %+v", last)
	}
	if _, err := held.close(&SessionCloseArguments{Session: "term"}); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := held.readScreen(&SessionReadArguments{Session: "term"}); err == nil {
		t.Fatalf("closed is gone")
	}
}
