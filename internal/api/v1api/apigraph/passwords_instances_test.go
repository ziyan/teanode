package apigraph

import (
	"strings"
	"testing"
)

// The wrong proofs given to unlock Passwords are counted in the database, so a
// second instance of the server shares the tries the first one has spent.
func TestWrongProofsAreCountedAcrossInstances(test *testing.T) {
	world := newPasswordsWorld(test)
	ada := newPasswordsDevice(test, world.ada)
	world.setUp(test, ada)
	wrongProof, _ := proofOf("a wrong secret")
	rightProof, _ := proofOf(ada.passphrase)

	for range passwordUnlockFailuresBurst {
		_, failures := world.callWith(test, world.ada, unlockPasswordsQuery, map[string]any{"proof": wrongProof})
		if message := strings.Join(failures, "; "); !strings.Contains(message, "That passphrase is not right.") {
			test.Fatalf("a wrong proof: %s", message)
		}
	}

	second, err := New(world.database, world.graph.config, world.files, nil, nil, nil, nil, world.graph.authenticator, nil, world.graph.settings)
	if err != nil {
		test.Fatalf("New: %s", err)
	}
	first := world.graph
	world.graph = second.(*graph)
	defer func() { world.graph = first }()
	if message := world.refusal(test, world.ada, unlockPasswordsQuery, map[string]any{"proof": rightProof}); !strings.Contains(message, "too many attempts") {
		test.Fatalf("the right proof on a second instance after the tries were spent on the first: %s", message)
	}
}
