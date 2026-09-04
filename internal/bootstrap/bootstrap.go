// Package bootstrap reads the settings a server needs before it can read its
// settings.
//
// Configuration lives in the database, which means the connection to the
// database cannot live there too. That one thing comes from the environment,
// and so does the seed for a database that has no configuration in it yet:
// a fresh instance has to be told its own name and where to listen before
// anyone can log in and change it.
//
// The split matters once more than one instance is running. Anything in the
// database is shared, and changing it in the dashboard changes it everywhere.
// Anything here is per-process, and changing it means restarting that process
// — so this stays deliberately small.
package bootstrap

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/op/go-logging"
	"gopkg.in/yaml.v3"

	"github.com/ziyan/teanode/internal/config"
)

var log = logging.MustGetLogger("bootstrap")

// Prefix is on the front of every variable this package reads.
const Prefix = "TEANODE_"

// instanceIDLength is what the usage tables allow for the column.
const instanceIDLength = 32

// Bootstrap is what the environment says.
type Bootstrap struct {
	// Database is where everything else is kept.
	Database config.Database

	// InstanceID distinguishes this process from the others sharing the
	// database. It is part of the primary key of the usage counters, which
	// are accumulated by reading a row, adding to it and writing it back —
	// safe only while no other instance is doing that to the same row.
	//
	// So it has to differ per process, and it has to be stable across a
	// restart of the same one, or yesterday's counters are stranded under a
	// name nothing writes to any more. A container's hostname is both.
	InstanceID string

	// UpgradeDirectory is where an upgrade stages a binary that cannot
	// replace the running one, and where the next start looks for it.
	//
	// Here rather than in the configuration, which is where every other path
	// this server uses lives, because of when it is needed. A staged binary
	// has to be found and run before anything opens the database — this
	// program reverts migrations it does not recognise, so an old binary that
	// reached the database first would undo the new one's schema — and the
	// configuration is in the database. So it comes from the environment, or
	// from the data directory the environment names, or from nowhere, in
	// which case a deployment that cannot write over its own binary is told
	// it cannot upgrade itself.
	UpgradeDirectory string

	// Seed is applied to the defaults when the database holds no
	// configuration yet, and ignored on every later start. See Seeded.
	//
	// It is not a complete configuration on its own — see SeedConfiguration,
	// which is what fills in the rest and says what is missing.
	Seed *config.Configuration

	// seeded names the variables that went into Seed, in the order they were
	// read, and pairs each with what it does, so that a start which ignores
	// them can say which ones actually disagree with what is stored.
	seeded []seededVariable
}

// Load reads the environment.
//
// Nothing here reaches the filesystem or the network, so it is safe to call
// before logging is configured and cheap to call in a test.
func Load() (*Bootstrap, error) {
	self := &Bootstrap{Seed: config.Default()}

	if err := self.loadDatabase(); err != nil {
		return nil, err
	}
	if err := self.loadInstanceID(); err != nil {
		return nil, err
	}
	if err := self.loadSeed(); err != nil {
		return nil, err
	}
	if err := self.loadUpgradeDirectory(); err != nil {
		return nil, err
	}
	return self, nil
}

