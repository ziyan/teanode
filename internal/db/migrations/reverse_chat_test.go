package migrations_test

import (
	"os"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// The chat migration is undone by its reverse file with chats in place: the
// tables and the column go, and the conversations and the sources that read
// them.
func TestTheChatMigrationReverses(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	userId := dbtest.CreateUser(t, database, "reverse-chat-user")
	otherId := dbtest.CreateUser(t, database, "reverse-chat-other")
	for _, statement := range []string{
		`INSERT INTO "agent" ("id", "created_at", "modified_at", "user_id", "name") VALUES ('reverse-agent', now(), now(), '` + userId + `', 'Agent')`,
		`INSERT INTO "chat" ("id", "created_at", "modified_at", "chat_kind", "direct_key") VALUES ('reverse-chat', now(), now(), 'direct', 'a:b')`,
		`INSERT INTO "chat_member" ("chat_id", "user_id", "member_name", "member_role", "joined_at") VALUES ('reverse-chat', '` + userId + `', 'One', 'member', now())`,
		`INSERT INTO "chat_member" ("chat_id", "user_id", "member_name", "member_role", "joined_at") VALUES ('reverse-chat', '` + otherId + `', 'Two', 'member', now())`,
		`INSERT INTO "chat_message" ("id", "chat_id", "created_at", "author_member_name", "body") VALUES ('reverse-message', 'reverse-chat', now(), 'One', 'hello')`,
		`INSERT INTO "chat_reaction" ("message_id", "reactor_key", "emoji", "created_at") VALUES ('reverse-message', 'user:` + userId + `', 'x', now())`,
		`INSERT INTO "agent_source" ("id", "agent_id", "created_at", "modified_at", "kind") VALUES ('reverse-chat-source', 'reverse-agent', now(), now(), 'chat')`,
		`INSERT INTO "agent_source" ("id", "agent_id", "created_at", "modified_at", "kind") VALUES ('reverse-web-source', 'reverse-agent', now(), now(), 'web')`,
		`INSERT INTO "agent_conversation" ("id", "created_at", "modified_at", "agent_id", "kind", "last_at", "chat_id") VALUES ('reverse-conversation', now(), now(), 'reverse-agent', 'chat', now(), 'reverse-chat')`,
	} {
		dbtest.Exec(t, database, statement)
	}
	reverse, err := os.ReadFile("0194_chat.reverse.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	dbtest.Exec(t, database, string(reverse))
	for _, table := range []string{"chat", "chat_member", "chat_message", "chat_reaction"} {
		if got := dbtest.QueryString(t, database, `SELECT coalesce(to_regclass('"`+table+`"')::text, '')`); got != "" {
			t.Errorf("the table %s is still there: %q", table, got)
		}
	}
	if got := dbtest.QueryString(t, database, `SELECT count(*) FROM "agent_conversation" WHERE "kind" = 'chat'`); got != "0" {
		t.Errorf("%s chat conversations are left", got)
	}
	if got := dbtest.QueryString(t, database, `SELECT string_agg("kind", ',') FROM "agent_source"`); got != "web" {
		t.Errorf("the sources left are %q; want the web one alone, the chat one gone", got)
	}
	if got := dbtest.QueryString(t, database, `SELECT count(*) FROM information_schema.columns WHERE table_name = 'agent_conversation' AND column_name = 'chat_id'`); got != "0" {
		t.Errorf("the chat_id column is still there")
	}
}
