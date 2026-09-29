package agent

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/computer"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// exampleCheckouts is a pass's last page over three invented checkouts:
// an app, the library it is built on, and a checkout holding the
// moduleset that builds both. requiresLibrary says whether the app's
// go.mod still names the library, and modulesetRequiresLibrary whether
// the moduleset still builds the app after it.
func exampleCheckouts(requiresLibrary, modulesetRequiresLibrary bool) ingestPage {
	application := &computer.RepositoryProfile{
		IsBuildRead: true,
		Head:        "1111111111111111111111111111111111111111",
		Module:      "git.example.com/apps/example-app",
		Remotes:     []string{"git@git.example.com:apps/example-app.git"},
		Dependencies: []computer.RepositoryDependency{
			{Name: "example-widgets", Ecosystem: computer.EcosystemNpm, File: "package.json"},
		},
	}
	if requiresLibrary {
		application.Dependencies = append(application.Dependencies,
			computer.RepositoryDependency{Name: "git.example.com/core/example-lib", Ecosystem: computer.EcosystemGo, File: "go.mod"})
	}
	library := &computer.RepositoryProfile{
		IsBuildRead: true,
		Head:        "2222222222222222222222222222222222222222",
		Remotes:     []string{"https://git.example.com/core/example-lib.git"},
	}
	applicationModule := computer.RepositoryModule{Name: "exampleappcpp", Repository: "example-app", File: "modulesets/example.modules"}
	if modulesetRequiresLibrary {
		applicationModule.Dependencies = []string{"examplelibcpp"}
	}
	build := &computer.RepositoryProfile{
		IsBuildRead: true,
		Head:        "3333333333333333333333333333333333333333",
		Modules: []computer.RepositoryModule{
			{Name: "examplelibcpp", Repository: "example-lib", File: "modulesets/example.modules"},
			applicationModule,
			// A module whose repository is not checked out here states
			// nothing that can be a link.
			{Name: "exampletoolcpp", Repository: "example-tool", Dependencies: []string{"examplelibcpp"}, File: "modulesets/example.modules"},
		},
	}
	return ingestPage{IsComplete: true, Entries: []computer.ScanEntry{
		{ExternalID: "example-app", Kind: "repository", Title: "example-app", Repository: application},
		{ExternalID: "example-build", Kind: "repository", Title: "example-build", Repository: build},
		{ExternalID: "example-lib", Kind: "repository", Title: "example-lib", Repository: library},
	}}
}

// dependencyLinksOf is every depends_on link in the graph, as "from ->
// to: quote, quote", sorted.
func dependencyLinksOf(test *testing.T, database db.Database, agentId string) []string {
	var links []string
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		edges, err := tx.ListAgentEdgesByRelation(agentId, models.EdgeDependsOn)
		if err != nil {
			test.Fatal(err)
		}
		for _, edge := range edges {
			var quotes []string
			for _, evidence := range edge.Evidence {
				quotes = append(quotes, evidence.Quote)
			}
			links = append(links, edge.FromPath+" -> "+edge.ToPath+": "+strings.Join(quotes, ", "))
		}
	})
	sort.Strings(links)
	return links
}