// loadUpgradeDirectory works out where a staged binary goes.
//
// TEANODE_UPGRADE_DIRECTORY when it is set, and otherwise "upgrade" under
// TEANODE_SERVER_DATA_DIRECTORY when that is — which for a container is the
// volume the spool and the keys already live on, so the ordinary deployment
// gets a working answer without being told twice.
//
// Nothing when neither is set, and the deployment is then told it cannot
// upgrade itself. Deliberately not falling back to the compiled-in default
// data directory: that path is a guess, and a wrong guess here is not a
// harmless one — on a container running as root it would put the new binary
// inside the image, where it works until the container is recreated and then
// silently is not there. The variables above are read straight from the
// environment rather than from the seed for the same reason. The seed is
// applied on a first run only, so a deployment that set its data directory in
// the dashboard has the default sitting in the seed and nothing in its
// environment, and following the seed would name a directory nobody chose.
func (self *Bootstrap) loadUpgradeDirectory() error {
	named := Prefix + "UPGRADE_DIRECTORY"
	directory, ok := lookup("UPGRADE_DIRECTORY")
	if !ok || directory == "" {
		data, ok := lookup("SERVER_DATA_DIRECTORY")
		if !ok || data == "" {
			return nil
		}
		named = Prefix + "SERVER_DATA_DIRECTORY"
		directory = filepath.Join(data, "upgrade")
	}

	// Absolute, and the feature is turned off rather than resolved when it is
	// not.
	//
	// filepath.Abs was here and was worse than nothing: it resolves against
	// the working directory, so a start from one place and a start from
	// another would look for the staged binary in two. The upgrade would
	// stage, exec — the path it execs is absolute, so that part works — and
	// report success, and then a restart from anywhere else would find
	// nothing and quietly run the old binary.
	//
	// Refusing the whole start was the next thing tried, and that was worse
	// again: a relative server.dataDirectory is legal and resolves against
	// the configuration file, so an ordinary deployment that had worked for a
	// year stopped booting over a setting for a feature it was not using —
	// and the message named a variable nobody had set. Upgrades are refused
	// and mail keeps moving.
	if !filepath.IsAbs(directory) {
		log.Warningf("not staging upgrades anywhere: %s gives %q, which is relative to whatever "+
			"directory the process starts in, and a staged binary has to be found again by a start "+
			"from any of them. Set %sUPGRADE_DIRECTORY to an absolute path to turn upgrades on",
			named, directory, Prefix)
		return nil
	}
	self.UpgradeDirectory = filepath.Clean(directory)
	return nil
}

// loadDatabase reads the connection. TEANODE_DATABASE_URL is the whole thing
// in one variable, which is what a hosted PostgreSQL hands you and what
// docker-compose is easiest to write with; the discrete variables are there
// for the deployment that assembles it from parts, and override the URL
// field by field.
func (self *Bootstrap) loadDatabase() error {
	// The port, the user and the SSL mode keep their defaults, because they
	// are usually the obvious ones. The host and the database name do not:
	// defaulting those to a local PostgreSQL would let an instance whose
	// variable is missing quietly reach an empty database of its own, decide
	// it is a brand new server, and seed itself — which looks like it worked.
	self.Database = config.Default().Database
	self.Database.Host = ""
	self.Database.Name = ""

	// An empty URL falls through to the check below, so that the error names
	// the variable and shows its shape rather than complaining about a scheme.
	if value, ok := lookup("DATABASE_URL"); ok && value != "" {
		if err := self.parseDatabaseURL(value); err != nil {
			return err
		}
	}

	for _, variable := range []struct {
		name  string
		parse func(string) error
	}{
		{"DATABASE_HOST", func(value string) error { self.Database.Host = value; return nil }},
		{"DATABASE_PORT", func(value string) error { return parsePort(value, &self.Database.Port) }},
		{"DATABASE_USER", func(value string) error { self.Database.User = value; return nil }},
		{"DATABASE_PASSWORD", func(value string) error { self.Database.Password = value; return nil }},
		{"DATABASE_NAME", func(value string) error { self.Database.Name = value; return nil }},
		{"DATABASE_SSL_MODE", func(value string) error { self.Database.SSLMode = value; return nil }},
		{"DATABASE_LOG_QUERIES", func(value string) error { return parseBool(value, &self.Database.LogQueries) }},
	} {
		value, ok := lookup(variable.name)
		if !ok {
			continue
		}
		if err := variable.parse(value); err != nil {
			return fmt.Errorf("bootstrap: %s%s: %w", Prefix, variable.name, err)
		}
	}

	if self.Database.Host == "" || self.Database.Name == "" {
		return fmt.Errorf("bootstrap: no database configured: set %sDATABASE_URL, "+
			"for example postgres://teanode:password@postgres:5432/teanode?sslmode=disable", Prefix)
	}
	return nil
}

