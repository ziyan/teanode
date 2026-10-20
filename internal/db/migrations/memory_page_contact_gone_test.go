package migrations_test

import (
	"os"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// A page naming a contact that is gone is tied again to the one contact with
// exactly its name in the books the agent was given, unless another page has
// that name or names that contact already. Every other tie to a contact that is
// gone is taken off, with the link saying the page is that contact and the
// account's own card, and a tie to a contact that is there is left alone.
// Running it twice changes nothing more.
func TestTheMemoryPageContactGoneMigrationTiesByNameOrUnties(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	userId := dbtest.CreateUser(t, database, "contact-gone-user")
	contact := func(book, id, name string) string {
		return `INSERT INTO "contact" ("id", "addressbook_id", "created_at", "modified_at", "uid", "etag", "card", "name") VALUES ('` + id + `', '` + book + `', now(), now(), 'uid-` + id + `', 'e1', '', '` + name + `')`
	}
	page := func(id, name, contactId string) string {
		return `INSERT INTO "agent_node" ("id", "agent_id", "path", "kind", "name", "contact_id", "created_at", "modified_at") VALUES ('` + id + `', 'contact-gone-agent', 'people/` + id + `', 'person', '` + name + `', '` + contactId + `', now(), now())`
	}
	link := func(id, nodeId, contactId string) string {
		return `INSERT INTO "agent_record_link" ("id", "agent_id", "node_id", "record_kind", "record_id", "record_relation", "created_at", "modified_at") VALUES ('` + id + `', 'contact-gone-agent', '` + nodeId + `', 'contact', '` + contactId + `', 'is_contact', now(), now())`
	}
	for _, statement := range []string{
		`INSERT INTO "agent" ("id", "created_at", "modified_at", "user_id", "name") VALUES ('contact-gone-agent', now(), now(), '` + userId + `', 'Agent')`,
		`INSERT INTO "addressbook" ("id", "created_at", "modified_at", "user_id", "name", "agent_granted") VALUES ('book-granted', now(), now(), '` + userId + `', 'Granted', true)`,
		`INSERT INTO "addressbook" ("id", "created_at", "modified_at", "user_id", "name", "agent_granted") VALUES ('book-kept', now(), now(), '` + userId + `', 'Kept', false)`,
		contact("book-granted", "ada-new", "Ada Example"),
		contact("book-granted", "bo-one", "Bo Example"),
		contact("book-granted", "bo-two", "Bo Example"),
		contact("book-kept", "cy-new", "Cy Example"),
		contact("book-granted", "dee-new", "Dee Example"),
		contact("book-granted", "eve", "Eve Example"),
		contact("book-granted", "fay-new", "Fay Example"),
		page("page-ada", "Ada Example", "ada-gone"),
		page("page-bo", "Bo Example", "bo-gone"),
		page("page-cy", "Cy Example", "cy-gone"),
		page("page-dee", "Dee Example", "dee-gone"),
		page("page-dee-again", "Dee Example", "dee-gone-again"),
		page("page-eve", "Eve Example", "eve"),
		page("page-fay", "Fay Example", "fay-gone"),
		page("page-fay-nickname", "Fay", "fay-new"),
		link("link-gone", "page-ada", "ada-gone"),
		link("link-kept", "page-eve", "eve"),
		`UPDATE "user" SET "contact_id" = 'me-gone' WHERE "id" = '` + userId + `'`,
	} {
		dbtest.Exec(t, database, statement)
	}

	forward, err := os.ReadFile("0248_memory_page_contact_gone.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	for round := 0; round < 2; round++ {
		dbtest.Exec(t, database, string(forward))
		for pageId, want := range map[string]string{
			"page-ada":          "ada-new",
			"page-bo":           "none",
			"page-cy":           "none",
			"page-dee":          "none",
			"page-dee-again":    "none",
			"page-eve":          "eve",
			"page-fay":          "none",
			"page-fay-nickname": "fay-new",
		} {
			if got := dbtest.QueryString(t, database, `SELECT coalesce("contact_id", 'none') FROM "agent_node" WHERE "id" = '`+pageId+`'`); got != want {
				t.Errorf("round %d: %s names %q, want %q", round, pageId, got, want)
			}
		}
		if got := dbtest.QueryString(t, database, `SELECT string_agg("id", ',' ORDER BY "id") FROM "agent_record_link"`); got != "link-kept" {
			t.Errorf("round %d: the links left are %q, want only the one to a contact that is there", round, got)
		}
		if got := dbtest.QueryString(t, database, `SELECT coalesce("contact_id", 'none') FROM "user" WHERE "id" = '`+userId+`'`); got != "none" {
			t.Errorf("round %d: the account still names a contact that is gone: %q", round, got)
		}
		if got := dbtest.QueryString(t, database, `SELECT count(*) FROM "agent_node" WHERE "agent_id" = 'contact-gone-agent'`); got != "8" {
			t.Errorf("round %d: %s of 8 pages are left", round, got)
		}
	}
}
