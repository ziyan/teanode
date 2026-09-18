package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/llm"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/ziyan/teanode/internal/computer"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// Reading a source, and turning what comes back into something the agent
// can search.
//
// A pass is bounded and says whether it wants another. A source with more
// to do is queued again **at once** rather than in ten minutes: a first
// pass over a checkout and a chat archive is a few hundred thousand
// chunks, and at one bounded pass every ten minutes that is two days,
// where run back to back it is a night. What bounds it instead is the
// job's own deadline and the embedding budget.

// The bounds.
const (
	// ingestEntries is how many things one pass asks a device for, and
	// ingestRecordEntries how many units of a records folder. A page is
	// bounded by bytes as well, and a chat unit is a few hundred bytes:
	// 256 of them was a page of 150 kilobytes against a bound of three
	// megabytes, and an archive of four hundred thousand units read at a
	// twentieth of what the socket allowed.
	ingestEntries       = 256
	ingestRecordEntries = 2048

	// ingestPasses is how many pages one job reads before handing the
	// queue back, so that one source cannot hold a slot for ever. The
	// job's own deadline bounds it too.
	ingestPasses = 16

	// ingestTurn is how long a source waits when another is reading the
	// same computer, and ingestRetry how long a pass that failed mid-tree
	// waits before trying the page again.
	ingestTurn  = 30 * time.Second
	ingestRetry = 5 * time.Minute

	// ingestEmbedBatch is how many chunks go in one call to the embedding
	// model. A hundred is what the providers take comfortably.
	ingestEmbedBatch = 100

	// ingestSoon is how long a source waits when it cannot run now --
	// its computer is not attached, the budget is spent.
	//
	// Minutes rather than the hour it was: a laptop that closes for a
	// meeting should not put a first pass an hour behind, and a source
	// that still has more to read stays due anyway, so this is really
	// only the bound for one that has nothing waiting.
	ingestSoon = 15 * time.Second

	// ingestEvery is how often the sweep looks for sources that are due.
	ingestEvery = 15 * time.Second

	// ingestAgain is how long a source with more to read waits before its
	// next slot.
	//
	// Its cron line is how often to *look* for new work, not how fast to
	// get through a backlog. Waiting for tomorrow's slot after every
	// eight pages meant a first pass over a checkout moved about twenty
	// megabytes a day, so a home directory would have taken weeks. The
	// eight-page limit is what keeps one source from holding the queue;
	// this is what keeps it from crawling.
	ingestAgain = 20 * time.Second

	// ingestDeviceWait is how long a scan may take on the device. A first
	// pass over a large repository walks the whole tree.
	ingestDeviceWait = 10 * time.Minute

	// ingestRefreshWait is the same for the first page of a records
	// source, which runs the folder's refresh script before it reads
	// anything: a script asking a wiki or a drive for everything that
	// changed can take half an hour, and the daemon gives it thirty
	// minutes, so waiting ten here would abandon a scan that was working.
	// Only the first page runs the script, so the pages after it wait the
	// ordinary time.
	ingestRefreshWait = 35 * time.Minute

	// ingestLongest is how long one ingest job may run, which has to
	// cover that wait and leave room to file what the scan came back
	// with. The job's deadline is the context the device wait selects on,
	// so a job bounded at ten minutes made the thirty-five above a number
	// nothing could reach.
	//
	// It is the ceiling for a job that is waiting, not what one costs: a
	// pass with pages to read writes down where it got to and comes back
	// in twenty seconds.
	ingestLongest = ingestRefreshWait + 5*time.Minute

	// cursorPassStarted is where a pass writes down when it began, and
	// cursorPassSeen how many things it has been shown since. Both live
	// in the source's cursor because a pass over a large tree is many
	// jobs long and the cursor is the one thing written down after every
	// page; both go when the pass reaches the end of the tree.
	cursorPassStarted = "passStartedAt"
	cursorPassSeen    = "passSeen"

	// cursorPassRefused is how many things this pass refused, kept the
	// same way, so that the count on the source's row is this pass's
	// rather than every pass's added together: the row said thirty-two
	// refused for a source whose reader no longer refused anything.
	cursorPassRefused = "passRefused"

	// unknownAuthorsKept is how many unplaced commit addresses a source
	// remembers. Enough to recognise yourself in the list, not a census
	// of everybody who ever committed to a mirrored upstream.
	unknownAuthorsKept = 8

	// ownCommitsWorthRecording is how much of a repository has to be the
	// person's before it is an event on their own page. Ten keeps the
	// timeline to what they worked on rather than what they once cloned
	// and fixed a typo in.
	ownCommitsWorthRecording = 10
)

// queueIngestion queues the sources whose time has come.
func (self *Agent) queueIngestion(ctx context.Context, now time.Time) {
	if now.Sub(self.lastIngest) < ingestEvery {
		return
	}
	configuration := self.settings.Configuration()
	if !FeatureAllowed(configuration, "knowledge") {
		return
	}
	self.lastIngest = now

	var due []*models.AgentKnowledgeSource
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		due, err = tx.ListDueAgentSources(now, 20)
		return err
	}); err != nil {
		log.Warningf("cannot list the sources that are due: %s", err)
		return
	}
	for _, source := range due {
		// A source that reads a device is claimed only on the instance
		// holding that device's socket. Any instance may claim any job,
		// but only one of them can reach the computer.
		if source.Instance != "" && source.Instance != self.settings.Instance {
			continue
		}
		if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
			_, err := self.Enqueue(tx, models.AgentJobIngest, source.AgentID, "", source.ID)
			return err
		}); err != nil {
			log.Warningf("cannot queue the reading of source %q: %s", source.ID, err)
		}
	}
}

