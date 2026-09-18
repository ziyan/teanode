package db_test

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// graphAgent makes an account with an agent and its roots, which is what
// every test here starts from.
func graphAgent(t *testing.T, tx db.Transaction) *models.Agent {
	t.Helper()
	owner, err := tx.CreateUser(&models.User{Username: "alice-" + time.Now().Format("150405.000000000"), Name: "Alice Example"})
	if err != nil {
		t.Fatalf("CreateUser: %s", err)
	}
	agent, err := tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Name: "Bertie"})
	if err != nil {
		t.Fatalf("CreateAgent: %s", err)
	}
	if err := tx.EnsureAgentRoots(agent.ID); err != nil {
		t.Fatalf("EnsureAgentRoots: %s", err)
	}
	return agent
}

// A page written three levels down brings the folders above it into
// being, because that is what somebody filing a thing three levels down
// means. Writing the roots twice changes nothing.
func TestGraphMakesTheFoldersAbeveAPage(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agent := graphAgent(t, tx)
		if err := tx.EnsureAgentRoots(agent.ID); err != nil {
			t.Fatalf("roots twice: %s", err)
		}

		node, err := tx.PutAgentNode(&models.AgentNode{
			AgentID: agent.ID, Path: "work/mujin/dev/portal",
			Kind: models.NodeProject, Name: "Portal",
		})
		if err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		if node.ParentID == "" {
			t.Fatalf("a page below a root has a parent")
		}
		for _, path := range []string{"work", "work/mujin", "work/mujin/dev"} {
			found, err := tx.GetAgentNode(agent.ID, path)
			if err != nil || found == nil {
				t.Fatalf("%q was made on the way down: %v %s", path, found, err)
			}
			if found.Kind != models.NodeFolder {
				t.Fatalf("%q is a folder, not %q", path, found.Kind)
			}
		}
		// And the roots are there, self among them.
		self, err := tx.GetAgentNode(agent.ID, models.PathSelf)
		if err != nil || self == nil || self.Kind != models.NodeSelf {
			t.Fatalf("the self page: %v %s", self, err)
		}
	})
}

// A path is an address: written twice it is the same page, and the second
// write changes it rather than making another.
func TestGraphAPathIsAnAddress(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agent := graphAgent(t, tx)
		first, err := tx.PutAgentNode(&models.AgentNode{AgentID: agent.ID, Path: "people/alice-chen", Kind: models.NodePerson, Name: "Alice Chen"})
		if err != nil {
			t.Fatalf("first: %s", err)
		}
		second, err := tx.PutAgentNode(&models.AgentNode{
			AgentID: agent.ID, Path: "people/alice-chen", Kind: models.NodePerson,
			Name: "Alice Chen", Summary: "Runs the platform team at Acme.",
		})
		if err != nil {
			t.Fatalf("second: %s", err)
		}
		if first.ID != second.ID {
			t.Fatalf("one path, one page: %q then %q", first.ID, second.ID)
		}
		if second.Summary == "" {
			t.Fatalf("the second write is what the page says now")
		}
		if !second.CreatedAt.Equal(first.CreatedAt) {
			t.Fatalf("a page keeps the day it was made")
		}
	})
}

