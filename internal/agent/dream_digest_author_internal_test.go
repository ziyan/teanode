package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// Who wrote what, and whether it reaches the graph.
//
// A commit is the only document that carries an author, and for three
// hours on the deployment this was written for the reading filed 162
// facts out of 1,781 of them and drew two links: 139 distinct authors sat
// in document metadata against 29 people in the graph. The reading was
// shown the author and told the link existed, and declined to draw it,
// because drawing it meant opening a page for somebody on the strength of
// one commit message and it had been told to leave out what it would not
// defend. The owner decided the page is worth having, so the prompt now
// says so outright.
//
// What a test can hold is the rest of it: that the author reaches the
// prompt at all, and that a page and a link asked for the way the prompt
// asks for them are actually drawn. The second half is where this used to
// fail silently -- a link named the path its fact asked for, the fact
// landed somewhere else, and the link was dropped without a word.

// authoringProvider is a model that does what the reading prompt tells
// it to. It picks each item's author out of the heading, opens a page for
// them under `people/`, gives the page the one thing the item shows, and
// joins it to the project the batch came from with `works_on`. Where the
// name reads as a machine it does none of that.
//
// The stand-in applies the prompt's rule rather than judging it: what a
// real model makes of the wording is not something a test can hold. What
// these tests hold is everything on either side of that judgement.
func authoringProvider(projectPath, projectName string) (*httptest.Server, func() []string) {
	var mutex sync.Mutex
	var prompts []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body struct {
			Stream   bool             `json:"stream"`
			Messages []map[string]any `json:"messages"`
		}
		_ = json.NewDecoder(request.Body).Decode(&body)
		var shown strings.Builder
		for _, message := range body.Messages {
			_, _ = fmt.Fprintf(&shown, "%v\n", message["content"])
		}
		prompt := shown.String()
		mutex.Lock()
		prompts = append(prompts, prompt)
		mutex.Unlock()

		answer, _ := json.Marshal(authoringAnswer(prompt, projectPath, projectName))
		if body.Stream {
			writer.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprintf(writer,
				"data: {\"id\":\"s1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"content\":%s},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":50,\"completion_tokens\":10}}\n\ndata: [DONE]\n\n",
				answer)
			return
		}
		_, _ = fmt.Fprintf(writer,
			`{"choices":[{"message":{"role":"assistant","content":%s},"finish_reason":"stop"}],"usage":{"prompt_tokens":50,"completion_tokens":10}}`,
			answer)
	}))
	return server, func() []string {
		mutex.Lock()
		defer mutex.Unlock()
		return append([]string(nil), prompts...)
	}
}

// authoringItem is one item as the digest showed it: the marker the
// reading cites it by, its title, and who the heading says wrote it.
type authoringItem struct {
	id     string
	title  string
	author string
}

// authoringItems is the batch, read back out of the prompt the way the
// model reads it: "[id] title — by author — date".
func authoringItems(prompt string) []authoringItem {
	var items []authoringItem
	for _, line := range strings.Split(prompt, "\n") {
		if !strings.HasPrefix(line, "[") {
			continue
		}
		marker, heading, found := strings.Cut(strings.TrimPrefix(line, "["), "] ")
		if !found {
			continue
		}
		item := authoringItem{id: marker, title: heading}
		if title, rest, found := strings.Cut(heading, " — by "); found {
			item.title = title
			item.author, _, _ = strings.Cut(rest, " — ")
		} else if title, _, found := strings.Cut(heading, " — "); found {
			item.title = title
		}
		items = append(items, item)
	}
	return items
}

