package migrations_test

import (
	"os"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// Every existing message and file starts as no voice message at all, a
// transcript state is one of the known ones, and the reverse file takes the
// columns and the index away.
func TestTheChatVoiceMessageMigrationStartsEmptyAndReverses(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	userId := dbtest.CreateUser(t, database, "reverse-voice-user")
	for _, statement := range []string{
		`INSERT INTO "agent" ("id", "created_at", "modified_at", "user_id", "name") VALUES ('reverse-voice-agent', now(), now(), '` + userId + `', 'Agent')`,
		`INSERT INTO "agent_attachment" ("id", "created_at", "agent_id", "name") VALUES ('reverse-voice-file', now(), 'reverse-voice-agent', 'Voice message.m4a')`,
		`INSERT INTO "chat" ("id", "created_at", "modified_at", "chat_kind", "direct_key") VALUES ('reverse-voice-chat', now(), now(), 'direct', 'voice:a:b')`,
		`INSERT INTO "chat_message" ("id", "chat_id", "created_at", "author_member_name", "body") VALUES ('reverse-voice-message', 'reverse-voice-chat', now(), 'Mira', 'hello')`,
	} {
		dbtest.Exec(t, database, statement)
	}
	if got := dbtest.QueryString(t, database, `SELECT "audio_duration_ms"::text || '/' || "audio_waveform" FROM "agent_attachment" WHERE "id" = 'reverse-voice-file'`); got != "0/" {
		t.Errorf("a file starts with no length and no waveform: %q", got)
	}
	if got := dbtest.QueryString(t, database, `SELECT "is_voice_message"::text || '/' || "transcript_text" || '/' || "transcript_state" || '/' || COALESCE("transcript_tried_at"::text, 'none') FROM "chat_message" WHERE "id" = 'reverse-voice-message'`); got != "false///none" {
		t.Errorf("a message starts as no voice message: %q", got)
	}
	for _, transcriptState := range []string{"waiting", "done", "failed", "off", ""} {
		dbtest.Exec(t, database, `UPDATE "chat_message" SET "transcript_state" = '`+transcriptState+`' WHERE "id" = 'reverse-voice-message'`)
	}
	if err := execute(database, `UPDATE "chat_message" SET "transcript_state" = 'pending' WHERE "id" = 'reverse-voice-message'`); err == nil {
		t.Error("the database accepted a transcript state it does not know")
	}

	reverse, err := os.ReadFile("0267_chat_voice_message.reverse.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	dbtest.Exec(t, database, string(reverse))
	for table, column := range map[string]string{
		"agent_attachment": "audio_duration_ms", "chat_message": "transcript_state",
	} {
		if got := dbtest.QueryString(t, database, `SELECT count(*)::text FROM information_schema.columns WHERE table_name = '`+table+`' AND column_name = '`+column+`'`); got != "0" {
			t.Errorf("the %s table still has %s", table, column)
		}
	}
	if got := dbtest.QueryString(t, database, `SELECT count(*)::text FROM pg_indexes WHERE indexname = 'chat_message_transcript_waiting'`); got != "0" {
		t.Errorf("the waiting index is still there")
	}
}
