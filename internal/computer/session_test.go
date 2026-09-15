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
		{"a kind it does not start", SessionStartArguments{Session: "x", Command: "cat", Kind: "pty"}, "not a kind"},
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
