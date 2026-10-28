package server

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
)

// A renewal by any instance writes the certificate and the keys into the
// shared configuration, and must not ask every instance for a restart.
func TestARenewedCertificateIsNotAStartupOnlyChange(test *testing.T) {
	before := config.Default()
	after := config.Default()
	after.TLS.ACME.Certificate = "a certificate renewed by another instance"
	after.TLS.ACME.PrivateKey = "the key that goes with it"
	after.TLS.ACME.AccountKey = "an account key registered meanwhile"

	changed := changedStartupOnly(before, after)
	if len(changed) != 0 {
		test.Fatalf("a renewal asks for a restart of %v", changed)
	}

	after.TLS.ACME.Email = "someone@example.com"
	if changed := changedStartupOnly(before, after); len(changed) != 1 || changed[0] != "tls" {
		test.Fatalf("a changed contact address asks for a restart of %v, want tls", changed)
	}
}

// Settings the listeners and the exchange read once say a restart is needed.
func TestSettingsReadOnceAtStartAreStartupOnly(test *testing.T) {
	for name, change := range map[string]func(*config.Configuration){
		"smtp.disableSend": func(configuration *config.Configuration) {
			configuration.SMTP.DisableSend = !configuration.SMTP.DisableSend
		},
		"smtp.maxMessageSize":        func(configuration *config.Configuration) { configuration.SMTP.MaxMessageSize += 1024 },
		"smtp.maxRecipientsIncoming": func(configuration *config.Configuration) { configuration.SMTP.MaxRecipientsIncoming++ },
		"smtp.maxRecipientsOutgoing": func(configuration *config.Configuration) { configuration.SMTP.MaxRecipientsOutgoing++ },
		"smtp.trustedSenders": func(configuration *config.Configuration) {
			configuration.SMTP.TrustedSenders = append(configuration.SMTP.TrustedSenders, "198.51.100.7")
		},
		"smtp.greylistDelay": func(configuration *config.Configuration) {
			configuration.SMTP.GreylistDelay += config.Duration(time.Second)
		},
		"smtp.requireReverseDns": func(configuration *config.Configuration) {
			configuration.SMTP.RequireReverseDNS = !configuration.SMTP.RequireReverseDNS
		},
		"smtp.authRateLimit": func(configuration *config.Configuration) { configuration.SMTP.AuthRateLimit++ },
		"smtp.authRateBurst": func(configuration *config.Configuration) { configuration.SMTP.AuthRateBurst++ },
		"dns.nameserver":     func(configuration *config.Configuration) { configuration.DNS.Nameserver = "192.0.2.53:53" },
		"dns.checkInterval": func(configuration *config.Configuration) {
			configuration.DNS.CheckInterval += config.Duration(time.Minute)
		},
		"passkey.redis": func(configuration *config.Configuration) {
			configuration.Passkey.Redis.Address = "redis.example.com:6379"
		},
		"server.name": func(configuration *config.Configuration) { configuration.Server.Name = "other.example.com" },
	} {
		before := config.Default()
		after := config.Default()
		change(after)
		if changed := changedStartupOnly(before, after); len(changed) != 1 || changed[0] != name {
			test.Errorf("changing %s asks for a restart of %v", name, changed)
		}
	}
}
