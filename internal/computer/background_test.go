package computer

import (
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"testing"
	"time"
)

// testComputer is Serve over a fake connection, past the welcome.
type testComputer struct {
	t          *testing.T
	connection *fakeConnection
	// told holds the endings that arrived while an answer was awaited.
	told []message
}

func serveForTest(t *testing.T, ctx context.Context, background *BackgroundCommands) *testComputer {
	t.Helper()
	connection := &fakeConnection{incoming: make(chan message, 8), outgoing: make(chan message, 32)}
	go func() {
		_ = Serve(ctx, connection, &Options{Token: "t", Name: "laptop", Home: t.TempDir(), Background: background})
	}()
	hello := connection.next(t, "hello")
	if len(hello.Features) != 1 || hello.Features[0] != FeatureBackground {
		t.Fatalf("the hello names background commands: %+v", hello)
	}
	connection.incoming <- message{Type: "welcome", Protocol: Protocol}
	return &testComputer{t: t, connection: connection}
}

func (self *testComputer) ask(id int64, action string, args any) message {
	self.t.Helper()
	encoded, _ := json.Marshal(args)
	self.connection.incoming <- message{Type: "act", ID: id, Action: action, Args: encoded}
	for {
		select {
		case sent := <-self.connection.outgoing:
			switch {
			case sent.Type == "background":
				self.told = append(self.told, sent)
			case sent.Type == "result" && sent.ID == id:
				return sent
			}
		case <-time.After(10 * time.Second):
			self.t.Fatalf("no answer to %s", action)
		}
	}
}

// ending is the next ending told, or nil when none comes within the wait.
func (self *testComputer) ending(wait time.Duration) *message {
	if len(self.told) > 0 {
		first := self.told[0]
		self.told = self.told[1:]
		return &first
	}
	deadline := time.After(wait)
	for {
		select {
		case sent := <-self.connection.outgoing:
			if sent.Type == "background" {
				return &sent
			}
		case <-deadline:
			return nil
		}
	}
}

func TestACommandPastItsWaitGoesOnInTheBackgroundAndSaysWhenItEnds(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the shell here is sh")
	}
	background := NewBackgroundCommands()
	defer background.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	computer := serveForTest(t, ctx, background)

	origin := json.RawMessage(`{"conversationId":"c1"}`)
	answer := computer.ask(1, "shell", ShellArguments{Command: "echo started; sleep 2; echo finished; exit 4", Timeout: 1, ShouldKeepOnTimeout: true, Origin: origin})
	var ran ShellResult
	_ = json.Unmarshal(answer.Data, &ran)
	if !answer.OK || ran.BackgroundID == "" || ran.TimedOut || ran.Stdout != "started\n" {
		t.Fatalf("the wait running out leaves it running, with what it wrote so far: %+v %+v", answer, ran)
	}

	// Read while it runs.
	answer = computer.ask(2, "background_read", BackgroundReadArguments{ID: ran.BackgroundID})
	var status BackgroundStatus
	_ = json.Unmarshal(answer.Data, &status)
	if !answer.OK || !status.IsRunning || status.Stdout != "started\n" {
		t.Fatalf("read while running: %+v %+v", answer, status)
	}

	ended := computer.ending(10 * time.Second)
	if ended == nil {
		t.Fatal("no ending was told")
	}
	var told BackgroundStatus
	_ = json.Unmarshal(ended.Data, &told)
	if ended.Event != "ended" || told.ID != ran.BackgroundID || told.IsRunning || told.ExitCode != 4 ||
		told.Stdout != "started\nfinished\n" || string(told.Origin) != string(origin) {
		t.Fatalf("the ending is told, with its origin and its output: %+v %+v", ended, told)
	}

	// Not acknowledged: a new connection hears it again.
	cancel()
	second, cancelSecond := context.WithCancel(context.Background())
	defer cancelSecond()
	computer = serveForTest(t, second, background)
	again := computer.ending(5 * time.Second)
	if again == nil || again.Session != ran.BackgroundID {
		t.Fatalf("an ending nobody acknowledged is told again: %+v", again)
	}
	if answer := computer.ask(3, "background_acknowledge", BackgroundAcknowledgeArguments{IDs: []string{ran.BackgroundID}}); !answer.OK {
		t.Fatalf("acknowledge: %+v", answer)
	}
	cancelSecond()
	third, cancelThird := context.WithCancel(context.Background())
	defer cancelThird()
	computer = serveForTest(t, third, background)
	answer = computer.ask(4, "background_list", struct{}{})
	var listed []BackgroundStatus
	_ = json.Unmarshal(answer.Data, &listed)
	if len(listed) != 1 || !listed[0].IsAcknowledged || listed[0].Stdout != "" {
		t.Fatalf("the list has it, acknowledged, without output: %+v", listed)
	}
	if told := computer.ending(300 * time.Millisecond); told != nil {
		t.Fatalf("an acknowledged ending is not told again: %+v", told)
	}
}