// runIngest is the handler for an ingest job; its subject is the source.
func (self *Agent) runIngest(ctx context.Context, run *Run) error {
	configuration := run.Configuration()
	if !FeatureAllowed(configuration, "knowledge") {
		return nil
	}
	var source *models.AgentKnowledgeSource
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		source, err = tx.GetAgentSource(run.Agent.ID, run.Job.SubjectID)
		return err
	}); err != nil {
		return err
	}
	if source == nil || !source.Enabled {
		return nil
	}

	counts := db.SourceCounts{
		Documents: source.DocumentCount,
		Chunks:    source.ChunkCount,
		Refused:   source.RefusedCount,
	}
	cursor := source.Cursor
	if cursor == nil {
		cursor = map[string]any{}
	}
	more := false
	failure := ""

	for pass := 0; pass < ingestPasses; pass++ {
		if ctx.Err() != nil {
			more = true
			break
		}
		// Written down before the page is asked for, so that the time the
		// sweep at the end compares against is older than anything the
		// pass can possibly have been shown.
		startedPass := markPassStart(source, cursor, time.Now())
		next, passCounts, err := self.readOnePass(ctx, run, source, cursor)
		counts.Documents += passCounts.Documents
		counts.Chunks += passCounts.Chunks
		counts.Refused += passCounts.Refused
		if !startedPass.IsZero() {
			cursor[cursorPassSeen] = countInCursor(cursor, cursorPassSeen) + passCounts.Seen
			cursor[cursorPassRefused] = countInCursor(cursor, cursorPassRefused) + passCounts.Refused
		}
		if err != nil {
			var waiting *waitingForDevice
			if errorsAs(err, &waiting) {
				// The computer is not attached. Not a failure: it is a
				// laptop, and it will be back.
				//
				// What it knew about having more to read is kept rather
				// than cleared. A source with more waiting is due
				// whatever its next run says, so keeping it means the
				// work resumes the moment the computer is there --
				// clearing it meant a first pass stalled until the hour
				// was up, which is what happened here every time the
				// server was restarted.
				// Mid-tree counts as more, whatever the source said last
				// time: a source resumed by hand had More off from the
				// pass before its pause, lost the computer on its first
				// page, and sat until its hour with the cursor halfway.
				midway := partWayThroughTree(cursor)
				when := self.nextRunOf(source, run.Owner)
				if source.More || midway {
					when = time.Now().Add(ingestSoon)
				}
				return self.markSource(ctx, source, cursor, counts, source.More || midway, waiting.Error(), when)
			}
			failure = err.Error()
			break
		}
		if next == "" {
			// The end of the tree. The cursor is cleared so the next run
			// starts at the beginning again: a source that kept its
			// cursor only ever saw what sorted after the last thing it
			// read, so a file added anywhere earlier was never noticed.
			//
			// Starting over is cheap. The server sends the hash of
			// everything it holds and the program leaves out whatever
			// still matches, so a second pass over an unchanged tree
			// carries the names and none of the text.
			delete(cursor, "after")
			delete(cursor, "before")
			delete(cursor, cursorKnownID)
			delete(cursor, cursorKnownSent)
			self.sweepUnseen(ctx, source, cursor, startedPass, &counts)
			// What the source holds now, counted. The running total
			// added every document a pass filed, and a document filed
			// again under the same name was counted twice: a converted
			// archive of four hundred thousand units showed seven
			// hundred thousand.
			if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
				documents, chunks, err := tx.CountAgentSourceDocuments(source.ID)
				if err != nil {
					return err
				}
				counts.Documents, counts.Chunks = documents, chunks
				return nil
			}); err != nil {
				log.Warningf("cannot count what source %q holds: %s", source.ID, err)
			}
			break
		}
		// A files source pages by path and a sent source by date; both
		// call it "where we got to".
		if source.Kind == models.SourceSent {
			cursor["before"] = next
		} else {
			cursor["after"] = next
		}
		// Written down after every page, not only at the end of the run:
		// a run the deadline ends between pages used to lose all of them,
		// and read the same pages again next time.
		if err := self.markSource(ctx, source, cursor, counts, true, "", time.Now().Add(ingestAgain)); err != nil {
			log.Warningf("cannot record where source %q got to: %s", source.ID, err)
		}
		if pass == ingestPasses-1 {
			more = true
		}
	}

	// Embed what is waiting, bounded.
	embedded, left, err := self.embedChunks(ctx, run.Agent, configuration.Agent.Limits.IngestChunksPerRun)
	if err != nil {
		log.Warningf("cannot embed what source %q found: %s", source.ID, err)
	}
	if left > 0 {
		more = true
	}
	if embedded > 0 {
		log.Debugf("embedded %d chunk(s) of source %s, %d left", embedded, source.ID, left)
	}

	nextRun := self.nextRunOf(source, run.Owner)
	if more && failure == "" {
		nextRun = time.Now().Add(ingestAgain)
	}
	// A failure with the cursor mid-tree is tried again in a few
	// minutes, not at the next scheduled hour: the page that failed is
	// named in the error and the person can see it, and most such
	// failures -- a deadline, a computer that blinked -- do not repeat.
	if failure != "" && partWayThroughTree(cursor) {
		nextRun = time.Now().Add(ingestRetry)
		more = true
	}
	// With a context that outlives the deadline: this is the write that
	// says where the run got to, and it is the one write that must not be
	// the deadline's victim.
	return self.markSource(context.WithoutCancel(ctx), source, cursor, counts, more, failure, nextRun)
}

// waitingForDevice is a source whose computer is not attached.
type waitingForDevice struct {
	name string
}

func (self *waitingForDevice) Error() string {
	return "waiting for the computer " + self.name + " to be attached"
}

// errorsAs is errors.As without the import, for the one use here.
func errorsAs(err error, target **waitingForDevice) bool {
	waiting, ok := err.(*waitingForDevice)
	if ok {
		*target = waiting
	}
	return ok
}

// The cursor keys under which a pass remembers the name of its known
// map and whether the daemon has been sent it.
const (
	cursorKnownID   = "knownId"
	cursorKnownSent = "knownSent"
)

// walksAWholeTree says whether a source's pass ends by having seen
// everything the source holds.
//
// Only a computer or an archive does: its pages walk a tree from one end
// to the other, so what it did not name it no longer has. Sent mail
// walks backwards through time and stops when the run is over, and a
// source read that way must never have anything taken from it.
func walksAWholeTree(source *models.AgentKnowledgeSource) bool {
	return source.Kind == models.SourceComputer || source.Kind == models.SourceArchive
}

// markPassStart notes when this pass over the tree began, and answers
// it; the zero time means this pass may not delete anything.
//
// A pass is many jobs long, so the time is kept in the cursor, which is
// written down after every page. Only a pass that starts at the top of
// the tree gets one: one that resumes mid-tree keeps what its first page
// wrote, and one that was already mid-tree when this was built -- or on
// the upgrade that added it -- gets none, and leaves the sweeping to the
// next pass, which will start at the top.
func markPassStart(source *models.AgentKnowledgeSource, cursor map[string]any, now time.Time) time.Time {
	if !walksAWholeTree(source) {
		return time.Time{}
	}
	if said, ok := cursor[cursorPassStarted].(string); ok && said != "" {
		started, err := time.Parse(time.RFC3339, said)
		if err != nil {
			log.Warningf("source %q says its pass began at %q, which is not a time", source.ID, said)
			return time.Time{}
		}
		return started
	}
	if after, _ := cursor["after"].(string); after != "" {
		return time.Time{}
	}
	// To the second, and so a little earlier than the pass really began:
	// what that costs is that a document last seen within the same second
	// survives one more pass, and what it buys is that nothing filed in
	// that second is mistaken for something the pass did not see.
	started := now.Truncate(time.Second)
	cursor[cursorPassStarted] = started.Format(time.RFC3339)
	cursor[cursorPassSeen] = 0
	cursor[cursorPassRefused] = 0
	return started
}

