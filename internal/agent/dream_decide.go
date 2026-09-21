package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/ziyan/teanode/internal/decide"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// decidersAtOnce is how many decisions are in flight together.
//
// One call answers in well under a second, and a batch is tens of files,
// so asking them one after another would spend most of a minute waiting on
// the network. Eight is enough to make the batch take about as long as its
// slowest call and few enough not to look like an attack on the service.
const decidersAtOnce = 8

// worthOpeningFloor is how strongly a file must read as worth opening
// before the night spends a picture call on it.
//
// Above the middle rather than at it. The decision costs money either way
// -- opening one sends the picture to a model to be described -- and there
// are far more furniture photographs than screenshots of errors, so the
// cheaper mistake is to pass over a picture that would have been useful.
// One passed over is offered again by a later night; one opened is paid for
// now.
const worthOpeningFloor = 0.6

// deciderFor is the decision model, and whether there is one at all.
//
// Beside embedderFor, and for the same reason: it is a capability some
// providers have and most do not, so it is asked for by name. Empty
// configuration is the whole of "off" and every caller has a path that asks
// a language model instead.
func (self *Agent) deciderFor() (llm.Decider, bool) {
	configuration := self.settings.Configuration()
	if self.settings.Registry == nil || configuration.Agent.Models.Decide == "" {
		return nil, false
	}
	found, _, err := self.settings.Registry.Deciding()
	if err != nil {
		log.Warningf("no decision model: %s", err)
		return nil, false
	}
	return found, true
}

// decideAttachments asks a decision model which of a batch are worth
// opening, and says whether it answered for all of them.
//
// The third value is whether this path ran at all. False means there is no
// decision model, or it could not answer, and the caller falls back to
// asking a language model: a service that is down should cost a slower
// night, never a wrong decision about what to keep. That matters more here
// than it looks, because a declined file is passed over by every night
// afterwards.
func (self *Agent) decideAttachments(ctx context.Context, documents []*models.AgentDocument) (map[string]string, bool) {
	decider, ok := self.deciderFor()
	if !ok || len(documents) == 0 {
		return nil, false
	}
	asking := make([]decideItem, 0, len(documents))
	for _, document := range documents {
		asking = append(asking, decideItem{id: document.ID, state: attachmentLine(document)})
	}
	return decideWorthOpening(ctx, decider, asking)
}

// decideItem is one thing to judge: what to call it back, and the line the
// judgement is made from.
type decideItem struct {
	id    string
	state string
}

// decideWorthOpening is the judgement itself, over things already
// described. Separate from the fetching above so that what it does with
// each kind of answer can be run against a decider that answers to order.
func decideWorthOpening(ctx context.Context, decider llm.Decider, items []decideItem) (map[string]string, bool) {
	if len(items) == 0 {
		return nil, false
	}

	// One call per file: each is a different thing being judged, and the
	// service answers about one state at a time. They go together rather
	// than one after another.
	type verdict struct {
		id     string
		reason string
		open   bool
		err    error
	}
	verdicts := make([]verdict, len(items))
	gate := make(chan struct{}, decidersAtOnce)
	var waiting sync.WaitGroup
	for index, item := range items {
		waiting.Add(1)
		go func(index int, item decideItem) {
			defer waiting.Done()
			gate <- struct{}{}
			defer func() { <-gate }()
			if ctx.Err() != nil {
				verdicts[index] = verdict{id: item.id, err: ctx.Err()}
				return
			}
			answers, err := decider.Decide(ctx, item.state, map[string]decide.Question{
				"worth_opening": {
					Instructions: "Is this file worth sending to a model to be described, at a cost?",
					Choices: map[string]string{
						"true": "It likely holds words or detail that exist nowhere else in writing: " +
							"an error, a log, a terminal, a stack trace, a diagram, a chart, a slide, " +
							"a scanned or photographed document, a screenshot of something going wrong",
						"false": "It is decoration or a record of a moment: a photograph of people, " +
							"a place or a meal, a logo, an avatar, a background, a sticker, " +
							"a screenshot of something ordinary working as it should",
					},
				},
			})
			if err != nil {
				verdicts[index] = verdict{id: item.id, err: err}
				return
			}
			answer := answers["worth_opening"]
			verdicts[index] = verdict{
				id:     item.id,
				open:   answer.Yes >= worthOpeningFloor,
				reason: reasonFor(answer.Yes),
			}
		}(index, item)
	}
	waiting.Wait()

	// All or none. A batch where some answered and some did not would
	// decline whatever failed, since the caller declines everything not
	// in the map, and a network error is not a judgement that a file is
	// furniture. One failure sends the whole batch to the model.
	chosen := map[string]string{}
	for _, each := range verdicts {
		if each.err != nil {
			log.Warningf("the decision model could not say whether a file is worth opening, so a model will be asked: %s", each.err)
			return nil, false
		}
		if each.open {
			chosen[each.id] = each.reason
		}
	}
	return chosen, true
}

// reasonFor is what the row says about a file the decision model chose.
// How sure it was, because the row shows a reason either way and "a model
// said so" is not one a person can weigh.
func reasonFor(yes float64) string {
	return fmt.Sprintf("the decision model judged it worth opening, %.0f%% sure", yes*100)
}
