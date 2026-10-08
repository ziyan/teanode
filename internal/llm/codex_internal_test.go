package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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
	// A sign-in of its own, at the fake service: the process-wide ones are
	// shared by token, and a test must not point another's elsewhere.
	signer, err := newSignIn(server.URL+"/token", codexClientId, "a-refresh-token", server.Client())
	if err != nil {
		test.Fatalf("newSignIn: %s", err)
	}
	made := &codex{baseUrl: server.URL, account: "an-account", signIn: signer, http: server.Client()}
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
	// No output limit: the plan's endpoint refuses one. And no reasoning
	// unless asked, which the plan must be told; a model told how to
	// reason takes no temperature.
	if body.MaxTokens != 0 || body.Temperature != nil || body.Reasoning == nil || body.Reasoning.Effort != "none" {
		test.Errorf("the bounds came out %d %v %+v", body.MaxTokens, body.Temperature, body.Reasoning)
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

	// A sign-in of its own, as signedIn makes: pointing a shared one at this
	// server wrote to it while the tests running beside this one read it.
	signer, err := newSignIn(server.URL+"/token", codexClientId, "a-refresh-token", server.Client())
	if err != nil {
		test.Fatalf("newSignIn: %s", err)
	}
	made := &codex{baseUrl: server.URL, account: "an-account", signIn: signer, http: server.Client()}

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

// It is a provider that chats.
func TestTheSignedInProviderChats(test *testing.T) {
	test.Parallel()

	service, err := NewSignedInProvider("openai-codex", "", "a-refresh-token-for-"+test.Name(), "an-account", time.Second)
	if err != nil {
		test.Fatalf("NewSignedInProvider: %s", err)
	}
	if _, ok := any(service).(Provider); !ok {
		test.Error("a signed-in provider does not hold a conversation")
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

// A rotated refresh token is reported, because nothing here writes the
// configuration and the one in it has just gone stale. Unsaid, the server
// signs in perfectly well until it restarts and then cannot, with nothing
// connecting the two.
func TestARotationIsReportedByTheProvider(test *testing.T) {
	test.Parallel()

	made, err := newCodex("https://example.test", "a-refresh-token-for-"+test.Name(), "an-account", nil)
	if err != nil {
		test.Fatalf("newCodex: %s", err)
	}
	if made.signIn.rotated == nil {
		test.Fatal("a rotation would pass unremarked")
	}
	// It says something rather than panicking on a nil logger or writing
	// the token out, which would put a secret in the log.
	made.signIn.rotated("the-next-one")
}

// The plan's endpoint refuses an output limit and the keyed one takes it,
// so only the keyed one is sent it.
func TestOnlyTheKeyedEndpointIsSentAnOutputLimit(test *testing.T) {
	request := &ChatRequest{Model: "gpt-5.5", MaxTokens: 8000, Messages: []ChatMessage{{Role: RoleUser, Content: "hello"}}}
	onPlan, err := (&codex{}).encode(request)
	if err != nil {
		test.Fatal(err)
	}
	if strings.Contains(string(onPlan), "max_output_tokens") {
		test.Errorf("the plan's endpoint was sent a limit: %s", onPlan)
	}
	keyed, err := (&codex{doesTakeOutputLimit: true}).encode(request)
	if err != nil {
		test.Fatal(err)
	}
	if !strings.Contains(string(keyed), `"max_output_tokens":8000`) {
		test.Errorf("the keyed endpoint was not sent the limit: %s", keyed)
	}
}

// The plan's usage is read from the answer's headers, fractions and all,
// and a missing header is no reading.
func TestThePlanSaysHowMuchOfItIsUsed(test *testing.T) {
	header := http.Header{}
	header.Set("X-Codex-Primary-Used-Percent", "12.5")
	header.Set("X-Codex-Secondary-Used-Percent", "3")
	made := &codex{}
	made.notePlanUsage(header)
	if !made.planUsage.isKnown || made.planUsage.primaryPercent != 12 || made.planUsage.secondaryPercent != 3 {
		test.Errorf("it read %d%% and %d%%", made.planUsage.primaryPercent, made.planUsage.secondaryPercent)
	}
	// An answer that mentions only one window leaves the other as it was.
	onlyShort := http.Header{}
	onlyShort.Set("X-Codex-Primary-Used-Percent", "20")
	made.notePlanUsage(onlyShort)
	if made.planUsage.primaryPercent != 20 || made.planUsage.secondaryPercent != 3 {
		test.Errorf("after one window it read %d%% and %d%%", made.planUsage.primaryPercent, made.planUsage.secondaryPercent)
	}
	if _, isKnown := usedPercent(http.Header{}, "X-Codex-Secondary-Used-Percent"); isKnown {
		test.Error("a missing header was read as a percentage")
	}
}

// A reading is handed on to be kept when it moves, not on every answer, and
// one kept from before a restart is shown until the plan answers again.
func TestThePlanReadingOutlivesARestart(test *testing.T) {
	header := http.Header{}
	header.Set("X-Codex-Plan-Type", "pro")
	header.Set("X-Codex-Primary-Used-Percent", "40")
	header.Set("X-Codex-Primary-Window-Minutes", "300")
	var kept []*PlanUsage
	before := &codex{}
	before.onPlanUsage(func(usage *PlanUsage) { kept = append(kept, usage) })
	before.notePlanUsage(header)
	before.notePlanUsage(header)
	if len(kept) != 1 {
		test.Fatalf("an unchanged reading was kept %d times", len(kept))
	}
	written, err := json.Marshal(kept[0])
	if err != nil {
		test.Fatal(err)
	}

	var read PlanUsage
	if err := json.Unmarshal(written, &read); err != nil {
		test.Fatal(err)
	}
	after := &codex{}
	if after.planUsageNow(time.Now()) != nil {
		test.Fatal("a provider that has heard nothing has a reading")
	}
	after.restorePlanUsage(&read)
	restored := after.planUsageNow(time.Now())
	if restored == nil || restored.PlanName != "pro" || len(restored.Windows) != 1 || restored.Windows[0].UsedPercent != 40 {
		test.Fatalf("the kept reading came back as %+v", restored)
	}

	// Once the plan answers, a reading kept from before does not replace it.
	header.Set("X-Codex-Primary-Used-Percent", "45")
	after.notePlanUsage(header)
	after.restorePlanUsage(&read)
	if now := after.planUsageNow(time.Now()); now.Windows[0].UsedPercent != 45 {
		test.Errorf("an old reading replaced a new one: %d%%", now.Windows[0].UsedPercent)
	}
}

// A plan that will not go without reasoning is asked for the least it
// takes, once refused and from then on.
func TestAPlanThatMustReasonIsAskedForLittle(test *testing.T) {
	test.Parallel()

	var mutex sync.Mutex
	var efforts []string
	made, server := signedIn(test, func(writer http.ResponseWriter, request *http.Request) {
		var body codexRequest
		_ = json.NewDecoder(request.Body).Decode(&body)
		effort := ""
		if body.Reasoning != nil {
			effort = body.Reasoning.Effort
		}
		mutex.Lock()
		efforts = append(efforts, effort)
		mutex.Unlock()
		if effort == "none" {
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(writer, `{"error":{"message":"Unsupported value: 'none' is not supported with this model for reasoning.effort"}}`)
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "data: "+`{"type":"response.output_text.delta","delta":"Fine."}`+"\n\n")
		_, _ = io.WriteString(writer, "data: "+`{"type":"response.completed","response":{"id":"resp-1","usage":{"input_tokens":10,"output_tokens":2}}}`+"\n\n")
	})
	defer server.Close()

	for attempt := 0; attempt < 2; attempt++ {
		answer, err := made.Chat(context.Background(), &ChatRequest{Model: "gpt-5.5", Messages: []ChatMessage{{Role: RoleUser, Content: "well?"}}})
		if err != nil || answer.Message.Content != "Fine." {
			test.Fatalf("attempt %d: %+v, %v", attempt, answer, err)
		}
	}
	if strings.Join(efforts, ",") != "none,low,low" {
		test.Errorf("it asked for %v", efforts)
	}
}

// A model that refuses no reasoning inside the stream, as the plan does,
// is asked again for low, and only that model: another on the same plan
// still goes without. The refusal says what it was, not that nothing was
// said.
func TestAModelThatMustReasonSaysSoInTheStream(test *testing.T) {
	test.Parallel()

	var mutex sync.Mutex
	var asked []string
	made, server := signedIn(test, func(writer http.ResponseWriter, request *http.Request) {
		var body codexRequest
		_ = json.NewDecoder(request.Body).Decode(&body)
		effort := ""
		if body.Reasoning != nil {
			effort = body.Reasoning.Effort
		}
		mutex.Lock()
		asked = append(asked, body.Model+":"+effort)
		mutex.Unlock()
		writer.Header().Set("Content-Type", "text/event-stream")
		if effort == "none" && body.Model == "insists" {
			_, _ = io.WriteString(writer, "event: error\ndata: "+`{"type":"error","error":{"message":"Unsupported value: 'none' is not supported with the 'insists' model.","param":"reasoning.effort","code":"unsupported_value"}}`+"\n\n")
			return
		}
		_, _ = io.WriteString(writer, "data: "+`{"type":"response.output_text.delta","delta":"Fine."}`+"\n\n")
		_, _ = io.WriteString(writer, "data: "+`{"type":"response.completed","response":{"id":"resp-1","usage":{"input_tokens":10,"output_tokens":2}}}`+"\n\n")
	})
	defer server.Close()

	for _, model := range []string{"insists", "insists", "relaxed"} {
		answer, err := made.Chat(context.Background(), &ChatRequest{Model: model, Messages: []ChatMessage{{Role: RoleUser, Content: "well?"}}})
		if err != nil || answer.Message.Content != "Fine." {
			test.Fatalf("%s: %+v, %v", model, answer, err)
		}
	}
	if strings.Join(asked, ",") != "insists:none,insists:low,insists:low,relaxed:none" {
		test.Errorf("it asked %v", asked)
	}

	// Another refusal in the stream is said in its own words.
	busy, refusing := signedIn(test, func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "data: "+`{"type":"error","error":{"message":"The model is busy.","param":""}}`+"\n\n")
	})
	defer refusing.Close()
	if _, err := busy.Chat(context.Background(), &ChatRequest{Model: "relaxed", Messages: []ChatMessage{{Role: RoleUser, Content: "well?"}}}); err == nil || !strings.Contains(err.Error(), "The model is busy.") {
		test.Errorf("the refusal said %v", err)
	}
}

// How long a window is and when it resets are read as the service says
// them: minutes for the one, seconds from now for the other.
func TestAPlanWindowSaysItsLengthAndReset(test *testing.T) {
	header := http.Header{}
	header.Set("X-Codex-Primary-Window-Minutes", "300")
	header.Set("X-Codex-Primary-Reset-After-Seconds", "7260")
	if got := windowOf(header, "X-Codex-Primary-Window-Minutes"); got != "5h0m0s" {
		test.Errorf("the window read %q", got)
	}
	if got := resetOf(header, "X-Codex-Primary-Reset-After-Seconds"); got != "2h1m0s" {
		test.Errorf("the reset read %q", got)
	}
	if got := resetOf(header, "X-Codex-Secondary-Reset-After-Seconds"); got != "a time not said" {
		test.Errorf("a missing reset read %q", got)
	}
}

// Providers built from one refresh token share one sign-in, and so does one
// built from a token the sign-in held before it was rotated: a second holder
// that refreshed on its own would leave the first holding a token the
// service no longer honours. The first holder's handling of a rotation is
// kept.
func TestProvidersFromOneTokenShareTheSignIn(test *testing.T) {
	first, err := newCodex("https://example.test", "a-token-for-"+test.Name(), "an-account", nil)
	if err != nil {
		test.Fatal(err)
	}
	kept := ""
	first.onRotated(func(refreshToken string) { kept = refreshToken })

	second, err := newCodex("https://example.test", "a-token-for-"+test.Name(), "an-account", nil)
	if err != nil {
		test.Fatal(err)
	}
	if second.signIn != first.signIn {
		test.Fatal("two providers from one token hold two sign-ins")
	}
	second.signIn.rotated("the-rotated")
	if kept != "the-rotated" {
		test.Error("the second provider replaced the first one's handling of a rotation")
	}

	first.adoptRefreshToken("a-newer-token-for-" + test.Name())
	behind, err := newCodex("https://example.test", "a-token-for-"+test.Name(), "an-account", nil)
	if err != nil {
		test.Fatal(err)
	}
	if behind.signIn != first.signIn {
		test.Error("a provider built from a token the sign-in held before was given its own")
	}
	other, err := newCodex("https://example.test", "another-token-for-"+test.Name(), "an-account", nil)
	if err != nil {
		test.Fatal(err)
	}
	if other.signIn == first.signIn {
		test.Error("a different token was given the same sign-in")
	}
}

// The models offered are the ones the service lists for the account, asked
// for as the whole list, less those it offers to nobody.
func TestThePlanListsItsOwnModels(test *testing.T) {
	test.Parallel()

	made, server := signedIn(test, func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/codex/models" || request.URL.Query().Get("client_version") != codexCatalogVersion {
			test.Errorf("the list was asked for at %s", request.URL)
		}
		if got := request.Header.Get("ChatGPT-Account-Id"); got != "an-account" {
			test.Errorf("the account went as %q", got)
		}
		_, _ = io.WriteString(writer, `{"models":[`+
			`{"slug":"model-new","visibility":"list","context_window":272000},`+
			`{"slug":"model-internal","visibility":"hide","context_window":272000},`+
			`{"slug":"model-old","visibility":"list"}]}`)
	})
	defer server.Close()

	models, err := made.ListModels(context.Background())
	if err != nil {
		test.Fatal(err)
	}
	if len(models) != 2 || models[0].ID != "model-new" || models[0].ContextLength != 272000 || models[1].ID != "model-old" {
		test.Errorf("it offered %+v", models)
	}
}