// Facts are numbered within their page, and a number never names two
// sentences even after one is forgotten.
func TestGraphFactNumbersAreStable(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agent := graphAgent(t, tx)
		node, err := tx.PutAgentNode(&models.AgentNode{AgentID: agent.ID, Path: "people/alice-chen", Kind: models.NodePerson, Name: "Alice Chen"})
		if err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		add := func(text string) *models.AgentFact {
			t.Helper()
			fact, err := tx.AddAgentFact(&models.AgentFact{
				AgentID: agent.ID, NodeID: node.ID, Kind: models.FactPlain, Text: text,
				Evidence:  []models.Evidence{{Kind: models.EvidencePerson, Quote: text}},
				Audiences: []models.AgentAudience{models.AudienceAsk},
			})
			if err != nil {
				t.Fatalf("AddAgentFact(%q): %s", text, err)
			}
			return fact
		}
		one := add("Runs the platform team.")
		two := add("Sits in the Tokyo office.")
		if one.Number != 1 || two.Number != 2 {
			t.Fatalf("numbered in order: %d, %d", one.Number, two.Number)
		}
		if err := tx.DeleteAgentFact(agent.ID, two.ID); err != nil {
			t.Fatalf("DeleteAgentFact: %s", err)
		}
		three := add("Moved to the fleet team.")
		if three.Number != 3 {
			t.Fatalf("a forgotten number is not handed out again: %d", three.Number)
		}
		found, err := tx.GetAgentFact(agent.ID, node.ID, 1)
		if err != nil || found == nil || found.ID != one.ID {
			t.Fatalf("a fact is reached by page and number: %v %s", found, err)
		}
		if found.Reference("people/alice-chen") != "people/alice-chen#1" {
			t.Fatalf("that is how it is cited: %q", found.Reference("people/alice-chen"))
		}
	})
}

// Moving a page rewrites the paths of everything beneath it, and refuses
// to put a page inside itself.
func TestGraphMoveRewritesTheSubtree(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agent := graphAgent(t, tx)
		for _, path := range []string{"notes/portal", "notes/portal/api", "notes/portal/api/graphql"} {
			if _, err := tx.PutAgentNode(&models.AgentNode{AgentID: agent.ID, Path: path, Kind: models.NodeTopic}); err != nil {
				t.Fatalf("PutAgentNode(%q): %s", path, err)
			}
		}
		if _, err := tx.MoveAgentNode(agent.ID, "notes/portal", "projects"); err != nil {
			t.Fatalf("MoveAgentNode: %s", err)
		}
		for _, path := range []string{"projects/portal", "projects/portal/api", "projects/portal/api/graphql"} {
			found, err := tx.GetAgentNode(agent.ID, path)
			if err != nil || found == nil {
				t.Fatalf("%q came with it: %v %s", path, found, err)
			}
		}
		if gone, err := tx.GetAgentNode(agent.ID, "notes/portal"); err != nil || gone != nil {
			t.Fatalf("and left where it was: %v %s", gone, err)
		}
		if _, err := tx.MoveAgentNode(agent.ID, "projects/portal", "projects/portal/api"); err == nil {
			t.Fatalf("a page cannot be moved inside itself")
		}
	})
}

// Words find pages and facts. Not every word: the search is what somebody
// typed, and every word of it has to be there.
func TestGraphSearchFindsPagesAndFacts(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agent := graphAgent(t, tx)
		node, err := tx.PutAgentNode(&models.AgentNode{
			AgentID: agent.ID, Path: "things/kittiwake", Kind: models.NodeThing,
			Name: "Kittiwake", Summary: "The neighbour's sailing boat.",
		})
		if err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		if _, err := tx.AddAgentFact(&models.AgentFact{
			AgentID: agent.ID, NodeID: node.ID, Kind: models.FactPlain,
			Text: "Repainted every spring, usually in April.",
		}); err != nil {
			t.Fatalf("AddAgentFact: %s", err)
		}
		nodes, _, err := tx.SearchAgentGraph(agent.ID, "kittiwake", 10)
		if err != nil {
			t.Fatalf("SearchAgentGraph: %s", err)
		}
		if len(nodes) != 1 || nodes[0].ID != node.ID {
			t.Fatalf("the page by its name: %v", nodes)
		}
		_, facts, err := tx.SearchAgentGraph(agent.ID, "repainted spring", 10)
		if err != nil {
			t.Fatalf("SearchAgentGraph: %s", err)
		}
		if len(facts) != 1 {
			t.Fatalf("the fact by two of its words: %v", facts)
		}
		nodes, facts, err = tx.SearchAgentGraph(agent.ID, "submarine", 10)
		if err != nil {
			t.Fatalf("SearchAgentGraph: %s", err)
		}
		if len(nodes) != 0 || len(facts) != 0 {
			t.Fatalf("and nothing for a word nothing holds: %v %v", nodes, facts)
		}
	})
}