// parseDatabaseURL reads a postgres:// URL. Only the parts TeaNode connects
// with are taken; a query parameter other than sslmode is refused rather than
// dropped, because a connection option that appears to be set and is not is
// the kind of thing that is discovered in production.
func (self *Bootstrap) parseDatabaseURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("bootstrap: %sDATABASE_URL is not a URL: %w", Prefix, err)
	}
	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return fmt.Errorf("bootstrap: %sDATABASE_URL must be a postgres:// URL, got %q", Prefix, parsed.Scheme)
	}

	self.Database.Host = parsed.Hostname()
	if port := parsed.Port(); port != "" {
		if err := parsePort(port, &self.Database.Port); err != nil {
			return fmt.Errorf("bootstrap: %sDATABASE_URL: %w", Prefix, err)
		}
	}
	if parsed.User != nil {
		self.Database.User = parsed.User.Username()
		if password, ok := parsed.User.Password(); ok {
			self.Database.Password = password
		}
	}
	self.Database.Name = strings.TrimPrefix(parsed.Path, "/")

	for name, values := range parsed.Query() {
		switch name {
		case "sslmode":
			self.Database.SSLMode = values[0]
		default:
			return fmt.Errorf("bootstrap: %sDATABASE_URL: unsupported parameter %q", Prefix, name)
		}
	}
	return nil
}

// loadInstanceID names this process.
//
// The hostname is the default because it is what a container orchestrator
// already assigns, and it is stable for the life of a pod or a compose
// service. It is not stable across a rescheduled pod, which is the case for
// setting the variable explicitly — to the StatefulSet ordinal, say.
func (self *Bootstrap) loadInstanceID() error {
	value, ok := lookup("INSTANCE_ID")
	if !ok {
		hostname, err := os.Hostname()
		if err != nil {
			return fmt.Errorf("bootstrap: no %sINSTANCE_ID and this host has no name: %w", Prefix, err)
		}
		value = hostname
	}

	if len(value) > instanceIDLength {
		// Truncating rather than refusing, because the names an orchestrator
		// generates are long and an operator did not choose this one. Taking
		// the tail keeps the part that differs: a pod name is a shared prefix
		// and a random suffix.
		value = value[len(value)-instanceIDLength:]
		log.Warningf("the instance name is longer than %d characters; using %q. "+
			"Set %sINSTANCE_ID if two instances would end up with the same one",
			instanceIDLength, value, Prefix)
	}

	self.InstanceID = value
	return nil
}