// authoringAnswer is the object the reading ends with: the project, and a
// page and a link for every author who is a person.
func authoringAnswer(prompt, projectPath, projectName string) string {
	answer := RememberAnswer{Facts: []RememberedFact{}, Links: []RememberedLink{}}
	filed := map[string]bool{}
	for _, item := range authoringItems(prompt) {
		if len(answer.Facts) == 0 {
			answer.Facts = append(answer.Facts, RememberedFact{
				Path: projectPath, NodeKind: "project", NodeName: projectName, Kind: "fact",
				Text:      projectName + " keeps its certificates without a cloud account.",
				Quote:     item.title,
				MessageID: "[" + item.id + "]",
			})
		}
		if item.author == "" || machineLooking(item.author) || filed[item.author] {
			continue
		}
		filed[item.author] = true
		name := authorName(item.author)
		path := models.JoinPath(models.PathPeople, name)
		answer.Facts = append(answer.Facts, RememberedFact{
			Path: path, NodeKind: "person", NodeName: name, Kind: "fact",
			Text:      fmt.Sprintf("Works on %s, where they wrote %q.", projectName, item.title),
			Quote:     item.title,
			MessageID: "[" + item.id + "]",
		})
		answer.Links = append(answer.Links, RememberedLink{
			From: path, To: projectPath, Relation: "works_on",
			Note: "wrote " + item.title,
		})
	}
	written, _ := json.Marshal(answer)
	return string(written)
}

// authorName is the person an author string names, as the prompt asks
// for them: the name in front of the address, or the part in front of the
// @ where the heading gives an address and nothing else. A git author is
// almost always "Name <address>", and a page called
// "Alice.Chen alice.chen example net" is nobody.
func authorName(author string) string {
	if name, _, found := strings.Cut(author, "<"); found && strings.TrimSpace(name) != "" {
		return strings.TrimSpace(name)
	}
	name := strings.TrimSpace(strings.Trim(strings.TrimSpace(author), "<>"))
	if local, _, found := strings.Cut(name, "@"); found {
		return local
	}
	return name
}

// machineLooking is the rule the prompt states, applied by the stand-in
// model: a name that reads as something that runs rather than somebody
// who types. It lives here, in a test, and not in the agent, because it
// is a reading of a name and not a fact about one -- somebody really
// called Robert Botha is owed the benefit of a model's judgement rather
// than a list's.
func machineLooking(author string) bool {
	name := strings.ToLower(author)
	if local, _, found := strings.Cut(name, "@"); found {
		name = local
	}
	name = strings.TrimSpace(strings.Trim(name, "<>"))
	if name == "" {
		return false
	}
	// A word with a number stuck on the end of it: host1093.
	if last := name[len(name)-1]; last >= '0' && last <= '9' {
		return true
	}
	for _, word := range strings.FieldsFunc(name, func(character rune) bool {
		return character < 'a' || character > 'z'
	}) {
		switch word {
		case "bot", "ci", "build", "jenkins", "deploy", "release",
			"noreply", "automation", "service":
			return true
		}
	}
	return false
}

// authoredCommit is one commit a checkout holds: its subject, and who the
// scan recorded as its author.
type authoredCommit struct {
	subject string
	author  string
}

// fileAuthoredCommits writes those commits down as a checkout's
// documents, with their words, and answers them in the order given.
func fileAuthoredCommits(t *testing.T, database db.Database, run *Run, commits []authoredCommit) []*models.AgentDocument {
	t.Helper()
	documents := make([]*models.AgentDocument, 0, len(commits))
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		source, err := tx.PutAgentSource(&models.AgentKnowledgeSource{
			AgentID: run.Agent.ID, Kind: models.SourceArchive, Name: "the checkouts", Enabled: true,
			Specification: models.AgentKnowledgeSpecification{
				Computer: "laptop", Path: "~/projects", Format: models.FormatFiles,
			},
		})
		if err != nil {
			t.Fatalf("PutAgentSource: %s", err)
		}
		happened := time.Date(2026, 9, 17, 5, 1, 0, 0, time.UTC)
		for _, commit := range commits {
			metadata := map[string]any{}
			if commit.author != "" {
				metadata["author"] = commit.author
			}
			document, err := tx.PutAgentDocument(&models.AgentDocument{
				AgentID: run.Agent.ID, SourceID: source.ID, ExternalID: commit.subject,
				Kind: models.DocumentCommit, Title: commit.subject,
				Hash: "hash-of-" + commit.subject, HappenedAt: &happened,
				Metadata: metadata,
			})
			if err != nil {
				t.Fatalf("PutAgentDocument %q: %s", commit.subject, err)
			}
			if err := tx.ReplaceAgentChunks(document, []*models.AgentChunk{{
				Text: commit.subject + "\n\nThe renewal loop used to be started by the\n" +
					"constructor, so a test that built a manager contacted the authority.\n\n" +
					"Files: internal/util/autoacme/manager.go",
			}}); err != nil {
				t.Fatalf("ReplaceAgentChunks %q: %s", commit.subject, err)
			}
			documents = append(documents, document)
		}
	})
	return documents
}

