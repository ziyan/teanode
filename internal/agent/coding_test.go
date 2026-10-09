package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// codingWorld is the recall world with a checkout on a computer and the
// coding sessions held in it.
type codingWorld struct {
	*recallWorld
	project *models.AgentNode
	source  *models.AgentKnowledgeSource
}

// newCodingWorld makes a checkout at ~/code/seedling on the computer
// "workbench", whose project page is projects/seedling, with the line
// saying where it is moved onto a page under it, as the night does.
func newCodingWorld(t *testing.T) *codingWorld {
	t.Helper()
	world := &codingWorld{recallWorld: newRecallWorld(t)}
	world.project = world.page(t, "projects/seedling", "seedling", "Seedling turns seed catalogues into planting calendars.",
		"The tests need a database container running.",
		"Releases are cut from main only after the changelog is written.")
	operations := world.page(t, "projects/seedling/operations", "operations", "How seedling is built and run.")
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		if err := putKeyedRepositoryFact(tx, world.agent.ID, operations.ID, checkoutFactKey, checkoutLine("~/code/seedling", "workbench"), "0123456"); err != nil {
			t.Fatalf("putKeyedRepositoryFact: %s", err)
		}
		var err error
		world.source, err = tx.PutAgentSource(&models.AgentKnowledgeSource{
			AgentID: world.agent.ID, Kind: models.SourceComputer, Name: "claude-code", Enabled: true,
			Specification: models.AgentKnowledgeSpecification{Type: "claude-code", Computer: "workbench", Format: models.FormatTyped},
		})
		if err != nil {
			t.Fatalf("PutAgentSource: %s", err)
		}
	})
	return world
}

// session files a unit of a coding session held in a directory.
func (self *codingWorld) session(t *testing.T, sessionId, title, directory string, at time.Time, text string) {
	t.Helper()
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		document, err := tx.PutAgentDocument(&models.AgentDocument{
			AgentID: self.agent.ID, SourceID: self.source.ID, ExternalID: "projects/-code-seedling/" + sessionId + ".jsonl#" + at.Format("150405"),
			Kind: models.DocumentChat, Title: title, HappenedAt: &at, Hash: sessionId + at.String(),
			Metadata: map[string]any{"directory": directory, "assistant": "Claude Code", "channel": title, "posts": 2},
		})
		if err != nil {
			t.Fatalf("PutAgentDocument: %s", err)
		}
		if err := tx.ReplaceAgentChunks(document, chunkText(text)); err != nil {
			t.Fatalf("ReplaceAgentChunks: %s", err)
		}
	})
}

// A session that starts in a checkout, even in a folder under it, is
// shown the checkout's project page (found from the page the line saying
// where the checkout is was moved to), the page's facts other than what
// the profile computed, and how the session before it there ended -- not
// how the session itself began, when it is the one being resumed.
func TestASessionStartsWithItsCheckoutAndTheLastSessionThere(t *testing.T) {
	world := newCodingWorld(t)
	directory := "/srv/alice/code/seedling"
	world.session(t, "older", "Add retries", directory, time.Now().Add(-3*time.Hour),
		"09:00 alice: please add a retry to the catalogue fetcher\n09:04 Claude Code: Added a retry with backoff to fetch.go; the tests pass.")
	world.session(t, "current", "Today", directory, time.Now().Add(-time.Minute),
		"11:00 alice: what is left from yesterday?")

	shown, err := world.run.agent.CodingSessionStart(context.Background(), world.agent, world.run.settings.Owner, &CodingRequest{
		Directory: directory + "/cmd", ComputerName: "workbench", HomeDirectory: "/srv/alice", SessionID: "current",
	})
	if err != nil {
		t.Fatalf("CodingSessionStart: %s", err)
	}
	if shown.ProjectPath != "projects/seedling" || shown.CheckoutDirectory != directory {
		t.Fatalf("the checkout is %q at %q, want projects/seedling at %s", shown.ProjectPath, shown.CheckoutDirectory, directory)
	}
	for _, want := range []string{"<teanode-memory>", "planting calendars", "projects/seedling#1 The tests need a database container running.",
		`"Add retries"`, "please add a retry to the catalogue fetcher", "Its last answer: Added a retry with backoff"} {
		if !strings.Contains(shown.Text, want) {
			t.Fatalf("the session is shown %q:\n%s", want, shown.Text)
		}
	}
	for _, unwanted := range []string{"The checkout is at", "what is left from yesterday"} {
		if strings.Contains(shown.Text, unwanted) {
			t.Fatalf("the session is not shown %q:\n%s", unwanted, shown.Text)
		}
	}
	if len(shown.ShownPaths) == 0 || shown.ShownPaths[0] != "projects/seedling" {
		t.Fatalf("the page shown is passed back, got %v", shown.ShownPaths)
	}
}

