package autoacme

import (
	"context"
	"crypto/ecdsa"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/acme"
)

// sharedState stands in for what several instances share: the configuration
// the certificates and the account key are saved to, the table of challenge
// answers, and the lock that lets one instance order at a time.
type sharedState struct {
	mutex               sync.Mutex
	certificates        map[string][2]string
	accountKey          string
	accountKeySaveCount int
	isOrderLockHeld     bool
	challenges          *memoryChallengeStore
}

func newSharedState() *sharedState {
	return &sharedState{
		certificates: map[string][2]string{},
		challenges:   newMemoryChallengeStore(),
	}
}

// instance builds a manager the way a server instance does, over the shared
// state, with ordering replaced by orderCertificate and the renewal loop not
// started: a test calls spinOnce for a tick.
func (self *sharedState) instance(t *testing.T, challenge string, orderCertificate func(context.Context, []string, string) (*tls.Certificate, error)) *manager {
	t.Helper()

	manager, err := newManager(&Settings{
		Challenge:      challenge,
		ACMEEmail:      "you@example.com",
		ChallengeStore: self.challenges,
		Certificates:   []CertificateRequest{{Key: "", Hosts: []string{"mail.example.com"}}},
		LoadCertificate: func(key string) (string, string, error) {
			self.mutex.Lock()
			defer self.mutex.Unlock()
			stored := self.certificates[key]
			return stored[0], stored[1], nil
		},
		SaveCertificate: func(key, certificate, privateKey string) error {
			self.mutex.Lock()
			defer self.mutex.Unlock()
			self.certificates[key] = [2]string{certificate, privateKey}
			return nil
		},
		LoadAccountKey: func() (string, error) {
			self.mutex.Lock()
			defer self.mutex.Unlock()
			return self.accountKey, nil
		},
		SaveAccountKey: func(accountKey string) error {
			self.mutex.Lock()
			defer self.mutex.Unlock()
			self.accountKey = accountKey
			self.accountKeySaveCount++
			return nil
		},
		WhileOrdering: func(ctx context.Context, work func(context.Context) error) (bool, error) {
			self.mutex.Lock()
			if self.isOrderLockHeld {
				self.mutex.Unlock()
				return false, nil
			}
			self.isOrderLockHeld = true
			self.mutex.Unlock()
			defer func() {
				self.mutex.Lock()
				self.isOrderLockHeld = false
				self.mutex.Unlock()
			}()
			return true, work(ctx)
		},
	})
	if err != nil {
		t.Fatalf("failed to build an instance: %s", err)
	}
	if orderCertificate != nil {
		manager.obtain = orderCertificate
	}
	return manager
}

// The authority's request can reach any instance: a challenge presented by
// one is answered by the other, and is gone for both once cleaned up.
func TestHTTP01ChallengePresentedByOneIsAnsweredByTheOther(t *testing.T) {
	shared := newSharedState()
	ordering := shared.instance(t, "http-01", nil)
	answering := shared.instance(t, "http-01", nil)

	challenges := challengesFor("mail.example.com")
	if err := ordering.httpSolver.Present(t.Context(), &fakeACMEClient{}, challenges); err != nil {
		t.Fatalf("failed to present: %s", err)
	}

	recorder := httptest.NewRecorder()
	answering.HTTPHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, challengePath+"token-mail.example.com", nil))
	if recorder.Code != http.StatusOK || recorder.Body.String() != "response-for-token-mail.example.com" {
		t.Errorf("the other instance answered %d %q, want the response", recorder.Code, recorder.Body.String())
	}

	if err := ordering.httpSolver.CleanUp(t.Context(), challenges); err != nil {
		t.Fatalf("failed to clean up: %s", err)
	}
	recorder = httptest.NewRecorder()
	answering.HTTPHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, challengePath+"token-mail.example.com", nil))
	if recorder.Code != http.StatusNotFound {
		t.Errorf("after cleanup the other instance answered %d, want 404", recorder.Code)
	}
}

