// Package llm talks to language models and knows nothing about mail.
//
// One interface, Provider, over three wire APIs: the OpenAI chat completion
// API, which every compatible server also speaks; Anthropic's Messages API;
// and Google's Gemini API. The rest of the server asks the Registry for a
// provider and a model by the kind of work it is doing, and never for a
// provider by name.
//
// Nothing here is constructed while the agent is disabled: Open returns nil
// then, and every caller treats nil as "no model".
package llm

import (
	"errors"
	"fmt"
	"strings"

	"github.com/op/go-logging"
)

var log = logging.MustGetLogger("llm")

// APIError is what a provider answered when it refused a request.
type APIError struct {
	Status  int
	Message string
}

func (self *APIError) Error() string {
	return fmt.Sprintf("llm: the provider answered %d: %s", self.Status, self.Message)
}

// IsContextLengthError says whether a provider refused because the request
// was too long for the model, which is the one error a caller can fix by
// sending less.
func IsContextLengthError(err error) bool {
	var apiError *APIError
	if !errors.As(err, &apiError) {
		return false
	}
	message := strings.ToLower(apiError.Message)
	return strings.Contains(message, "context length") ||
		strings.Contains(message, "context_length") ||
		strings.Contains(message, "too many tokens") ||
		strings.Contains(message, "maximum context") ||
		strings.Contains(message, "prompt is too long") ||
		strings.Contains(message, "exceeds the maximum")
}