// peopleOf is every page the graph holds under people/, by path, with the
// folder itself left out.
func peopleOf(t *testing.T, database db.Database, run *Run) map[string]*models.AgentNode {
	t.Helper()
	pages := map[string]*models.AgentNode{}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		nodes, err := tx.ListAgentNodesUnder(run.Agent.ID, models.PathPeople, 400)
		if err != nil {
			t.Fatalf("ListAgentNodesUnder: %s", err)
		}
		for _, node := range nodes {
			if node.Path == models.PathPeople {
				continue
			}
			pages[node.Path] = node
		}
	})
	return pages
}

// worksOnFrom is the works_on link out of a page, or nil.
func worksOnFrom(t *testing.T, database db.Database, run *Run, node *models.AgentNode) *models.AgentEdge {
	t.Helper()
	var found *models.AgentEdge
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		edges, err := tx.ListAgentEdges(run.Agent.ID, node.ID)
		if err != nil {
			t.Fatalf("ListAgentEdges: %s", err)
		}
		for _, edge := range edges {
			if edge.Relation == models.EdgeWorksOn && edge.FromID == node.ID {
				found = edge
			}
		}
	})
	return found
}

// The author of a commit earns a page and a link to what they worked on.
//
// One commit is the whole of the evidence, and that is the point: the
// owner was offered a bar -- two commits, five, a name seen in more than
// one repository -- and chose the page.
func TestTheAuthorOfACommitGetsAPageAndALink(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	provider, prompts := authoringProvider("projects/teanode", "TeaNode")
	defer provider.Close()

	worker, run := digestSplitWorld(t, database, provider.URL)
	documents := fileAuthoredCommits(t, database, run, []authoredCommit{
		{subject: "Obtain certificates without a cloud account", author: "Alice.Chen <alice.chen@example.net>"},
	})

	if _, answered := worker.digestBatch(context.Background(), run, documents, &dreamBudget{}, false); !answered {
		t.Fatal("the model answered, so the batch is read")
	}

	// The author reached the prompt, which is what the rule is applied
	// to. A heading joins a title, a name and a date with the same mark,
	// so the name is the one after "by".
	if made := prompts(); len(made) == 0 || !strings.Contains(made[0], "— by Alice.Chen <alice.chen@example.net>") {
		t.Fatalf("the reading is shown who wrote the commit:\n%s", strings.Join(made, "\n"))
	}

	pages := peopleOf(t, database, run)
	page := pages["people/alice-chen"]
	if page == nil {
		t.Fatalf("the author of a commit has a page under people/: %v", pages)
	}
	if page.Kind != models.NodePerson {
		t.Errorf("and it is a person's page, not a %q", page.Kind)
	}

	// A page that says nothing is worse than no page: the prompt asks for
	// the one thing the commit shows about them.
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		facts, err := tx.ListAgentFacts(run.Agent.ID, page.ID, false, 10)
		if err != nil {
			t.Fatalf("ListAgentFacts: %s", err)
		}
		if len(facts) == 0 {
			t.Fatal("the page says what they worked on rather than only that they exist")
		}
		if len(facts[0].Evidence) == 0 || facts[0].Evidence[0].ID != documents[0].ID {
			t.Errorf("with the commit it came from as its evidence: %+v", facts[0].Evidence)
		}
	})

	edge := worksOnFrom(t, database, run, page)
	if edge == nil {
		t.Fatalf("and a works_on link to what they worked on")
	}
	if edge.ToPath != "projects/teanode" {
		t.Errorf("which is the project the commit belongs to, not %q", edge.ToPath)
	}
	if edge.Note == "" {
		t.Errorf("with the sentence that justifies it")
	}
}

