package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

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
		// Carried from the row so that a job that never got a page --
		// the computer is not attached -- writes back what the source
		// already said rather than zero.
		CheckoutsKeptToProfile: source.CheckoutsKeptToProfile,
		FilesKeptToProfile:     source.FilesKeptToProfile,
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
		if err == nil {
			// Not added up: what the page says is what the whole tree
			// holds, counted afresh by the walk behind it.
			counts.CheckoutsKeptToProfile = passCounts.CheckoutsKeptToProfile
			counts.FilesKeptToProfile = passCounts.FilesKeptToProfile
		}
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
