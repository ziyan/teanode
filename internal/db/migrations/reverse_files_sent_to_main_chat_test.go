package migrations_test

import (
	"os"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// A file is not waiting for the main chat until it is sent there, and the
// reverse file takes the caption, the sender and the mark away.
func TestTheFilesSentToMainChatMigrationReverses(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	userId := dbtest.CreateUser(t, database, "reverse-files-sent-user")
	dbtest.Exec(t, database, `INSERT INTO "agent" ("id", "created_at", "modified_at", "user_id", "name") VALUES ('files-sent-agent', now(), now(), '`+userId+`', 'Agent')`)
	dbtest.Exec(t, database, `INSERT INTO "agent_attachment" ("id", "created_at", "agent_id", "name", "content_type", "size")
		VALUES ('files-sent-1', now(), 'files-sent-agent', 'chart.png', 'image/png', 1)`)
	if got := dbtest.QueryString(t, database, `SELECT ("main_chat_queued_at" IS NULL)::text || "caption" || "sent_by" FROM "agent_attachment" WHERE "id" = 'files-sent-1'`); got != "true" {
		t.Errorf("a file is not waiting for the main chat until it is sent there: %q", got)
	}

	reverse, err := os.ReadFile("0266_files_sent_to_main_chat.reverse.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	dbtest.Exec(t, database, string(reverse))
	for _, column := range []string{"caption", "sent_by", "main_chat_queued_at"} {
		if got := dbtest.QueryString(t, database, `SELECT count(*)::text FROM information_schema.columns WHERE table_name = 'agent_attachment' AND column_name = '`+column+`'`); got != "0" {
			t.Errorf("the agent_attachment table still has %s", column)
		}
	}
}
