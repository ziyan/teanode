package configdb_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/configdb"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/security"
)

// seed is the configuration a first run stores.
func seed(t *testing.T) *config.Configuration {
	t.Helper()

	configuration := config.Example()
	configuration.Server.DataDirectory = t.TempDir()
	configuration.Users = nil
	configuration.Database = connection()
	return configuration
}

// connection is what the store reports as the database it reached, which the
// server fills in from the environment. It has to describe a usable one,
// because it is part of every configuration the store validates.
func connection() config.Database {
	return config.Database{
		Host:    "127.0.0.1",
		Port:    5432,
		User:    "teanode",
		Name:    "teanode",
		SSLMode: "disable",
	}
}

// TestReplaceIntoAnUnconfiguredDatabase is the migration path off the
// configuration file, and it has to work against a database that has only
// just been migrated.
//
// It did not. Loading a configuration went through a store, and a store
// insists that what it reads is a usable server — so importing into an empty
// database was refused because the empty database was not a valid server,
// which is the wrong way round. Found while moving a real deployment.
func TestReplaceIntoAnUnconfiguredDatabase(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	// Nothing has been stored: this is a database straight from the
	// migrations, which is exactly what an operator moving off a file has.
	existing, err := configdb.Load(database)
	if err != nil {
		t.Fatalf("Load on an unconfigured database: %s", err)
	}
	if len(existing.Domains) != 0 || len(existing.Users) != 0 {
		t.Fatalf("expected nothing stored, got %d domains and %d operators",
			len(existing.Domains), len(existing.Users))
	}

	incoming := seed(t)
	incoming.Users = []*config.User{{
		Username:     "operator",
		PasswordHash: "$2a$12$" + strings.Repeat("x", 53),
	}}
	if _, err := configdb.Replace(database, incoming); err != nil {
		t.Fatalf("Replace: %s", err)
	}

	// And now it is a server, so a store will open on it.
	store, err := configdb.Open(database, connection())
	if err != nil {
		t.Fatalf("Open after Replace: %s", err)
	}
	defer func() {
		_ = store.Close()
	}()

	current := store.Current()
	if len(current.Domains) != len(incoming.Domains) {
		t.Errorf("stored %d domains, want %d", len(current.Domains), len(incoming.Domains))
	}
	if len(current.Users) != 1 || current.Users[0].Username != "operator" {
		t.Errorf("the operator did not survive: %v", current.Users)
	}
}

// TestASecretOfRawBytesSurvives is what a real deployment found and no test
// did.
//
// The server secret is 32 random bytes, not text, so roughly one secret in
// eight contains a zero byte. JSON encodes that as an escape a jsonb column
// refuses outright, and the error — "unsupported Unicode escape sequence" —
// says nothing about secrets, configuration, or what to do. The value column
// is text now: it holds a JSON document either way and nothing queries inside
// it, so jsonb only ever added the restriction.
func TestASecretOfRawBytesSurvives(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	// A zero byte at the front, one in the middle, one at the end — and a
	// byte that is not valid UTF-8, which is the other thing raw bytes are.
	secret := string([]byte{0x00, 0x41, 0x00, 0xff, 0x42, 0x00})

	configuration := seed(t)
	configuration.Server.Secret = secret
	configuration.Session.Key = string([]byte{0x00, 0xfe, 0x01})

	if _, err := configdb.Replace(database, configuration); err != nil {
		t.Fatalf("storing a secret of raw bytes: %s", err)
	}

	store, err := configdb.Open(database, connection())
	if err != nil {
		t.Fatalf("Open: %s", err)
	}
	defer func() {
		_ = store.Close()
	}()

	// Byte for byte, because every SMTP password is derived from this: a
	// secret that comes back changed silently invalidates all of them.
	if got := string(store.Current().Secret()); got != strings.TrimSpace(secret) {
		t.Errorf("the server secret came back as %q, want %q", got, secret)
	}
}

