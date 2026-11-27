package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// shortenTimeouts makes the idle and total clocks tests' length. These tests
// change package variables, so none of them runs in parallel.
func shortenTimeouts(test *testing.T, idle, total time.Duration) {
	test.Helper()
	previousIdle, previousTotal := streamIdleTimeout, plainRequestTimeout
	streamIdleTimeout, plainRequestTimeout = idle, total
	test.Cleanup(func() { streamIdleTimeout, plainRequestTimeout = previousIdle, previousTotal })
}

// drain reads a stream to its end and says the text it carried, and how the
// stream ended.
func drain(test *testing.T, events <-chan StreamEvent) (string, error) {
	test.Helper()
	var text strings.Builder
	var failure error
	for event := range events {
		switch event.Kind {
		case StreamText:
			text.WriteString(event.Text)
		case StreamError:
			failure = event.Err
		}
	}
	return text.String(), failure
}

func chatCompletionsEvent(word string) string {
	return fmt.Sprintf("data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", word)
}

// An answer that keeps arriving is never cut, however long it runs: this one
// takes about three times the idle clock, and finishes.
func TestAStreamThatKeepsArrivingOutlastsTheIdleClock(test *testing.T) {
	shortenTimeouts(test, 300*time.Millisecond, time.Minute)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		for index := 0; index < 24; index++ {
			_, _ = io.WriteString(writer, chatCompletionsEvent("word "))
			writer.(http.Flusher).Flush()
			time.Sleep(40 * time.Millisecond)
		}
		_, _ = io.WriteString(writer, "data: [DONE]\n\n")
	}))
	defer server.Close()
	provider, err := chatFor("openai", server.URL, "a-key")
	if err != nil {
		test.Fatal(err)
	}
	started := time.Now()
	events, err := provider.ChatStream(context.Background(), &ChatRequest{Model: "m", Messages: []ChatMessage{{Role: RoleUser, Content: "read"}}})
	if err != nil {
		test.Fatal(err)
	}
	text, failure := drain(test, events)
	if failure != nil {
		test.Fatalf("a slow stream that never went quiet was cut: %s", failure)
	}
	if elapsed := time.Since(started); elapsed < 900*time.Millisecond {
		test.Fatalf("the stream ended after %s, so it did not outlast the idle clock", elapsed)
	}
	if got := strings.Count(text, "word"); got != 24 {
		test.Fatalf("%d of 24 words arrived", got)
	}
}

// Comment lines that keep a connection warm count as something arriving.
func TestACommentLineResetsTheIdleClock(test *testing.T) {
	shortenTimeouts(test, 300*time.Millisecond, time.Minute)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		for index := 0; index < 12; index++ {
			_, _ = io.WriteString(writer, ": still thinking\n")
			writer.(http.Flusher).Flush()
			time.Sleep(100 * time.Millisecond)
		}
		_, _ = io.WriteString(writer, chatCompletionsEvent("done")+"data: [DONE]\n\n")
	}))
	defer server.Close()
	provider, _ := chatFor("openai", server.URL, "a-key")
	events, err := provider.ChatStream(context.Background(), &ChatRequest{Model: "m", Messages: []ChatMessage{{Role: RoleUser, Content: "read"}}})
	if err != nil {
		test.Fatal(err)
	}
	if text, failure := drain(test, events); failure != nil || text != "done" {
		test.Fatalf("a stream with a heartbeat was cut: %q, %v", text, failure)
	}
}

// A stream that goes silent is cut at the idle clock, with the words that
// arrived before and an error that says what happened.
func TestAStreamThatGoesSilentIsCut(test *testing.T) {
	shortenTimeouts(test, 250*time.Millisecond, time.Minute)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(io.Discard, request.Body)
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, chatCompletionsEvent("first"))
		writer.(http.Flusher).Flush()
		select {
		case <-release:
		case <-request.Context().Done():
		}
	}))
	defer server.Close()
	defer close(release)
	provider, _ := chatFor("openai", server.URL, "a-key")
	started := time.Now()
	events, err := provider.ChatStream(context.Background(), &ChatRequest{Model: "m", Messages: []ChatMessage{{Role: RoleUser, Content: "read"}}})
	if err != nil {
		test.Fatal(err)
	}
	text, failure := drain(test, events)
	if failure == nil {
		test.Fatal("a stream that went silent ended as if it had finished")
	}
	if !strings.Contains(failure.Error(), "nothing arrived") || !errors.Is(failure, context.DeadlineExceeded) {
		test.Fatalf("the cut says %q", failure)
	}
	if text != "first" {
		test.Fatalf("the words before the silence were %q", text)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		test.Fatalf("the silence took %s to end", elapsed)
	}
}

// The wait for the first byte is under the idle clock as well.
func TestAStreamThatNeverStartsIsCut(test *testing.T) {
	shortenTimeouts(test, 250*time.Millisecond, time.Minute)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		// The body is read so that the server notices the client leaving.
		_, _ = io.Copy(io.Discard, request.Body)
		<-request.Context().Done()
	}))
	defer server.Close()
	provider, _ := chatFor("openai", server.URL, "a-key")
	_, err := provider.ChatStream(context.Background(), &ChatRequest{Model: "m", Messages: []ChatMessage{{Role: RoleUser, Content: "read"}}})
	if err == nil || !strings.Contains(err.Error(), "nothing arrived") {
		test.Fatalf("a stream that never started: %v", err)
	}
}

