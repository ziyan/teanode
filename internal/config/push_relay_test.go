package config

import (
	"strings"
	"testing"
)

// A relay needs at least one of the two push credentials, the three Apple
// fields go together, and the Firebase service account has to be the JSON
// file.
func TestThePushRelayNeedsACredentialForAPlatform(t *testing.T) {
	t.Parallel()

	const serviceAccount = `{"type":"service_account","project_id":"example-project"}`
	for name, relay := range map[string]AgentPushRelay{
		"Apple alone":    {Enabled: true, APNSKeyID: "ABCDE12345", APNSTeamID: "TEAM123456", APNSKey: "key"},
		"Firebase alone": {Enabled: true, FCMServiceAccount: serviceAccount},
		"both":           {Enabled: true, APNSKeyID: "ABCDE12345", APNSTeamID: "TEAM123456", APNSKey: "key", FCMServiceAccount: serviceAccount},
		"off":            {},
	} {
		configuration := validConfiguration()
		configuration.Agent.Push.Relay = relay
		if err := configuration.Validate(); err != nil {
			t.Errorf("%s should be valid, got: %s", name, err)
		}
	}
	for name, relay := range map[string]AgentPushRelay{
		"on with nothing":          {Enabled: true},
		"part of the Apple key":    {Enabled: true, APNSKeyID: "ABCDE12345", FCMServiceAccount: serviceAccount},
		"a service account's path": {Enabled: true, FCMServiceAccount: "/etc/service-account.json"},
		"another kind of file":     {Enabled: true, FCMServiceAccount: `{"type":"authorized_user"}`},
	} {
		configuration := validConfiguration()
		configuration.Agent.Push.Relay = relay
		err := configuration.Validate()
		if err == nil {
			t.Errorf("%s should not be valid", name)
		} else if !strings.Contains(err.Error(), "agent.push.relay") {
			t.Errorf("%s: the error does not name the setting: %s", name, err)
		}
	}
}
