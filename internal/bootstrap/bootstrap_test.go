package bootstrap_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/bootstrap"
	"github.com/ziyan/teanode/internal/config"
)

func TestDatabaseURL(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("TEANODE_DATABASE_URL", "postgres://teanode:s3cret@postgres:5433/mail?sslmode=require")

	loaded, err := bootstrap.Load()
	if err != nil {
		t.Fatalf("Load: %s", err)
	}
	if loaded.Database.Host != "postgres" || loaded.Database.Port != 5433 {
		t.Errorf("wrong host and port: %s:%d", loaded.Database.Host, loaded.Database.Port)
	}
	if loaded.Database.User != "teanode" || loaded.Database.Password != "s3cret" {
		t.Errorf("wrong credentials: %s/%s", loaded.Database.User, loaded.Database.Password)
	}
	if loaded.Database.Name != "mail" || loaded.Database.SSLMode != "require" {
		t.Errorf("wrong database and mode: %s/%s", loaded.Database.Name, loaded.Database.SSLMode)
	}
}

// TestDiscreteVariablesWin covers the deployment that has a URL from
// somewhere else and overrides one part of it.
func TestDiscreteVariablesWin(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("TEANODE_DATABASE_URL", "postgres://teanode:s3cret@postgres:5432/mail")
	t.Setenv("TEANODE_DATABASE_HOST", "replica")
	t.Setenv("TEANODE_DATABASE_SSL_MODE", "verify-full")

	loaded, err := bootstrap.Load()
	if err != nil {
		t.Fatalf("Load: %s", err)
	}
	if loaded.Database.Host != "replica" {
		t.Errorf("the discrete host should win, got %q", loaded.Database.Host)
	}
	if loaded.Database.SSLMode != "verify-full" {
		t.Errorf("the discrete mode should win, got %q", loaded.Database.SSLMode)
	}
	if loaded.Database.Password != "s3cret" {
		t.Errorf("the rest of the URL should survive, got %q", loaded.Database.Password)
	}
}

// TestNoDatabaseIsRefused is the error an operator sees most often: a compose
// file without the one required variable. It has to name the variable and
// show its shape, because there is nothing else to go on.
func TestNoDatabaseIsRefused(t *testing.T) {
	clearEnvironment(t)

	_, err := bootstrap.Load()
	if err == nil {
		t.Fatalf("expected a refusal with no database configured")
	}
	if !strings.Contains(err.Error(), "TEANODE_DATABASE_URL") {
		t.Errorf("the error should name the variable to set, got: %s", err)
	}
	if !strings.Contains(err.Error(), "postgres://") {
		t.Errorf("the error should show the shape of it, got: %s", err)
	}
}

// TestUnknownParameterIsRefused: a connection option that appears to be set
// and is silently dropped is discovered in production, so it is refused here.
func TestUnknownParameterIsRefused(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("TEANODE_DATABASE_URL", "postgres://teanode@postgres:5432/mail?sslmode=disable&pool_max_conns=10")

	if _, err := bootstrap.Load(); err == nil {
		t.Errorf("expected a refusal for an unsupported parameter")
	}
}

// TestEmptyClearsTheDefault: some of these turn something off rather than
// change it. A development box has no HTTPS listener, and saying so means
// setting the variable to nothing.
func TestEmptyClearsTheDefault(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("TEANODE_DATABASE_URL", "postgres://teanode@postgres:5432/mail")
	t.Setenv("TEANODE_LISTEN_HTTPS", "")

	loaded, err := bootstrap.Load()
	if err != nil {
		t.Fatalf("Load: %s", err)
	}
	if loaded.Seed.Listen.HTTPS != "" {
		t.Errorf("the HTTPS listener should be cleared, got %q", loaded.Seed.Listen.HTTPS)
	}
	if len(loaded.SeededNames()) != 1 {
		t.Errorf("expected the one variable to count as seeded, got %v", loaded.SeededNames())
	}
}