// A checkout that requires another is linked to it, by its own build file
// and by the moduleset that builds both; the link goes when neither says
// so any more, and a link the person drew stays whatever the build files
// say.
func TestCheckoutsAreLinkedByWhatTheirBuildFilesSay(test *testing.T) {
	database, worker, run, source := ingestionPageFixture(test)
	if _, _, err := worker.fileComputerPage(test.Context(), run, source, exampleCheckouts(true, true), nil); err != nil {
		test.Fatalf("fileComputerPage: %s", err)
	}
	both := "projects/example-app -> projects/example-lib: dependency:git.example.com/core/example-lib, moduleset:projects/example-build:modulesets/example.modules"
	if got := dependencyLinksOf(test, database, source.AgentID); !reflect.DeepEqual(got, []string{both}) {
		test.Fatalf("links after the first pass:\n%s", strings.Join(got, "\n"))
	}

	// The person says the library depends on the build checkout, which no
	// build file does.
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		library, err := tx.GetAgentNode(source.AgentID, "projects/example-lib")
		if err != nil || library == nil {
			test.Fatalf("the library's page: %v %s", library, err)
		}
		build, err := tx.GetAgentNode(source.AgentID, "projects/example-build")
		if err != nil || build == nil {
			test.Fatalf("the build checkout's page: %v %s", build, err)
		}
		if err := tx.PutAgentEdge(&models.AgentEdge{
			AgentID: source.AgentID, FromID: library.ID, ToID: build.ID, Relation: models.EdgeDependsOn,
			Evidence: []models.Evidence{{Kind: models.EvidencePerson, Quote: "the release scripts live there"}},
		}); err != nil {
			test.Fatal(err)
		}
	})
	drawn := "projects/example-lib -> projects/example-build: the release scripts live there"

	// The app stops naming the library; the moduleset still builds it
	// after the library, so the link stays on that evidence alone.
	if _, _, err := worker.fileComputerPage(test.Context(), run, source, exampleCheckouts(false, true), nil); err != nil {
		test.Fatalf("fileComputerPage: %s", err)
	}
	moduleset := "projects/example-app -> projects/example-lib: moduleset:projects/example-build:modulesets/example.modules"
	if got := dependencyLinksOf(test, database, source.AgentID); !reflect.DeepEqual(got, []string{moduleset, drawn}) {
		test.Fatalf("links after the app stopped naming the library:\n%s", strings.Join(got, "\n"))
	}

	// Neither says so: the link is gone, and the one the person drew is
	// not.
	if _, _, err := worker.fileComputerPage(test.Context(), run, source, exampleCheckouts(false, false), nil); err != nil {
		test.Fatalf("fileComputerPage: %s", err)
	}
	if got := dependencyLinksOf(test, database, source.AgentID); !reflect.DeepEqual(got, []string{drawn}) {
		test.Fatalf("links after nothing names the library:\n%s", strings.Join(got, "\n"))
	}
}

// What a checkout needs that is not a checkout here is one line on its
// page, not a link to nowhere.
func TestDependenciesThatAreNotCheckoutsAreCounted(test *testing.T) {
	database, worker, run, source := ingestionPageFixture(test)
	if _, _, err := worker.fileComputerPage(test.Context(), run, source, exampleCheckouts(true, true), nil); err != nil {
		test.Fatalf("fileComputerPage: %s", err)
	}
	var texts []string
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		application, err := tx.GetAgentNode(source.AgentID, "projects/example-app")
		if err != nil || application == nil {
			test.Fatalf("the app's page: %v %s", application, err)
		}
		facts, err := tx.ListAgentFacts(source.AgentID, application.ID, true, 100)
		if err != nil {
			test.Fatal(err)
		}
		for _, fact := range facts {
			texts = append(texts, fact.Text)
		}
	})
	want := "Depends on 1 package that is not a checkout here, which is example-widgets."
	if !containsText(texts, want) {
		test.Fatalf("the app's page says %q among:\n%s", want, strings.Join(texts, "\n"))
	}
}

// containsText says whether one of the texts is the one wanted.
func containsText(texts []string, wanted string) bool {
	for _, text := range texts {
		if text == wanted {
			return true
		}
	}
	return false
}

// A remote is a host and a path whichever way it is written.
func TestRemoteLocation(test *testing.T) {
	for remote, want := range map[string][2]string{
		"git@git.example.com:core/example-lib.git":             {"git.example.com", "core/example-lib"},
		"https://git.example.com/core/example-lib.git":         {"git.example.com", "core/example-lib"},
		"ssh://git@git.example.com:2222/core/example-lib.git/": {"git.example.com", "core/example-lib"},
		"https://Git.Example.com/group/sub/example-lib":        {"git.example.com", "group/sub/example-lib"},
		"/srv/mirrors/example-lib.git":                         {"", ""},
		"../example-lib":                                       {"", ""},
	} {
		host, projectPath := remoteLocation(remote)
		if got := [2]string{host, projectPath}; got != want {
			test.Errorf("%s: got %q, want %q", remote, got, want)
		}
	}
}

