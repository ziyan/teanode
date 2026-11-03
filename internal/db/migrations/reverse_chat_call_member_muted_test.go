package migrations_test

import (
	"os"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// The migration gives each member of a call a microphone that starts on, and
// the reverse file takes it away again.
func TestTheChatCallMutedMigrationAddsTheMicrophoneAndReverses(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	userId := dbtest.CreateUser(t, database, "reverse-muted-user")
	for _, statement := range []string{
		`INSERT INTO "chat" ("id", "created_at", "modified_at", "chat_kind", "direct_key") VALUES ('reverse-muted-chat', now(), now(), 'direct', 'muted:a:b')`,
		`INSERT INTO "chat_call" ("id", "chat_id", "started_by_user_id", "started_at") VALUES ('reverse-muted-call', 'reverse-muted-chat', '` + userId + `', now())`,
		`INSERT INTO "chat_call_member" ("call_id", "user_id", "ring_state") VALUES ('reverse-muted-call', '` + userId + `', 'none')`,
	} {
		dbtest.Exec(t, database, statement)
	}
	if got := dbtest.QueryString(t, database, `SELECT "is_muted"::text FROM "chat_call_member" WHERE "call_id" = 'reverse-muted-call'`); got != "false" {
		t.Errorf("a new member's microphone started as muted %q", got)
	}

	reverse, err := os.ReadFile("0272_chat_call_member_muted.reverse.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	dbtest.Exec(t, database, string(reverse))
	countColumns := `SELECT count(*)::text FROM information_schema.columns WHERE table_name = 'chat_call_member' AND column_name = 'is_muted'`
	if got := dbtest.QueryString(t, database, countColumns); got != "0" {
		t.Error("chat_call_member still keeps whether a member is muted")
	}
}
