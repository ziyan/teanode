package push

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// testKey is a fresh P-256 key in the PEM form Apple hands out.
func testKey(test *testing.T) (*ecdsa.PrivateKey, string) {
	test.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		test.Fatalf("GenerateKey: %s", err)
	}
	encoded, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		test.Fatalf("MarshalPKCS8PrivateKey: %s", err)
	}
	return key, string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded}))
}

// fakeApple is APNs as a test sees it: what it was sent, and what it answers.
type fakeApple struct {
	mutex    sync.Mutex
	requests []*http.Request
	bodies   [][]byte
	status   int
	reason   string
}

func (self *fakeApple) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	body, _ := io.ReadAll(request.Body)
	self.mutex.Lock()
	self.requests = append(self.requests, request)
	self.bodies = append(self.bodies, body)
	status, reason := self.status, self.reason
	self.mutex.Unlock()
	if status == 0 {
		status = http.StatusOK
	}
	response.WriteHeader(status)
	if reason != "" {
		_ = json.NewEncoder(response).Encode(map[string]string{"reason": reason})
	}
}

// newFakeApple starts an HTTP/2 server standing in for both of Apple's
// hosts, and a client pointed at it.
func newFakeApple(test *testing.T) (*fakeApple, *APNs, *ecdsa.PrivateKey) {
	test.Helper()
	apple := &fakeApple{}
	server := httptest.NewUnstartedServer(apple)
	server.EnableHTTP2 = true
	server.StartTLS()
	test.Cleanup(server.Close)
	key, keyPEM := testKey(test)
	client, err := NewAPNs("ABCDE12345", "TEAM123456", "com.example.app", keyPEM)
	if err != nil {
		test.Fatalf("NewAPNs: %s", err)
	}
	client.http = server.Client()
	client.productionHost = server.URL
	client.sandboxHost = server.URL + "/sandbox"
	return apple, client, key
}

// testDeviceToken is a device token's shape, made at run time so that no
// literal in the repository looks like a credential to a secret scanner.
var testDeviceToken = strings.Repeat("a1", 32)

func TestANotificationReachesAppleSignedAndAddressed(test *testing.T) {
	apple, client, key := newFakeApple(test)
	err := client.Send(context.Background(), testDeviceToken, false, &Notification{
		PushType: PushTypeAlert, CollapseID: "a-session", Payload: json.RawMessage(`{"aps":{}}`),
	})
	if err != nil {
		test.Fatalf("Send: %s", err)
	}
	request := apple.requests[0]
	if request.URL.Path != "/3/device/"+testDeviceToken || request.ProtoMajor != 2 {
		test.Errorf("sent to %s over HTTP/%d", request.URL.Path, request.ProtoMajor)
	}
	if request.Header.Get("apns-topic") != "com.example.app" || request.Header.Get("apns-push-type") != "alert" ||
		request.Header.Get("apns-priority") != "10" || request.Header.Get("apns-collapse-id") != "a-session" {
		test.Errorf("headers %v", request.Header)
	}

	// The token is an ES256 JWT over the key ID and the team, verifiable
	// with the key's public half.
	token := strings.TrimPrefix(request.Header.Get("Authorization"), "bearer ")
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		test.Fatalf("the token is %q", token)
	}
	header, _ := base64.RawURLEncoding.DecodeString(parts[0])
	claims, _ := base64.RawURLEncoding.DecodeString(parts[1])
	if !strings.Contains(string(header), `"kid":"ABCDE12345"`) || !strings.Contains(string(header), `"alg":"ES256"`) ||
		!strings.Contains(string(claims), `"iss":"TEAM123456"`) {
		test.Errorf("header %s claims %s", header, claims)
	}
	signature, _ := base64.RawURLEncoding.DecodeString(parts[2])
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if len(signature) != 64 || !ecdsa.Verify(&key.PublicKey, digest[:],
		new(big.Int).SetBytes(signature[:32]), new(big.Int).SetBytes(signature[32:])) {
		test.Error("the token's signature does not verify")
	}

	// A Live Activity goes to the topic Apple names for it, and a sandbox
	// device to the sandbox host.
	if err := client.Send(context.Background(), testDeviceToken, true, &Notification{
		PushType: PushTypeLiveActivity, Payload: json.RawMessage(`{"aps":{}}`),
	}); err != nil {
		test.Fatalf("Send: %s", err)
	}
	if request := apple.requests[1]; request.Header.Get("apns-topic") != "com.example.app.push-type.liveactivity" ||
		!strings.HasPrefix(request.URL.Path, "/sandbox/3/device/") {
		test.Errorf("the live activity went to %s as %s", request.URL.Path, request.Header.Get("apns-topic"))
	}
}

