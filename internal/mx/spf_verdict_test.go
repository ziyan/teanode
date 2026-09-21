package mx

import (
	"errors"
	"testing"

	"github.com/ziyan/teanode/internal/util/authres"
	"github.com/ziyan/teanode/internal/util/dkim"
	"github.com/ziyan/teanode/internal/util/dmarc"
	"github.com/ziyan/teanode/internal/util/mailparse"
	"github.com/ziyan/teanode/internal/util/spf"
)

// An SPF error is not a reason to refuse mail the signatures vouch for. An
// Amazon order confirmation — two aligned signatures, DMARC passing — was
// bounced with a permanent 550 because the include chain of its SPF record
// could not be resolved from this host.
func TestAnSPFErrorDoesNotRefuseAuthenticatedMail(t *testing.T) {
	t.Parallel()

	signed := []*dkim.Verification{{Result: dkim.ResultPass, Domain: "example.com"}}
	unsigned := []*dkim.Verification(nil)
	broken := []*dkim.Verification{{Result: dkim.ResultFail}}
	quarantine := &dmarc.Discovery{Record: &dmarc.Record{Policy: dmarc.PolicyQuarantine}}
	reject := &dmarc.Discovery{Record: &dmarc.Record{Policy: dmarc.PolicyReject}}
	var none *dmarc.Discovery

	cases := []struct {
		name   string
		spf    spf.Result
		dkim   []*dkim.Verification
		dmarc  authres.ResultValue
		policy *dmarc.Discovery
		want   error
	}{
		// The Amazon case: DMARC passed on DKIM, and SPF's error is beside the point.
		{"permerror under a passing DMARC", spf.ResultPermError, signed, authres.ResultPass, quarantine, nil},
		{"temperror under a passing DMARC", spf.ResultTempError, signed, authres.ResultPass, quarantine, nil},
		// No policy: a permerror counts for nothing, and DKIM decides.
		{"permerror, no policy, signed", spf.ResultPermError, signed, authres.ResultNone, none, nil},
		{"permerror, no policy, unsigned", spf.ResultPermError, unsigned, authres.ResultNone, none, nil},
		{"permerror, no policy, only broken signatures", spf.ResultPermError, broken, authres.ResultNone, none, mailparse.ErrDKIMVerificationFailed},
		// A temporary error asks the sender to try again, whatever DKIM said,
		// so the next attempt gets a real SPF verdict.
		{"temperror, no policy, signed", spf.ResultTempError, signed, authres.ResultNone, none, mailparse.ErrSPFTemporaryError},
		{"temperror, no policy, unsigned", spf.ResultTempError, unsigned, authres.ResultNone, none, mailparse.ErrSPFTemporaryError},
		// What has not changed.
		{"spf fail, no policy", spf.ResultFail, signed, authres.ResultNone, none, mailparse.ErrSPFValidationFailed},
		{"dmarc fail under reject", spf.ResultFail, broken, authres.ResultFail, reject, mailparse.ErrDMARCAlignmentFailed},
		{"dmarc fail under quarantine is not refused on its own", spf.ResultNone, signed, authres.ResultFail, quarantine, nil},
		{"dmarc fail under quarantine with spf fail", spf.ResultFail, signed, authres.ResultFail, quarantine, mailparse.ErrSPFValidationFailed},
		{"everything passes", spf.ResultPass, signed, authres.ResultPass, reject, nil},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			got := authenticationVerdict(testCase.spf, testCase.dkim, testCase.dmarc, testCase.policy)
			if !errors.Is(got, testCase.want) {
				t.Errorf("verdict = %v, want %v", got, testCase.want)
			}
		})
	}
}

// A temporary refusal is one the sender retries; a permanent one is a bounce.
func TestSPFTemporaryErrorIsATemporaryRefusal(t *testing.T) {
	t.Parallel()
	var refusal *mailparse.Error
	if !errors.As(mailparse.ErrSPFTemporaryError, &refusal) || refusal.StatusCode() != 451 {
		t.Errorf("ErrSPFTemporaryError should be a 451, got %v", mailparse.ErrSPFTemporaryError)
	}
}