// An edge joins two pages and answers with both paths, because a path is
// what a reader and a model can do something with.
func TestGraphEdgesCarryPaths(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agent := graphAgent(t, tx)
		alice, err := tx.PutAgentNode(&models.AgentNode{AgentID: agent.ID, Path: "people/alice-chen", Kind: models.NodePerson, Name: "Alice Chen"})
		if err != nil {
			t.Fatalf("alice: %s", err)
		}
		project, err := tx.PutAgentNode(&models.AgentNode{AgentID: agent.ID, Path: "projects/portal", Kind: models.NodeProject, Name: "Portal"})
		if err != nil {
			t.Fatalf("project: %s", err)
		}
		if err := tx.PutAgentEdge(&models.AgentEdge{
			AgentID: agent.ID, FromID: alice.ID, ToID: project.ID, Relation: models.EdgeWorksOn,
		}); err != nil {
			t.Fatalf("PutAgentEdge: %s", err)
		}
		// Twice is once: an edge is a statement, not a log.
		if err := tx.PutAgentEdge(&models.AgentEdge{
			AgentID: agent.ID, FromID: alice.ID, ToID: project.ID, Relation: models.EdgeWorksOn, Weight: 2,
		}); err != nil {
			t.Fatalf("PutAgentEdge twice: %s", err)
		}
		edges, err := tx.ListAgentEdges(agent.ID, project.ID)
		if err != nil {
			t.Fatalf("ListAgentEdges: %s", err)
		}
		if len(edges) != 1 {
			t.Fatalf("one edge: %v", edges)
		}
		if edges[0].FromPath != "people/alice-chen" || edges[0].ToPath != "projects/portal" {
			t.Fatalf("with both paths: %q -> %q", edges[0].FromPath, edges[0].ToPath)
		}
		if err := tx.PutAgentEdge(&models.AgentEdge{
			AgentID: agent.ID, FromID: alice.ID, ToID: alice.ID, Relation: models.EdgeKnows,
		}); err == nil {
			t.Fatalf("a page is not joined to itself")
		}
	})
}

// A link the night guessed is stored as a guess, and stating the same
// link is how a person confirms it.
//
// There is no promotion stage and no expiry: a guess nobody confirms
// stays a guess, and the only thing that turns it into a statement is
// somebody making the same link themselves, which is the same call the
// Link dialog already made.
func TestGraphAProposedLinkIsConfirmedByStatingIt(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agent := graphAgent(t, tx)
		alice, err := tx.PutAgentNode(&models.AgentNode{AgentID: agent.ID, Path: "people/alice-chen", Kind: models.NodePerson, Name: "Alice Chen"})
		if err != nil {
			t.Fatalf("alice: %s", err)
		}
		gripper, err := tx.PutAgentNode(&models.AgentNode{AgentID: agent.ID, Path: "things/gripper", Kind: models.NodeThing, Name: "Gripper"})
		if err != nil {
			t.Fatalf("gripper: %s", err)
		}
		if err := tx.PutAgentEdge(&models.AgentEdge{
			AgentID: agent.ID, FromID: alice.ID, ToID: gripper.ID, Relation: models.EdgeWorksOn,
			Weight: 0.5, Status: models.EdgeProposed, Note: "she wrote the payload angle check",
		}); err != nil {
			t.Fatalf("PutAgentEdge: %s", err)
		}
		onlyEdge := func(where string) *models.AgentEdge {
			edges, err := tx.ListAgentEdges(agent.ID, gripper.ID)
			if err != nil {
				t.Fatalf("ListAgentEdges %s: %s", where, err)
			}
			if len(edges) != 1 {
				t.Fatalf("one link %s: %v", where, edges)
			}
			return edges[0]
		}
		if status := onlyEdge("as written").Status; status != models.EdgeProposed {
			t.Fatalf("a proposed link comes back proposed, not %q", status)
		}

		// The person draws the same link from the dashboard, which says
		// nothing about status and so means stated.
		if err := tx.PutAgentEdge(&models.AgentEdge{
			AgentID: agent.ID, FromID: alice.ID, ToID: gripper.ID, Relation: models.EdgeWorksOn,
			Note: "she wrote the payload angle check",
		}); err != nil {
			t.Fatalf("PutAgentEdge stating it: %s", err)
		}
		confirmed := onlyEdge("after confirming")
		if confirmed.Status != models.EdgeStated {
			t.Fatalf("stating a guess confirms it, not %q", confirmed.Status)
		}
		if confirmed.Note != "she wrote the payload angle check" {
			t.Fatalf("and keeps what the link is about: %q", confirmed.Note)
		}
	})
}

