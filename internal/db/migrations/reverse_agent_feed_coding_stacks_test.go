package migrations_test

import (
	"os"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// A coding session's question posted before its kind stacked goes in the
// stack of its day, the day and the title in the person's zone and language,
// and the reverse file takes it out again. Other cards are left alone.
func TestTheCodingStacksMigrationStacksQuestionsByDayAndReverses(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	japaneseUserId := dbtest.CreateUser(t, database, "coding-stack-japanese")
	unzonedUserId := dbtest.CreateUser(t, database, "coding-stack-unzoned")
	for _, statement := range []string{
		`UPDATE "user" SET "timezone" = 'Asia/Tokyo', "locale" = 'ja-JP' WHERE "id" = '` + japaneseUserId + `'`,
		`UPDATE "user" SET "timezone" = 'Nowhere/Unknown' WHERE "id" = '` + unzonedUserId + `'`,
		`INSERT INTO "agent" ("id", "created_at", "modified_at", "user_id", "name") VALUES ('coding-stack-agent-1', now(), now(), '` + japaneseUserId + `', 'Agent')`,
		`INSERT INTO "agent" ("id", "created_at", "modified_at", "user_id", "name") VALUES ('coding-stack-agent-2', now(), now(), '` + unzonedUserId + `', 'Agent')`,
		// 23:30 UTC on a Wednesday is already Thursday morning in Tokyo.
		`INSERT INTO "agent_feed_item" ("id", "agent_id", "user_id", "feed_item_kind", "feed_source_kind", "created_at", "updated_at")
			VALUES ('coding-stack-tokyo', 'coding-stack-agent-1', '` + japaneseUserId + `', 'decision', 'coding_session', '2026-03-04 23:30:00+00', now())`,
		`INSERT INTO "agent_feed_item" ("id", "agent_id", "user_id", "feed_item_kind", "feed_source_kind", "created_at", "updated_at")
			VALUES ('coding-stack-unzoned', 'coding-stack-agent-2', '` + unzonedUserId + `', 'decision', 'coding_session', '2026-03-04 23:30:00+00', now())`,
		`INSERT INTO "agent_feed_item" ("id", "agent_id", "user_id", "feed_item_kind", "feed_source_kind", "created_at", "updated_at")
			VALUES ('coding-stack-other', 'coding-stack-agent-2', '` + unzonedUserId + `', 'update', 'agent', '2026-03-04 23:30:00+00', now())`,
	} {
		dbtest.Exec(t, database, statement)
	}
	forward, err := os.ReadFile("0278_agent_feed_coding_stacks.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	dbtest.Exec(t, database, string(forward))
	stackOf := func(itemId string) string {
		return dbtest.QueryString(t, database, `SELECT "feed_stack_key" || ' / ' || "feed_stack_title" FROM "agent_feed_item" WHERE "id" = '`+itemId+`'`)
	}
	if got := stackOf("coding-stack-tokyo"); got != "coding:2026-03-05 / コーディングセッション、3月5日(木)" {
		t.Errorf("a question in Tokyo: %q", got)
	}
	if got := stackOf("coding-stack-unzoned"); got != "coding:2026-03-04 / Coding sessions, Wed Mar 4" {
		t.Errorf("a question of a person in no known zone: %q", got)
	}
	if got := stackOf("coding-stack-other"); got != " / " {
		t.Errorf("another card: %q", got)
	}

	reverse, err := os.ReadFile("0278_agent_feed_coding_stacks.reverse.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	dbtest.Exec(t, database, string(reverse))
	if got := stackOf("coding-stack-tokyo"); got != " / " {
		t.Errorf("after the reverse file: %q", got)
	}
}
