package migrations_test

import (
	"os"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// The migration ties a call to the recording it became, names the speaker
// of an original, and drops the call's own transcript; a call has one
// recording at most, and the reverse file puts the transcript's table back.
func TestTheChatCallRecordingMigrationTiesACallToItsRecordingAndReverses(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	userId := dbtest.CreateUser(t, database, "reverse-call-recording-user")
	for _, statement := range []string{
		`INSERT INTO "chat" ("id", "created_at", "modified_at", "chat_kind", "direct_key") VALUES ('reverse-recording-chat', now(), now(), 'direct', 'recording:a:b')`,
		`INSERT INTO "chat_call" ("id", "chat_id", "started_by_user_id", "started_at") VALUES ('reverse-recording-call', 'reverse-recording-chat', '` + userId + `', now())`,
		`INSERT INTO "recording" ("id", "user_id", "created_at", "updated_at", "client_recording_id", "recording_title", "recording_origin", "recording_capture_state", "chat_call_id") VALUES ('reverse-recording-1', '` + userId + `', now(), now(), 'call-one', '', 'audioImport', 'imported', 'reverse-recording-call')`,
		`UPDATE "chat_call" SET "recording_id" = 'reverse-recording-1' WHERE "id" = 'reverse-recording-call'`,
	} {
		dbtest.Exec(t, database, statement)
	}
	if err := execute(database, `INSERT INTO "recording" ("id", "user_id", "created_at", "updated_at", "client_recording_id", "recording_title", "recording_origin", "recording_capture_state", "chat_call_id") VALUES ('reverse-recording-2', '`+userId+`', now(), now(), 'call-two', '', 'audioImport', 'imported', 'reverse-recording-call')`); err == nil {
		t.Error("a second recording was made of one call")
	}
	if got := dbtest.QueryString(t, database, `SELECT count(*)::text FROM information_schema.tables WHERE table_name = 'chat_call_transcript_segment'`); got != "0" {
		t.Error("the call's own transcript table is still there")
	}

	reverse, err := os.ReadFile("0271_recording_chat_call.reverse.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	dbtest.Exec(t, database, string(reverse))
	if got := dbtest.QueryString(t, database, `SELECT count(*)::text FROM information_schema.tables WHERE table_name = 'chat_call_transcript_segment'`); got != "1" {
		t.Error("the call's own transcript table did not come back")
	}
	for table, column := range map[string]string{"recording": "chat_call_id", "chat_call": "recording_id", "recording_media": "speaker_user_id"} {
		if got := dbtest.QueryString(t, database, `SELECT count(*)::text FROM information_schema.columns WHERE table_name = '`+table+`' AND column_name = '`+column+`'`); got != "0" {
			t.Errorf("%s still has %s", table, column)
		}
	}
}
