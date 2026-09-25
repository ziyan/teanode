package memory

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// Looking at a picture again.
//
// Recall carries text into a turn, so a fact read out of a screenshot
// arrives as the sentence the night wrote about it. That answers "what did
// that error say" only where the night happened to write the error down,
// and there is no going back for the rest of it: the description is all the
// conversation ever sees.
//
// The bytes are still in the store, and a turn can already carry a picture
// -- it is how the night read the screenshot in the first place. `look`
// fetches them again and puts them in front of the model, so the agent
// answers from the picture rather than from a description of it. It costs
// what the night's own look costs, and it is the person asking, which is
// the moment to spend it.
//
// It is a read. Nothing moves and nothing is written, so a run held to
// reading -- which is every unattended run of the graph -- may use it.

// picturesLooked is how many pictures one look answers with.
//
// A fact usually cites one. The cap is for the fact that cites a thread's
// worth, so that a single call cannot drag several megabytes each out of
// the store; the turn itself bounds what reaches the model again.
const picturesLooked = 4

// lookAction shows the model the picture behind a fact, or one named file.
func lookAction(ctx context.Context, run tools.Run, arguments *memoryArguments) (*tools.Result, error) {
	documents, subject, err := lookedAt(ctx, run, arguments)
	if err != nil {
		return nil, err
	}
	if len(documents) == 0 {
		return tools.TextResult("%s", subject+" has no file kept behind it, so there is nothing to look at"), nil
	}
	agentId := run.Agent().ID
	lines := make([]string, 0, len(documents))
	var images []llm.ContentPart
	for _, document := range documents {
		// The agent's own, rechecked after the query the way the route
		// that serves these bytes to a browser rechecks it.
		if document == nil || document.AgentID != agentId {
			continue
		}
		line, picture := lookAtFile(ctx, run.Storage(), document)
		lines = append(lines, line)
		if picture == nil {
			continue
		}
		images = append(images, *picture)
		if len(images) >= picturesLooked {
			break
		}
	}
	if len(lines) == 0 {
		return tools.TextResult("%s", subject+" has no file kept behind it, so there is nothing to look at"), nil
	}
	result := tools.TextResult("%s", strings.Join(lines, "\n"))
	result.Images = images
	result.Note = "looked at what " + subject + " came from"
	return result, nil
}

// lookedAt is the files a look is about, and what to call what was asked
// for when there turn out to be none.
func lookedAt(ctx context.Context, run tools.Run, arguments *memoryArguments) ([]*models.AgentDocument, string, error) {
	agentId := run.Agent().ID
	if documentId := strings.TrimSpace(arguments.Document); documentId != "" {
		var document *models.AgentDocument
		if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
			// By the caller's own agent, so that an identifier taken from
			// somewhere else finds nothing rather than somebody else's
			// screenshot.
			document, err = tx.GetAgentDocument(agentId, documentId)
			return err
		}); err != nil {
			return nil, "", err
		}
		if document == nil || document.AgentID != agentId {
			return nil, "", fmt.Errorf("there is no file %q of yours", documentId)
		}
		return []*models.AgentDocument{document}, document.Cite(), nil
	}

	path := ownPath(run, arguments.Path)
	if path == "" {
		return nil, "", fmt.Errorf("look at what? give a page and the fact's number, like people/alice-chen and 3, or document for a file knowledge named")
	}
	if arguments.Number <= 0 {
		return nil, "", fmt.Errorf("which fact of %s? give its number, as in %s#3", path, path)
	}
	reference := path + "#" + strconv.Itoa(arguments.Number)
	var documents []*models.AgentDocument
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		node, err := tx.GetAgentNode(agentId, path)
		if err != nil {
			return err
		}
		if node == nil {
			return fmt.Errorf("there is no page at %s", path)
		}
		fact, err := tx.GetAgentFact(agentId, node.ID, arguments.Number)
		if err != nil {
			return err
		}
		if fact == nil {
			return fmt.Errorf("there is no %s", reference)
		}
		documentIds := citedDocumentIds(fact)
		if len(documentIds) == 0 {
			return nil
		}
		documents, err = tx.GetAgentDocuments(agentId, documentIds)
		return err
	}); err != nil {
		return nil, "", err
	}
	// A fact quotes what it was read in, and most of what it was read in
	// is words: a chat window, a commit, a page. Only the files are worth
	// answering with, and a fact that cites none of them says so once
	// rather than once per line of its evidence.
	kept := make([]*models.AgentDocument, 0, len(documents))
	for _, document := range documents {
		if document.Kind == models.DocumentAttachment {
			kept = append(kept, document)
		}
	}
	return kept, reference, nil
}

// citedDocumentIds is what one fact's evidence names, in the order it
// cites them and without repeats.
//
// A filing run writes the identifier as the prompt showed it, which is in
// brackets, so they are trimmed the same way the dashboard trims them.
func citedDocumentIds(fact *models.AgentFact) []string {
	seen := map[string]bool{}
	documentIds := make([]string, 0, len(fact.Evidence))
	for _, evidence := range fact.Evidence {
		id := strings.Trim(strings.TrimSpace(evidence.ID), "[]")
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		documentIds = append(documentIds, id)
	}
	return documentIds
}

// lookAtFile is one file as the model is shown it: the picture itself, or
// a line saying in plain words why there is no picture to show.
//
// Nothing here is an error. A file whose bytes were never kept, or that is
// not a picture at all, is an ordinary thing to find in somebody's archive;
// refusing the call would tell the model its question was wrong when the
// answer is simply that this one cannot be looked at.
func lookAtFile(ctx context.Context, store storage.Files, document *models.AgentDocument) (string, *llm.ContentPart) {
	name := document.Cite() + whereFrom(document)
	contentType := document.ContentType()
	switch {
	case document.StorageKey == "":
		return name + ": nothing was kept of this file here, so there is nothing to look at", nil
	case contentType == "":
		return name + ": nothing said what kind of file it is, and only a picture can be shown to you", nil
	case !tools.IsImage(contentType):
		return fmt.Sprintf("%s: a file of type %s is not a picture, and only a picture can be shown to you", name, contentType), nil
	case document.Bytes > tools.PictureLargest:
		return fmt.Sprintf("%s: %d bytes, more than the %d a picture may be to be shown to you",
			name, document.Bytes, tools.PictureLargest), nil
	case store == nil:
		return name + ": this server keeps no files, so there is nothing to look at", nil
	}
	content, err := tools.DocumentBytes(ctx, store, document)
	if err != nil {
		return name + ": the file could not be read out of the store", nil
	}
	// The bytes in hand as well as the size on the row. An empty picture
	// sent to a provider is a refused request that ends the turn rather
	// than an answer about a picture, and a row saying one thing while the
	// store holds another is exactly the case nobody would think of.
	switch {
	case len(content) == 0:
		return name + ": the file is empty, so there is nothing to look at", nil
	case len(content) > tools.PictureLargest:
		return fmt.Sprintf("%s: %d bytes, more than the %d a picture may be to be shown to you",
			name, len(content), tools.PictureLargest), nil
	}
	return name + ": the picture follows for you to look at",
		&llm.ContentPart{Type: "image", MediaType: contentType, Data: content}
}

// whereFrom is where a file was posted, in the words a person would use.
func whereFrom(document *models.AgentDocument) string {
	var parts []string
	if channel := document.Channel(); channel != "" {
		parts = append(parts, "in "+channel)
	}
	if thread := document.Thread(); thread != "" {
		parts = append(parts, "thread "+thread)
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, ", ") + ")"
}
