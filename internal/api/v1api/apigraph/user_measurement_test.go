package apigraph

import (
	"fmt"
	"strings"
	"testing"
)

// A person chooses the units they read measurements in as they choose their
// language and zone: it is set, changed and cleared on their own account, a
// value nothing knows is refused and changes nothing, and the account and the
// session both tell it, since the dashboard reads it from the session before
// it draws a page.
func TestAPersonSetsClearsAndIsRefusedTheUnitsTheyRead(t *testing.T) {
	world := newCircleWorld(t)
	read := func() (string, string) {
		data := world.call(t, world.alice, `query { GetCurrentUser { measurementSystem } GetSession { measurementSystem } }`)
		return data["GetCurrentUser"].(map[string]any)["measurementSystem"].(string), data["GetSession"].(map[string]any)["measurementSystem"].(string)
	}
	set := func(measurementSystem string) {
		t.Helper()
		world.call(t, world.alice, fmt.Sprintf(`mutation { UpdateUser(userId: %q, measurementSystem: %q) { measurementSystem } }`, world.alice.ID, measurementSystem))
	}

	if account, session := read(); account != "" || session != "" {
		t.Fatalf("a new account reads %q and %q, expected automatic", account, session)
	}
	for _, given := range []struct{ sent, stored string }{{"us", "us"}, {" metric ", "metric"}, {"", ""}} {
		set(given.sent)
		if account, session := read(); account != given.stored || session != given.stored {
			t.Errorf("after setting %q the account reads %q and the session %q, expected %q", given.sent, account, session, given.stored)
		}
	}

	set("us")
	for _, refused := range []string{"imperial", "Metric", "automatic", "c"} {
		said := world.refused(t, world.alice, fmt.Sprintf(`mutation { UpdateUser(userId: %q, measurementSystem: %q) { id } }`, world.alice.ID, refused))
		if !strings.Contains(said, "metric or us") {
			t.Errorf("%q was refused with %q", refused, said)
		}
		if account, _ := read(); account != "us" {
			t.Errorf("a refused %q changed the account to %q", refused, account)
		}
	}

	// A change to something else leaves the units alone.
	world.call(t, world.alice, fmt.Sprintf(`mutation { UpdateUser(userId: %q, name: "Alice Example") { id } }`, world.alice.ID))
	if account, _ := read(); account != "us" {
		t.Errorf("renaming changed the units to %q", account)
	}

	// Somebody else's account is not theirs to change without managing accounts.
	world.refused(t, world.bob, fmt.Sprintf(`mutation { UpdateUser(userId: %q, measurementSystem: "metric") { id } }`, world.alice.ID))
}