// A document nobody is recorded as having written makes nobody a page.
//
// The rule is about the author in the heading and not about every name a
// document happens to contain, and a document with no author has no
// heading to read one out of.
func TestADocumentWithNoAuthorMakesNobodyAPage(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	provider, prompts := authoringProvider("projects/teanode", "TeaNode")
	defer provider.Close()

	worker, run := digestSplitWorld(t, database, provider.URL)
	documents := fileAuthoredCommits(t, database, run, []authoredCommit{
		{subject: "Move the ACME handler in front of authentication"},
	})

	if _, answered := worker.digestBatch(context.Background(), run, documents, &dreamBudget{}, false); !answered {
		t.Fatal("the model answered, so the batch is read")
	}

	if made := prompts(); len(made) == 0 || strings.Contains(made[0], "— by ") {
		t.Fatalf("nothing said who wrote this, so the heading names nobody:\n%s", strings.Join(made, "\n"))
	}
	if pages := peopleOf(t, database, run); len(pages) != 0 {
		t.Fatalf("and nobody gained a page from it: %v", pages)
	}

	// The batch still taught something; it is only the authorship that is
	// missing.
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		node, err := tx.GetAgentNode(run.Agent.ID, "projects/teanode")
		if err != nil || node == nil {
			t.Fatalf("what the commit was about is still filed: %v %s", node, err)
		}
	})
}

// An author that is a machine is not a person, and gets nothing.
//
// Both of these are real authors in the owner's tree. A page for a build
// bot is noise, and there are enough of them to bury the people.
func TestAnAuthorThatReadsAsAMachineGetsNoPage(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	provider, prompts := authoringProvider("projects/teanode", "TeaNode")
	defer provider.Close()

	worker, run := digestSplitWorld(t, database, provider.URL)
	documents := fileAuthoredCommits(t, database, run, []authoredCommit{
		{subject: "Bump the base image", author: "bot-testing@example.net"},
		{subject: "Re-run the fixtures", author: "host1093@example.net"},
		{subject: "Sign the release archives", author: "Alice.Chen <alice.chen@example.net>"},
	})

	if _, answered := worker.digestBatch(context.Background(), run, documents, &dreamBudget{}, false); !answered {
		t.Fatal("the model answered, so the batch is read")
	}

	// The machines are shown as well: the rule is one the reading applies
	// to a name it can see, not one anything hides from it.
	made := prompts()
	if len(made) == 0 || !strings.Contains(made[0], "— by bot-testing@example.net") {
		t.Fatalf("the reading is shown every author, machine or not:\n%s", strings.Join(made, "\n"))
	}

	pages := peopleOf(t, database, run)
	if len(pages) != 1 || pages["people/alice-chen"] == nil {
		t.Fatalf("the person in the batch has a page and the machines do not: %v", pages)
	}
}

// The owner's own commits link to the owner's own page.
//
// Most of what is in a person's own checkouts was written by them, so
// most of the links this change draws start at them. The page they start
// at is `self` -- a fact filed at people/<their name> is routed there, so
// that what the agent knows about them is in one place -- and a link
// naming the path the fact asked for used to find nothing at it and be
// dropped without a word. That is the greater part of an authorship map
// going missing in silence.
func TestTheOwnersOwnCommitsLinkToTheirOwnPage(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	provider, _ := authoringProvider("projects/teanode", "TeaNode")
	defer provider.Close()

	// digestSplitWorld's person is Alice Example, so this is a commit of
	// their own.
	worker, run := digestSplitWorld(t, database, provider.URL)
	documents := fileAuthoredCommits(t, database, run, []authoredCommit{
		{subject: "Reject before the DATA command", author: "Alice Example"},
	})

	if _, answered := worker.digestBatch(context.Background(), run, documents, &dreamBudget{}, false); !answered {
		t.Fatal("the model answered, so the batch is read")
	}

	if pages := peopleOf(t, database, run); len(pages) != 0 {
		t.Fatalf("the person whose agent this is does not get a second page beside self: %v", pages)
	}

	var page *models.AgentNode
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		found, err := tx.GetAgentNode(run.Agent.ID, models.PathSelf)
		if err != nil || found == nil {
			t.Fatalf("their own page: %v %s", found, err)
		}
		page = found
	})
	edge := worksOnFrom(t, database, run, page)
	if edge == nil {
		t.Fatal("the link the reading drew for their own commit lands on their own page rather than being dropped")
	}
	if edge.ToPath != "projects/teanode" {
		t.Errorf("and joins them to the project, not to %q", edge.ToPath)
	}

	// And the fact went to the same page the link did, which is the whole
	// of the argument for routing both the same way.
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		facts, err := tx.ListAgentFacts(run.Agent.ID, page.ID, false, 10)
		if err != nil {
			t.Fatalf("ListAgentFacts: %s", err)
		}
		if len(facts) == 0 {
			t.Fatal("what their commit showed about them is on their own page")
		}
	})
}

