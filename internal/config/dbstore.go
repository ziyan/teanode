package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/util/deferutil"
)

// The store over the configuration table: one row per section, as YAML.
//
// Both directions of the mapping are here, next to each other, because the
// only thing that can go wrong is the two disagreeing — a section written by
// one and not read by the other is a setting that silently resets.

// settingSections are the parts of the configuration, each one row.
const (
	settingServer    = "server"
	settingListen    = "listen"
	settingTls       = "tls"
	settingSmtp      = "smtp"
	settingDkim      = "dkim"
	settingSession   = "session"
	settingDns       = "dns"
	settingAntivirus = "antivirus"
	settingAntispam  = "antispam"
	settingGeoIp     = "geoip"
	settingStorage   = "storage"
	settingPasskey   = "passkey"
	settingUpgrade   = "upgrade"
)

func (self *Configuration) sections() map[string]any {
	return map[string]any{
		settingServer:    &self.Server,
		settingListen:    &self.Listen,
		settingTls:       &self.TLS,
		settingSmtp:      &self.SMTP,
		settingDkim:      &self.DKIM,
		settingSession:   &self.Session,
		settingDns:       &self.DNS,
		settingAntivirus: &self.Antivirus,
		settingAntispam:  &self.Antispam,
		settingGeoIp:     &self.GeoIP,
		settingStorage:   &self.Storage,
		settingPasskey:   &self.Passkey,
		settingUpgrade:   &self.Upgrade,
	}
}

// FromRows builds a configuration from what the database holds.
//
// Defaults first, then the stored values on top, so a setting added in a new
// release has its default on a database written by an older one rather than
// its zero value — which for a timeout or a port is not a default, it is a
// server that does not work.
func FromRows(rows *db.ConfigurationRows) (*Configuration, error) {
	configuration := Default()
	for key, target := range configuration.sections() {
		stored, ok := rows.Settings[key]
		if !ok || len(stored) == 0 {
			continue
		}
		if err := yaml.Unmarshal([]byte(stored), target); err != nil {
			return nil, fmt.Errorf("config: cannot read the %q settings: %w", key, err)
		}
	}
	return configuration, nil
}

// ToRows turns a configuration into rows.
func ToRows(self *Configuration, version int64) (*db.ConfigurationRows, error) {
	rows := &db.ConfigurationRows{Version: version, Settings: map[string]string{}}
	for key, value := range self.sections() {
		encoded, err := yaml.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("config: cannot write the %q settings: %w", key, err)
		}
		rows.Settings[key] = string(encoded)
	}
	return rows, nil
}

// pollInterval is how often an instance checks whether another one changed
// the settings.
//
// It reads one row — the version — and only fetches the rest when that
// number has moved, so the cost of checking often is a single indexed lookup.
const pollInterval = 5 * time.Second

// retries is how many times a change is re-applied after losing a race with
// another instance. Two instances editing the settings at the same moment is
// rare; three of them doing it repeatedly is not a thing that happens, and
// looping forever on it would be worse than failing.
const retries = 3

type dbStore struct {
	database db.Database

	// connection is how this process reached that database. It is not stored
	// with the rest — it cannot be — so it is put back on every configuration
	// handed out.
	connection Database

	mutex   sync.RWMutex
	current *Configuration
	version int64

	subscribersMutex sync.Mutex
	subscribers      map[int]func(*Configuration)
	nextSubscriber   int

	stop chan struct{}
	done chan struct{}
}

// OpenStore reads the settings from the database and keeps them up to date.
//
// The connection is what this process used to get here, and is reported as
// part of every configuration the store hands out.
func OpenStore(database db.Database, connection Database) (Store, error) {
	self := &dbStore{
		database:    database,
		connection:  connection,
		subscribers: map[int]func(*Configuration){},
		stop:        make(chan struct{}),
		done:        make(chan struct{}),
	}
	if err := self.Reload(); err != nil {
		return nil, err
	}
	go self.watch()
	return self, nil
}

// validatePaths refuses a relative data directory.
//
// A relative path would land wherever the process happened to be started
// from — which is a different place for the server, for the command line, and
// for a container that changed its working directory. Two instances would
// disagree about where the spool is, and neither would say so.
func validatePaths(configuration *Configuration) error {
	directory := configuration.Server.DataDirectory
	if directory != "" && !filepath.IsAbs(directory) {
		return fmt.Errorf("config: server.dataDirectory is %q, which is relative; "+
			"it has to be an absolute path now that there is no configuration file to resolve it against", directory)
	}
	return nil
}

func (self *dbStore) Current() *Configuration {
	self.mutex.RLock()
	defer self.mutex.RUnlock()
	return self.current
}

// Filename says where the settings live, for the messages that used to name
// a file.
func (self *dbStore) Filename() string {
	return "the database"
}

func (self *dbStore) Reload() error {
	rows, err := self.database.LoadConfiguration()
	if err != nil {
		return err
	}
	configuration, err := FromRows(rows)
	if err != nil {
		return err
	}
	// Validated on the way in as well as on the way out: it may have been
	// written by a newer release, or by hand.
	if err := configuration.Validate(); err != nil {
		return fmt.Errorf("config: the stored configuration is not usable: %w", err)
	}
	if err := validatePaths(configuration); err != nil {
		return err
	}
	configuration.Database = self.connection

	// The domain table's secrets are sealed with the server secret, which
	// has just been read.
	if err := self.database.SetSecret(configuration.Secret()); err != nil {
		return err
	}

	self.store(configuration, rows.Version)
	self.notify(configuration)
	return nil
}