// The index is ordered by importance and never by use, because a prompt
// whose order moves every turn cannot be cached. Dormant pages are not in
// it at all.
func TestGraphIndexIsOrderedByImportance(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agent := graphAgent(t, tx)
		low, err := tx.PutAgentNode(&models.AgentNode{AgentID: agent.ID, Path: "topics/one", Kind: models.NodeTopic, Importance: 0.1})
		if err != nil {
			t.Fatalf("low: %s", err)
		}
		high, err := tx.PutAgentNode(&models.AgentNode{AgentID: agent.ID, Path: "topics/two", Kind: models.NodeTopic, Importance: 0.9})
		if err != nil {
			t.Fatalf("high: %s", err)
		}
		hidden, err := tx.PutAgentNode(&models.AgentNode{AgentID: agent.ID, Path: "topics/three", Kind: models.NodeTopic, Importance: 1, Dormant: true})
		if err != nil {
			t.Fatalf("hidden: %s", err)
		}
		// Using the low one now must not move it.
		if err := tx.TouchAgentNodes([]string{low.ID}, time.Now()); err != nil {
			t.Fatalf("TouchAgentNodes: %s", err)
		}
		index, err := tx.ListAgentIndex(agent.ID, 100)
		if err != nil {
			t.Fatalf("ListAgentIndex: %s", err)
		}
		var sawHigh, sawLow, sawHidden int
		for position, node := range index {
			switch node.ID {
			case high.ID:
				sawHigh = position + 1
			case low.ID:
				sawLow = position + 1
			case hidden.ID:
				sawHidden = position + 1
			}
		}
		if sawHigh == 0 || sawLow == 0 || sawHigh > sawLow {
			t.Fatalf("importance orders it: high at %d, low at %d", sawHigh, sawLow)
		}
		if sawHidden != 0 {
			t.Fatalf("a dormant page is not in the index, but was at %d", sawHidden)
		}
	})
}

// Deleting a page takes everything under it, and nothing of anybody
// else's.
func TestGraphDeleteTakesTheSubtree(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agent := graphAgent(t, tx)
		for _, path := range []string{"projects/portal", "projects/portal/api", "projects/other"} {
			if _, err := tx.PutAgentNode(&models.AgentNode{AgentID: agent.ID, Path: path, Kind: models.NodeProject}); err != nil {
				t.Fatalf("PutAgentNode(%q): %s", path, err)
			}
		}
		removed, err := tx.DeleteAgentNode(agent.ID, "projects/portal")
		if err != nil {
			t.Fatalf("DeleteAgentNode: %s", err)
		}
		if removed != 2 {
			t.Fatalf("the page and what was under it: %d", removed)
		}
		if other, err := tx.GetAgentNode(agent.ID, "projects/other"); err != nil || other == nil {
			t.Fatalf("a sibling stays: %v %s", other, err)
		}
	})
}