func TestABackgroundCommandIsStoppedWhenAsked(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the shell here is sh")
	}
	background := NewBackgroundCommands()
	defer background.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	computer := serveForTest(t, ctx, background)

	answer := computer.ask(1, "shell", ShellArguments{Command: "while true; do echo tick; sleep 0.1; done", IsBackground: true})
	var ran ShellResult
	_ = json.Unmarshal(answer.Data, &ran)
	if !answer.OK || ran.BackgroundID == "" {
		t.Fatalf("started in the background: %+v", answer)
	}
	time.Sleep(300 * time.Millisecond)
	answer = computer.ask(2, "background_stop", BackgroundStopArguments{ID: ran.BackgroundID})
	var status BackgroundStatus
	_ = json.Unmarshal(answer.Data, &status)
	if !answer.OK || status.IsRunning || status.StopReason != BackgroundStopReasonStopped || status.StdoutByteCount == 0 {
		t.Fatalf("stopped: %+v %+v", answer, status)
	}
	// Stopped by somebody who did not say they know: still told.
	told := computer.ending(5 * time.Second)
	if told == nil || told.Session != ran.BackgroundID {
		t.Fatalf("a stop the agent did not ask for is told: %+v", told)
	}

	answer = computer.ask(3, "shell", ShellArguments{Command: "sleep 30", IsBackground: true})
	_ = json.Unmarshal(answer.Data, &ran)
	answer = computer.ask(4, "background_stop", BackgroundStopArguments{ID: ran.BackgroundID, IsAcknowledged: true})
	if !answer.OK {
		t.Fatalf("stop: %+v", answer)
	}
	if told := computer.ending(300 * time.Millisecond); told != nil {
		t.Fatalf("a stop whoever asked already knows about is not told: %+v", told)
	}
}

func TestWithoutKeepOnTimeoutACommandPastItsWaitIsStillKilled(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the shell here is sh")
	}
	background := NewBackgroundCommands()
	defer background.Close()
	result, err := RunShell(context.Background(), &Options{Home: t.TempDir()}, background, &ShellArguments{Command: "echo a; sleep 5", Timeout: 1})
	if err != nil || !result.TimedOut || result.ExitCode != -1 || result.BackgroundID != "" || result.Stdout != "a\n" {
		t.Fatalf("a server that did not ask to keep it gets what it always got: %+v %v", result, err)
	}
	if listed := background.list(); len(listed) != 0 {
		t.Fatalf("and nothing is left in the background: %+v", listed)
	}
}

func TestOutputKeepsTheFirstAndTheLast(t *testing.T) {
	buffer := &outputBuffer{}
	_, _ = buffer.Write([]byte(strings.Repeat("h", outputHeadBytes)))
	_, _ = buffer.Write([]byte(strings.Repeat("m", outputTailBytes)))
	_, _ = buffer.Write([]byte("the end"))

	text, isTruncated := buffer.text()
	if !isTruncated || !strings.HasPrefix(text, "hhh") || !strings.HasSuffix(text, "mmmthe end") || !strings.Contains(text, "[7 bytes left out here]") {
		t.Fatalf("the head, a line for the middle, and the tail: %d bytes, truncated %v, %q...%q", len(text), isTruncated, text[:10], text[len(text)-40:])
	}
	if buffer.byteCount() != int64(outputBytes+7) {
		t.Fatalf("every byte is counted: %d", buffer.byteCount())
	}
	if tail, isTruncated := buffer.tail(7); tail != "the end" || !isTruncated {
		t.Fatalf("the tail is the end of the stream: %q", tail)
	}

	short := &outputBuffer{}
	_, _ = short.Write([]byte("one\ntwo\n"))
	if tail, isTruncated := short.tail(4); tail != "two\n" || !isTruncated {
		t.Fatalf("a short tail of a short stream: %q", tail)
	}
	if tail, isTruncated := short.tail(100); tail != "one\ntwo\n" || isTruncated {
		t.Fatalf("all of a short stream: %q", tail)
	}
}
