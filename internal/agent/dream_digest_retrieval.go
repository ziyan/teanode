package agent

import (
	"context"
	"fmt"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/ziyan/teanode/internal/agent/reading"
	"github.com/ziyan/teanode/internal/computer"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

type digestDocument struct {
	DocumentID string
	Heading    string
	Text       string
}

// digestPart is one stretch of a document too long for one reading call,
// read in a call of its own: the text of that stretch, and where it
// falls.
type digestPart struct {
	Text   string
	Number int
	Count  int
}

type digestMaterial struct {
	IndexLines []string
	Documents  []digestDocument
	SourceName string
	SourceRoot string
}

func (self *Agent) retrieveDigestMaterial(ctx context.Context, run *Run, documents []*models.AgentDocument, isCoarse bool) *digestMaterial {
	return self.retrieveDigestParts(ctx, run, documents, nil, isCoarse)
}

// retrieveDigestParts is retrieveDigestMaterial with the text of some
// documents given: a part of a long one, read on its own.
func (self *Agent) retrieveDigestParts(ctx context.Context, run *Run, documents []*models.AgentDocument, parts map[string]*digestPart, isCoarse bool) *digestMaterial {
	material := &digestMaterial{}
	if err := run.Database().TransactionContext(ctx, func(transaction db.Transaction) error {
		lines, err := memoryLines(transaction, run.Agent.ID, models.AudienceAsk, 10, false)
		material.IndexLines = lines
		return err
	}); err != nil {
		log.Debugf("cannot read the index for a dream: %s", err)
	}
	var source *models.AgentKnowledgeSource
	var checkouts []string
	if len(documents) > 0 {
		_ = run.Database().TransactionContext(ctx, func(transaction db.Transaction) error {
			found, err := transaction.GetAgentSource(run.Agent.ID, documents[0].SourceID)
			if err != nil || found == nil {
				return err
			}
			source = found
			material.SourceName, material.SourceRoot = found.Name, strings.Trim(found.RootPath, "/")
			checkouts, err = transaction.ListAgentSourceCheckouts(found.ID)
			if err != nil {
				log.Debugf("cannot list the checkouts of %q: %s", found.Name, err)
			}
			return nil
		})
	}
	for _, document := range documents {
		heading := document.Cite()
		// Where a file is, and whose it is. A file was shown by its name
		// alone -- "VMPCMac.java" -- and with nothing to say which of the
		// dozens of checkouts under a folder it came from, the reading
		// filed files from all of them on the page of the best-known
		// project there, and then the nightly split divided that page
		// into themes that had nothing to do with it.
		if document.Kind == models.DocumentFile && document.ExternalID != "" && document.ExternalID != heading {
			heading += " — at " + document.ExternalID
		}
		if checkout := checkoutOf(document, checkouts); checkout != "" && source != nil {
			heading += " — in the checkout " + checkout + ", whose page is " + checkoutPage(source, checkout)
		}
		if author := document.Author(); author != "" {
			// A record source writes the person as @you, which the
			// reading would take for somebody of that name: what they
			// wrote, and asked to have remembered, read as a stranger's.
			if author == computer.PersonAuthor {
				author = personName(run.Owner)
			}
			heading += " — by " + author
		}
		if document.HappenedAt != nil {
			heading += " — " + document.HappenedAt.Format("2 Jan 2006")
		}
		text := ""
		if part := parts[document.ID]; part != nil {
			heading += fmt.Sprintf(" — part %d of %d", part.Number, part.Count)
			text = part.Text
		} else {
			text = self.textOf(ctx, run, document, isCoarse)
		}
		material.Documents = append(material.Documents, digestDocument{DocumentID: document.ID, Heading: heading, Text: text})
	}
	return material
}

// checkoutOf is the checkout a document belongs to: the one a commit
// names, or the deepest checkout whose directory holds the file.
func checkoutOf(document *models.AgentDocument, checkouts []string) string {
	if named := document.Checkout(); named != "" {
		return named
	}
	deepest := ""
	for _, checkout := range checkouts {
		if strings.HasPrefix(document.ExternalID, checkout+"/") && len(checkout) > len(deepest) {
			deepest = checkout
		}
	}
	return deepest
}

// checkoutPage is the page a checkout's project is filed on, which is
// where fileRepository puts it: under the source's root, by the name of
// the checkout's own directory.
func checkoutPage(source *models.AgentKnowledgeSource, checkout string) string {
	root := strings.Trim(source.RootPath, "/")
	if root == "" {
		root = models.PathProjects
	}
	return models.JoinPath(root, path.Base(checkout))
}

// textOf is what the reading is shown of a document: all of it, from its
// passages in order, or as much as digestDocumentRunes allows where the
// operator set one; its title alone where the reading is coarse. A
// document longer than one call holds is read in parts instead (see
// digestPartsOf), and never cut here.
func (self *Agent) textOf(ctx context.Context, run *Run, document *models.AgentDocument, coarse bool) string {
	if coarse {
		return ""
	}
	var text strings.Builder
	for _, passage := range readPassagesOf(self.chunksOf(ctx, run, document)) {
		if text.Len() > 0 {
			text.WriteString("\n")
		}
		text.WriteString(passage)
	}
	if limit := digestDocumentRunes(run.Configuration()); limit > 0 {
		return cutRunes(text.String(), limit)
	}
	return text.String()
}

func (self *Agent) chunksOf(ctx context.Context, run *Run, document *models.AgentDocument) []*models.AgentChunk {
	var chunks []*models.AgentChunk
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		chunks, err = tx.ListAgentChunks(run.Agent.ID, document.ID)
		return err
	}); err != nil {
		log.Debugf("cannot read the passages of %q: %s", document.ID, err)
		return nil
	}
	return chunks
}

