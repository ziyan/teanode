package migrations_test

import (
	"os"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// The migration takes away each member's answer to a transcript, and the
// reverse file gives every member an empty one back.
func TestTheChatCallConsentMigrationDropsTheAnswerAndReverses(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	countColumns := `SELECT count(*)::text FROM information_schema.columns WHERE table_name = 'chat_call_member' AND column_name = 'transcription_consent'`
	if got := dbtest.QueryString(t, database, countColumns); got != "0" {
		t.Fatal("chat_call_member still keeps an answer to a transcript")
	}
	userId := dbtest.CreateUser(t, database, "reverse-consent-user")
	for _, statement := range []string{
		`INSERT INTO "chat" ("id", "created_at", "modified_at", "chat_kind", "direct_key") VALUES ('reverse-consent-chat', now(), now(), 'direct', 'consent:a:b')`,
		`INSERT INTO "chat_call" ("id", "chat_id", "started_by_user_id", "started_at") VALUES ('reverse-consent-call', 'reverse-consent-chat', '` + userId + `', now())`,
		`INSERT INTO "chat_call_member" ("call_id", "user_id", "ring_state") VALUES ('reverse-consent-call', '` + userId + `', 'none')`,
	} {
		dbtest.Exec(t, database, statement)
	}

	reverse, err := os.ReadFile("0270_chat_call_member_consent.reverse.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	dbtest.Exec(t, database, string(reverse))
	if got := dbtest.QueryString(t, database, `SELECT "transcription_consent" FROM "chat_call_member" WHERE "call_id" = 'reverse-consent-call'`); got != "" {
		t.Errorf("a member's answer came back as %q", got)
	}
	if err := execute(database, `UPDATE "chat_call_member" SET "transcription_consent" = 'maybe' WHERE "call_id" = 'reverse-consent-call'`); err == nil {
		t.Error("the database accepted an answer it does not know")
	}
}
