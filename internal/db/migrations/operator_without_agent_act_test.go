package migrations_test

import (
	"os"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// A server seeded after agent:act existed gave it to the Operator role. The
// migration takes it off Operator, leaves Operator's other permissions, and
// leaves the Administrator's agent:act alone.
func TestTheOperatorRoleLosesAgentAct(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	for id, name := range map[string]string{"role-operator-test": "Operator", "role-administrator-test": "Administrator"} {
		dbtest.Exec(t, database, `INSERT INTO "role" ("id", "created_at", "modified_at", "name") VALUES ('`+id+`', now(), now(), '`+name+`') ON CONFLICT DO NOTHING`)
	}
	operatorId := dbtest.QueryString(t, database, `SELECT "id" FROM "role" WHERE lower("name") = 'operator'`)
	administratorId := dbtest.QueryString(t, database, `SELECT "id" FROM "role" WHERE lower("name") = 'administrator'`)
	for _, row := range [][2]string{{operatorId, "agent:act"}, {operatorId, "agent:audit"}, {administratorId, "agent:act"}} {
		dbtest.Exec(t, database, `INSERT INTO "role_permission" ("role_id", "permission_key") VALUES ('`+row[0]+`', '`+row[1]+`') ON CONFLICT DO NOTHING`)
	}

	// The migration ran before the rows above existed, so it is run again here.
	forward, err := os.ReadFile("0252_operator_without_agent_act.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	dbtest.Exec(t, database, string(forward))

	for _, check := range []struct {
		roleId, permission string
		isHeld             bool
	}{
		{operatorId, "agent:act", false},
		{operatorId, "agent:audit", true},
		{administratorId, "agent:act", true},
	} {
		held := dbtest.QueryString(t, database, `SELECT count(*)::text FROM "role_permission" WHERE "role_id" = '`+check.roleId+`' AND "permission_key" = '`+check.permission+`'`) == "1"
		if held != check.isHeld {
			t.Errorf("role %s holds %s: %v, expected %v", check.roleId, check.permission, held, check.isHeld)
		}
	}
}
