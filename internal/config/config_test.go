package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
)

// writeValidConfiguration writes a configuration that passes validation into a
// temporary directory, along with the DKIM key file that validation insists
// on, and returns the path to it.
func writeValidConfiguration(t *testing.T, mutate func(*config.Configuration)) (string, *config.Configuration) {
	t.Helper()

	directory := t.TempDir()

	configuration := config.Example()
	configuration.Server.DataDirectory = directory
	if mutate != nil {
		mutate(configuration)
	}

	filename := filepath.Join(directory, "teanode.yaml")
	if err := config.Save(filename, configuration); err != nil {
		t.Fatalf("failed to save configuration: %s", err)
	}
	return filename, configuration
}

// openValidStore builds the same configuration as writeValidConfiguration and
// puts it in a store, for the tests whose subject is the store rather than
// the file.
func openValidStore(t *testing.T, mutate func(*config.Configuration)) (config.Store, *config.Configuration) {
	t.Helper()

	_, configuration := writeValidConfiguration(t, mutate)
	store := config.NewMemoryStore(configuration)
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store, configuration
}

func TestExampleIsValid(t *testing.T) {
	_, configuration := writeValidConfiguration(t, nil)
	if err := configuration.Validate(); err != nil {
		t.Fatalf("the example configuration should be valid, got: %s", err)
	}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	filename, saved := writeValidConfiguration(t, func(configuration *config.Configuration) {
		configuration.SMTP.MaxMessageSize = 70 * 1024 * 1024
		configuration.Storage.SpoolRetention = config.Duration(30 * 24 * time.Hour)
	})

	loaded, err := config.Load(filename)
	if err != nil {
		t.Fatalf("failed to load configuration: %s", err)
	}
	if loaded.Server.Name != saved.Server.Name {
		t.Errorf("server.name did not round trip: %q != %q", loaded.Server.Name, saved.Server.Name)
	}
	if loaded.SMTP.MaxMessageSize != saved.SMTP.MaxMessageSize {
		t.Errorf("smtp.maxMessageSize did not round trip: %s != %s", loaded.SMTP.MaxMessageSize, saved.SMTP.MaxMessageSize)
	}
	if loaded.Storage.SpoolRetention != saved.Storage.SpoolRetention {
		t.Errorf("storage.spoolRetention did not round trip: %s != %s", loaded.Storage.SpoolRetention, saved.Storage.SpoolRetention)
	}
}

func TestSavedFileIsPrivate(t *testing.T) {
	filename, _ := writeValidConfiguration(t, nil)
	info, err := os.Stat(filename)
	if err != nil {
		t.Fatalf("failed to stat configuration: %s", err)
	}
	// The file holds signing keys, credential keys and the server secret.
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("configuration file mode is %o, want 600", mode)
	}
}

func TestSavedFileKeepsExplanatoryHeader(t *testing.T) {
	filename, _ := writeValidConfiguration(t, nil)
	content, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("failed to read configuration: %s", err)
	}
	if !strings.HasPrefix(string(content), "# TeaNode configuration.") {
		t.Errorf("configuration file lost its header, starts with: %.40q", content)
	}
	// A file that is no longer read at startup has to say so, or an operator
	// edits it, restarts, and cannot see why nothing changed.
	if !strings.Contains(string(content), "config import") {
		t.Error("the header should tell the reader how to load this file back")
	}
	if !strings.Contains(string(content), "not read at startup") {
		t.Error("the header should say that editing the file changes nothing")
	}
}

func TestUnknownFieldIsRejected(t *testing.T) {
	_, err := config.Parse([]byte("server:\n  nmae: mail.example.com\n"))
	if err == nil {
		t.Fatal("a misspelled field should be an error, not silently ignored")
	}
	if !strings.Contains(err.Error(), "nmae") {
		t.Errorf("the error should name the offending field, got: %s", err)
	}
}

