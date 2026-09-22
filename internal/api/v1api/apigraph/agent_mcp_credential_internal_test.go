package apigraph

import (
	"testing"

	agenttools "github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/all"
)

// Every tool withheld from a program is a tool that exists.
//
// The list is by name, so a tool renamed later would drop out of it without
// anything failing, and the program would quietly be able to mint a
// credential again. The catalog is checked both as its tools are listed and
// under the names a grouped tool hides, since the catalog offers some as one
// tool with an action.
func TestEveryWithheldCredentialToolExists(test *testing.T) {
	catalog := agenttools.Build()
	known := map[string]bool{}
	for _, tool := range catalog.All() {
		known[tool.Name] = true
	}
	for name := range credentialTools {
		if !known[name] && catalog.Get(name) == nil {
			test.Errorf("%q is withheld from programs but is not a tool", name)
		}
	}
}