// digestPartsOf is a long document cut into stretches that each fit one
// reading call, at passage boundaries, in order. A passage longer than a
// stretch on its own is cut, and nothing else is.
func (self *Agent) digestPartsOf(ctx context.Context, run *Run, document *models.AgentDocument) []*digestPart {
	var parts []*digestPart
	var stretch strings.Builder
	flush := func() {
		if stretch.Len() > 0 {
			parts = append(parts, &digestPart{Text: stretch.String()})
			stretch.Reset()
		}
	}
	for _, text := range readPassagesOf(self.chunksOf(ctx, run, document)) {
		for utf8.RuneCountInString(text) > digestBatchRunes {
			flush()
			runes := []rune(text)
			parts = append(parts, &digestPart{Text: string(runes[:digestBatchRunes])})
			text = string(runes[digestBatchRunes:])
		}
		if utf8.RuneCountInString(stretch.String())+utf8.RuneCountInString(text) > digestBatchRunes {
			flush()
		}
		if stretch.Len() > 0 {
			stretch.WriteString("\n")
		}
		stretch.WriteString(text)
	}
	flush()
	for index, part := range parts {
		part.Number, part.Count = index+1, len(parts)
	}
	return parts
}

// readPassagesOf is a document's passages as the document says them once:
// each passage after the first begins with the end of the one before it,
// so a search finds a sentence cut by the boundary, and read in order the
// overlap is dropped.
func readPassagesOf(chunks []*models.AgentChunk) []string {
	passages := make([]string, 0, len(chunks))
	previous := ""
	for _, chunk := range chunks {
		passages = append(passages, withoutOverlap(previous, chunk.Text))
		previous = chunk.Text
	}
	return passages
}

// withoutOverlap is a passage without the words it repeats from the end
// of the one before it.
func withoutOverlap(previous, current string) string {
	runes := []rune(current)
	for length := min(len(runes), models.ChunkOverlap+50); length >= 20; length-- {
		if strings.HasSuffix(previous, string(runes[:length])) {
			return strings.TrimSpace(string(runes[length:]))
		}
	}
	return current
}

// digestBatchRunes is how much text one reading call is given, across its
// documents, so that a call fits a small model's window: a batch ends
// before it would pass this, and a document longer than this is read in
// parts of this size.
const digestBatchRunes = 40000

// digestDocumentRunes is the operator's bound on how much of each
// document is read, or zero for all of it.
func digestDocumentRunes(configuration *config.Configuration) int {
	if configuration != nil && configuration.Agent.Limits.DigestDocumentRunes > 0 {
		return configuration.Agent.Limits.DigestDocumentRunes
	}
	return 0
}

// chatNamesOf is what the person may be called in a chat archive: their
// username, and each word of their name, in lower case. A thread whose
// participants include one of these is one they took part in.
func chatNamesOf(owner *models.User) []string {
	return reading.ChatNamesOf(owner)
}
