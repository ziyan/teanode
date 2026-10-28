package llm

import (
	"context"
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

// fakeSignInStore is what several instances share: one refresh token, one
// access token and one lock, in memory.
type fakeSignInStore struct {
	lock sync.Mutex

	mutex        sync.Mutex
	refreshToken string
	accessToken  *SignInAccessToken
	saveCount    int
}

func (self *fakeSignInStore) WhileLocked(ctx context.Context, _ string, work func(context.Context) error) error {
	self.lock.Lock()
	defer self.lock.Unlock()
	return work(ctx)
}

func (self *fakeSignInStore) LoadRefreshToken(context.Context, string) (string, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.refreshToken, nil
}

func (self *fakeSignInStore) LoadAccessToken(context.Context, string) (*SignInAccessToken, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.accessToken == nil {
		return nil, nil
	}
	copied := *self.accessToken
	return &copied, nil
}

func (self *fakeSignInStore) Save(_ context.Context, _ string, refreshToken string, accessToken *SignInAccessToken) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.refreshToken, self.accessToken = refreshToken, accessToken
	self.saveCount++
	return nil
}

// rotatingService honours each refresh token once, as the real one does: the
// token used is replaced by the next, and the old one is refused.
type rotatingService struct {
	mutex        sync.Mutex
	refreshToken string
	refreshCount int
	refusedCount int
	server       *httptest.Server
}

func newRotatingService(test *testing.T, refreshToken string) *rotatingService {
	service := &rotatingService{refreshToken: refreshToken}
	service.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		form, _ := url.ParseQuery(string(body))
		service.mutex.Lock()
		defer service.mutex.Unlock()
		if form.Get("refresh_token") != service.refreshToken {
			service.refusedCount++
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(writer, `{"error":"invalid_grant","error_description":"the refresh token was already used"}`)
			return
		}
		service.refreshCount++
		service.refreshToken = "refresh-token-" + strings.Repeat("n", service.refreshCount)
		_, _ = io.WriteString(writer, `{"access_token":"access-token-`+strings.Repeat("n", service.refreshCount)+`","refresh_token":"`+service.refreshToken+`","expires_in":3600}`)
	}))
	test.Cleanup(service.server.Close)
	return service
}

// Two instances of one server hold the same refresh token and both need an
// access token at the same moment: one asks the service, the other takes
// what the first stored, and the service is not asked with a token it has
// already honoured.
func TestInstancesSharingASignInRefreshOnce(test *testing.T) {
	test.Parallel()

	service := newRotatingService(test, "refresh-token-0")
	store := &fakeSignInStore{refreshToken: "refresh-token-0"}
	var signers []*signIn
	for range 2 {
		signer, err := newSignIn(service.server.URL, "a-client", "refresh-token-0", service.server.Client())
		if err != nil {
			test.Fatal(err)
		}
		signer.share("plan", store)
		signers = append(signers, signer)
	}

	var waiting sync.WaitGroup
	tokens := make([]string, len(signers))
	for index := range signers {
		waiting.Add(1)
		go func() {
			defer waiting.Done()
			token, err := signers[index].token(context.Background())
			if err != nil {
				test.Errorf("instance %d: %s", index, err)
			}
			tokens[index] = token
		}()
	}
	waiting.Wait()

	if service.refreshCount != 1 || service.refusedCount != 0 {
		test.Errorf("the service refreshed %d times and refused %d", service.refreshCount, service.refusedCount)
	}
	if tokens[0] == "" || tokens[0] != tokens[1] {
		test.Errorf("the instances hold %q and %q", tokens[0], tokens[1])
	}
	for index, signer := range signers {
		if signer.current() != "refresh-token-n" {
			test.Errorf("instance %d holds the refresh token %q", index, signer.current())
		}
	}
	if store.refreshToken != "refresh-token-n" || store.saveCount != 1 {
		test.Errorf("the store holds %q after %d saves", store.refreshToken, store.saveCount)
	}
}

// An instance whose access token has run out finds that another instance
// rotated the refresh token and fetched a new access token since: it takes
// both and does not ask the service.
func TestAnInstanceTakesWhatAnotherStored(test *testing.T) {
	test.Parallel()

	service := newRotatingService(test, "refresh-token-0")
	store := &fakeSignInStore{refreshToken: "refresh-token-0"}
	first, err := newSignIn(service.server.URL, "a-client", "refresh-token-0", service.server.Client())
	if err != nil {
		test.Fatal(err)
	}
	second, err := newSignIn(service.server.URL, "a-client", "refresh-token-0", service.server.Client())
	if err != nil {
		test.Fatal(err)
	}
	first.share("plan", store)
	second.share("plan", store)

	// The second fetched an access token once, and it has since run out.
	if _, err := second.token(context.Background()); err != nil {
		test.Fatal(err)
	}
	second.mutex.Lock()
	second.expires = time.Now().Add(-time.Minute)
	second.mutex.Unlock()

	// The first rotates, as it would when its own ran out later.
	first.mutex.Lock()
	first.refresh, first.held["refresh-token-n"] = "refresh-token-n", true
	first.mutex.Unlock()
	store.mutex.Lock()
	store.refreshToken = "refresh-token-n"
	store.accessToken = &SignInAccessToken{AccessToken: "an-access-token-from-the-first", ForRefreshToken: "refresh-token-n", ExpiresAt: time.Now().Add(time.Hour)}
	store.mutex.Unlock()

	token, err := second.token(context.Background())
	if err != nil {
		test.Fatal(err)
	}
	if token != "an-access-token-from-the-first" || second.current() != "refresh-token-n" {
		test.Errorf("it holds %q with the refresh token %q", token, second.current())
	}
	if service.refreshCount != 1 {
		test.Errorf("the service was asked %d times", service.refreshCount)
	}
}