// seedVariables is every variable that can go into a database with no
// configuration in it. It is a table rather than a struct with tags because
// the destinations are spread across the configuration and a few of them
// need more than an assignment.
//
// Kept to what a container has to be told: its identity, where it listens,
// and where the shared spool is. Everything else has a working default and
// belongs in the dashboard.
var seedVariables = []struct {
	name  string
	apply func(*config.Configuration, string) error
}{
	{"SERVER_NAME", func(target *config.Configuration, value string) error {
		target.Server.Name = value
		return nil
	}},
	// Not a setting. It names a domain this server should serve, and is
	// remembered as that domain rather than as a property of the server —
	// which is the whole difference from the "primary domain" this replaced.
	// The subdomain and the signing key are filled in afterwards, once every
	// other variable has been applied and the server name and selector are
	// final.
	{"SERVER_DOMAIN", func(target *config.Configuration, value string) error {
		if value == "" || target.FindDomain(value) != nil {
			return nil
		}
		target.Domains = append(target.Domains, &config.Domain{ID: config.NewID(), Domain: value})
		return nil
	}},
	{"SERVER_DATA_DIRECTORY", func(target *config.Configuration, value string) error {
		target.Server.DataDirectory = value
		return nil
	}},
	{"SERVER_LOG_LEVEL", func(target *config.Configuration, value string) error {
		target.Server.LogLevel = value
		return nil
	}},
	{"SERVER_MAIL_SERVERS", func(target *config.Configuration, value string) error {
		target.Server.MailServers = splitList(value)
		return nil
	}},

	{"LISTEN_SMTP_INCOMING", func(target *config.Configuration, value string) error {
		target.Listen.SMTPIncoming = value
		return nil
	}},
	{"LISTEN_SMTP_OUTGOING", func(target *config.Configuration, value string) error {
		target.Listen.SMTPOutgoing = value
		return nil
	}},
	{"LISTEN_HTTP", func(target *config.Configuration, value string) error {
		target.Listen.HTTP = value
		return nil
	}},
	{"LISTEN_HTTPS", func(target *config.Configuration, value string) error {
		target.Listen.HTTPS = value
		return nil
	}},

	{"TLS_HOSTS", func(target *config.Configuration, value string) error {
		target.TLS.Hosts = splitList(value)
		return nil
	}},
	{"TLS_ACME_ENABLED", func(target *config.Configuration, value string) error {
		return parseBool(value, &target.TLS.ACME.Enabled)
	}},
	{"TLS_ACME_EMAIL", func(target *config.Configuration, value string) error {
		target.TLS.ACME.Email = value
		return nil
	}},

	{"SMTP_REQUIRE_REVERSE_DNS", func(target *config.Configuration, value string) error {
		return parseBool(value, &target.SMTP.RequireReverseDNS)
	}},
	{"SMTP_DISABLE_SEND", func(target *config.Configuration, value string) error {
		return parseBool(value, &target.SMTP.DisableSend)
	}},

	{"S3_ENABLED", func(target *config.Configuration, value string) error {
		return parseBool(value, &target.Storage.S3.Enabled)
	}},
	{"S3_ENDPOINT", func(target *config.Configuration, value string) error {
		target.Storage.S3.Endpoint = value
		return nil
	}},
	{"S3_BUCKET", func(target *config.Configuration, value string) error {
		target.Storage.S3.Bucket = value
		return nil
	}},
	{"S3_REGION", func(target *config.Configuration, value string) error {
		target.Storage.S3.Region = value
		return nil
	}},
	{"S3_PATH_STYLE", func(target *config.Configuration, value string) error {
		return parseBool(value, &target.Storage.S3.PathStyle)
	}},
	{"S3_ACCESS_KEY_ID", func(target *config.Configuration, value string) error {
		target.Storage.S3.AccessKeyID = value
		return nil
	}},
	{"S3_SECRET_ACCESS_KEY", func(target *config.Configuration, value string) error {
		target.Storage.S3.SecretAccessKey = value
		return nil
	}},

	{"PASSKEY_ENABLED", func(target *config.Configuration, value string) error {
		return parseBool(value, &target.Passkey.Enabled)
	}},
	{"PASSKEY_RELYING_PARTY_ID", func(target *config.Configuration, value string) error {
		target.Passkey.RelyingPartyID = value
		return nil
	}},
	{"PASSKEY_ORIGINS", func(target *config.Configuration, value string) error {
		target.Passkey.Origins = splitList(value)
		return nil
	}},
	{"PASSKEY_REDIS_ADDRESS", func(target *config.Configuration, value string) error {
		target.Passkey.Redis.Address = value
		return nil
	}},
	{"PASSKEY_REDIS_PASSWORD", func(target *config.Configuration, value string) error {
		target.Passkey.Redis.Password = value
		return nil
	}},
}

func (self *Bootstrap) loadSeed() error {
	for _, variable := range seedVariables {
		value, ok := lookup(variable.name)
		if !ok {
			continue
		}
		if err := variable.apply(self.Seed, value); err != nil {
			return fmt.Errorf("bootstrap: %s%s: %w", Prefix, variable.name, err)
		}
		self.seeded = append(self.seeded, seededVariable{name: variable.name, apply: variable.apply, value: value})
	}

	self.Seed.Database = self.Database
	return nil
}

