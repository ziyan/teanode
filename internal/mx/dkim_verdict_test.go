package mx

import (
	"testing"

	"github.com/ziyan/teanode/internal/util/dkim"
	"github.com/ziyan/teanode/internal/util/spf"
)

// With no DMARC policy to consult, one valid signature is enough — and so is
// SPF passing. Ten legitimate messages through Apple's private relay were
// refused in six days for carrying a second, broken signature beside a valid
// one.
func TestOneValidSignatureIsEnough(t *testing.T) {
	t.Parallel()

	twoSignatures := []*dkim.Verification{{Result: dkim.ResultFail}, {Result: dkim.ResultPass}}
	if err := dkimVerdict(twoSignatures, spf.ResultNone); err != nil {
		t.Errorf("refused a message with a valid signature beside a broken one: %v", err)
	}

	allBroken := []*dkim.Verification{{Result: dkim.ResultFail}}
	if err := dkimVerdict(allBroken, spf.ResultPass); err != nil {
		t.Errorf("refused a message SPF vouched for over a broken signature: %v", err)
	}
	if err := dkimVerdict(allBroken, spf.ResultNone); err == nil {
		t.Errorf("accepted a message with only broken signatures and nothing else vouching for it")
	}
	if err := dkimVerdict(nil, spf.ResultNone); err != nil {
		t.Errorf("refused an unsigned message on DKIM grounds: %v", err)
	}
}
