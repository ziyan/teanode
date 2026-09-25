package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/computer"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// fileComputerPage returns continuation only after every entry was recorded.
// Already committed entries can be replayed by their source and external ID.
func (self *Agent) fileComputerPage(ctx context.Context, run *Run, source *models.AgentKnowledgeSource, result ingestPage, fetchAttachment func(computer.ScanEntry) blobFetcher) (string, db.SourceCounts, error) {
	counts := db.SourceCounts{}
	if result.IsComplete != (result.NextCursor == "") {
		return "", counts, fmt.Errorf("source page completion disagrees with its cursor")
	}
	if err := self.checkSourceRead(ctx, source); err != nil {
		return "", counts, err
	}
	for index := range result.Entries {
		takeTheNullsOut(&result.Entries[index])
		namePerson(&result.Entries[index], run.Owner)
	}
	// What the device left where it was: the checkouts under this source
	// nobody here has ever committed to, kept to their profile. Whole-tree
	// numbers, the same on every page, so they are carried rather than
	// counted up.
	counts.CheckoutsKeptToProfile = result.CheckoutsKeptToProfile
	counts.FilesKeptToProfile = result.FilesKeptToProfile

	// What the source named but this pass did not file: the unchanged,
	// the refused, and the ones nothing could be made of. Their documents
	// need their seen time written by hand, where a document that is
	// filed has it written by the filing.
	named := make([]string, 0, len(result.Entries))
	for _, entry := range result.Entries {
		if ctx.Err() != nil {
			return "", counts, ctx.Err()
		}
		counts.Seen++
		if entry.Refused != "" {
			counts.Refused++
			named = append(named, entry.ExternalID)
			continue
		}
		// A repository's profile before the unchanged check, not after.
		// The profile is what git says about the checkout -- its head,
		// its languages, who committed and when -- and it is recomputed
		// every pass; the hash beside it is the hash of one file. Filing
		// it only when that file had changed meant a checkout whose tree
		// was untouched never had its facts refreshed and never got the
		// link to the person who works on it, which is why a graph of
		// forty projects had no edges at all.
		if entry.Repository != nil {
			self.fileRepository(ctx, run, source, entry)
		}
		// Before the unchanged check and before the empty-text one,
		// both of which an attachment would fall through: it has no text
		// by design, and whether it has changed says nothing about
		// whether its bytes ever reached the store.
		if entry.Kind == computer.KindAttachment {
			filed, err := self.fileAttachment(ctx, run, source, entry,
				fetchAttachment(entry))
			if err != nil {
				return "", counts, fmt.Errorf("filing source entry %q: %w", entry.ExternalID, err)
			}
			if filed {
				counts.Documents++
			} else {
				named = append(named, entry.ExternalID)
			}
			continue
		}
		if entry.Unchanged {
			named = append(named, entry.ExternalID)
			continue
		}
		if strings.TrimSpace(entry.Text) == "" {
			named = append(named, entry.ExternalID)
			continue
		}
		chunks, err := self.fileDocument(ctx, run, source, entry, "")
		if err != nil {
			return "", counts, fmt.Errorf("filing source entry %q: %w", entry.ExternalID, err)
		}
		counts.Documents++
		counts.Chunks += chunks
	}
	// Before the page is called done, and its failure fails the pass: a
	// pass that forgot a page of names and then reached the end of the
	// tree would take that page's documents for gone.
	if len(named) > 0 {
		if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
			if err := lockIngestSource(tx, source); err != nil {
				return err
			}
			return tx.MarkAgentDocumentsSeen(source.ID, named, time.Now())
		}); err != nil {
			return "", counts, fmt.Errorf("recording what %s still has of %s: %w", source.Specification.Computer, source.Specification.Path, err)
		}
	}
	return result.NextCursor, counts, nil
}

// namePerson writes the owner's username where a source marked the
// person's own posts as computer.PersonAuthor, in the text and among the
// participants, so the night, which reads only the chat the person was
// in, reads them. A source that reads the person's own tools (a coding
// agent's sessions) knows which turns are theirs but not what they are
// called here. The hash is
// left as the reader made it, so an unchanged conversation still matches.
func namePerson(entry *computer.ScanEntry, owner *models.User) {
	if owner == nil || owner.Username == "" || entry.Kind != string(models.DocumentChat) {
		return
	}
	marker, username := computer.PersonAuthor, strings.ToLower(owner.Username)
	entry.Text = strings.ReplaceAll(entry.Text, " "+marker+": ", " "+username+": ")
	switch participants := entry.Metadata["participants"].(type) {
	case []any:
		for index, participant := range participants {
			if participant == marker {
				participants[index] = username
			}
		}
	case []string:
		for index, participant := range participants {
			if participant == marker {
				participants[index] = username
			}
		}
	}
}
