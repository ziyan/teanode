package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/push"
)

// registrationRelay is a relay that answers each sealed registration its
// own way, so that a test can have the VoIP token refused and the ordinary
// one accepted.
type registrationRelay struct {
	mutex   sync.Mutex
	sent    map[string][]*push.Delivery
	answers map[string]error
}

func (self *registrationRelay) Send(_ context.Context, pushRegistration string, delivery *push.Delivery) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.sent == nil {
		self.sent = map[string][]*push.Delivery{}
	}
	self.sent[pushRegistration] = append(self.sent[pushRegistration], delivery)
	return self.answers[pushRegistration]
}

func (self *registrationRelay) to(pushRegistration string) []*push.Delivery {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.sent[pushRegistration]
}

// ringFixture is the phone fixture with the phone registered for calls too,
// and a relay that tells the two registrations apart.
func ringFixture(t *testing.T, voipPushRegistration string) (*phoneFixture, *registrationRelay) {
	t.Helper()
	fixture := newPhoneFixture(t)
	relay := &registrationRelay{answers: map[string]error{}}
	fixture.worker.phones.override = relay
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		if _, err := tx.PutAgentPhone(&models.AgentPhone{
			AgentID: fixture.agent.ID, ClientID: "a-phone-client", PhoneName: "A test phone",
			PushRegistration: "sealed:a-registration", VoIPPushRegistration: voipPushRegistration, PayloadKey: fixture.phone.PayloadKey,
		}); err != nil {
			t.Fatalf("PutAgentPhone: %s", err)
		}
	})
	return fixture, relay
}

func ringContent() *push.Content {
	return &push.Content{
		NotificationKind: models.NotificationCall, Title: "Sam", Body: "is calling",
		ConversationID: "a-chat", CallID: "a-call",
	}
}

func (self *phoneFixture) storedPhone(t *testing.T) *models.AgentPhone {
	t.Helper()
	var stored *models.AgentPhone
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		var err error
		if stored, err = tx.GetAgentPhone(self.agent.ID, self.phone.ID); err != nil || stored == nil {
			t.Fatalf("GetAgentPhone: %v %s", stored, err)
		}
	})
	return stored
}

// A phone that can ring is rung through its VoIP registration alone, and
// what it chose to be told of does not matter: the call is sealed for it.
func TestAPhoneThatCanRingIsRungThroughItsVoIPRegistration(t *testing.T) {
	fixture, relay := ringFixture(t, "sealed:a-voip-registration")
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		if err := tx.UpdateAgentPhoneSettings(fixture.agent.ID, fixture.phone.ID, "A test phone", []string{models.NotificationApproval}); err != nil {
			t.Fatalf("UpdateAgentPhoneSettings: %s", err)
		}
	})
	if !fixture.storedPhone(t).CanRing() {
		t.Fatal("a phone with a VoIP registration and a key cannot ring")
	}
	if rangCount := fixture.worker.Phones().RingPhones(fixture.agent.ID, ringContent()); rangCount != 1 {
		t.Fatalf("%d phones were rung, want 1", rangCount)
	}
	if sent := relay.to("sealed:a-registration"); len(sent) != 0 {
		t.Errorf("a phone that could be rung was also sent %d alerts", len(sent))
	}
	sent := relay.to("sealed:a-voip-registration")
	if len(sent) != 1 {
		t.Fatalf("the VoIP registration was sent %d pushes, want 1", len(sent))
	}
	content := fixture.read(t, sent[0])
	if content.NotificationKind != models.NotificationCall || content.CallID != "a-call" || content.ConversationID != "a-chat" || content.Title != "Sam" {
		t.Errorf("the phone was told %+v", content)
	}
}

// A phone whose app has no VoIP registration yet is told by an ordinary
// alert, so it still shows who is calling.
func TestAPhoneThatCannotRingIsAlertedInstead(t *testing.T) {
	fixture, relay := ringFixture(t, "")
	if fixture.storedPhone(t).CanRing() {
		t.Fatal("a phone with no VoIP registration can ring")
	}
	if rangCount := fixture.worker.Phones().RingPhones(fixture.agent.ID, ringContent()); rangCount != 1 {
		t.Fatalf("%d phones were reached, want 1", rangCount)
	}
	sent := relay.to("sealed:a-registration")
	if len(sent) != 1 || fixture.read(t, sent[0]).CallID != "a-call" {
		t.Errorf("the alert was %v", sent)
	}
}

// Apple saying the VoIP token is gone clears that registration and nothing
// else, and the call is told by an alert so it is not missed.
func TestAGoneVoIPRegistrationIsClearedAndTheCallIsAlerted(t *testing.T) {
	fixture, relay := ringFixture(t, "sealed:a-voip-registration")
	relay.answers["sealed:a-voip-registration"] = push.ErrGone
	if rangCount := fixture.worker.Phones().RingPhones(fixture.agent.ID, ringContent()); rangCount != 1 {
		t.Fatalf("%d phones were reached, want 1", rangCount)
	}
	if sent := relay.to("sealed:a-registration"); len(sent) != 1 {
		t.Errorf("the alert was sent %d times, want 1", len(sent))
	}
	stored := fixture.storedPhone(t)
	if stored.VoIPPushRegistration != "" || stored.CanRing() {
		t.Errorf("the gone VoIP registration is still stored: %q", stored.VoIPPushRegistration)
	}
	if !stored.CanBeNotified() {
		t.Error("the phone's ordinary registration was cleared with the VoIP one")
	}

	// Rung again, it is only alerted: there is nothing to ring.
	fixture.worker.Phones().RingPhones(fixture.agent.ID, ringContent())
	if sent := relay.to("sealed:a-voip-registration"); len(sent) != 1 {
		t.Errorf("a cleared VoIP registration was sent %d pushes, want only the first", len(sent))
	}
}

// Nobody to ring is zero, not an error.
func TestNoPhoneRegisteredRingsNobody(t *testing.T) {
	fixture, relay := ringFixture(t, "sealed:a-voip-registration")
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		if err := tx.DeleteAgentPhone(fixture.agent.ID, fixture.phone.ID); err != nil {
			t.Fatalf("DeleteAgentPhone: %s", err)
		}
	})
	if rangCount := fixture.worker.Phones().RingPhones(fixture.agent.ID, ringContent()); rangCount != 0 {
		t.Errorf("%d phones were rung with none registered", rangCount)
	}
	if len(relay.sent) != 0 {
		t.Errorf("%d registrations were sent to with no phone", len(relay.sent))
	}
}

// A call's end is told to the person's phones but the one that answered or
// declined it: a VoIP push must show a call, and that phone rings no more.
func TestTheEndOfACallSkipsThePhoneThatAnswered(t *testing.T) {
	fixture, relay := ringFixture(t, "sealed:a-voip-registration")
	stored := fixture.storedPhone(t)
	ended := &push.Content{NotificationKind: models.NotificationCallEnded, ConversationID: "a-chat", CallID: "a-call"}

	fixture.worker.Phones().UnringPhones(fixture.agent.ID, ended, stored.ID)
	fixture.worker.Phones().UnringPhones(fixture.agent.ID, ended, "")
	deadline := time.Now().Add(5 * time.Second)
	for len(relay.to("sealed:a-voip-registration")) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if sent := relay.to("sealed:a-voip-registration"); len(sent) != 1 {
		t.Fatalf("the phone was told %d times, want once: only when it was not the one that answered", len(sent))
	}
}