// A checkout filed on two project pages -- the profile's own and an older
// one the night grew around it -- shows both, and a prompt recalls from
// either.
func TestACheckoutFiledOnTwoPagesShowsBoth(t *testing.T) {
	world := newCodingWorld(t)
	older := world.page(t, "projects/seedling-legacy", "seedling", "The first plan for seedling, before the rewrite.",
		"The rewrite dropped the plugin loader.")
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		if err := putKeyedRepositoryFact(tx, world.agent.ID, older.ID, checkoutFactKey, checkoutLine("~/code/seedling", "workbench"), "0123456"); err != nil {
			t.Fatalf("putKeyedRepositoryFact: %s", err)
		}
	})
	request := &CodingRequest{Directory: "/srv/alice/code/seedling", ComputerName: "workbench", HomeDirectory: "/srv/alice"}
	shown, err := world.run.agent.CodingSessionStart(context.Background(), world.agent, world.run.settings.Owner, request)
	if err != nil {
		t.Fatalf("CodingSessionStart: %s", err)
	}
	for _, want := range []string{"projects/seedling:", "projects/seedling-legacy:", "The rewrite dropped the plugin loader."} {
		if !strings.Contains(shown.Text, want) {
			t.Fatalf("the session is shown %q:\n%s", want, shown.Text)
		}
	}
	request.Prompt = "what did the rewrite drop from the loader?"
	recalled, err := world.run.agent.CodingPromptRecall(context.Background(), world.agent, world.run.settings.Owner, request)
	if err != nil {
		t.Fatalf("CodingPromptRecall: %s", err)
	}
	if !strings.Contains(recalled.Text, "plugin loader") {
		t.Fatalf("a prompt recalls from the second page too:\n%s", recalled.Text)
	}
}

// A checkout another computer never profiled is the project only when it
// is the same repository: the same path there proves nothing. Found by its
// remote, the session is told memory read the project elsewhere; on the
// computer that profiled it, at a newer commit, it is told memory may be
// out of date.
func TestACheckoutElsewhereIsKnownByItsRemote(t *testing.T) {
	world := newCodingWorld(t)
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		if err := putKeyedRepositoryFact(tx, world.agent.ID, world.project.ID, "remote", "Lives at ssh://git@git.example.com:22/garden/seedling.git.", "0123456"); err != nil {
			t.Fatalf("putKeyedRepositoryFact: %s", err)
		}
	})
	start := func(request *CodingRequest) *CodingContext {
		shown, err := world.run.agent.CodingSessionStart(context.Background(), world.agent, world.run.settings.Owner, request)
		if err != nil {
			t.Fatalf("CodingSessionStart: %s", err)
		}
		return shown
	}

	samePath := start(&CodingRequest{Directory: "/srv/alice/code/seedling", ComputerName: "laptop", HomeDirectory: "/srv/alice",
		RemoteURLs: []string{"git@git.example.com:garden/another.git"}})
	if samePath.ProjectPath != "" {
		t.Fatalf("the same path on another computer, holding another repository, is not the project: %q", samePath.ProjectPath)
	}

	elsewhere := start(&CodingRequest{Directory: "/work/checkouts/seedling-copy/cmd", ComputerName: "laptop", HomeDirectory: "/srv/alice",
		RemoteURLs: []string{"https://git.example.com/garden/seedling"}, Head: "89abcdef", CheckoutRoot: "/work/checkouts/seedling-copy"})
	if elsewhere.ProjectPath != "projects/seedling" || elsewhere.CheckoutDirectory != "/work/checkouts/seedling-copy" {
		t.Fatalf("the same repository anywhere is the project: %q at %q", elsewhere.ProjectPath, elsewhere.CheckoutDirectory)
	}
	if !strings.Contains(elsewhere.Text, "read from its checkout at ~/code/seedling on workbench (commit 0123456), not this one") {
		t.Fatalf("the session is told where memory read it:\n%s", elsewhere.Text)
	}

	behind := start(&CodingRequest{Directory: "/srv/alice/code/seedling", ComputerName: "workbench", HomeDirectory: "/srv/alice", Head: "fedcba98"})
	if !strings.Contains(behind.Text, "last read this checkout at commit 0123456; it is now at fedcba98") {
		t.Fatalf("a checkout at another commit is told memory may be out of date:\n%s", behind.Text)
	}
	same := start(&CodingRequest{Directory: "/srv/alice/code/seedling", ComputerName: "workbench", HomeDirectory: "/srv/alice", Head: "0123456"})
	if same.ReadFrom != "" {
		t.Fatalf("at the commit memory read, nothing is said: %q", same.ReadFrom)
	}
}