func TestTLSALPN01ChallengePresentedByOneIsAnsweredByTheOther(t *testing.T) {
	shared := newSharedState()
	ordering := shared.instance(t, "tls-alpn-01", nil)
	answering := shared.instance(t, "tls-alpn-01", nil)

	challenges := challengesFor("mail.example.com")
	if err := ordering.alpnSolver.Present(t.Context(), &fakeACMEClient{}, challenges); err != nil {
		t.Fatalf("failed to present: %s", err)
	}

	hello := &tls.ClientHelloInfo{ServerName: "mail.example.com", SupportedProtos: []string{acme.ALPNProto}}
	certificate, err := answering.GetCertificate(hello)
	if err != nil || len(certificate.Certificate) != 1 {
		t.Fatalf("the other instance did not answer the handshake: %v", err)
	}

	if err := ordering.alpnSolver.CleanUp(t.Context(), challenges); err != nil {
		t.Fatalf("failed to clean up: %s", err)
	}
	if _, err := answering.GetCertificate(hello); err == nil {
		t.Error("after cleanup the other instance still answered the handshake")
	}
}

// A certificate one instance saved is served by the other after its next
// tick, with no order of its own and no restart.
func TestCertificateSavedByOneIsServedByTheOtherAfterItsNextTick(t *testing.T) {
	shared := newSharedState()
	var otherOrderCount atomic.Int32
	ordering := shared.instance(t, "http-01", func(context.Context, []string, string) (*tls.Certificate, error) {
		return selfSigned(t, []string{"mail.example.com"}, 90*24*time.Hour), nil
	})
	other := shared.instance(t, "http-01", func(context.Context, []string, string) (*tls.Certificate, error) {
		otherOrderCount.Add(1)
		return nil, ErrOrderFailed
	})

	hello := &tls.ClientHelloInfo{ServerName: "mail.example.com"}
	if err := ordering.spinOnce(t.Context()); err != nil {
		t.Fatalf("the ordering instance's tick failed: %s", err)
	}
	if _, err := ordering.GetCertificate(hello); err != nil {
		t.Fatalf("the ordering instance does not serve what it obtained: %s", err)
	}
	if _, err := other.GetCertificate(hello); err == nil {
		t.Fatal("the other instance served a certificate before its tick")
	}

	if err := other.spinOnce(t.Context()); err != nil {
		t.Fatalf("the other instance's tick failed: %s", err)
	}
	if _, err := other.GetCertificate(hello); err != nil {
		t.Errorf("the other instance does not serve the saved certificate after its tick: %s", err)
	}
	if otherOrderCount.Load() != 0 {
		t.Error("the other instance ordered although a valid certificate was saved")
	}
}

// A stored certificate that expires sooner than the one held is not adopted,
// and one that is for other names is not either.
func TestOlderOrForeignStoredCertificateIsNotAdopted(t *testing.T) {
	shared := newSharedState()
	instance := shared.instance(t, "http-01", func(context.Context, []string, string) (*tls.Certificate, error) {
		return nil, ErrOrderFailed
	})
	held := selfSigned(t, []string{"mail.example.com"}, 200*24*time.Hour)
	instance.certificates[0].certificate = held

	for name, stored := range map[string]*tls.Certificate{
		"expires sooner":  selfSigned(t, []string{"mail.example.com"}, 100*24*time.Hour),
		"for other names": selfSigned(t, []string{"mx.other.test"}, 300*24*time.Hour),
	} {
		encodedCertificate, encodedKey, err := encodeCertificate(stored)
		if err != nil {
			t.Fatal(err)
		}
		shared.certificates[""] = [2]string{encodedCertificate, encodedKey}
		if err := instance.spinOnce(t.Context()); err != nil {
			t.Fatalf("%s: tick failed: %s", name, err)
		}
		if instance.certificates[0].certificate != held {
			t.Errorf("%s: the stored certificate replaced the one held", name)
		}
	}
}