func TestValidationErrors(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*config.Configuration)
		wantPath string
	}{
		{
			name:     "missing server name",
			mutate:   func(configuration *config.Configuration) { configuration.Server.Name = "" },
			wantPath: "server.name",
		},
		{
			name: "dns-01 without a provider",
			mutate: func(configuration *config.Configuration) {
				configuration.TLS.ACME.Challenge = config.ChallengeDNS01
			},
			wantPath: "tls.acme.route53.enabled",
		},
		{
			name: "wildcard without dns-01",
			mutate: func(configuration *config.Configuration) {
				configuration.TLS.Hosts = append(configuration.TLS.Hosts, "*.example.com")
			},
			wantPath: "tls.hosts[1]",
		},
		{
			name: "colliding listen addresses",
			mutate: func(configuration *config.Configuration) {
				configuration.Listen.HTTP = configuration.Listen.SMTPIncoming
			},
			wantPath: "listen.",
		},
		{
			name: "no certificate source at all",
			mutate: func(configuration *config.Configuration) {
				configuration.TLS.ACME.Enabled = false
			},
			wantPath: "tls.acme.enabled",
		},
		{
			name: "geoip enabled without a database",
			mutate: func(configuration *config.Configuration) {
				configuration.GeoIP.Enabled = true
			},
			wantPath: "geoip.databaseFile",
		},
		{
			name: "s3 enabled without a bucket",
			mutate: func(configuration *config.Configuration) {
				configuration.Storage.S3.Enabled = true
			},
			wantPath: "storage.s3.bucket",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configuration := config.Example()
			configuration.Server.DataDirectory = t.TempDir()
			test.mutate(configuration)

			err := configuration.Validate()
			if err == nil {
				t.Fatalf("expected a validation error mentioning %q", test.wantPath)
			}
			if !strings.Contains(err.Error(), test.wantPath) {
				t.Errorf("expected an error mentioning %q, got:\n%s", test.wantPath, err)
			}
		})
	}
}

func TestStoreUpdateAppliesTheChange(t *testing.T) {
	store, _ := openValidStore(t, nil)

	var notified int
	unsubscribe := store.Subscribe(func(*config.Configuration) { notified++ })
	defer unsubscribe()

	if err := store.Update(func(configuration *config.Configuration) error {
		configuration.SMTP.MaxRecipientsIncoming = 42
		return nil
	}); err != nil {
		t.Fatalf("failed to update configuration: %s", err)
	}

	if notified != 1 {
		t.Errorf("subscribers notified %d times, want 1", notified)
	}
	if store.Current().SMTP.MaxRecipientsIncoming != 42 {
		t.Error("the change is not in the active configuration")
	}

	// That the change also survives being stored and read back is covered
	// against a real database, in internal/db.
}

func TestStoreUpdateRollsBackOnInvalidChange(t *testing.T) {
	store, _ := openValidStore(t, nil)
	original := store.Current().Server.Name

	err := store.Update(func(configuration *config.Configuration) error {
		configuration.Server.Name = ""
		return nil
	})
	if err == nil {
		t.Fatal("an invalid change should be refused")
	}

	if got := store.Current().Server.Name; got != original {
		t.Errorf("the active configuration was modified by a failed update: %q, want %q", got, original)
	}
}

func TestDurationParsing(t *testing.T) {
	tests := map[string]time.Duration{
		"0s":  0,
		"30s": 30 * time.Second,
		"5m":  5 * time.Minute,
		"12h": 12 * time.Hour,
		"30d": 30 * 24 * time.Hour,
	}
	for value, want := range tests {
		got, err := config.ParseDuration(value)
		if err != nil {
			t.Errorf("failed to parse %q: %s", value, err)
			continue
		}
		if got.Duration() != want {
			t.Errorf("%q parsed to %s, want %s", value, got.Duration(), want)
		}
		// Days are preferred on the way out so that 30d does not become 720h.
		if formatted := got.String(); formatted != value {
			t.Errorf("%q formatted back as %q", value, formatted)
		}
	}
	if _, err := config.ParseDuration("soon"); err == nil {
		t.Error("an unparseable duration should be an error")
	}
}

func TestByteSizeParsing(t *testing.T) {
	tests := map[string]uint64{
		"70MB":    70 * 1024 * 1024,
		"1G":      1024 * 1024 * 1024,
		"512KB":   512 * 1024,
		"1048576": 1024 * 1024,
	}
	for value, want := range tests {
		got, err := config.ParseByteSize(value)
		if err != nil {
			t.Errorf("failed to parse %q: %s", value, err)
			continue
		}
		if got.Bytes() != want {
			t.Errorf("%q parsed to %d, want %d", value, got.Bytes(), want)
		}
	}
	if _, err := config.ParseByteSize("huge"); err == nil {
		t.Error("an unparseable size should be an error")
	}
}

