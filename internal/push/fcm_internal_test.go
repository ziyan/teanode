package push

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// testRegistrationToken is an FCM registration token's shape, made at run time so
// that no literal in the repository looks like a credential to a secret
// scanner.
var testRegistrationToken = "f" + strings.Repeat("Ab1_-", 30) + ":" + strings.Repeat("x", 20)

// fakeGoogle is Google as a test sees it: the token endpoint that trades an
// assertion for an access token, and FCM, with what each was sent and what
// FCM answers.
type fakeGoogle struct {
	mutex         sync.Mutex
	tokenForms    []url.Values
	messages      []map[string]any
	messagePaths  []string
	authorization []string
	status        int
	answer        string
	tokenCount    int
}

func (self *fakeGoogle) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	body, _ := io.ReadAll(request.Body)
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if request.URL.Path == "/token" {
		form, _ := url.ParseQuery(string(body))
		self.tokenForms = append(self.tokenForms, form)
		self.tokenCount++
		_ = json.NewEncoder(response).Encode(map[string]any{"access_token": "access-" + string(rune('0'+self.tokenCount)), "expires_in": 3600})
		return
	}
	var sent map[string]any
	_ = json.Unmarshal(body, &sent)
	self.messages = append(self.messages, sent)
	self.messagePaths = append(self.messagePaths, request.URL.Path)
	self.authorization = append(self.authorization, request.Header.Get("Authorization"))
	status := self.status
	if status == 0 {
		status = http.StatusOK
	}
	response.WriteHeader(status)
	_, _ = io.WriteString(response, self.answer)
}

// newFakeGoogle starts a server standing in for Google's token endpoint and
// FCM, and a client for a service account pointed at it.
func newFakeGoogle(test *testing.T) (*fakeGoogle, *FCM, *rsa.PrivateKey) {
	test.Helper()
	google := &fakeGoogle{}
	server := httptest.NewServer(google)
	test.Cleanup(server.Close)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		test.Fatalf("GenerateKey: %s", err)
	}
	client, err := NewFCM(testServiceAccount(test, key, server.URL+"/token"))
	if err != nil {
		test.Fatalf("NewFCM: %s", err)
	}
	client.UseHost(server.Client(), server.URL)
	return google, client, key
}

// testServiceAccount is a service account's JSON file around key.
func testServiceAccount(test *testing.T, key *rsa.PrivateKey, tokenURI string) string {
	test.Helper()
	encoded, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		test.Fatalf("MarshalPKCS8PrivateKey: %s", err)
	}
	file, err := json.Marshal(map[string]string{
		"type": "service_account", "project_id": "example-project", "private_key_id": "0123456789abcdef",
		"private_key":  string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded})),
		"client_email": "relay@example-project.iam.example.net", "token_uri": tokenURI,
	})
	if err != nil {
		test.Fatalf("Marshal: %s", err)
	}
	return string(file)
}