func TestRemotesAreComparedAsRepositories(t *testing.T) {
	for remote, want := range map[string]string{
		"ssh://git@git.example.com:22/garden/seedling.git": "git.example.com/garden/seedling",
		"git@git.example.com:garden/seedling.git":          "git.example.com/garden/seedling",
		"https://Git.Example.com/garden/seedling/":         "git.example.com/garden/seedling",
		"git.example.com:garden/seedling":                  "git.example.com/garden/seedling",
		"/srv/mirrors/seedling.git":                        "", // a path names nothing on another computer
		"":                                                 "",
	} {
		if got := normalizeRemote(remote); got != want {
			t.Fatalf("normalizeRemote(%q) = %q, want %q", remote, got, want)
		}
	}
}

// A directory no checkout holds is shown nothing, rather than the whole
// of memory.
func TestASessionOutsideAnyCheckoutIsShownNothing(t *testing.T) {
	world := newCodingWorld(t)
	shown, err := world.run.agent.CodingSessionStart(context.Background(), world.agent, world.run.settings.Owner, &CodingRequest{
		Directory: "/srv/alice/code/seedlings-elsewhere", ComputerName: "workbench", HomeDirectory: "/srv/alice",
	})
	if err != nil {
		t.Fatalf("CodingSessionStart: %s", err)
	}
	if shown.Text != "" || shown.ProjectPath != "" {
		t.Fatalf("a folder whose name merely starts like a checkout's is not in it: %q\n%s", shown.ProjectPath, shown.Text)
	}
}

// A prompt recalls from the checkout's project and nowhere else, skips
// the pages the session was shown a moment ago, and a word of assent
// recalls nothing.
func TestAPromptRecallsOnlyItsProject(t *testing.T) {
	world := newCodingWorld(t)
	world.page(t, "projects/greenhouse", "greenhouse", "Another project.", "The greenhouse tests need a database container too.")
	// A colleague linked to the project is the person's business, not the
	// coding tool's, even where the prompt's words hit their page.
	colleague := world.page(t, "people/bob-example", "Bob Example", "A colleague.", "Bob keeps the database container images.")
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		colleague.Kind = models.NodePerson
		if _, err := tx.PutAgentNode(colleague); err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		if err := tx.PutAgentEdge(&models.AgentEdge{AgentID: world.agent.ID, FromID: colleague.ID, ToID: world.project.ID, Relation: models.EdgeWorksOn, Status: models.EdgeStated}); err != nil {
			t.Fatalf("PutAgentEdge: %s", err)
		}
	})
	request := &CodingRequest{Directory: "/srv/alice/code/seedling", ComputerName: "workbench", HomeDirectory: "/srv/alice",
		Prompt: "which database container do the tests need?"}

	shown, err := world.run.agent.CodingPromptRecall(context.Background(), world.agent, world.run.settings.Owner, request)
	if err != nil {
		t.Fatalf("CodingPromptRecall: %s", err)
	}
	if !strings.Contains(shown.Text, "The tests need a database container running.") {
		t.Fatalf("the project's fact is recalled:\n%s", shown.Text)
	}
	if strings.Contains(shown.Text, "greenhouse") || strings.Contains(shown.Text, "Bob") {
		t.Fatalf("another project, and a colleague linked to this one, are not:\n%s", shown.Text)
	}

	request.ShownPaths = shown.ShownPaths
	again, err := world.run.agent.CodingPromptRecall(context.Background(), world.agent, world.run.settings.Owner, request)
	if err != nil {
		t.Fatalf("CodingPromptRecall: %s", err)
	}
	if strings.Contains(again.Text, "projects/seedling#") {
		t.Fatalf("a page shown a moment ago is not shown again:\n%s", again.Text)
	}

	for _, prompt := range []string{"yes go on", "<task-notification> the database container tests finished </task-notification>"} {
		request.ShownPaths, request.Prompt = nil, prompt
		assent, err := world.run.agent.CodingPromptRecall(context.Background(), world.agent, world.run.settings.Owner, request)
		if err != nil {
			t.Fatalf("CodingPromptRecall: %s", err)
		}
		if assent.Text != "" {
			t.Fatalf("%q recalls nothing:\n%s", prompt, assent.Text)
		}
	}
}

