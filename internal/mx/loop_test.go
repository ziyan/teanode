package mx

import "testing"

// TestLoopDetectionUsesTheMailServers is the bug this replaced.
//
// A domain whose MX records point back at this server cannot be delivered to:
// the mail would arrive here again and go round. The check used to compare a
// candidate MX host against one name taken from server.name — but the DNS the
// dashboard asks an operator to publish points MX records at
// server.mailServers, which is a separate list. On any deployment where the
// two differ, and the live one does, no MX host ever matched and the check
// never fired.
//
// The first case here fails against the old comparison.
func TestLoopDetectionUsesTheMailServers(t *testing.T) {
	t.Parallel()

	// The shape the dashboard recommends: the server calls itself one thing
	// and receives mail at others.
	exchange := &exchange{settings: &Settings{
		Server:      "mail.example.test",
		MailServers: []string{"mx1.example.test", "mx2.example.test"},
	}}

	served := []string{
		// Exactly one of this server's names, which is what an MX record
		// normally carries and what the old suffix-only check missed.
		"mx1.example.test",
		"mx2.example.test",
		// Trailing dots and case are how DNS actually hands them over.
		"MX1.Example.Test.",
		// Something beneath one of the names, which the old check did catch.
		"a.mx1.example.test",
	}
	for _, host := range served {
		if !exchange.servedHere(host) {
			t.Errorf("%q names this server and should be refused as a loop", host)
		}
	}

	elsewhere := []string{
		"mx1.example.com",
		"mx.elsewhere.example",
		// The server's own name is not an MX target here, and a domain
		// pointing at it is somebody else's problem to explain — but it is
		// also not one of the names mail arrives at, so it is not a loop.
		"example.test",
		"",
	}
	for _, host := range elsewhere {
		if exchange.servedHere(host) {
			t.Errorf("%q is not this server and should be delivered to", host)
		}
	}
}

// With no mail servers configured, the server's own name is the one name mail
// arrives at, which is what Configuration.MailServers() returns and what
// cmd/run.go passes in. The check has to work on that shape too, because it is
// the common single-name deployment.
func TestLoopDetectionOnASingleNameDeployment(t *testing.T) {
	t.Parallel()

	exchange := &exchange{settings: &Settings{
		Server:      "mail.example.test",
		MailServers: []string{"mail.example.test"},
	}}

	if !exchange.servedHere("mail.example.test") {
		t.Error("the server's own name should be refused as a loop")
	}
	if exchange.servedHere("mail.example.com") {
		t.Error("another server's name should be delivered to")
	}
}
