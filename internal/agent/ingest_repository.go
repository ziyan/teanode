package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/computer"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// fileRepository writes what git said about a checkout as the first facts
// of its page, and as events on the person's own.
//
// No model runs here. A repository's profile is arithmetic: who committed,
// how much, and between which dates. Handing that to a model to be
// rephrased would cost money and lose precision.
func (self *Agent) fileRepository(ctx context.Context, run *Run, source *models.AgentKnowledgeSource, entry computer.ScanEntry) {
	if err := self.checkSourceRead(ctx, source); err != nil {
		return
	}
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
		if err := lockIngestSource(tx, source); err != nil {
			return err
		}
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
		// search; a page that opened with "# Northwind Portal" and six badges
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

// addressList is those addresses in a fixed order, for a request that is
// built again for every page and should not differ between them.
func addressList(addresses map[string]bool) []string {
	listed := make([]string, 0, len(addresses))
	for address := range addresses {
		listed = append(listed, address)
	}
	sort.Strings(listed)
	return listed
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
	// The head this process already asked about, whatever the model said:
	// the page keeps what it has, and the profile still files.
	_, askedBefore := self.describedHeads.LoadOrStore(source.ID+"@"+profile.Head, true)
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
	if askedBefore || readme == "" {
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
	// the pages a link could point at before it answers, and nothing
	// else. Describing a checkout is not the night, and was never given
	// the night's reach.
	thinking, err := self.think(ctx, run, "Described the checkout "+path, prompt, lookupTools,
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
		if text == "" || len(about) >= 5 {
			continue
		}
		about = append(about, cutRunes(text, 400))
	}
	var links []describedLink
	for _, link := range answer.Links {
		link.To = models.NormalizePath(link.To)
		link.Relation = strings.ToLower(strings.TrimSpace(link.Relation))
		if link.To == "" || link.To == path || !models.IsAgentEdgeRelation(models.AgentEdgeRelation(link.Relation)) {
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