// sweepUnseen removes what the source no longer has, now that a pass has
// walked its tree to the end.
//
// Until this, nothing ever took a document away. A file deleted from a
// checkout, a page deleted from a wiki, a chat export converted to
// records under new names: the row stayed, its passages stayed, and both
// went on being searched and dreamed over. Every entry a pass is shown
// -- filed, unchanged, or refused, because a thing the source holds and
// cannot read is still a thing it holds -- has its seen time written; so
// what is still older than the time this pass began is what the source
// stopped reporting.
func (self *Agent) sweepUnseen(ctx context.Context, source *models.AgentKnowledgeSource, cursor map[string]any, startedPass time.Time, counts *db.SourceCounts) {
	// However this ends, the next pass over this source starts its own.
	defer func() {
		delete(cursor, cursorPassStarted)
		delete(cursor, cursorPassSeen)
		delete(cursor, cursorPassRefused)
	}()
	if startedPass.IsZero() {
		return
	}
	// What this pass refused is what the row says, not every pass added
	// together.
	if _, kept := cursor[cursorPassRefused]; kept {
		counts.Refused = countInCursor(cursor, cursorPassRefused)
	}
	// A pass shown nothing at all is not somebody deleting everything
	// they own. It is a folder nothing mounted, or a checkout moved, and
	// the answer to either is to wait for the next pass rather than to
	// empty the source.
	if seen := countInCursor(cursor, cursorPassSeen); seen <= 0 {
		log.Debugf("source %q reached the end of its tree having been shown nothing; leaving what it holds alone", source.ID)
		return
	}
	removed := 0
	if err := self.settings.Database.TransactionContext(context.WithoutCancel(ctx), func(tx db.Transaction) (err error) {
		removed, err = tx.DeleteAgentDocumentsUnseen(source.ID, startedPass)
		return err
	}); err != nil {
		log.Warningf("cannot remove what source %q no longer holds: %s", source.ID, err)
		return
	}
	if removed == 0 {
		return
	}
	// Off the source's own count, which is what the person is shown, the
	// same way the documents this pass filed went on to it.
	counts.Documents -= removed
	if counts.Documents < 0 {
		counts.Documents = 0
	}
	log.Infof("source %q no longer has %d document(s); removed them with their passages", source.ID, removed)
}

// partWayThroughTree says whether the cursor stopped in the middle of a
// tree: a pass with pages read and the end not reached yet.
//
// Type-asserted rather than compared against "". The cursor comes back
// from the database as JSON, so a key no page ever wrote is nil, and nil
// is not the empty string: every source looked mid-tree, so one whose
// computer had gone away was made due again in fifteen seconds for ever,
// whether or not it had anything to resume.
func partWayThroughTree(cursor map[string]any) bool {
	after, _ := cursor["after"].(string)
	before, _ := cursor["before"].(string)
	return after != "" || before != ""
}

// countInCursor is a number the cursor is keeping. It comes back from
// the database as JSON, so what was written as an int is read as a
// float.
func countInCursor(cursor map[string]any, key string) int {
	switch value := cursor[key].(type) {
	case int:
		return value
	case float64:
		return int(value)
	}
	return 0
}

// readOnePass asks the source for one page and files it.
func (self *Agent) readOnePass(ctx context.Context, run *Run, source *models.AgentKnowledgeSource, cursor map[string]any) (string, db.SourceCounts, error) {
	switch source.Kind {
	case models.SourceComputer, models.SourceArchive:
		return self.readFromComputer(ctx, run, source, cursor)
	case models.SourceSent:
		return self.readSentMail(ctx, run, source, cursor)
	}
	return "", db.SourceCounts{}, fmt.Errorf("reading a %q source is not built yet", source.Kind)
}

// readFromComputer asks the device to scan and files what comes back.
//
// The device does the walking, the sniffing, the extracting and the
// refusing; this end does the chunking and the storing. That split is
// deliberate: three of those four can only be done where the files are,
// and the fourth -- refusing a secret -- must be, so that a mistake here
// cannot pull a private key across the socket.
func (self *Agent) readFromComputer(ctx context.Context, run *Run, source *models.AgentKnowledgeSource, cursor map[string]any) (string, db.SourceCounts, error) {
	counts := db.SourceCounts{}
	device := self.computerNamed(source.AgentID, source.Specification.Computer)
	if device == nil {
		return "", counts, &waitingForDevice{name: source.Specification.Computer}
	}

	after, _ := cursor["after"].(string)
	// The known hashes go to the daemon once a pass, under the pass's
	// name, and every later page names the pass instead of carrying the
	// map: fifty megabytes a page for a big source, which was most of
	// what a page cost. The name changes when a pass starts at the top,
	// and a daemon that no longer holds the map says so and is sent it
	// again.
	knownId, _ := cursor[cursorKnownID].(string)
	if after == "" || knownId == "" {
		knownId = source.ID + "@" + time.Now().Format(time.RFC3339)
	}
	sent, _ := cursor[cursorKnownSent].(string)
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
	ask := func(known map[string]string) (json.RawMessage, error) {
		return device.Ask(ctx, "scan", &computer.ScanArguments{
			Root:    source.Specification.Path,
			Format:  source.Specification.Format,
			Include: source.Specification.Include,
			Exclude: source.Specification.Exclude,
			Known:   known,
			KnownID: knownId,
			After:   after,
			Most:    most,
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
	var result computer.ScanResult
	if err := json.Unmarshal(answer, &result); err != nil {
		return "", counts, fmt.Errorf("the computer's answer is not readable: %w", err)
	}

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
		if entry.Unchanged {
			named = append(named, entry.ExternalID)
			continue
		}
		if strings.TrimSpace(entry.Text) == "" {
			named = append(named, entry.ExternalID)
			continue
		}
		chunks, err := self.fileDocument(ctx, run, source, entry)
		if err != nil {
			log.Warningf("cannot keep %q of source %q: %s", entry.ExternalID, source.ID, err)
			named = append(named, entry.ExternalID)
			continue
		}
		counts.Documents++
		counts.Chunks += chunks
	}
	// Before the page is called done, and its failure fails the pass: a
	// pass that forgot a page of names and then reached the end of the
	// tree would take that page's documents for gone.
	if len(named) > 0 {
		if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
			return tx.MarkAgentDocumentsSeen(source.ID, named, time.Now())
		}); err != nil {
			return "", counts, fmt.Errorf("recording what %s still has of %s: %w", source.Specification.Computer, source.Specification.Path, err)
		}
	}
	return result.Next, counts, nil
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

// fileDocument writes one thing and its chunks.
func (self *Agent) fileDocument(ctx context.Context, run *Run, source *models.AgentKnowledgeSource, entry computer.ScanEntry) (int, error) {
	chunks := chunkText(entry.Text)
	written := 0
	err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		metadata := entry.Metadata
		if metadata == nil {
			metadata = map[string]any{}
		}
		metadata["source"] = source.Name
		document, err := tx.PutAgentDocument(&models.AgentDocument{
			AgentID: source.AgentID, SourceID: source.ID, ExternalID: entry.ExternalID,
			Kind: documentKindOf(entry.Kind), Title: entry.Title, URL: entry.URL,
			HappenedAt: entry.HappenedAt, ModifiedAt: entry.ModifiedAt,
			Hash: entry.Hash, Bytes: entry.Size, Metadata: metadata, Private: entry.Private,
		})
		if err != nil {
			return err
		}
		if err := tx.ReplaceAgentChunks(document, chunks); err != nil {
			return err
		}
		written = len(chunks)
		if len(entry.Symbols) > 0 {
			symbols := make([]*models.AgentSymbol, 0, len(entry.Symbols))
			for _, symbol := range entry.Symbols {
				symbols = append(symbols, &models.AgentSymbol{
					AgentID: source.AgentID, DocumentID: document.ID,
					Symbol: symbol.Symbol, Kind: symbol.Kind, Line: symbol.Line,
				})
			}
			if err := tx.ReplaceAgentSymbols(source.AgentID, document.ID, symbols); err != nil {
				return err
			}
		}
		return nil
	})
	return written, err
}

