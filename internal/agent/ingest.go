package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	// ingestEntries is how many things one pass asks a device for.
	ingestEntries = 256

	// ingestPasses is how many pages one job reads before handing the
	// queue back, so that one source cannot hold a slot for ever.
	ingestPasses = 8

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
	ingestSoon = 5 * time.Minute

	// ingestEvery is how often the sweep looks for sources that are due.
	ingestEvery = time.Minute

	// ingestAgain is how long a source with more to read waits before its
	// next slot.
	//
	// Its cron line is how often to *look* for new work, not how fast to
	// get through a backlog. Waiting for tomorrow's slot after every
	// eight pages meant a first pass over a checkout moved about twenty
	// megabytes a day, so a home directory would have taken weeks. The
	// eight-page limit is what keeps one source from holding the queue;
	// this is what keeps it from crawling.
	ingestAgain = 2 * time.Minute

	// ingestDeviceWait is how long a scan may take on the device. A first
	// pass over a large repository walks the whole tree.
	ingestDeviceWait = 10 * time.Minute

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

	if computer := source.Specification.Computer; source.Kind == models.SourceComputer && computer != "" {
		if other, free := self.claimComputer(computer, source.ID); !free {
			return &Deferral{Until: time.Now().Add(ingestAgain), Reason: fmt.Sprintf("%s is reading %s first", computer, other)}
		}
		defer self.releaseComputer(computer, source.ID)
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
		next, passCounts, err := self.readOnePass(ctx, run, source, cursor)
		counts.Documents += passCounts.Documents
		counts.Chunks += passCounts.Chunks
		counts.Refused += passCounts.Refused
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
				midway := cursor["after"] != "" || cursor["before"] != ""
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
			break
		}
		// A files source pages by path and a sent source by date; both
		// call it "where we got to".
		if source.Kind == models.SourceSent {
			cursor["before"] = next
		} else {
			cursor["after"] = next
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
	return self.markSource(ctx, source, cursor, counts, more, failure, nextRun)
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

	var known map[string]string
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		known, err = tx.ListAgentDocumentHashes(source.ID)
		return err
	}); err != nil {
		return "", counts, err
	}
	after, _ := cursor["after"].(string)

	answer, err := device.Ask(ctx, "scan", &computer.ScanArguments{
		Root:    source.Specification.Path,
		Format:  source.Specification.Format,
		Include: source.Specification.Include,
		Exclude: source.Specification.Exclude,
		Allowed: source.Allowed,
		Known:   known,
		After:   after,
		Most:    ingestEntries,
	}, ingestDeviceWait)
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

	// Whatever it flagged as being about other people is remembered so
	// the person can let it in; nothing under one is read until they do.
	if len(result.Sensitive) > 0 {
		self.notedSensitive(ctx, source, result.Sensitive)
	}

	for _, entry := range result.Entries {
		if ctx.Err() != nil {
			return "", counts, ctx.Err()
		}
		if entry.Refused != "" {
			counts.Refused++
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
			continue
		}
		if strings.TrimSpace(entry.Text) == "" {
			continue
		}
		chunks, err := self.fileDocument(ctx, run, source, entry)
		if err != nil {
			log.Warningf("cannot keep %q of source %q: %s", entry.ExternalID, source.ID, err)
			continue
		}
		counts.Documents++
		counts.Chunks += chunks
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

// notedSensitive remembers the directories the scan passed over.
func (self *Agent) notedSensitive(ctx context.Context, source *models.AgentKnowledgeSource, names []string) {
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		found, err := tx.GetAgentSource(source.AgentID, source.ID)
		if err != nil || found == nil {
			return err
		}
		seen := map[string]bool{}
		for _, name := range found.Sensitive {
			seen[name] = true
		}
		changed := false
		for _, name := range names {
			if !seen[name] {
				found.Sensitive = append(found.Sensitive, name)
				seen[name] = true
				changed = true
			}
		}
		if !changed {
			return nil
		}
		_, err = tx.PutAgentSource(found)
		return err
	}); err != nil {
		log.Warningf("cannot note what source %q passed over: %s", source.ID, err)
	}
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
		log.Debugf("filed the checkout %q as %q: %d commits, %d of them theirs",
			entry.ExternalID, path, profile.Commits, ownCommits(own))
		// The person's own span on this project becomes an event on
		// their own page: a career timeline nobody had to type, from
		// dates that are already in git.
		if own != nil && own.Commits >= ownCommitsWorthRecording && own.First != nil && own.Last != nil {
			selfPage, err := tx.GetAgentNode(source.AgentID, models.PathSelf)
			if err != nil || selfPage == nil {
				return err
			}
			text := fmt.Sprintf("Worked on %s: %d commits, %s to %s.",
				name, own.Commits,
				own.First.Format("January 2006"), own.Last.Format("January 2006"))
			// One event per project, rewritten rather than repeated.
			events, err := tx.ListAgentFacts(source.AgentID, selfPage.ID, true, 200)
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
				AgentID: source.AgentID, NodeID: selfPage.ID, Kind: models.FactEvent,
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
