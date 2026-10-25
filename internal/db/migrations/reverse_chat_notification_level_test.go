package migrations_test

import (
	"os"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// The notification level carries a running mute over as off and an ended one
// as all, and its reverse file turns off back into a mute with no end and
// drops what a message mentions.
func TestTheChatNotificationLevelCarriesMutesOverAndReverses(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	forward, err := os.ReadFile("0258_chat_notification_level.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	reverse, err := os.ReadFile("0258_chat_notification_level.reverse.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	dbtest.Exec(t, database, string(reverse))

	mutedId := dbtest.CreateUser(t, database, "level-muted")
	endedId := dbtest.CreateUser(t, database, "level-ended")
	neverId := dbtest.CreateUser(t, database, "level-never")
	dbtest.Exec(t, database, `INSERT INTO "chat" ("id", "created_at", "modified_at", "chat_kind") VALUES ('level-chat', now(), now(), 'group')`)
	for _, statement := range []string{
		`INSERT INTO "chat_member" ("chat_id", "user_id", "member_name", "member_role", "joined_at", "muted_until") VALUES ('level-chat', '` + mutedId + `', 'One', 'member', now(), now() + interval '8 hours')`,
		`INSERT INTO "chat_member" ("chat_id", "user_id", "member_name", "member_role", "joined_at", "muted_until") VALUES ('level-chat', '` + endedId + `', 'Two', 'member', now(), now() - interval '1 hour')`,
		`INSERT INTO "chat_member" ("chat_id", "user_id", "member_name", "member_role", "joined_at") VALUES ('level-chat', '` + neverId + `', 'Three', 'member', now())`,
	} {
		dbtest.Exec(t, database, statement)
	}
	dbtest.Exec(t, database, string(forward))
	levelOf := func(userId string) string {
		return dbtest.QueryString(t, database, `SELECT "notification_level" FROM "chat_member" WHERE "user_id" = '`+userId+`'`)
	}
	if got := levelOf(mutedId); got != "off" {
		t.Errorf("a running mute became %q, want off", got)
	}
	if got := levelOf(endedId); got != "all" {
		t.Errorf("an ended mute became %q, want all", got)
	}
	if got := levelOf(neverId); got != "all" {
		t.Errorf("no mute became %q, want all", got)
	}
	dbtest.Exec(t, database, `INSERT INTO "chat_message" ("id", "chat_id", "created_at", "author_member_name", "body", "mentioned_user_ids", "is_all_mentioned") VALUES ('level-message', 'level-chat', now(), 'One', 'hello', '["`+neverId+`"]', true)`)
	dbtest.Exec(t, database, `UPDATE "chat_member" SET "notification_level" = 'mentions' WHERE "user_id" = '`+endedId+`'`)

	dbtest.Exec(t, database, string(reverse))
	if got := dbtest.QueryString(t, database, `SELECT count(*) FROM "chat_member" WHERE "muted_until" > now() + interval '100 years'`); got != "1" {
		t.Errorf("%s members are muted with no end, want the one that was off", got)
	}
	if got := dbtest.QueryString(t, database, `SELECT count(*) FROM "chat_member" WHERE "muted_until" IS NULL`); got != "2" {
		t.Errorf("%s members are not muted, want two", got)
	}
	for _, column := range [][2]string{{"chat_member", "notification_level"}, {"chat_message", "mentioned_user_ids"}, {"chat_message", "is_all_mentioned"}} {
		if got := dbtest.QueryString(t, database, `SELECT count(*) FROM information_schema.columns WHERE table_name = '`+column[0]+`' AND column_name = '`+column[1]+`'`); got != "0" {
			t.Errorf("the column %s.%s is still there", column[0], column[1])
		}
	}
	dbtest.Exec(t, database, string(forward))
}
