package apigraph

import (
	"crypto/rand"
	"encoding/base64"
	"net/url"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A phone registers the token PushKit gave the app beside its ordinary one,
// replaces it, and removes it, without touching the ordinary registration.
func TestAPhoneRegistersAndReplacesItsVoIPRegistration(test *testing.T) {
	world := newSignInWorld(test)
	world.giveAnAgent(test)
	appClient := world.register(test, "com.example.app:/oauth")
	_, answer := world.post(test, api.PathOAuthToken, url.Values{
		"grant_type": {"urn:teanode:params:oauth:grant-type:connect-code"}, "client_id": {appClient},
		"connect_code": {world.connectCode(test)}, "device_name": {"A test phone"},
	})
	appToken, _ := answer["access_token"].(string)
	if appToken == "" {
		test.Fatalf("the app could not connect: %v", answer)
	}
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	register := func(voipPushRegistration string) []string {
		_, errors := world.askWithToken(test, appToken, `mutation { RegisterAgentPhone(phoneName: "A test phone", phoneModel: "iPhone17,1", pushRegistration: "sealed:a-registration", voipPushRegistration: "`+
			voipPushRegistration+`", payloadKey: "`+base64.StdEncoding.EncodeToString(key)+`") { id } }`)
		return errors
	}
	stored := func() *models.AgentPhone {
		var phones []*models.AgentPhone
		dbtest.RunTransactionOn(test, world.database, func(tx db.Transaction) {
			var err error
			if phones, err = tx.ListNotifiableAgentPhones(); err != nil {
				test.Fatalf("ListNotifiableAgentPhones: %s", err)
			}
		})
		if len(phones) != 1 {
			test.Fatalf("%d phones are registered, want 1", len(phones))
		}
		return phones[0]
	}

	if errors := register("sealed:a-voip-registration"); len(errors) > 0 {
		test.Fatalf("RegisterAgentPhone: %v", errors)
	}
	phone := stored()
	if phone.VoIPPushRegistration != "sealed:a-voip-registration" || !phone.CanRing() {
		test.Errorf("the VoIP registration is %q", phone.VoIPPushRegistration)
	}

	if _, errors := world.ask(test, `mutation { UpdateAgentPhone(phoneId: "`+phone.ID+`", voipPushRegistration: "sealed:a-new-voip-registration") { id } }`); len(errors) > 0 {
		test.Fatalf("UpdateAgentPhone: %v", errors)
	}
	if replaced := stored(); replaced.VoIPPushRegistration != "sealed:a-new-voip-registration" || replaced.PushRegistration != "sealed:a-registration" {
		test.Errorf("after replacing it the registrations are %q and %q", replaced.VoIPPushRegistration, replaced.PushRegistration)
	}

	// Leaving it out keeps it; empty removes it.
	if _, errors := world.ask(test, `mutation { UpdateAgentPhone(phoneId: "`+phone.ID+`", phoneName: "Renamed") { id } }`); len(errors) > 0 {
		test.Fatalf("UpdateAgentPhone: %v", errors)
	}
	if kept := stored(); kept.VoIPPushRegistration != "sealed:a-new-voip-registration" {
		test.Errorf("renaming the phone changed its VoIP registration to %q", kept.VoIPPushRegistration)
	}
	if _, errors := world.ask(test, `mutation { UpdateAgentPhone(phoneId: "`+phone.ID+`", voipPushRegistration: "") { id } }`); len(errors) > 0 {
		test.Fatalf("UpdateAgentPhone: %v", errors)
	}
	if removed := stored(); removed.CanRing() || !removed.CanBeNotified() {
		test.Errorf("after removing it the phone can ring (%v) or cannot be notified (%v)", removed.CanRing(), removed.CanBeNotified())
	}

	if errors := register(strings.Repeat("x", pushRegistrationLongest+1)); len(errors) == 0 {
		test.Error("a VoIP registration longer than any the relay makes was accepted")
	}
}