// documentKindOf maps what the device called a thing to what this end
// calls it.
//
// A records script writes whatever it found -- a mail message, a forum
// post -- and those used to fall through to `file`, which is the right
// answer for a thing with no better name and the wrong one for a message.
// The kinds that have a name of their own keep it; the default stands for
// everything else.
func documentKindOf(kind string) models.AgentDocumentKind {
	switch kind {
	case "commit":
		return models.DocumentCommit
	case "chat":
		return models.DocumentChat
	case "journal":
		return models.DocumentJournal
	case "page":
		return models.DocumentPage
	case "message":
		return models.DocumentMessage
	case "post":
		return models.DocumentPost
	case "file":
		return models.DocumentFile
	}
	return models.DocumentFile
}

// fileRepository writes what git said about a checkout as the first facts
// of its page, and as events on the person's own.
//
// No model runs here. A repository's profile is arithmetic: who committed,
// how much, and between which dates. Handing that to a model to be
// rephrased would cost money and lose precision.
func (self *Agent) fileRepository(ctx context.Context, run *Run, source *models.AgentKnowledgeSource, entry computer.ScanEntry) {
	profile := entry.Repository
	if profile == nil {
		return
	}
	name := entry.Title
	if name == "" {
		name = models.LastSegment(entry.ExternalID)
	}
	path := models.JoinPath(source.RootPath, name)
	if source.RootPath == "" {
		path = models.JoinPath(models.PathProjects, name)
	}

	// Which of the authors are people this person actually worked with.
	// Everyone who has ever committed to a mirrored upstream is not:
	// on the maintainer's own machine that list is three thousand names.
	theirs := self.ownAddresses(ctx, run.Owner)
	var own *computer.ScanAuthor
	ownAddresses := 0
	for index := range profile.Authors {
		if theirs[strings.ToLower(profile.Authors[index].Address)] {
			ownAddresses++
			if own == nil {
				own = &profile.Authors[index]
			} else {
				own.Commits += profile.Authors[index].Commits
			}
		}
	}
	// The device counts distinct addresses; a person with three of them
	// is one person. "23 commits by 3 people, all of them Ziyan's" is
	// what the first version said.
	contributors := profile.Contributors
	if ownAddresses > 1 {
		contributors -= ownAddresses - 1
	}
	// Nothing here is theirs, and somebody wrote it: the busiest few
	// addresses are kept so the person can see who this program thinks
	// they are not. Without this the whole thing is silent -- no link, no
	// timeline, and no way to find out that the reason is one unmarked
	// card.
	if own == nil && len(profile.Authors) > 0 {
		unplaced := make([]string, 0, unknownAuthorsKept)
		for index := range profile.Authors {
			if index >= unknownAuthorsKept {
				break
			}
			if address := strings.ToLower(strings.TrimSpace(profile.Authors[index].Address)); address != "" {
				unplaced = append(unplaced, address)
			}
		}
		self.notedUnknownAuthors(ctx, source, unplaced)
	}

	// What the checkout says it is, in the model's words, from its readme:
	// asked once per head, outside the transaction below.
	opening, about, links := self.describeCheckout(ctx, run, source, entry, path)

	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		// Marked as the source's, so a page's history can say a sentence
		// came from a repository rather than from the person.
		tx.AsActor(models.ActorIngest)
		if err := tx.EnsureAgentRoots(source.AgentID); err != nil {
			return err
		}
		// By character, never by byte. A readme cut at the 1200th byte can
		// land in the middle of a character, and PostgreSQL refuses the
		// whole statement -- "invalid byte sequence for encoding UTF8" --
		// so one em-dash in the wrong place lost everything git had to
		// say about that checkout.
		// The opening is what the README says the thing is, not the
		// README. The whole file is indexed as a document and found by
		// search; a page that opened with "# Mujin Portal" and six badges
		// was a page nobody could read.
		summary := cutRunes(strings.TrimSpace(profile.Description), 600)
		if opening != "" {
			summary = opening
		}
		node, err := tx.PutAgentNode(&models.AgentNode{
			AgentID: source.AgentID, Path: path, Kind: models.NodeProject,
			Name: name, Summary: summary,
		})
		if err != nil {
			return err
		}
		// What a page is for: what this is, what is in it, and where it
		// lives -- enough for the agent to know where to dig, with the
		// tools it already has. Not the history: the profile's numbers
		// say how big and how old, and the checkout itself is where the
		// detail is.
		// Each line has a key, so that a pass that recomputes it can
		// change the words on the same numbered fact rather than strike
		// the old one and add a new one. Six numbers a night was what it
		// did before, and "projects/personal#2243" cited in a conversation
		// last week pointed at nothing by this one.
		type line struct{ key, text string }
		facts := []line{}
		for index, text := range about {
			facts = append(facts, line{fmt.Sprintf("about-%d", index+1), text})
		}
		where := source.Specification.Path
		if relative := strings.TrimSpace(entry.ExternalID); relative != "" && relative != "." {
			where = filepath.ToSlash(filepath.Join(where, relative))
		}
		// The description is a fact as well as the opening. The night
		// rewrites openings from the facts alone, and one written from a
		// readme the facts did not mention was rewritten into "This is
		// Ziyan's Go project, with Ziyan as the sole author" -- the
		// readme's one useful sentence gone, and padding in its place.
		if description := cutRunes(strings.TrimSpace(profile.Description), 300); description != "" {
			facts = append(facts, line{"description", "Its readme says: " + description})
		}
		if where != "" {
			facts = append(facts, line{"checkout", "The checkout is at " + where + " on " + source.Specification.Computer + "."})
		}
		if len(profile.Remotes) > 0 {
			facts = append(facts, line{"remote", "Lives at " + profile.Remotes[0] + "."})
		}
		if profile.Module != "" && profile.Module != name {
			facts = append(facts, line{"module", "Calls itself " + profile.Module + "."})
		}
		if languages := languagesOf(profile.Languages); languages != "" {
			facts = append(facts, line{"languages", "Written in " + languages + "."})
		}
		if len(profile.Directories) > 0 {
			listed := profile.Directories
			more := ""
			if len(listed) > 12 {
				more = fmt.Sprintf(" and %d more", len(listed)-12)
				listed = listed[:12]
			}
			facts = append(facts, line{"directories", "Top-level directories: " + strings.Join(listed, ", ") + more + "."})
		}
		if profile.First != nil && profile.Last != nil {
			facts = append(facts, line{"history", fmt.Sprintf("%d commits by %s, %s.",
				profile.Commits, people(contributors), monthSpan(profile.First, profile.Last))})
		}
		if own != nil && own.Commits > 0 && own.First != nil && own.Last != nil {
			facts = append(facts, line{"own", fmt.Sprintf("%s wrote %d of the commits, %s.",
				personName(run.Owner), own.Commits, monthSpan(own.First, own.Last))})
		}
		// A profile is recomputed every pass. What it says lands on the
		// same numbered facts it said it on last time -- changed where the
		// words changed, left alone where they did not -- and only a line
		// that has no counterpart any more is struck. The key that pairs
		// old with new is kept as the evidence's quote, which is what the
		// evidence of a computed line is: which computation.
		existing, err := tx.ListAgentFacts(source.AgentID, node.ID, true, 100)
		if err != nil {
			return err
		}
		previous := map[string]*models.AgentFact{}
		for _, fact := range existing {
			for _, evidence := range fact.Evidence {
				if evidence.Kind == models.EvidenceRepository && evidence.Quote != "" {
					previous[evidence.Quote] = fact
					break
				}
			}
		}
		kept := map[string]bool{}
		for _, wanted := range facts {
			evidence := []models.Evidence{{Kind: models.EvidenceRepository, ID: profile.Head, Quote: wanted.key}}
			if before := previous[wanted.key]; before != nil {
				delete(previous, wanted.key)
				kept[before.ID] = true
				if before.Text == wanted.text {
					continue
				}
				if _, err := tx.UpdateAgentFact(source.AgentID, before.ID, func(fact *models.AgentFact) error {
					fact.Text = wanted.text
					fact.Evidence = evidence
					return nil
				}); err != nil {
					return err
				}
				continue
			}
			if _, err := tx.AddAgentFact(&models.AgentFact{
				AgentID: source.AgentID, NodeID: node.ID, Kind: models.FactPlain, Text: wanted.text,
				Evidence: evidence,
			}); err != nil {
				return err
			}
		}
		// What the profile no longer says goes, and so does anything an
		// earlier version wrote without a key, and any second line under
		// a key that one line now carries. After the loop above, previous
		// holds exactly the keys nothing wanted this time, and kept the
		// one fact chosen for each key that was.
		//
		// Deleted rather than made dormant, which everything else the
		// agent takes off a page now is. These rows are not learned;
		// they are derived from the checkout's own profile and written
		// again from it on every describe, so keeping the old copies
		// would leave a page carrying one line of the readme once per
		// pass with nothing to say which is current.
		for _, fact := range existing {
			var repository *models.Evidence
			for index := range fact.Evidence {
				if fact.Evidence[index].Kind == models.EvidenceRepository {
					repository = &fact.Evidence[index]
					break
				}
			}
			if repository == nil {
				continue
			}
			_, unwanted := previous[repository.Quote]
			if repository.Quote == "" || unwanted || !kept[fact.ID] {
				if err := tx.DeleteAgentFact(source.AgentID, fact.ID); err != nil {
					return err
				}
			}
		}
		for _, link := range links {
			target, err := tx.GetAgentNode(source.AgentID, link.To)
			if err != nil {
				return err
			}
			if target == nil {
				continue
			}
			if err := tx.PutAgentEdge(&models.AgentEdge{
				AgentID: source.AgentID, FromID: node.ID, ToID: target.ID,
				Relation: models.AgentEdgeRelation(link.Relation), Note: cutRunes(link.Note, 200),
				Evidence: []models.Evidence{{Kind: models.EvidenceRepository, ID: profile.Head, Quote: "readme"}},
			}); err != nil {
				return err
			}
		}
		log.Debugf("filed the checkout %q as %q: %d commits, %d of them theirs",
			entry.ExternalID, path, profile.Commits, ownCommits(own))
		// The person's own span on this project becomes an event on
		// their work page: a career timeline nobody had to type, from
		// dates that are already in git.
		if own != nil && own.Commits >= ownCommitsWorthRecording && own.First != nil && own.Last != nil {
			selfPage, err := tx.GetAgentNode(source.AgentID, models.PathSelf)
			if err != nil || selfPage == nil {
				return err
			}
			workPage, err := tx.GetAgentNode(source.AgentID, models.PathWork)
			if err != nil {
				return err
			}
			if workPage == nil {
				workPage, err = tx.PutAgentNode(&models.AgentNode{
					AgentID: source.AgentID, Path: models.PathWork, Kind: models.NodeTopic,
					Name: "Work history", Summary: "What they have built, project by project, from the dates in git.",
				})
				if err != nil {
					return err
				}
			}
			text := fmt.Sprintf("Worked on %s: %d commits, %s to %s.",
				name, own.Commits,
				own.First.Format("January 2006"), own.Last.Format("January 2006"))
			// One event per project, rewritten rather than repeated.
			// Deleted for the same reason as the profile's facts above:
			// the span is counted again from git on every describe, and
			// a dormant copy of every count this checkout has ever had
			// is a work page nobody can read.
			events, err := tx.ListAgentFacts(source.AgentID, workPage.ID, true, 500)
			if err != nil {
				return err
			}
			for _, fact := range events {
				if fact.Kind == models.FactEvent && strings.HasPrefix(fact.Text, "Worked on "+name+":") {
					if err := tx.DeleteAgentFact(source.AgentID, fact.ID); err != nil {
						return err
					}
				}
			}
			happened := *own.First
			if _, err := tx.AddAgentFact(&models.AgentFact{
				AgentID: source.AgentID, NodeID: workPage.ID, Kind: models.FactEvent,
				Text: text, HappenedAt: &happened,
				Evidence: []models.Evidence{{Kind: models.EvidenceRepository, ID: profile.Head}},
			}); err != nil {
				return err
			}
			// And the link, which costs nothing and is the only kind of
			// edge that can be drawn without asking a model anything: git
			// says who committed and when, so "worked on" is a fact, not
			// a judgement.
			//
			// It matters more than it looks. Until something draws the
			// first edges a graph is a list of pages in a trench coat --
			// there is nothing for a walk to follow and nothing for the
			// nightly run to reweight, so the half of the night that
			// looks for connections has nothing to look at.
			if err := tx.PutAgentEdge(&models.AgentEdge{
				AgentID: source.AgentID, FromID: selfPage.ID, ToID: node.ID,
				Relation: models.EdgeWorksOn, HappenedAt: &happened,
				Note: fmt.Sprintf("%d commits, %s to %s", own.Commits,
					own.First.Format("January 2006"), own.Last.Format("January 2006")),
				Evidence: []models.Evidence{{Kind: models.EvidenceRepository, ID: profile.Head}},
			}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		log.Warningf("cannot keep what git said about %q: %s", path, err)
	}
}

// ownAddresses is every address that is the person: the card that is them
// says so, and their account's own address besides.
func (self *Agent) ownAddresses(ctx context.Context, owner *models.User) map[string]bool {
	addresses := map[string]bool{}
	if owner == nil {
		return addresses
	}
	if owner.Email != "" {
		addresses[strings.ToLower(owner.Email)] = true
	}
	if owner.ContactID == "" {
		return addresses
	}
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		contact, err := self.contactOf(tx, owner)
		if err != nil || contact == nil {
			return err
		}
		for _, address := range contact.Emails {
			addresses[strings.ToLower(strings.TrimSpace(address))] = true
		}
		return nil
	}); err != nil {
		log.Debugf("cannot read the addresses of %q: %s", owner.Username, err)
	}
	return addresses
}