func (self *dbStore) snapshot() (*Configuration, int64) {
	self.mutex.RLock()
	defer self.mutex.RUnlock()
	return self.current, self.version
}

func (self *dbStore) store(configuration *Configuration, version int64) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.current = configuration
	self.version = version
}

// Update applies a change, and retries it against a fresh copy when another
// instance got there first.
//
// The mutation runs again rather than being merged, because a mutation is a
// function of the configuration it was given.
func (self *dbStore) Update(mutate func(*Configuration) error) error {
	for attempt := 0; ; attempt++ {
		base, version := self.snapshot()

		changed, err := Clone(base)
		if err != nil {
			return err
		}
		if err := mutate(changed); err != nil {
			return err
		}
		changed.Database = self.connection
		if err := changed.Validate(); err != nil {
			return err
		}

		rows, err := ToRows(changed, version)
		if err != nil {
			return err
		}
		saved, err := self.database.SaveConfiguration(rows)
		if err == nil {
			if err := self.database.SetSecret(changed.Secret()); err != nil {
				return err
			}
			self.store(changed, saved)
			self.notify(changed)
			return nil
		}
		if !errors.Is(err, db.ErrConfigurationChanged) || attempt >= retries {
			return err
		}
		log.Noticef("the configuration changed while a change was being made; retrying against the new one")
		if err := self.Reload(); err != nil {
			return err
		}
	}
}

func (self *dbStore) Subscribe(subscriber func(*Configuration)) func() {
	self.subscribersMutex.Lock()
	defer self.subscribersMutex.Unlock()
	id := self.nextSubscriber
	self.nextSubscriber++
	self.subscribers[id] = subscriber
	return func() {
		self.subscribersMutex.Lock()
		defer self.subscribersMutex.Unlock()
		delete(self.subscribers, id)
	}
}

func (self *dbStore) Close() error {
	close(self.stop)
	<-self.done
	return nil
}

// watch notices a change made by another instance, by asking for the version
// rather than by listening: one indexed read every five seconds is cheaper
// than keeping a LISTEN connection alive.
func (self *dbStore) watch() {
	defer deferutil.Recover()
	defer close(self.done)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-self.stop:
			return
		case <-ticker.C:
			version, err := self.database.ConfigurationVersion()
			if err != nil {
				log.Warningf("could not check the configuration version: %s", err)
				continue
			}
			self.mutex.RLock()
			current := self.version
			self.mutex.RUnlock()
			if version == current {
				continue
			}
			log.Noticef("the configuration changed elsewhere, from version %d to %d; reloading", current, version)
			if err := self.Reload(); err != nil {
				log.Errorf("could not reload the configuration: %s", err)
			}
		}
	}
}

func (self *dbStore) notify(configuration *Configuration) {
	for _, subscriber := range self.subscriberList() {
		subscriber(configuration)
	}
}

func (self *dbStore) subscriberList() []func(*Configuration) {
	self.subscribersMutex.Lock()
	defer self.subscribersMutex.Unlock()
	subscribers := make([]func(*Configuration), 0, len(self.subscribers))
	for _, subscriber := range self.subscribers {
		subscribers = append(subscribers, subscriber)
	}
	return subscribers
}

// LoadStored reads the stored settings without judging them, for the callers
// that need to know what is there rather than to run on it.
func LoadStored(database db.Database) (*Configuration, error) {
	rows, err := database.LoadConfiguration()
	if err != nil {
		return nil, err
	}
	return FromRows(rows)
}

// Replace writes whole settings over whatever is stored, and reports what
// was there before. The one operation that replaces rather than changes, so
// it has nothing to reload and nobody to notify. The caller is expected to
// have validated what it is storing.
func Replace(database db.Database, configuration *Configuration) (*Configuration, error) {
	rows, err := database.LoadConfiguration()
	if err != nil {
		return nil, err
	}
	existing, err := FromRows(rows)
	if err != nil {
		return nil, err
	}
	replacement, err := ToRows(configuration, rows.Version)
	if err != nil {
		return nil, err
	}
	if _, err := database.SaveConfiguration(replacement); err != nil {
		return nil, err
	}
	return existing, nil
}

// Initialize writes a seed into a database that has no settings in it yet,
// and reports whether it did. The seed is described lazily, by the function
// passed in, so that the cost and the demands of building one fall only on a
// start that actually needs it.
//
// Two instances starting against the same empty database is a race that
// resolves itself: the write carries version zero, the stored version is
// still zero only for the one that gets there first, and the other is told
// the configuration changed and reports that it did not seed.
func Initialize(database db.Database, describe func() (*Configuration, error)) (bool, error) {
	version, err := database.ConfigurationVersion()
	if err != nil {
		return false, err
	}
	if version != 0 {
		return false, nil
	}
	seed, err := describe()
	if err != nil {
		return false, err
	}
	rows, err := ToRows(seed, 0)
	if err != nil {
		return false, err
	}
	if _, err := database.SaveConfiguration(rows); err != nil {
		if errors.Is(err, db.ErrConfigurationChanged) {
			log.Noticef("another instance configured this database first; using what it wrote")
			return false, nil
		}
		return false, err
	}
	log.Noticef("this database had no configuration; wrote the one described by the environment")
	return true, nil
}
