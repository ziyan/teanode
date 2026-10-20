package migrations_test

import (
	"os"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// A confirmed speaker name is tied to a contact by both of its columns or by
// neither, and only to a contact that exists. Deleting the contact unties the
// name and keeps it, and the reverse file takes the two columns away.
func TestTheRecordingSpeakerContactMigrationKeepsBothColumnsTogetherAndReverses(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	userId := dbtest.CreateUser(t, database, "reverse-speaker-contact-user")
	for _, statement := range []string{
		`INSERT INTO "addressbook" ("id", "created_at", "modified_at", "user_id", "name") VALUES ('reverse-speaker-book', now(), now(), '` + userId + `', 'Contacts')`,
		`INSERT INTO "contact" ("id", "addressbook_id", "created_at", "modified_at", "uid", "etag", "card", "name") VALUES ('reverse-speaker-contact', 'reverse-speaker-book', now(), now(), 'uid-one', 'e1', '', 'Sam Example')`,
		`INSERT INTO "recording_speaker_identity" ("id", "user_id", "confirmed_name", "created_at", "contact_addressbook_id", "contact_id") VALUES ('reverse-speaker-tied', '` + userId + `', 'Participant One', now(), 'reverse-speaker-book', 'reverse-speaker-contact')`,
		`INSERT INTO "recording_speaker_identity" ("id", "user_id", "confirmed_name", "created_at") VALUES ('reverse-speaker-untied', '` + userId + `', 'Participant Two', now())`,
	} {
		dbtest.Exec(t, database, statement)
	}

	for name, statement := range map[string]string{
		"book only":       `UPDATE "recording_speaker_identity" SET "contact_addressbook_id" = 'reverse-speaker-book' WHERE "id" = 'reverse-speaker-untied'`,
		"contact only":    `UPDATE "recording_speaker_identity" SET "contact_id" = 'reverse-speaker-contact' WHERE "id" = 'reverse-speaker-untied'`,
		"missing contact": `UPDATE "recording_speaker_identity" SET "contact_addressbook_id" = 'reverse-speaker-book', "contact_id" = 'missing' WHERE "id" = 'reverse-speaker-untied'`,
	} {
		if err := execute(database, statement); err == nil {
			t.Errorf("%s: the database accepted a row it should refuse", name)
		}
	}

	dbtest.Exec(t, database, `DELETE FROM "contact" WHERE "id" = 'reverse-speaker-contact'`)
	if got := dbtest.QueryString(t, database, `SELECT coalesce(contact_id, 'none') || ',' || coalesce(contact_addressbook_id, 'none') FROM "recording_speaker_identity" WHERE "id" = 'reverse-speaker-tied'`); got != "none,none" {
		t.Errorf("a deleted contact left the name tied: %q", got)
	}
	if got := dbtest.QueryString(t, database, `SELECT count(*) FROM "recording_speaker_identity"`); got != "2" {
		t.Errorf("a deleted contact took %s of 2 names with it", got)
	}

	reverse, err := os.ReadFile("0247_recording_speaker_contact.reverse.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	dbtest.Exec(t, database, string(reverse))
	if got := dbtest.QueryString(t, database, `SELECT count(*) FROM information_schema.columns WHERE table_name = 'recording_speaker_identity' AND column_name LIKE 'contact%'`); got != "0" {
		t.Errorf("%s contact columns are still there", got)
	}
}