// A monorepo's parts are pages under its own, linked to one another and
// to other checkouts by what they need; a part that goes is put away, and
// its links go with it.
func TestAMonoreposComponentsArePagesWithTheirOwnLinks(test *testing.T) {
	database, worker, run, source := ingestionPageFixture(test)
	page := func(hasViewer bool) ingestPage {
		components := []computer.RepositoryComponent{
			{Path: "libs/core", Name: "examplecore", Ecosystem: computer.EcosystemCMake, File: "libs/core/CMakeLists.txt"},
			{Path: "tools/sync", Name: "git.example.com/mono/tools/sync", Ecosystem: computer.EcosystemGo, File: "tools/sync/go.mod",
				Dependencies: []string{"git.example.com/core/example-lib"}},
		}
		if hasViewer {
			components = append(components, computer.RepositoryComponent{
				Path: "apps/viewer", Name: "exampleviewer", Ecosystem: computer.EcosystemCMake, File: "apps/viewer/CMakeLists.txt",
				Dependencies: []string{"examplecore"},
			})
		}
		return ingestPage{IsComplete: true, Entries: []computer.ScanEntry{
			{ExternalID: "example-lib", Kind: "repository", Title: "example-lib", Repository: &computer.RepositoryProfile{
				IsBuildRead: true, Head: "2222222222222222222222222222222222222222", Remotes: []string{"https://git.example.com/core/example-lib.git"},
			}},
			{ExternalID: "example-mono", Kind: "repository", Title: "example-mono", Repository: &computer.RepositoryProfile{
				IsBuildRead: true, Head: "4444444444444444444444444444444444444444", Components: components,
			}},
		}}
	}
	if _, _, err := worker.fileComputerPage(test.Context(), run, source, page(true), nil); err != nil {
		test.Fatalf("fileComputerPage: %s", err)
	}
	want := []string{
		"projects/example-mono/apps-viewer -> projects/example-mono/libs-core: dependency:examplecore",
		"projects/example-mono/tools-sync -> projects/example-lib: dependency:git.example.com/core/example-lib",
	}
	if got := dependencyLinksOf(test, database, source.AgentID); !reflect.DeepEqual(got, want) {
		test.Fatalf("links:\n%s", strings.Join(got, "\n"))
	}
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		viewer, err := tx.GetAgentNode(source.AgentID, "projects/example-mono/apps-viewer")
		if err != nil || viewer == nil || viewer.Kind != models.NodeProject || viewer.Name != "exampleviewer" {
			test.Fatalf("the viewer's page: %+v %v", viewer, err)
		}
		if viewer.Summary != "exampleviewer, a CMake project in apps/viewer of example-mono." {
			test.Fatalf("the viewer's opening: %q", viewer.Summary)
		}
	})

	// The viewer is gone from the checkout.
	if _, _, err := worker.fileComputerPage(test.Context(), run, source, page(false), nil); err != nil {
		test.Fatalf("fileComputerPage: %s", err)
	}
	if got := dependencyLinksOf(test, database, source.AgentID); !reflect.DeepEqual(got, want[1:]) {
		test.Fatalf("links after the viewer went:\n%s", strings.Join(got, "\n"))
	}
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		viewer, err := tx.GetAgentNode(source.AgentID, "projects/example-mono/apps-viewer")
		if err != nil || viewer == nil || !viewer.Dormant {
			test.Fatalf("the viewer's page is put away, not deleted: %+v %v", viewer, err)
		}
		core, err := tx.GetAgentNode(source.AgentID, "projects/example-mono/libs-core")
		if err != nil || core == nil || core.Dormant {
			test.Fatalf("the core's page stays: %+v %v", core, err)
		}
	})

	// And back again.
	if _, _, err := worker.fileComputerPage(test.Context(), run, source, page(true), nil); err != nil {
		test.Fatalf("fileComputerPage: %s", err)
	}
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		viewer, err := tx.GetAgentNode(source.AgentID, "projects/example-mono/apps-viewer")
		if err != nil || viewer == nil || viewer.Dormant {
			test.Fatalf("the viewer's page is back: %+v %v", viewer, err)
		}
	})
}

// factTextsOf is the live facts of a page, as their words.
func factTextsOf(test *testing.T, database db.Database, agentId, path string) []string {
	var texts []string
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		page, err := tx.GetAgentNode(agentId, path)
		if err != nil || page == nil {
			test.Fatalf("the page %s: %v %v", path, page, err)
		}
		facts, err := tx.ListAgentFacts(agentId, page.ID, false, 100)
		if err != nil {
			test.Fatal(err)
		}
		for _, fact := range facts {
			texts = append(texts, fact.Text)
		}
	})
	sort.Strings(texts)
	return texts
}