// The contact that is the person is kept on the account, and read back
// with them.
func TestGraphTheContactThatIsThePerson(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "carol", Name: "Carol Example"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		book, err := tx.CreateAddressBook(&models.AddressBook{UserID: owner.ID, Name: "Contacts"})
		if err != nil {
			t.Fatalf("CreateAddressBook: %s", err)
		}
		contact, err := tx.PutContact(&models.Contact{
			AddressBookID: book.ID, UID: "carol-card", Name: "Carol Example", Emails: []string{"carol@example.com"},
			Card: "BEGIN:VCARD\r\nVERSION:4.0\r\nFN:Carol Example\r\nEND:VCARD\r\n",
		})
		if err != nil {
			t.Fatalf("PutContact: %s", err)
		}
		if err := tx.SetUserContact(owner.ID, contact.ID); err != nil {
			t.Fatalf("SetUserContact: %s", err)
		}
		read, err := tx.GetUser(owner.ID)
		if err != nil || read == nil {
			t.Fatalf("GetUser: %v %s", read, err)
		}
		if read.ContactID != contact.ID {
			t.Fatalf("the card that is them: %q", read.ContactID)
		}
		if err := tx.SetUserContact(owner.ID, ""); err != nil {
			t.Fatalf("clearing it: %s", err)
		}
		read, err = tx.GetUser(owner.ID)
		if err != nil || read == nil || read.ContactID != "" {
			t.Fatalf("cleared: %v %s", read, err)
		}
	})
}

// A night the server restarted under is closed when the next one starts,
// with a word about why, rather than saying "still working" for ever.
func TestGraphADreamCutShortIsClosedByTheNext(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agent := graphAgent(t, tx)
		first, err := tx.StartAgentDream(&models.AgentDream{AgentID: agent.ID})
		if err != nil {
			t.Fatalf("StartAgentDream: %s", err)
		}
		second, err := tx.StartAgentDream(&models.AgentDream{AgentID: agent.ID})
		if err != nil {
			t.Fatalf("StartAgentDream again: %s", err)
		}
		dreams, err := tx.ListAgentDreams(agent.ID, 10)
		if err != nil {
			t.Fatalf("ListAgentDreams: %s", err)
		}
		if len(dreams) != 2 {
			t.Fatalf("expected two dreams, got %d", len(dreams))
		}
		for _, dream := range dreams {
			switch dream.ID {
			case first.ID:
				if dream.FinishedAt == nil || dream.LastError == "" {
					t.Errorf("the first night should be closed with a word about why, got finished %v, error %q", dream.FinishedAt, dream.LastError)
				}
			case second.ID:
				if dream.FinishedAt != nil {
					t.Errorf("the second night should still be working")
				}
			}
		}
	})
}

// A month with record and no page is owed a page. So is a month whose
// page reads like a guess, but after the months with none: a page that
// says what a count "suggests" is rewritten, not left as memory.
func TestGraphAGuessedMonthIsOwedAgain(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agent := graphAgent(t, tx)
		self, err := tx.GetAgentNode(agent.ID, models.PathSelf)
		if err != nil || self == nil {
			t.Fatalf("self page: %v", err)
		}
		for _, month := range []string{"2025-03", "2025-04", "2025-05"} {
			happened, _ := time.Parse("2006-01", month)
			if _, err := tx.AddAgentFact(&models.AgentFact{
				AgentID: agent.ID, NodeID: self.ID, Kind: models.FactEvent,
				Text: "Something happened in " + month + ".", HappenedAt: &happened,
			}); err != nil {
				t.Fatalf("AddAgentFact: %s", err)
			}
		}
		for path, summary := range map[string]string{
			"time/2025/03": "The month was spent on the portal.",
			"time/2025/05": "The count of threads suggests a busy month.",
		} {
			if _, err := tx.PutAgentNode(&models.AgentNode{
				AgentID: agent.ID, Path: path, Kind: models.NodePeriod, Name: path, Summary: summary,
			}); err != nil {
				t.Fatalf("PutAgentNode: %s", err)
			}
		}
		guessed := `\m(suggests?|likely)\M`
		owed, err := tx.ListAgentMonthsToWriteUp(agent.ID, nil, 1, 10, guessed)
		if err != nil {
			t.Fatalf("ListAgentMonthsToWriteUp: %s", err)
		}
		if len(owed) != 2 || owed[0] != "2025/04" || owed[1] != "2025/05" {
			t.Fatalf("expected the unwritten month then the guessed one, got %v", owed)
		}
		owed, err = tx.ListAgentMonthsToWriteUp(agent.ID, nil, 1, 10, "")
		if err != nil {
			t.Fatalf("ListAgentMonthsToWriteUp without a pattern: %s", err)
		}
		if len(owed) != 1 || owed[0] != "2025/04" {
			t.Fatalf("expected only the unwritten month, got %v", owed)
		}
	})
}

