package llm

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// A token is fetched once and held until it is nearly out.
func TestAnAccessTokenIsFetchedOnceAndHeld(test *testing.T) {
	test.Parallel()

	fetches := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		fetches++
		body, _ := io.ReadAll(request.Body)
		if !strings.Contains(string(body), "grant_type=refresh_token") {
			test.Errorf("it asked with %q", string(body))
		}
		if !strings.Contains(string(body), "client_id=a-client") {
			test.Errorf("the client was not named: %q", string(body))
		}
		_, _ = io.WriteString(writer, `{"access_token":"an-access-token","expires_in":3600}`)
	}))
	defer server.Close()

	signer, err := newSignIn(server.URL, "a-client", "a-refresh-token", server.Client())
	if err != nil {
		test.Fatalf("newSignIn: %s", err)
	}
	for attempt := 0; attempt < 3; attempt++ {
		token, err := signer.token(context.Background())
		if err != nil {
			test.Fatalf("token: %s", err)
		}
		if token != "an-access-token" {
			test.Fatalf("it gave %q", token)
		}
	}
	if fetches != 1 {
		test.Errorf("it signed in %d times for three calls", fetches)
	}

	// And once it is thrown away, it is fetched again: the answer that
	// says a token is no good must not be met with the same token.
	signer.forget()
	if _, err := signer.token(context.Background()); err != nil {
		test.Fatalf("token after forget: %s", err)
	}
	if fetches != 2 {
		test.Errorf("after forgetting it signed in %d times", fetches)
	}
}

// A token close to running out is replaced before it is used, not after it
// fails: a request in flight when one expires is a request lost.
func TestATokenNearlyOutIsReplacedFirst(test *testing.T) {
	test.Parallel()

	fetches := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		fetches++
		// Shorter than the leeway, so the next call must fetch again.
		_, _ = fmt.Fprintf(writer, `{"access_token":"token-%d","expires_in":10}`, fetches)
	}))
	defer server.Close()

	signer, _ := newSignIn(server.URL, "a-client", "a-refresh-token", server.Client())
	first, _ := signer.token(context.Background())
	second, _ := signer.token(context.Background())
	if first == second {
		test.Error("a token inside the leeway was used again")
	}
	if fetches != 2 {
		test.Errorf("it signed in %d times", fetches)
	}
}

