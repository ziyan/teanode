package safefetch_test

import (
	"testing"

	"github.com/ziyan/teanode/internal/util/safefetch"
)

// What an operator lists is reachable; everything else on the network is
// not. The guard is what stands between a link in a stranger's message and
// the network this server sits in, so an allowance has to be exactly what
// was written and nothing near it.
func TestAnAllowanceIsWhatWasWritten(t *testing.T) {
	t.Parallel()

	allowance := safefetch.ParseAllowance([]string{"192.168.255.254", "10.0.0.0/24", "printer.lan", "  ", ""})

	for _, address := range []string{"192.168.255.254:443", "10.0.0.7:80", "10.0.0.255:443"} {
		if !allowance.PermitsAddress(address) {
			t.Errorf("%s was listed", address)
		}
		if err := safefetch.AllowAddressWith(allowance, address); err != nil {
			t.Errorf("%s: %s", address, err)
		}
	}
	// Its neighbours are not it: an address one away from the one written,
	// and a range one wider, are somebody else's equipment.
	for _, address := range []string{"192.168.255.253:443", "10.0.1.7:80", "127.0.0.1:80", "169.254.169.254:80"} {
		if allowance.PermitsAddress(address) {
			t.Errorf("%s was not listed", address)
		}
		if err := safefetch.AllowAddressWith(allowance, address); err == nil {
			t.Errorf("%s should be refused", address)
		}
	}
	// A name is allowed as it stands, and matched however it is cased.
	if !allowance.PermitsHost("PRINTER.LAN") || allowance.PermitsHost("other.lan") {
		t.Error("a name is allowed as written, and nothing else is")
	}
	// Nothing listed is nothing allowed, and a nil allowance refuses the
	// way the guard on its own does.
	if safefetch.ParseAllowance(nil) != nil || safefetch.ParseAllowance([]string{" "}) != nil {
		t.Error("an empty list is no allowance at all")
	}
	if err := safefetch.AllowAddressWith(nil, "10.0.0.7:80"); err == nil {
		t.Error("with no allowance the guard stands")
	}
	if err := safefetch.AllowAddressWith(allowance, "93.184.216.34:443"); err != nil {
		t.Errorf("a public address needs no allowance: %s", err)
	}
}