// What the agent takes off a page it takes off; it does not delete it.
// A fold leaves the row pointing at the one that absorbed it, a striking
// leaves it pointing at nothing, and both say in the page's history which
// of the two happened and why. Only the person's own forgetting removes a
// row, and the entry it leaves carries the whole of what it removed,
// because after that the journal is the only copy.
func TestGraphAFoldAndAStrikingKeepTheRow(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agent := graphAgent(t, tx)
		node, err := tx.PutAgentNode(&models.AgentNode{
			AgentID: agent.ID, Path: "people/alice-chen", Kind: models.NodePerson, Name: "Alice Chen"})
		if err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		happened := time.Date(2019, 4, 1, 0, 0, 0, 0, time.UTC)
		add := func(text string) *models.AgentFact {
			t.Helper()
			fact, err := tx.AddAgentFact(&models.AgentFact{
				AgentID: agent.ID, NodeID: node.ID, Kind: models.FactEvent, Text: text,
				HappenedAt: &happened, Confidence: 0.8, Inferred: true,
				Evidence:  []models.Evidence{{Kind: models.EvidencePerson, Quote: text}},
				Audiences: []models.AgentAudience{models.AudienceAsk},
			})
			if err != nil {
				t.Fatalf("AddAgentFact(%q): %s", text, err)
			}
			return fact
		}
		kept := add("Moved to Osaka in 2019.")
		repeated := add("In 2019 she moved to Osaka.")
		vacuous := add("Alice Chen is a person.")

		folded, err := tx.FoldAgentFact(agent.ID, repeated.ID, kept.ID, "it says what #1 already says")
		if err != nil {
			t.Fatalf("FoldAgentFact: %s", err)
		}
		if folded.SupersededBy != kept.ID || !folded.Dormant {
			t.Fatalf("a folded fact stands behind the one that absorbed it: %+v", folded)
		}
		struck, err := tx.StrikeAgentFact(agent.ID, vacuous.ID, "it only said what the page already says")
		if err != nil {
			t.Fatalf("StrikeAgentFact: %s", err)
		}
		if struck.SupersededBy != "" || !struck.Dormant {
			t.Fatalf("a struck fact stands behind nothing: %+v", struck)
		}

		// Both rows are still there, and the page states neither.
		all, err := tx.ListAgentFacts(agent.ID, node.ID, true, 50)
		if err != nil {
			t.Fatalf("ListAgentFacts: %s", err)
		}
		if len(all) != 3 {
			t.Fatalf("nothing was deleted, so there are three rows, not %d", len(all))
		}
		live, err := tx.ListAgentFacts(agent.ID, node.ID, false, 50)
		if err != nil {
			t.Fatalf("ListAgentFacts: %s", err)
		}
		if len(live) != 1 || live[0].ID != kept.ID {
			t.Fatalf("the page says one thing: %v", live)
		}

		// The person forgets the one that is left.
		if err := tx.DeleteAgentFact(agent.ID, kept.ID); err != nil {
			t.Fatalf("DeleteAgentFact: %s", err)
		}

		revisions, err := tx.ListAgentRevisions(agent.ID, node.ID, 50)
		if err != nil {
			t.Fatalf("ListAgentRevisions: %s", err)
		}
		byKind := map[models.RevisionKind]*models.AgentRevision{}
		for _, revision := range revisions {
			if _, seen := byKind[revision.Kind]; !seen {
				byKind[revision.Kind] = revision
			}
		}
		fold := byKind[models.RevisionFactFolded]
		if fold == nil || fold.After["supersededBy"] != kept.ID || fold.Reason == "" {
			t.Fatalf("the fold is in the history, with both ends and a reason: %+v", fold)
		}
		if byKind[models.RevisionFactStruck] == nil {
			t.Fatalf("and so is the striking, under its own kind: %v", byKind)
		}
		gone := byKind[models.RevisionFactGone]
		if gone == nil {
			t.Fatalf("and so is the deletion")
		}
		// Enough to put it back: the words are not the half of it.
		for _, wanted := range []string{"number", "text", "kind", "confidence", "inferred", "evidence", "audiences", "happenedAt"} {
			if _, carried := gone.Before[wanted]; !carried {
				t.Fatalf("a deletion carries the whole fact, and not %q: %v", wanted, gone.Before)
			}
		}
	})
}