// languageNames is the language a file extension means, for the ones
// worth naming. Anything not here is not a language a page should claim
// a project is "written in": the first version named the three commonest
// extensions in the tree, and told the person their main project was
// written mostly in gitignore.
var languageNames = map[string]string{
	"go": "Go", "py": "Python", "ts": "TypeScript", "tsx": "TypeScript",
	"js": "JavaScript", "jsx": "JavaScript", "rs": "Rust", "java": "Java",
	"kt": "Kotlin", "c": "C", "h": "C", "cc": "C++", "cpp": "C++", "hpp": "C++",
	"cs": "C#", "rb": "Ruby", "php": "PHP", "swift": "Swift", "m": "Objective-C",
	"scala": "Scala", "sh": "shell", "bash": "shell", "sql": "SQL",
	"html": "HTML", "css": "CSS", "scss": "CSS", "vue": "Vue", "dart": "Dart",
	"lua": "Lua", "pl": "Perl", "r": "R", "jl": "Julia", "ex": "Elixir",
	"exs": "Elixir", "erl": "Erlang", "hs": "Haskell", "ml": "OCaml",
	"clj": "Clojure", "zig": "Zig", "tf": "Terraform", "proto": "Protocol Buffers",
	"ipynb": "Jupyter", "cmake": "CMake", "make": "Make",
}

