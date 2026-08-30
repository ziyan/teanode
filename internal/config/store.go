package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/ziyan/teanode/internal/util/atomicfile"
)

var (
	// ErrReadOnly is returned by Update on a store opened for reading only,
	// such as the one "teanode config validate" uses.
	ErrReadOnly = errors.New("config: store is read only")
)

// Store owns the configuration. It holds the active configuration in memory,
// hands out snapshots that callers treat as read only, and persists changes
// wherever the implementation keeps them — for the server, the database; see
// internal/configdb.
//
// The mail path reads Current() per message rather than caching it, so a
// change made in the dashboard takes effect on the next message without a
// restart.
type Store interface {
	// Current returns the active configuration. Treat it as read only;
	// change it through Update.
	Current() *Configuration

	// Update applies a change. The function receives a copy of the active
	// configuration; if it returns nil, the copy is validated and stored, and
	// only then becomes active. On any failure the active configuration and
	// the stored one are both left untouched.
	//
	// The function may be called more than once. An implementation backed by
	// something several processes share re-runs it against a fresh copy when
	// another process committed in between, so it has to be a function of the
	// configuration it is given rather than of anything captured beforehand.
	Update(func(*Configuration) error) error

	// Reload re-reads from wherever the configuration is kept.
	Reload() error

	// Subscribe registers a function called after every successful Update or
	// Reload, for components that need to react to a change. The returned
	// function unregisters it.
	Subscribe(func(*Configuration)) func()

	// Filename says where this store reads and writes, for messages that
	// point an operator at it.
	Filename() string

	Close() error
}

type memoryStore struct {
	readOnly bool

	mutex   sync.RWMutex
	current *Configuration

	subscribersMutex sync.Mutex
	subscribers      map[uint64]func(*Configuration)
	nextSubscriberID uint64
}

// NewMemoryStore returns a store that keeps the configuration in memory and
// nowhere else.
//
// For tests, and for the commands that construct a configuration to look at
// rather than to persist. A server uses configdb.Open, because a change made
// on one instance has to reach the others.
func NewMemoryStore(configuration *Configuration) Store {
	return &memoryStore{
		current:     configuration,
		subscribers: make(map[uint64]func(*Configuration)),
	}
}

// NewReadOnlyStore returns a store that refuses to change anything.
func NewReadOnlyStore(configuration *Configuration) Store {
	return &memoryStore{
		readOnly:    true,
		current:     configuration,
		subscribers: make(map[uint64]func(*Configuration)),
	}
}

func (self *memoryStore) Filename() string {
	return "memory"
}

func (self *memoryStore) Current() *Configuration {
	self.mutex.RLock()
	defer self.mutex.RUnlock()
	return self.current
}

// Reload has nothing to re-read, so it only tells the subscribers, which is
// what a caller reloading is asking for.
func (self *memoryStore) Reload() error {
	self.notify(self.Current())
	return nil
}

func (self *memoryStore) Update(mutate func(*Configuration) error) error {
	if self.readOnly {
		return ErrReadOnly
	}

	updated, err := self.apply(mutate)
	if err != nil {
		return err
	}
	self.notify(updated)
	return nil
}

// apply does the locked part of an update.
//
// The unlock is deferred rather than repeated on each failure path, because
// the mutation is caller-supplied code: a panic inside it would otherwise
// leave the store locked forever, and every later request — including reading
// the configuration to deliver a message — would block behind it.
func (self *memoryStore) apply(mutate func(*Configuration) error) (*Configuration, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	updated, err := clone(self.current)
	if err != nil {
		return nil, err
	}
	if err := mutate(updated); err != nil {
		return nil, err
	}

	// The mutation may have both read and changed the snapshot. A read builds
	// the lookup tables, so anything added afterwards would be missing from
	// them; rebuild on next use.
	updated.invalidateIndex()

	if err := updated.Validate(); err != nil {
		return nil, err
	}
	self.current = updated
	return updated, nil
}

func (self *memoryStore) Subscribe(subscriber func(*Configuration)) func() {
	self.subscribersMutex.Lock()
	defer self.subscribersMutex.Unlock()

	self.nextSubscriberID++
	id := self.nextSubscriberID
	self.subscribers[id] = subscriber

	return func() {
		self.subscribersMutex.Lock()
		defer self.subscribersMutex.Unlock()
		delete(self.subscribers, id)
	}
}

func (self *memoryStore) notify(configuration *Configuration) {
	// Copy the list under the lock, then call them outside it. A subscriber
	// that tried to subscribe or unsubscribe would otherwise deadlock.
	subscribers := self.currentSubscribers()
	for _, subscriber := range subscribers {
		subscriber(configuration)
	}
}

func (self *memoryStore) currentSubscribers() []func(*Configuration) {
	self.subscribersMutex.Lock()
	defer self.subscribersMutex.Unlock()

	subscribers := make([]func(*Configuration), 0, len(self.subscribers))
	for _, subscriber := range self.subscribers {
		subscribers = append(subscribers, subscriber)
	}
	return subscribers
}