func TestAMessageReachesFirebaseAuthorizedAndAddressed(test *testing.T) {
	google, client, key := newFakeGoogle(test)
	err := client.Send(context.Background(), testRegistrationToken, &FCMMessage{
		Data:           map[string]string{FCMDataSealed: "c2VhbGVk", FCMDataPushKind: FCMPushKindNotification},
		IsHighPriority: true, CollapseKey: "a1b2", TimeToLive: 90 * time.Second,
	})
	if err != nil {
		test.Fatalf("Send: %s", err)
	}
	if google.messagePaths[0] != "/v1/projects/example-project/messages:send" || google.authorization[0] != "Bearer access-1" {
		test.Errorf("sent to %s as %q", google.messagePaths[0], google.authorization[0])
	}
	message := google.messages[0]["message"].(map[string]any)
	android := message["android"].(map[string]any)
	if message["token"] != testRegistrationToken || android["priority"] != "HIGH" || android["ttl"] != "90s" || android["collapse_key"] != "a1b2" {
		test.Errorf("message %v", message)
	}
	if _, hasNotification := message["notification"]; hasNotification {
		test.Error("the message carries a notification the system would draw: it must be data only, for the app to draw after decrypting")
	}
	data := message["data"].(map[string]any)
	if len(data) != 2 || data["sealed"] != "c2VhbGVk" || data["pushKind"] != "notification" {
		test.Errorf("data %v", data)
	}

	// The access token is traded for with an RS256 assertion over the
	// service account, verifiable with the key's public half.
	form := google.tokenForms[0]
	if form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
		test.Errorf("grant %q", form.Get("grant_type"))
	}
	parts := strings.Split(form.Get("assertion"), ".")
	if len(parts) != 3 {
		test.Fatalf("the assertion is %q", form.Get("assertion"))
	}
	header, _ := base64.RawURLEncoding.DecodeString(parts[0])
	claims, _ := base64.RawURLEncoding.DecodeString(parts[1])
	if !strings.Contains(string(header), `"alg":"RS256"`) || !strings.Contains(string(header), `"kid":"0123456789abcdef"`) ||
		!strings.Contains(string(claims), `"iss":"relay@example-project.iam.example.net"`) ||
		!strings.Contains(string(claims), `"scope":"https://www.googleapis.com/auth/firebase.messaging"`) {
		test.Errorf("header %s claims %s", header, claims)
	}
	signature, _ := base64.RawURLEncoding.DecodeString(parts[2])
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], signature); err != nil {
		test.Errorf("the assertion's signature does not verify: %s", err)
	}

	// A second send uses the same access token, and an ordinary message is
	// not marked high priority.
	if err := client.Send(context.Background(), testRegistrationToken, &FCMMessage{Data: map[string]string{FCMDataSealed: "c2VhbGVk"}}); err != nil {
		test.Fatalf("Send: %s", err)
	}
	if google.tokenCount != 1 || google.authorization[1] != "Bearer access-1" {
		test.Errorf("the access token was traded for %d times", google.tokenCount)
	}
	if priority := google.messages[1]["message"].(map[string]any)["android"].(map[string]any)["priority"]; priority != "NORMAL" {
		test.Errorf("priority %v", priority)
	}
}

func TestAnAccessTokenAboutToExpireIsTradedForAgain(test *testing.T) {
	google, client, _ := newFakeGoogle(test)
	now := time.Now()
	for _, at := range []time.Time{now, now.Add(30 * time.Minute), now.Add(56 * time.Minute)} {
		if _, err := client.accessTokenAt(context.Background(), at); err != nil {
			test.Fatalf("accessTokenAt: %s", err)
		}
	}
	if google.tokenCount != 2 {
		test.Errorf("an hour long token was traded for %d times in 56 minutes, want 2", google.tokenCount)
	}
}

