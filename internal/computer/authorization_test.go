package computer

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestAuthorizationForwardSendsTheBrowserOn(t *testing.T) {
	result, err := RunAuthorizationForward(&AuthorizationForwardArguments{ForwardURL: "https://mail.example.com/agent?connect=tracker", LifetimeSeconds: 30})
	if err != nil {
		t.Fatal(err)
	}
	redirect, err := url.Parse(result.RedirectURL)
	if err != nil || redirect.Scheme != "http" || redirect.Hostname() != "localhost" || redirect.Path != "/callback" {
		t.Fatalf("redirect address %q", result.RedirectURL)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	// A visit that carries no authorization is not forwarded.
	response, err := client.Get(result.RedirectURL)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("an empty visit answered %d", response.StatusCode)
	}

	response, err = client.Get(result.RedirectURL + "?code=abc&state=xyz&unrelated=1")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusFound {
		t.Fatalf("the redirect answered %d", response.StatusCode)
	}
	location, err := url.Parse(response.Header.Get("Location"))
	if err != nil || location.Host != "mail.example.com" || location.Path != "/agent" {
		t.Fatalf("forwarded to %q", response.Header.Get("Location"))
	}
	query := location.Query()
	if query.Get("connect") != "tracker" || query.Get("code") != "abc" || query.Get("state") != "xyz" {
		t.Fatalf("forwarded query %v", query)
	}
	if query.Has("unrelated") {
		t.Fatalf("a parameter that is not the authorization's was carried on: %v", query)
	}
}

func TestAuthorizationForwardRefusesAnAddressThatIsNotOne(t *testing.T) {
	for _, forward := range []string{"", "mail.example.com/agent", "javascript:alert(1)", "file:///etc/passwd"} {
		if _, err := RunAuthorizationForward(&AuthorizationForwardArguments{ForwardURL: forward}); err == nil || !strings.Contains(err.Error(), "not an http or https address") {
			t.Fatalf("forward to %q: %v", forward, err)
		}
	}
}

// Pressing Authorize again replaces the listener for that server rather than
// adding to a pile that ends in a refusal, and past the most the oldest one
// makes room: nothing asked for is ever refused for the ones left waiting.
func TestAuthorizationForwardReplacesWhatWasLeftWaiting(t *testing.T) {
	open := func(server string) string {
		t.Helper()
		result, err := RunAuthorizationForward(&AuthorizationForwardArguments{ForwardURL: "https://mail.example.com/agent?connect=" + server, LifetimeSeconds: 60})
		if err != nil {
			t.Fatalf("%s: %s", server, err)
		}
		return result.RedirectURL
	}
	waitingCount := func() int {
		authorizationListenersMutex.Lock()
		defer authorizationListenersMutex.Unlock()
		return len(authorizationListeners)
	}
	first := open("tracker")
	for attempt := 0; attempt < authorizationListenersAtMost+2; attempt++ {
		open("tracker")
	}
	if count := waitingCount(); count != 1 {
		t.Fatalf("the same server asked again should leave one listener, not %d", count)
	}
	// The one given up on stops answering.
	deadline := time.Now().Add(5 * time.Second)
	for {
		response, err := http.Get(first + "?code=late&state=late")
		if err != nil {
			break
		}
		_ = response.Body.Close()
		if time.Now().After(deadline) {
			t.Fatal("a replaced listener should close")
		}
		time.Sleep(50 * time.Millisecond)
	}
	for index := 0; index < authorizationListenersAtMost+2; index++ {
		open("server-" + strings.Repeat("x", index+1))
	}
	if count := waitingCount(); count != authorizationListenersAtMost {
		t.Fatalf("past the most, the oldest should make room: %d waiting", count)
	}
}
