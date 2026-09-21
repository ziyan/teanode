package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// signedIn is a fake of both halves: the sign-in that hands out tokens and
// the service that takes them.
func signedIn(test *testing.T, handle http.HandlerFunc) (*codex, *httptest.Server) {
	test.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, "/token") {
			_, _ = io.WriteString(writer, `{"access_token":"an-access-token","expires_in":3600}`)
			return
		}
		handle(writer, request)
	}))
	made, err := newCodex(server.URL, "a-refresh-token", "an-account", server.Client())
	if err != nil {
		test.Fatalf("newCodex: %s", err)
	}
	made.signIn.tokenUrl = server.URL + "/token"
	return made, server
}

// A conversation becomes what the responses protocol wants.
//
// The two protocols disagree about shape: a system message is a field and
// not a message, what a tool answered is its own kind of item rather than a
// message with a role, and a round in which the model only called something
// carries no message at all.
func TestAConversationBecomesResponseItems(test *testing.T) {
	test.Parallel()

	temperature := 0.5
	made, server := signedIn(test, nil)
	defer server.Close()

	encoded, err := made.encode(&ChatRequest{
		Model: "gpt-5.5",
		Messages: []ChatMessage{
			{Role: RoleSystem, Content: "Be brief."},
			{Role: RoleSystem, Content: "And plain."},
			{Role: RoleUser, Content: "What is on tomorrow?"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "call-1", Name: "calendar", Arguments: `{"day":"tomorrow"}`}}},
			{Role: RoleTool, ToolCallID: "call-1", Content: "Nothing."},
			{Role: RoleAssistant, Content: "Nothing at all."},
		},
		Tools:       []ToolDefinition{{Name: "calendar", Description: "What is on", Parameters: map[string]any{"type": "object"}}},
		Temperature: &temperature,
		MaxTokens:   256,
	})
	if err != nil {
		test.Fatalf("encode: %s", err)
	}

	var body codexRequest
	if err := json.Unmarshal(encoded, &body); err != nil {
		test.Fatalf("what it sent was not readable: %s", err)
	}
	// Both system messages, joined rather than the last winning.
	if body.Instructions != "Be brief.\n\nAnd plain." {
		test.Errorf("the instructions came out %q", body.Instructions)
	}
	// Nothing is left on the service: what is sent is a person's own mail.
	if body.Store {
		test.Error("it asked the service to keep a copy")
	}
	if !body.Stream {
		test.Error("the protocol streams, and it did not ask to")
	}
	if body.MaxTokens != 256 || body.Temperature == nil || *body.Temperature != 0.5 {
		test.Errorf("the bounds came out %d %v", body.MaxTokens, body.Temperature)
	}

	kinds := make([]string, 0, len(body.Input))
	for _, item := range body.Input {
		kinds = append(kinds, item.Type)
	}
	want := []string{"message", "function_call", "function_call_output", "message"}
	if fmt.Sprint(kinds) != fmt.Sprint(want) {
		test.Fatalf("the items came out %v, wanted %v", kinds, want)
	}
	// The round that only called something carries no message: an
	// assistant item with no content is refused by the service.
	if body.Input[1].CallID != "call-1" || body.Input[1].Name != "calendar" {
		test.Errorf("the call came out %+v", body.Input[1])
	}
	if body.Input[2].CallID != "call-1" || body.Input[2].Output != "Nothing." {
		test.Errorf("what the tool answered came out %+v", body.Input[2])
	}
	if len(body.Tools) != 1 || body.Tools[0].Type != "function" || body.Tools[0].Name != "calendar" {
		test.Errorf("the tools came out %+v", body.Tools)
	}
}

// The stream becomes text, calls and a finished answer with what it cost.
func TestTheStreamBecomesAnAnswer(test *testing.T) {
	test.Parallel()

	made, server := signedIn(test, func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("ChatGPT-Account-Id"); got != "an-account" {
			test.Errorf("the account went as %q", got)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer an-access-token" {
			test.Errorf("the token went as %q", got)
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		for _, event := range []string{
			`{"type":"response.output_text.delta","delta":"Not"}`,
			`{"type":"response.output_text.delta","delta":"hing."}`,
			`{"type":"response.output_item.done","item":{"type":"function_call","call_id":"call-9","name":"calendar","arguments":"{}"}}`,
			`{"type":"response.completed","response":{"id":"resp-1","usage":{"input_tokens":120,"output_tokens":8,"input_tokens_details":{"cached_tokens":100}}}}`,
		} {
			_, _ = io.WriteString(writer, "data: "+event+"\n\n")
		}
	})
	defer server.Close()

	answer, err := made.Chat(context.Background(), &ChatRequest{
		Model: "gpt-5.5", Messages: []ChatMessage{{Role: RoleUser, Content: "well?"}},
	})
	if err != nil {
		test.Fatalf("Chat: %s", err)
	}
	if answer.Message.Content != "Nothing." {
		test.Errorf("it said %q", answer.Message.Content)
	}
	if len(answer.Message.ToolCalls) != 1 || answer.Message.ToolCalls[0].ID != "call-9" {
		test.Errorf("the calls came back %+v", answer.Message.ToolCalls)
	}
	if answer.FinishReason != "tool_calls" {
		test.Errorf("it finished as %q", answer.FinishReason)
	}
	// The protocol counts cached tokens inside the input and this package
	// counts them beside it, so the two must not be added twice.
	if answer.Usage.PromptTokens != 20 || answer.Usage.CacheReadTokens != 100 {
		test.Errorf("the usage came back %+v", answer.Usage)
	}
	if answer.Usage.Total() != 128 {
		test.Errorf("the total came to %d", answer.Usage.Total())
	}
}

