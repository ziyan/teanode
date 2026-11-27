package migrations_test

import (
	"os"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// The migration gives the Administrator role agent:observe and leaves the
// Operator role without it, and the audit log's action column holds the name
// of a read.
func TestTheAdministratorRoleGainsAgentObserve(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	for id, name := range map[string]string{"role-operator-observe-test": "Operator", "role-administrator-observe-test": "Administrator"} {
		dbtest.Exec(t, database, `INSERT INTO "role" ("id", "created_at", "modified_at", "name") VALUES ('`+id+`', now(), now(), '`+name+`') ON CONFLICT DO NOTHING`)
	}
	operatorId := dbtest.QueryString(t, database, `SELECT "id" FROM "role" WHERE lower("name") = 'operator'`)
	administratorId := dbtest.QueryString(t, database, `SELECT "id" FROM "role" WHERE lower("name") = 'administrator'`)

	// The migration ran before the roles above existed, so it is run again.
	forward, err := os.ReadFile("0283_agent_observe.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	dbtest.Exec(t, database, string(forward))

	for _, check := range []struct {
		roleId string
		isHeld bool
	}{{administratorId, true}, {operatorId, false}} {
		held := dbtest.QueryString(t, database, `SELECT count(*)::text FROM "role_permission" WHERE "role_id" = '`+check.roleId+`' AND "permission_key" = 'agent:observe'`) == "1"
		if held != check.isHeld {
			t.Errorf("role %s holds agent:observe: %v, expected %v", check.roleId, held, check.isHeld)
		}
	}
	dbtest.Exec(t, database, `INSERT INTO "audit_event" ("id", "created_at", "actor_kind", "resource_type", "resource_id", "action") VALUES ('observe-test', now(), 'user', 'agent_conversation', 'conversation-one', 'agent.conversation.observe')`)
}
