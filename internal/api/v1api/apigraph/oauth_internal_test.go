package apigraph

import (
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
)

// An approved token is good for the agent tools and nothing else.
//
// A program that names no resource used to get a token good for the whole
// API, since that is what an empty resource means on a token minted by hand.
// And one that named the management API would have been handed a token for it
// by a page that only promised the agent tools.
func TestAnApprovalIsOnlyEverForTheAgentTools(test *testing.T) {
	resource, err := agentToolsResource("")
	if err != nil || resource != api.PathAgentMCP {
		test.Errorf("no resource became %q, %v; it should be the agent tools", resource, err)
	}
	for _, allowed := range []string{
		"https://mail.example.com" + api.PathAgentMCP,
		"https://mail.example.com:10443" + api.PathAgentMCP,
	} {
		if resource, err := agentToolsResource(allowed); err != nil || resource != allowed {
			test.Errorf("%q was refused: %v", allowed, err)
		}
	}
	for _, refused := range []string{
		"https://mail.example.com" + api.PathGraphQL,
		"https://mail.example.com/",
		"https://mail.example.com" + api.PathAgentMCP + "/../graphql",
		"not an address at all %",
	} {
		if _, err := agentToolsResource(refused); err == nil {
			test.Errorf("%q was accepted", refused)
		}
	}
}

// A loopback address matches whatever its port, and nothing else is loosened.
//
// A program on somebody's machine listens on whichever port is free, so the
// port it registered with is rarely the one it uses the next time it asks.
func TestALoopbackRedirectMatchesOnAnyPort(test *testing.T) {
	client := &models.OAuthClient{RedirectURIs: []string{
		"http://127.0.0.1:53121/callback",
		"https://harness.example.com/callback",
	}}
	for _, allowed := range []string{
		"http://127.0.0.1:53121/callback",
		"http://127.0.0.1:61000/callback",
		"http://127.0.0.1/callback",
		"https://harness.example.com/callback",
	} {
		if !allowsRedirect(client, allowed) {
			test.Errorf("%q was refused", allowed)
		}
	}
	for _, refused := range []string{
		"http://127.0.0.1:61000/elsewhere",
		"http://localhost:61000/callback",
		"https://127.0.0.1:61000/callback",
		"http://127.0.0.1:61000/callback?extra=1",
		"https://harness.example.com:8443/callback",
		"http://evil.example.com:53121/callback",
		"http://user@127.0.0.1:61000/callback",
	} {
		if allowsRedirect(client, refused) {
			test.Errorf("%q was allowed", refused)
		}
	}
}
