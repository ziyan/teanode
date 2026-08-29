package autoacme

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/crypto/acme"
)

// fakeACMEClient stands in for *acme.Client so that a solver can be exercised
// without a certificate authority. The values it returns are not real key
// authorizations; the solvers only move them around.
type fakeACMEClient struct {
	certificate tls.Certificate
	err         error
}

func (self *fakeACMEClient) HTTP01ChallengeResponse(token string) (string, error) {
	if self.err != nil {
		return "", self.err
	}
	return "response-for-" + token, nil
}

func (self *fakeACMEClient) TLSALPN01ChallengeCert(token, domain string, options ...acme.CertOption) (tls.Certificate, error) {
	if self.err != nil {
		return tls.Certificate{}, self.err
	}
	return self.certificate, nil
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
	solver := newHTTP01Solver()
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
	solver := newTLSALPN01Solver()
	client := &fakeACMEClient{certificate: tls.Certificate{Certificate: [][]byte{{1, 2, 3}}}}
	challenges := challengesFor("mail.example.com")

	if solver.Type() != "tls-alpn-01" {
		t.Fatalf("solver type is %q", solver.Type())
	}
	if _, ok := solver.certificateFor("mail.example.com"); ok {
		t.Fatal("there should be no challenge certificate before presenting")
	}
	if err := solver.Present(context.Background(), client, challenges); err != nil {
		t.Fatalf("failed to present: %s", err)
	}

	// Server names arrive lowercased in practice, but not always.
	for _, name := range []string{"mail.example.com", "MAIL.EXAMPLE.COM"} {
		certificate, ok := solver.certificateFor(name)
		if !ok {
			t.Fatalf("no challenge certificate for %q", name)
		}
		if len(certificate.Certificate) != 1 || certificate.Certificate[0][0] != 1 {
			t.Errorf("wrong challenge certificate for %q", name)
		}
	}
	if _, ok := solver.certificateFor("other.example.com"); ok {
		t.Error("a name with no outstanding challenge should not get a certificate")
	}

	if err := solver.CleanUp(context.Background(), challenges); err != nil {
		t.Fatalf("failed to clean up: %s", err)
	}
	if _, ok := solver.certificateFor("mail.example.com"); ok {
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
			if got := isALPNChallenge(&tls.ClientHelloInfo{SupportedProtos: test.protocols}); got != test.want {
				t.Errorf("isALPNChallenge = %v, want %v", got, test.want)
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