// What is true this round goes after the conversation, where the loop put
// it, not into the instructions: they come first, and a line in them that
// changes every round -- the time -- left nothing after it to be read from
// the cache. The rounds of one conversation share a cache key, and a round
// may ask for several tools at once.
func TestTheRoundsOverlayStaysAfterTheConversation(test *testing.T) {
	provider := &codex{}
	const conversationId = "conversation-1"
	encoded, err := provider.encode(&ChatRequest{
		Model: "plan-model",
		Messages: []ChatMessage{
			{Role: RoleSystem, Content: "Be brief."},
			{Role: RoleUser, Content: "What is on today?"},
			{Role: RoleSystem, Content: "<now>Saturday 14:06</now>"},
		},
		Tools:    []ToolDefinition{{Name: "calendar", Description: "What is on", Parameters: map[string]any{"type": "object"}}},
		CacheKey: conversationId,
	})
	if err != nil {
		test.Fatalf("encode: %s", err)
	}
	var body codexRequest
	if err := json.Unmarshal(encoded, &body); err != nil {
		test.Fatalf("what it sent was not readable: %s", err)
	}
	if body.Instructions != "Be brief." {
		test.Errorf("the instructions should be the prompt alone, came out %q", body.Instructions)
	}
	if len(body.Input) != 2 || body.Input[1].Role != "developer" || body.Input[1].Content[0].Text != "<now>Saturday 14:06</now>" {
		test.Fatalf("the overlay should follow the conversation as the developer's message: %+v", body.Input)
	}
	if body.PromptCacheKey != conversationId {
		test.Errorf("the cache key came out %q", body.PromptCacheKey)
	}
	if body.ParallelToolCalls == nil || !*body.ParallelToolCalls {
		test.Error("a round with tools should be able to call several at once")
	}
}