// TestValidateFiles covers the checks that look at the filesystem. They are
// separate from Validate so that "teanode dkim generate" can load a
// configuration whose key it has not created yet.
func TestValidateFiles(t *testing.T) {
	_, configuration := writeValidConfiguration(t, nil)

	if err := configuration.ValidateFiles(); err != nil {
		t.Fatalf("the example configuration refers to files that exist, got: %s", err)
	}

	configuration.GeoIP.Enabled = true
	configuration.GeoIP.DatabaseFile = "absent.mmdb"
	err := configuration.ValidateFiles()
	if err == nil {
		t.Fatal("a missing GeoIP database should be reported")
	}
	if !strings.Contains(err.Error(), "geoip.databaseFile") {
		t.Errorf("expected an error mentioning geoip.databaseFile, got: %s", err)
	}
	// Structural validation must still pass: a missing file is an environment
	// problem, not a malformed configuration.
	if err := configuration.Validate(); err != nil {
		t.Errorf("a missing file is not a structural problem, got: %s", err)
	}
}

// TestRelativePathsResolveAgainstTheConfigurationFile guards a subtle failure:
// if a relative server.dataDirectory resolved against the process working
// directory, then "teanode credential list" run from elsewhere would look for
// the server secret in the wrong place, silently generate a second one, and
// print SMTP passwords that do not work.
func TestRelativePathsResolveAgainstTheConfigurationFile(t *testing.T) {
	directory := t.TempDir()
	if err := os.MkdirAll(filepath.Join(directory, "state"), 0o700); err != nil {
		t.Fatalf("failed to create state directory: %s", err)
	}
	configuration := config.Example()
	configuration.Server.DataDirectory = "state"

	filename := filepath.Join(directory, "teanode.yaml")
	if err := config.Save(filename, configuration); err != nil {
		t.Fatalf("failed to save configuration: %s", err)
	}

	loaded, err := config.Load(filename)
	if err != nil {
		t.Fatalf("failed to load configuration: %s", err)
	}

	want := filepath.Join(directory, "state")
	if got := loaded.DataDirectory(); got != want {
		t.Errorf("data directory resolved to %q, want %q", got, want)
	}
	if got := loaded.Path("teanode1.key"); got != filepath.Join(want, "teanode1.key") {
		t.Errorf("key path resolved to %q", got)
	}

	// An absolute path is left alone, so an operator can keep state elsewhere.
	loaded.Server.DataDirectory = "/var/lib/teanode"
	if got := loaded.DataDirectory(); got != "/var/lib/teanode" {
		t.Errorf("an absolute data directory should not be rewritten, got %q", got)
	}
}

// TestSecretsAreGeneratedOnceAndReused covers the property that matters: the
// server secret signs every SMTP password, so regenerating it would silently
// stop every configured device from being able to send.
func TestSecretsAreGeneratedOnceAndReused(t *testing.T) {
	store, _ := openValidStore(t, nil)

	if err := config.EnsureSecrets(store); err != nil {
		t.Fatalf("failed to generate secrets: %s", err)
	}
	secret := string(store.Current().Secret())
	sessionKey := string(store.Current().SessionKey())

	if len(secret) < 32 || len(sessionKey) < 32 {
		t.Fatalf("secrets are too short: %d and %d", len(secret), len(sessionKey))
	}
	if secret == sessionKey {
		t.Error("the server secret and the session key are the same value")
	}

	if err := config.EnsureSecrets(store); err != nil {
		t.Fatalf("failed on the second call: %s", err)
	}
	if string(store.Current().Secret()) != secret {
		t.Error("the server secret changed; every SMTP password would stop working")
	}
	if string(store.Current().SessionKey()) != sessionKey {
		t.Error("the session key changed; everybody would be logged out")
	}
}

// TestSecretIsAdoptedFromTheLegacyFile covers upgrading an installation whose
// secret was a file beside the configuration. Generating a new one instead
// would invalidate every SMTP password on the machine.
func TestSecretIsAdoptedFromTheLegacyFile(t *testing.T) {
	_, configuration := writeValidConfiguration(t, nil)

	const existing = "an-existing-secret-from-an-older-release"
	if err := os.WriteFile(filepath.Join(configuration.DataDirectory(), "teanode.secret"), []byte(existing), 0o600); err != nil {
		t.Fatalf("failed to write the legacy secret: %s", err)
	}

	store := config.NewMemoryStore(configuration)
	defer func() {
		_ = store.Close()
	}()

	if err := config.EnsureSecrets(store); err != nil {
		t.Fatalf("failed to adopt secrets: %s", err)
	}
	if got := string(store.Current().Secret()); got != existing {
		t.Errorf("the secret is %q, want the adopted %q", got, existing)
	}
}

// TestUpdateThatReadsBeforeWriting guards a bug that made a domain created
// through the dashboard invisible until the process restarted.
//
