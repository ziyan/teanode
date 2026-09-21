package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/decide"
)

// A provider that decides is not a provider that writes, and the type says
// so rather than a method apologising at the moment of use.
//
// This is the whole point of splitting Service from Provider: a caller that
// wants a conversation asks for one and is refused here, where the refusal
// can name the provider, rather than being handed a client whose Chat
// returns an error the first time a person waits on it.
func TestADeciderIsNotAChatProvider(test *testing.T) {
	test.Parallel()

	service, err := NewProvider("typesafe", "https://example.test/v1", "a-key", time.Second)
	if err != nil {
		test.Fatalf("NewProvider: %s", err)
	}
	if _, ok := service.(Provider); ok {
		test.Error("a decider satisfied the interface for holding a conversation")
	}
	if _, ok := service.(Decider); !ok {
		test.Error("a decider does not satisfy the interface for deciding")
	}
	if _, ok := service.(Embedder); ok {
		test.Error("a decider satisfied the interface for embedding")
	}
	if service.Kind() != "typesafe" {
		test.Errorf("it calls itself %q", service.Kind())
	}

	// And it says what it answers to without being asked over the wire,
	// since the service has no endpoint that lists models and an empty
	// list reads as a provider that could not be reached.
	models, err := service.ListModels(context.Background())
	if err != nil || len(models) == 0 {
		test.Fatalf("ListModels: %v %v", models, err)
	}
}

// The chat providers are still chat providers, which is the other half of
// the same claim: the split did not quietly demote them.
func TestTheChatProvidersStillChat(test *testing.T) {
	test.Parallel()

	for _, kind := range []string{"openai", "anthropic", "gemini"} {
		service, err := NewProvider(kind, "https://example.test", "a-key", time.Second)
		if err != nil {
			test.Fatalf("%s: %s", kind, err)
		}
		if _, ok := service.(Provider); !ok {
			test.Errorf("%s no longer holds a conversation", kind)
		}
	}
}

// A decision goes out and comes back through the provider, so the wiring
// between this package and the client underneath is exercised and not just
// assumed.
func TestADecisionGoesThroughTheProvider(test *testing.T) {
	test.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/systemone" {
			test.Errorf("it asked at %q", request.URL.Path)
		}
		_, _ = io.WriteString(writer, `{"answers":{"open":{"type":"noul","noul":0.87}}}`)
	}))
	defer server.Close()

	service, err := NewProvider("typesafe", server.URL+"/v1", "a-key", time.Second)
	if err != nil {
		test.Fatalf("NewProvider: %s", err)
	}
	decider, ok := service.(Decider)
	if !ok {
		test.Fatal("it does not decide")
	}
	answers, err := decider.Decide(context.Background(), "a screenshot of a stack trace", map[string]decide.Question{
		"open": {
			Instructions: "Is it worth opening?",
			Choices:      map[string]string{"true": "It holds words", "false": "It is furniture"},
		},
	})
	if err != nil {
		test.Fatalf("Decide: %s", err)
	}
	if answers["open"].Yes != 0.87 {
		test.Errorf("the answer came back as %+v", answers["open"])
	}
}
