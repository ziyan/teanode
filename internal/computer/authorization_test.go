package computer

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
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
