package llm

import (
	"context"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/decide"
)

// Decider is a service that answers a question whose answers are known in
// advance, without writing any prose. TypeSafe's API has it; the chat
// services do not, so it is a separate interface a caller asserts, as
// Embedder is.
//
// The two kinds are not alternatives and neither replaces the other. A
// decider cannot write a reply; a chat model cannot say how sure it is of
// one answer against the rest. Work that decides asks this where one is
// configured, and asks a model where none is.
type Decider interface {
	// Decide answers every question about the state in one call.
	Decide(ctx context.Context, state string, questions map[string]decide.Question) (decide.Answers, error)
}

// typeSafe speaks TypeSafe's decision API.
//
// It is a Service and a Decider and deliberately not a Provider: it has no
// Chat and no ChatStream, not even ones that return an error. Something
// that cannot hold a conversation should fail to satisfy the interface for
// holding one, so the registry can say which provider cannot do what was
// asked of it rather than handing back a client that apologises at the
// moment of use.
type typeSafe struct {
	client *decide.Client
}

// The address the service answers at, where an operator names none.
const typeSafeBaseUrl = "https://api.typesafe.ai/v1"

// What it answers to. The service has no endpoint that lists its models,
// and a provider contributing nothing to the model list reads on the
// settings page as one that could not be reached, so the name it takes is
// written down here.
//
// "jev-latest" follows the service's newest. An operator who needs an
// answer to mean the same thing next month names a dated version instead,
// which the model filter on the provider lets through unchanged.
var typeSafeModels = []string{"jev-latest"}

func newTypeSafe(baseUrl, apiKey string, timeout time.Duration) (*typeSafe, error) {
	if baseUrl == "" {
		baseUrl = typeSafeBaseUrl
	}
	client, err := decide.New(baseUrl, apiKey, "", timeout)
	if err != nil {
		return nil, err
	}
	return &typeSafe{client: client}, nil
}

func (self *typeSafe) Kind() string {
	return config.AgentProviderKindTypeSafe
}

// ListModels is what this service answers to. It asks nothing: there is no
// list endpoint, and the names do not change between releases.
func (self *typeSafe) ListModels(_ context.Context) ([]ModelInformation, error) {
	models := make([]ModelInformation, 0, len(typeSafeModels))
	for _, name := range typeSafeModels {
		models = append(models, ModelInformation{ID: name})
	}
	return models, nil
}

// Decide answers questions about one state.
func (self *typeSafe) Decide(ctx context.Context, state string, questions map[string]decide.Question) (decide.Answers, error) {
	return self.client.Ask(ctx, state, questions)
}
