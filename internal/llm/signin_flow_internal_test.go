package llm

import (
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
)

// The page a person opens carries the fingerprint of a secret, not the
// secret: that is the whole of what this grant buys, since a program anyone
// can read cannot keep a client secret and so is given none.
func TestTheSignInAsksWithAFingerprintOfItsSecret(test *testing.T) {
	flow, err := BeginSignIn("openai-codex")
	if err != nil {
		test.Skipf("port 1455 is not free here: %s", err)
	}
	defer flow.Close()

	address, err := url.Parse(flow.Address)
	if err != nil {
		test.Fatalf("the page is not an address: %s", err)
	}
	query := address.Query()

	if query.Get("code_challenge_method") != "S256" {
		test.Errorf("it asked with %q", query.Get("code_challenge_method"))
	}
	// The challenge is the digest of the verifier, and the verifier is
	// never in the address.
	digest := sha256.Sum256([]byte(flow.verifier))
	if want := base64.RawURLEncoding.EncodeToString(digest[:]); query.Get("code_challenge") != want {
		test.Error("the fingerprint is not of the secret it will show later")
	}
	if strings.Contains(flow.Address, flow.verifier) {
		test.Error("the secret itself was put in the address")
	}
	// Without offline_access the service gives an access token and nothing
	// to renew it with, and the sign-in has to be done again every hour.
	if !strings.Contains(query.Get("scope"), "offline_access") {
		test.Errorf("the scope was %q", query.Get("scope"))
	}
	if query.Get("redirect_uri") != signInRedirect {
		test.Errorf("it would be sent back to %q", query.Get("redirect_uri"))
	}
	if query.Get("state") == "" || query.Get("state") != flow.state {
		test.Error("there is nothing to tell this sign-in from another")
	}
	if query.Get("client_id") != codexClientId {
		test.Errorf("it signed in as %q", query.Get("client_id"))
	}
}

// Two sign-ins never share a secret or a state.
func TestEverySignInInventsItsOwnSecret(test *testing.T) {
	first, err := BeginSignIn("openai-codex")
	if err != nil {
		test.Skipf("port 1455 is not free here: %s", err)
	}
	verifier, state := first.verifier, first.state
	first.Close()

	second, err := BeginSignIn("openai-codex")
	if err != nil {
		test.Skipf("port 1455 did not come back: %s", err)
	}
	defer second.Close()

	if second.verifier == verifier || second.state == state {
		test.Error("two sign-ins shared a secret")
	}
}

// A kind that does not sign in is refused here, rather than opening a page
// that goes nowhere.
func TestOnlyAKindThatSignsInMaySignIn(test *testing.T) {
	test.Parallel()
	if _, err := BeginSignIn("openai"); err == nil {
		test.Error("a keyed kind was offered a sign-in")
	}
}

// Which account and plan were signed in as, read out of the token so a
// person is told rather than having to go and look.
func TestTheAccountIsReadFromWhatTheServiceIssued(test *testing.T) {
	test.Parallel()

	claims := `{"https://api.openai.com/auth":{"chatgpt_account_id":"an-account","chatgpt_plan_type":"a-plan"}}`
	token := "header." + base64.RawURLEncoding.EncodeToString([]byte(claims)) + ".signature"
	account, plan := accountOfToken(token)
	if account != "an-account" || plan != "a-plan" {
		test.Errorf("it read %q and %q", account, plan)
	}

	// And anything that is not a token reads as nothing rather than
	// failing: the sign-in itself succeeded, and this is a nicety.
	if account, plan := accountOfToken("not-a-token"); account != "" || plan != "" {
		test.Errorf("it read %q and %q out of nothing", account, plan)
	}
}