// SeedConfiguration builds the configuration a first run stores.
//
// Deliberately not done in Load. Most starts are not first runs, and on those
// the environment does not have to describe a whole server — the database
// already does, and demanding a host name from someone who is only restarting
// a container would be wrong. So the demand is made here, at the one moment
// it is real.
// subdomainOf returns the label mail.example.com has under example.com, or
// nothing when the server name is not under the domain at all — which is
// allowed, and means the domain publishes its records at its apex.
func subdomainOf(serverName, domain string) string {
	suffix := "." + domain
	if !strings.HasSuffix(serverName, suffix) {
		return ""
	}
	label := strings.TrimSuffix(serverName, suffix)
	if strings.Contains(label, ".") {
		return ""
	}
	return label
}

func (self *Bootstrap) SeedConfiguration() (*config.Configuration, error) {
	seed, err := config.Clone(self.Seed)
	if err != nil {
		return nil, err
	}

	// Every domain the seed named gets the parts that depend on values only
	// settled once all the variables have been applied: where its own records
	// live, and a signing key.
	//
	// Creating the domain here is what turns two variables into a working
	// server. Without it a first start would store something that does not
	// validate, and the operator would be told to add a domain before they
	// could reach the dashboard that adds domains.
	for _, domain := range seed.Domains {
		if domain == nil {
			continue
		}
		if domain.Subdomain == "" {
			// The label the server's own records live under, taken from the
			// server name rather than asked for separately: mail.example.com
			// serving example.com means "mail", and there is no second answer
			// worth offering. It matters because a domain with no subdomain
			// wants its CNAME at the apex, where the MX record already is.
			domain.Subdomain = subdomainOf(seed.Server.Name, domain.Domain)
		}
		if domain.DKIM.PrivateKey == "" {
			// Signed from the start, so that nobody has to know DKIM exists
			// before their mail is trusted. Its own key, not a copy of
			// anybody else's: every domain publishes its own record.
			//
			// Here rather than left to config.EnsureSecrets, which would also
			// do it, because configuring a database is its own step: an
			// operator can run it, ask "teanode dkim show" for the record to
			// publish, and put it in DNS before the server has ever started.
			// A domain with no key until first run makes that impossible.
			//
			// It is written before there is a server secret to encrypt it
			// with, so it is stored as it stands and sealed by the save that
			// generates the secret, moments later. See internal/configdb.
			key, err := config.GenerateDomainKey(seed.DKIM.Selector)
			if err != nil {
				return nil, err
			}
			domain.DKIM = key
		}
	}

	// A certificate is for the name this server answers to, which is the one
	// already given.
	if len(seed.TLS.Hosts) == 0 && seed.Server.Name != "" {
		seed.TLS.Hosts = []string{seed.Server.Name}
	}

	// No account is created. A server with none shows a setup screen asking
	// the first person to arrive to choose their own username and password,
	// which beats inventing one called "admin" and putting its password in an
	// environment variable that every "docker inspect" prints.
	seed.Users = nil

	if err := seed.Validate(); err != nil {
		return nil, fmt.Errorf("bootstrap: this database has no configuration yet, and the environment "+
			"does not describe a server to create:\n%w\n\nSet the variables for those settings — "+
			"%sSERVER_NAME and %sSERVER_DOMAIN are the two a first run always needs — "+
			"or run \"teanode config import\" to load an existing teanode.yaml",
			err, Prefix, Prefix)
	}
	return seed, nil
}

// seededVariable is one first-run variable and what it would have done.
type seededVariable struct {
	name  string
	value string
	apply func(*config.Configuration, string) error
}

// SeededNames lists the first-run variables that were set, whatever became of
// them.
func (self *Bootstrap) SeededNames() []string {
	names := make([]string, 0, len(self.seeded))
	for _, variable := range self.seeded {
		names = append(names, Prefix+variable.name)
	}
	return names
}