// A profile from a program that predates the build-file reader says
// nothing about dependencies, components or activity, and the links, the
// component pages and the lines an earlier pass wrote from them stay.
func TestAProfileFromAnOlderProgramLeavesWhatTheBuildFilesSaidAlone(test *testing.T) {
	database, worker, run, source := ingestionPageFixture(test)
	page := exampleCheckouts(true, true)
	application := page.Entries[0].Repository
	application.Activity = &computer.RepositoryActivity{CommitCountLast90Days: 8, CommitCountLast365Days: 30, AuthorCountLast365Days: 3}
	application.Components = []computer.RepositoryComponent{
		{Path: "cmd/example", Name: "git.example.com/apps/example-app/cmd/example", Ecosystem: computer.EcosystemGo, File: "cmd/example/go.mod"},
	}
	if _, _, err := worker.fileComputerPage(test.Context(), run, source, page, nil); err != nil {
		test.Fatalf("fileComputerPage: %s", err)
	}
	linksBefore := dependencyLinksOf(test, database, source.AgentID)
	factsBefore := factTextsOf(test, database, source.AgentID, "projects/example-app")
	if len(linksBefore) == 0 || !strings.Contains(strings.Join(factsBefore, "\n"), "Activity: ") {
		test.Fatalf("the first pass wrote nothing to keep: %v %v", linksBefore, factsBefore)
	}

	older := exampleCheckouts(true, true)
	for index := range older.Entries {
		profile := *older.Entries[index].Repository
		profile.IsBuildRead, profile.Dependencies, profile.Modules, profile.Components, profile.Activity = false, nil, nil, nil, nil
		older.Entries[index].Repository = &profile
	}
	if _, _, err := worker.fileComputerPage(test.Context(), run, source, older, nil); err != nil {
		test.Fatalf("fileComputerPage: %s", err)
	}
	if got := dependencyLinksOf(test, database, source.AgentID); !reflect.DeepEqual(got, linksBefore) {
		test.Fatalf("links after an older program's pass:\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(linksBefore, "\n"))
	}
	if got := factTextsOf(test, database, source.AgentID, "projects/example-app"); !reflect.DeepEqual(got, factsBefore) {
		test.Fatalf("facts after an older program's pass:\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(factsBefore, "\n"))
	}
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		component, err := tx.GetAgentNode(source.AgentID, "projects/example-app/cmd-example")
		if err != nil || component == nil || component.Dormant {
			test.Fatalf("the component's page is not put away: %+v %v", component, err)
		}
	})
}

// Two checkouts in directories of one name are two pages; the one the
// page already belongs to keeps it; and a name both answer to finds
// neither.
func TestCheckoutsSharingADirectoryNameKeepTheirOwnPages(test *testing.T) {
	database, worker, run, source := ingestionPageFixture(test)
	page := func() ingestPage {
		return ingestPage{IsComplete: true, Entries: []computer.ScanEntry{
			{ExternalID: "tools/example-lib", Kind: "repository", Title: "example-lib", Repository: &computer.RepositoryProfile{
				IsBuildRead: true, Head: "5555555555555555555555555555555555555555",
			}},
			{ExternalID: "upstream/example-lib", Kind: "repository", Title: "example-lib", Repository: &computer.RepositoryProfile{
				IsBuildRead: true, Head: "6666666666666666666666666666666666666666",
			}},
			{ExternalID: "example-app", Kind: "repository", Title: "example-app", Repository: &computer.RepositoryProfile{
				IsBuildRead: true, Head: "7777777777777777777777777777777777777777",
				Dependencies: []computer.RepositoryDependency{{Name: "example-lib", Ecosystem: computer.EcosystemNpm, File: "package.json"}},
			}},
		}}
	}
	// The page is already the second checkout's, from before the first
	// was cloned.
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		existing, err := tx.PutAgentNode(&models.AgentNode{AgentID: source.AgentID, Path: "projects/example-lib", Kind: models.NodeProject, Name: "example-lib"})
		if err != nil {
			test.Fatal(err)
		}
		if err := putKeyedRepositoryFact(tx, source.AgentID, existing.ID, checkoutFactKey,
			checkoutLine("/fixture/upstream/example-lib", "fixture-computer"), "6666666666666666666666666666666666666666"); err != nil {
			test.Fatal(err)
		}
	})
	for range 2 {
		if _, _, err := worker.fileComputerPage(test.Context(), run, source, page(), nil); err != nil {
			test.Fatalf("fileComputerPage: %s", err)
		}
	}
	for path, where := range map[string]string{
		"projects/example-lib":       "/fixture/upstream/example-lib",
		"projects/example-lib-tools": "/fixture/tools/example-lib",
	} {
		if texts := factTextsOf(test, database, source.AgentID, path); !containsText(texts, checkoutLine(where, "fixture-computer")) {
			test.Errorf("%s is not the checkout at %s: %v", path, where, texts)
		}
	}
	if got := dependencyLinksOf(test, database, source.AgentID); len(got) != 0 {
		test.Errorf("a name two checkouts answer to was linked: %v", got)
	}
	if texts := factTextsOf(test, database, source.AgentID, "projects/example-app"); !containsText(texts,
		"Depends on 1 package that is not a checkout here, which is example-lib.") {
		test.Errorf("the ambiguous name is not counted elsewhere: %v", texts)
	}
}

