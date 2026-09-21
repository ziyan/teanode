package upgrade

import (
	"runtime"
	"strings"
	"testing"
)

// The asset fetched is the server's, not the client's.
//
// This program ships two binaries and only one of them upgrades itself. The
// name here was written when there was one, kept saying "teanode" after the
// split, and so downloaded the command-line client and staged it as the
// server — which exec'd a program with no run command, failed to start, and
// was refused by the guard on every boot afterwards. Self-upgrade was broken
// for three releases and nothing failed, because nothing asserted the name.
func TestTheAssetIsTheServerNotTheClient(test *testing.T) {
	test.Parallel()

	name := assetName()
	if !strings.HasPrefix(name, "teanode-server-") {
		test.Errorf("assetName() = %q, want the server binary; the client is named teanode-<os>-<arch> "+
			"and is not what a server replaces itself with", name)
	}
	if !strings.Contains(name, runtime.GOOS) || !strings.Contains(name, runtime.GOARCH) {
		test.Errorf("assetName() = %q, want this platform in it", name)
	}
}

// And the staged file is named for the same program, so that what is fetched
// and what is exec'd cannot drift apart again.
func TestTheStagedNameMatchesTheAsset(test *testing.T) {
	test.Parallel()

	if !strings.HasPrefix(assetName(), stagedName+"-") {
		test.Errorf("assetName() = %q does not name the staged binary %q", assetName(), stagedName)
	}
}