func TestWhatFirebaseAnswersIsWhatTheCallerActsOn(test *testing.T) {
	google, client, _ := newFakeGoogle(test)
	send := func(status int, answer string) error {
		google.status, google.answer = status, answer
		return client.Send(context.Background(), testRegistrationToken, &FCMMessage{Data: map[string]string{FCMDataSealed: "c2VhbGVk"}})
	}
	errorBody := func(status, message, errorCode string) string {
		details := ""
		if errorCode != "" {
			details = `,"details":[{"@type":"type.googleapis.com/google.firebase.fcm.v1.FcmError","errorCode":"` + errorCode + `"}]`
		}
		return `{"error":{"code":0,"message":"` + message + `","status":"` + status + `"` + details + `}}`
	}
	if err := send(http.StatusNotFound, errorBody("NOT_FOUND", "Requested entity was not found.", "UNREGISTERED")); !errors.Is(err, ErrGone) {
		test.Errorf("an unregistered token came back as %v", err)
	}
	if err := send(http.StatusForbidden, errorBody("PERMISSION_DENIED", "SenderId mismatch", "SENDER_ID_MISMATCH")); !errors.Is(err, ErrGone) {
		test.Errorf("a token of another project came back as %v", err)
	}
	if err := send(http.StatusBadRequest, errorBody("INVALID_ARGUMENT", "The registration token is not a valid FCM registration token", "INVALID_ARGUMENT")); !errors.Is(err, ErrGone) {
		test.Errorf("a bad token came back as %v", err)
	}
	if err := send(http.StatusBadRequest, errorBody("INVALID_ARGUMENT", "Invalid value at 'message.data'", "INVALID_ARGUMENT")); !errors.Is(err, ErrRefused) || errors.Is(err, ErrGone) {
		test.Errorf("a bad message came back as %v", err)
	}
	if err := send(http.StatusServiceUnavailable, errorBody("UNAVAILABLE", "try later", "UNAVAILABLE")); err == nil || errors.Is(err, ErrGone) || errors.Is(err, ErrRefused) {
		test.Errorf("503 came back as %v", err)
	}
	if err := send(http.StatusTooManyRequests, errorBody("RESOURCE_EXHAUSTED", "slow down", "QUOTA_EXCEEDED")); err == nil || errors.Is(err, ErrGone) || errors.Is(err, ErrRefused) {
		test.Errorf("429 came back as %v", err)
	}
	if err := send(http.StatusInternalServerError, ""); err == nil || errors.Is(err, ErrGone) || errors.Is(err, ErrRefused) {
		test.Errorf("500 came back as %v", err)
	}
	// A credential Google stopped taking is traded for again, and is not the
	// phone's fault.
	tokensBefore := google.tokenCount
	if err := send(http.StatusUnauthorized, errorBody("UNAUTHENTICATED", "expired", "")); err == nil || errors.Is(err, ErrGone) || errors.Is(err, ErrRefused) {
		test.Errorf("401 came back as %v", err)
	}
	if err := send(http.StatusOK, ""); err != nil || google.tokenCount != tokensBefore+1 {
		test.Errorf("after 401: %v, access tokens traded for %d times, want %d", err, google.tokenCount, tokensBefore+1)
	}
	if err := client.Send(context.Background(), testRegistrationToken, &FCMMessage{
		Data: map[string]string{FCMDataSealed: strings.Repeat("x", 5000)},
	}); !errors.Is(err, ErrTooLarge) {
		test.Errorf("an oversized message came back as %v", err)
	}
}

func TestTheServiceAccountIsChecked(test *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		test.Fatalf("GenerateKey: %s", err)
	}
	good := testServiceAccount(test, key, "")
	if client, err := NewFCM(good); err != nil || client.account.TokenURI != fcmTokenURI {
		test.Errorf("a service account with no token_uri: %v", err)
	}
	for name, file := range map[string]string{
		"not JSON":      "not a file",
		"another type":  strings.Replace(good, `"service_account"`, `"authorized_user"`, 1),
		"no project":    strings.Replace(good, `"project_id":"example-project"`, `"project_id":""`, 1),
		"no key ID":     strings.Replace(good, `"private_key_id":"0123456789abcdef"`, `"private_key_id":""`, 1),
		"a broken key":  strings.Replace(good, "MII", "XXX", 1),
		"no key at all": strings.Replace(good, `"private_key":`, `"private_key_x":`, 1),
	} {
		if _, err := NewFCM(file); err == nil {
			test.Errorf("%s was accepted", name)
		}
	}
}

// newAndroidRelay is a relay for Android alone.
func newAndroidRelay(test *testing.T) (*fakeGoogle, *Relay) {
	test.Helper()
	google, client, _ := newFakeGoogle(test)
	relay, err := NewRelay(nil, client)
	if err != nil {
		test.Fatalf("NewRelay: %s", err)
	}
	return google, relay
}

