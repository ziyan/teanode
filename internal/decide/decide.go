// Package decide asks a question whose answers are known in advance and
// gets one of them back, without a model writing any prose.
//
// The agent makes a great many decisions that are not writing: is this
// message worth a person's attention, is this file worth opening, which of
// these folders does this page belong under. Asking a language model means
// paying for a prompt, waiting for it to generate an answer a token at a
// time, and then parsing English back into the choice that was wanted --
// and it may answer with something that was not on the list, or with
// nothing at all, which is most of the error handling around every such
// call in this program.
//
// A decision model scores every answer in the schema at once instead. It
// returns the answer, how likely each was, and how sure it is, in one call
// and in a fraction of the time. There is no prose to parse and no way to
// answer off the list, because the list is the schema.
//
// This is optional and off unless an operator configures it. Nothing here
// is required for the agent to work: every caller keeps the path it had,
// and asks this first only when it is switched on.
package decide

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Timeout is how long one decision may take. Generous for something that
// answers in well under a second: it is a network timeout, not a budget,
// and a caller that cannot wait passes a shorter context.
const Timeout = 20 * time.Second

// Client asks decisions of a service that answers them.
type Client struct {
	baseUrl string
	apiKey  string
	model   string
	http    *http.Client
}

// New builds a client. It opens no connection; the first question does.
//
// The model is required by the service, which refuses a request that names
// none. Naming a dated version rather than the latest pins it, which is
// worth doing where an answer is stored and compared with later ones.
func New(baseUrl, apiKey, model string, timeout time.Duration) (*Client, error) {
	baseUrl = strings.TrimRight(strings.TrimSpace(baseUrl), "/")
	if baseUrl == "" {
		return nil, errors.New("decide: no address for the decision service")
	}
	if strings.TrimSpace(apiKey) == "" {
		return nil, errors.New("decide: no key for the decision service")
	}
	if timeout <= 0 {
		timeout = Timeout
	}
	return &Client{
		baseUrl: baseUrl,
		apiKey:  strings.TrimSpace(apiKey),
		model:   strings.TrimSpace(model),
		http:    &http.Client{Timeout: timeout},
	}, nil
}

// Question is one thing asked about the state, and the answers it may have.
//
// The three kinds are the three shapes a decision comes in: yes or no, one
// of several, or a level on a scale. Every kind carries its instructions
// and what each answer means, because the words are what the decision is
// made from -- there is no prompt anywhere else to put them in.
type Question struct {
	// Instructions is the question itself, in a sentence.
	Instructions string

	// Choices are the answers, each with what it means. Giving these
	// makes it a question with several answers; giving exactly the two
	// names "true" and "false" makes it a yes or no.
	//
	// Ordered by the caller's map iteration nowhere: the names are sent
	// sorted, so the same question is the same request every time and a
	// service that caches sees it as one.
	Choices map[string]string

	// Levels are the rungs of a scale, lowest first, each named for what
	// it means. Giving these makes it a question about degree.
	Levels []string
}

// Answer is what came back for one question.
//
// Which field is meaningful follows the question that was asked, and a
// caller that knows what it asked reads the one it wanted.
//
// A yes-or-no answer is the one exception worth knowing about: it comes
// back as Yes alone, with no Confidence and no Probabilities, because for
// two answers they would say nothing that Yes does not. How sure it is, is
// how far Yes sits from the middle. Sure enough to act on, for a yes-or-no,
// is a question about Yes and not about Confidence -- reading Confidence
// there gets zero, which is not the service being unsure.
type Answer struct {
	// Yes is how strongly the answer is yes, between nothing and one, for
	// a question with two answers. Near the middle is the service being
	// unsure; Confidence stays zero for these.
	Yes float64

	// Choice is the answer's name, for a question with several.
	Choice string

	// Level is the rung of the scale, and may sit between two of them
	// where the service is unsure which side of the line the state is on.
	Level float64

	// Probabilities is how likely each answer was, by name. For a scale
	// the names are the rungs' numbers.
	Probabilities map[string]float64

	// Confidence is how sure the service is, between nothing and one, for
	// a choice or a scale. Zero for a yes-or-no, which carries its own
	// certainty in Yes.
	//
	// This is the value that makes a decision model usable for work that
	// matters: a caller can take the answer where it is sure and fall
	// back to asking a language model where it is not, which is neither
	// available nor meaningful from prose.
	Confidence float64
}

// Answers are the answers to one call, by the names the questions were
// asked under.
type Answers map[string]Answer