// The Responses stream, which makes its own request, follows the same rule.
func TestAResponsesStreamIsCutOnlyWhenItGoesSilent(test *testing.T) {
	shortenTimeouts(test, 300*time.Millisecond, time.Minute)
	isSilent := false
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(io.Discard, request.Body)
		writer.Header().Set("Content-Type", "text/event-stream")
		for index := 0; index < 12; index++ {
			_, _ = io.WriteString(writer, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"x\"}\n\n")
			writer.(http.Flusher).Flush()
			time.Sleep(60 * time.Millisecond)
		}
		if isSilent {
			select {
			case <-release:
			case <-request.Context().Done():
			}
			return
		}
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n")
	}))
	defer server.Close()
	defer close(release)
	provider := newOpenAI(server.URL, "a-key", server.Client())
	provider.speaksResponses = true
	request := &ChatRequest{Model: "gpt-5.6-terra", ReasoningEffort: EffortHigh, Messages: []ChatMessage{{Role: RoleUser, Content: "read"}}}

	events, err := provider.ChatStream(context.Background(), request)
	if err != nil {
		test.Fatal(err)
	}
	if text, failure := drain(test, events); failure != nil || text != strings.Repeat("x", 12) {
		test.Fatalf("a slow Responses stream: %q, %v", text, failure)
	}

	isSilent = true
	events, err = provider.ChatStream(context.Background(), request)
	if err != nil {
		test.Fatal(err)
	}
	if _, failure := drain(test, events); failure == nil || !strings.Contains(failure.Error(), "nothing arrived") {
		test.Fatalf("a silent Responses stream: %v", failure)
	}
}

// A call that does not stream has a total, and a dead connection ends at it.
func TestAPlainCallEndsAtItsTotal(test *testing.T) {
	shortenTimeouts(test, time.Minute, 300*time.Millisecond)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(io.Discard, request.Body)
		if strings.Contains(request.URL.RawQuery, "slow") {
			select {
			case <-time.After(5 * time.Second):
			case <-request.Context().Done():
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
		_, _ = io.WriteString(writer, `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{}}`)
	}))
	defer server.Close()
	request := &ChatRequest{Model: "m", Messages: []ChatMessage{{Role: RoleUser, Content: "hi"}}}

	provider, _ := chatFor("openai", server.URL, "a-key")
	answer, err := provider.Chat(context.Background(), request)
	if err != nil || answer.Message.Content != "ok" {
		test.Fatalf("a plain call inside its total: %v, %v", answer, err)
	}

	provider, _ = chatFor("openai", server.URL+"?slow", "a-key")
	started := time.Now()
	_, err = provider.Chat(context.Background(), request)
	if err == nil || !strings.Contains(err.Error(), "did not finish answering") || !errors.Is(err, context.DeadlineExceeded) {
		test.Fatalf("a plain call that never answered: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		test.Fatalf("the total took %s to end the call", elapsed)
	}
}

// The caller's own cancellation is still a cancellation, not a timeout.
func TestACancelledCallIsNotReportedAsATimeout(test *testing.T) {
	shortenTimeouts(test, time.Minute, time.Minute)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		// The body is read so that the server notices the client leaving.
		_, _ = io.Copy(io.Discard, request.Body)
		<-request.Context().Done()
	}))
	defer server.Close()
	provider, _ := chatFor("openai", server.URL, "a-key")
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	_, err := provider.Chat(ctx, &ChatRequest{Model: "m", Messages: []ChatMessage{{Role: RoleUser, Content: "hi"}}})
	if !errors.Is(err, context.Canceled) {
		test.Fatalf("a cancelled call: %v", err)
	}
	if strings.Contains(err.Error(), "nothing arrived") {
		test.Fatalf("a cancelled call was blamed on silence: %v", err)
	}
}

// The plan's own stream follows the same rule: a slow one that keeps
// arriving finishes, and one that goes silent is cut.
func TestASignedInStreamIsCutOnlyWhenItGoesSilent(test *testing.T) {
	shortenTimeouts(test, 300*time.Millisecond, time.Minute)
	isSilent := false
	release := make(chan struct{})
	made, server := signedIn(test, func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(io.Discard, request.Body)
		writer.Header().Set("Content-Type", "text/event-stream")
		for index := 0; index < 12; index++ {
			_, _ = io.WriteString(writer, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"x\"}\n\n")
			writer.(http.Flusher).Flush()
			time.Sleep(60 * time.Millisecond)
		}
		if isSilent {
			select {
			case <-release:
			case <-request.Context().Done():
			}
			return
		}
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n")
	})
	defer server.Close()
	defer close(release)
	request := &ChatRequest{Model: "gpt-5.5", Messages: []ChatMessage{{Role: RoleUser, Content: "read"}}}

	events, err := made.ChatStream(context.Background(), request)
	if err != nil {
		test.Fatal(err)
	}
	if text, failure := drain(test, events); failure != nil || text != strings.Repeat("x", 12) {
		test.Fatalf("a slow signed-in stream: %q, %v", text, failure)
	}

	isSilent = true
	events, err = made.ChatStream(context.Background(), request)
	if err != nil {
		test.Fatal(err)
	}
	if _, failure := drain(test, events); failure == nil || !errors.Is(failure, context.DeadlineExceeded) {
		test.Fatalf("a silent signed-in stream: %v", failure)
	}
}

// Both clocks' errors are recognized as the provider timing out, and a plain
// deadline or cancellation is not.
func TestProviderTimeoutsAreRecognized(t *testing.T) {
	for name, want := range map[string]struct {
		err error
		is  bool
	}{
		"idle":      {idleTimeoutOf(time.Minute), true},
		"plain":     {fmt.Errorf("wrapped: %w", plainTimeoutOf(time.Minute)), true},
		"deadline":  {context.DeadlineExceeded, false},
		"cancelled": {context.Canceled, false},
	} {
		if got := IsProviderTimeout(want.err); got != want.is {
			t.Errorf("%s: %v, want %v", name, got, want.is)
		}
	}
}
