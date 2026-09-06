package config_test

import (
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/config"
)

// An upgrade must not move a server onto a different spam filter behind the
// operator's back. A deployment that has been talking to a daemon has a host
// stored and no engine, because the engine did not exist when it was
// configured; it has to keep talking to that daemon.
func TestAnExistingDaemonKeepsWorking(t *testing.T) {
	t.Parallel()

	stored := config.Antispam{Enabled: true, Host: "spamassassin", Port: 783}
	if engine := stored.ResolvedEngine(); engine != config.AntispamEngineSpamd {
		t.Errorf("ResolvedEngine() = %q, want spamd for a configuration that names a host", engine)
	}
	if host, port := stored.SpamdHost(), stored.SpamdPort(); host != "spamassassin" || port != 783 {
		t.Errorf("SpamdHost(), SpamdPort() = %q, %d; want the deprecated fields to still be read", host, port)
	}
}

// A new installation has no host anywhere, and gets the filter that needs no
// second program.
func TestANewInstallationGetsTheBuiltinFilter(t *testing.T) {
	t.Parallel()

	empty := config.Antispam{Enabled: true}
	if engine := empty.ResolvedEngine(); engine != config.AntispamEngineBuiltin {
		t.Errorf("ResolvedEngine() = %q, want builtin when no host is configured", engine)
	}
}

// An explicit choice wins over the guess, in both directions.
func TestAnExplicitEngineWins(t *testing.T) {
	t.Parallel()

	withHost := config.Antispam{Enabled: true, Engine: config.AntispamEngineBuiltin, Host: "spamassassin"}
	if engine := withHost.ResolvedEngine(); engine != config.AntispamEngineBuiltin {
		t.Errorf("ResolvedEngine() = %q, want the explicit builtin even though a host is set", engine)
	}

	withoutHost := config.Antispam{Enabled: true, Engine: config.AntispamEngineSpamd}
	if engine := withoutHost.ResolvedEngine(); engine != config.AntispamEngineSpamd {
		t.Errorf("ResolvedEngine() = %q, want the explicit spamd", engine)
	}
}

// The current field wins over the deprecated one when both are set, so that
// editing through the dashboard actually takes effect.
func TestTheCurrentFieldWinsOverTheDeprecatedOne(t *testing.T) {
	t.Parallel()

	both := config.Antispam{
		Enabled: true,
		Spamd:   config.AntispamSpamd{Host: "new.example.com", Port: 7830},
		Host:    "old.example.com",
		Port:    783,
	}
	if host := both.SpamdHost(); host != "new.example.com" {
		t.Errorf("SpamdHost() = %q, want the current field", host)
	}
	if port := both.SpamdPort(); port != 7830 {
		t.Errorf("SpamdPort() = %d, want the current field", port)
	}
}

// Choosing spamd without saying where it is has to be refused, and choosing
// the built-in filter must not demand a host.
//
// A default configuration is not otherwise complete — it has no domains and
// no server name — so these look for the antispam complaints among whatever
// else Validate has to say, rather than for overall validity.
func TestValidation(t *testing.T) {
	t.Parallel()

	complaints := func(change func(*config.Configuration)) string {
		configuration := config.Default()
		configuration.Antispam.Enabled = true
		change(configuration)
		err := configuration.Validate()
		if err == nil {
			return ""
		}
		return err.Error()
	}

	missingHost := complaints(func(configuration *config.Configuration) {
		configuration.Antispam.Engine = config.AntispamEngineSpamd
		configuration.Antispam.Spamd = config.AntispamSpamd{}
		configuration.Antispam.Host = ""
		configuration.Antispam.Port = 0
	})
	if !strings.Contains(missingHost, "antispam.spamd.host") {
		t.Errorf("expected a complaint about antispam.spamd.host, got: %s", missingHost)
	}

	builtin := complaints(func(configuration *config.Configuration) {
		configuration.Antispam.Engine = config.AntispamEngineBuiltin
		configuration.Antispam.Spamd = config.AntispamSpamd{}
	})
	if strings.Contains(builtin, "antispam.") {
		t.Errorf("the built-in filter should need no antispam settings, got: %s", builtin)
	}

	unknown := complaints(func(configuration *config.Configuration) {
		configuration.Antispam.Engine = "rspamd"
	})
	if !strings.Contains(unknown, "antispam.engine") {
		t.Errorf("expected a complaint about antispam.engine, got: %s", unknown)
	}
}