// Ask puts questions about one state and returns the answers.
//
// All of the questions are about the same state and are answered in the
// one call, which is the whole shape of this: asking six things about a
// message costs what asking one does.
func (self *Client) Ask(ctx context.Context, state string, questions map[string]Question) (Answers, error) {
	if strings.TrimSpace(state) == "" {
		return nil, errors.New("decide: nothing to decide about")
	}
	if len(questions) == 0 {
		return nil, errors.New("decide: nothing asked")
	}

	asked := make(map[string]any, len(questions))
	for name, question := range questions {
		encoded, err := question.encode()
		if err != nil {
			return nil, fmt.Errorf("decide: %q: %w", name, err)
		}
		asked[name] = encoded
	}
	body := map[string]any{"state": state, "questions": asked}
	if self.model != "" {
		body["model"] = self.model
	}

	var answered struct {
		Answers map[string]struct {
			Type          string             `json:"type"`
			Noul          float64            `json:"noul"`
			Choice        string             `json:"choice"`
			Score         float64            `json:"score"`
			Confidence    float64            `json:"confidence"`
			Probabilities map[string]float64 `json:"probabilities"`
		} `json:"answers"`
	}
	if err := self.post(ctx, "/systemone", body, &answered); err != nil {
		return nil, err
	}
	if len(answered.Answers) == 0 {
		return nil, errors.New("decide: the service answered nothing")
	}

	answers := make(Answers, len(answered.Answers))
	for name, one := range answered.Answers {
		answers[name] = Answer{
			Yes:           one.Noul,
			Choice:        one.Choice,
			Level:         one.Score,
			Probabilities: one.Probabilities,
			Confidence:    one.Confidence,
		}
	}
	// Every question asked is answered, or the caller cannot tell a
	// decision from a gap and would read the zero value as a real "no".
	for name := range questions {
		if _, ok := answers[name]; !ok {
			return nil, fmt.Errorf("decide: %q was asked and not answered", name)
		}
	}
	return answers, nil
}

// encode turns a question into what the service expects, and says so when
// the question does not describe anything answerable.
func (self *Question) encode() (map[string]any, error) {
	instructions := strings.TrimSpace(self.Instructions)
	if instructions == "" {
		return nil, errors.New("the question is not written down")
	}
	switch {
	case len(self.Choices) > 0 && len(self.Levels) > 0:
		return nil, errors.New("it is either a choice or a scale, not both")

	case len(self.Choices) > 0:
		if len(self.Choices) < 2 {
			return nil, errors.New("a choice of one is not a choice")
		}
		criteria := make(map[string]string, len(self.Choices))
		for name, meaning := range self.Choices {
			if strings.TrimSpace(name) == "" {
				return nil, errors.New("an answer with no name")
			}
			criteria[name] = strings.TrimSpace(meaning)
		}
		// Two answers named true and false is a yes-or-no question, which
		// the service answers with a single number rather than a winner
		// and a field of also-rans.
		if len(criteria) == 2 {
			if _, hasTrue := criteria["true"]; hasTrue {
				if _, hasFalse := criteria["false"]; hasFalse {
					return map[string]any{
						"type":         "noul",
						"instructions": instructions,
						"criteria":     criteria,
					}, nil
				}
			}
		}
		return map[string]any{
			"type":         "choice",
			"instructions": instructions,
			"criteria":     criteria,
		}, nil

	case len(self.Levels) > 0:
		if len(self.Levels) < 2 {
			return nil, errors.New("a scale needs at least two rungs")
		}
		levels := make([]string, 0, len(self.Levels))
		for _, level := range self.Levels {
			if strings.TrimSpace(level) == "" {
				return nil, errors.New("a rung with no name")
			}
			levels = append(levels, strings.TrimSpace(level))
		}
		return map[string]any{
			"type":         "score",
			"instructions": instructions,
			"criteria":     levels,
		}, nil
	}
	return nil, errors.New("the question has no answers to choose from")
}

// Names are a question's answers in a settled order, for writing a question
// down the same way twice.
func (self *Question) Names() []string {
	names := make([]string, 0, len(self.Choices))
	for name := range self.Choices {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// post sends a body and decodes the answer, turning anything but a 2xx into
// an error carrying what the service said about it.
func (self *Client) post(ctx context.Context, path string, body any, result any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("decide: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, self.baseUrl+path, strings.NewReader(string(encoded)))
	if err != nil {
		return fmt.Errorf("decide: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+self.apiKey)
	request.Header.Set("Content-Type", "application/json")

	response, err := self.http.Do(request)
	if err != nil {
		return fmt.Errorf("decide: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode/100 != 2 {
		// The body is the service's own account of what was wrong with
		// the request, and a validation error names the field. Carrying
		// it is the difference between fixing a question and guessing at
		// it. Bounded, because an error page is not a message.
		said := make([]byte, 2048)
		read, _ := response.Body.Read(said)
		return &Error{Status: response.StatusCode, Said: strings.TrimSpace(string(said[:read]))}
	}
	if err := json.NewDecoder(response.Body).Decode(result); err != nil {
		return fmt.Errorf("decide: the answer was not readable: %w", err)
	}
	return nil
}

// Error is the service refusing, with what it said.
type Error struct {
	Status int
	Said   string
}

func (self *Error) Error() string {
	if self.Said == "" {
		return "decide: the service answered " + strconv.Itoa(self.Status)
	}
	return "decide: the service answered " + strconv.Itoa(self.Status) + ": " + self.Said
}

// Retryable says whether asking again later is worth it: the service being
// busy or rate limiting is, a malformed question or a bad key is not.
func (self *Error) Retryable() bool {
	return self.Status == http.StatusTooManyRequests || self.Status == 529 || self.Status/100 == 5
}
