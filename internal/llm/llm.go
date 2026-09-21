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
	"net/http"
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

// IsOutOfCreditError says whether a provider refused because the account
// cannot pay, which no caller can fix and no later request will get past.
//
// It is told apart from an ordinary rate limit on purpose, even though the
// providers answer both with 429. A rate limit clears by itself and the
// next request is worth making; an empty account refuses every request
// there is, so work that marches through a list of items would otherwise
// ask once per item and fail every time.
func IsOutOfCreditError(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	var apiError *APIError
	if errors.As(err, &apiError) {
		// Payment required, where a provider uses it. Most answer 429
		// for this, the same status as a rate limit, which is why the
		// words below have to do the telling apart.
		if apiError.Status == http.StatusPaymentRequired {
			return true
		}
		message = apiError.Message
	}
	message = strings.ToLower(message)
	return strings.Contains(message, "insufficient_quota") ||
		strings.Contains(message, "insufficient quota") ||
		strings.Contains(message, "no credits remaining") ||
		strings.Contains(message, "credit balance is too low") ||
		strings.Contains(message, "exceeded your current quota") ||
		strings.Contains(message, "billing details")
}

// IsContextLengthError says whether a provider refused because the request
// was too long for the model, which is the one error a caller can fix by
// sending less.
func IsContextLengthError(err error) bool {
	if err == nil {
		return false
	}
	// The provider's own message where there is one; the words of the
	// error otherwise, since a turn that failed on it is reported as text
	// by the loop and still has to be told apart from a model being down.
	message := err.Error()
	var apiError *APIError
	if errors.As(err, &apiError) {
		message = apiError.Message
	}
	message = strings.ToLower(message)
	return strings.Contains(message, "context length") ||
		strings.Contains(message, "context_length") ||
		strings.Contains(message, "too many tokens") ||
		strings.Contains(message, "maximum context") ||
		strings.Contains(message, "prompt is too long") ||
		strings.Contains(message, "exceeds the maximum") ||
		// llama.cpp: "request (18438 tokens) exceeds the available
		// context size (16384 tokens)".
		strings.Contains(message, "exceeds the available context") ||
		strings.Contains(message, "context size")
}
