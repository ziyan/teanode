package migrations_test

import (
	"os"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// A chat holds one going call at a time, a call's states are the known ones,
// every existing message names no call, and the reverse file takes the tables
// and the column away.
func TestTheChatCallMigrationHoldsOneGoingCallAndReverses(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	userId := dbtest.CreateUser(t, database, "reverse-call-user")
	for _, statement := range []string{
		`INSERT INTO "chat" ("id", "created_at", "modified_at", "chat_kind", "direct_key") VALUES ('reverse-call-chat', now(), now(), 'direct', 'call:a:b')`,
		`INSERT INTO "chat_message" ("id", "chat_id", "created_at", "author_member_name", "body") VALUES ('reverse-call-message', 'reverse-call-chat', now(), 'Mira', 'hello')`,
		`INSERT INTO "chat_call" ("id", "chat_id", "started_by_user_id", "started_at") VALUES ('reverse-call-1', 'reverse-call-chat', '` + userId + `', now())`,
		`INSERT INTO "chat_call_member" ("call_id", "user_id", "ring_state") VALUES ('reverse-call-1', '` + userId + `', 'none')`,
	} {
		dbtest.Exec(t, database, statement)
	}
	if got := dbtest.QueryString(t, database, `SELECT COALESCE("call_id", 'none') FROM "chat_message" WHERE "id" = 'reverse-call-message'`); got != "none" {
		t.Errorf("an existing message names a call: %q", got)
	}
	if err := execute(database, `INSERT INTO "chat_call" ("id", "chat_id", "started_at") VALUES ('reverse-call-2', 'reverse-call-chat', now())`); err == nil {
		t.Error("a second going call was kept in one chat")
	}
	dbtest.Exec(t, database, `UPDATE "chat_call" SET "ended_at" = now(), "end_reason" = 'ended' WHERE "id" = 'reverse-call-1'`)
	dbtest.Exec(t, database, `INSERT INTO "chat_call" ("id", "chat_id", "started_at") VALUES ('reverse-call-2', 'reverse-call-chat', now())`)
	for statement, what := range map[string]string{
		`UPDATE "chat_call" SET "end_reason" = 'hung up' WHERE "id" = 'reverse-call-1'`:          "an end reason",
		`UPDATE "chat_call" SET "transcript_state" = 'pending' WHERE "id" = 'reverse-call-1'`:    "a transcript state",
		`UPDATE "chat_call_member" SET "ring_state" = 'busy' WHERE "call_id" = 'reverse-call-1'`: "a ring state",
	} {
		if err := execute(database, statement); err == nil {
			t.Errorf("the database accepted %s it does not know", what)
		}
	}

	// The migrations after it are undone first, as they are undone newest
	// first.
	for _, name := range []string{"0271_recording_chat_call.reverse.sql", "0270_chat_call_member_consent.reverse.sql", "0269_chat_call.reverse.sql"} {
		reverse, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("ReadFile: %s", err)
		}
		dbtest.Exec(t, database, string(reverse))
	}
	if got := dbtest.QueryString(t, database, `SELECT count(*)::text FROM information_schema.columns WHERE table_name = 'chat_message' AND column_name = 'call_id'`); got != "0" {
		t.Error("the chat_message table still has call_id")
	}
	for _, table := range []string{"chat_call", "chat_call_member", "chat_call_transcript_segment"} {
		if got := dbtest.QueryString(t, database, `SELECT count(*)::text FROM information_schema.tables WHERE table_name = '`+table+`'`); got != "0" {
			t.Errorf("the %s table is still there", table)
		}
	}
}