func TestSeed(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("TEANODE_DATABASE_URL", "postgres://teanode@postgres:5432/mail")
	t.Setenv("TEANODE_SERVER_NAME", "mail.example.test")
	t.Setenv("TEANODE_SERVER_DOMAIN", "example.test")
	t.Setenv("TEANODE_TLS_HOSTS", "mail.example.test, mx1.example.test")
	t.Setenv("TEANODE_LISTEN_SMTP_INCOMING", ":2525")
	t.Setenv("TEANODE_S3_ENABLED", "true")
	t.Setenv("TEANODE_S3_ENDPOINT", "http://minio:9000")
	t.Setenv("TEANODE_S3_BUCKET", "teanode")
	t.Setenv("TEANODE_S3_PATH_STYLE", "true")

	loaded, err := bootstrap.Load()
	if err != nil {
		t.Fatalf("Load: %s", err)
	}

	seed := loaded.Seed
	if seed.Server.Name != "mail.example.test" {
		t.Errorf("wrong identity: %+v", seed.Server)
	}
	// TEANODE_SERVER_DOMAIN names a domain to serve rather than a property of
	// the server, so it turns up in the domain list.
	if len(seed.Domains) != 1 || seed.Domains[0].Domain != "example.test" {
		t.Errorf("the seeded domain should be example.test, got %+v", seed.Domains)
	}
	if len(seed.TLS.Hosts) != 2 || seed.TLS.Hosts[1] != "mx1.example.test" {
		t.Errorf("a comma separated list should be split and trimmed, got %v", seed.TLS.Hosts)
	}
	if seed.Listen.SMTPIncoming != ":2525" {
		t.Errorf("wrong listen address: %q", seed.Listen.SMTPIncoming)
	}
	if !seed.Storage.S3.Enabled || seed.Storage.S3.Endpoint != "http://minio:9000" || !seed.Storage.S3.PathStyle {
		t.Errorf("wrong object store: %+v", seed.Storage.S3)
	}

	// The database is on the seed too, so that a first run stores something
	// that validates as a whole.
	if seed.Database.Host != "postgres" {
		t.Errorf("the seed should carry the connection, got %q", seed.Database.Host)
	}

	// Nine variables are set above; the database URL is not part of the seed.
	if len(loaded.SeededNames()) != 8 {
		t.Errorf("expected eight seeded variables, got %d: %v", len(loaded.SeededNames()), loaded.SeededNames())
	}
}

// TestOnlyDisagreementIsReported: a compose file sets these on every start,
// almost always to what is already stored. Warning about all of them every
// time would be a wall of text nobody reads, so only the ones that would
// actually change something are named.
func TestOnlyDisagreementIsReported(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("TEANODE_DATABASE_URL", "postgres://teanode@postgres:5432/mail")
	t.Setenv("TEANODE_SERVER_NAME", "mail.example.test")
	t.Setenv("TEANODE_SERVER_LOG_LEVEL", "DEBUG")

	loaded, err := bootstrap.Load()
	if err != nil {
		t.Fatalf("Load: %s", err)
	}

	stored := config.Example()
	stored.Server.Name = "mail.example.test"
	stored.Server.LogLevel = "INFO"

	// Reported through the log, so what is checked here is the decision
	// rather than the message: the name agrees and the level does not.
	agrees, err := loaded.WouldChange(stored)
	if err != nil {
		t.Fatalf("WouldChange: %s", err)
	}
	if len(agrees) != 1 || agrees[0] != "TEANODE_SERVER_LOG_LEVEL" {
		t.Errorf("expected only the log level to disagree, got %v", agrees)
	}
}

// TestBadValueNamesTheVariable: a typo in a boolean should say which one, or
// the operator is left comparing a compose file against documentation.
func TestBadValueNamesTheVariable(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("TEANODE_DATABASE_URL", "postgres://teanode@postgres:5432/mail")
	t.Setenv("TEANODE_S3_ENABLED", "yes-please")

	_, err := bootstrap.Load()
	if err == nil {
		t.Fatalf("expected a refusal")
	}
	if !strings.Contains(err.Error(), "TEANODE_S3_ENABLED") {
		t.Errorf("the error should name the variable, got: %s", err)
	}
}

// TestInstanceID: sharing one identity between instances corrupts the usage
// counters, which are accumulated by read-modify-write under this key.
func TestInstanceID(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("TEANODE_DATABASE_URL", "postgres://teanode@postgres:5432/mail")
	t.Setenv("TEANODE_INSTANCE_ID", "teanode-2")

	loaded, err := bootstrap.Load()
	if err != nil {
		t.Fatalf("Load: %s", err)
	}
	if loaded.InstanceID != "teanode-2" {
		t.Errorf("wrong instance name: %q", loaded.InstanceID)
	}
}

func TestInstanceIDIsTruncatedToWhatTheColumnHolds(t *testing.T) {
	clearEnvironment(t)
	name := "teanode-mail-worker-" + strings.Repeat("a", 40) + "-xyz9"
	t.Setenv("TEANODE_DATABASE_URL", "postgres://teanode@postgres:5432/mail")
	t.Setenv("TEANODE_INSTANCE_ID", name)

	loaded, err := bootstrap.Load()
	if err != nil {
		t.Fatalf("Load: %s", err)
	}
	if len(loaded.InstanceID) != 32 {
		t.Fatalf("expected 32 characters, got %d: %q", len(loaded.InstanceID), loaded.InstanceID)
	}
	// The tail, because that is the part that differs between the names an
	// orchestrator generates.
	if !strings.HasSuffix(name, loaded.InstanceID) {
		t.Errorf("expected the tail of %q, got %q", name, loaded.InstanceID)
	}
}

