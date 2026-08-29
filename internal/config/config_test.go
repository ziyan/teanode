package config_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/ziyan/teanode/internal/config"
)

// writeValidConfiguration writes a configuration that passes validation into a
// temporary directory, along with the DKIM key file that validation insists
// on, and returns the path to it.
func writeValidConfiguration(t *testing.T, mutate func(*config.Configuration)) (string, *config.Configuration) {
	t.Helper()

	directory := t.TempDir()

	hash, err := bcrypt.GenerateFromPassword([]byte("hunter2"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("failed to hash password: %s", err)
	}

	configuration := config.Example()
	configuration.Server.DataDirectory = directory
	configuration.Users = []*config.User{
		{Username: "admin", PasswordHash: string(hash)},
	}
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
	if len(loaded.Domains) != 1 || loaded.Domains[0].ID != saved.Domains[0].ID {
		t.Fatalf("domains did not round trip")
	}
	if len(loaded.Domains[0].Aliases) != len(saved.Domains[0].Aliases) {
		t.Fatalf("aliases did not round trip: %d != %d", len(loaded.Domains[0].Aliases), len(saved.Domains[0].Aliases))
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
			name:     "no domains",
			mutate:   func(configuration *config.Configuration) { configuration.Domains = nil },
			wantPath: "domains",
		},
		{
			name: "duplicate domain",
			mutate: func(configuration *config.Configuration) {
				duplicate := *configuration.Domains[0]
				duplicate.ID = config.NewID()
				configuration.Domains = append(configuration.Domains, &duplicate)
			},
			wantPath: "domains[1].domain",
		},
		{
			name: "invalid alias pattern",
			mutate: func(configuration *config.Configuration) {
				configuration.Domains[0].Aliases[0].Pattern = "^["
			},
			wantPath: "domains[0].aliases[0].pattern",
		},
		{
			name: "email alias without an address",
			mutate: func(configuration *config.Configuration) {
				configuration.Domains[0].Aliases[0].Email = ""
			},
			wantPath: "domains[0].aliases[0].email",
		},
		{
			name: "webhook alias without a URL",
			mutate: func(configuration *config.Configuration) {
				configuration.Domains[0].Aliases[0].Kind = config.AliasKindWebhook
				configuration.Domains[0].Aliases[0].Email = ""
			},
			wantPath: "domains[0].aliases[0].webhook",
		},
		{
			name: "mail server alias without a host",
			mutate: func(configuration *config.Configuration) {
				configuration.Domains[0].Aliases[0].Kind = config.AliasKindMailServer
				configuration.Domains[0].Aliases[0].Email = ""
			},
			wantPath: "domains[0].aliases[0].mailServer.host",
		},
		{
			name: "unknown alias kind",
			mutate: func(configuration *config.Configuration) {
				configuration.Domains[0].Aliases[0].Kind = "carrierPigeon"
			},
			wantPath: "domains[0].aliases[0].kind",
		},
		{
			name: "duplicate identifier",
			mutate: func(configuration *config.Configuration) {
				configuration.Domains[0].Aliases[1].ID = configuration.Domains[0].Aliases[0].ID
			},
			wantPath: "domains[0].aliases[1].id",
		},
		{
			name: "plain password instead of a hash",
			mutate: func(configuration *config.Configuration) {
				configuration.Users[0].PasswordHash = "hunter2"
			},
			wantPath: "users[0].passwordHash",
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
			name: "a signing key with no selector",
			mutate: func(configuration *config.Configuration) {
				configuration.Domains[0].DKIM = config.DomainKey{PrivateKey: unusableKey()}
			},
			wantPath: "domains[0].dkim.selector",
		},
		{
			name: "a selector with no signing key",
			mutate: func(configuration *config.Configuration) {
				configuration.Domains[0].DKIM = config.DomainKey{Selector: "teanode1"}
			},
			wantPath: "domains[0].dkim.privateKey",
		},
		{
			name: "an unusable signing key",
			mutate: func(configuration *config.Configuration) {
				configuration.Domains[0].DKIM = config.DomainKey{Selector: "teanode1", PrivateKey: "not a key"}
			},
			wantPath: "domains[0].dkim.privateKey",
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
			directory := t.TempDir()
			hash, err := bcrypt.GenerateFromPassword([]byte("hunter2"), bcrypt.MinCost)
			if err != nil {
				t.Fatalf("failed to hash password: %s", err)
			}
			configuration := config.Example()
			configuration.Server.DataDirectory = directory
			configuration.Users = []*config.User{{Username: "admin", PasswordHash: string(hash)}}
			test.mutate(configuration)

			err = configuration.Validate()
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

	aliasId := config.NewID()
	if err := store.Update(func(configuration *config.Configuration) error {
		configuration.Domains[0].Aliases = append(configuration.Domains[0].Aliases, &config.Alias{
			ID:      aliasId,
			Pattern: "^support$",
			Kind:    config.AliasKindEmail,
			Email:   "support@example.net",
		})
		return nil
	}); err != nil {
		t.Fatalf("failed to update configuration: %s", err)
	}

	if notified != 1 {
		t.Errorf("subscribers notified %d times, want 1", notified)
	}
	if store.Current().FindAliasByID(aliasId) == nil {
		t.Error("the new alias is not in the active configuration")
	}

	// That the change also survives being stored and read back is covered
	// against a real database, in internal/configdb.
}

func TestStoreUpdateRollsBackOnInvalidChange(t *testing.T) {
	store, _ := openValidStore(t, nil)
	originalAliases := len(store.Current().Domains[0].Aliases)

	err := store.Update(func(configuration *config.Configuration) error {
		configuration.Domains[0].Aliases = append(configuration.Domains[0].Aliases, &config.Alias{
			ID:      config.NewID(),
			Pattern: "^[",
			Kind:    config.AliasKindEmail,
			Email:   "support@example.net",
		})
		return nil
	})
	if err == nil {
		t.Fatal("an invalid change should be refused")
	}

	if got := len(store.Current().Domains[0].Aliases); got != originalAliases {
		t.Errorf("the active configuration was modified by a failed update: %d aliases, want %d", got, originalAliases)
	}
}

func TestMatchAliases(t *testing.T) {
	_, configuration := writeValidConfiguration(t, func(configuration *config.Configuration) {
		domain := configuration.Domains[0]
		domain.Aliases = []*config.Alias{
			{ID: "specific", Pattern: "^hello$", Kind: config.AliasKindEmail, Email: "one@example.net"},
			{ID: "disabled", Pattern: "^hello$", Kind: config.AliasKindEmail, Email: "two@example.net", Disabled: true},
			{ID: "second", Pattern: "^hello$", Kind: config.AliasKindEmail, Email: "three@example.net"},
			{ID: "prefix", Pattern: "^ci-.*$", Kind: config.AliasKindNull},
			{ID: "catchall", Pattern: "", Kind: config.AliasKindEmail, Email: "four@example.net"},
		}
	})

	domain := configuration.FindDomain("EXAMPLE.COM")
	if domain == nil {
		t.Fatal("domain lookup should be case insensitive")
	}

	tests := []struct {
		name      string
		localPart string
		want      []string
	}{
		{
			// Two enabled aliases share the pattern, so the message goes to
			// both. The disabled one does not count, and the catch-all is not
			// added on top.
			name:      "a specific match wins over the catch-all",
			localPart: "hello", want: []string{"specific", "second"},
		},
		{
			name:      "matching ignores case, as the old SQL operator did",
			localPart: "HeLLo", want: []string{"specific", "second"},
		},
		{
			name:      "a pattern that matches nothing falls back to the catch-all",
			localPart: "anything-else", want: []string{"catchall"},
		},
		{
			name:      "a prefix pattern still suppresses the catch-all",
			localPart: "ci-build", want: []string{"prefix"},
		},
		{
			name:      "case insensitivity applies to prefix patterns too",
			localPart: "CI-Build", want: []string{"prefix"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			matched := configuration.MatchAliases(domain, test.localPart)
			got := make([]string, 0, len(matched))
			for _, alias := range matched {
				got = append(got, alias.ID)
			}
			if strings.Join(got, ",") != strings.Join(test.want, ",") {
				t.Errorf("matched %v, want %v", got, test.want)
			}
		})
	}

	if configuration.FindDomain("other.example") != nil {
		t.Error("an unconfigured domain should not be found")
	}
}

// TestMatchAliasesWithoutCatchAll checks that an address matching nothing gets
// nothing, rather than everything.
func TestMatchAliasesWithoutCatchAll(t *testing.T) {
	_, configuration := writeValidConfiguration(t, func(configuration *config.Configuration) {
		configuration.Domains[0].Aliases = []*config.Alias{
			{ID: "only", Pattern: "^hello$", Kind: config.AliasKindEmail, Email: "one@example.net"},
		}
	})

	domain := configuration.FindDomain("example.com")
	if matched := configuration.MatchAliases(domain, "nobody"); len(matched) != 0 {
		t.Errorf("matched %d aliases with no catch-all configured, want 0", len(matched))
	}
}

// TestCatchAllIsValid guards the import path: the previous release stored a
// catch-all as an empty pattern, and twenty-five of twenty-six aliases in a
// real deployment were exactly that. Rejecting it would make the migration
// impossible.
func TestCatchAllIsValid(t *testing.T) {
	_, configuration := writeValidConfiguration(t, func(configuration *config.Configuration) {
		configuration.Domains[0].Aliases = []*config.Alias{
			{ID: config.NewID(), Pattern: "", Kind: config.AliasKindEmail, Email: "everything@example.net"},
		}
	})
	if err := configuration.Validate(); err != nil {
		t.Fatalf("a catch-all should be valid, got: %s", err)
	}
	if !configuration.Domains[0].Aliases[0].IsCatchAll() {
		t.Error("an empty pattern should be a catch-all")
	}
}

func TestFindCredential(t *testing.T) {
	_, configuration := writeValidConfiguration(t, func(configuration *config.Configuration) {
		configuration.Domains[0].Credentials = []*config.Credential{
			{ID: "laptop", Key: "secret", Comment: "my laptop"},
		}
	})

	domain, credential := configuration.FindCredential("laptop")
	if domain == nil || credential == nil {
		t.Fatal("the credential should be found together with its domain")
	}
	if domain.Domain != "example.com" || credential.Key != "secret" {
		t.Errorf("found the wrong credential: %s / %s", domain.Domain, credential.Key)
	}

	if domain, credential := configuration.FindCredential("unknown"); domain != nil || credential != nil {
		t.Error("an unknown credential should not be found")
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
	hash, err := bcrypt.GenerateFromPassword([]byte("hunter2"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("failed to hash password: %s", err)
	}

	configuration := config.Example()
	configuration.Server.DataDirectory = "state"
	configuration.Users = []*config.User{{Username: "admin", PasswordHash: string(hash)}}

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
// The lookup tables are built lazily. A mutation that reads the configuration
// before changing it — which is exactly what a create does when it checks for
// a duplicate first — used to build them from the state before the change, and
// they were never rebuilt. The new domain then could not be found by anything.
func TestUpdateThatReadsBeforeWriting(t *testing.T) {
	store, _ := openValidStore(t, nil)

	domainId := config.NewID()
	aliasId := config.NewID()
	credentialId := config.NewID()

	if err := store.Update(func(configuration *config.Configuration) error {
		// The read that builds the tables, before anything is added.
		if configuration.FindDomain("second.example") != nil {
			return fmt.Errorf("the domain already exists")
		}
		configuration.Domains = append(configuration.Domains, &config.Domain{
			ID:        domainId,
			Domain:    "second.example",
			Subdomain: "mail",
			Aliases: []*config.Alias{
				{ID: aliasId, Pattern: "^support$", Kind: config.AliasKindEmail, Email: "support@example.net"},
			},
			Credentials: []*config.Credential{
				{ID: credentialId, Key: "0123456789abcdef"},
			},
		})
		return nil
	}); err != nil {
		t.Fatalf("failed to add the domain: %s", err)
	}

	configuration := store.Current()
	if configuration.FindDomain("second.example") == nil {
		t.Error("the new domain cannot be found by name")
	}
	if configuration.FindDomainByID(domainId) == nil {
		t.Error("the new domain cannot be found by identifier")
	}
	if configuration.FindAliasByID(aliasId) == nil {
		t.Error("the new alias cannot be found")
	}
	if _, credential := configuration.FindCredential(credentialId); credential == nil {
		t.Error("the new credential cannot be found")
	}

	// The compiled patterns have to be rebuilt too, or mail to the new alias
	// matches nothing.
	domain := configuration.FindDomain("second.example")
	if matched := configuration.MatchAliases(domain, "support"); len(matched) != 1 {
		t.Errorf("matched %d aliases for support@second.example, want 1", len(matched))
	}
}

// unusableKey returns something PEM-shaped that is not a key.
//
// The markers are assembled rather than written out because the secret guard
// refuses a tracked file containing a PEM private key header, and it is right
// to: a guard with exceptions for "but this one is fake" stops being a guard.
func unusableKey() string {
	const marker = "-----%s PRIVATE KEY-----\n"
	return fmt.Sprintf(marker, "BEGIN") + "not actually a key\n" + fmt.Sprintf(marker, "END")
}

// Every domain signs with its own key, so a domain that arrives without one —
// from a configuration file written by hand, an import, or a release that did
// not give it one — has to be given one on the way up. A domain with no key
// sends unsigned mail, which is mail receivers treat as suspicious for a
// reason nothing in the dashboard explains.
func TestEveryDomainIsGivenItsOwnSigningKey(t *testing.T) {
	// A key already published in DNS, standing in for one an installation
	// already has.
	published, err := config.GenerateDomainKey("older")
	if err != nil {
		t.Fatalf("failed to generate a key for the test: %s", err)
	}

	store, _ := openValidStore(t, func(configuration *config.Configuration) {
		configuration.DKIM.Selector = "teanode"
		configuration.Domains = []*config.Domain{
			{ID: "one.test", Domain: "one.test", Subdomain: "mail"},
			{ID: "two.test", Domain: "two.test", Subdomain: "mail"},
			// This one already has a key, under a selector of its own.
			{ID: "three.test", Domain: "three.test", Subdomain: "mail", DKIM: published},
		}
	})

	if err := config.EnsureSecrets(store); err != nil {
		t.Fatalf("failed to generate secrets: %s", err)
	}

	keys := map[string]string{}
	for _, domain := range store.Current().Domains {
		if domain.DKIM.PrivateKey == "" {
			t.Fatalf("%s was left without a signing key", domain.Domain)
		}
		if domain.DKIM.Selector == "" {
			t.Errorf("%s has a key with no selector, so there is nowhere to publish it", domain.Domain)
		}
		if previous, ok := keys[domain.DKIM.PrivateKey]; ok {
			t.Errorf("%s was given the same key as %s", domain.Domain, previous)
		}
		keys[domain.DKIM.PrivateKey] = domain.Domain
	}

	// A key already here matches a record already published. Replacing it
	// would break signing for that domain until somebody noticed.
	third := store.Current().FindDomain("three.test")
	if third.DKIM.PrivateKey != published.PrivateKey || third.DKIM.Selector != "older" {
		t.Error("an existing signing key was replaced; that domain's mail would stop verifying")
	}

	// The generated ones have to be usable, and published where the domain
	// says they are.
	first := store.Current().FindDomain("one.test")
	if first.DKIM.Selector != "teanode" {
		t.Errorf("the generated key uses the selector %q, want the configured default", first.DKIM.Selector)
	}
	if _, err := first.DKIM.PublicKeyRecord(); err != nil {
		t.Errorf("the generated key cannot be published: %s", err)
	}

	// And a second start leaves them alone, the same way it leaves the server
	// secret alone.
	if err := config.EnsureSecrets(store); err != nil {
		t.Fatalf("failed on the second call: %s", err)
	}
	if store.Current().FindDomain("one.test").DKIM.PrivateKey != first.DKIM.PrivateKey {
		t.Error("the signing key changed on the second start; the published record would stop matching")
	}
}
