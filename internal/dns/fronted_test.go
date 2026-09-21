package dns

import (
	"net"
	"strings"
	"testing"
)

func isIPv4(value string) bool { return net.ParseIP(value).To4() != nil }

// A server behind a forwarder is reached at the forwarder's address, so the
// MX host's A record names that address on purpose. Checking it against the
// address the server sees from outside made the dashboard ask, for ever, for
// a change that would have stopped the mail.
func TestADeclaredAddressIsRightRatherThanAChange(test *testing.T) {
	test.Parallel()

	declared := map[string]bool{"54.178.37.88": true, "52.68.201.66": true}

	found, verified, wanted := judgeAddresses([]string{"54.178.37.88"}, "108.226.71.106", declared, isIPv4)
	if !verified {
		test.Error("an address the operator declared as reaching this server was called a mistake")
	}
	if wanted != "54.178.37.88" {
		test.Errorf("the row asks for %q, want the address that is published and right", wanted)
	}
	if strings.Join(found, ",") != "54.178.37.88" {
		test.Errorf("found = %v, want what is published", found)
	}
}

// The server's own address is still what an ordinary deployment publishes,
// and it wins over a declared one when both are there.
func TestTheServersOwnAddressStillCounts(test *testing.T) {
	test.Parallel()

	declared := map[string]bool{"54.178.37.88": true}
	_, verified, wanted := judgeAddresses([]string{"108.226.71.106"}, "108.226.71.106", declared, isIPv4)
	if !verified || wanted != "108.226.71.106" {
		test.Errorf("verified = %v, asking for %q; want the discovered address to verify itself", verified, wanted)
	}

	_, verified, wanted = judgeAddresses([]string{"54.178.37.88", "108.226.71.106"}, "108.226.71.106", declared, isIPv4)
	if !verified || wanted != "108.226.71.106" {
		test.Errorf("with both published, verified = %v asking for %q; want this server's own address", verified, wanted)
	}
}

// Nothing declared, and an address that is neither, is still a change to
// make: this is the check that tells somebody their DNS points elsewhere.
func TestAnAddressThatIsNeitherIsStillAChange(test *testing.T) {
	test.Parallel()

	found, verified, wanted := judgeAddresses([]string{"203.0.113.9"}, "108.226.71.106", map[string]bool{}, isIPv4)
	if verified {
		test.Error("an address that is not this server's and was not declared verified the record")
	}
	if wanted != "108.226.71.106" {
		test.Errorf("the row asks for %q, want this server's own address", wanted)
	}
	if strings.Join(found, ",") != "203.0.113.9" {
		test.Errorf("found = %v, want what is published, so the row can say what is there", found)
	}
}
