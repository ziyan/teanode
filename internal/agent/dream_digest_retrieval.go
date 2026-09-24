package agent

import (
	"context"
	"path"
	"strings"

	"github.com/ziyan/teanode/internal/agent/reading"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

type digestDocument struct {
	DocumentID string
	Heading    string
	Opening    string
}

type digestMaterial struct {
	IndexLines []string
	Documents  []digestDocument
	SourceName string
	SourceRoot string
}

func (self *Agent) retrieveDigestMaterial(ctx context.Context, run *Run, documents []*models.AgentDocument, isCoarse bool) *digestMaterial {
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
			heading += " — by " + author
		}
		if document.HappenedAt != nil {
			heading += " — " + document.HappenedAt.Format("2 Jan 2006")
		}
		material.Documents = append(material.Documents, digestDocument{DocumentID: document.ID, Heading: heading, Opening: self.openingOf(ctx, run, document, isCoarse)})
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

// openingOf is as much of a document as the digest reads: its first
// passage, or its title alone where the night is working coarsely.
func (self *Agent) openingOf(ctx context.Context, run *Run, document *models.AgentDocument, coarse bool) string {
	if coarse {
		return ""
	}
	var chunks []*models.AgentChunk
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		chunks, err = tx.ListAgentChunks(run.Agent.ID, document.ID)
		return err
	}); err != nil || len(chunks) == 0 {
		return ""
	}
	return cutRunes(chunks[0].Text, 1200)
}

// chatNamesOf is what the person may be called in a chat archive: their
// username, and each word of their name, in lower case. A thread whose
// participants include one of these is one they took part in.
func chatNamesOf(owner *models.User) []string {
	return reading.ChatNamesOf(owner)
}