// ReportIgnoredSeed says which variables disagree with the configuration that
// is actually in force.
//
// This is the confusing half of the design: an operator edits
// TEANODE_SERVER_NAME in their compose file, restarts, and nothing changes,
// because the database already has an answer and the database wins.
//
// Only the ones that disagree are reported. A compose file sets all of these
// on every start and almost always sets them to what is already stored;
// warning about those too would put a wall of text in the log on every
// restart, and a warning that is always there is one nobody reads.
func (self *Bootstrap) ReportIgnoredSeed(current *config.Configuration) {
	ignored, err := self.WouldChange(current)
	if err != nil {
		log.Warningf("cannot compare the environment with the stored configuration: %s", err)
		return
	}
	if len(ignored) == 0 {
		return
	}

	log.Warningf("ignoring %s: they disagree with what this database holds, and the database wins. "+
		"Change these in the dashboard, or load a whole configuration with \"teanode config import\"",
		strings.Join(ignored, ", "))
}

// WouldChange names the first-run variables that disagree with a stored
// configuration — the ones whose value is not already in force.
func (self *Bootstrap) WouldChange(current *config.Configuration) ([]string, error) {
	var names []string
	for _, variable := range self.seeded {
		differs, err := variable.wouldChange(current)
		if err != nil {
			return nil, fmt.Errorf("%s%s: %w", Prefix, variable.name, err)
		}
		if differs {
			names = append(names, Prefix+variable.name)
		}
	}
	return names, nil
}

// wouldChange reports whether applying this variable to the stored
// configuration would change anything.
//
// By applying it to a copy and comparing, rather than by reading the field it
// targets: the table says how to write each variable and nothing says how to
// read one back, and keeping a second mapping for that is how the two drift.
func (self *seededVariable) wouldChange(current *config.Configuration) (bool, error) {
	changed, err := config.Clone(current)
	if err != nil {
		return false, err
	}
	if err := self.apply(changed, self.value); err != nil {
		return false, err
	}

	before, err := yaml.Marshal(current)
	if err != nil {
		return false, err
	}
	after, err := yaml.Marshal(changed)
	if err != nil {
		return false, err
	}
	return !bytes.Equal(before, after), nil
}

// lookup reads one variable.
//
// A variable that is set but empty means empty, not absent. It has to: some
// of these clear a default rather than replace it — a development box turns
// off the HTTPS listener by setting TEANODE_LISTEN_HTTPS to nothing — and
// there would otherwise be no way to say so.
//
// The cost is that a half-filled env file is taken at its word. "teanode
// config env" therefore comments out the keys it has no value for, rather
// than writing them empty.
// databaseVariables is every variable that describes the connection. Named
// here rather than inline so that Variables can report them.
var databaseVariables = []string{
	"DATABASE_URL", "DATABASE_HOST", "DATABASE_PORT", "DATABASE_USER",
	"DATABASE_PASSWORD", "DATABASE_NAME", "DATABASE_SSL_MODE", "DATABASE_LOG_QUERIES",
}

// Variables is every environment variable this package reads, fully prefixed.
//
// Derived from the tables that read them rather than listed a second time,
// because a second list is one that goes out of date the first time somebody
// adds a variable and does not know it exists.
func Variables() []string {
	names := make([]string, 0, len(databaseVariables)+len(seedVariables)+1)
	for _, name := range databaseVariables {
		names = append(names, Prefix+name)
	}
	names = append(names, Prefix+"INSTANCE_ID")
	for _, variable := range seedVariables {
		names = append(names, Prefix+variable.name)
	}
	return names
}

func lookup(name string) (string, bool) {
	value, ok := os.LookupEnv(Prefix + name)
	if !ok {
		return "", false
	}
	return strings.TrimSpace(value), true
}

func splitList(value string) []string {
	var items []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
}

func parseBool(value string, target *bool) error {
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fmt.Errorf("%q is not true or false", value)
	}
	*target = parsed
	return nil
}

func parsePort(value string, target *uint16) error {
	parsed, err := strconv.ParseUint(value, 10, 16)
	if err != nil {
		return fmt.Errorf("%q is not a port number", value)
	}
	*target = uint16(parsed)
	return nil
}
