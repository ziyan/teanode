package apipush

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/push"
)

// testRegistrationToken is an FCM registration token's shape, made at run
// time so that no literal in the repository looks like a credential to a
// secret scanner.
var testRegistrationToken = "f" + strings.Repeat("Ab1_-", 30) + ":" + strings.Repeat("x", 20)

// turnOnFirebase gives the relay a service account that trades with the
// fake Google.
func (self *relayWorld) turnOnFirebase(test *testing.T) {
	test.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		test.Fatalf("GenerateKey: %s", err)
	}
	encoded, _ := x509.MarshalPKCS8PrivateKey(key)
	file, _ := json.Marshal(map[string]string{
		"type": "service_account", "project_id": "example-project", "private_key_id": "0123456789abcdef",
		"private_key":  string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded})),
		"client_email": "relay@example-project.iam.example.net", "token_uri": self.firebase.URL + "/token",
	})
	if err := self.store.Update(func(configuration *config.Configuration) error {
		configuration.Agent.Push.Relay.FCMServiceAccount = string(file)
		return nil
	}); err != nil {
		test.Fatalf("Update: %s", err)
	}
}

func TestAnAndroidPhoneRegistersAndAServerReachesIt(test *testing.T) {
	world := newRelayWorld(test, true)
	world.turnOnFirebase(test)
	status, registered := world.post(api.PathPushRegistrations, push.RegisterRequest{DeviceToken: testRegistrationToken, Platform: push.PlatformAndroid})
	sealed, _ := registered["pushRegistration"].(string)
	if status != http.StatusOK || sealed == "" {
		test.Fatalf("registering: %d %v", status, registered)
	}
	status, answer := world.post(api.PathPushNotifications, push.DeliverRequest{PushRegistration: sealed, Delivery: testDelivery})
	if status != http.StatusOK || len(world.firebaseMessages) != 1 || world.appleCalled != 0 {
		test.Fatalf("delivering: %d %v, Firebase called %d times, Apple %d", status, answer, len(world.firebaseMessages), world.appleCalled)
	}
	data := world.firebaseMessages[0]["message"].(map[string]any)["data"].(map[string]any)
	if data["sealed"] != testDelivery.Sealed {
		test.Errorf("Firebase was sent %v", data)
	}

	// An iPhone registered with the same relay still reaches Apple, with no
	// platform said, as a phone that registered before Android did.
	status, registered = world.post(api.PathPushRegistrations, push.RegisterRequest{DeviceToken: testDeviceToken})
	iPhone, _ := registered["pushRegistration"].(string)
	if status != http.StatusOK || iPhone == "" {
		test.Fatalf("registering an iPhone: %d %v", status, registered)
	}
	if status, _ := world.post(api.PathPushNotifications, push.DeliverRequest{PushRegistration: iPhone, Delivery: testDelivery}); status != http.StatusOK || world.appleCalled != 1 {
		test.Errorf("the iPhone's notification: %d, Apple called %d times", status, world.appleCalled)
	}

	// Firebase saying the token is gone comes back as gone, for the sending
	// server to forget the phone; a bad token is gone too.
	world.firebaseStatus = http.StatusNotFound
	world.firebaseAnswer = `{"error":{"status":"NOT_FOUND","details":[{"errorCode":"UNREGISTERED"}]}}`
	if status, answer := world.post(api.PathPushNotifications, push.DeliverRequest{
		PushRegistration: sealed, Delivery: testDelivery,
	}); status != http.StatusGone || answer["errorCode"] != push.ErrorCodeGone {
		test.Errorf("an unregistered phone was answered %d %v", status, answer)
	}
	world.firebaseStatus = http.StatusBadRequest
	world.firebaseAnswer = `{"error":{"status":"INVALID_ARGUMENT","message":"The registration token is not a valid FCM registration token","details":[{"errorCode":"INVALID_ARGUMENT"}]}}`
	if status, answer := world.post(api.PathPushNotifications, push.DeliverRequest{
		PushRegistration: sealed, Delivery: testDelivery,
	}); status != http.StatusGone || answer["errorCode"] != push.ErrorCodeGone {
		test.Errorf("a bad token was answered %d %v", status, answer)
	}

	// Nor does Firebase's failure tell the caller more than that.
	world.firebaseStatus, world.firebaseAnswer = http.StatusServiceUnavailable, ""
	if status, answer := world.post(api.PathPushNotifications, push.DeliverRequest{
		PushRegistration: sealed, Delivery: testDelivery,
	}); status != http.StatusBadGateway || strings.Contains(answer["errorMessage"].(string), testRegistrationToken) {
		test.Errorf("an unreachable Firebase was answered %d %v", status, answer)
	}
}

func TestARelayWithoutFirebaseRefusesAndroid(test *testing.T) {
	world := newRelayWorld(test, true)
	status, answer := world.post(api.PathPushRegistrations, push.RegisterRequest{DeviceToken: testRegistrationToken, Platform: push.PlatformAndroid})
	if status != http.StatusBadRequest || answer["errorCode"] != push.ErrorCodeRefused {
		test.Errorf("an Android phone on a relay with no Firebase was answered %d %v", status, answer)
	}
	if status, _ := world.post(api.PathPushRegistrations, push.RegisterRequest{DeviceToken: testRegistrationToken, Platform: "windows"}); status != http.StatusBadRequest {
		test.Errorf("another platform was answered %d", status)
	}
}

// An operator who serves Android alone seals with the service account, and
// turns away iPhones.
func TestARelayForAndroidAlone(test *testing.T) {
	world := newRelayWorld(test, true)
	world.turnOnFirebase(test)
	if err := world.store.Update(func(configuration *config.Configuration) error {
		configuration.Agent.Push.Relay.APNSKeyID, configuration.Agent.Push.Relay.APNSTeamID, configuration.Agent.Push.Relay.APNSKey = "", "", ""
		return nil
	}); err != nil {
		test.Fatalf("Update: %s", err)
	}
	if status, _ := world.post(api.PathPushRegistrations, push.RegisterRequest{DeviceToken: testRegistrationToken, Platform: push.PlatformAndroid}); status != http.StatusOK {
		test.Errorf("an Android phone was answered %d", status)
	}
	if status, _ := world.post(api.PathPushRegistrations, push.RegisterRequest{DeviceToken: testDeviceToken}); status != http.StatusBadRequest {
		test.Errorf("an iPhone on a relay with no Apple key was answered %d", status)
	}
}
