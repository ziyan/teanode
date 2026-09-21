package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/computer"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// readFromComputer asks the device to scan and files what comes back.
//
// The device does the walking, the sniffing, the extracting and the
// refusing; this end does the chunking and the storing. That split is
// deliberate: three of those four can only be done where the files are,
// and the fourth -- refusing a secret -- must be, so that a mistake here
// cannot pull a private key across the socket.
func (self *Agent) readFromComputer(ctx context.Context, run *Run, source *models.AgentKnowledgeSource, cursor map[string]any) (string, db.SourceCounts, error) {
	counts := db.SourceCounts{}
	position, err := readDeviceCursor(cursor, time.Now())
	if err != nil {
		return "", counts, err
	}
	device := self.computerNamed(source.AgentID, source.Specification.Computer)
	if device == nil {
		return "", counts, &waitingForDevice{name: source.Specification.Computer}
	}

	after := position.After
	// The known hashes go to the daemon once a pass, under the pass's
	// name, and every later page names the pass instead of carrying the
	// map: fifty megabytes a page for a big source, which was most of
	// what a page cost. The name changes when a pass starts at the top,
	// and a daemon that no longer holds the map says so and is sent it
	// again.
	knownId := position.KnownID
	if after == "" || knownId == "" {
		knownId = source.ID + "@" + time.Now().Format(time.RFC3339)
	}
	sent := position.KnownSent
	carry := sent != knownId
	var known map[string]string
	if carry {
		if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
			known, err = tx.ListAgentDocumentHashes(source.ID)
			return err
		}); err != nil {
			return "", counts, err
		}
	}
	most := ingestEntries
	format := source.Specification.Format
	if format == computer.FormatRecords {
		most = ingestRecordEntries
	}
	// The first page of a records pass is the one that runs the folder's
	// refresh script, and that is the only page allowed to be slow.
	wait := ingestDeviceWait
	if format == computer.FormatRecords && after == "" {
		wait = ingestRefreshWait
	}

	// One request to a computer at a time, across the sources that read
	// it, and only for as long as the request: the claim used to be held
	// for the whole job, and a job describing a checkout held it through
	// a model call of minutes, so a pass over another source on the same
	// computer moved only in the gaps between those calls.
	if name := source.Specification.Computer; source.Kind == models.SourceComputer && name != "" {
		waited := time.Now()
		for {
			other, free := self.claimComputer(name, source.ID)
			if free {
				break
			}
			if time.Since(waited) > ingestTurn {
				return "", counts, &waitingForDevice{name: name + " (it is reading " + other + ")"}
			}
			select {
			case <-ctx.Done():
				return "", counts, ctx.Err()
			case <-time.After(time.Second):
			}
		}
	}
	// Released as soon as the answer is in, before anything is filed:
	// filing a checkout asks a model, and that is the wait the claim
	// must not cover.
	release := func() {
		if name := source.Specification.Computer; source.Kind == models.SourceComputer && name != "" {
			self.releaseComputer(name, source.ID)
		}
	}
	mostAttachmentBytes := maxAttachmentBytes(run.Configuration(), source)
	// Who the person is, so that the device can tell the checkouts they
	// work in from the ones they cloned. Sent with every page rather than
	// held over there: an address added to their card takes effect on the
	// next page, and a device that keeps nothing about them cannot get it
	// wrong later.
	own := addressList(self.ownAddresses(ctx, run.Owner))
	ask := func(known map[string]string) (json.RawMessage, error) {
		if err := self.checkSourceRead(ctx, source); err != nil {
			return nil, err
		}
		return device.Ask(ctx, "scan", &computer.ScanArguments{
			Root:    source.Specification.Path,
			Format:  source.Specification.Format,
			Include: source.Specification.Include,
			Exclude: source.Specification.Exclude,
			Known:   known,
			KnownID: knownId,
			After:   after,
			Most:    most,
			// The daemon is told the bound rather than knowing it, so
			// that an operator raising it -- or one source that needs
			// more than the rest -- is a setting here and not a release
			// everybody has to install.
			MaxAttachmentBytes: mostAttachmentBytes,
			// And who they are, with what this source says about the
			// checkouts under it that nobody here ever committed to.
			OwnAddresses:      own,
			ReadEveryCheckout: source.Specification.ReadEveryCheckout,
			// And how much of a checkout's history has to be theirs
			// before its files are read, which is the same argument as
			// the one above and belongs in the same place.
			OwnCommitsAtLeast: source.Specification.OwnCommitsAtLeast,
			// How much of the history one pass over this tree carries,
			// told to the daemon for the same reason: a tree of a
			// hundred checkouts and a third of a million commits is
			// paced by what the person set here.
			CommitsPerPass: source.Specification.CommitsPerPass,
		}, wait)
	}
	answer, err := ask(known)
	if err != nil && !carry && strings.Contains(err.Error(), computer.ErrKnownMissing.Error()) {
		// The daemon was restarted mid-pass, or is an older build that
		// does not keep the map: send it, this once.
		if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
			known, err = tx.ListAgentDocumentHashes(source.ID)
			return err
		}); err != nil {
			return "", counts, err
		}
		carry = true
		answer, err = ask(known)
	}
	release()
	if err == nil {
		cursor[cursorKnownID] = knownId
		if carry {
			cursor[cursorKnownSent] = knownId
		}
	}
	if err != nil {
		// Leaving mid-answer is the same as not being there: the daemon
		// reconnects within the second, and a pass put down for its next
		// scheduled hour over that stood still until morning, twice.
		if errors.Is(err, ErrDeviceDetached) {
			return "", counts, &waitingForDevice{name: source.Specification.Computer}
		}
		return "", counts, fmt.Errorf("asking %s to read %s: %w", source.Specification.Computer, source.Specification.Path, err)
	}
	result, err := decodeIngestPage(answer, after)
	if err != nil {
		return "", counts, fmt.Errorf("the computer's answer is not readable: %w", err)
	}
	return self.fileComputerPage(ctx, run, source, result, func(entry computer.ScanEntry) blobFetcher {
		return func(ctx context.Context) ([]byte, error) {
			if err := self.checkSourceRead(ctx, source); err != nil {
				return nil, err
			}
			return blobFrom(device, entry, mostAttachmentBytes)(ctx)
		}
	})
}