func TestWhatAppleAnswersIsWhatTheCallerActsOn(test *testing.T) {
	apple, client, _ := newFakeApple(test)
	send := func() error {
		return client.Send(context.Background(), testDeviceToken, false, &Notification{PushType: PushTypeAlert, Payload: json.RawMessage(`{}`)})
	}
	apple.status = http.StatusGone
	if err := send(); !errors.Is(err, ErrGone) {
		test.Errorf("410 came back as %v", err)
	}
	apple.status, apple.reason = http.StatusBadRequest, "BadDeviceToken"
	if err := send(); !errors.Is(err, ErrGone) {
		test.Errorf("a bad device token came back as %v", err)
	}
	apple.status, apple.reason = http.StatusBadRequest, "TopicDisallowed"
	if err := send(); !errors.Is(err, ErrRefused) {
		test.Errorf("a refused topic came back as %v", err)
	}
	apple.status, apple.reason = http.StatusServiceUnavailable, ""
	if err := send(); err == nil || errors.Is(err, ErrGone) || errors.Is(err, ErrRefused) {
		test.Errorf("503 came back as %v", err)
	}
	if err := client.Send(context.Background(), testDeviceToken, false, &Notification{
		PushType: PushTypeAlert, Payload: json.RawMessage(`"` + strings.Repeat("x", 5000) + `"`),
	}); !errors.Is(err, ErrTooLarge) {
		test.Errorf("an oversized payload came back as %v", err)
	}
}

func TestTheKeyIsReadAsTheFileOrItsBase64(test *testing.T) {
	key, keyPEM := testKey(test)
	lines := strings.Split(strings.TrimSpace(keyPEM), "\n")
	for _, written := range []string{keyPEM, strings.Join(lines[1:len(lines)-1], "")} {
		parsed, err := ParseKey(written)
		if err != nil || !parsed.Equal(key) {
			test.Errorf("ParseKey(%d bytes): %v", len(written), err)
		}
	}
	if _, err := ParseKey("not a key"); err == nil {
		test.Error("text that is not a key was read as one")
	}
}

func TestOnlyTheRelayThatSealedARegistrationCanUseIt(test *testing.T) {
	key, _ := testKey(test)
	otherKey, _ := testKey(test)
	sealer, _ := NewSealer("ABCDE12345", key)
	other, _ := NewSealer("ABCDE12345", otherKey)
	replaced, _ := NewSealer("FGHIJ67890", otherKey)
	now := time.Now()
	sealed, err := sealer.Seal(&Registration{DeviceToken: testDeviceToken, IsSandbox: true, PushType: PushTypeAlert, CreatedAt: now})
	if err != nil {
		test.Fatalf("Seal: %s", err)
	}
	if strings.Contains(sealed, testDeviceToken) {
		test.Error("the device token can be read in the sealed registration")
	}
	opened, err := sealer.Open(sealed, now)
	if err != nil || opened.DeviceToken != testDeviceToken || !opened.IsSandbox {
		test.Errorf("Open: %v %v", opened, err)
	}
	if _, err := other.Open(sealed, now); !errors.Is(err, ErrRefused) {
		test.Errorf("another key under the same identifier came back as %v", err)
	}
	// A relay given a new key says the old registrations are gone, for the
	// servers to clear them and the phones to register again.
	if _, err := replaced.Open(sealed, now); !errors.Is(err, ErrGone) {
		test.Errorf("a registration under a replaced key came back as %v", err)
	}
	if _, err := sealer.Open(sealed, now.Add(registrationLifetime+time.Hour)); !errors.Is(err, ErrGone) {
		test.Errorf("an expired registration came back as %v", err)
	}
}

