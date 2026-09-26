package llm

import (
	"context"
	"encoding/base64"
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

// A code is asked for, waited on until it is entered, and redeemed for a
// refresh token and the account it bills to.
func TestASignInWithACodeWaitsForItAndRedeemsIt(test *testing.T) {
	test.Parallel()

	var mutex sync.Mutex
	polls := 0
	var redeemedWith url.Values
	claims := base64.RawURLEncoding.EncodeToString([]byte(`{"https://api.example/auth":{"chatgpt_account_id":"an-account","chatgpt_plan_type":"a-plan"}}`))
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		mutex.Lock()
		defer mutex.Unlock()
		body, _ := io.ReadAll(request.Body)
		switch request.URL.Path {
		case "/api/accounts/deviceauth/usercode":
			if !strings.Contains(string(body), `"client_id":"a-client"`) {
				test.Errorf("the code was asked for with %s", body)
			}
			_, _ = io.WriteString(writer, `{"device_auth_id":"an-id","user_code":"ABCD-1234","interval":"1"}`)
		case "/api/accounts/deviceauth/token":
			polls++
			if polls == 1 {
				writer.WriteHeader(http.StatusForbidden)
				_, _ = io.WriteString(writer, `{"error":{"message":"pending","code":"deviceauth_authorization_pending"}}`)
				return
			}
			_, _ = io.WriteString(writer, `{"authorization_code":"a-code","code_challenge":"x","code_verifier":"a-verifier"}`)
		case "/oauth/token":
			redeemedWith, _ = url.ParseQuery(string(body))
			_, _ = io.WriteString(writer, `{"refresh_token":"a-refresh-token","id_token":"header.`+claims+`.signature"}`)
		default:
			test.Errorf("asked for %s", request.URL.Path)
		}
	}))
	defer server.Close()

	started, err := beginDeviceSignIn(context.Background(), server.URL, "a-client", server.URL+"/oauth/token", server.Client())
	if err != nil {
		test.Fatal(err)
	}
	if started.UserCode != "ABCD-1234" || started.VerificationAddress != server.URL+"/codex/device" || started.interval != time.Second {
		test.Fatalf("it began as %+v", started)
	}

	// A wait shorter than the interval ends pending.
	short, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := started.Wait(short); !errors.Is(err, ErrSignInPending) {
		test.Fatalf("a short wait answered %v", err)
	}
	result, err := started.Wait(context.Background())
	if err != nil {
		test.Fatal(err)
	}
	if result.RefreshToken != "a-refresh-token" || result.Account != "an-account" || result.Plan != "a-plan" {
		test.Errorf("it signed in as %+v", result)
	}
	if redeemedWith.Get("code") != "a-code" || redeemedWith.Get("code_verifier") != "a-verifier" || redeemedWith.Get("redirect_uri") != server.URL+"/deviceauth/callback" {
		test.Errorf("it redeemed with %v", redeemedWith)
	}
	// Asked again, it answers the same without redeeming twice.
	again, err := started.Wait(context.Background())
	if err != nil || again != result {
		test.Errorf("asked again: %+v, %v", again, err)
	}
}

// A code refused, or one that has run out, says so rather than waiting.
func TestASignInWithACodeThatCannotFinishSaysSo(test *testing.T) {
	test.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/accounts/deviceauth/usercode" {
			_, _ = io.WriteString(writer, `{"device_auth_id":"an-id","user_code":"ABCD-1234","interval":1}`)
			return
		}
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(writer, `{"error":{"message":"the code was declined","code":"deviceauth_declined"}}`)
	}))
	defer server.Close()

	started, err := beginDeviceSignIn(context.Background(), server.URL, "a-client", server.URL+"/oauth/token", server.Client())
	if err != nil {
		test.Fatal(err)
	}
	if _, err := started.Wait(context.Background()); err == nil || !strings.Contains(err.Error(), "the code was declined") {
		test.Errorf("a declined code answered %v", err)
	}
	started.ExpiresAt = time.Now().Add(-time.Second)
	if _, err := started.Wait(context.Background()); err == nil || !strings.Contains(err.Error(), "expired") {
		test.Errorf("an expired code answered %v", err)
	}
}
