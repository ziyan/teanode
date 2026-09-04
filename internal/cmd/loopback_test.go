package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func post(t *testing.T, address, body string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, address, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://mail.example.com")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("POST %s: %s", address, err)
	}
	_ = response.Body.Close()
	return response
}

func TestLoopbackDeliversTheRightPost(t *testing.T) {
	ctx := context.Background()
	listener, err := newLoopback(ctx, "https://mail.example.com")
	if err != nil {
		t.Fatalf("newLoopback: %s", err)
	}
	defer listener.Close()

	authorize := listener.AuthorizeURL("https://mail.example.com", "laptop", "720h")
	parsed, err := url.Parse(authorize)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Path != CommandLinePagePath || parsed.Host != "mail.example.com" {
		t.Errorf("the page to open is %s", authorize)
	}
	query := parsed.Query()
	if query.Get("port") != fmt.Sprint(listener.Port()) || query.Get("name") != "laptop" || query.Get("lifetime") != "720h" {
		t.Errorf("query = %v", query)
	}
	state := query.Get("state")
	if len(state) != 32 {
		t.Errorf("the state nonce is %q, want 16 random bytes in hex", state)
	}

	callback := fmt.Sprintf("http://127.0.0.1:%d/callback", listener.Port())

	// The browser's preflight, which has to be acknowledged for a public page
	// to be allowed to post to a loopback address at all.
	request, _ := http.NewRequest(http.MethodOptions, callback, nil)
	request.Header.Set("Origin", "https://mail.example.com")
	request.Header.Set("Access-Control-Request-Private-Network", "true")
	preflight, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = preflight.Body.Close()
	if preflight.StatusCode != http.StatusNoContent {
		t.Errorf("preflight answered %d", preflight.StatusCode)
	}
	if origin := preflight.Header.Get("Access-Control-Allow-Origin"); origin != "https://mail.example.com" {
		t.Errorf("only the server's own origin may post; allowed %q", origin)
	}
	if preflight.Header.Get("Access-Control-Allow-Private-Network") != "true" {
		t.Error("the private network preflight was not acknowledged")
	}

	// A post that was not made by this command's page is refused and, more
	// to the point, not delivered.
	wrong := post(t, callback, `{"state":"0000","token":"tnt_stolen"}`)
	if wrong.StatusCode != http.StatusBadRequest {
		t.Errorf("a wrong nonce answered %d", wrong.StatusCode)
	}
	select {
	case result := <-listener.results:
		t.Fatalf("a post with the wrong nonce was delivered: %+v", result)
	default:
	}

	right := post(t, callback, fmt.Sprintf(`{"state":%q,"token":"tnt_right","tokenId":"01T","username":"ziyan"}`, state))
	if right.StatusCode != http.StatusOK {
		t.Errorf("the right post answered %d", right.StatusCode)
	}
	result, err := listener.Wait(ctx, time.Second)
	if err != nil {
		t.Fatalf("Wait: %s", err)
	}
	if result.Token != "tnt_right" || result.TokenID != "01T" || result.Username != "ziyan" {
		t.Errorf("got %+v", result)
	}
}

func TestLoopbackReportsTheServersRefusal(t *testing.T) {
	ctx := context.Background()
	listener, err := newLoopback(ctx, "http://127.0.0.1:10081")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	state := listener.state
	post(t, fmt.Sprintf("http://127.0.0.1:%d/callback", listener.Port()),
		fmt.Sprintf(`{"state":%q,"error":"not logged in"}`, state))
	if _, err := listener.Wait(ctx, time.Second); err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Errorf("the page's error should be reported, got %v", err)
	}
}

func TestLoopbackGivesUp(t *testing.T) {
	ctx := context.Background()
	listener, err := newLoopback(ctx, "https://mail.example.com")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if _, err := listener.Wait(ctx, 10*time.Millisecond); err == nil || !strings.Contains(err.Error(), "--token") {
		t.Errorf("a timeout should say what to do instead, got %v", err)
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := listener.Wait(cancelled, time.Second); err == nil {
		t.Error("a cancelled context should end the wait")
	}
}