func TestAnAndroidPhoneRegistersAndIsSentDataOnly(test *testing.T) {
	google, relay := newAndroidRelay(test)
	now := time.Now()
	alert, err := relay.Register(testRegistrationToken, false, "", PlatformAndroid, now)
	if err != nil {
		test.Fatalf("Register: %s", err)
	}
	opened, err := relay.Open(alert, now)
	if err != nil || opened.Platform != PlatformAndroid || opened.DeviceToken != testRegistrationToken || opened.PushType != PushTypeAlert {
		test.Fatalf("Open: %+v %v", opened, err)
	}
	if strings.Contains(alert, testRegistrationToken) {
		test.Error("the registration shows the token")
	}

	key := make([]byte, PhoneKeyBytes)
	delivery, err := EncryptContent(key, &Content{NotificationKind: "chat", Title: "Sam", Body: "Lunch at noon?", ThreadID: "a-chat", CallID: "a-card"})
	if err != nil {
		test.Fatalf("EncryptContent: %s", err)
	}
	if err := relay.Send(context.Background(), alert, delivery); err != nil {
		test.Fatalf("Send: %s", err)
	}
	message := google.messages[0]["message"].(map[string]any)
	android := message["android"].(map[string]any)
	data := message["data"].(map[string]any)
	if android["priority"] != "HIGH" || android["collapse_key"] != delivery.CollapseID || android["ttl"] != "3600s" {
		test.Errorf("android %v", android)
	}
	if data["sealed"] != delivery.Sealed || data["pushKind"] != "notification" || data["threadId"] != delivery.ThreadID || len(data) != 3 {
		test.Errorf("data %v", data)
	}
	body, _ := json.Marshal(google.messages[0])
	if strings.Contains(string(body), "Sam") || strings.Contains(string(body), "Lunch") || strings.Contains(string(body), "a-chat") {
		test.Errorf("Firebase can read the notification in %s", body)
	}
	if decrypted, err := DecryptContent(key, data["sealed"].(string)); err != nil || decrypted.Body != "Lunch at noon?" {
		test.Errorf("the phone reads %+v %v", decrypted, err)
	}
}

func TestAnAndroidRingIsAHighPriorityCallThatIsNotCollapsed(test *testing.T) {
	google, relay := newAndroidRelay(test)
	now := time.Now()
	voip, err := relay.Register(testRegistrationToken, false, PushTypeVoIP, PlatformAndroid, now)
	if err != nil {
		test.Fatalf("Register: %s", err)
	}
	key := make([]byte, PhoneKeyBytes)
	delivery, err := EncryptContent(key, &Content{NotificationKind: "call", Title: "Sam", Body: "is calling", CallID: "a-call", ThreadID: "a-chat"})
	if err != nil {
		test.Fatalf("EncryptContent: %s", err)
	}
	if err := relay.Send(context.Background(), voip, delivery); err != nil {
		test.Fatalf("Send: %s", err)
	}
	message := google.messages[0]["message"].(map[string]any)
	android := message["android"].(map[string]any)
	data := message["data"].(map[string]any)
	if android["priority"] != "HIGH" || android["ttl"] != "60s" || android["collapse_key"] != nil {
		test.Errorf("android %v", android)
	}
	if len(data) != 2 || data["pushKind"] != "call" || data["sealed"] != delivery.Sealed {
		test.Errorf("data %v", data)
	}

	// A server may ask for a shorter ring, never one that outlasts a call.
	later := *delivery
	later.ExpiresAt = now.Add(time.Hour)
	if err := relay.Send(context.Background(), voip, &later); err != nil {
		test.Fatalf("Send: %s", err)
	}
	if ttl := google.messages[1]["message"].(map[string]any)["android"].(map[string]any)["ttl"]; ttl != "120s" && ttl != "119s" {
		test.Errorf("a ring asked to last an hour was given %v", ttl)
	}
	// Only sealed content rings a phone.
	for _, forged := range []*Delivery{{Sealed: "Your account is locked."}, {ActivityState: "working"}, {Sealed: delivery.Sealed, ExpiresAt: now.Add(-time.Minute)}} {
		if err := relay.Send(context.Background(), voip, forged); !errors.Is(err, ErrRefused) {
			test.Errorf("%+v to an Android ring came back as %v", forged, err)
		}
	}
	if len(google.messages) != 2 {
		test.Errorf("%d messages reached Firebase, want only the 2 rings", len(google.messages))
	}
}