// Where the service says nothing about how long, the token's own claim is
// read; where it says neither, an hour is assumed.
func TestHowLongATokenLastsIsReadFromItWhereTheServiceIsSilent(test *testing.T) {
	test.Parallel()

	expires := time.Now().Add(2 * time.Hour).Unix()
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d}`, expires)))
	jwt := "header." + payload + ".signature"

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, `{"access_token":"`+jwt+`"}`)
	}))
	defer server.Close()

	signer, _ := newSignIn(server.URL, "a-client", "a-refresh-token", server.Client())
	if _, err := signer.token(context.Background()); err != nil {
		test.Fatalf("token: %s", err)
	}
	if got := signer.expires.Unix(); got != expires {
		test.Errorf("it expires at %d, and the token says %d", got, expires)
	}

	// And a token that says nothing falls back rather than expiring at the
	// zero time, which would refetch on every call.
	plain := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, `{"access_token":"not-a-jwt"}`)
	}))
	defer plain.Close()
	other, _ := newSignIn(plain.URL, "a-client", "a-refresh-token", plain.Client())
	if _, err := other.token(context.Background()); err != nil {
		test.Fatalf("token: %s", err)
	}
	if time.Until(other.expires) < 30*time.Minute {
		test.Errorf("a token with no expiry was taken as good for %v", time.Until(other.expires))
	}
}

// A rotated refresh token is kept, and whoever holds the configuration is
// told: a service that rotates and a caller that does not store the new one
// is signed out at the next refresh, hours later, for no visible reason.
func TestARotatedRefreshTokenIsKeptAndReported(test *testing.T) {
	test.Parallel()

	var asked []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		values, _ := url.ParseQuery(string(body))
		asked = append(asked, values.Get("refresh_token"))
		_, _ = io.WriteString(writer, `{"access_token":"an-access-token","refresh_token":"the-next-one","expires_in":1}`)
	}))
	defer server.Close()

	told := ""
	signer, _ := newSignIn(server.URL, "a-client", "the-first-one", server.Client())
	signer.rotated = func(refresh string) { told = refresh }

	if _, err := signer.token(context.Background()); err != nil {
		test.Fatalf("token: %s", err)
	}
	if told != "the-next-one" {
		test.Errorf("the rotation was reported as %q", told)
	}
	// The second call must use what came back, not what was configured.
	if _, err := signer.token(context.Background()); err != nil {
		test.Fatalf("token: %s", err)
	}
	if len(asked) != 2 || asked[1] != "the-next-one" {
		test.Errorf("it asked with %v", asked)
	}
}

// A refusal carries what the service said and whether signing in again is
// what it wants.
func TestARefusedSignInSaysWhetherToSignInAgain(test *testing.T) {
	test.Parallel()

	for _, each := range []struct {
		status      int
		reauthorize bool
	}{
		{http.StatusBadRequest, true},
		{http.StatusUnauthorized, true},
		{http.StatusTooManyRequests, false},
		{http.StatusBadGateway, false},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(each.status)
			_, _ = io.WriteString(writer, `{"error":"invalid_grant","error_description":"the refresh token is expired"}`)
		}))
		signer, _ := newSignIn(server.URL, "a-client", "a-refresh-token", server.Client())
		_, err := signer.token(context.Background())
		server.Close()

		var refused *SignInError
		if !asSignInError(err, &refused) {
			test.Fatalf("%d came back as %v", each.status, err)
		}
		if refused.Reauthorize() != each.reauthorize {
			test.Errorf("%d wants a new sign-in: %v", each.status, refused.Reauthorize())
		}
		if !strings.Contains(refused.Said, "expired") {
			test.Errorf("%d carried %q", each.status, refused.Said)
		}
	}
}

// Several callers refreshing at once sign in once between them. The reading
// runs its batches in parallel, and a service that rotates refresh tokens
// would hand each of them a token that the next one invalidates.
func TestCallersAtOnceSignInOnce(test *testing.T) {
	test.Parallel()

	var mutex sync.Mutex
	fetches := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		mutex.Lock()
		fetches++
		mutex.Unlock()
		time.Sleep(20 * time.Millisecond)
		_, _ = io.WriteString(writer, `{"access_token":"an-access-token","expires_in":3600}`)
	}))
	defer server.Close()

	signer, _ := newSignIn(server.URL, "a-client", "a-refresh-token", server.Client())
	var waiting sync.WaitGroup
	for caller := 0; caller < 8; caller++ {
		waiting.Add(1)
		go func() {
			defer waiting.Done()
			if _, err := signer.token(context.Background()); err != nil {
				test.Errorf("token: %s", err)
			}
		}()
	}
	waiting.Wait()

	mutex.Lock()
	defer mutex.Unlock()
	if fetches != 1 {
		test.Errorf("eight callers signed in %d times", fetches)
	}
}

// Nothing to sign in with is refused when the client is built, not at the
// first request: an operator who configured it wrongly learns at startup.
func TestASignInNeedsSomethingToSignInWith(test *testing.T) {
	test.Parallel()

	for _, each := range []struct {
		what                        string
		tokenUrl, clientId, refresh string
	}{
		{"no address", "", "a-client", "a-refresh-token"},
		{"no client", "https://example.test/token", "", "a-refresh-token"},
		{"no refresh token", "https://example.test/token", "a-client", "  "},
	} {
		if _, err := newSignIn(each.tokenUrl, each.clientId, each.refresh, nil); err == nil {
			test.Errorf("%s was accepted", each.what)
		}
	}
}

// asSignInError is errors.As without the ceremony.
func asSignInError(err error, target **SignInError) bool {
	for err != nil {
		if refused, ok := err.(*SignInError); ok {
			*target = refused
			return true
		}
		unwrapped, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapped.Unwrap()
	}
	return false
}

// A refresh token given from outside replaces the one held and the access
// token it bought; the one already held changes nothing.
func TestAGivenRefreshTokenIsAdopted(test *testing.T) {
	test.Parallel()

	var mutex sync.Mutex
	var askedWith []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		form, _ := url.ParseQuery(string(body))
		mutex.Lock()
		askedWith = append(askedWith, form.Get("refresh_token"))
		mutex.Unlock()
		_, _ = io.WriteString(writer, `{"access_token":"an-access-token","expires_in":3600}`)
	}))
	defer server.Close()

	signer, err := newSignIn(server.URL, "a-client", "the-first", server.Client())
	if err != nil {
		test.Fatalf("newSignIn: %s", err)
	}
	if _, err := signer.token(context.Background()); err != nil {
		test.Fatal(err)
	}
	signer.adopt("the-first")
	if _, err := signer.token(context.Background()); err != nil {
		test.Fatal(err)
	}
	signer.adopt("the-second")
	if _, err := signer.token(context.Background()); err != nil {
		test.Fatal(err)
	}
	if strings.Join(askedWith, ",") != "the-first,the-second" {
		test.Errorf("it signed in with %v", askedWith)
	}
}

// A rotated refresh token is handed to whoever keeps the configuration, by
// the provider's name, without holding up the refresh that received it.
func TestARotatedRefreshTokenIsHandedOnToKeep(test *testing.T) {
	test.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, `{"access_token":"an-access-token","refresh_token":"the-rotated","expires_in":3600}`)
	}))
	defer server.Close()

	signer, err := newSignIn(server.URL, "a-client", "the-first", server.Client())
	if err != nil {
		test.Fatalf("newSignIn: %s", err)
	}
	registry := &Registry{providers: map[string]*providerEntry{"plan": {service: &codex{signIn: signer}}}}
	kept := make(chan string, 1)
	registry.KeepRefreshTokens(func(provider, refreshToken string) {
		kept <- provider + "=" + refreshToken
	})
	if _, err := signer.token(context.Background()); err != nil {
		test.Fatal(err)
	}
	select {
	case said := <-kept:
		if said != "plan=the-rotated" {
			test.Errorf("it kept %q", said)
		}
	case <-time.After(5 * time.Second):
		test.Fatal("the rotated refresh token was never handed on")
	}
}
