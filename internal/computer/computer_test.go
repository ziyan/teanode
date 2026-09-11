package computer

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeConnection is the server's side: what it sends the program, and what
// the program answered.
type fakeConnection struct {
	incoming chan message
	outgoing chan message
}

func (self *fakeConnection) ReadJSON(value any) error {
	received, ok := <-self.incoming
	if !ok {
		return context.Canceled
	}
	encoded, _ := json.Marshal(received)
	return json.Unmarshal(encoded, value)
}

func (self *fakeConnection) WriteJSON(value any) error {
	encoded, _ := json.Marshal(value)
	var sent message
	_ = json.Unmarshal(encoded, &sent)
	self.outgoing <- sent
	return nil
}

func (self *fakeConnection) next(t *testing.T, kind string) message {
	t.Helper()
	for {
		select {
		case sent := <-self.outgoing:
			if sent.Type == kind {
				return sent
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no %s came", kind)
		}
	}
}

func TestServeAnswersTheAgentInsideWhatWasAllowed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the shell here is sh")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("first\nsecond\nthird\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	connection := &fakeConnection{incoming: make(chan message, 8), outgoing: make(chan message, 8)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, connection, &Options{Token: "t", Name: "laptop", Home: root}) }()

	hello := connection.next(t, "hello")
	if hello.Protocol != Protocol || hello.Token != "t" || hello.Name != "laptop" || hello.System == "" || hello.Home != root {
		t.Fatalf("hello %+v", hello)
	}
	connection.incoming <- message{Type: "welcome", Protocol: Protocol}

	ask := func(id int64, action string, args any) message {
		encoded, _ := json.Marshal(args)
		connection.incoming <- message{Type: "act", ID: id, Action: action, Args: encoded}
		for {
			answer := connection.next(t, "result")
			if answer.ID == id {
				return answer
			}
		}
	}
	answer := ask(1, "shell", ShellArguments{Command: "echo hi; echo oops >&2; exit 3"})
	var ran ShellResult
	_ = json.Unmarshal(answer.Data, &ran)
	if !answer.OK || ran.Stdout != "hi\n" || ran.Stderr != "oops\n" || ran.ExitCode != 3 {
		t.Fatalf("shell %+v %+v", answer, ran)
	}
	if answer := ask(2, "shell", ShellArguments{Command: "   "}); answer.OK || !strings.Contains(answer.Error, "no command") {
		t.Fatalf("an empty command is said: %+v", answer)
	}
	if answer := ask(3, "shell", ShellArguments{Command: "sleep 5", Timeout: 1}); !answer.OK || !strings.Contains(string(answer.Data), `"timedOut":true`) {
		t.Fatalf("a long command is stopped: %+v", answer)
	}
	answer = ask(4, "filesystem", FilesystemArguments{Action: "list", Path: "."})
	if !answer.OK || !strings.Contains(string(answer.Data), `"name":"notes.txt"`) {
		t.Fatalf("list %+v", answer)
	}
	answer = ask(5, "filesystem", FilesystemArguments{Action: "read", Path: "~/notes.txt", Offset: 1, Limit: 1})
	if !answer.OK || !strings.Contains(string(answer.Data), `"content":"second"`) || !strings.Contains(string(answer.Data), `"more":true`) {
		t.Fatalf("read %+v", answer)
	}
	if answer := ask(6, "filesystem", FilesystemArguments{Action: "info", Path: "/"}); !answer.OK || !strings.Contains(string(answer.Data), `"kind":"directory"`) {
		t.Fatalf("anywhere on the machine, as the person: %+v", answer)
	}
	if answer := ask(7, "filesystem", FilesystemArguments{Action: "write", Path: "made/later.txt", Content: "x"}); !answer.OK {
		t.Fatalf("a write makes the directories on the way: %+v", answer)
	}
	if answer := ask(8, "filesystem", FilesystemArguments{Action: "append", Path: "made/later.txt", Content: "yz\n"}); !answer.OK {
		t.Fatalf("append %+v", answer)
	}
	if answer := ask(9, "filesystem", FilesystemArguments{Action: "edit", Path: "made/later.txt", Find: "xyz", Replace: "abc"}); !answer.OK || !strings.Contains(string(answer.Data), `"replaced":1`) {
		t.Fatalf("edit %+v", answer)
	}
	if answer := ask(17, "filesystem", FilesystemArguments{Action: "edit", Path: "notes.txt", Find: "nowhere"}); answer.OK || !strings.Contains(answer.Error, "does not contain") {
		t.Fatalf("an edit of text that is not there changes nothing: %+v", answer)
	}
	if answer := ask(18, "filesystem", FilesystemArguments{Action: "edit", Path: "notes.txt", Find: "d", Replace: "D"}); answer.OK || !strings.Contains(answer.Error, "times") {
		t.Fatalf("an ambiguous edit changes nothing: %+v", answer)
	}
	if answer := ask(19, "filesystem", FilesystemArguments{Action: "edit", Path: "notes.txt", Find: "d", Replace: "D", All: true}); !answer.OK || !strings.Contains(string(answer.Data), `"replaced":2`) {
		t.Fatalf("all replaces every one: %+v", answer)
	}
	if answer := ask(20, "filesystem", FilesystemArguments{Action: "copy", Path: "made/later.txt", Destination: "copies/later.txt"}); !answer.OK {
		t.Fatalf("copy %+v", answer)
	}
	if content, err := os.ReadFile(filepath.Join(root, "copies", "later.txt")); err != nil || string(content) != "abc\n" {
		t.Fatalf("the copy carries the edited content: %q %v", content, err)
	}
	if answer := ask(10, "filesystem", FilesystemArguments{Action: "search", Path: ".", Pattern: "*.txt"}); !answer.OK || !strings.Contains(string(answer.Data), "later.txt") || !strings.Contains(string(answer.Data), "notes.txt") {
		t.Fatalf("search %+v", answer)
	}
	if answer := ask(15, "filesystem", FilesystemArguments{Action: "grep", Path: ".", Pattern: "sec.n[dD]|^abc$"}); !answer.OK || !strings.Contains(string(answer.Data), `"line":2,"text":"seconD"`) || !strings.Contains(string(answer.Data), "later.txt") {
		t.Fatalf("grep %+v", answer)
	}
	if answer := ask(16, "filesystem", FilesystemArguments{Action: "grep", Path: "notes.txt", Pattern: "("}); answer.OK || !strings.Contains(answer.Error, "regular expression") {
		t.Fatalf("a bad pattern is said: %+v", answer)
	}
	if answer := ask(11, "filesystem", FilesystemArguments{Action: "move", Path: "made/later.txt", Destination: "moved.txt"}); !answer.OK {
		t.Fatalf("move %+v", answer)
	}
	if answer := ask(13, "filesystem", FilesystemArguments{Action: "delete", Path: "moved.txt"}); !answer.OK {
		t.Fatalf("delete %+v", answer)
	}
	if answer := ask(14, "burn", nil); answer.OK || !strings.Contains(answer.Error, "not something") {
		t.Fatalf("an unknown action: %+v", answer)
	}
	cancel()
	if err := <-done; err == nil {
		t.Fatal("a cancelled context ends the service")
	}
}

func TestServeStopsWhenRefused(t *testing.T) {
	connection := &fakeConnection{incoming: make(chan message, 2), outgoing: make(chan message, 2)}
	done := make(chan error, 1)
	go func() { done <- Serve(context.Background(), connection, &Options{Token: "bad", Home: t.TempDir()}) }()
	connection.next(t, "hello")
	connection.incoming <- message{Type: "refused", Reason: "the token is not one this server takes"}
	err := <-done
	var refused *RefusedError
	if !errorAs(err, &refused) || !strings.Contains(err.Error(), "not one this server takes") {
		t.Fatalf("refused: %v", err)
	}
}

func errorAs(err error, target **RefusedError) bool {
	refused, ok := err.(*RefusedError)
	if ok {
		*target = refused
	}
	return ok
}
