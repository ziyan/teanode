package people_test

import (
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/people"
)

// Another account's notification address is where its mailed notices go, so
// a new one asks first like the groups and the sign-in switch do; a new name
// stays an ordinary write.
func TestUserUpdateRisk(t *testing.T) {
	var user *tools.Tool
	for _, each := range tools.Build().All() {
		if each.Name == "user" {
			user = each
		}
	}
	if user == nil {
		t.Fatal("user is not registered")
	}
	for arguments, wanted := range map[string]tools.Risk{
		`{"action":"update","user_id":"user-two","email":"someone@example.org"}`: tools.RiskOutward,
		`{"action":"update","user_id":"user-two","name":"Robin"}`:                tools.RiskWrite,
		`{"action":"update","user_id":"user-two","group_ids":["group-one"]}`:     tools.RiskGranting,
		`{"action":"update","user_id":"user-two","disabled":true}`:               tools.RiskDestructive,
	} {
		if risk := user.RiskFor([]byte(arguments)); risk != wanted {
			t.Errorf("%s is judged %q, wanted %q", arguments, risk, wanted)
		}
	}
}