// TestChangesSurviveARestart is the property the dashboard rests on, and the
// one the file used to provide: a change made through the store has to be
// there when a different process reads it. Two stores over one database are
// what a restart looks like from the database's side.
func TestChangesSurviveARestart(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	if _, err := configdb.Initialize(database, func() (*config.Configuration, error) {
		return seed(t), nil
	}); err != nil {
		t.Fatalf("Initialize: %s", err)
	}

	store, err := configdb.Open(database, connection())
	if err != nil {
		t.Fatalf("Open: %s", err)
	}

	aliasId := config.NewID()
	domainId := config.NewID()
	if err := store.Update(func(configuration *config.Configuration) error {
		configuration.Domains = append(configuration.Domains, &config.Domain{
			ID:        domainId,
			Domain:    "second.example",
			Subdomain: "mail",
			Aliases: []*config.Alias{
				{ID: aliasId, Pattern: "^support$", Kind: config.AliasKindEmail, Email: "support@example.net"},
			},
			Credentials: []*config.Credential{
				{ID: config.NewID(), Key: "0123456789abcdef"},
			},
		})
		return nil
	}); err != nil {
		t.Fatalf("Update: %s", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %s", err)
	}

	restarted, err := configdb.Open(database, connection())
	if err != nil {
		t.Fatalf("reopening: %s", err)
	}
	defer func() {
		_ = restarted.Close()
	}()

	reloaded := restarted.Current()
	domain := reloaded.FindDomain("second.example")
	if domain == nil {
		t.Fatal("the new domain did not survive")
	}
	// The identifiers matter more than the rest: stored mail and deliveries
	// point at them.
	if domain.ID != domainId {
		t.Errorf("the domain identifier changed to %q", domain.ID)
	}
	if reloaded.FindAliasByID(aliasId) == nil {
		t.Error("the new alias did not survive")
	}
}

// TestUpdateThatReadsBeforeWriting guards a bug that made a credential
// created through the dashboard unusable until the process restarted.
//
// The lookup tables are built lazily. A mutation that reads the configuration
// before changing it — which is exactly what a create does when it checks for
// a duplicate first — builds them from the state before the change, and they
// were never rebuilt. The new credential then could not be found by anything,
// and the symptom was remote from the cause: mail submitted with it was
// refused as "Invalid credentials" while the credential sat plainly in the
// configuration.
//
// The memory store has had this right for a while. This is the same test
// against the store the server actually uses, because the property belongs to
// every implementation of config.Store and was missing from the new one.
func TestUpdateThatReadsBeforeWriting(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	if _, err := configdb.Initialize(database, func() (*config.Configuration, error) {
		return seed(t), nil
	}); err != nil {
		t.Fatalf("Initialize: %s", err)
	}

	store, err := configdb.Open(database, connection())
	if err != nil {
		t.Fatalf("Open: %s", err)
	}
	defer func() {
		_ = store.Close()
	}()

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
		t.Fatalf("Update: %s", err)
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
	// The one that mattered: this lookup is what the submission port makes
	// for every message, and returning nil here refuses the mail.
	if _, credential := configuration.FindCredential(credentialId); credential == nil {
		t.Error("the new credential cannot be found, so mail sent with it would be refused")
	}
}

// TestSecretsSurviveARestart: the server secret signs every SMTP password and
// the session key signs every login, so losing either on a restart logs
// everybody out and stops every configured device from sending.
func TestSecretsSurviveARestart(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	if _, err := configdb.Initialize(database, func() (*config.Configuration, error) {
		return seed(t), nil
	}); err != nil {
		t.Fatalf("Initialize: %s", err)
	}

	store, err := configdb.Open(database, connection())
	if err != nil {
		t.Fatalf("Open: %s", err)
	}
	if err := config.EnsureSecrets(store); err != nil {
		t.Fatalf("EnsureSecrets: %s", err)
	}
	secret := string(store.Current().Secret())
	sessionKey := string(store.Current().SessionKey())
	if len(secret) < 32 || len(sessionKey) < 32 {
		t.Fatalf("secrets are too short: %d and %d", len(secret), len(sessionKey))
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %s", err)
	}

	restarted, err := configdb.Open(database, connection())
	if err != nil {
		t.Fatalf("reopening: %s", err)
	}
	defer func() {
		_ = restarted.Close()
	}()

	if string(restarted.Current().Secret()) != secret {
		t.Error("the server secret changed; every SMTP password would stop working")
	}
	if string(restarted.Current().SessionKey()) != sessionKey {
		t.Error("the session key changed; everybody would be logged out")
	}

	// And a second instance starting against the same database must not
	// generate its own.
	if err := config.EnsureSecrets(restarted); err != nil {
		t.Fatalf("EnsureSecrets on the second instance: %s", err)
	}
	if string(restarted.Current().Secret()) != secret {
		t.Error("a second instance replaced the server secret")
	}
}

// TestSecondInstanceSeesTheChange is the whole point of moving configuration
// into the database. One instance adds a domain; the other has to be serving
// mail for it without being restarted.
func TestSecondInstanceSeesTheChange(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	if _, err := configdb.Initialize(database, func() (*config.Configuration, error) {
		return seed(t), nil
	}); err != nil {
		t.Fatalf("Initialize: %s", err)
	}

	first, err := configdb.Open(database, connection())
	if err != nil {
		t.Fatalf("Open: %s", err)
	}
	defer func() {
		_ = first.Close()
	}()

	second, err := configdb.Open(database, connection())
	if err != nil {
		t.Fatalf("Open: %s", err)
	}
	defer func() {
		_ = second.Close()
	}()

	notified := make(chan *config.Configuration, 4)
	unsubscribe := second.Subscribe(func(configuration *config.Configuration) {
		notified <- configuration
	})
	defer unsubscribe()

	if err := first.Update(func(configuration *config.Configuration) error {
		configuration.Domains = append(configuration.Domains, &config.Domain{
			ID:      "third.example",
			Domain:  "third.example",
			Aliases: []*config.Alias{{ID: config.NewID(), Pattern: "^hello$", Kind: config.AliasKindEmail, Email: "hello@example.net"}},
		})
		return nil
	}); err != nil {
		t.Fatalf("Update: %s", err)
	}

	// The poll interval is five seconds; this waits longer than that so a
	// slow machine does not fail it, and stops as soon as the change lands.
	deadline := time.After(30 * time.Second)
	for {
		select {
		case configuration := <-notified:
			if configuration.FindDomain("third.example") != nil {
				return
			}
		case <-deadline:
			t.Fatal("the second instance never saw the domain the first one added")
		}
	}
}

// TestAConcurrentChangeIsNotLost covers two operators, or two instances,
// changing different things at the same moment. The one that commits second
// used to be applied to a stale copy, which would drop the first change.
func TestAConcurrentChangeIsNotLost(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	if _, err := configdb.Initialize(database, func() (*config.Configuration, error) {
		return seed(t), nil
	}); err != nil {
		t.Fatalf("Initialize: %s", err)
	}

	first, err := configdb.Open(database, connection())
	if err != nil {
		t.Fatalf("Open: %s", err)
	}
	defer func() {
		_ = first.Close()
	}()
	second, err := configdb.Open(database, connection())
	if err != nil {
		t.Fatalf("Open: %s", err)
	}
	defer func() {
		_ = second.Close()
	}()

	// Both stores are now holding the same version. The first commits.
	if err := first.Update(func(configuration *config.Configuration) error {
		configuration.Server.LogLevel = "DEBUG"
		return nil
	}); err != nil {
		t.Fatalf("the first change: %s", err)
	}

	// The second has not polled yet, so it is working from the older copy.
	// Its change has to be applied on top of the first one rather than over
	// it — that is what the retry is for.
	if err := second.Update(func(configuration *config.Configuration) error {
		configuration.Domains[0].Comment = "changed by the other one"
		return nil
	}); err != nil {
		t.Fatalf("the second change: %s", err)
	}

	final, err := configdb.Open(database, connection())
	if err != nil {
		t.Fatalf("reopening: %s", err)
	}
	defer func() {
		_ = final.Close()
	}()

	current := final.Current()
	if current.Server.LogLevel != "DEBUG" {
		t.Errorf("the first change was lost: log level is %q", current.Server.LogLevel)
	}
	if current.Domains[0].Comment != "changed by the other one" {
		t.Errorf("the second change was lost: comment is %q", current.Domains[0].Comment)
	}
}

// TestInitializeIsOnlyEverFirst: the environment describes a server once. A
// restart with different variables must not quietly rewrite what the operator
// has since configured in the dashboard.
func TestInitializeIsOnlyEverFirst(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	seeded, err := configdb.Initialize(database, func() (*config.Configuration, error) {
		return seed(t), nil
	})
	if err != nil {
		t.Fatalf("Initialize: %s", err)
	}
	if !seeded {
		t.Fatal("the first call should have seeded an empty database")
	}

	store, err := configdb.Open(database, connection())
	if err != nil {
		t.Fatalf("Open: %s", err)
	}
	defer func() {
		_ = store.Close()
	}()
	if err := store.Update(func(configuration *config.Configuration) error {
		configuration.Server.Name = "chosen-in-the-dashboard.test"
		configuration.TLS.Hosts = []string{"chosen-in-the-dashboard.test"}
		return nil
	}); err != nil {
		t.Fatalf("Update: %s", err)
	}

	// A second start, with an environment that says something else.
	seeded, err = configdb.Initialize(database, func() (*config.Configuration, error) {
		other := seed(t)
		other.Server.Name = "from-the-environment.test"
		return other, nil
	})
	if err != nil {
		t.Fatalf("the second Initialize: %s", err)
	}
	if seeded {
		t.Fatal("a database that is already configured must not be seeded again")
	}

	if err := store.Reload(); err != nil {
		t.Fatalf("Reload: %s", err)
	}
	if got := store.Current().Server.Name; got != "chosen-in-the-dashboard.test" {
		t.Errorf("the environment overwrote a configured server: %q", got)
	}
}

// TestARelativeDataDirectoryIsRefused: it used to resolve against the
// directory holding teanode.yaml. With no file, it would land wherever each
// process happened to be started from, and two instances would disagree about
// where the spool is without either saying so.
func TestARelativeDataDirectoryIsRefused(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	if _, err := configdb.Initialize(database, func() (*config.Configuration, error) {
		configuration := seed(t)
		configuration.Server.DataDirectory = "state"
		return configuration, nil
	}); err != nil {
		t.Fatalf("Initialize: %s", err)
	}

	_, err := configdb.Open(database, connection())
	if err == nil {
		t.Fatal("a relative data directory should be refused")
	}
	if !strings.Contains(err.Error(), "dataDirectory") {
		t.Errorf("the error should name the setting, got: %s", err)
	}
}

// TestSavingTheConfigurationKeepsSessionsAndTokens is the deployment test's
// finding, made cheap to run.
//
// Sessions, API tokens and passkeys reference an account and cascade from it.
// Saving the configuration used to delete every account row and put it back,
// which meant every change to any setting signed everybody out and revoked
// every token — silently, because the rows were gone rather than refused. An
// API token stopped working the moment the server saved anything.
func TestSavingTheConfigurationKeepsSessionsAndTokens(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	stored := seed(t)
	stored.Users = []*config.User{{
		ID:           config.NewID(),
		Username:     "ziyan",
		PasswordHash: "$2a$12$" + strings.Repeat("x", 53),
	}}
	if _, err := configdb.Replace(database, stored); err != nil {
		t.Fatalf("Replace: %s", err)
	}

	userId := stored.Users[0].ID
	if _, err := database.CreateSession(&models.Session{
		ID: security.NewULID(), UserID: userId, ExpiresAt: time.Now().Add(time.Hour),
	}, "a-key-hash"); err != nil {
		t.Fatalf("CreateSession: %s", err)
	}
	if _, err := database.CreateToken(&models.Token{
		ID: security.NewULID(), UserID: userId, Name: "laptop",
	}, "a-key-hash"); err != nil {
		t.Fatalf("CreateToken: %s", err)
	}

	// Any change at all. This one is a setting nothing else in the test cares
	// about, because the point is that saving is what did the damage.
	changed, err := configdb.Load(database)
	if err != nil {
		t.Fatalf("Load: %s", err)
	}
	changed.Server.LogLevel = "DEBUG"
	if _, err := configdb.Replace(database, changed); err != nil {
		t.Fatalf("Replace after the change: %s", err)
	}

	sessions, err := database.ListSessions(userId, nil)
	if err != nil {
		t.Fatalf("ListSessions: %s", err)
	}
	if len(sessions) != 1 {
		t.Errorf("the session did not survive the save: got %d", len(sessions))
	}
	tokens, err := database.ListTokens(userId, nil)
	if err != nil {
		t.Fatalf("ListTokens: %s", err)
	}
	if len(tokens) != 1 {
		t.Errorf("the API token did not survive the save: got %d", len(tokens))
	}

	// An account that is genuinely removed still takes its rows with it,
	// which is the behaviour the cascade is there for.
	without, err := configdb.Load(database)
	if err != nil {
		t.Fatalf("Load: %s", err)
	}
	without.Users = nil
	if _, err := configdb.Replace(database, without); err != nil {
		t.Fatalf("Replace without the account: %s", err)
	}
	remaining, err := database.ListTokens(userId, nil)
	if err != nil {
		t.Fatalf("ListTokens: %s", err)
	}
	if len(remaining) != 0 {
		t.Errorf("removing the account left %d token(s) behind", len(remaining))
	}
}

// TestEveryDomainFieldSurvivesARestart guards a class of bug rather than one
// field, because it had already happened once and nothing would have said so.
//
// A domain is written to the database column by column. A field added to the
// configuration and to the reading half, but not to the writing half, is
// accepted by the API, echoed back to the caller from memory, and gone at the
// next reload — with no error anywhere. That is exactly what had happened to
// mailServers: a domain configured with names of its own kept them until the
// configuration was next saved, and the deployment that looked right was
// deriving the same names by accident.
//
// So this asserts on every field an operator can set, and a new one belongs in
// it.
func TestEveryDomainFieldSurvivesARestart(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	if _, err := configdb.Initialize(database, func() (*config.Configuration, error) {
		return seed(t), nil
	}); err != nil {
		t.Fatalf("Initialize: %s", err)
	}

	store, err := configdb.Open(database, connection())
	if err != nil {
		t.Fatalf("Open: %s", err)
	}

	domainId := config.NewID()
	if err := store.Update(func(configuration *config.Configuration) error {
		configuration.Domains = append(configuration.Domains, &config.Domain{
			ID:                       domainId,
			Domain:                   "second.example",
			Subdomain:                "mail",
			Comment:                  "a note",
			SpamFilterScoreThreshold: 7.5,
			MailServers:              []string{"mx1.second.example", "mx2.second.example"},
			LinkHost:                 "pictures.second.example",
		})
		return nil
	}); err != nil {
		t.Fatalf("Update: %s", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %s", err)
	}

	restarted, err := configdb.Open(database, connection())
	if err != nil {
		t.Fatalf("reopening: %s", err)
	}
	defer func() {
		_ = restarted.Close()
	}()

	domain := restarted.Current().FindDomain("second.example")
	if domain == nil {
		t.Fatal("the domain did not survive")
	}
	if domain.Subdomain != "mail" {
		t.Errorf("subdomain is %q", domain.Subdomain)
	}
	if domain.Comment != "a note" {
		t.Errorf("comment is %q", domain.Comment)
	}
	if domain.SpamFilterScoreThreshold != 7.5 {
		t.Errorf("the spam threshold is %v", domain.SpamFilterScoreThreshold)
	}
	if strings.Join(domain.MailServers, ",") != "mx1.second.example,mx2.second.example" {
		t.Errorf("the mail server names are %v, and an operator who set them would find the default instead", domain.MailServers)
	}
	if domain.LinkHost != "pictures.second.example" {
		t.Errorf("the picture host is %q", domain.LinkHost)
	}
	// The selector is left out: it cannot be set without a key beside it, and
	// the key has a test of its own.
}