// A stream that stops before it finishes is an error, not a short answer.
//
// The alternative is a reply that silently ends mid-sentence, which reads
// as the model having nothing more to say.
func TestAStreamCutShortIsNotAFinishedAnswer(test *testing.T) {
	test.Parallel()

	made, server := signedIn(test, func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, "data: "+`{"type":"response.output_text.delta","delta":"Half a sen"}`+"\n\n")
	})
	defer server.Close()

	if _, err := made.Chat(context.Background(), &ChatRequest{
		Model: "gpt-5.5", Messages: []ChatMessage{{Role: RoleUser, Content: "well?"}},
	}); err == nil {
		test.Fatal("a stream that stopped early was taken as a finished answer")
	}
}

// What the service says about a refusal is carried, because the two an
// operator meets are a model the plan does not have and an allowance
// already spent, and those want different things done about them.
func TestARefusalSaysWhichRefusalItIs(test *testing.T) {
	test.Parallel()

	for _, each := range []struct {
		status int
		body   string
		expect string
	}{
		{http.StatusBadRequest, `{"detail":"The 'gpt-5.6' model is not supported when using Codex with a ChatGPT account."}`, "not supported"},
		{http.StatusTooManyRequests, `{"error":{"type":"usage_limit_reached","message":"The usage limit has been reached","resets_at":1790472515}}`, "usage limit"},
	} {
		made, server := signedIn(test, func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(each.status)
			_, _ = io.WriteString(writer, each.body)
		})
		_, err := made.Chat(context.Background(), &ChatRequest{
			Model: "gpt-5.5", Messages: []ChatMessage{{Role: RoleUser, Content: "well?"}},
		})
		server.Close()

		if err == nil || !strings.Contains(err.Error(), each.expect) {
			test.Errorf("%d came back as %v", each.status, err)
		}
	}
	// And the allowance one says when it turns over, which is the whole of
	// what an operator can do about it.
	made, server := signedIn(test, func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusTooManyRequests)
		_, _ = fmt.Fprintf(writer, `{"error":{"message":"spent","resets_at":%d}}`, time.Now().Add(time.Hour).Unix())
	})
	defer server.Close()
	_, err := made.Chat(context.Background(), &ChatRequest{
		Model: "gpt-5.5", Messages: []ChatMessage{{Role: RoleUser, Content: "well?"}},
	})
	if err == nil || !strings.Contains(err.Error(), "until") {
		test.Errorf("it did not say when the allowance turns over: %v", err)
	}
}

// A token the service will not take is thrown away and fetched again, once.
func TestATokenTheServiceRefusesIsFetchedAgainOnce(test *testing.T) {
	test.Parallel()

	tokens, calls := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, "/token") {
			tokens++
			_, _ = fmt.Fprintf(writer, `{"access_token":"token-%d","expires_in":3600}`, tokens)
			return
		}
		calls++
		if calls == 1 {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "data: "+`{"type":"response.completed","response":{"id":"r"}}`+"\n\n")
	}))
	defer server.Close()

	made, _ := newCodex(server.URL, "a-refresh-token", "an-account", server.Client())
	made.signIn.tokenUrl = server.URL + "/token"

	if _, err := made.Chat(context.Background(), &ChatRequest{
		Model: "gpt-5.5", Messages: []ChatMessage{{Role: RoleUser, Content: "well?"}},
	}); err != nil {
		test.Fatalf("Chat: %s", err)
	}
	if tokens != 2 {
		test.Errorf("it signed in %d times", tokens)
	}
	if calls != 2 {
		test.Errorf("it called %d times", calls)
	}
}

// It is a provider that chats, and says what it answers to without asking.
func TestTheSignedInProviderChatsAndNamesItsModels(test *testing.T) {
	test.Parallel()

	service, err := NewSignedInProvider("openai-codex", "", "a-refresh-token", "an-account", time.Second)
	if err != nil {
		test.Fatalf("NewSignedInProvider: %s", err)
	}
	if _, ok := any(service).(Provider); !ok {
		test.Error("a signed-in provider does not hold a conversation")
	}
	models, err := service.ListModels(context.Background())
	if err != nil || len(models) == 0 {
		test.Fatalf("ListModels: %v %v", models, err)
	}

	// Without a refresh token there is nothing to sign in with, and that is
	// refused when it is built rather than at the first request.
	if _, err := NewSignedInProvider("openai-codex", "", "  ", "", time.Second); err == nil {
		test.Error("a sign-in with no refresh token was accepted")
	}
	if _, err := NewSignedInProvider("openai", "", "a-refresh-token", "", time.Second); err == nil {
		test.Error("a keyed kind was accepted as one that signs in")
	}
}