// languagesOf is what a repository is written in, by file count, as the
// two or three languages that account for most of it. Empty for a tree
// with no source files in it, which is the honest answer for one.
func languagesOf(extensions map[string]int) string {
	type counted struct {
		name  string
		files int
	}
	byName := map[string]int{}
	for extension, files := range extensions {
		if name := languageNames[extension]; name != "" {
			byName[name] += files
		}
	}
	all := make([]counted, 0, len(byName))
	total := 0
	for name, files := range byName {
		all = append(all, counted{name, files})
		total += files
	}
	sort.Slice(all, func(left, right int) bool { return all[left].files > all[right].files })
	names := make([]string, 0, 3)
	covered := 0
	for _, entry := range all {
		if len(names) >= 3 || (len(names) > 0 && entry.files*20 < total) {
			break // three at most, and nothing under a twentieth of the tree
		}
		names = append(names, entry.name)
		covered += entry.files
	}
	return strings.Join(names, ", ")
}

// readSentMail files the person's own sent messages, which is what lets
// the agent write the way they do.
//
// Their own words to real people, with the quoting stripped: not a style
// guide somebody wrote about them, but two thousand examples of how they
// actually open, how long they make it and how they sign off.
func (self *Agent) readSentMail(ctx context.Context, run *Run, source *models.AgentKnowledgeSource, cursor map[string]any) (string, db.SourceCounts, error) {
	counts := db.SourceCounts{}
	mailboxId := source.Specification.MailboxID
	if mailboxId == "" {
		return "", counts, fmt.Errorf("which mailbox?")
	}
	before := time.Now()
	if said, ok := cursor["before"].(string); ok && said != "" {
		if parsed, err := time.Parse(time.RFC3339, said); err == nil {
			before = parsed
		}
	}

	var folder *models.MailboxFolder
	var items []*models.MailboxItem
	var known map[string]string
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if folder, err = tx.GetFolderByKind(mailboxId, models.MailboxFolderKindSent); err != nil || folder == nil {
			return err
		}
		if known, err = tx.ListAgentDocumentHashes(source.ID); err != nil {
			return err
		}
		items, err = tx.ListItems(folder.ID, &db.ItemOptions{
			Before: before, Limit: ingestEntries, ByReceived: true,
		})
		return err
	}); err != nil {
		return "", counts, err
	}
	if folder == nil {
		return "", counts, fmt.Errorf("that mailbox has no sent folder")
	}
	if len(items) == 0 {
		return "", counts, nil
	}

	oldest := before
	for _, item := range items {
		if ctx.Err() != nil {
			return "", counts, ctx.Err()
		}
		var mail *models.Mail
		if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
			found, err := tx.GetMails([]string{item.MailID}, nil)
			if err != nil || len(found) == 0 {
				return err
			}
			mail = found[0]
			return nil
		}); err != nil || mail == nil {
			continue
		}
		if mail.ReceivedAt.Before(oldest) {
			oldest = mail.ReceivedAt
		}
		if known[mail.ID] != "" {
			continue
		}
		// Without the quoted reply underneath: what they wrote is the
		// example, and the message they were answering is not.
		message, err := BuildMessageContext(ctx, run.Storage(), mail, sentCharacters, false)
		if err != nil || strings.TrimSpace(message.Text) == "" {
			continue
		}
		happened := mail.ReceivedAt
		entry := computer.ScanEntry{
			ExternalID: mail.ID,
			Kind:       "message",
			Title:      "To " + message.To + ": " + message.Subject,
			HappenedAt: &happened,
			Hash:       mail.ID,
			Text:       message.Text,
			Metadata: map[string]any{
				"to": message.To, "subject": message.Subject, "author": "them",
			},
		}
		written, err := self.fileDocument(ctx, run, source, entry)
		if err != nil {
			continue
		}
		counts.Documents++
		counts.Chunks += written
	}
	// Backwards through time: the newest are the best examples, and a
	// pass that stops halfway has the useful half.
	if oldest.Before(before) {
		return oldest.Format(time.RFC3339), counts, nil
	}
	return "", counts, nil
}

