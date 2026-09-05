package configdb

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/op/go-logging"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
)

var log = logging.MustGetLogger("configdb")

// pollInterval is how often an instance checks whether another one changed
// the configuration.
//
// It reads one row — the version — and only fetches the rest when that
// number has moved, so the cost of checking often is a single indexed lookup.
// Five seconds is chosen so that adding a domain in the dashboard is serving
// mail on every instance before the operator has finished reading the DNS
// records they were just shown.
const pollInterval = 5 * time.Second

// retries is how many times a change is re-applied after losing a race with
// another instance. Two instances editing the same configuration at the same
// moment is rare; three of them doing it repeatedly is not a thing that
// happens, and looping forever on it would be worse than failing.
const retries = 3

type store struct {
	database db.Database

	// connection is how this process reached that database. It is not stored
	// with the rest — it cannot be — so it is put back on every configuration
	// handed out, or "teanode config show" would print the defaults and an
	// operator would go looking for a server on the wrong host.
	connection config.Database

	mutex   sync.RWMutex
	current *config.Configuration
	version int64

	subscribersMutex sync.Mutex
	subscribers      map[int]func(*config.Configuration)
	nextSubscriber   int

	stop chan struct{}
	done chan struct{}
}

// Open reads the configuration from the database and keeps it up to date.
//
// The returned store satisfies config.Store, so everything that already takes
// one — the mail path, the resolvers, the command line client — works against
// the database without knowing it changed.
//
// The connection is what this process used to get here, and is reported as
// part of every configuration the store hands out.
func Open(database db.Database, connection config.Database) (config.Store, error) {
	self := &store{
		database:    database,
		connection:  connection,
		subscribers: map[int]func(*config.Configuration){},
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
// It used to resolve against the directory holding teanode.yaml. There is no
// such file now, so a relative path would land wherever the process happened
// to be started from — which is a different place for the server, for the
// command line, and for a container that changed its working directory. Two
// instances would disagree about where the spool is, and neither would say
// so.
func validatePaths(configuration *config.Configuration) error {
	directory := configuration.Server.DataDirectory
	if directory != "" && !filepath.IsAbs(directory) {
		return fmt.Errorf("configdb: server.dataDirectory is %q, which is relative; "+
			"it has to be an absolute path now that there is no configuration file to resolve it against", directory)
	}
	return nil
}

func (self *store) Current() *config.Configuration {
	self.mutex.RLock()
	defer self.mutex.RUnlock()
	return self.current
}

// Filename says where the configuration lives, for the messages that used to
// name a file. There is no file any more, and saying so is more useful than
// an empty string somebody has to guess the meaning of.
func (self *store) Filename() string {
	return "the database"
}

func (self *store) Reload() error {
	rows, err := self.database.LoadConfiguration()
	if err != nil {
		return err
	}
	configuration, err := FromRows(rows)
	if err != nil {
		return err
	}

	// A stored configuration is validated on the way in as well as on the way
	// out: it may have been written by a newer release, or by hand.
	if err := configuration.Validate(); err != nil {
		return fmt.Errorf("configdb: the stored configuration is not usable: %w", err)
	}
	if err := validatePaths(configuration); err != nil {
		return err
	}
	configuration.Database = self.connection

	self.store(configuration, rows.Version)
	self.notify(configuration)
	return nil
}

// store replaces the snapshot this instance is serving.
//
// A function of its own so the unlock can be deferred: the assignment cannot
// panic today, but a lock released only by the line after it is a lock that
// stays held the first time something between them can. The notify that
// follows every call to this deliberately happens outside it — it calls
// subscribers, and a subscriber that reads the configuration under a lock
// this still held would deadlock.
func (self *store) store(configuration *config.Configuration, version int64) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.current = configuration
	self.version = version
}

// Update applies a change, and retries it against a fresh copy when another
// instance got there first.
//
// The mutation runs again rather than being merged, because a mutation is a
// function of the configuration it was given — "add this domain" applied to
// the newer configuration is the right answer, where merging two documents
// would be a guess.
func (self *store) Update(mutate func(*config.Configuration) error) error {
	for attempt := 0; ; attempt++ {
		self.mutex.RLock()
		base, version := self.current, self.version
		self.mutex.RUnlock()

		changed, err := config.Clone(base)
		if err != nil {
			return err
		}
		if err := mutate(changed); err != nil {
			return err
		}

		// The mutation may have both read and changed the snapshot. A read
		// builds the lookup tables, so anything added afterwards would be
		// missing from them; rebuild on next use.
		changed.InvalidateIndex()

		// Whatever a mutation did to it, the connection is the one in use.
		// Put back before validating, not after, so that what is checked is
		// what will be in force.
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
			self.store(changed, saved)
			self.notify(changed)
			return nil
		}

		if !errors.Is(err, db.ErrConfigurationChanged) || attempt >= retries {
			return err
		}

		// Somebody else committed in between. Take their configuration and
		// apply this change to it.
		log.Noticef("the configuration changed while a change was being made; retrying against the new one")
		if err := self.Reload(); err != nil {
			return err
		}
	}
}