func TestTheRelayWritesWhatAppleShows(test *testing.T) {
	apple, client, _ := newFakeApple(test)
	relay, err := NewRelay(client)
	if err != nil {
		test.Fatalf("NewRelay: %s", err)
	}
	now := time.Now()
	for _, token := range []string{"", "not-hex", "abc", strings.Repeat("g", 64)} {
		if _, err := relay.Register(token, false, "", now); err == nil {
			test.Errorf("%q was registered", token)
		}
	}
	sealed, err := relay.Register(strings.ToUpper(testDeviceToken), false, "", now)
	if err != nil {
		test.Fatalf("Register: %s", err)
	}
	key := make([]byte, PhoneKeyBytes)
	delivery, err := EncryptContent(key, &Content{NotificationKind: "approval", Title: "Approve?", Body: "Wants to use mail_send.", ThreadID: "a-conversation", CallID: "a-call"})
	if err != nil {
		test.Fatalf("EncryptContent: %s", err)
	}
	if err := relay.Send(context.Background(), sealed, delivery); err != nil {
		test.Fatalf("Send: %s", err)
	}
	var payload struct {
		Aps struct {
			Alert          map[string]string `json:"alert"`
			MutableContent int               `json:"mutable-content"`
			ThreadID       string            `json:"thread-id"`
		} `json:"aps"`
		Sealed string `json:"sealed"`
	}
	_ = json.Unmarshal(apple.bodies[0], &payload)
	if payload.Aps.Alert["body"] != "New message" || payload.Aps.MutableContent != 1 || payload.Sealed != delivery.Sealed {
		test.Errorf("Apple was sent %s", apple.bodies[0])
	}
	if strings.Contains(string(apple.bodies[0]), "a-conversation") || payload.Aps.ThreadID != delivery.ThreadID {
		test.Errorf("the conversation can be read in %s", apple.bodies[0])
	}
	request := apple.requests[0]
	if request.Header.Get("apns-collapse-id") != delivery.CollapseID || request.Header.Get("apns-expiration") == "0" {
		test.Errorf("headers %v", request.Header)
	}

	// A server cannot write words of its own on the lock screen: what is
	// not sealed content is refused.
	for _, forged := range []*Delivery{
		{Sealed: "Your account is locked. Call us."},
		{Sealed: delivery.Sealed, ThreadID: "Read me"},
		{Sealed: delivery.Sealed, ExpiresAt: now.Add(-time.Minute)},
	} {
		if err := relay.Send(context.Background(), sealed, forged); !errors.Is(err, ErrRefused) {
			test.Errorf("%+v came back as %v", forged, err)
		}
	}
	if err := relay.Send(context.Background(), "ABCDE12345.not-ours", delivery); !errors.Is(err, ErrRefused) {
		test.Errorf("a registration the relay did not make came back as %v", err)
	}
}

func TestAnUnreachableAppleDoesNotGiveAwayTheDeviceToken(test *testing.T) {
	_, client, _ := newFakeApple(test)
	client.productionHost = "https://127.0.0.1:1"
	err := client.Send(context.Background(), testDeviceToken, false, &Notification{PushType: PushTypeAlert, Payload: json.RawMessage(`{}`)})
	if err == nil || strings.Contains(err.Error(), testDeviceToken) {
		test.Errorf("Send: %v", err)
	}
}

func TestContentIsReadableOnlyWithThePhonesKey(test *testing.T) {
	key := make([]byte, PhoneKeyBytes)
	_, _ = rand.Read(key)
	delivery, err := EncryptContent(key, &Content{
		NotificationKind: "alert", Title: "A question", Body: "Is the meeting still at four?",
		Buttons: []Button{{Label: "Open", ButtonAction: "open"}},
	})
	if err != nil {
		test.Fatalf("EncryptContent: %s", err)
	}
	if strings.Contains(delivery.Sealed, "meeting") {
		test.Error("the content can be read in what the relay is sent")
	}
	payload := struct{ Sealed string }{delivery.Sealed}
	opened, err := DecryptContent(key, payload.Sealed)
	if err != nil || opened.Body != "Is the meeting still at four?" || opened.Buttons[0].ButtonAction != "open" {
		test.Errorf("DecryptContent: %v %v", opened, err)
	}
	other := make([]byte, PhoneKeyBytes)
	if _, err := DecryptContent(other, payload.Sealed); err == nil {
		test.Error("another key decrypted the content")
	}
}

func TestALongBodyIsShortenedToFit(test *testing.T) {
	key := make([]byte, PhoneKeyBytes)
	delivery, err := EncryptContent(key, &Content{NotificationKind: "alert", Title: "Long", Body: strings.Repeat("十分に長い本文。", 600), ThreadID: "a-conversation"})
	if err != nil {
		test.Fatalf("EncryptContent: %s", err)
	}
	notification, err := delivery.notification(time.Now())
	if err != nil || len(notification.Payload) > payloadBytesMost {
		test.Errorf("the payload is %v %v", notification, err)
	}
	opened, _ := DecryptContent(key, delivery.Sealed)
	if !strings.HasSuffix(opened.Body, "…") || len([]rune(opened.Body)) < 100 {
		test.Errorf("the body came out %d characters", len([]rune(opened.Body)))
	}
}

