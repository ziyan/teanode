package apipush

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/push"
)

// testDeviceToken is a device token's shape, made at run time so that no
// literal in the repository looks like a credential to a secret scanner.
var testDeviceToken = strings.Repeat("a1", 32)

// testDelivery is sealed content as a server sends it.
var testDelivery = &push.Delivery{Sealed: "c2VhbGVkIGZvciB0aGUgcGhvbmU="}

// relayWorld is a server with the relay component, an Apple standing in, and
// how many notifications reached it.
type relayWorld struct {
	router      *mux.Router
	store       config.Store
	appleCalled int
	appleStatus int
}

func newRelayWorld(test *testing.T, isRelayOn bool) *relayWorld {
	test.Helper()
	world := &relayWorld{}
	apple := httptest.NewUnstartedServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		world.appleCalled++
		if world.appleStatus != 0 {
			response.WriteHeader(world.appleStatus)
		}
	}))
	apple.EnableHTTP2 = true
	apple.StartTLS()
	test.Cleanup(apple.Close)

	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	encoded, _ := x509.MarshalPKCS8PrivateKey(key)
	directory := test.TempDir()
	_ = os.WriteFile(filepath.Join(directory, "teanode1.key"), []byte("not a real key"), 0o600)
	configuration := config.Example()
	configuration.Server.DataDirectory = directory
	configuration.Agent.Push.Relay = config.AgentPushRelay{
		Enabled: isRelayOn, APNSKeyID: "ABCDE12345", APNSTeamID: "TEAM123456",
		APNSKey: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded})),
	}
	world.store = config.NewMemoryStore(configuration)
	test.Cleanup(func() { _ = world.store.Close() })
	if err := config.EnsureSecrets(world.store); err != nil {
		test.Fatalf("EnsureSecrets: %s", err)
	}
	component, _ := New(world.store)
	relay := component.(*relayComponent)
	relay.buildAPNs = func(keyID, teamID, topic, keyPEM string) (*push.APNs, error) {
		client, err := push.NewAPNs(keyID, teamID, topic, keyPEM)
		if err == nil {
			client.UseHosts(apple.Client(), apple.URL, apple.URL)
		}
		return client, err
	}
	world.router = mux.NewRouter()
	_ = relay.AddRoutes(world.router)
	return world
}

func (self *relayWorld) post(path string, body any) (int, map[string]any) {
	return self.postFrom("192.0.2.20:5000", path, body)
}

func (self *relayWorld) postFrom(remoteAddress, path string, body any) (int, map[string]any) {
	encoded, _ := json.Marshal(body)
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(encoded))
	request.RemoteAddr = remoteAddress
	response := httptest.NewRecorder()
	self.router.ServeHTTP(response, request)
	answer := map[string]any{}
	_ = json.Unmarshal(response.Body.Bytes(), &answer)
	return response.Code, answer
}

func TestAPhoneRegistersAndAServerReachesIt(test *testing.T) {
	world := newRelayWorld(test, true)
	status, registered := world.post(api.PathPushRegistrations, push.RegisterRequest{DeviceToken: testDeviceToken})
	sealed, _ := registered["pushRegistration"].(string)
	if status != http.StatusOK || sealed == "" {
		test.Fatalf("registering: %d %v", status, registered)
	}
	status, answer := world.post(api.PathPushNotifications, push.DeliverRequest{PushRegistration: sealed, Delivery: testDelivery})
	if status != http.StatusOK || world.appleCalled != 1 {
		test.Errorf("delivering: %d %v, Apple called %d times", status, answer, world.appleCalled)
	}

	// Apple saying the phone is gone comes back as gone, for the sending
	// server to forget the phone.
	world.appleStatus = http.StatusGone
	if status, answer := world.post(api.PathPushNotifications, push.DeliverRequest{
		PushRegistration: sealed, Delivery: testDelivery,
	}); status != http.StatusGone || answer["errorCode"] != push.ErrorCodeGone {
		test.Errorf("a gone phone was answered %d %v", status, answer)
	}

	// A registration this relay did not make reaches nobody.
	if status, _ := world.post(api.PathPushNotifications, push.DeliverRequest{
		PushRegistration: "ABCDE12345.somebody-else's", Delivery: testDelivery,
	}); status != http.StatusBadRequest {
		test.Errorf("a foreign registration was answered %d", status)
	}

	// Nor does Apple's failure tell the caller more than that.
	world.appleStatus = http.StatusServiceUnavailable
	if status, answer := world.post(api.PathPushNotifications, push.DeliverRequest{
		PushRegistration: sealed, Delivery: testDelivery,
	}); status != http.StatusBadGateway || strings.Contains(answer["errorMessage"].(string), testDeviceToken) {
		test.Errorf("an unreachable Apple was answered %d %v", status, answer)
	}
}

func TestARelayThatIsOffAnswersNothing(test *testing.T) {
	world := newRelayWorld(test, false)
	if status, _ := world.post(api.PathPushRegistrations, push.RegisterRequest{DeviceToken: testDeviceToken}); status != http.StatusNotFound {
		test.Errorf("a relay that is off answered %d", status)
	}
}

func TestOnePhoneCannotBeFlooded(test *testing.T) {
	world := newRelayWorld(test, true)
	_, registered := world.post(api.PathPushRegistrations, push.RegisterRequest{DeviceToken: testDeviceToken})
	sealed := registered["pushRegistration"].(string)
	refused := 0
	for attempt := 0; attempt < 200; attempt++ {
		// Base64 skips line breaks, so the one registration can be spelled
		// many ways; the limit is the phone's, whatever the spelling.
		spelled := sealed[:len(sealed)-4] + strings.Repeat("\n", attempt%7) + sealed[len(sealed)-4:]
		status, _ := world.post(api.PathPushNotifications, push.DeliverRequest{PushRegistration: spelled, Delivery: testDelivery})
		if status == http.StatusTooManyRequests {
			refused++
		}
	}
	if refused == 0 {
		test.Error("two hundred notifications to one phone at once were all passed on")
	}
}

func TestOneNetworkIsOneCaller(test *testing.T) {
	world := newRelayWorld(test, true)
	refused := 0
	for attempt := 0; attempt < 40; attempt++ {
		address := fmt.Sprintf("[2001:db8:1:2::%x]:5000", attempt+1)
		if status, _ := world.postFrom(address, api.PathPushRegistrations, push.RegisterRequest{DeviceToken: testDeviceToken}); status == http.StatusTooManyRequests {
			refused++
		}
	}
	if refused == 0 {
		test.Error("forty addresses in one /64 each registered as a caller of their own")
	}
}
