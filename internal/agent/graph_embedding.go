package agent

import (
	"context"
	"strings"

	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// embedderFor is the embedding model as this deployment has it, the width
// to ask for, and whether there is one at all.
//
// The name carries the width where one was asked for, because two widths
// of one model are two spaces: a vector written at 512 must never be
// ranked against one the same model wrote at 1536.
func (self *Agent) embedderFor() (embedder llm.Embedder, model, modelName string, dimensions int, ok bool) {
	if !self.settings.Registry.HasEmbedding() {
		return nil, "", "", 0, false
	}
	selection, err := self.settings.Registry.Embedding()
	if err != nil {
		log.Warningf("no embedding model for the graph: %s", err)
		return nil, "", "", 0, false
	}
	return selection.Embedder, selection.Model, selection.Name, selection.Dimensions, true
}

// meaning is what a piece of text means: the vector, and the model that
// read it, which is part of the key every vector is stored under.
//
// It exists so that the embedding can be worked out before the
// transaction that needs it. Embedding is an HTTP call to another
// service; made with a transaction open it holds a database connection --
// and whatever rows that transaction has locked -- for as long as the
// provider takes to answer, which on a bad minute is the whole request
// timeout, once per fact, on a run that files fifteen of them.
type meaning struct {
	ModelName string
	Vector    []float32
}

// meaningOf is one embedding call for one piece of text. Nil where there
// is no embedding model, or the call failed, or there was nothing to
// embed: every caller reads that as "compare by name alone", which is
// what a deployment without an embedder has always done.
func (self *Agent) meaningOf(ctx context.Context, agentId, kind, text string) *meaning {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	vectors, modelName, ok := self.embed(ctx, agentId, kind, []string{text})
	if !ok {
		return nil
	}
	return &meaning{ModelName: modelName, Vector: vectors[0]}
}

// embed is one call to the embedding model, at the configured width.
func (self *Agent) embed(ctx context.Context, agentId, kind string, texts []string) ([][]float32, string, bool) {
	embedder, model, modelName, dimensions, ok := self.embedderFor()
	if !ok || len(texts) == 0 {
		return nil, "", false
	}
	configuration := self.settings.Configuration()
	callContext, cancel := context.WithTimeout(ctx, configuration.Agent.Limits.RequestTimeout.Duration())
	defer cancel()
	vectors, usage, err := embedder.Embed(callContext, llm.EmbedRequest{
		Model: model, Inputs: texts, Dimensions: dimensions,
	})
	RecordUsage(self.settings.Database, agentId, "", modelName, kind, usage)
	if err != nil {
		log.Warningf("cannot embed: %s", err)
		return nil, modelName, false
	}
	return vectors, modelName, len(vectors) > 0
}

// meaningOfQuestion is some words as a vector, worked out at most once a
// turn.
//
// The two halves of recall ask the graph and the documents the same
// question, and a turn embedded it once for each: two calls to another
// service, before the model had been asked anything, for one question.
// The answer is kept by the words it came from, so a tool searching for
// the same thing later in the turn is free as well. The kind the call is
// booked under is whoever asked first, which is what the usage rows say.
func (self *AskRun) meaningOfQuestion(ctx context.Context, kind, words string) *meaning {
	text := cutRunes(strings.TrimSpace(words), graphEmbedCharacters)
	if text == "" {
		return nil
	}
	self.meaningsMutex.Lock()
	remembered, asked := self.meanings[text]
	self.meaningsMutex.Unlock()
	if asked {
		// Including a nil: a deployment with no embedder, or a call that
		// failed, is not worth asking again this turn.
		return remembered
	}
	found := self.agent.meaningOf(ctx, self.settings.Agent.ID, kind, text)
	self.meaningsMutex.Lock()
	defer self.meaningsMutex.Unlock()
	if self.meanings == nil {
		self.meanings = make(map[string]*meaning, turnMeaningsKept)
	}
	if len(self.meanings) < turnMeaningsKept {
		self.meanings[text] = found
	}
	return found
}

// factText is what is embedded of a fact: the sentence and the page it is
// on, so that "moved to the fleet team" carries whose move it was.
func factText(fact *models.AgentFact, path, name string) string {
	text := strings.TrimSpace(fact.Text)
	if name != "" {
		text = name + ": " + text
	} else if path != "" {
		text = path + ": " + text
	}
	return cutRunes(text, graphEmbedCharacters)
}

// nodeText is what is embedded of a page.
func nodeText(node *models.AgentNode) string {
	text := strings.TrimSpace(node.Name + "\n" + node.Summary)
	if len(node.Aliases) > 0 {
		text += "\n" + strings.Join(node.Aliases, " ")
	}
	if strings.TrimSpace(text) == "" {
		text = node.Path
	}
	return cutRunes(text, graphEmbedCharacters)
}