// sentCharacters is how much of one sent message is kept as an example.
// Long enough to show how they structure something; short enough that two
// thousand of them are a reasonable corpus.
const sentCharacters = 4000

// markSource records what a pass did.
func (self *Agent) markSource(ctx context.Context, source *models.AgentKnowledgeSource, cursor map[string]any, counts db.SourceCounts, more bool, failure string, nextRun time.Time) error {
	var next *time.Time
	if !nextRun.IsZero() {
		next = &nextRun
	}
	return self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		return tx.MarkAgentSourceRun(source.ID, cursor, counts, more, failure, next)
	})
}

// nextRunOf is when a source is due again, from its own cron line.
func (self *Agent) nextRunOf(source *models.AgentKnowledgeSource, owner *models.User) time.Time {
	if strings.TrimSpace(source.Cron) == "" {
		return time.Time{}
	}
	next, err := nextCron(source.Cron, time.Now(), Location(owner))
	if err != nil {
		log.Warningf("source %q has a schedule that cannot be read: %s", source.ID, err)
		return time.Time{}
	}
	return next
}

// embedChunks gives vectors to chunks that have none, and says how many
// it wrote and how many are left.
//
// Embedding is measured against its own budget rather than the day's
// token allowance. The first pass over a person's checkout and chat
// archive is on the order of a hundred million tokens at a thousandth of
// the price of a conversation; counting it against the same cap would
// stop the load on its first night and every night after.
func (self *Agent) embedChunks(ctx context.Context, agent *models.Agent, most int) (int, int64, error) {
	_, _, modelName, _, ok := self.embedderFor()
	if !ok {
		return 0, 0, nil
	}
	if most <= 0 {
		most = 2000
	}
	written := 0
	for written < most {
		if ctx.Err() != nil {
			break
		}
		batch := ingestEmbedBatch
		if remaining := most - written; remaining < batch {
			batch = remaining
		}
		var chunks []*models.AgentChunk
		if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
			chunks, err = tx.ListAgentChunksWithoutVector(agent.ID, modelName, batch)
			return err
		}); err != nil {
			return written, 0, err
		}
		if len(chunks) == 0 {
			break
		}
		texts := make([]string, 0, len(chunks))
		for _, chunk := range chunks {
			texts = append(texts, chunk.Text)
		}
		vectors, _, ok := self.embed(ctx, agent.ID, "ingest", texts)
		if !ok {
			break
		}
		writing := make([]db.AgentChunkVector, 0, len(chunks))
		for index, chunk := range chunks {
			if index >= len(vectors) || len(vectors[index]) == 0 {
				continue
			}
			writing = append(writing, db.AgentChunkVector{
				ChunkID: chunk.ID, SourceID: chunk.SourceID, Model: modelName, Vector: vectors[index],
			})
		}
		// A round that wrote nothing is a round that will write nothing
		// next time either: the same passages come back from the same
		// query, and the provider answered -- with empty vectors, which
		// some do for text they refuse -- so there is no error to stop
		// on. Without this the loop turned for as long as the job had,
		// asking the provider for the same batch over and over.
		if len(writing) == 0 {
			log.Warningf("the embedding model answered with nothing for %d passage(s) of agent %s", len(chunks), agent.ID)
			break
		}
		if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
			// In a fixed order, because two ingest runs happen at once and
			// their batches overlap.
			return tx.PutAgentChunkVectors(agent.ID, writing)
		}); err != nil {
			return written, 0, err
		}
		written += len(writing)
	}
	var left int64
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		left, err = tx.CountAgentChunksWithoutVector(agent.ID, modelName)
		return err
	}); err != nil {
		return written, 0, err
	}
	return written, left, nil
}

// chunkText cuts a document into slices small enough to rank.
//
// Cut at a blank line where there is one within reach, otherwise at a
// line ending, otherwise at the bound: a slice that begins mid-sentence
// still matches, and one that begins mid-word does not. Each carries the
// tail of the one before it, so a sentence cut in half is whole somewhere.
func chunkText(text string) []*models.AgentChunk {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	var chunks []*models.AgentChunk
	runes := []rune(text)
	start := 0
	for start < len(runes) {
		end := start + models.ChunkCharacters
		if end >= len(runes) {
			end = len(runes)
		} else {
			end = cutAt(runes, start, end)
		}
		slice := strings.TrimSpace(string(runes[start:end]))
		if slice != "" {
			chunks = append(chunks, &models.AgentChunk{
				Text:      slice,
				Segmented: segmentable(slice),
			})
		}
		if end >= len(runes) {
			break
		}
		next := end - models.ChunkOverlap
		if next <= start {
			next = end
		}
		start = next
	}
	return chunks
}

// cutAt is where to end a slice: the last blank line, else the last line
// ending, else where it was going to end anyway.
func cutAt(runes []rune, start, end int) int {
	window := end - start
	floor := start + window/2
	for index := end - 1; index > floor; index-- {
		if runes[index] == '\n' && index > 0 && runes[index-1] == '\n' {
			return index
		}
	}
	for index := end - 1; index > floor; index-- {
		if runes[index] == '\n' {
			return index
		}
	}
	for index := end - 1; index > floor; index-- {
		if unicode.IsSpace(runes[index]) {
			return index
		}
	}
	return end
}

// segmentable says whether PostgreSQL's full-text search can make words
// of this text.
//
// It cannot segment Chinese or Japanese: there are no spaces to split on,
// and the stock server has no dictionary for them. A chunk that is mostly
// those characters would get one enormous token matching nothing, so it
// is left out of the word index and found by meaning alone -- and marked,
// so that an answer citing it can say which way it was found rather than
// leaving somebody to wonder why a search missed it.
func segmentable(text string) bool {
	unsegmented, total := 0, 0
	for _, character := range text {
		if !unicode.IsLetter(character) {
			continue
		}
		total++
		if unicode.Is(unicode.Han, character) ||
			unicode.Is(unicode.Hiragana, character) ||
			unicode.Is(unicode.Katakana, character) {
			unsegmented++
		}
	}
	if total == 0 {
		return true
	}
	return float64(unsegmented)/float64(total) < 0.3
}