// Hearing of another instance's refresh token through the configuration does
// not throw away an access token that is still good: the one stored beside
// it is taken with it.
func TestAdoptingAStoredRefreshTokenKeepsItsGoodAccessToken(test *testing.T) {
	test.Parallel()

	service := newRotatingService(test, "refresh-token-0")
	store := &fakeSignInStore{refreshToken: "refresh-token-new"}
	store.accessToken = &SignInAccessToken{AccessToken: "a-good-access-token", ForRefreshToken: "refresh-token-new", ExpiresAt: time.Now().Add(time.Hour)}
	signer, err := newSignIn(service.server.URL, "a-client", "refresh-token-0", service.server.Client())
	if err != nil {
		test.Fatal(err)
	}
	signer.share("plan", store)

	signer.adopt("refresh-token-new")
	token, err := signer.token(context.Background())
	if err != nil {
		test.Fatal(err)
	}
	if token != "a-good-access-token" || service.refreshCount != 0 {
		test.Errorf("it holds %q after %d refreshes", token, service.refreshCount)
	}

	// An access token bought with another refresh token is not taken: after
	// a new sign-in it may belong to another account.
	store.mutex.Lock()
	store.accessToken = &SignInAccessToken{AccessToken: "an-access-token-of-another-account", ForRefreshToken: "refresh-token-other", ExpiresAt: time.Now().Add(time.Hour)}
	store.mutex.Unlock()
	other, err := newSignIn(service.server.URL, "a-client", "refresh-token-0", service.server.Client())
	if err != nil {
		test.Fatal(err)
	}
	other.share("plan", store)
	other.adopt("refresh-token-new")
	if token, _ := other.heldAccess(); token != "" {
		test.Errorf("it took %q bought with another refresh token", token)
	}
}

// A refresh token this sign-in held before is the configuration a moment
// behind a rotation, not a sign-in: taking it up would put a token the
// service no longer honours in place of the newer one.
func TestAdoptingAnOlderRefreshTokenChangesNothing(test *testing.T) {
	test.Parallel()

	signer, err := newSignIn("https://sign-in.example/token", "a-client", "refresh-token-0", nil)
	if err != nil {
		test.Fatal(err)
	}
	signer.adopt("refresh-token-1")
	signer.adopt("refresh-token-0")
	if signer.current() != "refresh-token-1" {
		test.Errorf("it holds %q", signer.current())
	}
}

// The access token the service refused is not taken again from the store,
// where it is still within its time.
func TestARefusedAccessTokenIsNotTakenBackFromTheStore(test *testing.T) {
	test.Parallel()

	service := newRotatingService(test, "refresh-token-0")
	store := &fakeSignInStore{refreshToken: "refresh-token-0"}
	signer, err := newSignIn(service.server.URL, "a-client", "refresh-token-0", service.server.Client())
	if err != nil {
		test.Fatal(err)
	}
	signer.share("plan", store)
	first, err := signer.token(context.Background())
	if err != nil {
		test.Fatal(err)
	}
	signer.forget()
	second, err := signer.token(context.Background())
	if err != nil {
		test.Fatal(err)
	}
	if first == second || service.refreshCount != 2 {
		test.Errorf("it held %q and then %q after %d refreshes", first, second, service.refreshCount)
	}
}

// A store that cannot be read stops the refresh: going on without the lock's
// protection is how the service is asked with a token it already honoured.
func TestARefreshIsNotMadeWithoutTheStore(test *testing.T) {
	test.Parallel()

	service := newRotatingService(test, "refresh-token-0")
	signer, err := newSignIn(service.server.URL, "a-client", "refresh-token-0", service.server.Client())
	if err != nil {
		test.Fatal(err)
	}
	signer.share("plan", unreadableSignInStore{})
	if _, err := signer.token(context.Background()); err == nil {
		test.Fatal("it refreshed")
	}
	if service.refreshCount != 0 {
		test.Errorf("the service was asked %d times", service.refreshCount)
	}
}

type unreadableSignInStore struct{}

func (unreadableSignInStore) WhileLocked(ctx context.Context, _ string, work func(context.Context) error) error {
	return work(ctx)
}
func (unreadableSignInStore) LoadRefreshToken(context.Context, string) (string, error) {
	return "", errors.New("the database is down")
}
func (unreadableSignInStore) LoadAccessToken(context.Context, string) (*SignInAccessToken, error) {
	return nil, errors.New("the database is down")
}
func (unreadableSignInStore) Save(context.Context, string, string, *SignInAccessToken) error {
	return errors.New("the database is down")
}
