package agent

import (
	"strings"
	"testing"
)

// Location is offered on the tour only to a person who may use it, and it is
// the last of the offers that cost nothing to set up, before mail.
func TestPhoneTourOffersLocationOnlyWhenThePersonCanUseIt(t *testing.T) {
	without := phoneTourInstructions(phoneTourFacts{isPushEnabled: true})
	if strings.Contains(without, "Location") {
		t.Errorf("Location is offered to a person without location:use:\n%s", without)
	}
	with := phoneTourInstructions(phoneTourFacts{isPushEnabled: true, hasLocation: true})
	for _, want := range []string{"Location: the app can record the places they go", "look back on where they were", "share where they are with their family", "set_up action"} {
		if !strings.Contains(with, want) {
			t.Errorf("the offer does not say %q:\n%s", want, with)
		}
	}
	withMail := phoneTourInstructions(phoneTourFacts{isPushEnabled: true, hasLocation: true, hasMailbox: true})
	if strings.Index(withMail, "Location:") > strings.Index(withMail, "Their mail") {
		t.Errorf("Location comes before mail:\n%s", withMail)
	}
}