// A link whose two ends turn out to be the same page loses nothing.
//
// Both spellings of the person route to `self` now, so a reading that
// joined one to the other asks for a page joined to itself -- which the
// database refuses with an error, not a shrug, and an error there throws
// away the whole window, since everything a run learned is written in one
// transaction. Before this routing the second spelling was simply not
// found and the link was dropped, so the shape was unreachable.
func TestALinkBetweenTwoSpellingsOfThePersonDoesNotLoseTheBatch(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	provider, _ := authoringProvider("projects/teanode", "TeaNode")
	defer provider.Close()

	worker, run := digestSplitWorld(t, database, provider.URL)
	answer := &RememberAnswer{
		Facts: []RememberedFact{{
			Path: "people/alice-example", NodeKind: "person", NodeName: "Alice Example",
			Kind: "fact", Text: "Wrote the certificate change.",
		}},
		Links: []RememberedLink{{
			From: "people/alice-example", To: "people/alice",
			Relation: "knows", Note: "the same person twice",
		}},
	}
	filed, err := worker.fileWhatWasLearned(context.Background(), run, answer, nil,
		models.EvidenceDocument, nil, nil)
	if err != nil {
		t.Fatalf("the window is written rather than thrown away over a link nobody can draw: %s", err)
	}
	if filed.Filed != 1 {
		t.Errorf("and what it learned is kept: %d", filed.Filed)
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		page, err := tx.GetAgentNode(run.Agent.ID, models.PathSelf)
		if err != nil || page == nil {
			t.Fatalf("their own page: %v %s", page, err)
		}
		facts, err := tx.ListAgentFacts(run.Agent.ID, page.ID, false, 10)
		if err != nil {
			t.Fatalf("ListAgentFacts: %s", err)
		}
		if len(facts) != 1 {
			t.Fatalf("on their own page and not on a second one: %d facts", len(facts))
		}
	})
	if pages := peopleOf(t, database, run); len(pages) != 0 {
		t.Errorf("and nothing was opened beside it: %v", pages)
	}
}

// The wording itself, because the wording is the change.
//
// The reading was already shown the author and already told that a person
// works_on a project. What it was missing was permission to open a page
// for somebody it had seen once, and it is the last line of that
// paragraph -- leave out any you would not defend -- that it was obeying.
func TestTheReadingIsToldTheAuthorOfAnItemGetsAPage(t *testing.T) {
	t.Parallel()

	rendered, err := render("digest.txt", map[string]any{
		"PersonName": "Alice", "Items": "[d1] Obtain certificates — by Alice.Chen — 17 Sep 2026", "Most": 5,
	})
	if err != nil {
		t.Fatalf("digest.txt: %s", err)
	}
	for _, said := range []string{
		// The author of an item, and not every name in it.
		"The author of an item gets a page",
		"after `by`",
		"goes under `people/`",
		"`works_on` link",
		// Named for the person and not for the address they commit from.
		"the address in neither its name nor its path",
		// The page is worth opening only if it says something.
		"the one thing the item shows about them",
		// And the machines are left out by a rule read off the name.
		"An author that is a machine is not a person",
		"stuck on the end of it",
		// The line the reading was obeying when it drew nothing.
		"except the author's",
	} {
		if !strings.Contains(rendered, said) {
			t.Errorf("the reading is told %q:\n%s", said, rendered)
		}
	}

	// And the object the words are turned into when the model answers in
	// prose has to ask for the same thing, or a reading that took the long
	// way round files none of it.
	object, err := render("digest_object.txt", map[string]any{
		"PersonName": "Alice", "Reading": "Alice.Chen wrote the certificate change.",
	})
	if err != nil {
		t.Fatalf("digest_object.txt: %s", err)
	}
	for _, said := range []string{"`people/`", "`works_on`", "reads as a machine"} {
		if !strings.Contains(object, said) {
			t.Errorf("and so is the call that writes the object: %q missing:\n%s", said, object)
		}
	}
}