// A Live Activity is shown as it arrives, readable by Apple and the relay,
// so the relay writes it from a state alone and refuses anything with words.
func TestALiveActivityIsToldAStateAndNothingElse(test *testing.T) {
	apple, client, _ := newFakeApple(test)
	relay, err := NewRelay(client)
	if err != nil {
		test.Fatalf("NewRelay: %s", err)
	}
	now := time.Now()
	if _, err := relay.Register(testDeviceToken, false, "voip", now); !errors.Is(err, ErrRefused) {
		test.Errorf("a voip registration came back as %v", err)
	}
	activity, err := relay.Register(testDeviceToken, false, PushTypeLiveActivity, now)
	if err != nil {
		test.Fatalf("Register: %s", err)
	}
	if err := relay.Send(context.Background(), activity, &Delivery{ActivityState: "waiting"}); err != nil {
		test.Fatalf("Send: %s", err)
	}
	request := apple.requests[0]
	if request.Header.Get("apns-push-type") != "liveactivity" || request.Header.Get("apns-topic") != "com.example.app.push-type.liveactivity" {
		test.Errorf("headers %v", request.Header)
	}
	var payload struct {
		Aps struct {
			Event        string            `json:"event"`
			ContentState map[string]string `json:"content-state"`
			Timestamp    int64             `json:"timestamp"`
		} `json:"aps"`
	}
	_ = json.Unmarshal(apple.bodies[0], &payload)
	if payload.Aps.Event != "update" || payload.Aps.ContentState["activityState"] != "waiting" || payload.Aps.Timestamp == 0 {
		test.Errorf("Apple was sent %s", apple.bodies[0])
	}
	if err := relay.Send(context.Background(), activity, &Delivery{ActivityState: "done", IsActivityEnded: true}); err != nil {
		test.Fatalf("Send: %s", err)
	}
	if !strings.Contains(string(apple.bodies[1]), `"event":"end"`) || !strings.Contains(string(apple.bodies[1]), "dismissal-date") {
		test.Errorf("the end was sent as %s", apple.bodies[1])
	}

	key := make([]byte, PhoneKeyBytes)
	sealed, _ := EncryptContent(key, &Content{NotificationKind: "answer", Title: "Done", Body: "Your flight is booked."})
	for _, forged := range []*Delivery{
		{ActivityState: "Your account is locked. Call us."},
		{ActivityState: "working", Sealed: sealed.Sealed},
		{ActivityState: "working", ThreadID: "abc"},
		sealed,
	} {
		if err := relay.Send(context.Background(), activity, forged); !errors.Is(err, ErrRefused) {
			test.Errorf("%+v to a Live Activity came back as %v", forged, err)
		}
	}
	phone, _ := relay.Register(testDeviceToken, false, "", now)
	if err := relay.Send(context.Background(), phone, &Delivery{ActivityState: "working"}); !errors.Is(err, ErrRefused) {
		test.Errorf("a state sent to the phone came back as %v", err)
	}
}

// iOS shows the Live Activity update with the latest aps.timestamp. The
// relay's own clock would order a turn's states by when each reached it, so
// it writes when the server says the state changed, and its own clock only
// when that is missing or not believable.
func TestALiveActivityIsStampedWithWhenItsStateChanged(test *testing.T) {
	apple, client, _ := newFakeApple(test)
	relay, err := NewRelay(client)
	if err != nil {
		test.Fatalf("NewRelay: %s", err)
	}
	now := time.Now()
	activity, err := relay.Register(testDeviceToken, false, PushTypeLiveActivity, now)
	if err != nil {
		test.Fatalf("Register: %s", err)
	}
	registration, err := relay.Open(activity, now)
	if err != nil {
		test.Fatalf("Open: %s", err)
	}
	earlier := now.Add(-20 * time.Second)
	for index, stamp := range []struct {
		name      string
		changedAt time.Time
		want      int64
	}{
		{"a moment ago", earlier, earlier.Unix()},
		{"not said", time.Time{}, now.Unix()},
		{"in the future", now.Add(time.Hour), now.Unix()},
		{"long ago", now.Add(-time.Hour), now.Unix()},
	} {
		if err := relay.DeliverTo(context.Background(), registration, &Delivery{ActivityState: "working", ActivityChangedAt: stamp.changedAt}, now); err != nil {
			test.Fatalf("%s: DeliverTo: %s", stamp.name, err)
		}
		var payload struct {
			Aps struct {
				Timestamp int64 `json:"timestamp"`
			} `json:"aps"`
		}
		_ = json.Unmarshal(apple.bodies[index], &payload)
		if payload.Aps.Timestamp != stamp.want {
			test.Errorf("%s: stamped %d, want %d", stamp.name, payload.Aps.Timestamp, stamp.want)
		}
	}
	// The phone's own alerts carry no state, and so no time of one.
	phone, _ := relay.Register(testDeviceToken, false, "", now)
	if err := relay.Send(context.Background(), phone, &Delivery{ActivityChangedAt: now}); !errors.Is(err, ErrRefused) {
		test.Errorf("a state's time sent to the phone came back as %v", err)
	}
}