func (self *memoryStore) Close() error {
	return nil
}

// Load parses one configuration file without creating a store. Defaults are
// applied for every field the file leaves out, and the result is validated.
func Load(filename string) (*Configuration, error) {
	content, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("config: cannot read %s: %w", filename, err)
	}
	configuration, err := Parse(content)
	if err != nil {
		return nil, fmt.Errorf("config: %s: %w", filename, err)
	}

	// Relative paths in the file are relative to the file, not to wherever
	// the command happened to be run from.
	absolute, err := filepath.Abs(filename)
	if err != nil {
		return nil, fmt.Errorf("config: cannot resolve %s: %w", filename, err)
	}
	configuration.SetBaseDirectory(filepath.Dir(absolute))
	return configuration, nil
}

// Parse decodes configuration from YAML, applying defaults first so that a
// short file still produces a complete configuration. Unknown fields are an
// error: a misspelled key would otherwise be silently ignored, which is how
// operators end up with a setting that appears to do nothing.
func Parse(content []byte) (*Configuration, error) {
	configuration := Default()
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)
	if err := decoder.Decode(configuration); err != nil {
		return nil, err
	}
	// Settings an older release wrote are adopted or reported before the
	// result is judged, so upgrading does not fail on a file that was valid
	// yesterday.
	configuration.migrateDeprecated()

	if err := configuration.Validate(); err != nil {
		return nil, err
	}
	return configuration, nil
}

// Clone returns a deep copy, which is what a caller mutates before the change
// is validated and stored. Exported so a store implemented elsewhere can do
// the same thing rather than a shallower version of it.
//
// The copy goes through YAML, which is the same representation an export
// uses and therefore cannot drift from it. A fresh value also gets a fresh
// lookup index, so a mutated snapshot never serves stale compiled patterns.
func Clone(configuration *Configuration) (*Configuration, error) {
	return clone(configuration)
}

// Adopt copies every setting from another configuration into this one.
//
// Field by field over the exported fields rather than one struct assignment,
// because a Configuration carries an unexported lookup index with a mutex in
// it. Assigning the whole struct copies that lock, and hands this
// configuration the compiled alias patterns belonging to the other one.
func (self *Configuration) Adopt(other *Configuration) {
	source := reflect.ValueOf(other).Elem()
	target := reflect.ValueOf(self).Elem()
	for index := 0; index < source.NumField(); index++ {
		if !source.Type().Field(index).IsExported() {
			continue
		}
		target.Field(index).Set(source.Field(index))
	}
	self.invalidateIndex()
}

func clone(configuration *Configuration) (*Configuration, error) {
	content, err := yaml.Marshal(configuration)
	if err != nil {
		return nil, fmt.Errorf("config: cannot copy configuration: %w", err)
	}
	copied := &Configuration{}
	if err := yaml.Unmarshal(content, copied); err != nil {
		return nil, fmt.Errorf("config: cannot copy configuration: %w", err)
	}
	// The base directory is not in the file, so carry it over by hand.
	copied.SetBaseDirectory(configuration.baseDirectory)
	return copied, nil
}

// header is written above every configuration file this program produces.
const header = `# TeaNode configuration.
#
# A snapshot of what one server was configured with. The server itself keeps
# its configuration in its database, so this file is not read at startup and
# editing it changes nothing; load it back with:
#
#     teanode config import --file <this file>
#
# It holds signing keys, credential keys and the server secret, so it is as
# sensitive as a private key. Paths are relative to server.dataDirectory
# unless they start with a "/".
# Documentation: https://github.com/ziyan/teanode/blob/main/docs/configuration.md
`

// Save writes a configuration to disk atomically: the new content goes to a
// temporary file in the same directory which is then renamed over the target,
// so a crash or a full disk leaves the previous file intact rather than a
// half-written one.
//
// Nothing in the server calls this. It is how "teanode config export" makes a
// backup, and how a test builds one to load.
func Save(filename string, configuration *Configuration) error {
	var buffer bytes.Buffer
	buffer.WriteString(header)

	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(configuration); err != nil {
		return fmt.Errorf("config: cannot encode configuration: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return fmt.Errorf("config: cannot encode configuration: %w", err)
	}

	file, err := atomicfile.Create(filename)
	if err != nil {
		return fmt.Errorf("config: cannot write %s: %w", filename, err)
	}
	defer func() {
		_ = atomicfile.Discard(file)
	}()

	// The file holds signing keys, credential keys and the server secret, so
	// it is readable only by the user that wrote it. Set the mode on the
	// temporary file, before it is renamed into place, so the final file is
	// never briefly world readable.
	if err := file.Chmod(0o600); err != nil {
		return fmt.Errorf("config: cannot write %s: %w", filename, err)
	}
	if _, err := file.Write(buffer.Bytes()); err != nil {
		return fmt.Errorf("config: cannot write %s: %w", filename, err)
	}
	if err := atomicfile.Commit(file); err != nil {
		return fmt.Errorf("config: cannot write %s: %w", filename, err)
	}
	return nil
}
