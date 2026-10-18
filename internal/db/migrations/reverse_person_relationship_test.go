package migrations_test

import (
	"os"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// The relationship migration keeps one fact per pair whichever of the two
// said it, refuses a kind, a gender or a pronoun outside its vocabularies
// and a person related to themselves, and goes with either account. Its
// reverse file takes the table and the column away.
func TestThePersonRelationshipMigrationKeepsOnePerPairAndReverses(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	userId := dbtest.CreateUser(t, database, "reverse-relationship-user")
	otherId := dbtest.CreateUser(t, database, "reverse-relationship-other")
	insert := func(id, from, to, kind, gender string) string {
		return `INSERT INTO "person_relationship" ("id", "created_at", "modified_at", "user_id", "related_user_id", "relationship_kind", "related_user_term_gender") VALUES ('` +
			id + `', now(), now(), '` + from + `', '` + to + `', '` + kind + `', '` + gender + `')`
	}
	dbtest.Exec(t, database, insert("reverse-relationship", userId, otherId, "parent", "feminine"))
	dbtest.Exec(t, database, `UPDATE "user" SET "pronoun" = 'she' WHERE "id" = '`+userId+`'`)

	for name, statement := range map[string]string{
		"otherside":  insert("reverse-relationship-again", otherId, userId, "child", "neutral"),
		"badkind":    insert("reverse-relationship-kind", userId, otherId, "neighbor", "neutral"),
		"badgender":  insert("reverse-relationship-gender", userId, otherId, "spouse", "female"),
		"themselves": insert("reverse-relationship-self", userId, userId, "friend", "neutral"),
		"badpronoun": `UPDATE "user" SET "pronoun" = 'it' WHERE "id" = '` + otherId + `'`,
	} {
		if err := execute(database, statement); err == nil {
			t.Errorf("%s: the database accepted a row it should refuse", name)
		}
	}

	dbtest.Exec(t, database, `DELETE FROM "user" WHERE "id" = '`+otherId+`'`)
	if got := dbtest.QueryString(t, database, `SELECT count(*) FROM "person_relationship"`); got != "0" {
		t.Errorf("a deleted account leaves %s relationships", got)
	}

	reverse, err := os.ReadFile("0243_person_relationship.reverse.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	dbtest.Exec(t, database, string(reverse))
	if got := dbtest.QueryString(t, database, `SELECT coalesce(to_regclass('"person_relationship"')::text, '')`); got != "" {
		t.Errorf("the person_relationship table is still there: %q", got)
	}
	if got := dbtest.QueryString(t, database, `SELECT count(*) FROM information_schema.columns WHERE table_name = 'user' AND column_name = 'pronoun'`); got != "0" {
		t.Errorf("the pronoun column is still there")
	}
}