func (self *store) Subscribe(subscriber func(*config.Configuration)) func() {
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

func (self *store) Close() error {
	close(self.stop)
	<-self.done
	return nil
}

// watch notices a change made by another instance.
//
// By asking for the version rather than by listening: LISTEN holds a
// connection open for the life of the process and has to be re-established
// after every network hiccup, and the thing being avoided — a second of
// staleness — is not worth that. One indexed read every five seconds is
// cheaper than the reconnection logic.
func (self *store) watch() {
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

func (self *store) notify(configuration *config.Configuration) {
	for _, subscriber := range self.subscriberList() {
		subscriber(configuration)
	}
}

// subscriberList is a copy of the subscribers, taken under the lock.
//
// The copy is the point: a subscriber is somebody else's function and may do
// anything, including reading the configuration, so calling one while holding
// this lock is a deadlock waiting for the right subscriber. Taking the copy in
// a function of its own lets the unlock be deferred, so a panic in the loop
// below cannot leave the list locked for the life of the process.
func (self *store) subscriberList() []func(*config.Configuration) {
	self.subscribersMutex.Lock()
	defer self.subscribersMutex.Unlock()
	subscribers := make([]func(*config.Configuration), 0, len(self.subscribers))
	for _, subscriber := range self.subscribers {
		subscribers = append(subscribers, subscriber)
	}
	return subscribers
}

// Load reads the stored configuration without judging it.
//
// For the callers that need to know what is there rather than to run on it —
// deciding whether a database is already configured, before replacing what it
// holds. A store validates instead, which is right for a server and wrong
// here: a database that has only just been migrated holds a configuration
// that is not a usable server, and that is not an error to report, it is the
// answer to the question.
func Load(database db.Database) (*config.Configuration, error) {
	rows, err := database.LoadConfiguration()
	if err != nil {
		return nil, err
	}
	return FromRows(rows)
}

// Replace writes a whole configuration over whatever is stored, and reports
// what was there before.
//
// Deliberately not routed through a Store. A store insists that what it reads
// is a usable server, which a database that has only just been migrated is
// not — and refusing to load a configuration into an empty database because
// the empty database is not a valid server is the wrong way round. This is
// the one operation that replaces rather than changes, so it has nothing to
// reload and nobody to notify.
//
// The caller is expected to have validated what it is storing; config.Load
// does.
func Replace(database db.Database, configuration *config.Configuration) (*config.Configuration, error) {
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

// Initialize writes a seed into a database that has no configuration in it
// yet, and reports whether it did. The seed is described lazily, by the
// function passed in, so that the cost and the demands of building one fall
// only on a start that actually needs it.
//
// This is the one moment the environment is allowed to decide what a server
// looks like. After it, the database is the answer, and the same variables
// are ignored — see bootstrap.Bootstrap.ReportIgnoredSeed, which is what says
// so out loud.
//
// Two instances starting against the same empty database is a race that
// resolves itself: the write carries version zero, the stored version is
// still zero only for the one that gets there first, and the other is told
// the configuration changed and reports that it did not seed.
func Initialize(database db.Database, describe func() (*config.Configuration, error)) (bool, error) {
	version, err := database.ConfigurationVersion()
	if err != nil {
		return false, err
	}
	if version != 0 {
		return false, nil
	}

	// Asked for only now, because building it demands settings that a start
	// against a database that is already configured has no reason to have.
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