// ownCommits is how many of a checkout's commits are the person's, for a
// log line that has to work when none of them are.
func ownCommits(own *computer.ScanAuthor) int {
	if own == nil {
		return 0
	}
	return own.Commits
}

// people is "1 person" or "3 people".
// monthSpan is when something ran, by month: "July 2026" when it began
// and ended in one, "March 2024 to July 2026" otherwise. A fact reading
// "July 2026 to July 2026" said one thing twice.
func monthSpan(first, last *time.Time) string {
	from, until := first.Format("January 2006"), last.Format("January 2006")
	if from == until {
		return from
	}
	return from + " to " + until
}

func people(count int) string {
	if count == 1 {
		return "1 person"
	}
	return fmt.Sprintf("%d people", count)
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

// describeCheckout is what a checkout is, in the model's words: an
// opening for its page and a few facts, read from its readme. The
// profile git gives says where a thing is and what it is written in; it
// never says what it is for, and a page of forty such profiles read like
// an inventory. One call per checkout, repeated only when its head moves;
// the facts are keyed about-1.. so the pass that files them changes them
// in place.
// A link the readme supports -- the website of a project, a plugin of a
// system -- is filed too, to a page the person already has.
type describedLink struct {
	To       string `json:"to"`
	Relation string `json:"relation"`
	Note     string `json:"note"`
}

func (self *Agent) describeCheckout(ctx context.Context, run *Run, source *models.AgentKnowledgeSource, entry computer.ScanEntry, path string) (string, []string, []describedLink) {
	profile := entry.Repository
	if profile == nil || profile.Head == "" {
		return "", nil, nil
	}
	// Already said, for this head: what the page has stays. Said for an
	// older head, it stays too unless a fresh one is written: a call that
	// failed used to leave the page with nothing, since the pass that
	// files the profile strikes every line it was not asked for.
	var existingOpening string
	var existing, fallback []string
	var readme string
	var index []string
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		node, err := tx.GetAgentNode(source.AgentID, path)
		if err != nil {
			return err
		}
		if node != nil {
			facts, err := tx.ListAgentFacts(source.AgentID, node.ID, true, 100)
			if err != nil {
				return err
			}
			byKey, older := map[string]string{}, map[string]string{}
			for _, fact := range facts {
				for _, evidence := range fact.Evidence {
					if evidence.Kind == models.EvidenceRepository && strings.HasPrefix(evidence.Quote, "about-") {
						if evidence.ID == profile.Head {
							byKey[evidence.Quote] = fact.Text
						} else {
							older[evidence.Quote] = fact.Text
						}
					}
				}
			}
			for index := 1; index <= 5; index++ {
				if text, found := byKey[fmt.Sprintf("about-%d", index)]; found {
					existing = append(existing, text)
				}
				if text, found := older[fmt.Sprintf("about-%d", index)]; found {
					fallback = append(fallback, text)
				}
			}
			existingOpening = node.Summary
			if len(existing) > 0 {
				return nil
			}
		}
		if lines, err := memoryLines(tx, source.AgentID, models.AudienceAsk, 10, false); err == nil {
			index = lines
		}
		prefix := strings.Trim(entry.ExternalID, "./")
		for _, name := range []string{"README.md", "README", "readme.md", "README.rst", "README.txt", "Readme.md"} {
			id := name
			if prefix != "" {
				id = prefix + "/" + name
			}
			document, err := tx.GetAgentDocumentByExternal(source.ID, id)
			if err != nil {
				return err
			}
			if document == nil {
				continue
			}
			chunks, err := tx.ListAgentChunks(source.AgentID, document.ID)
			if err != nil {
				return err
			}
			var text strings.Builder
			for _, chunk := range chunks {
				text.WriteString(chunk.Text)
				text.WriteByte('\n')
			}
			readme = cutRunes(strings.TrimSpace(text.String()), 8000)
			break
		}
		return nil
	}); err != nil {
		log.Debugf("cannot read what %q says about itself: %s", path, err)
		return existingOpening, fallback, nil
	}
	if len(existing) > 0 {
		return existingOpening, existing, nil
	}
	if readme == "" {
		return existingOpening, fallback, nil
	}

	history := ""
	if profile.First != nil && profile.Last != nil {
		history = fmt.Sprintf("%d commits, %s.", profile.Commits, monthSpan(profile.First, profile.Last))
	}
	prompt, err := render("describe_project.txt", map[string]any{
		"PersonName":  personName(run.Owner),
		"Path":        path,
		"Name":        models.LastSegment(path),
		"Languages":   languagesOf(profile.Languages),
		"Directories": strings.Join(profile.Directories, ", "),
		"History":     history,
		"Readme":      readme,
		"Index":       index,
	})
	if err != nil {
		return existingOpening, fallback, nil
	}
	// A run of the loop, like every call: it may look the graph up for
	// the pages a link could point at before it answers.
	thinking, err := self.think(ctx, run, "Described the checkout "+path, prompt, dreamTools,
		roundsFor(run.Configuration(), models.AgentJobIngest), models.AgentJobIngest, config.AgentWorkScan)
	if err != nil {
		log.Debugf("cannot ask what %q is: %s", path, err)
		return existingOpening, fallback, nil
	}
	extracted, err := llm.ExtractJSON(thinking.Text)
	if err != nil {
		return existingOpening, fallback, nil
	}
	var answer struct {
		Opening string          `json:"opening"`
		Facts   []string        `json:"facts"`
		Links   []describedLink `json:"links"`
	}
	if err := json.Unmarshal([]byte(extracted), &answer); err != nil {
		return existingOpening, fallback, nil
	}
	var about []string
	for _, text := range answer.Facts {
		text = strings.TrimSpace(text)
		if text == "" || isPromptExample(text) || len(about) >= 5 {
			continue
		}
		about = append(about, cutRunes(text, 400))
	}
	var links []describedLink
	for _, link := range answer.Links {
		link.To = models.NormalizePath(link.To)
		link.Relation = strings.ToLower(strings.TrimSpace(link.Relation))
		if link.To == "" || link.To == path || !models.IsAgentEdgeRelation(models.AgentEdgeRelation(link.Relation)) || isPromptExample(link.Note) {
			continue
		}
		links = append(links, link)
	}
	opening := cutRunes(strings.TrimSpace(answer.Opening), 600)
	// An opening and no facts is an answer, not a failure: a checkout
	// whose readme says what it is in one line has nothing else to file.
	// Kept as the one about- line all the same, because those lines are
	// what says this head has been described -- without it the same
	// checkout was described again on every pass, a model call per
	// checkout per night for as long as its head did not move.
	//
	// An answer with nothing in it at all leaves no mark and is asked
	// again, which is what should happen: nothing was learned.
	if len(about) == 0 && opening != "" {
		about = []string{opening}
	}
	return opening, about, links
}
