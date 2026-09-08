package config

import "testing"

// What a mail program is told is what it can reach, which is not what this
// server binds when something in front forwards a different port. The
// dashboard used to report the listen ports, so a deployment publishing
// 10993 told everybody to connect to 10993.
func TestWhatAMailProgramIsToldFollowsTheServerUntilItIsSet(test *testing.T) {
	test.Parallel()

	configuration := &Configuration{}
	configuration.Server.Name = "mail.example.com"
	configuration.Listen.IMAP = ":10143"
	configuration.Listen.IMAPS = ":10993"

	if host := configuration.IMAPHost(); host != "mail.example.com" {
		test.Errorf("IMAPHost() = %q, want the server's own name", host)
	}
	if port := configuration.IMAPPort(); port != "10143" {
		test.Errorf("IMAPPort() = %q, want the port it listens on", port)
	}
	if port := configuration.IMAPTLSPort(); port != "10993" {
		test.Errorf("IMAPTLSPort() = %q, want the port it listens on", port)
	}

	// A gateway takes the usual ports on the outside and forwards them here.
	configuration.IMAP.Host = "imap.example.com"
	configuration.IMAP.Port = 143
	configuration.IMAP.TLSPort = 993

	if host := configuration.IMAPHost(); host != "imap.example.com" {
		test.Errorf("IMAPHost() = %q, want what was configured", host)
	}
	if port := configuration.IMAPPort(); port != "143" {
		test.Errorf("IMAPPort() = %q, want what was configured", port)
	}
	if port := configuration.IMAPTLSPort(); port != "993" {
		test.Errorf("IMAPTLSPort() = %q, want what was configured", port)
	}
}

// A listener that is off says so by having no port, rather than reporting a
// number nothing answers on.
func TestAListenerThatIsOffHasNoPortToTell(test *testing.T) {
	test.Parallel()

	configuration := &Configuration{}
	configuration.Server.Name = "mail.example.com"
	if port := configuration.IMAPPort(); port != "" {
		test.Errorf("IMAPPort() = %q with nothing listening, want empty", port)
	}
	if port := configuration.IMAPTLSPort(); port != "" {
		test.Errorf("IMAPTLSPort() = %q with nothing listening, want empty", port)
	}
}