// computerNamed is one of a person's attached computers, or the only one
// where the source did not say which.
func (self *Agent) computerNamed(agentId, name string) *attachedComputer {
	computers := self.computersFor(agentId)
	for _, candidate := range computers {
		if candidate.name == name {
			return candidate
		}
	}
	if name == "" && len(computers) == 1 {
		return computers[0]
	}
	return nil
}

// notedUnknownAuthors keeps the commit addresses a source found that are
// not the person's, so that a silence becomes something they can see.
//
// At most a handful: this is a prompt to go and mark a card, not a
// census of everybody who has ever committed to a mirrored upstream.
func (self *Agent) notedUnknownAuthors(ctx context.Context, source *models.AgentKnowledgeSource, addresses []string) {
	if len(addresses) == 0 {
		return
	}
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		if err := lockIngestSource(tx, source); err != nil {
			return err
		}
		found, err := tx.GetAgentSource(source.AgentID, source.ID)
		if err != nil || found == nil {
			return err
		}
		seen := map[string]bool{}
		for _, address := range found.UnknownAuthors {
			seen[address] = true
		}
		changed := false
		for _, address := range addresses {
			if seen[address] || len(found.UnknownAuthors) >= unknownAuthorsKept {
				continue
			}
			found.UnknownAuthors = append(found.UnknownAuthors, address)
			seen[address] = true
			changed = true
		}
		if !changed {
			return nil
		}
		_, err = tx.PutAgentSource(found)
		return err
	}); err != nil {
		log.Warningf("cannot note whose commits source %q could not place: %s", source.ID, err)
	}
}

// claimComputer says this source is reading from the computer now, or
// which source already is.
func (self *Agent) claimComputer(computer, sourceId string) (string, bool) {
	self.readingMutex.Lock()
	defer self.readingMutex.Unlock()
	if self.computersBusy == nil {
		self.computersBusy = map[string]string{}
	}
	if other, busy := self.computersBusy[computer]; busy && other != sourceId {
		return other, false
	}
	self.computersBusy[computer] = sourceId
	return "", true
}

func (self *Agent) releaseComputer(computer, sourceId string) {
	self.readingMutex.Lock()
	defer self.readingMutex.Unlock()
	if self.computersBusy[computer] == sourceId {
		delete(self.computersBusy, computer)
	}
}