// A capture asks the computer's source of that tool to read again, and
// leaves alone a source of another tool, on another computer, or paused
// by the person; asked twice before it runs, it is asked once.
func TestACaptureAsksOnlyTheToolsSourceOnThatComputer(t *testing.T) {
	world := newCodingWorld(t)
	other := func(name, sourceType, computerName string, isEnabled bool) *models.AgentKnowledgeSource {
		var source *models.AgentKnowledgeSource
		dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
			var err error
			source, err = tx.PutAgentSource(&models.AgentKnowledgeSource{
				AgentID: world.agent.ID, Kind: models.SourceComputer, Name: name, Enabled: isEnabled,
				Specification: models.AgentKnowledgeSpecification{Type: sourceType, Computer: computerName, Format: models.FormatTyped},
			})
			if err != nil {
				t.Fatalf("PutAgentSource: %s", err)
			}
		})
		return source
	}
	codex := other("codex", "codex", "workbench", true)
	elsewhere := other("claude-code-laptop", "claude-code", "laptop", true)
	paused := other("claude-code-paused", "claude-code", "workbench", false)

	capture := func() bool {
		var isAsked bool
		dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
			var err error
			if isAsked, err = CaptureCodingSession(tx, world.agent.ID, "workbench", "claude-code"); err != nil {
				t.Fatalf("CaptureCodingSession: %s", err)
			}
		})
		return isAsked
	}
	if !capture() {
		t.Fatalf("the tool's source on the computer is asked")
	}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		for _, source := range []*models.AgentKnowledgeSource{world.source, codex, elsewhere, paused} {
			current, err := tx.GetAgentSource(world.agent.ID, source.ID)
			if err != nil {
				t.Fatalf("GetAgentSource: %s", err)
			}
			isDue := current.NextRunAt != nil && !current.NextRunAt.After(time.Now())
			if isDue != (source.ID == world.source.ID) {
				t.Fatalf("%s due: %v", source.Name, isDue)
			}
			if current.Generation != source.Generation {
				t.Fatalf("%s was saved as if edited, which abandons a pass under way", source.Name)
			}
		}
	})
	// Asked again before it runs, it stays due from the first request,
	// and the request is recorded again: a pass may already be reading.
	var firstDue *time.Time
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		current, _ := tx.GetAgentSource(world.agent.ID, world.source.ID)
		firstDue = current.NextRunAt
	})
	if !capture() {
		t.Fatalf("a source already due is still asked, so a pass under way reads again")
	}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		current, _ := tx.GetAgentSource(world.agent.ID, world.source.ID)
		if current.NextRunAt == nil || !current.NextRunAt.Equal(*firstDue) || current.RunRequestedAt == nil || !current.RunRequestedAt.After(*firstDue) {
			t.Fatalf("due as before (%v, was %v), asked again (%v)", current.NextRunAt, firstDue, current.RunRequestedAt)
		}
	})
}

