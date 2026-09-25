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
	"github.com/ziyan/teanode/internal/sources"
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
	if format == computer.FormatRecords || format == computer.FormatTyped {
		most = ingestRecordEntries
	}
	// The first page of a records pass is the one that runs the folder's
	// refresh script, and of a typed pass the one that brings a tool's
	// copy up to date and lists; that is the only page allowed to be slow.
	wait := ingestDeviceWait
	if (format == computer.FormatRecords || format == computer.FormatTyped) && after == "" {
		wait = ingestRefreshWait
	}
	// A typed source is sent its type with every page, so the computer
	// runs whatever this server holds and nothing is installed there.
	var sourceType string
	var settings map[string]any
	var secrets map[string]string
	if format == computer.FormatTyped {
		if sourceType, settings, secrets, err = self.typedSourceParts(ctx, run, source); err != nil {
			return "", counts, err
		}
	}

	// One request to a computer at a time, across the sources that read
	// it, and only for as long as the request: the claim used to be held
	// for the whole job, and a job describing a checkout held it through
	// a model call of minutes, so a pass over another source on the same
	// computer moved only in the gaps between those calls.
	if name := source.Specification.Computer; source.Kind == models.SourceComputer && name != "" {
		waited := time.Now()
		for {
			turn := self.claimComputer(name, source.ID)
			if turn.isFree {
				break
			}
			if time.Since(waited) > ingestTurn {
				other := self.sourceName(ctx, run, source.AgentID, turn.other)
				if turn.isReading {
					return "", counts, &waitingForDevice{name: name, readingOther: other}
				}
				return "", counts, &waitingForDevice{name: name, behindOther: other}
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
			// A typed source's type and settings, and the name of the
			// directory in the person's cache it keeps what it knows in.
			SourceType: sourceType,
			Settings:   settings,
			Secrets:    secrets,
			SourceKey:  typedSourceKey(source),
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
	if result.IsUnfinished {
		// Some of what this pass should have read ran out of time and
		// is read next pass: this pass must not delete what it did not
		// see.
		cursor[cursorPassUnfinished] = true
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
		return tx.SetAgentSourceUnknownAuthors(found.ID, found.UnknownAuthors)
	}); err != nil {
		log.Warningf("cannot note whose commits source %q could not place: %s", source.ID, err)
	}
}

// computerWait is one source waiting for a computer: since when, and when
// it last asked.
type computerWait struct {
	since, asked time.Time
}

const (
	// computerWaitAsking is how recently a waiting source must have asked
	// to be let go first. One that has stopped asking -- its job is back
	// in the queue behind others -- is not waited for, or the computer
	// would stand idle for it.
	computerWaitAsking = time.Minute

	// computerWaitForgotten is how long a source may go without asking
	// before its place is forgotten, and it waits from the start again.
	computerWaitForgotten = 30 * time.Minute
)

// computerTurn is the answer to a source asking for a computer.
type computerTurn struct {
	// isFree is whether it has the computer now.
	isFree bool

	// other is the source that has it, or that goes first, and isReading
	// whether that one has it now rather than having waited longer.
	other     string
	isReading bool
}

// claimComputer gives this source the computer, or says which source has
// it or goes first.
//
// The one that has waited longest goes first. Taking turns by who asked
// first after the computer came free let a source partway through a pass,
// which asks again within seconds, have the computer every time, while one
// starting a pass asked every few minutes and waited behind them all
// evening.
func (self *Agent) claimComputer(computer, sourceId string) computerTurn {
	return self.claimComputerAt(computer, sourceId, time.Now())
}

func (self *Agent) claimComputerAt(computer, sourceId string, now time.Time) computerTurn {
	self.readingMutex.Lock()
	defer self.readingMutex.Unlock()
	if self.computersBusy == nil {
		self.computersBusy = map[string]string{}
	}
	if self.computersWanted == nil {
		self.computersWanted = map[string]map[string]computerWait{}
	}
	waiting := self.computersWanted[computer]
	if waiting == nil {
		waiting = map[string]computerWait{}
		self.computersWanted[computer] = waiting
	}
	mine, found := waiting[sourceId]
	if !found || now.Sub(mine.asked) > computerWaitForgotten {
		mine.since = now
	}
	mine.asked = now
	waiting[sourceId] = mine

	if other, busy := self.computersBusy[computer]; busy && other != sourceId {
		return computerTurn{other: other, isReading: true}
	}
	for other, wait := range waiting {
		if other != sourceId && now.Sub(wait.asked) <= computerWaitAsking && wait.since.Before(mine.since) {
			return computerTurn{other: other}
		}
	}
	self.computersBusy[computer] = sourceId
	delete(waiting, sourceId)
	return computerTurn{isFree: true}
}

func (self *Agent) releaseComputer(computer, sourceId string) {
	self.readingMutex.Lock()
	defer self.readingMutex.Unlock()
	if self.computersBusy[computer] == sourceId {
		delete(self.computersBusy, computer)
	}
}

// typedSourceKey names the directory a typed source keeps what it knows in
// on the computer: the source's own identifier, so two sources of one type
// never share it. Empty for any other source.
func typedSourceKey(source *models.AgentKnowledgeSource) string {
	if source.Specification.Format != computer.FormatTyped {
		return ""
	}
	return strings.ToLower(source.ID)
}

// typedSourceParts is a typed source's type, as installed, its settings,
// and the secrets its type declares, opened to be sent with the request.
func (self *Agent) typedSourceParts(ctx context.Context, run *Run, source *models.AgentKnowledgeSource) (string, map[string]any, map[string]string, error) {
	var installed *models.AgentSourceType
	var stored []*models.AgentSourceSecret
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if installed, err = tx.GetAgentSourceType(source.Specification.Type); err != nil {
			return err
		}
		stored, err = tx.ListAgentSourceSecrets(source.ID)
		return err
	}); err != nil {
		return "", nil, nil, err
	}
	if installed == nil {
		return "", nil, nil, fmt.Errorf("the source type %q is not installed on this server", source.Specification.Type)
	}
	settings := map[string]any{}
	if len(source.Specification.Settings) > 0 {
		if err := json.Unmarshal(source.Specification.Settings, &settings); err != nil {
			return "", nil, nil, fmt.Errorf("the settings of this source are not readable: %w", err)
		}
	}
	// Checked again against the type as it is now: a type replaced or
	// updated since the source was saved may ask for other settings, and
	// its commands must not run with ones it never checked.
	parsed, err := sources.Parse([]byte(installed.Content))
	if err != nil {
		return "", nil, nil, fmt.Errorf("the installed %s cannot be read: %w", installed.Name, err)
	}
	if _, err := parsed.CheckSettings(settings); err != nil {
		return "", nil, nil, fmt.Errorf("the settings of this source no longer suit %s as installed: %w", installed.Name, err)
	}
	// Only what the type declares, and every one it cannot do without.
	filled := map[string]string{}
	for _, secret := range stored {
		opened, err := self.OpenSecret(secret.Value)
		if err != nil {
			return "", nil, nil, fmt.Errorf("the stored value of %s cannot be read: %w", secret.Key, err)
		}
		filled[secret.Key] = opened
	}
	secrets := map[string]string{}
	var missing []string
	for _, secret := range parsed.Secrets {
		if value := filled[secret.Key]; strings.TrimSpace(value) != "" {
			secrets[secret.Key] = value
		} else if !secret.Optional {
			missing = append(missing, secret.Key)
		}
	}
	if len(missing) > 0 {
		return "", nil, nil, fmt.Errorf("%s needs %s filled in for this source; set it on the source in the dashboard, or with `teanode agent knowledge secret set %q %s`",
			parsed.Name, strings.Join(missing, " and "), source.Name, missing[0])
	}
	return installed.Content, settings, secrets, nil
}

// sourceName is a source's name, for saying which one a computer is busy
// with; its identifier where it cannot be looked up.
func (self *Agent) sourceName(ctx context.Context, run *Run, agentId, sourceId string) string {
	var found *models.AgentKnowledgeSource
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		found, err = tx.GetAgentSource(agentId, sourceId)
		return err
	}); err != nil || found == nil {
		return "another source"
	}
	return found.Name
}