func TestAnAndroidRegistrationIsCheckedAndKeptApartFromTheIPhones(test *testing.T) {
	_, androidRelay := newAndroidRelay(test)
	now := time.Now()
	if _, err := androidRelay.Register(testRegistrationToken, false, PushTypeLiveActivity, PlatformAndroid, now); !errors.Is(err, ErrRefused) {
		test.Errorf("a Live Activity on Android came back as %v", err)
	}
	for name, token := range map[string]string{"empty": "", "short": "abc", "spaces": strings.Repeat("a b ", 30), "a URL": "https://example.com/" + strings.Repeat("a", 60)} {
		if _, err := androidRelay.Register(token, false, "", PlatformAndroid, now); !errors.Is(err, ErrRefused) {
			test.Errorf("%s token came back as %v", name, err)
		}
	}
	if _, err := androidRelay.Register(testRegistrationToken, false, "", "windows", now); !errors.Is(err, ErrRefused) {
		test.Errorf("another platform came back as %v", err)
	}
	// An FCM token is not an APNs one, and a relay for Android alone has no
	// APNs.
	if _, err := androidRelay.Register(testDeviceToken, false, "", "", now); !errors.Is(err, ErrRefused) {
		test.Errorf("an iPhone on a relay with no APNs came back as %v", err)
	}

	// A relay for iPhones alone refuses Android, and a registration made for
	// one platform does not open on a relay without it.
	_, client, _ := newFakeApple(test)
	appleRelay, err := NewRelay(client, nil)
	if err != nil {
		test.Fatalf("NewRelay: %s", err)
	}
	if _, err := appleRelay.Register(testRegistrationToken, false, "", PlatformAndroid, now); !errors.Is(err, ErrRefused) {
		test.Errorf("an Android phone on a relay with no Firebase came back as %v", err)
	}
	if _, err := NewRelay(nil, nil); err == nil {
		test.Error("a relay with nothing to send through was built")
	}
}

// A relay that gains Android keeps every registration its iPhones hold, and
// serves both.
func TestARelayForBothPlatformsOpensWhatItSealedBeforeAndroid(test *testing.T) {
	apple, appleClient, _ := newFakeApple(test)
	before, err := NewRelay(appleClient, nil)
	if err != nil {
		test.Fatalf("NewRelay: %s", err)
	}
	now := time.Now()
	iPhone, err := before.Register(testDeviceToken, false, "", "", now)
	if err != nil {
		test.Fatalf("Register: %s", err)
	}

	google, googleClient, _ := newFakeGoogle(test)
	both, err := NewRelay(appleClient, googleClient)
	if err != nil {
		test.Fatalf("NewRelay: %s", err)
	}
	opened, err := both.Open(iPhone, now)
	if err != nil || opened.ResolvedPlatform() != PlatformIOS {
		test.Fatalf("an iPhone registration sealed before Android: %+v %v", opened, err)
	}
	android, err := both.Register(testRegistrationToken, false, "", PlatformAndroid, now)
	if err != nil {
		test.Fatalf("Register: %s", err)
	}
	delivery := &Delivery{Sealed: "c2VhbGVk"}
	if err := both.Send(context.Background(), iPhone, delivery); err != nil || len(apple.requests) != 1 || len(google.messages) != 0 {
		test.Errorf("the iPhone's notification: %v, %d to Apple, %d to Firebase", err, len(apple.requests), len(google.messages))
	}
	if err := both.Send(context.Background(), android, delivery); err != nil || len(apple.requests) != 1 || len(google.messages) != 1 {
		test.Errorf("the Android phone's notification: %v, %d to Apple, %d to Firebase", err, len(apple.requests), len(google.messages))
	}
	// Firebase saying the token is gone is the relay saying so.
	google.status, google.answer = http.StatusNotFound, `{"error":{"status":"NOT_FOUND","details":[{"errorCode":"UNREGISTERED"}]}}`
	if err := both.Send(context.Background(), android, delivery); !errors.Is(err, ErrGone) {
		test.Errorf("an unregistered Android phone came back as %v", err)
	}
}
