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
	// And a name allows the address it points at, which is the only form
	// the guard ever sees: by the time a dial is checked the name is gone.
	// localhost is the one name a test can count on resolving.
	named := safefetch.ParseAllowance([]string{"localhost"})
	if !named.PermitsAddress("127.0.0.1:80") {
		t.Error("a listed name has to permit what it resolves to, or listing one does nothing")
	}
	if err := safefetch.AllowAddressWith(named, "127.0.0.1:80"); err != nil {
		t.Errorf("a listed name, dialled: %s", err)
	}
	// Somewhere else private is still refused: naming one host does not
	// open the network it is on.
	if named.PermitsAddress("10.1.2.3:80") {
		t.Error("one name is one host")
	}
	// A caller cannot widen the allowance by writing into what it is given.
	hosts := allowance.Hosts()
	hosts["anything.lan"] = true
	if allowance.PermitsHost("anything.lan") {
		t.Error("the map handed out is a copy")
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