// Both instances due at once: one orders, the other does not, and afterwards
// the other serves what the first obtained without ordering.
func TestOnlyOneInstanceOrdersWhenBothAreDue(t *testing.T) {
	shared := newSharedState()
	var orderCount atomic.Int32
	orderStarted := make(chan struct{}, 2)
	releaseOrder := make(chan struct{})
	order := func(context.Context, []string, string) (*tls.Certificate, error) {
		orderCount.Add(1)
		orderStarted <- struct{}{}
		<-releaseOrder
		return selfSigned(t, []string{"mail.example.com"}, 90*24*time.Hour), nil
	}
	first := shared.instance(t, "http-01", order)
	second := shared.instance(t, "http-01", order)

	firstDone := make(chan error, 1)
	go func() { firstDone <- first.spinOnce(context.Background()) }()
	select {
	case <-orderStarted:
	case <-time.After(10 * time.Second):
		t.Fatal("the first instance never started ordering")
	}

	// The second is due too, and finds the lock taken.
	if err := second.spinOnce(t.Context()); err != nil {
		t.Fatalf("the second instance's tick failed: %s", err)
	}
	if orderCount.Load() != 1 {
		t.Fatalf("%d instances ordered at once, want 1", orderCount.Load())
	}

	close(releaseOrder)
	if err := <-firstDone; err != nil {
		t.Fatalf("the first instance's tick failed: %s", err)
	}

	if err := second.spinOnce(t.Context()); err != nil {
		t.Fatalf("the second instance's next tick failed: %s", err)
	}
	if orderCount.Load() != 1 {
		t.Errorf("%d orders in all, want 1", orderCount.Load())
	}
	if _, err := second.GetCertificate(&tls.ClientHelloInfo{ServerName: "mail.example.com"}); err != nil {
		t.Errorf("the second instance does not serve the certificate the first obtained: %s", err)
	}
}

// An instance that takes the lock after another has already ordered finds the
// certificate saved and does not order a second time.
func TestInstanceThatTakesTheLockLateDoesNotOrderAgain(t *testing.T) {
	shared := newSharedState()
	var orderCount atomic.Int32
	order := func(context.Context, []string, string) (*tls.Certificate, error) {
		orderCount.Add(1)
		return selfSigned(t, []string{"mail.example.com"}, 90*24*time.Hour), nil
	}
	first := shared.instance(t, "http-01", order)
	second := shared.instance(t, "http-01", order)

	// The second checked before the first ordered, then waits for the lock.
	secondStates := second.certificateStates()
	if !second.shouldRequestCertificate(secondStates[0]) {
		t.Fatal("the second instance should be due")
	}
	if err := first.spinOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := second.orderDueCertificates(t.Context(), secondStates); err != nil {
		t.Fatal(err)
	}
	if orderCount.Load() != 1 {
		t.Errorf("%d orders, want 1", orderCount.Load())
	}
}

// Two instances starting with no account key end up with the same one, made
// once.
func TestAccountKeyIsCreatedOnce(t *testing.T) {
	shared := newSharedState()
	first := shared.instance(t, "http-01", nil)
	second := shared.instance(t, "http-01", nil)

	firstKey, err := first.loadOrCreateAccountKey()
	if err != nil {
		t.Fatalf("the first instance failed to create the key: %s", err)
	}
	secondKey, err := second.loadOrCreateAccountKey()
	if err != nil {
		t.Fatalf("the second instance failed to load the key: %s", err)
	}
	if shared.accountKeySaveCount != 1 {
		t.Errorf("the account key was created %d times, want 1", shared.accountKeySaveCount)
	}
	if !firstKey.(*ecdsa.PrivateKey).Equal(secondKey) {
		t.Error("the two instances use different account keys")
	}
}