// A line is found by how it starts, with % and _ in the prefix taken as
// they are written rather than as wildcards.
func TestLinesAreFoundByHowTheyStart(t *testing.T) {
	world := newRecallWorld(t)
	world.page(t, "topics/discounts", "discounts", "Offers.", "50% off the seed catalogue", "50x off nothing", "500 seeds sown")
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		facts, err := tx.ListAgentFactsStartingWith(world.agent.ID, "50%", 0)
		if err != nil {
			t.Fatalf("ListAgentFactsStartingWith: %s", err)
		}
		if len(facts) != 1 || facts[0].Text != "50% off the seed catalogue" {
			t.Fatalf("only the line that starts with 50%% itself: %d found", len(facts))
		}
	})
}

// A pass that ends after a capture asked for another keeps the request
// rather than putting it off to the source's next scheduled time: the
// pass may have read the transcript before the answer was written.
func TestAPassKeepsARequestMadeWhileItRan(t *testing.T) {
	world := newCodingWorld(t)
	tomorrow := time.Now().Add(24 * time.Hour)
	nextRunAfterPass := func(isAskedDuringPass bool) *time.Time {
		var snapshot *models.AgentKnowledgeSource
		dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
			var err error
			if snapshot, err = tx.GetAgentSource(world.agent.ID, world.source.ID); err != nil {
				t.Fatalf("GetAgentSource: %s", err)
			}
			if isAskedDuringPass {
				if err := tx.RequestAgentSourceRun(world.source.ID, time.Now()); err != nil {
					t.Fatalf("RequestAgentSourceRun: %s", err)
				}
			}
		})
		if err := world.run.agent.markSource(context.Background(), snapshot, nil, db.SourceCounts{}, false, "", tomorrow); err != nil {
			t.Fatalf("markSource: %s", err)
		}
		var current *models.AgentKnowledgeSource
		dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
			current, _ = tx.GetAgentSource(world.agent.ID, world.source.ID)
		})
		return current.NextRunAt
	}
	if next := nextRunAfterPass(false); next == nil || next.Before(tomorrow.Add(-time.Minute)) {
		t.Fatalf("with nothing asked, the next run is the scheduled one: %v", next)
	}
	if next := nextRunAfterPass(true); next == nil || next.After(time.Now()) {
		t.Fatalf("a request made during the pass stands: %v", next)
	}
	// The pass that then starts from the top reads what was asked for,
	// and answers the request.
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		if err := tx.ClearAgentSourceRunRequest(world.source.ID, time.Now().Add(time.Second)); err != nil {
			t.Fatalf("ClearAgentSourceRunRequest: %s", err)
		}
	})
	if next := nextRunAfterPass(false); next == nil || next.Before(tomorrow.Add(-time.Minute)) {
		t.Fatalf("once a pass has answered the request, the next run is the scheduled one: %v", next)
	}
}

// A unit's passages overlap, and a post read in one is not read twice.
func TestPostsAreReadOnceAcrossOverlappingPassages(t *testing.T) {
	posts := postsOf([]*models.AgentChunk{
		{Text: "09:00 alice: first request\n09:01 Claude Code: an answer\nthat runs on"},
		{Text: "09:01 Claude Code: an answer\nthat runs on\n09:02 alice: second request"},
	})
	var said []string
	for _, post := range posts {
		said = append(said, post.author+": "+post.text)
	}
	want := []string{"alice: first request", "Claude Code: an answer\nthat runs on", "alice: second request"}
	if strings.Join(said, "|") != strings.Join(want, "|") {
		t.Fatalf("posts %q, want %q", said, want)
	}
}

func TestCleanDirectoryReadsTheHomeDirectory(t *testing.T) {
	for _, example := range []struct{ directory, home, want string }{
		{"~/code/seedling/", "/srv/alice", "/srv/alice/code/seedling"},
		{"/srv/checkouts/./seedling", "", "/srv/checkouts/seedling"},
		{"~/code/", "", "~/code"},
		{"code/seedling", "/srv/alice", ""},
	} {
		if got := cleanDirectory(example.directory, example.home); got != example.want {
			t.Fatalf("cleanDirectory(%q, %q) = %q, want %q", example.directory, example.home, got, example.want)
		}
	}
}