// A Go module past its first major version, and a scoped npm package,
// find the checkout by the word they end in.
func TestVersionedAndScopedNamesFindTheirCheckouts(test *testing.T) {
	database, worker, run, source := ingestionPageFixture(test)
	page := ingestPage{IsComplete: true, Entries: []computer.ScanEntry{
		{ExternalID: "example-lib", Kind: "repository", Title: "example-lib", Repository: &computer.RepositoryProfile{
			IsBuildRead: true, Head: "8888888888888888888888888888888888888888",
			Module: "git.example.com/core/example-lib", Remotes: []string{"git@git.example.com:core/example-lib.git"},
		}},
		{ExternalID: "example-widgets", Kind: "repository", Title: "example-widgets", Repository: &computer.RepositoryProfile{
			IsBuildRead: true, Head: "9999999999999999999999999999999999999999",
		}},
		{ExternalID: "example-app", Kind: "repository", Title: "example-app", Repository: &computer.RepositoryProfile{
			IsBuildRead: true, Head: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Dependencies: []computer.RepositoryDependency{
				{Name: "git.example.com/core/example-lib/v3", Ecosystem: computer.EcosystemGo, File: "go.mod"},
				{Name: "@example/example-widgets", Ecosystem: computer.EcosystemNpm, File: "package.json"},
			},
		}},
	}}
	if _, _, err := worker.fileComputerPage(test.Context(), run, source, page, nil); err != nil {
		test.Fatalf("fileComputerPage: %s", err)
	}
	want := []string{
		"projects/example-app -> projects/example-lib: dependency:git.example.com/core/example-lib/v3",
		"projects/example-app -> projects/example-widgets: dependency:@example/example-widgets",
	}
	if got := dependencyLinksOf(test, database, source.AgentID); !reflect.DeepEqual(got, want) {
		test.Fatalf("links:\n%s", strings.Join(got, "\n"))
	}
}

// A page the person filed where a component would go stays theirs: no
// component line is written on it, and no link goes to or from it.
func TestAComponentDoesNotTakeOverThePersonsPage(test *testing.T) {
	database, worker, run, source := ingestionPageFixture(test)
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		if _, err := tx.PutAgentNode(&models.AgentNode{AgentID: source.AgentID, Path: "projects/example-mono", Kind: models.NodeProject, Name: "example-mono"}); err != nil {
			test.Fatal(err)
		}
		notes, err := tx.PutAgentNode(&models.AgentNode{AgentID: source.AgentID, Path: "projects/example-mono/libs-core", Kind: models.NodeTopic, Name: "Notes on the core"})
		if err != nil {
			test.Fatal(err)
		}
		if _, err := tx.AddAgentFact(&models.AgentFact{AgentID: source.AgentID, NodeID: notes.ID, Kind: models.FactPlain, Text: "The core is due a rewrite."}); err != nil {
			test.Fatal(err)
		}
	})
	page := ingestPage{IsComplete: true, Entries: []computer.ScanEntry{
		{ExternalID: "example-mono", Kind: "repository", Title: "example-mono", Repository: &computer.RepositoryProfile{
			IsBuildRead: true, Head: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			Components: []computer.RepositoryComponent{
				{Path: "libs/core", Name: "examplecore", Ecosystem: computer.EcosystemCMake, File: "libs/core/CMakeLists.txt"},
				{Path: "apps/viewer", Name: "exampleviewer", Ecosystem: computer.EcosystemCMake, File: "apps/viewer/CMakeLists.txt",
					Dependencies: []string{"examplecore"}},
			},
		}},
	}}
	if _, _, err := worker.fileComputerPage(test.Context(), run, source, page, nil); err != nil {
		test.Fatalf("fileComputerPage: %s", err)
	}
	if texts := factTextsOf(test, database, source.AgentID, "projects/example-mono/libs-core"); !reflect.DeepEqual(texts, []string{"The core is due a rewrite."}) {
		test.Errorf("the person's page was written on: %v", texts)
	}
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		notes, err := tx.GetAgentNode(source.AgentID, "projects/example-mono/libs-core")
		if err != nil || notes == nil || notes.Kind != models.NodeTopic || notes.Name != "Notes on the core" {
			test.Errorf("the person's page changed: %+v %v", notes, err)
		}
	})
	if got := dependencyLinksOf(test, database, source.AgentID); len(got) != 0 {
		test.Errorf("a link was written to the person's page: %v", got)
	}
}
