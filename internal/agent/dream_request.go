package agent

import (
	"context"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// lookupTools is the pair a call reaches when all it needs is to look
// something up: the graph, to find a page before filing to it, and the
// sources, to read a document whole when its first passage is not enough.
// Nothing here changes anything, which is why a call given this set can
// be read-only as well.
//
// This is what describing a checkout gets. The night itself gets
// everyTool; the two are named apart so that a call site says which it
// is asking for.
//
// It is the same pair twice over: what a lookup is given, and what the
// night -- which is given everything -- may only look with, as
// AskSettings.ReadOnlyTools. Naming it once is what keeps a tool added to
// the graph from arriving in one place and not the other.
var lookupTools = map[string]bool{"memory": true, "knowledge": true}

// everyTool is what a call of the night reaches: all of them, including
// the person's own computer where they have attached one.
//
// Nil is how the loop is told not to filter by name (see AskSettings.Allow),
// so this is a nil map rather than a list that would have to be kept in
// step with the catalog.
//
// Unattended calls still enforce confirmation: a call that requires the
// person's approval is refused when nobody is present to give it.
var everyTool map[string]bool

// dreamFrame is what every call of a dream is told first: it has the
// person's tools and may use them, and the one thing it does not do by
// hand is change the graph -- not because it lacks the permission, but
// because a change made through the object the call ends with is filed by
// code together with the evidence it came from, which is what keeps a
// fact attached to its source and a move a proposal the person can refuse.
//
// Said as a fact about the run rather than as a request, because it is
// one: the two tools are held to reading by AskSettings.ReadOnlyTools, so
// a call that would change something comes back refused whatever the
// frame says. Telling the model what will happen saves it the round it
// would spend finding out, and the memory tool's own description, which
// invites it to note what it learns, is right there arguing the other way.
const dreamFrame = "You are working through your own memory with nobody present. You have the person's own tools here, their computer among them where they have attached one: read a file, run something over it, look at what came back, the same as you would in a conversation with them. Nobody is there to be asked, so a call that would need their word comes back refused; say in your answer what you would have done rather than looking for another way to do it. The memory and knowledge tools are for looking in this run: `index`, `get`, `search`, `history`, `read`, `sources` and `shape` go through, and anything that would change something -- `note`, `page`, `link`, `unlink`, `move`, `merge`, `forget`, or adding or syncing a source -- comes back refused, inside a `batch` call as well as on its own. That is not a permission you are missing. Every change you want is said in the object you end with, and code files it with the evidence it came from, so that a fact keeps its source and a move stays a proposal the person can refuse; said any other way it is refused and nothing is filed. Use `get` and `search` freely to look a page up first, and when several need looking up, look them all up in one `batch` call of up to eight items rather than one call at a time. Then answer with the object."

// dreamThink is one call of a dream as a run of the loop, titled by what
// it is doing, on the scan model, with what it cost taken off the
// dream's budget. What comes back is the last thing the run said; the
// caller reads the object out of it as it read the response before.
func (self *Agent) dreamThink(ctx context.Context, run *Run, budget *dreamBudget, title, prompt string, lookups bool) (string, error) {
	thinking, err := self.dreamThought(ctx, run, budget, title, prompt, lookups)
	if err != nil {
		return "", err
	}
	return thinking.Text, nil
}

// dreamThought is dreamThink with the run itself handed back, for a
// caller that wants to say afterwards what the run turned out to be.
//
// lookups says whether the call may look pages up: the reading and the
// filing of orphans, which decide where things go, may; a month written
// from its record, an opening rewritten from its facts, a page divided,
// a walk judged and a question rehearsed have all they need in the prompt,
// and a model given tools for those spent ten rounds looking instead of
// answering.
func (self *Agent) dreamThought(ctx context.Context, run *Run, budget *dreamBudget, title, prompt string, lookups bool) (*thought, error) {
	return self.dreamThoughtAbout(ctx, run, budget, title, prompt, nil, lookups)
}

// dreamThoughtAbout is dreamThought with pictures in the turn, for the
// one phase that has something to look at. The allowance is claimed and
// settled here exactly as it is for a call in words, which is what makes
// a night that has spent its share stop looking at pictures too.
func (self *Agent) dreamThoughtAbout(ctx context.Context, run *Run, budget *dreamBudget, title, prompt string, pictures []llm.ContentPart, lookups bool) (*thought, error) {
	// A call with nothing to look up answers from its prompt, so it keeps
	// the read-only turn it always had; there is nothing for a tool to do
	// in it and no reason to offer one.
	tools, rounds, think := noTools, 1, self.thinkAbout
	if lookups {
		// Said before the prompt, because the memory tool's own
		// description invites the model to note what it learns, and a
		// dream that tried to note ran out of rounds refused and never
		// answered.
		tools, rounds, think = everyTool, roundsFor(run.Configuration(), models.AgentJobDream), self.thinkAboutFreely
		prompt = dreamFrame + "\n\n" + prompt
	}
	// Claimed before the call and settled after. A check that stands on
	// its own is a promise made to every batch in flight at once: three
	// of them asked whether there was room before any had spent
	// anything, and all three were told yes.
	if !budget.reserve() {
		return nil, errNothingLeftToSpend
	}
	thinking, err := think(ctx, run, title, prompt, pictures, tools, rounds, models.AgentJobDream, config.AgentWorkScan)
	usage := llm.Usage{}
	if thinking != nil {
		usage = thinking.Usage
	}
	budget.settle(usage)
	// An account that cannot pay refuses the next call too, and every
	// stage of the night walks a list: without this the night asked once
	// per page, per document and per person, and failed every time.
	if llm.IsOutOfCreditError(err) {
		budget.refuse()
	}
	return thinking, err
}
