package migrations_test

import (
	"os"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// A share names its tools and a currency for any limit it sets, a call is in
// one of the six states, a call that waits has a moment it stops waiting, and
// one skill is shared with one person once. The reverse file takes both tables
// away.
func TestTheSkillShareMigrationRefusesBadRowsAndReverses(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	ownerId := dbtest.CreateUser(t, database, "reverse-skill-owner")
	granteeId := dbtest.CreateUser(t, database, "reverse-skill-grantee")
	for _, statement := range []string{
		`INSERT INTO "agent" ("id", "created_at", "modified_at", "user_id", "name") VALUES ('reverse-skill-agent', now(), now(), '` + ownerId + `', 'Agent')`,
		`INSERT INTO "skill_share" ("id", "created_at", "modified_at", "owner_agent_id", "skill_name", "tool_names", "grantee_user_id", "payment_limit_minor", "currency_code")
			VALUES ('reverse-skill-share', now(), now(), 'reverse-skill-agent', 'payments', ARRAY['pay'], '` + granteeId + `', 10000, 'usd')`,
		`INSERT INTO "skill_share_call" ("id", "created_at", "modified_at", "skill_share_id", "tool_name", "call_state")
			VALUES ('reverse-skill-call', now(), now(), 'reverse-skill-share', 'pay', 'ran')`,
	} {
		dbtest.Exec(t, database, statement)
	}

	insertShare := func(id, columns, values string) string {
		return `INSERT INTO "skill_share" ("id", "created_at", "modified_at", "owner_agent_id", "skill_name", "grantee_user_id", ` + columns + `)
			VALUES ('` + id + `', now(), now(), 'reverse-skill-agent', 'other', '` + granteeId + `', ` + values + `)`
	}
	insertCall := func(id, callState, extraColumns, extraValues string) string {
		return `INSERT INTO "skill_share_call" ("id", "created_at", "modified_at", "skill_share_id", "tool_name", "call_state"` + extraColumns + `)
			VALUES ('` + id + `', now(), now(), 'reverse-skill-share', 'pay', '` + callState + `'` + extraValues + `)`
	}
	for name, statement := range map[string]string{
		"no tools":         insertShare("reverse-skill-none", `"tool_names"`, `ARRAY[]::text[]`),
		"limit no money":   insertShare("reverse-skill-money", `"tool_names", "payment_limit_minor"`, `ARRAY['pay'], 500`),
		"zero limit":       insertShare("reverse-skill-zero", `"tool_names", "monthly_limit_minor", "currency_code"`, `ARRAY['pay'], 0, 'usd'`),
		"upper currency":   insertShare("reverse-skill-upper", `"tool_names", "currency_code"`, `ARRAY['pay'], 'USD'`),
		"twice":            `INSERT INTO "skill_share" ("id", "created_at", "modified_at", "owner_agent_id", "skill_name", "tool_names", "grantee_user_id") VALUES ('reverse-skill-twice', now(), now(), 'reverse-skill-agent', 'payments', ARRAY['pay'], '` + granteeId + `')`,
		"unknown state":    insertCall("reverse-skill-state", "running", "", ""),
		"waits for ever":   insertCall("reverse-skill-forever", "waiting_for_approval", "", ""),
		"call of no share": `INSERT INTO "skill_share_call" ("id", "created_at", "modified_at", "skill_share_id", "tool_name", "call_state") VALUES ('reverse-skill-orphan', now(), now(), 'nobody', 'pay', 'ran')`,
	} {
		if err := execute(database, statement); err == nil {
			t.Errorf("%s: the database accepted a row it should refuse", name)
		}
	}
	// Waiting with a moment to stop waiting is fine.
	dbtest.Exec(t, database, insertCall("reverse-skill-waiting", "waiting_for_approval", `, "expires_at"`, `, now() + interval '1 day'`))

	// Removing the share takes its calls with it.
	dbtest.Exec(t, database, `DELETE FROM "skill_share" WHERE "id" = 'reverse-skill-share'`)
	if got := dbtest.QueryString(t, database, `SELECT count(*)::text FROM "skill_share_call"`); got != "0" {
		t.Errorf("%s calls are left after their share went", got)
	}

	reverse, err := os.ReadFile("0197_skill_share.reverse.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	dbtest.Exec(t, database, string(reverse))
	for _, table := range []string{"skill_share", "skill_share_call"} {
		if got := dbtest.QueryString(t, database, `SELECT coalesce(to_regclass('"`+table+`"')::text, '')`); got != "" {
			t.Errorf("the %s table is still there: %q", table, got)
		}
	}
}