// Moving a page moves what is under it, and search has to follow.
//
// A path is one word to this index, deliberately, so that "notes/kittiwake"
// finds the page and nothing else. The move rewrote the path of every row
// in the subtree but rebuilt the search column of only the one page that
// was named, so everything under it went on being findable by where it
// used to be and was findable by where it is through nothing at all: the
// pages were there, the parent had moved, and searching the path a person
// could see in front of them returned nothing.
func TestGraphMovingAPageReindexesWhatIsUnderIt(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	var agentId, nestlingId string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agentId = graphAgent(t, tx).ID
		if _, err := tx.PutAgentNode(&models.AgentNode{
			AgentID: agentId, Path: "burrow/kittiwake", Kind: models.NodeTopic, Name: "Kittiwake",
		}); err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		nestling, err := tx.PutAgentNode(&models.AgentNode{
			AgentID: agentId, Path: "burrow/kittiwake/nestling", Kind: models.NodeTopic, Name: "Nestling",
		})
		if err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		nestlingId = nestling.ID

		// Before the move it is found where it is, which is what makes
		// the assertions after the move mean something.
		if !graphSearchFinds(t, tx, agentId, "burrow/kittiwake/nestling", nestlingId) {
			t.Fatalf("a page is found by its own path")
		}
	})

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if _, err := tx.MoveAgentNode(agentId, "burrow/kittiwake", "warren"); err != nil {
			t.Fatalf("MoveAgentNode: %s", err)
		}
	})

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		moved, err := tx.GetAgentNode(agentId, "warren/kittiwake/nestling")
		if err != nil || moved == nil || moved.ID != nestlingId {
			t.Fatalf("the page under the moved one has the new path: %v %s", moved, err)
		}
		if !graphSearchFinds(t, tx, agentId, "warren/kittiwake/nestling", nestlingId) {
			t.Errorf("and is found by it")
		}
		if graphSearchFinds(t, tx, agentId, "burrow/kittiwake/nestling", nestlingId) {
			t.Errorf("and is no longer found by the path it left")
		}
	})
}

// graphSearchFinds says whether searching for some words returns a page.
func graphSearchFinds(t *testing.T, tx db.Transaction, agentId, query, nodeId string) bool {
	t.Helper()
	nodes, _, err := tx.SearchAgentGraph(agentId, query, 20)
	if err != nil {
		t.Fatalf("SearchAgentGraph(%q): %s", query, err)
	}
	for _, node := range nodes {
		if node.ID == nodeId {
			return true
		}
	}
	return false
}