// TestDefaultsAlone is the shortest possible deployment: one variable.
func TestDefaultsAlone(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("TEANODE_DATABASE_URL", "postgres://teanode:teanode@postgres:5432/teanode?sslmode=disable")

	loaded, err := bootstrap.Load()
	if err != nil {
		t.Fatalf("one variable should be enough to start: %s", err)
	}
	if loaded.InstanceID == "" {
		t.Errorf("an instance should always have a name")
	}
	if loaded.Seed.Server.DataDirectory == "" {
		t.Errorf("the seed should carry the default data directory")
	}
}

// clearEnvironment unsets everything this package reads.
//
// Called first by every test here, because these read the real environment
// and a developer with the deployment's own env file sourced in their shell
// would otherwise see failures that have nothing to do with their change.
func clearEnvironment(t *testing.T) {
	t.Helper()

	// From the package rather than from a list here: a hand-kept copy goes
	// out of date the first time a variable is added, and the failure it
	// causes looks like a bug in the change that added it.
	for _, name := range bootstrap.Variables() {
		// Setenv first, so that the test framework restores it afterwards;
		// unset rather than blank, because blank is a value.
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatalf("cannot unset %s: %s", name, err)
		}
	}
}

// TestEveryVariableIsDocumented fails when a variable is added and the page
// that lists them is not. A variable nobody documented is one nobody can use,
// and the usual way that happens is somebody adding one without knowing the
// page exists.
func TestEveryVariableIsDocumented(t *testing.T) {
	t.Parallel()

	content, err := os.ReadFile(filepath.Join("..", "..", "docs", "configuration.md"))
	if err != nil {
		t.Fatalf("cannot read the reference: %s", err)
	}
	reference := string(content)

	for _, name := range bootstrap.Variables() {
		if !strings.Contains(reference, name) {
			t.Errorf("%s is not in docs/configuration.md", name)
		}
	}
}

// Where a staged binary goes has to be the same place at every start, whatever
// directory the process happens to be started in.
//
// filepath.Abs was here, and it resolves against the working directory — so a
// start from one place and a start from another would look in two. The upgrade
// stages, execs (that path is absolute, so it works), says it worked, and then
// a restart from anywhere else finds nothing and runs the old binary with no
// refusal recorded at any point.
func TestARelativeUpgradeDirectoryTurnsUpgradesOff(t *testing.T) {
	for _, variable := range []string{"UPGRADE_DIRECTORY", "SERVER_DATA_DIRECTORY"} {
		t.Run(variable, func(t *testing.T) {
			t.Setenv(bootstrap.Prefix+"DATABASE_URL", "postgres://teanode:x@postgres:5432/teanode")
			t.Setenv(bootstrap.Prefix+variable, "data")

			// Not a refusal to start. A relative server.dataDirectory is
			// legal and resolves against the configuration file, so failing
			// here stopped an ordinary deployment booting over a setting for
			// a feature it was not using.
			loaded, err := bootstrap.Load()
			if err != nil {
				t.Fatalf("a relative path stopped the server starting: %s", err)
			}
			if loaded.UpgradeDirectory != "" {
				t.Errorf("it kept %q, which a start from elsewhere would not find",
					loaded.UpgradeDirectory)
			}
		})
	}
}

// And an absolute one is kept as it is.
func TestTheUpgradeDirectoryFollowsTheDataDirectory(t *testing.T) {
	t.Setenv(bootstrap.Prefix+"DATABASE_URL", "postgres://teanode:x@postgres:5432/teanode")
	t.Setenv(bootstrap.Prefix+"SERVER_DATA_DIRECTORY", "/var/lib/teanode")

	loaded, err := bootstrap.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.UpgradeDirectory != "/var/lib/teanode/upgrade" {
		t.Errorf("the upgrade directory is %q", loaded.UpgradeDirectory)
	}
}

// Neither set means no staging, and a deployment that cannot write over its
// own binary is told it cannot upgrade itself. Not a guess at where the data
// might be: on a container running as root a wrong guess stages into the layer
// a recreate throws away.
func TestNoUpgradeDirectoryWithoutOne(t *testing.T) {
	t.Setenv(bootstrap.Prefix+"DATABASE_URL", "postgres://teanode:x@postgres:5432/teanode")

	loaded, err := bootstrap.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.UpgradeDirectory != "" {
		t.Errorf("it guessed %q", loaded.UpgradeDirectory)
	}
}
