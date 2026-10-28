package autoacme

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/asn1"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/crypto/acme"
)

// fakeACMEClient stands in for *acme.Client so that a solver can be exercised
// without a certificate authority. The values it returns are not real key
// authorizations; the solvers only move them around.
type fakeACMEClient struct {
	err error
}

func (self *fakeACMEClient) HTTP01ChallengeResponse(token string) (string, error) {
	if self.err != nil {
		return "", self.err
	}
	return "response-for-" + token, nil
}

func (self *fakeACMEClient) DNS01ChallengeRecord(token string) (string, error) {
	if self.err != nil {
		return "", self.err
	}
	return "record-for-" + token, nil
}

func challengesFor(domains ...string) []Challenge {
	challenges := make([]Challenge, 0, len(domains))
	for _, domain := range domains {
		challenges = append(challenges, Challenge{
			Domain:    domain,
			Challenge: &acme.Challenge{Type: "test", Token: "token-" + domain},
		})
	}
	return challenges
}

func TestHTTP01SolverServesAndForgets(t *testing.T) {
	solver := newHttp01Solver(newMemoryChallengeStore())
	client := &fakeACMEClient{}
	challenges := challengesFor("mail.example.com")

	if solver.Type() != "http-01" {
		t.Fatalf("solver type is %q", solver.Type())
	}
	if err := solver.Present(context.Background(), client, challenges); err != nil {
		t.Fatalf("failed to present: %s", err)
	}

	handler := solver.Handler()

	// The certificate authority fetches the token over plain HTTP.
	request := httptest.NewRequest(http.MethodGet, challengePath+"token-mail.example.com", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("challenge request returned %d, want 200", recorder.Code)
	}
	if body := recorder.Body.String(); body != "response-for-token-mail.example.com" {
		t.Errorf("challenge body is %q", body)
	}

	// An unknown token is a probe, not an error.
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, challengePath+"nonsense", nil))
	if recorder.Code != http.StatusNotFound {
		t.Errorf("unknown token returned %d, want 404", recorder.Code)
	}

	// A token containing a path separator must not escape the map lookup.
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, challengePath+"a/b", nil))
	if recorder.Code != http.StatusNotFound {
		t.Errorf("nested path returned %d, want 404", recorder.Code)
	}

	// After cleanup the response is gone, so a later probe learns nothing.
	if err := solver.CleanUp(context.Background(), challenges); err != nil {
		t.Fatalf("failed to clean up: %s", err)
	}
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, challengePath+"token-mail.example.com", nil))
	if recorder.Code != http.StatusNotFound {
		t.Errorf("after cleanup the challenge returned %d, want 404", recorder.Code)
	}
}

func TestTLSALPN01SolverHoldsChallengeCertificates(t *testing.T) {
	solver := newTlsalpn01Solver(newMemoryChallengeStore())
	client := &fakeACMEClient{}
	challenges := challengesFor("mail.example.com")

	if solver.Type() != "tls-alpn-01" {
		t.Fatalf("solver type is %q", solver.Type())
	}
	if _, ok := solver.certificateFor(context.Background(), "mail.example.com"); ok {
		t.Fatal("there should be no challenge certificate before presenting")
	}
	if err := solver.Present(context.Background(), client, challenges); err != nil {
		t.Fatalf("failed to present: %s", err)
	}

	// Server names arrive lowercased in practice, but not always.
	for _, name := range []string{"mail.example.com", "MAIL.EXAMPLE.COM"} {
		certificate, ok := solver.certificateFor(context.Background(), name)
		if !ok {
			t.Fatalf("no challenge certificate for %q", name)
		}
		leaf, err := x509.ParseCertificate(certificate.Certificate[0])
		if err != nil {
			t.Fatalf("the challenge certificate for %q cannot be parsed: %s", name, err)
		}
		if err := leaf.VerifyHostname("mail.example.com"); err != nil {
			t.Errorf("the challenge certificate for %q does not name the domain: %s", name, err)
		}
		// The extension carries the hash of the key authorization the client
		// built for the token.
		digest := sha256.Sum256([]byte("response-for-token-mail.example.com"))
		wantValue, err := asn1.Marshal(digest[:])
		if err != nil {
			t.Fatal(err)
		}
		var isFound bool
		for _, extension := range leaf.Extensions {
			if extension.Id.Equal(acmeIdentifierExtension) {
				isFound = true
				if !extension.Critical || !bytes.Equal(extension.Value, wantValue) {
					t.Errorf("the acmeIdentifier extension is wrong: critical %v, value %x, want %x", extension.Critical, extension.Value, wantValue)
				}
			}
		}
		if !isFound {
			t.Errorf("the challenge certificate for %q has no acmeIdentifier extension", name)
		}
	}
	if _, ok := solver.certificateFor(context.Background(), "other.example.com"); ok {
		t.Error("a name with no outstanding challenge should not get a certificate")
	}

	if err := solver.CleanUp(context.Background(), challenges); err != nil {
		t.Fatalf("failed to clean up: %s", err)
	}
	if _, ok := solver.certificateFor(context.Background(), "mail.example.com"); ok {
		t.Error("the challenge certificate should be gone after cleanup")
	}
}

func TestIsALPNChallenge(t *testing.T) {
	tests := []struct {
		name      string
		protocols []string
		want      bool
	}{
		{"a certificate authority validating", []string{acme.ALPNProto}, true},
		{"a browser speaking h2", []string{"h2", "http/1.1"}, false},
		{"a plain client offering nothing", nil, false},
		{"a client offering both", []string{"h2", acme.ALPNProto}, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isAlpnChallenge(&tls.ClientHelloInfo{SupportedProtos: test.protocols}); got != test.want {
				t.Errorf("isAlpnChallenge = %v, want %v", got, test.want)
			}
		})
	}
}

func TestRoute53SolverChallengeRecordNames(t *testing.T) {
	solver := &route53Solver{hosts: []string{"example.com", "*.example.com", "mail.other.example"}}

	names := solver.challengeRecordNames()
	want := []string{"_acme-challenge.example.com", "_acme-challenge.mail.other.example"}
	if len(names) != len(want) {
		t.Fatalf("got %v, want %v", names, want)
	}
	for index, name := range names {
		if name != want[index] {
			// A wildcard is authorized under its bare domain, so
			// example.com and *.example.com share one record.
			t.Errorf("record %d is %q, want %q", index, name, want[index])
		}
	}
}
